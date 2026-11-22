package store

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ps on Windows answers from the process itself: its start in ps's layout,
// UTC, and its image path.
func TestPsWindows(t *testing.T) {
	me := os.Getpid()
	out, err := ps(me, "lstart=")
	if err != nil {
		t.Fatal(err)
	}
	started, err := lstart(out)
	if err != nil {
		t.Fatalf("lstart %q: %v", out, err)
	}
	if now := time.Now(); started.After(now) || started.Before(now.Add(-time.Hour)) {
		t.Errorf("this process started at %v, now %v", started, now)
	}
	if out != strings.TrimSpace(out) || len(out) != len("Mon Jan _2 15:04:05 2006") {
		t.Errorf("lstart %q is not in ps's layout", out)
	}

	name, err := ps(me, "comm=", "-o", "args=")
	if err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	if !strings.EqualFold(filepath.Clean(name), filepath.Clean(self)) {
		t.Errorf("image %q, want %q", name, self)
	}

	if _, err := ps(0x7ffffff0, "lstart="); err == nil {
		t.Error("a pid no process has answered")
	}
}

// A claude at a reused pid is kept: the Windows form of the exec -a check.
func TestSameProcessKeepsAClaudeWindows(t *testing.T) {
	sys := filepath.Join(os.Getenv("SystemRoot"), "System32", "ping.exe")
	claude := filepath.Join(t.TempDir(), "claude.exe")
	in, err := os.Open(sys)
	if err != nil {
		t.Skip(err)
	}
	out, err := os.Create(claude)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(out, in)
	in.Close()
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(claude, "-n", "30", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	pid := cmd.Process.Pid

	stamp, err := ps(pid, "lstart=")
	if err != nil {
		t.Fatal(err)
	}
	if !SameProcess(pid, stamp) {
		t.Errorf("its own stamp %q failed", stamp)
	}
	old := time.Now().Add(-24 * time.Hour).UTC().Format("Mon Jan _2 15:04:05 2006")
	if !SameProcess(pid, old) {
		t.Error("a claude at a reused pid was dropped")
	}

	// The same check for a process that is no claude drops the record.
	other := exec.Command(sys, "-n", "30", "127.0.0.1")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Process.Kill(); other.Wait() })
	if SameProcess(other.Process.Pid, old) {
		t.Error("a reused pid that is no claude passed")
	}
}
