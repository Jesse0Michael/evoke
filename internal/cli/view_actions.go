package cli

import (
	"bytes"
	"fmt"
	"os"
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

// runEvokeImageCommand runs an image-in subcommand (edit, paint) for source with
// args, returning what it reported. It re-runs this same binary rather than
// calling the command in process:
// the command writes its resolution trace to stdout, which would land in the
// middle of an absolutely-positioned frame, and a child's output can simply be
// captured instead. os.Executable is the binary actually running, so a viewer
// launched from ./bin does not silently drive a different evoke on PATH.
//
// Stdin stays nil for the same reason chafa's does — a child that inherits the
// terminal swallows the keystrokes typed while it runs.
func runEvokeImageCommand(cmd, source string, args []string) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to locate the evoke binary: %w", err)
	}

	child := exec.Command(self, append([]string{cmd, "-i", source}, args...)...)
	var out bytes.Buffer
	child.Stdout, child.Stderr = &out, &out
	if err := child.Run(); err != nil {
		if text := strings.TrimSpace(out.String()); text != "" {
			return "", fmt.Errorf("%s", text)
		}
		return "", fmt.Errorf("evoke %s: %w", cmd, err)
	}
	return out.String(), nil
}

// splitArgs splits a typed line into arguments the way a shell would, so a
// quoted prompt survives as one argument. Without it every word would classify
// as its own selector, and `evoke edit` distinguishes a literal prompt from a
// tag by whether it contains a space.
//
// Quotes group, a backslash escapes the next character, and an unterminated
// quote closes at the end of the line rather than erroring — the line is being
// typed, and rejecting it would only lose what was already entered.
func splitArgs(line string) []string {
	var args []string
	var cur strings.Builder
	var quote byte
	started := false

	flush := func() {
		if started {
			args = append(args, cur.String())
			cur.Reset()
			started = false
		}
	}

	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\\' && i+1 < len(line):
			i++
			cur.WriteByte(line[i])
			started = true
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote = c
			started = true
		case c == ' ' || c == '\t':
			flush()
		default:
			cur.WriteByte(c)
			started = true
		}
	}
	flush()

	return args
}
