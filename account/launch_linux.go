package account

import (
	"errors"
	"os"
	"os/exec"
)

// terminals are tried in order: the user's $TERMINAL, Debian's
// alternative, then the common ones, each with how it takes a command.
var terminals = []struct {
	bin  string
	args []string
}{
	{"x-terminal-emulator", []string{"-e"}}, {"gnome-terminal", []string{"--"}}, {"konsole", []string{"-e"}},
	{"xfce4-terminal", []string{"-x"}}, {"kitty", nil}, {"alacritty", []string{"-e"}}, {"xterm", []string{"-e"}},
}

// LaunchInTerminal opens a terminal window at folder running claude with
// configDir as its CLAUDE_CONFIG_DIR. Under WSL it opens Windows Terminal
// (wt.exe) on this distribution.
func LaunchInTerminal(folder, configDir string) error {
	cmd := RunCommand(folder, configDir)
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		if wt, err := exec.LookPath("wt.exe"); err == nil {
			return exec.Command(wt, "wsl.exe", "-d", os.Getenv("WSL_DISTRO_NAME"), "--", "sh", "-c", cmd).Start()
		}
	}
	list := terminals
	if t := os.Getenv("TERMINAL"); t != "" {
		list = append([]struct {
			bin  string
			args []string
		}{{t, []string{"-e"}}}, list...)
	}
	for _, t := range list {
		if bin, err := exec.LookPath(t.bin); err == nil {
			return exec.Command(bin, append(t.args, "sh", "-c", cmd)...).Start()
		}
	}
	return errors.New("No terminal found to open: set $TERMINAL.")
}
