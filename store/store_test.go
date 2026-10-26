package store

import (
	"os"
	"os/exec"
	"testing"
)

func TestPIDAlive(t *testing.T) {
	if !PIDAlive(os.Getpid()) {
		t.Error("own PID reads as dead")
	}
	// launchd belongs to root: Kill answers EPERM, and the process is alive.
	if !PIDAlive(1) {
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
