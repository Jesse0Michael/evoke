package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// countingEmbedServer returns a deterministic vector for any input and records
// how many embed calls the build made.
func countingEmbedServer(t *testing.T, dims int, calls *int) *httptest.Server {
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
		*calls++
		// A vector derived from the input length: stable, and distinct enough
		// that retrieval ordering is not degenerate.
		vec := make([]float64, dims)
		for i := range vec {
			vec[i] = float64((len(req.Input) + i) % 7)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float64{vec}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// writeCorpus lays out a markdown tree under a temp dir.
func writeCorpus(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return root
}

func TestCorpusFiles(t *testing.T) {
	corpus := map[string]string{
		"index.md":               "# Index",
		"Design/Flora.md":        "# Flora",
		"Design/Fauna.md":        "# Fauna",
		"Design/notes.txt":       "not markdown",
		".obsidian/workspace.md": "# Hidden dir",
		".hidden.md":             "# Hidden file",
		"node_modules/pkg.md":    "# Vendored",
		"Planes/Deep/Nest.md":    "# Nested",
	}

	tests := []struct {
		name     string
		exclude  []string
		expected []string
	}{
		{
			name:     "hidden entries and node_modules are skipped",
			exclude:  nil,
			expected: []string{"Design/Fauna.md", "Design/Flora.md", "Planes/Deep/Nest.md", "index.md"},
		},
		{
			name:     "exclude by base name",
			exclude:  []string{"index.md"},
			expected: []string{"Design/Fauna.md", "Design/Flora.md", "Planes/Deep/Nest.md"},
		},
		{
			name:     "exclude by relative path glob",
			exclude:  []string{"Design/*"},
			expected: []string{"Planes/Deep/Nest.md", "index.md"},
		},
		{
			name:     "multiple excludes combine",
			exclude:  []string{"index.md", "Flora.md"},
			expected: []string{"Design/Fauna.md", "Planes/Deep/Nest.md"},
		},
	}

	root := writeCorpus(t, corpus)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := corpusFiles(root, tt.exclude)
			require.NoError(t, err)

			rel := make([]string, len(files))
			for i, f := range files {
				r, err := filepath.Rel(root, f)
				require.NoError(t, err)
				rel[i] = filepath.ToSlash(r)
			}
			require.Equal(t, tt.expected, rel)
		})
	}
}

func TestBuild(t *testing.T) {
	root := writeCorpus(t, map[string]string{
		"Flora.md": "# Flora\n\nAshroot grows in ash.\n\n## Blightvine\n\nA creeping vine.\n",
		"Fauna.md": "# Fauna\n\nDire elk roam the ridge.\n",
		"skip.md":  "# Skip\n\nExcluded.\n",
	})
	out := filepath.Join(t.TempDir(), "nested", "knowledge.db")
	calls := 0
	srv := countingEmbedServer(t, 3, &calls)

	result, err := Build(t.Context(), BuildOptions{
		Input:      root,
		Output:     out,
		EmbedModel: "test-embed-model",
		EmbedURL:   srv.URL,
		Exclude:    []string{"skip.md"},
	})

	require.NoError(t, err)
	require.Equal(t, &BuildResult{Output: out, Files: 2, Chunks: 3, Model: "test-embed-model", Dims: 3}, result)
	require.Equal(t, 3, calls)

	// The database the build wrote must be loadable by the reader, with the
	// build's model recorded so the reader adopts it.
	base, err := Open(Config{DBPath: out, EmbedURL: srv.URL})
	require.NoError(t, err)
	require.Equal(t, 3, base.Len())
	require.Equal(t, "test-embed-model", base.Meta().EmbedModel)
	require.Equal(t, 3, base.Meta().Dims)
	require.Equal(t, root, base.Meta().Source)
}

func TestBuild_DryRunWritesNothing(t *testing.T) {
	root := writeCorpus(t, map[string]string{
		"Flora.md": "# Flora\n\nAshroot grows in ash.\n\n## Blightvine\n\nA creeping vine.\n",
	})
	out := filepath.Join(t.TempDir(), "knowledge.db")
	calls := 0
	srv := countingEmbedServer(t, 3, &calls)

	result, err := Build(t.Context(), BuildOptions{
		Input: root, Output: out, EmbedModel: "test-embed-model", EmbedURL: srv.URL, DryRun: true,
	})

	require.NoError(t, err)
	require.Equal(t, 2, result.Chunks)
	require.Equal(t, 0, calls, "dry run must not contact the embedding endpoint")
	require.NoFileExists(t, out)
}

// TestBuild_FailureLeavesExistingDatabase is the reason the build goes through a
// temp file: a rebuild against an unreachable endpoint must not destroy the
// database that is already there.
func TestBuild_FailureLeavesExistingDatabase(t *testing.T) {
	root := writeCorpus(t, map[string]string{"Flora.md": "# Flora\n\nAshroot.\n"})
	dir := t.TempDir()
	out := filepath.Join(dir, "knowledge.db")
	require.NoError(t, os.WriteFile(out, []byte("existing database"), 0o644))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "embedding model unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := Build(t.Context(), BuildOptions{
		Input: root, Output: out, EmbedModel: "test-embed-model", EmbedURL: srv.URL,
	})

	require.Error(t, err)
	content, readErr := os.ReadFile(out)
	require.NoError(t, readErr)
	require.Equal(t, "existing database", string(content))

	// No temp files left behind.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestBuild_Errors(t *testing.T) {
	populated := writeCorpus(t, map[string]string{"Flora.md": "# Flora\n\nAshroot.\n"})

	tests := []struct {
		name string
		opts BuildOptions
	}{
		{
			name: "missing input",
			opts: BuildOptions{Output: "out.db"},
		},
		{
			name: "input with no markdown",
			opts: BuildOptions{Input: writeCorpus(t, map[string]string{"notes.txt": "hi"}), Output: "out.db"},
		},
		{
			name: "everything excluded",
			opts: BuildOptions{Input: populated, Output: "out.db", Exclude: []string{"*.md"}},
		},
		{
			name: "invalid exclude pattern",
			opts: BuildOptions{Input: populated, Output: "out.db", Exclude: []string{"["}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Build(t.Context(), tt.opts)
			require.Error(t, err)
		})
	}
}

func TestBuild_Progress(t *testing.T) {
	root := writeCorpus(t, map[string]string{
		"Flora.md": "# Flora\n\nAshroot grows in ash.\n\n## Blightvine\n\nA creeping vine.\n",
		"Fauna.md": "# Fauna\n\nDire elk roam the ridge.\n",
	})
	calls := 0
	srv := countingEmbedServer(t, 3, &calls)

	var progress []FileProgress
	_, err := Build(t.Context(), BuildOptions{
		Input:      root,
		Output:     filepath.Join(t.TempDir(), "knowledge.db"),
		EmbedModel: "test-embed-model",
		EmbedURL:   srv.URL,
		Progress:   func(p FileProgress) { progress = append(progress, p) },
	})

	require.NoError(t, err)
	require.Equal(t, []FileProgress{
		{File: "Fauna.md", Chunks: 1},
		{File: "Flora.md", Chunks: 2},
	}, progress)
}

func TestBuild_ContextCancelled(t *testing.T) {
	root := writeCorpus(t, map[string]string{"Flora.md": "# Flora\n\nAshroot.\n"})
	calls := 0
	srv := countingEmbedServer(t, 3, &calls)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := Build(ctx, BuildOptions{
		Input: root, Output: filepath.Join(t.TempDir(), "knowledge.db"),
		EmbedModel: "test-embed-model", EmbedURL: srv.URL,
	})

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 0, calls)
}
