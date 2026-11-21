//go:build windows

package store

import "os"

// PIDAlive reports whether a process exists: Windows opens a handle only
// to one that does.
func PIDAlive(pid int) bool {
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

// inode is 0: Windows gives no inode, so a grown file is read whole.
func inode(os.FileInfo) uint64 { return 0 }
