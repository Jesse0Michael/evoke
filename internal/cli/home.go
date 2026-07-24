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

// Settings holds user-editable persistent configuration.
type Settings struct {
	Registry string   `json:"registry,omitempty"`
	Paths    []string `json:"paths,omitempty"`
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
