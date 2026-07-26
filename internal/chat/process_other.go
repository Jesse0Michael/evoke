//go:build !unix

package chat

import (
	"os"
	"os/exec"
)

// setProcessGroup is a no-op on platforms without POSIX process groups.
func setProcessGroup(_ *exec.Cmd) {}

// terminate asks the backend to shut down gracefully.
func terminate(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Signal(os.Interrupt)
}

// forceKill hard-kills the backend.
func forceKill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
