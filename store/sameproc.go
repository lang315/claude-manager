package store

import (
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// pidReuseSlack is how much later than its record a process may have
// started and still be the claude that wrote it (claude-swap's
// PID_REUSE_SLACK_S).
const pidReuseSlack = 2 * time.Minute

// SameProcess reports whether the live process at pid is the claude that
// wrote a session record stamped procStart, claude's reading of its own
// start, as claude-swap's pid_matches_record: a crashed claude leaves its
// record, and the system can give the pid to another process. On Linux the
// stamp is /proc/<pid>/stat's start in clock ticks, equal for life; else
// ps -o lstart, and only a process started after the record that isn't a
// claude fails. What can't be told passes: "can't tell" never makes a
// live session nobody's.
func SameProcess(pid int, procStart string) bool {
	if procStart == "" {
		return true
	}
	if _, err := strconv.ParseUint(procStart, 10, 64); err == nil {
		t := startTicks(pid)
		return t == "" || t == procStart
	}
	recorded, err := lstart(procStart)
	if err != nil {
		return true
	}
	out, err := ps(pid, "lstart=")
	if err != nil {
		return true
	}
	started, err := lstart(out)
	if err != nil || !started.After(recorded.Add(pidReuseSlack)) {
		return true
	}
	name, err := ps(pid, "comm=", "-o", "args=")
	return err != nil || strings.Contains(strings.ToLower(name), "claude")
}

// Stamp is a record's procStart as SameProcess takes it: a string, or a
// number as Linux's clock ticks may be written.
func Stamp(raw json.RawMessage) string {
	return strings.Trim(strings.TrimSpace(string(raw)), `"`)
}

// lstart parses ps's lstart, "Wed Sep  2 20:35:59 2026", in UTC.
func lstart(s string) (time.Time, error) {
	return time.Parse("Mon Jan _2 15:04:05 2006", strings.Join(strings.Fields(s), " "))
}

func ps(pid int, field ...string) (string, error) {
	args := append([]string{"-p", strconv.Itoa(pid), "-o"}, field...)
	cmd := exec.Command("ps", args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "TZ=UTC") // as claude stamps procStart
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// startTicks is field 22 of /proc/<pid>/stat, "" off Linux or unreadable.
func startTicks(pid int) string {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ""
	}
	s := string(data)
	fields := strings.Fields(s[strings.LastIndex(s, ")")+1:])
	if len(fields) <= 19 {
		return ""
	}
	return fields[19]
}
