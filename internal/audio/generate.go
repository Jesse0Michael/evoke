package audio

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Result describes one completed synthesis.
type Result struct {
	// Path is the written audio file.
	Path string
	// Command is the runtime invocation, for verbose output.
	Command string
}

// Generate synthesizes text into dir and returns the written file.
//
// The runtime is a one-shot process: it loads the model, writes the file and
// exits, so a command that synthesizes once pays the model load once and has
// nothing to shut down. A resident server is the right shape only for a caller
// that synthesizes repeatedly.
func Generate(ctx context.Context, p *Plan, text, dir string) (*Result, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("no text to speak")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	prefix := nextPrefix(dir, cmpOr(p.Name, "audio"), p.Format)
	args := p.Args(text, dir, prefix)

	cmd := exec.CommandContext(ctx, p.Executable, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, lookErr := exec.LookPath(p.Executable); lookErr != nil {
			return nil, fmt.Errorf("%s not found on PATH: install it with `uv tool install --force mlx-audio --prerelease=allow --python 3.12`", p.Executable)
		}
		return nil, fmt.Errorf("%s failed: %w\n%s", p.Executable, err, strings.TrimSpace(string(out)))
	}

	path, err := findOutput(dir, prefix, p.Format)
	if err != nil {
		return nil, fmt.Errorf("%w\n%s", err, strings.TrimSpace(string(out)))
	}

	return &Result{Path: path, Command: p.Executable + " " + strings.Join(args, " ")}, nil
}

// nextPrefix returns a file prefix that does not collide with existing output.
// The runtime appends its own _NNN counter, so a prefix is taken when anything
// already starts with it.
func nextPrefix(dir, base, format string) string {
	if !prefixTaken(dir, base, format) {
		return base
	}
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s_%d", base, i)
		if !prefixTaken(dir, candidate, format) {
			return candidate
		}
	}
	return base
}

func prefixTaken(dir, prefix, format string) bool {
	matches, err := filepath.Glob(filepath.Join(dir, prefix+"_*."+format))
	return err == nil && len(matches) > 0
}

// findOutput locates the file the runtime wrote. The counter it appends is its
// own, so the name is discovered rather than assumed.
func findOutput(dir, prefix, format string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, prefix+"_*."+format))
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no %s file was written to %s", format, dir)
	}
	return matches[len(matches)-1], nil
}
