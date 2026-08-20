package cli

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// flashGlyph confirms a completed action. A check mark rather than an emoji: it
// is single-width in a monospace cell, so it cannot shift the columns beside it.
const flashGlyph = "✓"

// File actions shell out rather than link a clipboard library, so the same two
// constraints chafa taught us apply to every child here: it must not inherit
// stdin, or it swallows keystrokes typed while it runs, and it must not write to
// stdout, or a stray line lands in the middle of an absolutely-positioned frame.
// Leaving Stdin/Stdout/Stderr nil points all three at the null device, which is
// exactly the wiring we want — so none of these set them.

// copyTextToClipboard puts s on the system clipboard.
func copyTextToClipboard(s string) error {
	cmd := exec.Command("pbcopy")
	// A pipe from us, never the terminal.
	cmd.Stdin = strings.NewReader(s)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run pbcopy: %w", err)
	}
	return nil
}

// copyImageToClipboard puts the image itself on the clipboard as PNG data, so it
// pastes into an editor or a chat window instead of arriving as a file
// reference. PNG only: the cast tags whatever bytes it reads with a pasteboard
// type rather than converting them, so handing it a JPEG would publish data
// mislabelled as PNG.
func copyImageToClipboard(path string) error {
	if !strings.EqualFold(filepath.Ext(path), ".png") {
		return fmt.Errorf("only PNG images can be copied")
	}
	script := fmt.Sprintf("set the clipboard to (read (POSIX file %s) as «class PNGf»)", appleScriptString(path))
	if err := exec.Command("osascript", "-e", script).Run(); err != nil {
		return fmt.Errorf("failed to run osascript: %w", err)
	}
	return nil
}

// revealInFinder selects the file in Finder. `open` without -R would hand a PNG
// to Preview instead, which is what the details pane's file:// link already did.
func revealInFinder(path string) error {
	if err := exec.Command("open", "-R", path).Run(); err != nil {
		return fmt.Errorf("failed to run open: %w", err)
	}
	return nil
}

// appleScriptString renders s as an AppleScript string literal.
func appleScriptString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
