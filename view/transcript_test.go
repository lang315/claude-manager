package view

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/store"
)

func TestTranscript(t *testing.T) {
	a, tt, _, _ := newApp(t)
	// count is how many texts contain s: HasText would also match the
	// collapsed label "tool_result · package auth".
	count := func(s string) (n int) {
		for _, x := range tt.Texts() {
			if strings.Contains(x, s) {
				n++
			}
		}
		return n
	}
	must(t, tt.Click("Sessions"))
	must(t, tt.Click("/Users/me/proj"))
	must(t, tt.Click("Login bug fix"))
	must(t, tt.Click("Open Transcript"))
	tt.Frame()
	if n := len(a.sessions.msgs); n != 6 {
		t.Fatalf("%d messages, want 6", n)
	}
	for _, s := range []string{
		"1 unreadable lines",
		`tool_use · Read {"file_path":"auth.go"}`,
		"thinking · Look at auth.go",
		"6 messages",
	} {
		if !tt.HasText(s) {
			t.Errorf("transcript lacks %q; texts %q", s, tt.Texts())
		}
	}
	if n := count("package auth"); n != 1 {
		t.Errorf("%d texts hold the tool result before it is expanded, want 1 (its label)", n)
	}
	must(t, tt.Click("tool_result · package auth"))
	tt.Frame()
	if n := count("package auth"); n != 2 {
		t.Errorf("%d texts hold the tool result once expanded, want 2 (label and body)", n)
	}
	must(t, tt.Click("Back"))
	if a.sessions.open != nil || !tt.HasText("Open Transcript") {
		t.Error("Back does not return to the table")
	}
	if cur := a.sessions.table.Current(); cur == nil || cur.Title != "Login bug fix" {
		t.Error("Back loses the chosen row")
	}
}

func TestNewRowLabel(t *testing.T) {
	long := newRow(store.Message{Kind: "tool_result", Text: strings.Repeat("x", 5<<20)})
	if want := "tool_result · " + strings.Repeat("x", 120); long.label != want {
		t.Errorf("label %q, want %q", long.label, want)
	}
	// "é" is 2 bytes: byte 512 starts a rune without a prefix and is a
	// continuation byte with a one-byte prefix.
	for _, pre := range []string{"", "a"} {
		r := newRow(store.Message{Kind: "thinking", Text: pre + strings.Repeat("é", 600)})
		if !utf8.ValidString(r.label) {
			t.Errorf("prefix %q: label is not valid UTF-8", pre)
		}
	}
}

// openLogin opens the fixture's "Login bug fix" transcript.
func openLogin(t *testing.T, tt *ui.Tester) {
	t.Helper()
	must(t, tt.Click("Sessions"))
	must(t, tt.Click("/Users/me/proj"))
	must(t, tt.Click("Login bug fix"))
	must(t, tt.Click("Open Transcript"))
}

func TestTranscriptBackDropsLateMessages(t *testing.T) {
	a, tt, _, _ := newApp(t)
	var queued []func()
	a.Go = func(f func()) { queued = append(queued, f) }
	openLogin(t, tt)
	must(t, tt.Click("Back"))
	for _, f := range queued {
		f()
	}
	tt.Frame()
	if a.sessions.open != nil || len(a.sessions.msgs) != 0 || a.sessions.loading || a.busy != 0 {
		t.Errorf("open %v, %d messages, loading %v, busy %d after Back and the late read",
			a.sessions.open, len(a.sessions.msgs), a.sessions.loading, a.busy)
	}
}

func TestTranscriptReopenKeepsOnlyTheLastRead(t *testing.T) {
	a, tt, _, _ := newApp(t)
	var queued []func()
	a.Go = func(f func()) { queued = append(queued, f) }
	openLogin(t, tt)
	must(t, tt.Click("Back"))
	must(t, tt.Click("Open Transcript"))
	for _, f := range queued {
		f()
	}
	tt.Frame()
	if n := len(a.sessions.msgs); n != 6 {
		t.Errorf("%d messages after reopening, want 6", n)
	}
	if a.sessions.loading || a.busy != 0 {
		t.Errorf("loading %v, busy %d", a.sessions.loading, a.busy)
	}
}
