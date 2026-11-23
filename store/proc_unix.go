//go:build !windows

package store

import (
	"errors"
	"os"
	"syscall"
)

// PIDAlive reports whether a process exists. EPERM means it exists but
// belongs to another user.
func PIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// inode is a file's inode, 0 where the system gives none.
func inode(info os.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino)
	}
	return 0
}
