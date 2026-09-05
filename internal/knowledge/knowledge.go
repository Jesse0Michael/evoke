package knowledge

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"
)

// ragPrefix introduces retrieved material to the model. It tells the model to
// use the material without narrating that it was handed any, so retrieval does
// not leak into the character's voice.
const ragPrefix = "Use the following reference material to inform your response. " +
	"If the material is relevant, draw on it naturally. " +
	"Do not mention that you were given reference material.\n\n---\n\n"

// Config holds the parameters needed to open a knowledge base.
type Config struct {
	// DBPath is the resolved absolute path to the SQLite knowledge database.
	DBPath string
	// EmbedModel overrides the embedding model. Leave empty to use the model the
	// database records having been built with.
	EmbedModel string
	// EmbedURL is the ollama-compatible API base URL (default: DefaultEmbedURL).
	EmbedURL string
	// TopK is the number of chunks to retrieve per query (default: DefaultTopK).
	TopK int
}

// Base provides retrieval-augmented generation: it holds a pre-built vector
// database of text chunks in memory, embeds queries, and returns the chunks
// most relevant to each one for injection into the conversation.
type Base struct {
	chunks   []storedChunk
	topK     int
	embedder *Embedder
	meta     Meta
}

type storedChunk struct {
	file    string
	heading string
	content string
	vec     []float32
}

// Open loads a knowledge database into memory for fast retrieval. The database
// must contain a `chunks` table with columns: id, file, heading, content,
// embedding (BLOB of little-endian float32 values).
func Open(cfg Config) (*Base, error) {
	if cfg.DBPath == "" {
		return nil, fmt.Errorf("knowledge database path is empty")
	}
	if cfg.TopK <= 0 {
		cfg.TopK = DefaultTopK
	}

	db, err := sql.Open("sqlite3", cfg.DBPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("failed to open knowledge database: %w", err)
	}
	defer func() { _ = db.Close() }()

	meta, err := readMeta(db)
	if err != nil {
		return nil, err
	}

	// The database records the model that produced its vectors. Querying it with
	// a different model yields meaningless cosine scores rather than an error,
	// so an explicit mismatch has to fail loudly here.
	model := cfg.EmbedModel
	switch {
	case model == "":
		model = cmpOr(meta.EmbedModel, DefaultEmbedModel)
	case meta.EmbedModel != "" && meta.EmbedModel != model:
		return nil, fmt.Errorf(
			"knowledge database %s was built with embedding model %q but %q is configured; rebuild it with `evoke knowledge` or set embed_model=%s on the KNOWLEDGE declaration",
			cfg.DBPath, meta.EmbedModel, model, meta.EmbedModel)
	}

	chunks, err := loadChunks(db)
	if err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return nil, fmt.Errorf("knowledge database %s contains no chunks", cfg.DBPath)
	}
	if meta.Dims == 0 {
		meta.Dims = len(chunks[0].vec)
	}
	for _, c := range chunks {
		if len(c.vec) != meta.Dims {
			return nil, fmt.Errorf("knowledge database %s has mixed embedding sizes: %q is %d dimensions, expected %d",
				cfg.DBPath, c.heading, len(c.vec), meta.Dims)
		}
	}

	return &Base{
		chunks:   chunks,
		topK:     cfg.TopK,
		embedder: NewEmbedder(model, cfg.EmbedURL),
		meta:     meta,
	}, nil
}

func loadChunks(db *sql.DB) ([]storedChunk, error) {
	rows, err := db.Query("SELECT file, heading, content, embedding FROM chunks")
	if err != nil {
		return nil, fmt.Errorf("failed to query knowledge chunks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var chunks []storedChunk
	for rows.Next() {
		var c storedChunk
		var blob []byte
		if err := rows.Scan(&c.file, &c.heading, &c.content, &blob); err != nil {
			return nil, fmt.Errorf("failed to scan chunk row: %w", err)
		}
		c.vec, err = decodeFloat32Blob(blob)
		if err != nil {
			return nil, fmt.Errorf("failed to decode embedding for %q: %w", c.heading, err)
		}
		chunks = append(chunks, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate knowledge rows: %w", err)
	}
	return chunks, nil
}

// Meta returns how the loaded database was built.
func (b *Base) Meta() Meta { return b.meta }

// Len returns the number of chunks held in memory.
func (b *Base) Len() int { return len(b.chunks) }

// EmbedModel returns the model queries are embedded with — the one recorded in
// the database unless the declaration overrode it.
func (b *Base) EmbedModel() string { return b.embedder.Model() }

// Retrieve embeds the query and returns the most relevant text chunks,
// formatted as a single context string ready for injection into the
// conversation.
func (b *Base) Retrieve(ctx context.Context, query string) (string, error) {
	queryVec, err := b.embedder.Embed(ctx, query)
	if err != nil {
		return "", fmt.Errorf("failed to embed query: %w", err)
	}

	type scored struct {
		idx   int
		score float64
	}
	scores := make([]scored, len(b.chunks))
	for i, c := range b.chunks {
		scores[i] = scored{idx: i, score: cosineSimilarity(queryVec, c.vec)}
	}
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	topK := min(b.topK, len(scores))

	var sb strings.Builder
	sb.WriteString(ragPrefix)
	for i := range topK {
		c := b.chunks[scores[i].idx]
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

// decodeFloat32Blob decodes a BLOB of little-endian float32 values.
func decodeFloat32Blob(data []byte) ([]float32, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty embedding blob")
	}
	if len(data)%4 != 0 {
		return nil, fmt.Errorf("embedding blob size %d is not a multiple of 4", len(data))
	}
	vec := make([]float32, len(data)/4)
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &vec); err != nil {
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

// cmpOr returns a if non-empty, otherwise b.
func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
