package view

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/account"
	"github.com/lang315/claude-manager/internal/fixture"
)

// lim is the app with a Limits page on a fake Anthropic. Every token is an
// account of its own name, <token>@example.com, at 62% of its session limit
// and all of its weekly one. "expired" is refused, and no renewal works.
type lim struct {
	a              *App
	tt             *ui.Tester
	book           *account.Book
	vault          *account.Memory
	now            *time.Time     // the book's clock
	live           *account.Login // the login in use: alice's
	usage          *atomic.Int32  // usage requests answered
	weekly         *sync.Map      // a token's weekly percent, where not 100
	weeklyReset    *atomic.Value  // string: when weekly windows reset
	extra          *atomic.Value  // string: the answer's extra_usage
	opened, copied *[]string
}

func limitsApp(t *testing.T) *lim {
	t.Helper()
	a, tt, _, _ := newApp(t)
	var usage atomic.Int32
	var weekly sync.Map
	var weeklyReset atomic.Value
	weeklyReset.Store("2026-11-03T02:00:00Z")
	var extra atomic.Value
	extra.Store("null")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		switch r.URL.Path {
		case "/api/oauth/profile":
			fmt.Fprintf(w, `{"account":{"uuid":%q,"email":"%s@example.com"},"organization":{"uuid":"org","name":"Acme"}}`, tok, tok)
		case "/api/oauth/usage":
			if tok == "expired" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			usage.Add(1)
			pct := 100
			if p, ok := weekly.Load(tok); ok {
				pct = p.(int)
			}
			fmt.Fprintf(w, `{"limits":[{"kind":"session","percent":62,"resets_at":"2026-10-26T15:00:00Z"},{"kind":"weekly_all","percent":%d,"resets_at":%q}],"extra_usage":%s}`, pct, weeklyReset.Load(), extra.Load())
		case "/v1/messages": // the header probe: a setup-token at 37% and 12%
			w.Header().Set("anthropic-ratelimit-unified-5h-utilization", "0.37")
			w.Header().Set("anthropic-ratelimit-unified-7d-utilization", "0.12")
		case "/v1/oauth/token":
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":"invalid_grant"}`)
		}
	}))
	t.Cleanup(srv.Close)
	api := account.NewAPI("test")
	api.Base, api.AuthBase = srv.URL, srv.URL
	now := fixture.Now
	live := account.Login{AccessToken: "alice", ExpiresAt: fixture.Now.Add(time.Hour).UnixMilli(), SubscriptionType: "max"}
	l := &lim{a: a, tt: tt, vault: account.NewMemory(), now: &now, live: &live, usage: &usage, weekly: &weekly, weeklyReset: &weeklyReset, extra: &extra, opened: &[]string{}, copied: &[]string{}}
	l.book = &account.Book{
		Dir: t.TempDir(), Vault: l.vault, API: api,
		Now:  func() time.Time { return now },
		Live: func() (account.Login, error) { return live, nil },
	}
	a.Accounts = l.book
	a.OpenURL = func(u string) error { *l.opened = append(*l.opened, u); return nil }
	a.CopyText = func(s string) { *l.copied = append(*l.copied, s) }
	return l
}

func waitFor(t *testing.T, tt *ui.Tester, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		tt.Frame()
		if ok() {
			return
		}
	}
	t.Fatalf("timed out; texts: %q", tt.Texts())
}

// open shows the Limits page and waits for its figures.
func (l *lim) open(t *testing.T) {
	t.Helper()
	must(t, l.tt.Click("Limits"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("62%") })
}

func TestLimitsShowsTheAccountInUse(t *testing.T) {
	l := limitsApp(t)
	l.open(t)
	for _, s := range []string{"alice@example.com", "Max", "In use", "Session (5h)", "62%", "resets 3h 0m · 15:00",
		"Weekly", "100%", "resets 7d 14h · Nov 3, 02:00", "Limit reached", "Updated just now", "Add Account…", "Refresh"} {
		if !l.tt.HasText(s) {
			t.Errorf("no %q in %q", s, l.tt.Texts())
		}
	}
	if l.tt.HasText("Rescan") {
		t.Error("the Limits page offers Rescan")
	}
	var meter bool
	for _, n := range l.tt.Nodes() {
		if n.Label == "Session (5h)" && n.Max == 100 && n.Now == 62 {
			meter = true
		}
	}
	if !meter {
		t.Error("no meter at 62 of 100 for the session limit")
	}
}

