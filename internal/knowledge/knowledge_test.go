package knowledge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeEmbedServer serves an ollama-compatible /api/embed that returns a fixed
// vector per input text, so retrieval ordering is deterministic.
func fakeEmbedServer(t *testing.T, vectors map[string][]float64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
			Input string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		vec, ok := vectors[req.Input]
		if !ok {
			http.Error(w, "no vector for input "+req.Input, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float64{vec}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// seedDB writes a database with three orthogonal unit vectors so a query
// matching one axis retrieves exactly that chunk first.
func seedDB(t *testing.T, meta Meta) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	store, err := CreateStore(path, meta)
	require.NoError(t, err)

	seed := []struct {
		chunk Chunk
		vec   []float32
	}{
		{Chunk{File: "a.md", Heading: "Fire", Content: "flames burn"}, []float32{1, 0, 0}},
		{Chunk{File: "b.md", Heading: "Water", Content: "waves crash"}, []float32{0, 1, 0}},
		{Chunk{File: "c.md", Heading: "Earth", Content: "stone holds"}, []float32{0, 0, 1}},
	}
	for i, s := range seed {
		require.NoError(t, store.Insert(i+1, s.chunk, s.vec))
	}
	require.NoError(t, store.Close())
	return path
}

func TestOpen_EmbedModelValidation(t *testing.T) {
	tests := []struct {
		name       string
		dbModel    string
		cfgModel   string
		wantError  bool
		wantActive string // the model the base ends up embedding with
	}{
		{
			name:       "unset config adopts the model the database was built with",
			dbModel:    "test-embed-model",
			cfgModel:   "",
			wantActive: "test-embed-model",
		},
		{
			name:       "matching model is accepted",
			dbModel:    "test-embed-model",
			cfgModel:   "test-embed-model",
			wantActive: "test-embed-model",
		},
		{
			name:      "conflicting model fails loudly rather than scoring garbage",
			dbModel:   "test-embed-model",
			cfgModel:  "test-other-model",
			wantError: true,
		},
		{
			name:       "a database without recorded metadata falls back to the configured model",
			dbModel:    "",
			cfgModel:   "test-other-model",
			wantActive: "test-other-model",
		},
		{
			name:       "a database without metadata and no configured model uses the default",
			dbModel:    "",
			cfgModel:   "",
			wantActive: DefaultEmbedModel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := seedDB(t, Meta{EmbedModel: tt.dbModel, Dims: 3})

			base, err := Open(Config{DBPath: path, EmbedModel: tt.cfgModel})

			require.Equal(t, tt.wantError, err != nil, "unexpected error state: %v", err)
			if tt.wantError {
				return
			}
			require.Equal(t, tt.wantActive, base.embedder.Model())
			require.Equal(t, 3, base.Len())
		})
	}
}

func TestOpen_Errors(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) string
	}{
		{
			name:  "empty path",
			setup: func(*testing.T) string { return "" },
		},
		{
			name: "database with no chunks",
			setup: func(t *testing.T) string {
				t.Helper()
				path := filepath.Join(t.TempDir(), "empty.db")
				store, err := CreateStore(path, Meta{EmbedModel: "test-embed-model", Dims: 3})
				require.NoError(t, err)
				require.NoError(t, store.Close())
				return path
			},
		},
		{
			name: "mixed embedding sizes",
			setup: func(t *testing.T) string {
				t.Helper()
				path := filepath.Join(t.TempDir(), "mixed.db")
				writeLegacyDB(t, path, []readChunk{
					{File: "a.md", Heading: "Fire", Content: "flames", Embedding: []float32{1, 0, 0}},
					{File: "b.md", Heading: "Water", Content: "waves", Embedding: []float32{0, 1}},
				})
				return path
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Open(Config{DBPath: tt.setup(t)})
			require.Error(t, err)
		})
	}
}

