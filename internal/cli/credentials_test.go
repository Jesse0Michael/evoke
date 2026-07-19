package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCredentialsRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	want := &Credentials{
		Registry:     "http://localhost:8080",
		Username:     "test-user",
		AccessToken:  "test-access",
		RefreshToken: "test-refresh",
	}
	require.NoError(t, saveCredentials(want))

	// Written under ~/.evoke with owner-only permissions.
	info, err := os.Stat(filepath.Join(home, ".evoke", "credentials.json"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	got, err := loadCredentials()
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestLoadCredentialsMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := loadCredentials()
	require.ErrorIs(t, err, os.ErrNotExist)
}
