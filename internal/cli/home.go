package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// home returns the Evoke home directory, creating it if it does not exist.
// It checks EVOKE_HOME first, then falls back to ~/.evoke.
func home() (string, error) {
	var dir string
	if env := os.Getenv("EVOKE_HOME"); env != "" {
		abs, err := filepath.Abs(env)
		if err != nil {
			return "", fmt.Errorf("failed to resolve EVOKE_HOME: %w", err)
		}
		dir = abs
	} else {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to resolve home directory: %w", err)
		}
		dir = filepath.Join(userHome, ".evoke")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create evoke home: %w", err)
	}
	return dir, nil
}

// library returns the path to the library directory inside the Evoke home.
func library() (string, error) {
	h, err := home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "library"), nil
}

// sessions returns the path to the chat session directory inside the Evoke
// home. Stored conversations live here rather than beside any .evoke file:
// a transcript belongs to the caller's invocation, not to the character, which
// is shareable and may be published to a registry.
func sessions() (string, error) {
	h, err := home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "sessions"), nil
}

// Settings holds user-editable persistent configuration.
type Settings struct {
	Registry string   `json:"registry,omitempty"`
	Paths    []string `json:"paths,omitempty"`
	// OutputPath is the directory evoke view browses — where the backend saves
	// what evoke generated, which for a standard ComfyUI install is
	// <ComfyUI>/output/images. Machine-specific, like the chat model paths.
	OutputPath string        `json:"output_path,omitempty"`
	Chat       *ChatSettings `json:"chat,omitzero"`
}

// ChatSettings holds trusted local configuration for the chat command. It is
// machine-specific and must never be embedded in portable .evoke files: a
// portable declaration names a model file (like an IMAGE checkpoint), and this
// says where such files live and which backend executable to launch.
type ChatSettings struct {
	// Executable is the llama-server binary (name on PATH or absolute path).
	Executable string `json:"executable,omitempty"`
	// Host and Port are the loopback endpoint the backend binds to.
	Host string `json:"host,omitempty"`
	Port int    `json:"port,omitempty"`
	// ModelPaths are directories searched (recursively) for the GGUF file named
	// in a CHAT declaration and knowledge DB files in KNOWLEDGE declarations.
	ModelPaths []string `json:"model_paths,omitempty"`
	// EmbedURL is the ollama-compatible API base URL for query-time embeddings.
	EmbedURL string `json:"embed_url,omitempty"`
	// Color forces ANSI styling of interactive chat output on (true) or off
	// (false). When unset, styling is auto-detected from the terminal (honoring
	// NO_COLOR).
	Color *bool `json:"color,omitempty"`
	// Stream shows the reply token-by-token as it generates (true) or waits and
	// renders the completed reply (false). When unset, streaming is off.
	Stream *bool `json:"stream,omitempty"`
}

// settings reads settings.json from the Evoke home directory.
// Returns a zero-value Settings if the file does not exist.
func settings() (*Settings, error) {
	h, err := home()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(h, "settings.json"))
	if os.IsNotExist(err) {
		return &Settings{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read settings: %w", err)
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("failed to parse settings: %w", err)
	}
	return &s, nil
}

// saveSettings writes settings.json to the Evoke home directory.
func saveSettings(s *Settings) error {
	h, err := home()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode settings: %w", err)
	}
	return os.WriteFile(filepath.Join(h, "settings.json"), append(data, '\n'), 0o644)
}

// paths returns the configured source paths, deduplicated.
// If no paths are configured, it defaults to the library directory.
func (s *Settings) paths() ([]string, error) {
	if len(s.Paths) == 0 {
		lib, err := library()
		if err != nil {
			return nil, err
		}
		return []string{lib}, nil
	}
	var result []string
	for _, p := range s.Paths {
		if !slices.Contains(result, p) {
			result = append(result, p)
		}
	}
	return result, nil
}

// Manifest holds durable CLI-managed information about files pulled from registries.
type Manifest struct {
	Artifacts map[string]Artifact `json:"artifacts"`
}

// Artifact records a single pulled registry artifact.
type Artifact struct {
	File     string `json:"file"`
	Registry string `json:"registry"`
	Revision string `json:"revision"`
	SHA256   string `json:"sha256"`
}

// manifest reads manifest.json from the Evoke home directory.
// Returns an empty Manifest if the file does not exist.
func manifest() (*Manifest, error) {
	h, err := home()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(h, "manifest.json"))
	if os.IsNotExist(err) {
		return &Manifest{Artifacts: make(map[string]Artifact)}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}
	if m.Artifacts == nil {
		m.Artifacts = make(map[string]Artifact)
	}
	return &m, nil
}

// saveManifest writes manifest.json atomically to the Evoke home directory.
func saveManifest(m *Manifest) error {
	h, err := home()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode manifest: %w", err)
	}
	return os.WriteFile(filepath.Join(h, "manifest.json"), append(data, '\n'), 0o644)
}

// Tags holds local tag associations for .evoke files — e.g. marking a
// favorite — keyed by file basename rather than the file's own TAGS block.
// A file's TAGS are a statement about the file, shareable with anyone who
// uses it; these are the caller's own preference and never leave this
// machine or get written into a .evoke file.
type Tags struct {
	Files map[string][]string `json:"files"`
}

// tags reads tags.json from the Evoke home directory.
// Returns an empty Tags if the file does not exist.
func tags() (*Tags, error) {
	h, err := home()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(h, "tags.json"))
	if os.IsNotExist(err) {
		return &Tags{Files: make(map[string][]string)}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read tags: %w", err)
	}
	var t Tags
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("failed to parse tags: %w", err)
	}
	if t.Files == nil {
		t.Files = make(map[string][]string)
	}
	return &t, nil
}

// saveTags writes tags.json to the Evoke home directory.
func saveTags(t *Tags) error {
	h, err := home()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode tags: %w", err)
	}
	return os.WriteFile(filepath.Join(h, "tags.json"), append(data, '\n'), 0o644)
}

// expandPath expands ~ to the user's home directory and returns an absolute path.
func expandPath(p string) (string, error) {
	if strings.HasPrefix(p, "~/") || p == "~" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(userHome, p[1:])
	}
	return filepath.Abs(p)
}
