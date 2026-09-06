package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResolveKnowledgeOutput pins --output to ordinary CLI behavior: a path
// relative to the working directory. chat.model_paths is a search path, often
// owned by another application, so it is never a write destination.
func TestResolveKnowledgeOutput(t *testing.T) {
	absDir := t.TempDir()
	cwd, err := filepath.Abs(".")
	require.NoError(t, err)
	userHome, err := os.UserHomeDir()
	require.NoError(t, err)

	tests := []struct {
		name     string
		output   string
		input    string
		expected string
	}{
		{
			name:     "default is named after the corpus directory",
			output:   "",
			input:    filepath.Join(absDir, "stardew"),
			expected: filepath.Join(cwd, "stardew.db"),
		},
		{
			name:     "default for the working directory uses its name",
			output:   "",
			input:    cwd,
			expected: filepath.Join(cwd, filepath.Base(cwd)+".db"),
		},
		{
			name:     "a filesystem root falls back to the generic name",
			output:   "",
			input:    string(filepath.Separator),
			expected: filepath.Join(cwd, "knowledge.db"),
		},
		{
			name:     "a bare name is relative to the working directory",
			output:   "lore.db",
			expected: filepath.Join(cwd, "lore.db"),
		},
		{
			name:     "a relative path is honored",
			output:   filepath.Join("build", "lore.db"),
			expected: filepath.Join(cwd, "build", "lore.db"),
		},
		{
			name:     "an absolute path is honored",
			output:   filepath.Join(absDir, "lore.db"),
			expected: filepath.Join(absDir, "lore.db"),
		},
		{
			name:     "a tilde path expands to the user home",
			output:   "~/lore.db",
			expected: filepath.Join(userHome, "lore.db"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveKnowledgeOutput(tt.output, tt.input)

			require.NoError(t, err)
			require.Equal(t, tt.expected, got)
		})
	}
}

func TestUnderAny(t *testing.T) {
	root := t.TempDir()
	sibling := t.TempDir()

	tests := []struct {
		name     string
		path     string
		dirs     []string
		expected bool
	}{
		{
			name:     "directly inside",
			path:     filepath.Join(root, "lore.db"),
			dirs:     []string{root},
			expected: true,
		},
		{
			name:     "nested deeper, since db= resolves recursively",
			path:     filepath.Join(root, "rag", "lore.db"),
			dirs:     []string{root},
			expected: true,
		},
		{
			name:     "outside every directory",
			path:     filepath.Join(sibling, "lore.db"),
			dirs:     []string{root},
			expected: false,
		},
		{
			name:     "matches a later directory in the list",
			path:     filepath.Join(sibling, "lore.db"),
			dirs:     []string{root, sibling},
			expected: true,
		},
		{
			name:     "no directories configured",
			path:     filepath.Join(root, "lore.db"),
			dirs:     nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, underAny(tt.path, tt.dirs))
		})
	}
}

func TestKnowledgeModelPaths(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name     string
		settings *Settings
		expected []string
	}{
		{name: "nil settings", settings: nil, expected: nil},
		{name: "no chat settings", settings: &Settings{}, expected: nil},
		{
			name:     "configured paths are made absolute",
			settings: &Settings{Chat: &ChatSettings{ModelPaths: []string{dir}}},
			expected: []string{dir},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, knowledgeModelPaths(tt.settings))
		})
	}
}
