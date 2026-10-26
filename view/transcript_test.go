package view

import (
	"strings"
	"testing"
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
