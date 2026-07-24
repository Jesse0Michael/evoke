package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHome(t *testing.T) {
	tests := []struct {
		name     string
		envHome  string
		expected string
	}{
		{
			name:     "uses EVOKE_HOME when set",
			envHome:  "/tmp/test-evoke-home",
			expected: "/tmp/test-evoke-home",
		},
		{
			name:    "falls back to ~/.evoke",
			envHome: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envHome != "" {
				t.Setenv("EVOKE_HOME", tt.envHome)
			} else {
				t.Setenv("EVOKE_HOME", "")
			}

			got, err := home()
			require.NoError(t, err)

			if tt.expected != "" {
				require.Equal(t, tt.expected, got)
			} else {
				userHome, err := os.UserHomeDir()
				require.NoError(t, err)
				require.Equal(t, filepath.Join(userHome, ".evoke"), got)
			}
		})
	}
}

func TestHomeWithEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOKE_HOME", dir)

	got, err := home()
	require.NoError(t, err)
	require.Equal(t, dir, got)
}

func TestSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOKE_HOME", dir)

	want := &Settings{
		Registry: "https://registry.evoke.example",
		Paths:    []string{"/home/user/evoke", "/home/user/projects/shared"},
	}
	require.NoError(t, saveSettings(want))

	got, err := settings()
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestSettingsMissing(t *testing.T) {
	t.Setenv("EVOKE_HOME", t.TempDir())

	s, err := settings()
	require.NoError(t, err)
	require.Equal(t, &Settings{}, s)
}

func TestSettingsPaths(t *testing.T) {
	tests := []struct {
		name      string
		paths     []string
		wantCount int
	}{
		{
			name:      "defaults to library",
			paths:     nil,
			wantCount: 1,
		},
		{
			name:      "deduplicates",
			paths:     []string{"/tmp/a", "/tmp/b", "/tmp/a"},
			wantCount: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("EVOKE_HOME", t.TempDir())
			s := &Settings{Paths: tt.paths}
			got, err := s.paths()
			require.NoError(t, err)
			require.Len(t, got, tt.wantCount)
		})
	}
}

func TestManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOKE_HOME", dir)

	want := &Manifest{
		Artifacts: map[string]Artifact{
			"@jesse/ashley": {
				File:     "library/jesse/ashley.evoke",
				Registry: "https://registry.evoke.example",
				Revision: "7",
				SHA256:   "934f63d5f819",
			},
		},
	}
	require.NoError(t, saveManifest(want))

	got, err := manifest()
	require.NoError(t, err)
	require.Equal(t, want, got)

	// Verify atomic write produced valid JSON.
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	var check Manifest
	require.NoError(t, json.Unmarshal(data, &check))
	require.Equal(t, want.Artifacts, check.Artifacts)
}

func TestManifestMissing(t *testing.T) {
	t.Setenv("EVOKE_HOME", t.TempDir())

	m, err := manifest()
	require.NoError(t, err)
	require.NotNil(t, m.Artifacts)
	require.Empty(t, m.Artifacts)
}
