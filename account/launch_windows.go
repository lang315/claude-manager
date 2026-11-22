package account

import (
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
)

// LaunchInTerminal opens a console window at folder running claude with
// configDir as its CLAUDE_CONFIG_DIR. The folder and configDir reach the
// window as its working folder and environment, never through cmd's
// command line, where & or % in a path would run or expand.
func LaunchInTerminal(folder, configDir string, args ...string) error {
	claude, err := FindClaude() // claude-swap runs the path shutil.which found
	if err != nil {
		return err
	}
	if strings.ContainsAny(claude, "\"%\r\n") {
		return errors.New("The path of claude holds a character cmd would act on: run it from a console instead.")
	}
	// The outer quotes are cmd /k's: it strips the first and last when the line starts with one.
	line := `/c start "" cmd /k ""` + claude + `"`
	for _, a := range args {
		if strings.ContainsAny(a, "\"&|<>^%!()\r\n") {
			return errors.New("An argument for claude holds a character cmd would act on: run it from a console instead.")
		}
		line += ` "` + a + `"`
	}
	line += `"`
	cmd := exec.Command("cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: "cmd " + line}
	cmd.Dir = folder
	if configDir != "" { // else the account in use, its environment as it is
		cmd.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool {
			return slices.Contains(authOverrides, strings.SplitN(kv, "=", 2)[0])
		}), "CLAUDE_CONFIG_DIR="+configDir)
	}
	return cmd.Start()
}
