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
