package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Credentials are the tokens the registry issued us, persisted between runs so
// later commands (push/pull) can authenticate without logging in again.
type Credentials struct {
	Registry     string `json:"registry"`
	Username     string `json:"username"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// credentialsPath returns ~/.evoke/credentials.json.
func credentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve home directory: %w", err)
	}
	return filepath.Join(home, ".evoke", "credentials.json"), nil
}

// saveCredentials writes the tokens with owner-only permissions.
func saveCredentials(c *Credentials) error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode credentials: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write credentials: %w", err)
	}
	return nil
}

// loadCredentials reads the stored tokens, returning os.ErrNotExist if absent.
func loadCredentials() (*Credentials, error) {
	path, err := credentialsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("failed to parse credentials: %w", err)
	}
	return &c, nil
}
