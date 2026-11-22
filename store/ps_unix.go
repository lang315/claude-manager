//go:build !windows

package store

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func ps(pid int, field ...string) (string, error) {
	args := append([]string{"-p", strconv.Itoa(pid), "-o"}, field...)
	cmd := exec.Command("ps", args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "TZ=UTC") // as claude stamps procStart
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}
