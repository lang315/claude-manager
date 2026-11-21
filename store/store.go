// Package store reads a Claude Code config directory (~/.claude) and moves
// what the user picks to the Trash. It never writes anything else.
package store

import (
	"time"
)

// Store reads one Claude Code config directory.
type Store struct {
	Root       string                  // ~/.claude, or $CLAUDE_CONFIG_DIR
	ClaudeJSON string                  // ~/.claude.json, read for its MCP servers
	Trash      func(path string) error // moves a path to the Trash: mygo.Shell.TrashItem in the app
	Now        func() time.Time
	Alive      func(pid int) bool // PIDAlive in the app
	// Profiles holds Claude Manager's per-account profiles, each a
	// CLAUDE_CONFIG_DIR sharing projects/ with Root: their sessions are
	// live too. "" for none.
	Profiles string
}

// recent is how long after its last write a file counts as in use.
const recent = 10 * time.Minute
