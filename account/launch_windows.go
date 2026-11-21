package account

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// LaunchInTerminal opens a console window at folder running claude with
// configDir as its CLAUDE_CONFIG_DIR. The folder and configDir reach the
// window as its working folder and environment, never through cmd's
// command line, where & or % in a path would run or expand.
func LaunchInTerminal(folder, configDir string, args ...string) error {
	line := `/c start "" cmd /k claude`
	for _, a := range args {
		if strings.ContainsAny(a, "\"&|<>^%!()\r\n") {
			return errors.New("An argument for claude holds a character cmd would act on: run it from a console instead.")
		}
		line += ` "` + a + `"`
	}
	cmd := exec.Command("cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: "cmd " + line}
	cmd.Dir = folder
	cmd.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+configDir)
	return cmd.Start()
}
