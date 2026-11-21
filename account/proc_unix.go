//go:build !windows

package account

import (
	"errors"
	"os/exec"
	"syscall"
)

func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return pid > 0 && (err == nil || errors.Is(err, syscall.EPERM))
}

// ownGroup runs cmd in a process group of its own, so a cancel also stops
// what it started.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
