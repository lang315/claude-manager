package store

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/lang315/claude-manager/internal/fixture"
)

func TestPIDAlive(t *testing.T) {
	if !PIDAlive(os.Getpid()) {
		t.Error("own PID reads as dead")
	}
	// launchd belongs to root: Kill answers EPERM, and the process is alive.
	if runtime.GOOS != "windows" && !PIDAlive(1) {
		t.Error("PID 1 reads as dead")
	}
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if PIDAlive(cmd.Process.Pid) {
		t.Error("exited process reads as alive")
	}
	if PIDAlive(0) || PIDAlive(-1) {
		t.Error("non-positive PID reads as alive")
	}
}

// newStore returns a Store on a fresh fixture, with a fake Trash that
// removes what it gets and records it.
func newStore(t *testing.T) (*Store, fixture.Dirs, *[]string) {
	t.Helper()
	d := fixture.New(t)
	var trashed []string
	s := &Store{
		Root:       d.Root,
		ClaudeJSON: d.ClaudeJSON,
		Now:        func() time.Time { return fixture.Now },
		Alive:      fixture.Alive,
		Trash: func(p string) error {
			trashed = append(trashed, p)
			return os.RemoveAll(p)
		},
	}
	return s, d, &trashed
}
