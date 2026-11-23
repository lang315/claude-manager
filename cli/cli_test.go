package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lang315/claude-manager/account"
)

var t0 = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

// app is a book on a fake Anthropic: each access token "<name>-at" is
// <name>@example.com at usage[name] percent of its session limit. Claude
// Code is signed in to alice; bob is added.
type app struct {
	b     *account.Book
	v     *account.Memory
	h     *account.Home
	mu    sync.Mutex
	usage map[string]int
}

func login(name string) []byte {
	return []byte(fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"%s-at","refreshToken":"%s-rt","expiresAt":%d}}`, name, name, t0.Add(5*time.Hour).UnixMilli()))
}

func newApp(t *testing.T) *app {
	t.Helper()
	fakeService(t) // never the real LaunchAgents or launchctl
	a := &app{usage: map[string]int{"alice": 50, "bob": 10}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), "-at")
		a.mu.Lock()
		pct, ok := a.usage[name]
		a.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/oauth/profile":
			fmt.Fprintf(w, `{"account":{"uuid":%q,"email":"%s@example.com"},"organization":{"uuid":"org","name":"Acme"}}`, name, name)
		case "/api/oauth/usage":
			fmt.Fprintf(w, `{"limits":[{"kind":"session","percent":%d,"resets_at":"2026-10-26T15:00:00Z"}]}`, pct)
		}
	}))
	t.Cleanup(srv.Close)
	api := account.NewAPI("test")
	api.Base, api.AuthBase = srv.URL, srv.URL
	home := t.TempDir()
	a.h = &account.Home{Root: filepath.Join(home, ".claude"), ClaudeJSON: filepath.Join(home, ".claude.json"), User: "me"}
	must(t, os.Mkdir(a.h.Root, 0o700))
	must(t, os.WriteFile(a.h.ClaudeJSON, []byte(`{"oauthAccount":{"accountUuid":"alice","organizationUuid":"org","emailAddress":"alice@example.com"}}`), 0o600))
	a.v = account.NewMemory()
	must(t, a.v.Put("Claude Code-credentials", "me", login("alice")))
	must(t, a.v.Put(account.ParkService, "bob/org", login("bob")))
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "accounts.json"), []byte(`{"accounts":[{"accountUuid":"bob","orgUuid":"org","email":"bob@example.com","oauthAccount":{"accountUuid":"bob","organizationUuid":"org"}}]}`), 0o600))
	a.b = &account.Book{Dir: dir, Vault: a.v, API: api, Home: a.h,
		Now:  func() time.Time { return t0 },
		Live: func() (account.Login, error) { return account.ReadLive(a.v, "", a.h.Root) }}
	return a
}

func (a *app) run(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errs bytes.Buffer
	code := Run(context.Background(), args, a.b, strings.NewReader(stdin), &out, &errs)
	return code, out.String(), errs.String()
}

// fakeService points the menu bar service paths at temp dirs and records
// launchctl calls, so a purge never reaches the real LaunchAgents.
func fakeService(t *testing.T) *[]string {
	t.Helper()
	dir := t.TempDir()
	var calls []string
	agentsDir = func() string { return filepath.Join(dir, "LaunchAgents") }
	system = func(name string, args ...string) error {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil
	}
	goos = "darwin"
	t.Cleanup(func() {
		agentsDir = func() string { h, _ := os.UserHomeDir(); return filepath.Join(h, "Library", "LaunchAgents") }
		system = func(name string, args ...string) error { return exec.Command(name, args...).Run() }
		goos = runtime.GOOS
	})
	return &calls
}

// --yes skips the question; the LaunchAgent plist and the menu bar log go too.
func TestPurgeYesRemovesAgentAndLogs(t *testing.T) {
	a := newApp(t)
	calls := fakeService(t)
	must(t, os.MkdirAll(agentsDir(), 0o755))
	must(t, os.WriteFile(plistPath(), []byte("x"), 0o644))
	must(t, os.WriteFile(filepath.Join(a.b.Dir, "menubar.log"), []byte("x"), 0o644))
	must(t, os.WriteFile(filepath.Join(a.b.Dir, "menubar.err"), []byte("x"), 0o644))
	if code, out, errs := a.run(t, "", "purge", "--yes"); code != ok || strings.Contains(errs, "Are you sure") || !strings.Contains(out, "Removed 1 accounts") {
		t.Fatalf("purge --yes: %d %s %s", code, out, errs)
	}
	if _, err := os.Stat(plistPath()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("plist left: %v", err)
	}
	if _, err := os.Stat(a.b.Dir); !errors.Is(err, fs.ErrNotExist) || len(*calls) != 1 {
		t.Errorf("dir %v calls %v", err, *calls)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestListJSONIsCswapsAndImports(t *testing.T) {
	a := newApp(t)
	code, out, errs := a.run(t, "", "list", "--json")
	if code != ok {
		t.Fatalf("exit %d: %s", code, errs)
	}
	var doc struct {
		Schema   int `json:"schemaVersion"`
		Active   int `json:"activeAccountNumber"`
		Accounts []struct {
			Email  string  `json:"email"`
			Org    string  `json:"organizationUuid"`
			Status string  `json:"usageStatus"`
			Age    float64 `json:"usageAgeSeconds"`
			Usage  struct {
				FiveHour struct{ Pct float64 } `json:"fiveHour"`
			} `json:"usage"`
		} `json:"accounts"`
	}
	must(t, json.Unmarshal([]byte(out), &doc))
	// By slot, as cswap's: bob was added first; alice, followed, second.
	if doc.Schema != 1 || doc.Active != 2 || len(doc.Accounts) != 2 || doc.Accounts[1].Email != "alice@example.com" ||
		doc.Accounts[1].Status != "ok" || doc.Accounts[1].Usage.FiveHour.Pct != 50 || doc.Accounts[0].Usage.FiveHour.Pct != 10 {
		t.Fatalf("list %s", out)
	}
	// Another Mac takes it as readings, as cswap import-usage does.
	other := newApp(t)
	path := filepath.Join(t.TempDir(), "list.json")
	must(t, os.WriteFile(path, []byte(out), 0o600))
	other.b.Now = func() time.Time { return t0.Add(time.Minute) }
	other.usage["bob"] = 77
	other.b.Refresh(context.Background(), true)
	other.b.Now = func() time.Time { return t0.Add(2 * time.Minute) }
	code, out, _ = other.run(t, "", "import", path, "--hold", "600")
	if code != ok || !strings.Contains(out, "Readings: 2 taken, 0 kept") {
		t.Errorf("import: %d %s", code, out)
	}
	accts, _ := other.b.Blocks()
	for _, x := range accts {
		if x.Email == "bob@example.com" && (x.Limits[0].Percent != 10 || x.HeldUntil.IsZero()) {
			t.Errorf("bob = %+v", x)
		}
	}
}

func TestStatusAndSwitch(t *testing.T) {
	a := newApp(t)
	// cswap's status: the account, its usage, how many are managed.
	if code, out, _ := a.run(t, "", "status"); code != ok || !strings.HasPrefix(out, "Status: ") || !strings.Contains(out, "alice@example.com") ||
		!strings.Contains(out, "50%") || !strings.Contains(out, "Total managed accounts: 2") {
		t.Fatalf("status: %d %q", code, out)
	}
	// cswap: managed sits inside active; totalManagedAccounts only for a managed one.
	var st struct {
		Active map[string]any `json:"active"`
		Total  *int           `json:"totalManagedAccounts"`
		Top    *bool          `json:"managed"`
	}
	_, out, _ := a.run(t, "", "status", "--json")
	must(t, json.Unmarshal([]byte(out), &st))
	if st.Active["managed"] != true || st.Total == nil || *st.Total != 2 || st.Top != nil || st.Active["email"] != "alice@example.com" {
		t.Errorf("status --json: %s", out)
	}
	code, out, errs := a.run(t, "", "switch", "bob@example.com", "--json")
	// cswap's switch --json: from and to as {number, email}, reason switched.
	if code != ok || !strings.Contains(out, `"switched":true`) || !strings.Contains(out, `"to":{"email":"bob@example.com","number":1}`) ||
		!strings.Contains(out, `"from":{"email":"alice@example.com"`) || !strings.Contains(out, `"reason":"switched"`) {
		t.Fatalf("switch: %d %s %s", code, out, errs)
	}
	live, _ := a.v.Get("Claude Code-credentials", "me")
	if !strings.Contains(string(live), "bob-at") {
		t.Errorf("live %s", live)
	}
	if code, _, errs := a.run(t, "", "switch", "nobody@example.com"); code != failed || !strings.Contains(errs, "no account") {
		t.Errorf("unknown: %d %s", code, errs)
	}
	if code, out, _ := a.run(t, "", "switch", "nobody@example.com", "--json"); code != failed || !strings.Contains(out, `"error"`) {
		t.Errorf("unknown --json: %d %s", code, out)
	}
}

// cswap auto --once's exit codes: 0 switched, 2 nothing to do, 3 blocked.
func TestAutoOnce(t *testing.T) {
	a := newApp(t)
	if code, out, _ := a.run(t, "", "auto", "--once"); code != nothing || !strings.Contains(out, "under its lines") {
		t.Errorf("under: %d %s", code, out)
	}
	if _, out, _ := a.run(t, "", "auto", "--once", "--json"); !strings.Contains(out, `"event":"no-switch"`) || !strings.Contains(out, `"reason":"below-threshold"`) {
		t.Errorf("under --json: %s", out)
	}
	a.usage["alice"] = 95
	a.b.Now = func() time.Time { return t0.Add(4 * time.Minute) }
	code, out, _ := a.run(t, "", "auto", "--once", "--dry-run", "--json")
	// cswap's auto --json: a poll line, then the switch with its trigger and refs.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// cswap exits 0 (SWITCHED) on a dry run that would switch.
	if code != ok || len(lines) != 2 || !strings.Contains(lines[0], `"event":"poll"`) || !strings.Contains(lines[0], `"headroomPct":{`) ||
		!strings.Contains(lines[1], `"event":"switch"`) || !strings.Contains(lines[1], `"dryRun":true`) || !strings.Contains(lines[1], `"trigger":"proactive"`) ||
		!strings.Contains(lines[1], `"to":{"email":"bob@example.com","number":1}`) || !strings.Contains(lines[0], `"active":{"email":"alice@example.com"`) {
		t.Errorf("dry run: %d %s", code, out)
	}
	if live, _ := a.v.Get("Claude Code-credentials", "me"); !strings.Contains(string(live), "alice-at") {
		t.Error("a dry run switched")
	}
	if code, out, _ := a.run(t, "", "auto", "--once"); code != ok || !strings.Contains(out, "Switched to bob@example.com") {
		t.Errorf("switch: %d %s", code, out)
	}
	b := newApp(t)
	b.usage["alice"], b.usage["bob"] = 100, 100
	if code, out, _ := b.run(t, "", "auto", "--once"); code != blocked {
		t.Errorf("blocked: %d %s", code, out)
	}
}

func TestAddRemovePurge(t *testing.T) {
	a := newApp(t)
	if code, out, errs := a.run(t, "sk-ant-api03-fromstdinfromstdin\n", "add-token", "-", "--email", "ci@example.com"); code != ok || !strings.Contains(out, "Added ci@example.com") {
		t.Fatalf("add-token: %d %s %s", code, out, errs)
	}
	if code, out, _ := a.run(t, "", "add-token", "sk-ant-oat01-tok"); code != ok || !strings.Contains(out, "setup-token-1@token.local") {
		t.Errorf("setup-token: %d %s", code, out)
	}
	if code, out, _ := a.run(t, "y\n", "remove", "ci@example.com"); code != ok || !strings.Contains(out, "Removed ci@example.com") {
		t.Errorf("remove: %d %s", code, out)
	}
	calls := fakeService(t)
	// cswap asks "Are you sure you want to purge all data? [y/N]": no answer is no.
	for _, in := range []string{"", "n\n", "\n"} {
		if code, out, errs := a.run(t, in, "purge"); code != ok || !strings.Contains(errs, "Are you sure you want to purge all data? [y/N]") || strings.Contains(out, "Removed") {
			t.Errorf("purge answered %q: %d %s %s", in, code, out, errs)
		}
	}
	if _, err := os.Stat(filepath.Join(a.b.Dir, "accounts.json")); err != nil || len(*calls) != 0 {
		t.Errorf("a declined purge removed data or touched the service: %v %v", err, *calls)
	}
	if code, out, _ := a.run(t, "y\n", "purge"); code != ok || !strings.Contains(out, "Removed 2 accounts") {
		t.Errorf("purge: %d %s", code, out)
	}
	// The whole data dir goes, and on darwin the menu bar LaunchAgent (cswap's purge).
	if _, err := os.Stat(a.b.Dir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("data dir left: %v", err)
	}
	if len(*calls) != 1 || !strings.HasPrefix((*calls)[0], "launchctl bootout") {
		t.Errorf("service calls: %v", *calls)
	}
	if a.v.Len() != 1 {
		t.Errorf("%d keychain items left, want Claude Code's own", a.v.Len())
	}
}

func TestExportImportRun(t *testing.T) {
	a := newApp(t)
	path := filepath.Join(t.TempDir(), "out.json")
	if code, out, errs := a.run(t, "", "export", path); code != ok || !strings.Contains(out, "Exported 1 accounts") {
		t.Fatalf("export: %d %s %s", code, out, errs)
	}
	b := newApp(t)
	must(t, b.b.Remove("bob/org"))
	// import-usage takes readings only, never logins (cswap's
	// "unsupported usage document").
	if code, out, _ := b.run(t, "", "import-usage", path); code != failed || strings.Contains(out, "Added") {
		t.Errorf("import-usage of an export: %d %s", code, out)
	}
	if code, out, _ := b.run(t, "", "import", path); code != ok || !strings.Contains(out, "Added 1") {
		t.Errorf("import: %d %s", code, out)
	}
	var folder, profile string
	a.b.Launch = func(f, p string, _ ...string) error { folder, profile = f, p; return nil }
	a.b.Running = func(string) (bool, error) { return false, nil }
	dir := t.TempDir()
	if code, _, errs := a.run(t, "", "run", "bob@example.com", dir, "--no-share-history"); code != ok {
		t.Fatalf("run: %d %s", code, errs)
	}
	if folder != dir || profile == "" {
		t.Errorf("launched %q %q", folder, profile)
	}
	if _, err := os.Lstat(filepath.Join(profile, "projects")); err == nil {
		t.Error("history shared with --no-share-history")
	}
}

func TestReorder(t *testing.T) {
	got := reorder([]string{"bob", "--json", "--email", "e@x", "--hold=5", "-", "--", "--x"})
	want := []string{"--json", "--email", "e@x", "--hold=5", "--", "bob", "-", "--", "--x"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("%q", got)
	}
	// With no account named, the -- for claude comes first: flag.Parse
	// would take it for its own end of flags.
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.Bool("no-share", false, "")
	must(t, fs.Parse(reorder([]string{"--no-share", "--", "-p", "x"})))
	if got := strings.Join(fs.Args(), " "); got != "-- -p x" {
		t.Errorf("args %q", got)
	}
}

// cswap's move takes NUM|EMAIL|ALIAS for the account and a number for the
// target; swap takes either for both.
func TestMoveSwapByEmailOrAlias(t *testing.T) {
	a := newApp(t)
	slot := func(email string) int {
		accts, _ := a.b.Blocks()
		for _, x := range accts {
			if x.Email == email {
				return x.Slot
			}
		}
		return -1
	}
	a.run(t, "", "list") // takes alice, the login in use, into the book
	must(t, a.b.SetAlias("bob/org", "dev"))
	if code, _, errs := a.run(t, "", "swap", "alice@example.com", "dev"); code != ok || slot("alice@example.com") != 1 || slot("bob@example.com") != 2 {
		t.Fatalf("swap: %d %s alice %d bob %d", code, errs, slot("alice@example.com"), slot("bob@example.com"))
	}
	if code, _, errs := a.run(t, "", "move", "dev", "5"); code != ok || slot("bob@example.com") != 5 {
		t.Fatalf("move alias: %d %s bob %d", code, errs, slot("bob@example.com"))
	}
	if code, _, errs := a.run(t, "", "move", "alice@example.com", "3"); code != ok || slot("alice@example.com") != 3 {
		t.Fatalf("move email: %d %s", code, errs)
	}
	if code, _, errs := a.run(t, "", "move", "3", "4"); code != ok || slot("alice@example.com") != 4 {
		t.Fatalf("move num: %d %s", code, errs)
	}
	for _, args := range [][]string{{"move", "dev", "alice@example.com"}, {"move", "nobody", "2"}, {"swap", "dev", "nobody"}} {
		if code, _, _ := a.run(t, "", args...); code != failed {
			t.Errorf("%v succeeded", args)
		}
	}
}

// cswap's switch --json carries strategy, message and warnings; switching to
// the account in use, not forced, is a no-op that Switch never sees.
// The book adds the login in use at each read, so an unmanaged active is
// reached through statusActive: cswap gives it {email, managed:false} alone.
func TestStatusActiveUnmanaged(t *testing.T) {
	got, _ := json.Marshal(statusActive(account.Account{Email: "c@x"}, time.Now()))
	if string(got) != `{"email":"c@x","managed":false}` {
		t.Errorf("%s", got)
	}
}

func TestSwitchJSONFields(t *testing.T) {
	a := newApp(t)
	a.run(t, "", "list") // alice, in use, is Account-2
	live, _ := a.v.Get("Claude Code-credentials", "me")
	_, out, _ := a.run(t, "", "switch", "alice@example.com", "--json")
	var doc map[string]any
	must(t, json.Unmarshal([]byte(out), &doc))
	if doc["switched"] != false || doc["strategy"] != "direct" || doc["reason"] != "already-active" ||
		doc["message"] != "Already on Account-2 (alice@example.com)" || fmt.Sprint(doc["warnings"]) != "[]" ||
		fmt.Sprint(doc["from"]) != fmt.Sprint(doc["to"]) {
		t.Errorf("no-op: %s", out)
	}
	if now, _ := a.v.Get("Claude Code-credentials", "me"); !bytes.Equal(now, live) {
		t.Error("a no-op switch rewrote the live login")
	}
	// a forced self-switch rewrote the live login: cswap says "activated"
	_, out, _ = a.run(t, "", "switch", "alice@example.com", "--force", "--json")
	doc = nil
	must(t, json.Unmarshal([]byte(out), &doc))
	if doc["switched"] != false || doc["reason"] != "activated" || doc["message"] != "Activated Account-2 (alice@example.com) from stored backup" {
		t.Errorf("forced self-switch: %s", out)
	}
	_, out, _ = a.run(t, "", "switch", "bob@example.com", "--json")
	doc = nil
	must(t, json.Unmarshal([]byte(out), &doc))
	if doc["switched"] != true || doc["strategy"] != "direct" || doc["reason"] != "switched" ||
		doc["message"] != "Switched to Account-1 (bob@example.com)" || fmt.Sprint(doc["warnings"]) != "[]" {
		t.Errorf("switched: %s", out)
	}
	// Bare switch names its strategy: cswap's "rotation" unless best or next-available.
	for strat, want := range map[string]string{"": "rotation", "next-available": "next-available"} {
		args := []string{"switch", "--json"}
		if strat != "" {
			args = append(args, "--strategy", strat)
		}
		_, out, _ = a.run(t, "", args...)
		if !strings.Contains(out, `"strategy":"`+want+`"`) || !strings.Contains(out, `"warnings":[]`) {
			t.Errorf("%q: %s", strat, out)
		}
	}
}

// cswap prints the dropped-alias warning to stderr and imports the rest.
func TestImportWarnsOfDroppedAlias(t *testing.T) {
	a := newApp(t)
	must(t, a.b.SetAlias("bob/org", "dev"))
	path := filepath.Join(t.TempDir(), "e.json")
	must(t, os.WriteFile(path, []byte(`{"version":1,"encrypted":false,"accounts":[{"number":7,"email":"k@example.com","kind":"api_key","credentials":"sk-ant-api03-zzzz","alias":"Dev"}]}`), 0o600))
	code, out, errs := a.run(t, "", "import", path)
	if code != ok || !strings.Contains(out, "Added 1") || !strings.Contains(errs, "Warning: alias 'dev' for k@example.com already used by an existing account, dropping the imported alias") {
		t.Errorf("import: %d %q %q", code, out, errs)
	}
}

func TestFindAmbiguous(t *testing.T) {
	accts := []account.Account{{AccountUUID: "x", OrgUUID: "o1", Email: "e@x"}, {AccountUUID: "x", OrgUUID: "o2", Email: "e@x"}}
	if _, err := find(accts, "e@x"); err == nil || !strings.Contains(err.Error(), "x/o1, x/o2") {
		t.Errorf("err = %v", err)
	}
	if a, err := find(accts, "x/o2"); err != nil || a.OrgUUID != "o2" {
		t.Errorf("by key: %+v %v", a, err)
	}
}

// An account with no reading to go by is "unavailable", with its error.
func TestListUnavailable(t *testing.T) {
	a := newApp(t)
	delete(a.usage, "bob")
	_, out, _ := a.run(t, "", "list", "--json")
	for _, want := range []string{`"email":"bob@example.com"`, `"organizationUuid":"org","isOrganization":true,"key":"bob/org","active":false,"added":true,"usageStatus":"unavailable"`} {
		if !strings.Contains(out, want) {
			t.Errorf("no %s in list %s", want, out)
		}
	}
	// cswap adds the warning arrays only when there is a warning.
	for _, no := range []string{"duplicateAccountWarnings", "lockstepUsageWarnings"} {
		if strings.Contains(out, no) {
			t.Errorf("clean list has %s: %s", no, out)
		}
	}
}

func TestAliasMapRun(t *testing.T) {
	a := newApp(t)
	if code, out, _ := a.run(t, "", "alias", "bob@example.com", "Dev"); code != ok || !strings.Contains(out, "dev is bob@example.com") {
		t.Fatalf("alias: %d %s", code, out)
	}
	if code, out, _ := a.run(t, "", "alias"); code != ok || out != "dev\tbob@example.com\n" {
		t.Errorf("alias list: %q", out)
	}
	work := t.TempDir()
	if code, out, errs := a.run(t, "", "map", "dev", work); code != ok || !strings.Contains(out, "now opens as bob@example.com") {
		t.Fatalf("map: %d %s %s", code, out, errs)
	}
	var folder, profile string
	a.b.Launch = func(f, p string, _ ...string) error { folder, profile = f, p; return nil }
	a.b.Running = func(string) (bool, error) { return false, nil }
	sub := filepath.Join(work, "src")
	must(t, os.Mkdir(sub, 0o700))
	if code, _, errs := a.run(t, "", "run", sub); code != ok || folder != sub {
		t.Fatalf("run by mapping: %d %s %q", code, errs, folder)
	}
	// cswap run's fast path: a folder mapped to nobody, or the account in
	// use, opens plain claude as the account in use; --require-session
	// refuses.
	if code, out, errs := a.run(t, "", "run", t.TempDir()); code != ok || profile != "" || !strings.Contains(out, "No account is mapped") {
		t.Errorf("unmapped: %d %s %s %q", code, out, errs, profile)
	}
	if code, _, errs := a.run(t, "", "run", t.TempDir(), "--require-session"); code != failed || !strings.Contains(errs, "No account is mapped") {
		t.Errorf("unmapped --require-session: %d %s", code, errs)
	}
	a.run(t, "", "list") // the account in use is added on a read
	if code, out, errs := a.run(t, "", "run", "alice@example.com", t.TempDir()); code != ok || profile != "" || !strings.Contains(out, "is the account in use") {
		t.Errorf("the account in use: %d %s %s %q", code, out, errs, profile)
	}
	if code, _, _ := a.run(t, "", "run", "alice@example.com", t.TempDir(), "--require-session"); code != failed {
		t.Error("the account in use with --require-session")
	}
	// Signed out of Claude Code, cswap launches from the stored login in a
	// profile, --require-session or not, and says so.
	must(t, a.v.Put("Claude Code-credentials", "me", []byte(`{"claudeAiOauth":{"accessToken":"","refreshToken":""}}`)))
	profile = ""
	if code, out, errs := a.run(t, "", "run", "alice@example.com", t.TempDir(), "--require-session"); code != ok || profile == "" || !strings.Contains(out, "alice@example.com is the default login, but that login holds no usable token; launching from the stored login in a session profile.") {
		t.Errorf("the account in use, signed out: %d %s %s %q", code, out, errs, profile)
	}
	must(t, a.v.Put("Claude Code-credentials", "me", login("alice")))
	// The fast path says which history it uses when the profile has its own
	// and sharing is off.
	must(t, os.Mkdir(filepath.Join(profile, "projects"), 0o700))
	if code, out, errs := a.run(t, "", "run", "alice@example.com", t.TempDir(), "--no-share-history"); code != ok || !strings.Contains(out, "   This launch uses the default login's chat history (~/.claude), not the account's own.") {
		t.Errorf("history notice: %d %s %s", code, out, errs)
	}
	// An account's name wins over a folder of that name here.
	here := t.TempDir()
	t.Chdir(here)
	must(t, os.Mkdir("dev", 0o700))
	if code, _, errs := a.run(t, "", "run", "dev"); code != ok || folder != here {
		t.Errorf("run dev beside ./dev: %d %s %q", code, errs, folder)
	}
	if code, out, _ := a.run(t, "", "unmap", work); code != ok || !strings.Contains(out, "Unmapped") {
		t.Errorf("unmap: %d %s", code, out)
	}
	if code, _, _ := a.run(t, "", "unmap", work); code != failed {
		t.Error("unmapped twice")
	}
	if code, out, _ := a.run(t, "", "alias", "dev", "--unset"); code != ok || !strings.Contains(out, "no alias") {
		t.Errorf("unset: %d %s", code, out)
	}
	Version = "9.9"
	if _, out, _ := a.run(t, "", "version"); out != "claude-manager 9.9\n" {
		t.Errorf("version %q", out)
	}
	for _, h := range []string{"help", "-h", "--help"} {
		if code, out, _ := a.run(t, "", h); code != ok || out != usage+"\n" {
			t.Errorf("%s: %d %q", h, code, out)
		}
	}
	if code, _, errs := a.run(t, "", "swtich"); code != failed || errs != usage+"\n" {
		t.Errorf("a typo: %d %q", code, errs)
	}
}

func TestConfigDebugUpgrade(t *testing.T) {
	a := newApp(t)
	if code, _, errs := a.run(t, "", "config", "set", "ui.theme", "dark"); code != ok {
		t.Fatalf("set: %s", errs)
	}
	if _, out, _ := a.run(t, "", "config", "get", "ui.theme"); out != "dark\n" {
		t.Errorf("get %q", out)
	}
	if _, out, _ := a.run(t, "", "config"); !strings.Contains(out, "ui.theme") || !strings.Contains(out, "autoswitch.threshold") || !strings.Contains(out, "90 (default)") {
		t.Errorf("list %s", out)
	}
	if code, _, _ := a.run(t, "", "config", "set", "ui.theme", "pink"); code != failed {
		t.Error("pink taken")
	}
	a.run(t, "", "config", "unset", "ui.theme")
	if _, out, _ := a.run(t, "", "config", "get", "ui.theme"); out != "auto\n" {
		t.Errorf("unset %q", out)
	}
	_, _, errs := a.run(t, "", "list", "--debug")
	if !strings.Contains(errs, "GET /api/oauth/usage: 200 OK") || strings.Contains(errs, "-at") {
		t.Errorf("debug %q", errs)
	}
	if code, _, errs := a.run(t, "", "upgrade"); code != failed || !strings.Contains(errs, "doesn't update itself") {
		t.Errorf("upgrade: %d %s", code, errs)
	}
	// run.shareHistory false: run shares no history unless --share-history.
	a.run(t, "", "config", "set", "run.shareHistory", "false")
	var profile string
	a.b.Launch = func(f, p string, _ ...string) error { profile = p; return nil }
	a.b.Running = func(string) (bool, error) { return false, nil }
	must(t, os.Mkdir(filepath.Join(a.h.Root, "projects"), 0o700))
	if code, _, errs := a.run(t, "", "run", "bob@example.com", t.TempDir()); code != ok {
		t.Fatal(errs)
	}
	if _, err := os.Lstat(filepath.Join(profile, "projects")); err == nil {
		t.Error("history shared with run.shareHistory false")
	}
}

func TestSlots(t *testing.T) {
	a := newApp(t)
	a.run(t, "", "list") // alice is added second
	if code, out, _ := a.run(t, "", "switch", "1"); code != ok || !strings.Contains(out, "bob@example.com") {
		t.Fatalf("switch by slot: %d %s", code, out)
	}
	if code, out, _ := a.run(t, "", "move", "1", "5"); code != ok || !strings.Contains(out, "Moved slot 1 to 5") {
		t.Fatalf("move: %d %s", code, out)
	}
	if code, out, _ := a.run(t, "", "swap", "2", "5"); code != ok || !strings.Contains(out, "Swapped") {
		t.Fatalf("swap: %d %s", code, out)
	}
	_, out, _ := a.run(t, "", "list")
	if !strings.Contains(out, " 2 bob@example.com") || !strings.Contains(out, " 5 alice@example.com") {
		t.Errorf("after: %s", out)
	}
	if code, out, _ := a.run(t, "", "move", "2", "5"); code != ok || !strings.Contains(out, "Moved") {
		t.Errorf("move onto a taken slot: %d %s", code, out)
	}
	_, out, _ = a.run(t, "", "list")
	if !strings.Contains(out, " 5 bob@example.com") || !strings.Contains(out, " 2 alice@example.com") {
		t.Errorf("a move onto a taken slot swaps: %s", out)
	}
	for _, bad := range [][]string{{"swap", "2", "9"}, {"move", "9", "1"}, {"move", "2", "0"}, {"move", "x", "1"}} {
		if code, _, _ := a.run(t, "", bad...); code != failed {
			t.Errorf("%v taken", bad)
		}
	}
}

func TestUnclaimed(t *testing.T) {
	a := newApp(t)
	if _, out, _ := a.run(t, "", "unclaimed"); out != "No unclaimed logins.\n" {
		t.Errorf("empty %q", out)
	}
	must(t, a.v.Put("Claude Code-credentials", "me", []byte(`{"claudeAiOauth":{"accessToken":"nobody-at","refreshToken":"nobody-rt","expiresAt":1}}`)))
	if code, _, errs := a.run(t, "", "switch", "bob@example.com"); code != ok {
		t.Fatalf("switch: %s", errs)
	}
	_, out, _ := a.run(t, "", "unclaimed")
	id := strings.Fields(out)[0]
	if !strings.Contains(out, "names no added account") {
		t.Errorf("list %q", out)
	}
	if code, out, _ := a.run(t, "", "unclaimed", "--purge", id); code != ok || !strings.Contains(out, "Deleted "+id) {
		t.Errorf("purge: %d %s", code, out)
	}
}

// A login nobody could be named for is no row of list --json: an
// import-usage of it refuses every row without an email.
func TestListJSONLeavesOutUnnamedLogin(t *testing.T) {
	a := newApp(t)
	must(t, os.WriteFile(a.h.ClaudeJSON, []byte(`{}`), 0o600))
	delete(a.usage, "alice")
	_, out, _ := a.run(t, "", "list", "--json")
	if strings.Contains(out, `"email":""`) || !strings.Contains(out, "bob@example.com") {
		t.Errorf("list --json: %s", out)
	}
}

// cswap's verb aliases, and auto's --cooldown.
func TestAliasesAndCooldown(t *testing.T) {
	a := newApp(t)
	_, list, _ := a.run(t, "", "list")
	if code, out, _ := a.run(t, "", "ls"); code != ok || out != list {
		t.Errorf("ls: %d %q", code, out)
	}
	if code, _, errs := a.run(t, "n\n", "rm", "bob@example.com"); code != failed || !strings.Contains(errs, "nothing removed") {
		t.Errorf("rm: %d %s", code, errs)
	}
	if _, _, errs := a.run(t, "", "update"); !strings.Contains(errs, "doesn't update itself") {
		t.Errorf("update: %s", errs)
	}
	if _, _, errs := a.run(t, "", "auto", "--once", "--dry-run", "--cooldown", "0"); strings.Contains(errs, "not defined") {
		t.Errorf("auto --cooldown: %s", errs)
	}
	if s, _ := a.b.AutoInEffect(); s.Cooldown != 0 {
		t.Errorf("--cooldown 0: cooldown %d", s.Cooldown)
	}
}

// An unread setup-token in use is cswap's unknown usage on the active account.
func TestNoSwitchReasonUnreadToken(t *testing.T) {
	if got := noSwitchReason("A setup-token is in use and its usage isn't read: turn on Read Setup-Token Usage for auto-switch to watch it."); got != "active-usage-unknown" {
		t.Errorf("reason %q", got)
	}
}
