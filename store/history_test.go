package store

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lang315/claude-manager/internal/fixture"
)

func TestHistory(t *testing.T) {
	s, _, _ := newStore(t)
	ps, bad, err := s.History()
	if err != nil {
		t.Fatal(err)
	}
	if bad != 1 {
		t.Errorf("bad = %d, want 1", bad)
	}
	var lines []int
	for _, p := range ps {
		lines = append(lines, p.Line)
	}
	if want := []int{2, 4, 5, 1, 6}; !slices.Equal(lines, want) {
		t.Fatalf("lines %v, want %v: newest first, no time last", lines, want)
	}
	p := ps[0]
	if p.Text != "Fix the login bug" || p.Project != "/Users/me/proj" || p.SessionID != fixture.Old || p.Time.UnixMilli() != 1793008800000 {
		t.Errorf("first prompt %+v", p)
	}
	review := ps[1]
	if want := []Paste{{1, "func main() {\n}"}, {3, "unused paste"}}; !slices.Equal(review.Pastes, want) || review.Lost != 0 {
		t.Errorf("pastes %+v lost %d, want %+v and 0", review.Pastes, review.Lost, want)
	}
	if ps[2].Lost != 1 || len(ps[2].Pastes) != 0 {
		t.Errorf("hash-only paste: pastes %+v lost %d, want none and 1", ps[2].Pastes, ps[2].Lost)
	}
	if !ps[4].Time.IsZero() {
		t.Errorf("no timestamp gives %v, want the zero time", ps[4].Time)
	}
}

func TestHistoryMissing(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	ps, bad, err := s.History()
	if err != nil || bad != 0 || len(ps) != 0 {
		t.Errorf("missing file: %d prompts, bad %d, err %v; want none", len(ps), bad, err)
	}
}

// A paste can be megabytes on one line, and Claude Code may be writing the
// last line: neither stops the read.
func TestHistoryLongLastLine(t *testing.T) {
	root := t.TempDir()
	big := strings.Repeat("x", 2<<20)
	line := `{"display":"[Pasted text #1 +1 lines]","pastedContents":{"1":{"id":1,"type":"text","content":"` + big + `"}},"timestamp":1,"project":"/p","sessionId":"s"}`
	if err := os.WriteFile(filepath.Join(root, "history.jsonl"), []byte(line), 0o644); err != nil { // no trailing newline
		t.Fatal(err)
	}
	ps, bad, err := (&Store{Root: root}).History()
	if err != nil || bad != 0 || len(ps) != 1 || len(ps[0].Pastes) != 1 || len(ps[0].Pastes[0].Content) != len(big) {
		t.Fatalf("read %d prompts, bad %d, err %v", len(ps), bad, err)
	}
}

func TestExpand(t *testing.T) {
	p := Prompt{
		Text:   "a [Pasted text #1 +2 lines] b [Pasted text #2] c [Pasted text #3 +9 lines] [Image #1]",
		Pastes: []Paste{{1, "ONE"}, {2, "TWO"}, {4, "unused"}},
	}
	want := "a ONE b TWO c [Pasted text #3 +9 lines] [Image #1]"
	if got := p.Expand(); got != want {
		t.Errorf("Expand = %q, want %q", got, want)
	}
}
