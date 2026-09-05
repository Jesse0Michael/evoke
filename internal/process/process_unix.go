//go:build unix

package process

import (
	"os/exec"
	"syscall"
)

// SetGroup puts the child in its own process group so we can signal the
// whole group (the server plus any subprocesses it spawns) and avoid orphans.
func SetGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// Terminate asks the process group to shut down gracefully.
func Terminate(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}

// ForceKill hard-kills the process group.
func ForceKill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
