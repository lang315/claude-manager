//go:build !darwin && !linux && !windows

package account

import "errors"

// LaunchInTerminal has no terminal to open here.
func LaunchInTerminal(folder, configDir string, args ...string) error {
	return errors.New("Open in Terminal isn't there on this system: run CLAUDE_CONFIG_DIR=" + configDir + " claude in " + folder + ".")
}
