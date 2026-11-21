//go:build windows

package account

import (
	"os"
	"os/exec"
)

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	p.Release()
	return true
}

// ownGroup: on Windows a cancel kills cmd itself (exec's default).
func ownGroup(*exec.Cmd) {}
