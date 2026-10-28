package view

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	opened, copied *[]string
}

func limitsApp(t *testing.T) *lim {
	t.Helper()
	a, tt, _, _ := newApp(t)
	var usage atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		switch r.URL.Path {
		case "/api/oauth/profile":
			fmt.Fprintf(w, `{"account":{"uuid":%q,"email_address":"%s@example.com"},"organization":{"uuid":"org","name":"Acme"}}`, tok, tok)
		case "/api/oauth/usage":
			if tok == "expired" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			usage.Add(1)
			io.WriteString(w, `{"limits":[{"kind":"session","percent":62,"resets_at":"2026-10-26T15:00:00Z"},{"kind":"weekly_all","percent":100,"resets_at":"2026-11-03T02:00:00Z"}]}`)
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
	l := &lim{a: a, tt: tt, vault: account.NewMemory(), now: &now, live: &live, usage: &usage, opened: &[]string{}, copied: &[]string{}}
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
	for _, s := range []string{"alice@example.com", "Max", "In use", "Session (5h)", "62%", "resets 15:00",
		"Weekly", "100%", "resets Nov 3, 02:00", "Limit reached", "Updated just now", "Add Account…", "Refresh"} {
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

func TestLimitsTicksOnlyOnItsPage(t *testing.T) {
	l := limitsApp(t)
	l.open(t)
	*l.now = l.now.Add(5 * time.Minute)
	l.a.LimitsTick()
	waitFor(t, l.tt, func() bool { return l.usage.Load() == 2 })
	must(t, l.tt.Click("Overview"))
	l.tt.Frame()
	*l.now = l.now.Add(5 * time.Minute)
	l.a.LimitsTick()
	l.tt.Frame()
	l.tt.Frame()
	if n := l.usage.Load(); n != 2 {
		t.Fatalf("usage asked %d times, want 2: the tick reads only on the Limits page", n)
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
		{resets(now.Add(3*time.Hour), now), "resets 15:00"},
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
