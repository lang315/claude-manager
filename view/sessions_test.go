package view

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/lang315/claude-manager/internal/fixture"
)

func TestTrashSession(t *testing.T) {
	a, tt, d, trashed := newApp(t)
	must(t, tt.Click("Sessions"))
	must(t, tt.Click("me/proj"))
	must(t, tt.Click("Login bug fix"))
	must(t, tt.Click("Move to Trash…"))
	if !tt.HasText("Move 1 sessions to the Trash?") {
		t.Fatalf("no confirmation; texts %q", tt.Texts())
	}
	must(t, tt.Click("Move to Trash"))
	tt.Frame()
	want := []string{
		filepath.Join(d.Root, fixture.Proj, fixture.Old+".jsonl"),
		filepath.Join(d.Root, fixture.Proj, fixture.Old),
		filepath.Join(d.Root, "file-history", fixture.Old),
		filepath.Join(d.Root, "session-env", fixture.Old),
	}
	if !slices.Equal(*trashed, want) {
		t.Errorf("trashed %q, want %q", *trashed, want)
	}
	if tt.HasText("Login bug fix") {
		t.Error("the trashed session still shows after the rescan")
	}
	if a.busy != 0 {
		t.Errorf("busy %d after the move", a.busy)
	}
}

func TestTrashSkipsLiveSession(t *testing.T) {
	a, tt, d, trashed := newApp(t)
	must(t, tt.Click("Sessions"))
	must(t, tt.Click("me/proj"))
	must(t, tt.Click("Live work (live)"))
	tt.Click("Move to Trash…") // disabled: a click does nothing
	if tt.HasText("to the Trash?") || len(*trashed) > 0 {
		t.Fatalf("a live session alone can be trashed: %q", *trashed)
	}

	a.sessions.table.Checked.Add(fixture.Old)
	a.sessions.table.Checked.Add(fixture.Live)
	tt.Frame()
	must(t, tt.Click("Move to Trash…"))
	if !tt.HasText("1 live sessions are left alone") {
		t.Errorf("the confirmation does not say a live session is skipped; texts %q", tt.Texts())
	}
	must(t, tt.Click("Move to Trash"))
	tt.Frame()
	for _, p := range *trashed {
		if filepath.Base(p) == fixture.Live || filepath.Base(p) == fixture.Live+".jsonl" {
			t.Errorf("trashed live session data %s", p)
		}
	}
	if len(*trashed) != 4 || (*trashed)[0] != filepath.Join(d.Root, fixture.Proj, fixture.Old+".jsonl") {
		t.Errorf("trashed %q, want only the old session's 4 paths", *trashed)
	}
}

func TestSessionSearch(t *testing.T) {
	a, tt, _, _ := newApp(t)
	must(t, tt.Click("Sessions"))
	must(t, tt.Click("me/proj"))
	a.sessions.table.SetSearch("Session", "login")
	tt.Frame()
	if a.sessions.table.Len() != 1 || !tt.HasText("Login bug fix") || tt.HasText("Late title") {
		t.Errorf("search for login shows %d rows; texts %q", a.sessions.table.Len(), tt.Texts())
	}
	a.sessions.table.SetSearch("Session", "paste")
	tt.Frame()
	if a.sessions.table.Len() != 1 || !tt.HasText("Late title") {
		t.Errorf("search by first prompt shows %d rows", a.sessions.table.Len())
	}
}

func TestShortPath(t *testing.T) {
	for in, want := range map[string]string{
		"/Users/lang/lang315/mygo": "lang315/mygo",
		"/Users/me/proj":           "me/proj",
		"/proj":                    "/proj",
		"proj":                     "proj",
		"":                         "",
	} {
		if got := shortPath(in); got != want {
			t.Errorf("shortPath(%q) = %q, want %q", in, got, want)
		}
	}
}
