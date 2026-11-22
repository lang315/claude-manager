package account

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The app and the command line are two processes on one accounts.json: a
// change one saves while the other is mid-refresh outlives the latter's
// save. Two Books on one folder stand in for them.
func TestTwoBooksKeepEachOthersChanges(t *testing.T) {
	f := newAnthropic()
	f.who["alice-at"], f.who["bob-at"] = alice, bob
	app, v, _, live := newBook(t, f)
	*live = login("alice-at", "alice-rt", t0.Add(time.Hour))
	_, err := app.keep(login("bob-at", "bob-rt", t0.Add(time.Hour)).Marshal(), bob, json.RawMessage(bobAccount))
	must(t, err)

	entered, release := make(chan struct{}), make(chan struct{})
	gated := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gated {
			gated = false
			close(entered)
			<-release
		}
		f.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	app.API.Base, app.API.AuthBase = srv.URL, srv.URL

	cli := &Book{Dir: app.Dir, Vault: v, API: app.API, Now: app.Now, Live: app.Live}
	refreshed := make(chan struct{})
	go func() { app.Refresh(ctx, true); close(refreshed) }()
	<-entered // the app is mid-refresh, its book read
	aliased := make(chan error, 1)
	go func() { aliased <- cli.SetAlias(bob.Key(), "dev") }()
	time.Sleep(100 * time.Millisecond)
	close(release)
	<-refreshed
	must(t, <-aliased)

	again := &Book{Dir: app.Dir, Vault: v, API: app.API, Now: app.Now}
	got, _ := again.Blocks()
	if a := blockOf(t, got, "bob@example.com"); a.Alias != "dev" {
		t.Errorf("the command line's alias was saved over: %+v", a)
	}
	// And the app's next call sees it, not its own older copy.
	got, _ = app.Blocks()
	if a := blockOf(t, got, "bob@example.com"); a.Alias != "dev" {
		t.Errorf("the app kept its older copy: alias %q", a.Alias)
	}
}

// A re-read keeps a failing account's back-off, saved as Refresh saves it,
// as every process goes by it.
func TestReReadKeepsBackoff(t *testing.T) {
	app, v, _, _ := newBook(t, newAnthropic())
	_, err := app.keep(login("bob-at", "bob-rt", t0.Add(time.Hour)).Marshal(), bob, json.RawMessage(bobAccount))
	must(t, err)
	retry := t0.Add(4 * time.Minute)
	app.added[0].Failures, app.added[0].Retry = 3, retry
	must(t, app.save())
	cli := &Book{Dir: app.Dir, Vault: v, API: app.API, Now: app.Now}
	must(t, cli.SetAlias(bob.Key(), "dev"))
	got, _ := app.Blocks()
	a := blockOf(t, got, "bob@example.com")
	if a.Alias != "dev" || a.RetryAt() != retry || a.Failures != 3 {
		t.Errorf("after a re-read: alias %q, retry %v, failures %d", a.Alias, a.RetryAt(), a.Failures)
	}
}

// claude-swap's usage store: a failing read's back-off is shared, so a
// command line started every few seconds doesn't ask again at once.
func TestBackoffIsShared(t *testing.T) {
	f := newAnthropic()
	app, v, _, _ := newBook(t, f)
	_, err := app.keep(login("bob-at", "bob-rt", t0.Add(time.Hour)).Marshal(), bob, json.RawMessage(bobAccount))
	must(t, err)
	f.status["bob-at"] = 500
	app.Refresh(ctx, false)
	f.took()
	cli := &Book{Dir: app.Dir, Vault: v, API: app.API, Now: app.Now, Live: app.Live}
	cli.Refresh(ctx, false)
	if calls := f.took(); len(calls) != 0 {
		t.Errorf("another process asked again inside the back-off: %q", calls)
	}
}

// claude-swap's autoswitch_state.json: the cooldown, the no-return bar and
// how long the account in use has been unreadable hold for every process,
// so auto --once from cron can't flap between two accounts.
func TestAutoStateIsShared(t *testing.T) {
	b, v, h, f := switchBook(t)
	f.usage["alice-at"], f.usage["bob-at"] = 95, 10
	b.Refresh(ctx, true)
	s := DefaultAuto()
	s.On = true
	must(t, b.SetAuto(s))
	if d, err := b.AutoSwitch(ctx); err != nil || d.Email != "bob@example.com" {
		t.Fatalf("first switch: %+v %v", d, err)
	}
	f.usage["alice-at"], f.usage["bob-at"] = 10, 95
	next := &Book{Dir: b.Dir, Vault: v, API: b.API, Now: b.Now, Home: h, Live: b.Live}
	next.Refresh(ctx, true)
	if d, _ := next.Once(ctx, false); d.To != "" {
		t.Errorf("another process switched back in the cooldown: %+v", d)
	}
}

// claude-swap persists its quarantine: a refused refresh token is not
// posted again by the next process either.
func TestQuarantineIsShared(t *testing.T) {
	f := newAnthropic()
	b, v, _, _ := newBook(t, f)
	_, err := b.keep(login("bob-at", "bob-rt", t0.Add(time.Minute)).Marshal(), bob, json.RawMessage(bobAccount))
	must(t, err)
	b.Refresh(ctx, true) // the renewal is refused: quarantined
	f.took()
	next := &Book{Dir: b.Dir, Vault: v, API: b.API, Now: func() time.Time { return t0.Add(time.Hour) }, Live: b.Live}
	next.Refresh(ctx, true)
	for _, c := range f.took() {
		if c == "renew bob-rt" {
			t.Error("another process posted the refused token again")
		}
	}
}
