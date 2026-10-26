package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lang315/claude-manager/internal/fixture"
)

func TestLiveIDs(t *testing.T) {
	s, _, _ := newStore(t)
	got := s.LiveIDs()
	if len(got) != 1 || !got[fixture.Live] {
		t.Errorf("LiveIDs %v, want only %s (9999.json's PID is dead)", got, fixture.Live)
	}
}

func TestProjects(t *testing.T) {
	s, d, _ := newStore(t)
	projects, err := s.Projects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 || projects[0].Path != "/Users/me/proj" || projects[1].Path != "/Users/me/other" {
		t.Fatalf("projects %+v, want /Users/me/proj then /Users/me/other", projects)
	}
	if n := len(projects[0].Sessions); n != 3 {
		t.Fatalf("%d sessions in proj, want 3 (the orphan dir, memory dirs and .wakatime are not sessions)", n)
	}
	by := map[string]Session{}
	for _, p := range projects {
		for _, x := range p.Sessions {
			by[x.ID] = x
		}
	}

	old := by[fixture.Old]
	info, err := os.Stat(filepath.Join(d.Root, fixture.Proj, fixture.Old+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if old.Title != "Login bug fix" || old.FirstPrompt != "Fix the login bug" || old.Cwd != "/Users/me/proj" ||
		old.Branch != "main" || old.Version != "2.1.300" || old.Subagents != 1 || old.Live || old.Err != "" {
		t.Errorf("old session %+v", old)
	}
	if want := time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC); !old.Started.Equal(want) {
		t.Errorf("started %v, want %v", old.Started, want)
	}
	if want := info.Size() + int64(len("{}\n")+len("result")); old.Size != want {
		t.Errorf("size %d, want %d (jsonl + side dir)", old.Size, want)
	}

	late := by[fixture.Late]
	if late.Title != "Late title" {
		t.Errorf("late title %q: the ai-title past 64 KB was missed", late.Title)
	}
	if want := "Paste: " + strings.Repeat("x", 193); late.FirstPrompt != want {
		t.Errorf("late first prompt has %d runes, want 200", len([]rune(late.FirstPrompt)))
	}
	if late.Live {
		t.Error("2-day-old session reads as live")
	}
	if x := by[fixture.Live]; !x.Live || x.FirstPrompt != "Live work" {
		t.Errorf("live session %+v", x)
	}
	if x := by[fixture.Recent]; !x.Live {
		t.Error("session written a minute ago is not live")
	}
}

func TestFirstLine(t *testing.T) {
	for in, want := range map[string]string{
		"  one\ntwo": "one",
		"":           "",
		"ééééé":      "ééé",
	} {
		if got := FirstLine(in, 3); got != want {
			t.Errorf("FirstLine(%q, 3) = %q, want %q", in, got, want)
		}
	}
}
