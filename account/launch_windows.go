package account

import (
	"os/exec"
	"syscall"
)

// LaunchInTerminal opens a console window at folder running claude with
// configDir as its CLAUDE_CONFIG_DIR. Windows paths can't hold a double
// quote, so quoting them is enough.
func LaunchInTerminal(folder, configDir string) error {
	line := `/c start "" cmd /k "cd /d "` + folder + `" && set "CLAUDE_CONFIG_DIR=` + configDir + `" && claude"`
	cmd := exec.Command("cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: "cmd " + line}
	return cmd.Start()
}
