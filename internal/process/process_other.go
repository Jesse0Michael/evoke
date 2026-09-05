//go:build !unix

package process

import (
	"os"
	"os/exec"
)

// SetGroup is a no-op on platforms without POSIX process groups.
func SetGroup(_ *exec.Cmd) {}

// Terminate asks the process to shut down gracefully.
func Terminate(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Signal(os.Interrupt)
}

// ForceKill hard-kills the process.
func ForceKill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
