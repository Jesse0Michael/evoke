package audio

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// DefaultPlayer is the platform's built-in audio player. mlx-audio — and
// therefore a resident voice backend — runs only on Apple Silicon, so afplay
// (ships with every macOS install) needs no extra dependency.
const DefaultPlayer = "afplay"

// Play writes data to a temporary file named by format, plays it to completion,
// and removes the file whether playback succeeds or not. A spoken reply is
// heard, not kept, so nothing of the conversation's audio lands on disk. dir
// empty means the OS default temporary directory.
func Play(ctx context.Context, player, dir, format string, data []byte) error {
	player = cmpOr(player, DefaultPlayer)

	f, err := os.CreateTemp(dir, "evoke-voice-*."+format)
	if err != nil {
		return fmt.Errorf("failed to create temporary audio file: %w", err)
	}
	path := f.Name()
	defer func() { _ = os.Remove(path) }()

	_, writeErr := f.Write(data)
	if err := f.Close(); writeErr != nil || err != nil {
		if writeErr != nil {
			return fmt.Errorf("failed to write temporary audio file: %w", writeErr)
		}
		return fmt.Errorf("failed to write temporary audio file: %w", err)
	}

	cmd := exec.CommandContext(ctx, player, path) //nolint:gosec // player is operator-configured, path is ours
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s failed: %w\n%s", player, err, out)
	}
	return nil
}
