// Package knowledge builds and reads the RAG vector databases referenced by
// KNOWLEDGE declarations. It owns both ends of the format: the builder walks a
// markdown corpus, splits it into heading-scoped chunks, embeds each chunk, and
// writes a SQLite database; the reader loads that database and retrieves the
// chunks most relevant to a query. Writer and reader share one embedder and one
// schema so a database is never built with a model the reader cannot match.
package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const (
	// DefaultEmbedModel is the ollama embedding model used when none is named.
	DefaultEmbedModel = "nomic-embed-text"
	// DefaultEmbedURL is the ollama-compatible API base URL.
	DefaultEmbedURL = "http://localhost:11434"
	// DefaultTopK is the number of chunks retrieved per query.
	DefaultTopK = 5
)

// Embedder turns text into vectors via an ollama-compatible /api/embed
// endpoint. The same embedder serves the builder and the query path, so a
// database and the queries run against it cannot drift onto different models.
type Embedder struct {
	model  string
	url    string
	client *http.Client
}

// NewEmbedder returns an Embedder for the given model and base URL. Empty
// values fall back to the package defaults.
func NewEmbedder(model, url string) *Embedder {
	if model == "" {
		model = DefaultEmbedModel
	}
	if url == "" {
		url = DefaultEmbedURL
	}
	return &Embedder{
		model:  model,
		url:    strings.TrimRight(url, "/"),
		client: http.DefaultClient,
	}
}

// Model returns the embedding model name.
func (e *Embedder) Model() string { return e.model }

// Embed returns the embedding vector for a single piece of text.
func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(map[string]any{
		"model": e.model,
		"input": text,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to encode embed request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to build embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed request to %s failed: %w", e.url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed endpoint returned status %d", resp.StatusCode)
	}

	// The JSON numbers decode as float64; the stored format is float32.
	var result struct {
		Embeddings [][]float64 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode embed response: %w", err)
	}
	if len(result.Embeddings) == 0 || len(result.Embeddings[0]) == 0 {
		return nil, fmt.Errorf("embed endpoint returned no vectors")
	}

	f64 := result.Embeddings[0]
	vec := make([]float32, len(f64))
	for i, v := range f64 {
		vec[i] = float32(v)
	}
	return vec, nil
}