func TestLimitsShowsALapsedLogin(t *testing.T) {
	l := limitsApp(t)
	l.live.ExpiresAt = fixture.Now.UnixMilli()
	must(t, l.tt.Click("Limits"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText(account.LiveExpired) })
	if !l.tt.HasText("Claude Code") || !l.tt.HasText("In use") {
		t.Fatalf("texts %q", l.tt.Texts())
	}
}

func TestLimitsRefreshSkipsTheFloor(t *testing.T) {
	l := limitsApp(t)
	l.open(t)
	n := l.usage.Load()
	must(t, l.tt.Click("Refresh"))
	waitFor(t, l.tt, func() bool { return l.usage.Load() == n+1 })
}

// The menu bar item reads the limits on any page, the floor kept.
func TestTrayTickReadsOnAnyPage(t *testing.T) {
	l := limitsApp(t)
	l.weekly.Store("alice", 50) // not spent: read every 3 minutes, not 10
	l.open(t)
	must(t, l.tt.Click("Overview"))
	l.tt.Frame()
	*l.now = l.now.Add(5 * time.Minute)
	l.a.TrayTick()
	waitFor(t, l.tt, func() bool { return l.usage.Load() == 2 })
	*l.now = l.now.Add(time.Minute)
	l.a.TrayTick()
	l.tt.Frame()
	l.tt.Frame()
	if n := l.usage.Load(); n != 2 {
		t.Fatalf("usage asked %d times, want 2: within the 3-minute floor", n)
	}
}

func TestLimitsKeepsAForcedLoadAskedDuringOne(t *testing.T) {
	l := limitsApp(t)
	var queued []func()
	l.a.Go = func(f func()) { queued = append(queued, f) }
	l.a.limits.load(l.a, false)
	l.a.limits.load(l.a, true) // Refresh, while the first load runs
	if len(queued) != 1 {
		t.Fatalf("%d loads started, want 1", len(queued))
	}
	queued[0]()
	l.tt.Frame() // the first lands, and starts the one asked meanwhile
	if len(queued) != 2 {
		t.Fatalf("%d loads started, want 2", len(queued))
	}
	queued[1]()
	l.tt.Frame()
	if n := l.usage.Load(); n != 2 {
		t.Fatalf("usage asked %d times, want 2: the forced load was dropped or not forced", n)
	}
}

