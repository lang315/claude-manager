package store

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
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
	projects, err := s.Projects(nil)
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

func TestReadHeadStopsAtTitleLimit(t *testing.T) {
	f := filepath.Join(t.TempDir(), "s.jsonl")
	filler := `{"type":"attachment","x":"` + strings.Repeat("y", 1<<20) + `"}` + "\n"
	body := `{"type":"user","message":{"content":"hi"}}` + "\n" + strings.Repeat(filler, titleLimit>>20+1) +
		`{"type":"ai-title","aiTitle":"Too late"}` + "\n"
	if err := os.WriteFile(f, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var x Session
	if err := readHead(f, &x); err != nil {
		t.Fatal(err)
	}
	if x.Title != "" || x.FirstPrompt != "hi" {
		t.Errorf("title %q prompt %q: want the scan stopped before the late title", x.Title, x.FirstPrompt)
	}
}

func TestRootWithGlobChars(t *testing.T) {
	s, d, _ := newStore(t)
	dir := filepath.Join(filepath.Dir(d.Root), "My [Stuff]")
	if err := os.Rename(d.Root, dir); err != nil {
		t.Fatal(err)
	}
	s.Root = dir
	if got := s.LiveIDs(); len(got) != 1 || !got[fixture.Live] {
		t.Errorf("LiveIDs %v, want only %s", got, fixture.Live)
	}
	if p, err := s.Projects(nil); err != nil || len(p) != 2 {
		t.Errorf("projects %d err %v, want 2", len(p), err)
	}
}

func TestReadHeadFindsPromptPastHead(t *testing.T) {
	filler := strings.Repeat(`{"type":"attachment","x":"`+strings.Repeat("y", 1000)+`"}`+"\n", 100)
	user := `{"type":"user","cwd":"/late","message":{"content":"Real prompt"}}` + "\n"
	title := `{"type":"ai-title","aiTitle":"Named"}` + "\n"
	for name, tail := range map[string]string{"with title": user + title, "no title": user} {
		f := filepath.Join(t.TempDir(), "s.jsonl")
		if err := os.WriteFile(f, []byte(filler+tail), 0o644); err != nil {
			t.Fatal(err)
		}
		var x Session
		if err := readHead(f, &x); err != nil {
			t.Fatal(err)
		}
		want := ""
		if name == "with title" {
			want = "Named"
		}
		if x.FirstPrompt != "Real prompt" || x.Title != want || x.Cwd != "" {
			t.Errorf("%s: prompt %q title %q cwd %q", name, x.FirstPrompt, x.Title, x.Cwd)
		}
	}
}

// A startedAt that is no integer makes the start unknown, not the session dead.
func TestLiveIDsStartedAtNotAnInteger(t *testing.T) {
	for name, startedAt := range map[string]string{"float": `1761480000000.5`, "string": `"1761480000000"`} {
		t.Run(name, func(t *testing.T) {
			s, d, _ := newStore(t)
			os.WriteFile(filepath.Join(d.Root, "sessions", "4242.json"), []byte(`{"pid":4242,"sessionId":"`+fixture.Live+`","startedAt":`+startedAt+`}`), 0o644)
			if !s.LiveIDs()[fixture.Live] {
				t.Errorf("live session dropped when startedAt is %s", startedAt)
			}
			if _, since := s.live(); !since.IsZero() {
				t.Errorf("since %v, want unknown (zero)", since)
			}
		})
	}
}

// Claude Code can encode one cwd into two folders: they are one project,
// keyed by the folder of its newest session.
func TestProjectsMergeSameCwd(t *testing.T) {
	s, d, _ := newStore(t)
	before, _ := s.Projects(nil)
	var count int
	for _, p := range before {
		if p.Path == "/Users/me/proj" {
			count = len(p.Sessions)
		}
	}
	dir, id := movedSession(t, d)
	ps, _ := s.Projects(nil)
	var hits []Project
	for _, p := range ps {
		if p.Path == "/Users/me/proj" {
			hits = append(hits, p)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("%d projects for /Users/me/proj, want 1", len(hits))
	}
	if p := hits[0]; p.Dir != dir || len(p.Sessions) != count+1 || p.Sessions[0].ID != id {
		t.Errorf("merged: dir %q, %d sessions, newest %q; want %q, %d, %q", p.Dir, len(p.Sessions), p.Sessions[0].ID, dir, count+1, id)
	}
}

// A Rescan rereads the head of only the sessions whose .jsonl changed, and
// forgets the ones gone.
func TestProjectsReadsOnlyChangedHeads(t *testing.T) {
	s, d, _ := newStore(t)
	var scan SessionScan
	first, err := s.Projects(&scan)
	n := scan.reads
	if err != nil || n == 0 {
		t.Fatalf("first scan: %d reads, err %v", n, err)
	}
	again, _ := s.Projects(&scan)
	if scan.reads != n || !reflect.DeepEqual(first, again) {
		t.Errorf("an unchanged tree: %d reads (want %d), same projects %v", scan.reads, n, reflect.DeepEqual(first, again))
	}
	recent := filepath.Join(d.Root, fixture.Other, fixture.Recent+".jsonl")
	f, err := os.OpenFile(recent, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"type":"user","message":{"role":"user","content":"more"}}` + "\n")
	f.Close()
	s.Projects(&scan)
	if scan.reads != n+1 {
		t.Errorf("one grown session: %d reads, want %d", scan.reads, n+1)
	}
	late := filepath.Join(d.Root, fixture.Proj, fixture.Late+".jsonl")
	if err := os.Remove(late); err != nil {
		t.Fatal(err)
	}
	s.Projects(&scan)
	if _, ok := scan.heads[late]; ok {
		t.Error("a deleted session stays in the cache")
	}
}

// The Sessions and Cleanup loads can scan at once.
func TestProjectsScanFromTwoLoads(t *testing.T) {
	s, _, _ := newStore(t)
	var scan SessionScan
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { s.Projects(&scan) })
	}
	wg.Wait()
}

// movedSession writes the newest session of all, on the fixture's clock, in
// a second folder for /Users/me/proj, and returns that folder and its id.
func movedSession(t *testing.T, d fixture.Dirs) (dir, id string) {
	t.Helper()
	dir = filepath.Join(d.Root, "projects", "-Users-me-proj-v2")
	id = "99999999-9999-4999-8999-999999999999"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","cwd":"/Users/me/proj","timestamp":"2026-10-26T11:00:00.000Z","message":{"role":"user","content":"Moved folder"}}` + "\n"
	file := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(file, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, fixture.Now, fixture.Now); err != nil {
		t.Fatal(err)
	}
	return dir, id
}

// A head that couldn't be read is read again on the next scan, once the
// file can be: the error isn't kept.
func TestProjectsRetriesFailedHead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any file")
	}
	s, d, _ := newStore(t)
	recent := filepath.Join(d.Root, fixture.Other, fixture.Recent+".jsonl")
	if err := os.Chmod(recent, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(recent, 0o644) })
	var scan SessionScan
	ps, _ := s.Projects(&scan)
	if x := session(t, ps, fixture.Recent); x.Err == "" {
		t.Fatal("an unreadable session has no error")
	}
	if err := os.Chmod(recent, 0o644); err != nil {
		t.Fatal(err)
	}
	ps, _ = s.Projects(&scan)
	if x := session(t, ps, fixture.Recent); x.Err != "" || x.FirstPrompt != "Recent thing" {
		t.Errorf("after the file can be read: err %q, first prompt %q", x.Err, x.FirstPrompt)
	}
}

