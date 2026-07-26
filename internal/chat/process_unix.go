//go:build unix

package chat

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the child in its own process group so we can signal the
// whole group (the server plus any subprocesses it spawns) and avoid orphans.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// terminate asks the backend's process group to shut down gracefully.
func terminate(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}

// forceKill hard-kills the backend's process group.
func forceKill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
