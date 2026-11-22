package account

import "os/exec"

// LaunchInTerminal opens Terminal at folder running claude with configDir
// as its CLAUDE_CONFIG_DIR. The command goes to AppleScript as an
// argument, so only the shell's quoting applies.
func LaunchInTerminal(folder, configDir string, args ...string) error {
	claude, err := FindClaude()
	if err != nil {
		return err
	}
	return exec.Command("/usr/bin/osascript",
		"-e", "on run argv", "-e", `tell application "Terminal"`, "-e", "activate",
		"-e", "do script (item 1 of argv)", "-e", "end tell", "-e", "end run",
		RunCommand(claude, folder, configDir, args...)).Run()
}