// A session run from a Claude Manager profile writes its transcript to the
// shared projects/, so it is live too.
func TestLiveIDsInProfiles(t *testing.T) {
	s, _, _ := newStore(t)
	s.Profiles = t.TempDir()
	dir := filepath.Join(s.Profiles, "abc", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "4242.json"), []byte(`{"pid":4242,"sessionId":"other"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := s.LiveIDs(); !got["other"] || !got[fixture.Live] {
		t.Errorf("LiveIDs %v", got)
	}
}

func TestInstances(t *testing.T) {
	s, _, _ := newStore(t)
	s.Profiles = t.TempDir()
	dir := filepath.Join(s.Profiles, "abc", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "4242.json"), []byte(`{"pid":4242,"sessionId":"x","cwd":"/w","entrypoint":"claude-vscode"}`), 0o600)
	os.MkdirAll(filepath.Join(s.Root, "ide"), 0o700)
	os.WriteFile(filepath.Join(s.Root, "ide", "1.lock"), []byte(`{"pid":4242,"ideName":"Visual Studio Code","workspaceFolders":["/a","/b"]}`), 0o600)
	os.WriteFile(filepath.Join(s.Root, "ide", "2.lock"), []byte(`{"pid":9,"ideName":"Gone"}`), 0o600)
	got := s.Instances()
	var kinds []string
	for _, i := range got {
		kinds = append(kinds, i.Kind+"@"+i.Folder)
	}
	want := map[string]bool{"VS Code@/w": false, "VS Code@/a, /b": false}
	for _, k := range kinds {
		if _, ok := want[k]; ok {
			want[k] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("%s missing in %q", k, kinds)
		}
	}
	for _, i := range got {
		if i.Kind == "Gone" {
			t.Error("a dead IDE listed")
		}
	}
}
