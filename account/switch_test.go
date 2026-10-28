package account

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakePitboard is a pitboard that writes its arguments, one per line, to
// args, then runs body.
func fakePitboard(t *testing.T, body string) (bin, args string) {
	t.Helper()
	dir := t.TempDir()
	bin, args = filepath.Join(dir, "pitboard"), filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + args + "'\n" + body + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, args
}

func switchBook(bin string) *Book {
	return &Book{PitboardCLI: func() (string, error) { return bin, nil }}
}

const switched = `{"v":1,"command":"use","ok":true,"data":{"from":"work","to":"Lãng","provider":"claude","changed":true,"parked_at":1,"adoption_ceiling_seconds":33,"adoption":{}},"warnings":[],"error":null}`

func TestSwitchArgs(t *testing.T) {
	bin, args := fakePitboard(t, "echo '"+switched+"'")
	s, err := switchBook(bin).Switch(ctx, "Lãng", false)
	if err != nil {
		t.Fatal(err)
	}
	if s != (Switched{To: "Lãng", Changed: true, Follow: 33 * time.Second}) {
		t.Errorf("got %+v", s)
	}
	got, _ := os.ReadFile(args)
	if string(got) != "use\nclaude/Lãng\n--json\n" {
		t.Errorf("args %q", got)
	}
	if _, err := switchBook(bin).Switch(ctx, "Lãng", true); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(args)
	if string(got) != "use\nclaude/Lãng\n--json\n--yes\n" {
		t.Errorf("args with replace %q", got)
	}
}

func TestSwitchFollow(t *testing.T) {
	for _, c := range []struct {
		data    string
		changed bool
		follow  time.Duration
	}{
		{`"changed":true,"adoption_ceiling_seconds":null`, true, 0},
		{`"changed":true`, true, 0},
		{`"changed":false`, false, 0},
	} {
		bin, _ := fakePitboard(t, `echo '{"v":1,"command":"use","ok":true,"data":{"to":"w",`+c.data+`},"warnings":[],"error":null}'`)
		s, err := switchBook(bin).Switch(ctx, "w", false)
		if err != nil || s.Changed != c.changed || s.Follow != c.follow {
			t.Errorf("%s: %+v, %v", c.data, s, err)
		}
	}
}

func TestSwitchRefused(t *testing.T) {
	bin, _ := fakePitboard(t, `echo '{"v":1,"command":"use","ok":false,"data":null,"warnings":[],"error":{"code":"parked_login_refused","message":"Its saved sign-in no longer works.","cause":null}}'
echo 'error: secret path' >&2
exit 1`)
	_, err := switchBook(bin).Switch(ctx, "w", false)
	var se *SwitchError
	if !errors.As(err, &se) || se.Code != "parked_login_refused" || err.Error() != "Its saved sign-in no longer works." {
		t.Fatalf("err = %v", err)
	}
	bin, _ = fakePitboard(t, `echo '{"v":1,"command":"use","ok":false,"data":null,"warnings":[],"error":{"code":"live_login_refused","message":"x","cause":null}}'; exit 1`)
	if _, err := switchBook(bin).Switch(ctx, "w", false); !errors.Is(err, ErrLiveRefused) {
		t.Fatalf("live_login_refused: err = %v", err)
	}
}

func TestSwitchNoAnswer(t *testing.T) {
	for _, body := range []string{"exit 0", "echo 'not json'; exit 3", "echo '{\"v\":1,\"ok\":true}'"} {
		bin, _ := fakePitboard(t, body)
		if _, err := switchBook(bin).Switch(ctx, "w", false); err == nil || err.Error() != "Pitboard gave no answer." {
			t.Errorf("%q: err = %v", body, err)
		}
	}
}

func TestSwitchTimesOut(t *testing.T) {
	bin, _ := fakePitboard(t, "sleep 30")
	b := switchBook(bin)
	b.SwitchTimeout = 200 * time.Millisecond
	start := time.Now()
	_, err := b.Switch(ctx, "w", false)
	if err == nil || err.Error() != "Pitboard took too long." {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("took %v: the sleep was not killed", d)
	}
}

func TestCanSwitch(t *testing.T) {
	if (&Book{}).CanSwitch() {
		t.Error("no PitboardCLI, yet it can switch")
	}
	b := &Book{PitboardCLI: func() (string, error) { return "", ErrNoPitboard }}
	if b.CanSwitch() {
		t.Error("pitboard not found, yet it can switch")
	}
	if _, err := b.Switch(ctx, "w", false); !errors.Is(err, ErrNoPitboard) {
		t.Errorf("err = %v", err)
	}
	if !switchBook("/bin/true").CanSwitch() {
		t.Error("pitboard found, yet it can't switch")
	}
	if !strings.Contains(ErrNoPitboard.Error(), "Pitboard") {
		t.Error(ErrNoPitboard)
	}
}

func TestPitboardFoundOnce(t *testing.T) {
	calls := 0
	b := &Book{PitboardCLI: func() (string, error) { calls++; return "/bin/true", nil }}
	b.CanSwitch()
	b.CanSwitch()
	if calls != 1 {
		t.Fatalf("looked for pitboard %d times, want once: a login shell each time", calls)
	}
}

func TestFindToolDoesNotWaitOnAChildsPipe(t *testing.T) {
	start := time.Now()
	// The shell exits at once, but a child it leaves keeps stdout open.
	findTool("nonexistent-tool; sleep 30 &", "not found")
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("took %v: a leftover child held the lookup", d)
	}
}
