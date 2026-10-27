// Package store reads a Claude Code config directory (~/.claude) and moves
// what the user picks to the Trash. It never writes anything else.
package store

import (
	"errors"
	"syscall"
	"time"
)

// Store reads one Claude Code config directory.
type Store struct {
	Root       string                  // ~/.claude, or $CLAUDE_CONFIG_DIR
	ClaudeJSON string                  // ~/.claude.json, read for its MCP servers
	Trash      func(path string) error // moves a path to the Trash: mygo.Shell.TrashItem in the app
	Now        func() time.Time
	Alive      func(pid int) bool // PIDAlive in the app
}

// recent is how long after its last write a file counts as in use.
const recent = 10 * time.Minute

// PIDAlive reports whether a process exists. EPERM means it exists but
// belongs to another user.
func PIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
