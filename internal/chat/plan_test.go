package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	evoke "github.com/jesse0michael/evoke/pkg/evoke"
	"github.com/stretchr/testify/require"
)

// modelsDir returns a temp directory seeded with the given GGUF file names.
func modelsDir(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		require.NoError(t, os.WriteFile(filepath.Join(dir, n), []byte("fake"), 0o600))
	}
	return dir
}

func chatComposition(settings map[string]string, instructions []string) *evoke.Composition {
	return &evoke.Composition{
		Name: "Yasmin",
		Chat: &evoke.ChatConfig{Settings: settings, Instructions: instructions},
	}
}

func TestCompile(t *testing.T) {
	tests := []struct {
		name      string
		comp      *evoke.Composition
		trusted   func(t *testing.T) TrustedConfig
		wantError bool
		check     func(t *testing.T, p *Plan)
	}{
		{
			name:      "no CHAT declaration",
			comp:      &evoke.Composition{Name: "Yasmin"},
			trusted:   func(t *testing.T) TrustedConfig { return TrustedConfig{ModelDirs: []string{modelsDir(t)}} },
			wantError: true,
		},
		{
			name: "valid plan resolves model file, runtime, and sampling",
			comp: chatComposition(map[string]string{
				"model":             "roleplay-12b.gguf",
				"temperature":       "0.85",
				"context_window":    "4096",
				"gpu_layers":        "35",
				"max_output_tokens": "256",
			}, nil),
			trusted: func(t *testing.T) TrustedConfig {
				return TrustedConfig{Executable: "llama-server", ModelDirs: []string{modelsDir(t, "roleplay-12b.gguf")}, Host: "127.0.0.1", Port: 8080}
			},
			check: func(t *testing.T, p *Plan) {
				require.Equal(t, backendLlamaCpp, p.Backend)
				require.Equal(t, "roleplay-12b.gguf", p.Model)
				require.FileExists(t, p.ModelPath)
				require.Equal(t, "roleplay-12b.gguf", filepath.Base(p.ModelPath))
				require.Equal(t, "llama-server", p.Runtime.Executable)
				require.Equal(t, 4096, p.Runtime.ContextWindow)
				require.Equal(t, 35, p.Runtime.GPULayers)
				require.NotNil(t, p.Sampling.Temperature)
				require.InEpsilon(t, 0.85, *p.Sampling.Temperature, 1e-9)
				require.Equal(t, 256, p.Sampling.MaxOutputTokens)
			},
		},
		{
			name: "model file name resolves without the .gguf extension",
			comp: chatComposition(map[string]string{"model": "roleplay-12b"}, nil),
			trusted: func(t *testing.T) TrustedConfig {
				return TrustedConfig{ModelDirs: []string{modelsDir(t, "roleplay-12b.gguf")}}
			},
			check: func(t *testing.T, p *Plan) {
				require.Equal(t, "roleplay-12b.gguf", filepath.Base(p.ModelPath))
			},
		},
		{
			name: "model file is found by recursive search in a nested layout",
			comp: chatComposition(map[string]string{"model": "violet.gguf"}, nil),
			trusted: func(t *testing.T) TrustedConfig {
				dir := t.TempDir()
				nested := filepath.Join(dir, "llama-cpp", "models", "violet")
				require.NoError(t, os.MkdirAll(nested, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(nested, "violet.gguf"), []byte("fake"), 0o600))
				return TrustedConfig{ModelDirs: []string{dir}}
			},
			check: func(t *testing.T, p *Plan) {
				require.FileExists(t, p.ModelPath)
				require.Equal(t, "violet.gguf", filepath.Base(p.ModelPath))
			},
		},
		{
			name: "launch command is built from resolved path and runtime settings",
			comp: chatComposition(map[string]string{"model": "roleplay-12b.gguf", "context_window": "4096", "gpu_layers": "35"}, nil),
			trusted: func(t *testing.T) TrustedConfig {
				return TrustedConfig{Executable: "llama-server", ModelDirs: []string{modelsDir(t, "roleplay-12b.gguf")}, Host: "127.0.0.1", Port: 8080}
			},
			check: func(t *testing.T, p *Plan) {
				args := p.commandArgs()
				require.Equal(t, "--model", args[0])
				require.Equal(t, p.ModelPath, args[1])
				require.Equal(t, []string{"--host", "127.0.0.1", "--port", "8080", "--ctx-size", "4096", "-ngl", "35"}, args[2:])
			},
		},
		{
			name: "unresolved model is a diagnostic, not a compile error",
			comp: chatComposition(map[string]string{"model": "missing.gguf"}, nil),
			trusted: func(t *testing.T) TrustedConfig {
				return TrustedConfig{ModelDirs: []string{modelsDir(t, "other.gguf")}}
			},
			check: func(t *testing.T, p *Plan) {
				require.Empty(t, p.ModelPath)
				require.Equal(t, "missing.gguf", p.Model)
				require.Len(t, p.Diagnostics, 1)
				require.Contains(t, p.Diagnostics[0], "not found under chat.model_paths")
			},
		},
		{
			name:    "no configured model paths gives a clear diagnostic",
			comp:    chatComposition(map[string]string{"model": "roleplay-12b.gguf"}, nil),
			trusted: func(t *testing.T) TrustedConfig { return TrustedConfig{} },
			check: func(t *testing.T, p *Plan) {
				require.Empty(t, p.ModelPath)
				require.Contains(t, p.Diagnostics[0], "no chat.model_paths configured")
			},
		},
		{
			name:      "missing model reference",
			comp:      chatComposition(map[string]string{"context_window": "4096"}, nil),
			trusted:   func(t *testing.T) TrustedConfig { return TrustedConfig{ModelDirs: []string{modelsDir(t)}} },
			wantError: true,
		},
		{
			name: "mlx backend resolves a local model directory",
			comp: chatComposition(map[string]string{"model": "Qwen3-8B-4bit", "backend": "mlx"}, nil),
			trusted: func(t *testing.T) TrustedConfig {
				return TrustedConfig{ModelDirs: []string{mlxModelsDir(t, "Qwen3-8B-4bit")}}
			},
			check: func(t *testing.T, p *Plan) {
				require.Equal(t, backendMLX, p.Backend)
				require.Equal(t, "mlx_lm.server", p.Runtime.Executable)
				require.True(t, strings.HasSuffix(p.ModelPath, "Qwen3-8B-4bit"), "got %q", p.ModelPath)
			},
		},
		{
			name: "mlx backend keeps a repo id for the runtime to fetch",
			comp: chatComposition(map[string]string{"model": "mlx-community/Qwen3-8B-4bit", "backend": "mlx"}, nil),
			trusted: func(t *testing.T) TrustedConfig {
				return TrustedConfig{ModelDirs: []string{mlxModelsDir(t)}}
			},
			check: func(t *testing.T, p *Plan) {
				require.Equal(t, "mlx-community/Qwen3-8B-4bit", p.ModelPath)
				require.Empty(t, p.Diagnostics)
			},
		},
		{
			name: "mlx reports gpu_layers as having no effect",
			comp: chatComposition(map[string]string{"model": "mlx-community/Qwen3-8B-4bit", "backend": "mlx", "gpu_layers": "99"}, nil),
			trusted: func(t *testing.T) TrustedConfig {
				return TrustedConfig{ModelDirs: []string{mlxModelsDir(t)}}
			},
			check: func(t *testing.T, p *Plan) {
				require.Contains(t, p.Diagnostics, `CHAT setting "gpu_layers" has no effect on the mlx backend`)
			},
		},
		{
			name: "context_window still budgets history on mlx",
			comp: chatComposition(map[string]string{"model": "mlx-community/Qwen3-8B-4bit", "backend": "mlx", "context_window": "4096"}, nil),
			trusted: func(t *testing.T) TrustedConfig {
				return TrustedConfig{ModelDirs: []string{mlxModelsDir(t)}}
			},
			check: func(t *testing.T, p *Plan) {
				require.Equal(t, 4096, p.History.ContextWindow)
				require.NotContains(t, p.commandArgs(), "--ctx-size")
			},
		},
		{
			name: "thinking off compiles to a per-request toggle",
			comp: chatComposition(map[string]string{"model": "mlx-community/Qwen3-8B-4bit", "backend": "mlx", "thinking": "off"}, nil),
			trusted: func(t *testing.T) TrustedConfig {
				return TrustedConfig{ModelDirs: []string{mlxModelsDir(t)}}
			},
			check: func(t *testing.T, p *Plan) {
				require.NotNil(t, p.Sampling.Thinking)
				require.False(t, *p.Sampling.Thinking)
			},
		},
		{
			name: "thinking unset leaves the backend default",
			comp: chatComposition(map[string]string{"model": "mlx-community/Qwen3-8B-4bit", "backend": "mlx"}, nil),
			trusted: func(t *testing.T) TrustedConfig {
				return TrustedConfig{ModelDirs: []string{mlxModelsDir(t)}}
			},
			check: func(t *testing.T, p *Plan) {
				require.Nil(t, p.Sampling.Thinking)
			},
		},
		{
			name: "invalid thinking value",
			comp: chatComposition(map[string]string{"model": "mlx-community/Qwen3-8B-4bit", "backend": "mlx", "thinking": "maybe"}, nil),
			trusted: func(t *testing.T) TrustedConfig {
				return TrustedConfig{ModelDirs: []string{mlxModelsDir(t)}}
			},
			wantError: true,
		},
		{
			name: "unsupported backend driver",
			comp: chatComposition(map[string]string{"model": "roleplay-12b.gguf", "backend": "vllm"}, nil),
			trusted: func(t *testing.T) TrustedConfig {
				return TrustedConfig{ModelDirs: []string{modelsDir(t, "roleplay-12b.gguf")}}
			},
			wantError: true,
		},
		{
			name: "output reserve exceeds context window",
			comp: chatComposition(map[string]string{"model": "roleplay-12b.gguf", "context_window": "512", "max_output_tokens": "512"}, nil),
			trusted: func(t *testing.T) TrustedConfig {
				return TrustedConfig{ModelDirs: []string{modelsDir(t, "roleplay-12b.gguf")}}
			},
			wantError: true,
		},
		{
			name: "non-numeric temperature",
			comp: chatComposition(map[string]string{"model": "roleplay-12b.gguf", "temperature": "hot"}, nil),
			trusted: func(t *testing.T) TrustedConfig {
				return TrustedConfig{ModelDirs: []string{modelsDir(t, "roleplay-12b.gguf")}}
			},
			wantError: true,
		},
		{
			name: "unknown setting surfaces a diagnostic",
			comp: chatComposition(map[string]string{"model": "roleplay-12b.gguf", "wobble": "1"}, nil),
			trusted: func(t *testing.T) TrustedConfig {
				return TrustedConfig{ModelDirs: []string{modelsDir(t, "roleplay-12b.gguf")}}
			},
			check: func(t *testing.T, p *Plan) {
				require.Contains(t, p.Diagnostics, `ignored unknown CHAT setting "wobble"`)
			},
		},
		{
			name: "runtime executable, host, and port fall back to defaults",
			comp: chatComposition(map[string]string{"model": "roleplay-12b.gguf"}, nil),
			trusted: func(t *testing.T) TrustedConfig {
				return TrustedConfig{ModelDirs: []string{modelsDir(t, "roleplay-12b.gguf")}}
			},
			check: func(t *testing.T, p *Plan) {
				require.Equal(t, drivers[backendLlamaCpp].executable, p.Runtime.Executable)
				require.Equal(t, defaultHost, p.Runtime.Host)
				require.Equal(t, defaultPort, p.Runtime.Port)
				require.Equal(t, defaultGPULayers, p.Runtime.GPULayers)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := Compile(tt.comp, tt.trusted(t))

			require.Equal(t, tt.wantError, err != nil, "error = %v", err)
			if tt.check != nil {
				require.NoError(t, err)
				tt.check(t, plan)
			}
		})
	}
}