func TestLimitsTimes(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct{ got, want string }{
		{clock(now.Add(3*time.Hour), now), "15:00"},
		{clock(now.Add(30*time.Hour), now), "Oct 27, 18:00"},
		{resets(time.Time{}, now), ""},
		{resets(now.Add(3*time.Hour), now), "resets 3h 0m · 15:00"},
		{ago(now.Add(-30*time.Second), now), "just now"},
		{ago(now.Add(-4*time.Minute), now), "4 min ago"},
		{ago(now.Add(-2*time.Hour), now), "at 10:00"},
		{goodUntil(now.Add(-time.Minute), now), "Sign-in expired"},
		{goodUntil(now.Add(5*time.Hour), now), "Sign-in expires today"},
		{goodUntil(now.Add(40*time.Hour), now), "Sign-in expires in 2 days"},
		{goodUntil(now.Add(10*24*time.Hour), now), "Sign-in good until Nov 5"},
		{planText(account.Account{Plan: "team", OrgName: "Acme"}), "Team · Acme"},
		{planText(account.Account{Plan: "max", OrgName: "Me"}), "Max"},
		{planText(account.Account{}), ""},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

// seedBob lists bob as an added account, as a sign-in would have, with his
// login under token tok.
func seedBob(t *testing.T, l *lim, tok string) {
	t.Helper()
	login := account.Login{AccessToken: tok, RefreshToken: "bob-rt", ExpiresAt: fixture.Now.Add(time.Hour).UnixMilli()}
	must(t, l.vault.Put(account.ParkService, "bob/org", login.Marshal()))
	must(t, os.WriteFile(filepath.Join(l.book.Dir, "accounts.json"),
		[]byte(`{"accounts":[{"accountUuid":"bob","orgUuid":"org","email":"bob@example.com","goodUntil":"2026-11-15T12:00:00Z"}]}`), 0o600))
}

// claudeAt stands in for claude auth login: it prints a link, asks for a
// code and, on "good", leaves bob's login in its config folder.
func claudeAt(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	login := filepath.Join(dir, "login.json")
	must(t, os.WriteFile(login, account.Login{AccessToken: "bob", RefreshToken: "bob-rt", ExpiresAt: fixture.Now.Add(time.Hour).UnixMilli()}.Marshal(), 0o600))
	return fixture.FakeClaude(t, fixture.Claude{Link: "https://claude.ai/oauth/authorize?state=abc", Login: login})
}

func TestLimitsRemove(t *testing.T) {
	l := limitsApp(t)
	seedBob(t, l, "bob")
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") })
	if !l.tt.HasText("Sign-in good until Nov 15") {
		t.Errorf("no sign-in date in %q", l.tt.Texts())
	}
	must(t, l.tt.Click("⋯"))
	must(t, l.tt.ChooseMenuItem("Remove…"))
	l.tt.Frame()
	must(t, l.tt.Click("Remove"))
	waitFor(t, l.tt, func() bool { return !l.tt.HasText("bob@example.com") })
	if l.vault.Len() != 0 {
		t.Fatal("bob's login stays in the keychain")
	}
}

func TestLimitsOffersSignInAgain(t *testing.T) {
	l := limitsApp(t)
	seedBob(t, l, "expired")
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Sign-in expired.") })
	if !l.tt.HasText("Sign In Again") {
		t.Fatalf("texts %q", l.tt.Texts())
	}
}

func TestLimitsAddAccount(t *testing.T) {
	l := limitsApp(t)
	bin := claudeAt(t)
	l.book.Claude = func() (string, error) { return bin, nil }
	l.open(t)
	must(t, l.tt.Click("Add Account…"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Submit Code") })
	must(t, l.tt.Click("Copy Link"))
	must(t, l.tt.Click("Open Browser"))
	want := []string{"https://claude.ai/oauth/authorize?state=abc"}
	if !slices.Equal(*l.copied, want) || !slices.Equal(*l.opened, want) {
		t.Fatalf("copied %v, opened %v", *l.copied, *l.opened)
	}
	l.tt.Update(func() { l.a.limits.code = "good" })
	l.tt.Frame()
	must(t, l.tt.Click("Submit Code"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") && !l.tt.HasText("Submit Code") })
}

func TestLimitsAddAccountFails(t *testing.T) {
	l := limitsApp(t)
	bin := claudeAt(t)
	l.book.Claude = func() (string, error) { return bin, nil }
	l.open(t)
	must(t, l.tt.Click("Add Account…"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Submit Code") })
	l.tt.Update(func() { l.a.limits.code = "bad" })
	l.tt.Frame()
	must(t, l.tt.Click("Submit Code"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Invalid code") && l.tt.HasText("Try Again") })
}

func TestLimitsCancelAddAccount(t *testing.T) {
	l := limitsApp(t)
	bin := claudeAt(t)
	l.book.Claude = func() (string, error) { return bin, nil }
	l.open(t)
	must(t, l.tt.Click("Add Account…"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Submit Code") })
	must(t, l.tt.Click("Cancel"))
	waitFor(t, l.tt, func() bool {
		left, _ := filepath.Glob(filepath.Join(l.book.Dir, "signin-*"))
		return !l.tt.HasText("Submit Code") && len(left) == 0
	})
}

func TestLimitsIgnoresAnOldSignIn(t *testing.T) {
	l := limitsApp(t)
	bin := claudeAt(t)
	l.book.Claude = func() (string, error) { return bin, nil }
	l.open(t)
	must(t, l.tt.Click("Add Account…"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Submit Code") })
	defer l.a.limits.stopSignIn()
	l.tt.Update(func() { l.a.limits.startAdd(l.a) }) // Try Again: the first is canceled
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Submit Code") })
	for range 30 { // the first one's end comes late; it must change nothing
		l.tt.Frame()
		time.Sleep(10 * time.Millisecond)
	}
	if l.a.limits.failed != "" || !l.a.limits.adding || l.a.limits.signIn == nil {
		t.Fatalf("the old sign-in's end reached the new dialog: failed %q, adding %v", l.a.limits.failed, l.a.limits.adding)
	}
}

// Starting a sign-in must not hold the main thread: finding claude runs a
// login shell, and the book may be busy with a refresh.
func TestLimitsAddAccountDoesNotBlock(t *testing.T) {
	l := limitsApp(t)
	bin := claudeAt(t)
	l.book.Claude = func() (string, error) { time.Sleep(time.Second); return bin, nil }
	l.open(t)
	l.a.Go = func(f func()) { go f() }
	start := time.Now()
	l.a.limits.startAdd(l.a)
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("startAdd held the main thread for %v", d)
	}
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Submit Code") })
	l.a.limits.stopSignIn()
}

func TestLimitsShowsPitboardAccounts(t *testing.T) {
	l := limitsApp(t)
	l.book.Pitboard = func() ([]account.Account, error) {
		return []account.Account{{AccountUUID: "carol", OrgUUID: "org", Email: "carol@example.com", Plan: "Max 5x", Source: "Pitboard",
			ReadAt: fixture.Now.Add(-4 * time.Minute), Limits: []account.Limit{{Kind: "session", Percent: 5}}}}, nil
	}
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("carol@example.com") })
	for _, s := range []string{"Max 5x", "5%", "Read 4 min ago by Pitboard"} {
		if !l.tt.HasText(s) {
			t.Errorf("no %q in %q", s, l.tt.Texts())
		}
	}
	if l.tt.HasText("⋯") {
		t.Error("a Pitboard account offers the app's own actions")
	}
	if l.tt.HasText("Add this account here to switch to it.") {
		t.Error("a switch hint while the app can't switch")
	}
	l.book.Home = &account.Home{}
	must(t, l.tt.Click("Refresh"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Add this account here to switch to it.") })
}

func TestLimitsNotesUnreadablePitboard(t *testing.T) {
	l := limitsApp(t)
	l.book.Pitboard = func() ([]account.Account, error) { return nil, errors.New("Pitboard's files could not be read.") }
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Pitboard's files could not be read.") })
}

func canSwitch(t *testing.T, l *lim) *account.Home {
	t.Helper()
	home := t.TempDir()
	h := &account.Home{Root: filepath.Join(home, ".claude"), ClaudeJSON: filepath.Join(home, ".claude.json"), User: "me"}
	must(t, os.Mkdir(h.Root, 0o700))
	must(t, os.WriteFile(h.ClaudeJSON, []byte(`{"oauthAccount":{"accountUuid":"alice","organizationUuid":"org"}}`), 0o600))
	must(t, l.vault.Put("Claude Code-credentials", "me", l.live.Marshal()))
	l.book.Home = h
	l.book.Live = func() (account.Login, error) { return account.ReadLive(l.vault, "", h.Root) }
	seedBob(t, l, "bob")
	must(t, os.WriteFile(filepath.Join(l.book.Dir, "accounts.json"), []byte(`{"accounts":[{"accountUuid":"bob","orgUuid":"org",
		"email":"bob@example.com","goodUntil":"2026-11-15T12:00:00Z","oauthAccount":{"accountUuid":"bob","organizationUuid":"org"}}]}`), 0o600))
	return h
}

// clickMenu opens the "⋯" menu on email's row.
func clickMenu(t *testing.T, tt *ui.Tester, email string) {
	t.Helper()
	var row, menu []ui.TestNode
	for _, n := range tt.Nodes() {
		switch n.Label {
		case email:
			row = append(row, n)
		case "⋯":
			menu = append(menu, n)
		}
	}
	for _, r := range row {
		for _, m := range menu {
			if m.Bounds.Y < r.Bounds.Y+r.Bounds.H && r.Bounds.Y < m.Bounds.Y+m.Bounds.H {
				tt.ClickAt(m.Bounds.X+m.Bounds.W/2, m.Bounds.Y+m.Bounds.H/2)
				return
			}
		}
	}
	t.Fatalf("no menu on %s's row", email)
}

func TestLimitsSwitch(t *testing.T) {
	l := limitsApp(t)
	h := canSwitch(t, l)
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") })
	l.a.Go = func(f func()) { go f() } // so the switch is seen under way
	configLock := h.ClaudeJSON + ".lock"
	must(t, os.Mkdir(configLock, 0o755))
	clickMenu(t, l.tt, "bob@example.com")
	must(t, l.tt.ChooseMenuItem("Switch to This Account"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Switching…") })
	clickMenu(t, l.tt, "bob@example.com")
	if err := l.tt.ChooseMenuItem("Switch to This Account"); err == nil {
		t.Error("a second switch started while one runs")
	}
	must(t, os.Remove(configLock))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Switched to bob@example.com.") })
	conf, _ := os.ReadFile(h.ClaudeJSON)
	if !strings.Contains(string(conf), `"accountUuid":"bob"`) {
		t.Errorf("~/.claude.json = %s", conf)
	}
	waitFor(t, l.tt, func() bool {
		n := 0
		for _, s := range l.tt.Texts() {
			if s == "⋯" {
				n++
			}
		}
		return n == 2
	})
}

func TestLimitsSwitchNotOffered(t *testing.T) {
	l := limitsApp(t)
	canSwitch(t, l)
	l.book.Home = nil
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") })
	clickMenu(t, l.tt, "bob@example.com")
	if slices.Contains(l.tt.Menu(), "Switch to This Account") {
		t.Errorf("a switch offered outside Claude Code's default folder: %q", l.tt.Menu())
	}
}

func TestLimitsSwitchError(t *testing.T) {
	l := limitsApp(t)
	canSwitch(t, l)
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") })
	must(t, l.vault.Delete(account.ParkService, "bob/org"))
	clickMenu(t, l.tt, "bob@example.com")
	must(t, l.tt.ChooseMenuItem("Switch to This Account"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText(account.ErrInvalidGrant.Error()) })
	waitFor(t, l.tt, func() bool { return !l.tt.HasText("Switching…") })
}

// Half way through the week at 70%: 20 points ahead of pace.
func TestLimitsAheadOfPace(t *testing.T) {
	l := limitsApp(t)
	l.weekly.Store("alice", 70)
	l.weeklyReset.Store("2026-10-30T00:00:00Z") // 3.5 days from fixture.Now
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Ahead of pace") })
	l.weekly.Store("alice", 60)
	must(t, l.tt.Click("Refresh"))
	waitFor(t, l.tt, func() bool { return !l.tt.HasText("Ahead of pace") })
}

func TestLimitsExtraUsage(t *testing.T) {
	l := limitsApp(t)
	l.weekly.Store("alice", 10)
	l.extra.Store(`{"is_enabled":true,"used_credits":1234,"monthly_limit":5000,"utilization":90,"currency":"USD"}`)
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Extra usage") && l.tt.HasText("12.34 of 50.00 USD") })
	if !l.tt.HasText("90%") {
		t.Errorf("no share in %q", l.tt.Texts())
	}
	var title string
	l.tt.Update(func() { title, _ = l.a.Tray() })
	l.tt.Frame()
	if title != "alice · 62% · 10%" {
		t.Errorf("title %q: extra usage is no window", title)
	}
	if it := trayItem(t, l, "alice@example.com"); !strings.HasSuffix(it.Label, "extra 90%") {
		t.Errorf("tray %q", it.Label)
	}
}

func TestPlanText(t *testing.T) {
	now := fixture.Now
	for _, c := range []struct {
		a    account.Account
		want string
	}{
		{account.Account{Plan: "max"}, "Max"},
		{account.Account{Plan: "max", PlanTier: "default_claude_max_20x"}, "Max 20x"},
		{account.Account{PlanTier: "default_claude_ai", PlanOrg: "claude_team", OrgName: "Acme"}, "Team · Acme"},
	} {
		if got := planText(c.a); got != c.want {
			t.Errorf("%+v: %q", c.a, got)
		}
	}
	if planAge(account.Account{PlanAt: now.Add(-11 * time.Hour)}, now) != "" || planAge(account.Account{PlanAt: now.Add(-13 * time.Hour)}, now) == "" {
		t.Error("planAge")
	}
}

// A lapsed subscription shows a highlighted Free, as claude-swap's.
func TestLimitsLapsedPlan(t *testing.T) {
	l := limitsApp(t)
	seedBob(t, l, "bob")
	must(t, os.WriteFile(filepath.Join(l.book.Dir, "accounts.json"), []byte(`{"accounts":[{"accountUuid":"bob","orgUuid":"org","email":"bob@example.com",`+
		`"planTier":"default_claude_ai","planStatus":"canceled","planAt":"2026-10-26T11:00:00Z"}]}`), 0o600))
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") && l.tt.HasText("Free (lapsed)") })
}

// load off the main thread, as the app runs it: under -race, a closure the
// main thread runs must share no variable the worker writes after it.
func TestLimitsLoadOffTheMainThread(t *testing.T) {
	l := limitsApp(t)
	l.open(t)
	l.a.Go = func(f func()) { go f() }
	for range 5 {
		n := l.usage.Load()
		l.a.Update(func() { l.a.limits.load(l.a, true) })
		waitFor(t, l.tt, func() bool { return l.usage.Load() > n && !l.a.limits.loading })
	}
}

// count is how many elements show s.
func count(tt *ui.Tester, s string) int {
	n := 0
	for _, x := range tt.Texts() {
		if x == s {
			n++
		}
	}
	return n
}

// A saved account that can be switched to has its Switch button on the
// card, not only in the ⋯ menu; the account in use has none.
func TestLimitsSwitchButton(t *testing.T) {
	l := limitsApp(t)
	h := canSwitch(t, l)
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") })
	if n := count(l.tt, "Switch"); n != 1 {
		t.Fatalf("%d Switch buttons, want 1 (bob's): %q", n, l.tt.Texts())
	}
	must(t, l.tt.Click("Switch"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Switched to bob@example.com.") })
	conf, _ := os.ReadFile(h.ClaudeJSON)
	if !strings.Contains(string(conf), `"accountUuid":"bob"`) {
		t.Errorf("~/.claude.json = %s", conf)
	}
}

func TestLimitsSwitchButtonNotOffered(t *testing.T) {
	l := limitsApp(t)
	canSwitch(t, l)
	l.book.Home = nil
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") })
	if count(l.tt, "Switch") > 0 {
		t.Errorf("a Switch button outside Claude Code's default folder: %q", l.tt.Texts())
	}
}

// A Pitboard-only account can't be switched to until its login is saved
// here, so its card offers the sign-in that saves it.
func TestLimitsPitboardAccountOffersAdd(t *testing.T) {
	l := limitsApp(t)
	l.book.Pitboard = func() ([]account.Account, error) {
		return []account.Account{{AccountUUID: "carol", OrgUUID: "org", Email: "carol@example.com", Source: "Pitboard",
			ReadAt: fixture.Now, Limits: []account.Limit{{Kind: "session", Percent: 5}}}}, nil
	}
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("carol@example.com") })
	if n := count(l.tt, "Add Account…"); n != 1 {
		t.Errorf("%d Add Account buttons while the app can't switch, want the toolbar's only", n)
	}
	if l.tt.HasText("Take Over from Pitboard") {
		t.Error("a take-over offered for an account whose login Pitboard doesn't hold")
	}
	l.book.Home = &account.Home{}
	must(t, l.tt.Click("Refresh"))
	waitFor(t, l.tt, func() bool { return count(l.tt, "Add Account…") == 2 })
}

// A Pitboard-only account whose login Pitboard parks is taken over in one
// click, with no sign-in, and can then be switched to.
func TestLimitsTakeOverFromPitboard(t *testing.T) {
	l := limitsApp(t)
	h := canSwitch(t, l)
	dir := t.TempDir()
	sum := sha256.Sum256([]byte("carol-rt"))
	must(t, os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"schema":8,"accounts":[
		{"id":"p1","provider":"claude","label":"side","email":"carol@example.com","account_uuid":"carol","organization_uuid":"org",
		 "parked":{"service":"pitboard-park-p1","refresh_fingerprint":"`+hex.EncodeToString(sum[:8])+`"},
		 "oauth_account":{"accountUuid":"carol","emailAddress":"carol@example.com","organizationUuid":"org"}}]}`), 0o600))
	carol := account.Login{AccessToken: "carol", RefreshToken: "carol-rt", ExpiresAt: fixture.Now.Add(time.Hour).UnixMilli()}
	must(t, l.vault.Put("pitboard-park-p1", h.User, carol.Marshal()))
	var forgot []string
	l.book.PitboardDir = dir
	l.book.PitboardForget = func(_ context.Context, label string) error { forgot = append(forgot, label); return nil }
	l.book.Pitboard = func() ([]account.Account, error) { return account.ReadPitboard(dir) }
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("carol@example.com") })
	if n := count(l.tt, "Switch"); n != 1 {
		t.Fatalf("%d Switch buttons before the take-over, want bob's alone", n)
	}
	must(t, l.tt.Click("Take Over from Pitboard"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Took over carol@example.com from Pitboard.") })
	if !slices.Equal(forgot, []string{"side"}) {
		t.Errorf("Pitboard asked to forget %q", forgot)
	}
	waitFor(t, l.tt, func() bool { return count(l.tt, "Switch") == 2 && count(l.tt, "Take Over from Pitboard") == 0 })
}
