package chat

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// mlxModelsDir returns a temp directory seeded with the given model
// subdirectories, mirroring how MLX weights sit on disk.
func mlxModelsDir(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, n), 0o750))
	}
	return dir
}

func TestResolveMLXModel(t *testing.T) {
	tests := []struct {
		name     string
		ref      string
		dirs     func(t *testing.T) []string
		wantOK   bool
		wantPath func(t *testing.T, dirs []string) string
	}{
		{
			name:     "local directory under a model path",
			ref:      "Qwen3-8B-4bit",
			dirs:     func(t *testing.T) []string { return []string{mlxModelsDir(t, "Qwen3-8B-4bit")} },
			wantOK:   true,
			wantPath: func(_ *testing.T, dirs []string) string { return filepath.Join(dirs[0], "Qwen3-8B-4bit") },
		},
		{
			name:     "repo id passes through for the runtime to fetch",
			ref:      "mlx-community/Qwen3-8B-4bit",
			dirs:     func(t *testing.T) []string { return []string{mlxModelsDir(t)} },
			wantOK:   true,
			wantPath: func(_ *testing.T, _ []string) string { return "mlx-community/Qwen3-8B-4bit" },
		},
		{
			name:     "local directory wins over an identically named repo id",
			ref:      "mlx-community/Qwen3-8B-4bit",
			dirs:     func(t *testing.T) []string { return []string{mlxModelsDir(t, "mlx-community/Qwen3-8B-4bit")} },
			wantOK:   true,
			wantPath: func(_ *testing.T, dirs []string) string { return filepath.Join(dirs[0], "mlx-community/Qwen3-8B-4bit") },
		},
		{
			name:     "bare name that is not present does not become a repo id",
			ref:      "Qwen3-8B-4bit",
			dirs:     func(t *testing.T) []string { return []string{mlxModelsDir(t)} },
			wantOK:   false,
			wantPath: func(_ *testing.T, _ []string) string { return "" },
		},
		{
			name:     "a file is not an MLX model",
			ref:      "roleplay-12b.gguf",
			dirs:     func(t *testing.T) []string { return []string{modelsDir(t, "roleplay-12b.gguf")} },
			wantOK:   false,
			wantPath: func(_ *testing.T, _ []string) string { return "" },
		},
		{
			name:     "relative path marker is never a repo id",
			ref:      "./models/thing",
			dirs:     func(t *testing.T) []string { return []string{mlxModelsDir(t)} },
			wantOK:   false,
			wantPath: func(_ *testing.T, _ []string) string { return "" },
		},
		{
			name:     "empty reference",
			ref:      "",
			dirs:     func(t *testing.T) []string { return []string{mlxModelsDir(t)} },
			wantOK:   false,
			wantPath: func(_ *testing.T, _ []string) string { return "" },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dirs := tt.dirs(t)

			path, ok := resolveMLXModel(tt.ref, dirs)

			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.wantPath(t, dirs), path)
		})
	}
}

func TestPlanCommandArgs(t *testing.T) {
	tests := []struct {
		name string
		plan *Plan
		want []string
	}{
		{
			name: "llama.cpp carries context size and gpu layers",
			plan: &Plan{
				Backend:   backendLlamaCpp,
				ModelPath: "/models/roleplay.gguf",
				Runtime:   RuntimeSpec{Host: "127.0.0.1", Port: 8080, ContextWindow: 8192, GPULayers: 99},
			},
			want: []string{
				"--model", "/models/roleplay.gguf",
				"--host", "127.0.0.1",
				"--port", "8080",
				"--ctx-size", "8192",
				"-ngl", "99",
			},
		},
		{
			name: "mlx has no context or offload flag to carry",
			plan: &Plan{
				Backend:   backendMLX,
				ModelPath: "mlx-community/Qwen3-8B-4bit",
				Runtime:   RuntimeSpec{Host: "127.0.0.1", Port: 8080, ContextWindow: 8192, GPULayers: 99},
			},
			want: []string{
				"--model", "mlx-community/Qwen3-8B-4bit",
				"--host", "127.0.0.1",
				"--port", "8080",
			},
		},
		{
			name: "unknown backend produces no command",
			plan: &Plan{Backend: "nope"},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.plan.commandArgs())
		})
	}
}