func TestCompileSystemPrompt(t *testing.T) {
	comp := &evoke.Composition{
		Name:        "Yasmin",
		Character:   []string{"a 25-year-old marine biologist"},
		Personality: evoke.Prompt{Positive: []string{"confident", "playful"}, Negative: []string{"cruel"}},
		Backstory:   []string{"Grew up on the coast.", "Studies coral reefs."},
		Appearance:  evoke.Prompt{Positive: []string{"long dark hair"}},
		Voice:       evoke.Prompt{Positive: []string{"low alto, slight rasp"}},
		Apparel:     evoke.Prompt{Positive: []string{"a wetsuit"}},
		Environment: evoke.Prompt{Positive: []string{"a sunny beach"}},
		Scenario:    "You just surfaced from a dive.",
		// Image-only content that must not leak into the chat prompt.
		Prompt: evoke.Prompt{Positive: []string{"masterpiece, 8k, detailed"}},
		Chat:   &evoke.ChatConfig{Instructions: []string{"Stay in character."}},
	}

	want := strings.Join([]string{
		"You are Yasmin.",
		"a 25-year-old marine biologist",
		"",
		"Personality: confident, playful",
		"",
		"Traits to avoid: cruel",
		"",
		"Backstory:",
		"Grew up on the coast.",
		"Studies coral reefs.",
		"",
		"Stay in character.",
	}, "\n")

	got := compileSystemPrompt(comp)

	require.Equal(t, want, got)
	require.NotContains(t, got, "masterpiece", "image-only PROMPT content must not leak into the chat prompt")
	require.NotContains(t, got, "long dark hair", "appearance is a generate-only concern, not chat")
	// The scene is seeded via the opening turn, not the always-resent system prompt.
	require.NotContains(t, got, "Starting situation", "scenario must not live in the system prompt")
	require.NotContains(t, got, "surfaced from a dive", "scenario belongs in the opening turn")
	require.NotContains(t, got, "wetsuit", "apparel is a generate-only concern, not chat")
	require.NotContains(t, got, "sunny beach", "environment is a generate-only concern, not chat")
	// Deterministic across calls.
	require.Equal(t, got, compileSystemPrompt(comp))
}

func TestCompileOpening(t *testing.T) {
	tests := []struct {
		name string
		comp *evoke.Composition
		want string
	}{
		{
			name: "scenario seeds the opening turn",
			comp: &evoke.Composition{Scenario: "You just surfaced from a dive."},
			want: "Set the scene and begin in character. The situation:\n\nYou just surfaced from a dive.",
		},
		{
			name: "no scenario yields no opening",
			comp: &evoke.Composition{},
			want: "",
		},
		{
			name: "apparel and environment do not seed the opening",
			comp: &evoke.Composition{
				Apparel:     evoke.Prompt{Positive: []string{"a wetsuit"}},
				Environment: evoke.Prompt{Positive: []string{"a sunny beach"}},
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, compileOpening(tt.comp))
		})
	}
}
