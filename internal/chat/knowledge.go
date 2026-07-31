package chat

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

const (
	defaultEmbedModel  = "nomic-embed-text"
	defaultEmbedURL    = "http://localhost:11434"
	defaultKnowledgeK  = 5
	defaultEmbedDims   = 768
	knowledgeRAGPrefix = "Use the following reference material to inform your response. " +
		"If the material is relevant, draw on it naturally. " +
		"Do not mention that you were given reference material.\n\n---\n\n"
)

// Knowledge provides retrieval-augmented generation: it loads a pre-built
// vector database of text chunks, embeds user queries via an ollama-compatible
// endpoint, and retrieves the most relevant chunks for context injection.
type Knowledge struct {
	chunks []chunk
	topK   int
	model  string
	url    string
	dims   int
}

type chunk struct {
	file    string
	heading string
	content string
	vec     []float32
}

// KnowledgeConfig holds the parameters needed to open a knowledge base.
type KnowledgeConfig struct {
	// DBPath is the resolved absolute path to the SQLite knowledge database.
	DBPath string
	// EmbedModel is the ollama embedding model name (default: nomic-embed-text).
	EmbedModel string
	// EmbedURL is the ollama API base URL (default: http://localhost:11434).
	EmbedURL string
	// TopK is the number of chunks to retrieve per query (default: 5).
	TopK int
}

// OpenKnowledge loads the knowledge database into memory for fast retrieval.
// The database must contain a `chunks` table with columns: id, file, heading,
// content, embedding (BLOB of little-endian float32 values).
func OpenKnowledge(cfg KnowledgeConfig) (*Knowledge, error) {
	if cfg.DBPath == "" {
		return nil, fmt.Errorf("knowledge database path is empty")
	}
	if cfg.EmbedModel == "" {
		cfg.EmbedModel = defaultEmbedModel
	}
	if cfg.EmbedURL == "" {
		cfg.EmbedURL = defaultEmbedURL
	}
	if cfg.TopK <= 0 {
		cfg.TopK = defaultKnowledgeK
	}

	db, err := sql.Open("sqlite3", cfg.DBPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("failed to open knowledge database: %w", err)
	}
	defer func() { _ = db.Close() }()

	rows, err := db.Query("SELECT file, heading, content, embedding FROM chunks")
	if err != nil {
		return nil, fmt.Errorf("failed to query knowledge chunks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var chunks []chunk
	for rows.Next() {
		var c chunk
		var embBlob []byte
		if err := rows.Scan(&c.file, &c.heading, &c.content, &embBlob); err != nil {
			return nil, fmt.Errorf("failed to scan chunk row: %w", err)
		}
		c.vec, err = decodeFloat32Blob(embBlob)
		if err != nil {
			return nil, fmt.Errorf("failed to decode embedding for %q: %w", c.heading, err)
		}
		chunks = append(chunks, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate knowledge rows: %w", err)
	}
	if len(chunks) == 0 {
		return nil, fmt.Errorf("knowledge database contains no chunks")
	}

	dims := len(chunks[0].vec)
	return &Knowledge{
		chunks: chunks,
		topK:   cfg.TopK,
		model:  cfg.EmbedModel,
		url:    strings.TrimRight(cfg.EmbedURL, "/"),
		dims:   dims,
	}, nil
}

// Retrieve embeds the query and returns the most relevant text chunks from the
// knowledge base, formatted as a single context string ready for injection into
// the conversation.
func (k *Knowledge) Retrieve(ctx context.Context, query string) (string, error) {
	queryVec, err := k.embed(ctx, query)
	if err != nil {
		return "", fmt.Errorf("failed to embed query: %w", err)
	}

	type scored struct {
		idx   int
		score float64
	}
	scores := make([]scored, len(k.chunks))
	for i, c := range k.chunks {
		scores[i] = scored{idx: i, score: cosineSimilarity(queryVec, c.vec)}
	}
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	topK := k.topK
	if topK > len(scores) {
		topK = len(scores)
	}

	var sb strings.Builder
	sb.WriteString(knowledgeRAGPrefix)
	for i := 0; i < topK; i++ {
		c := k.chunks[scores[i].idx]
		if i > 0 {
			sb.WriteString("\n\n---\n\n")
		}
		if c.heading != "" {
			sb.WriteString("## ")
			sb.WriteString(c.heading)
			sb.WriteString("\n\n")
		}
		sb.WriteString(c.content)
	}
	return sb.String(), nil
}

// embed calls the ollama-compatible /api/embed endpoint to get a vector for the
// given text.
func (k *Knowledge) embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(map[string]any{
		"model": k.model,
		"input": text,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, k.url+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embed returned status %d", resp.StatusCode)
	}

	var result struct {
		Embeddings [][]float64 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode embed response: %w", err)
	}
	if len(result.Embeddings) == 0 || len(result.Embeddings[0]) == 0 {
		return nil, fmt.Errorf("ollama returned empty embedding")
	}

	// Convert float64 (JSON default) to float32.
	f64 := result.Embeddings[0]
	vec := make([]float32, len(f64))
	for i, v := range f64 {
		vec[i] = float32(v)
	}
	return vec, nil
}

// decodeFloat32Blob decodes a BLOB of little-endian float32 values.
func decodeFloat32Blob(data []byte) ([]float32, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty embedding blob")
	}
	if len(data)%4 != 0 {
		return nil, fmt.Errorf("embedding blob size %d is not a multiple of 4", len(data))
	}
	n := len(data) / 4
	vec := make([]float32, n)
	reader := bytes.NewReader(data)
	if err := binary.Read(reader, binary.LittleEndian, &vec); err != nil {
		return nil, err
	}
	return vec, nil
}

// cosineSimilarity computes the cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
