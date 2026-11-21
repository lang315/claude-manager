package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	if code, out, _ := a.run(t, "", "status"); code != ok || strings.TrimSpace(out) != "alice@example.com" {
		t.Fatalf("status: %d %q", code, out)
	}
	code, out, errs := a.run(t, "", "switch", "bob@example.com", "--json")
	if code != ok || !strings.Contains(out, `"switched":true`) || !strings.Contains(out, `"to":"bob@example.com"`) {
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
	a.usage["alice"] = 95
	a.b.Now = func() time.Time { return t0.Add(4 * time.Minute) }
	code, out, _ := a.run(t, "", "auto", "--once", "--dry-run", "--json")
	if code != nothing || !strings.Contains(out, `"event":"switch"`) || !strings.Contains(out, `"dryRun":true`) {
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
	if code, _, errs := a.run(t, "", "purge"); code != failed || !strings.Contains(errs, "--yes") {
		t.Errorf("purge without --yes: %d %s", code, errs)
	}
	if code, out, _ := a.run(t, "", "purge", "--yes"); code != ok || !strings.Contains(out, "Removed 2 accounts") {
		t.Errorf("purge: %d %s", code, out)
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
	want := []string{"--json", "--email", "e@x", "--hold=5", "bob", "-", "--", "--x"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("%q", got)
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
	if !strings.Contains(out, `"email":"bob@example.com","organizationUuid":"org","key":"bob/org","active":false,"added":true,"usageStatus":"unavailable"`) {
		t.Errorf("list %s", out)
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
	var folder string
	a.b.Launch = func(f, p string, _ ...string) error { folder = f; return nil }
	a.b.Running = func(string) (bool, error) { return false, nil }
	sub := filepath.Join(work, "src")
	must(t, os.Mkdir(sub, 0o700))
	if code, _, errs := a.run(t, "", "run", sub); code != ok || folder != sub {
		t.Fatalf("run by mapping: %d %s %q", code, errs, folder)
	}
	if code, _, errs := a.run(t, "", "run", t.TempDir()); code != failed || !strings.Contains(errs, "No account is mapped") {
		t.Errorf("unmapped: %d %s", code, errs)
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
