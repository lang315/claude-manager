package store

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestSameProcess(t *testing.T) {
	me := os.Getpid()
	if !SameProcess(me, "") || !SameProcess(me, "garbage") {
		t.Error("an unstamped or unreadable record must pass")
	}
	out, err := ps(me, "lstart=")
	if err != nil {
		t.Skip("no ps")
	}
	if !SameProcess(me, out) {
		t.Errorf("this process's own stamp %q failed", out)
	}
	// A record from long before this process started, at its pid: the pid
	// was reused, and this test binary is no claude.
	old := time.Now().Add(-24 * time.Hour).UTC().Format("Mon Jan _2 15:04:05 2006")
	if SameProcess(me, old) {
		t.Error("a reused pid passed")
	}
	// A claude at the pid is kept, reused or not.
	sleep := exec.Command("/bin/sh", "-c", "exec -a claude sleep 5")
	if err := sleep.Start(); err != nil {
		t.Fatal(err)
	}
	defer sleep.Process.Kill()
	time.Sleep(200 * time.Millisecond)
	if name, _ := ps(sleep.Process.Pid, "comm=", "-o", "args="); name != "" && !SameProcess(sleep.Process.Pid, old) {
		t.Errorf("a claude at the pid failed (%q)", name)
	}
}
