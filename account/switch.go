package account

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"syscall"
	"time"
)

var (
	ErrNoPitboard  = errors.New("Pitboard not found.")
	ErrLiveRefused = errors.New("Claude Code's sign-in no longer works.")
	errNoAnswer    = errors.New("Pitboard gave no answer.")
	errSwitchSlow  = errors.New("Pitboard took too long.")
)

// SwitchError is Pitboard's refusal: its code, and its words for a person.
type SwitchError struct{ Code, Message string }

func (e *SwitchError) Error() string { return e.Message }

// Switched is what a switch did. Follow is how long sessions already
// running take to follow; 0 means only once they're started again.
type Switched struct {
	To      string
	Changed bool
	Follow  time.Duration
}

// FindPitboard finds Pitboard's command line, which its app bundle holds.
func FindPitboard() (string, error) {
	p, err := findTool("pitboard", ErrNoPitboard.Error(), "/opt/homebrew/bin/pitboard", "/usr/local/bin/pitboard",
		"/Applications/Pitboard.app/Contents/Helpers/pitboard")
	if err != nil {
		return "", ErrNoPitboard
	}
	return p, nil
}

// CanSwitch says whether Pitboard's command is here. It may run a login
// shell: call it off the main thread.
func (b *Book) CanSwitch() bool {
	_, err := b.pitboard()
	return err == nil
}

func (b *Book) pitboard() (string, error) {
	if b.PitboardCLI == nil {
		return "", ErrNoPitboard
	}
	return b.PitboardCLI()
}

// Switch puts Claude Code on the account Pitboard parks under label, by
// running pitboard use. replace answers yes to Pitboard's one question:
// whether to replace a sign-in Anthropic refuses for good. Pitboard does
// every write; the app reads its JSON answer, and never its stderr.
func (b *Book) Switch(ctx context.Context, label string, replace bool) (Switched, error) {
	bin, err := b.pitboard()
	if err != nil {
		return Switched{}, err
	}
	limit := b.SwitchTimeout
	if limit == 0 {
		limit = time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	args := []string{"use", "claude/" + label, "--json"}
	if replace {
		args = append(args, "--yes")
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	out, _ := cmd.Output() // the exit status says less than the envelope
	if ctx.Err() == context.DeadlineExceeded {
		return Switched{}, errSwitchSlow
	}
	var env struct {
		OK   *bool `json:"ok"`
		Data *struct {
			To      string   `json:"to"`
			Changed bool     `json:"changed"`
			Follow  *float64 `json:"adoption_ceiling_seconds"`
		} `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(bytes.TrimSpace(out), &env) != nil || env.OK == nil {
		return Switched{}, errNoAnswer
	}
	switch {
	case !*env.OK && env.Error != nil && env.Error.Code == "live_login_refused":
		return Switched{}, ErrLiveRefused
	case !*env.OK && env.Error != nil:
		return Switched{}, &SwitchError{Code: env.Error.Code, Message: env.Error.Message}
	case !*env.OK || env.Data == nil:
		return Switched{}, errNoAnswer
	}
	s := Switched{To: env.Data.To, Changed: env.Data.Changed}
	if f := env.Data.Follow; f != nil && *f > 0 {
		s.Follow = time.Duration(*f * float64(time.Second))
	}
	return s, nil
}