// TestOpen_LegacyDatabase covers a database written before the meta table
// existed — the shape the standalone embed tool produced.
func TestOpen_LegacyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	writeLegacyDB(t, path, []readChunk{
		{File: "a.md", Heading: "Fire", Content: "flames burn", Embedding: []float32{1, 0, 0}},
		{File: "b.md", Heading: "Water", Content: "waves crash", Embedding: []float32{0, 1, 0}},
	})

	base, err := Open(Config{DBPath: path})

	require.NoError(t, err)
	require.Equal(t, 2, base.Len())
	require.Equal(t, Meta{Dims: 3}, base.Meta())
}

func TestBase_Retrieve(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		queryVec []float64
		topK     int
		expected string
	}{
		{
			name:     "the nearest chunk is returned first",
			query:    "test-query-fire",
			queryVec: []float64{1, 0, 0},
			topK:     1,
			expected: ragPrefix + "## Fire\n\nflames burn",
		},
		{
			name:     "top_k returns several chunks ranked by similarity",
			query:    "test-query-earth",
			queryVec: []float64{0.1, 0, 1},
			topK:     2,
			expected: ragPrefix + "## Earth\n\nstone holds\n\n---\n\n## Fire\n\nflames burn",
		},
		{
			name:     "top_k larger than the corpus returns every chunk",
			query:    "test-query-water",
			queryVec: []float64{0, 1, 0},
			topK:     10,
			expected: ragPrefix + "## Water\n\nwaves crash\n\n---\n\n## Fire\n\nflames burn\n\n---\n\n## Earth\n\nstone holds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := fakeEmbedServer(t, map[string][]float64{tt.query: tt.queryVec})
			path := seedDB(t, Meta{EmbedModel: "test-embed-model", Dims: 3})

			base, err := Open(Config{DBPath: path, EmbedURL: srv.URL, TopK: tt.topK})
			require.NoError(t, err)

			got, err := base.Retrieve(t.Context(), tt.query)

			require.NoError(t, err)
			require.Equal(t, tt.expected, got)
		})
	}
}

func TestBase_RetrieveEmbedFailure(t *testing.T) {
	srv := fakeEmbedServer(t, nil) // every input 404s
	path := seedDB(t, Meta{EmbedModel: "test-embed-model", Dims: 3})

	base, err := Open(Config{DBPath: path, EmbedURL: srv.URL})
	require.NoError(t, err)

	_, err = base.Retrieve(t.Context(), "test-query")

	require.Error(t, err)
}

func TestDecodeFloat32Blob(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		expected  []float32
		wantError bool
	}{
		{
			name:      "round trips a serialized vector",
			input:     mustSerialize(t, []float32{1, -0.5, 3}),
			expected:  []float32{1, -0.5, 3},
			wantError: false,
		},
		{name: "empty blob", input: []byte{}, wantError: true},
		{name: "size not a multiple of four", input: []byte{1, 2, 3}, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeFloat32Blob(tt.input)

			require.Equal(t, tt.wantError, err != nil)
			require.Equal(t, tt.expected, got)
		})
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a, b     []float32
		expected float64
	}{
		{name: "identical vectors", a: []float32{1, 0}, b: []float32{1, 0}, expected: 1},
		{name: "orthogonal vectors", a: []float32{1, 0}, b: []float32{0, 1}, expected: 0},
		{name: "opposite vectors", a: []float32{1, 0}, b: []float32{-1, 0}, expected: -1},
		{name: "mismatched lengths score zero", a: []float32{1, 0}, b: []float32{1}, expected: 0},
		{name: "zero vector scores zero", a: []float32{0, 0}, b: []float32{1, 0}, expected: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.InDelta(t, tt.expected, cosineSimilarity(tt.a, tt.b), 1e-9)
		})
	}
}

func mustSerialize(t *testing.T, vec []float32) []byte {
	t.Helper()
	b, err := serializeFloat32(vec)
	require.NoError(t, err)
	return b
}
