package audio

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakePlayer writes a script standing in for afplay: it records the path it
// was given and exits, so Play's write-play-delete sequence is exercised
// without a real audio device.
func fakePlayer(t *testing.T, recordInto string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-player.sh")
	script := "#!/bin/sh\necho \"$1\" > " + recordInto + "\n"
	require.NoError(t, os.WriteFile(path, []byte(script), 0o700))
	return path
}

func TestPlay(t *testing.T) {
	tests := []struct {
		name    string
		player  func(t *testing.T, recordInto string) string
		wantErr string
	}{
		{
			name:   "plays and cleans up",
			player: fakePlayer,
		},
		{
			name: "player failure is reported",
			player: func(t *testing.T, _ string) string {
				t.Helper()
				path := filepath.Join(t.TempDir(), "failing-player.sh")
				require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0o700))
				return path
			},
			wantErr: "failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			recordInto := filepath.Join(dir, "played-path.txt")
			player := tt.player(t, recordInto)

			err := Play(t.Context(), player, dir, "wav", []byte("fake-audio"))

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)

			played, readErr := os.ReadFile(recordInto)
			require.NoError(t, readErr)
			playedPath := string(played[:len(played)-1]) // trailing newline from echo

			require.FileExists(t, player)
			_, statErr := os.Stat(playedPath)
			require.True(t, os.IsNotExist(statErr), "temporary audio file must be removed after playback")

			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Len(t, entries, 1, "only the recorded-path file should remain in dir")
		})
	}
}

func TestPlayDefaultPlayer(t *testing.T) {
	// cmpOr falls back to DefaultPlayer ("afplay") when none is configured; this
	// just exercises that the empty string is not passed straight to exec, which
	// would fail differently (empty command) than a missing afplay would.
	err := Play(t.Context(), "", t.TempDir(), "wav", []byte("fake-audio"))
	if err == nil {
		t.Skip("afplay is present on this machine and happily played garbage bytes")
	}
	require.NotContains(t, err.Error(), "exec: no command")
}
