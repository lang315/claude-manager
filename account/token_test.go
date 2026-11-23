package account

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const tokA, tokB = "sk-ant-oat01-aaaa", "sk-ant-oat01-bbbb"

func headers(fiveH, sevenD string, reset time.Time) map[string]string {
	r := strconv.FormatInt(reset.Unix(), 10)
	return map[string]string{
		"anthropic-ratelimit-unified-5h-utilization": fiveH, "anthropic-ratelimit-unified-5h-reset": r,
		"anthropic-ratelimit-unified-7d-utilization": sevenD, "anthropic-ratelimit-unified-7d-reset": r,
	}
}

func TestAddToken(t *testing.T) {
	b, v, _, _ := switchBook(t)
	for _, bad := range []string{"sk-ant-api03-x", "x", "sk-ant-oat01 x"} {
		if _, err := b.AddToken(bad, ""); !errors.Is(err, errNotSetupToken) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if _, err := b.AddToken(tokA, "noat"); !errors.Is(err, errTokenEmail) {
		t.Errorf("email: %v", err)
	}
	a, err := b.AddToken(" "+tokA+"\n", "")
	must(t, err)
	b2, err := b.AddToken(tokB, "")
	must(t, err)
	if a.Email != "setup-token-1@token.local" || b2.Email != "setup-token-2@token.local" || !a.Token || keyOf(a.OAuthAccount) != "" {
		t.Fatalf("a %+v b %+v", a, b2)
	}
	if l, _ := parseLogin(get(t, v, ParkService, a.Key())); l.AccessToken != tokA || l.RefreshToken != "" || !slices.Equal(l.Scopes, []string{"user:inference"}) {
		t.Errorf("saved %+v", l)
	}
	_, err = b.AddToken(tokB+"x", "setup-token-1@token.local")
	must(t, err)
	if l, _ := parseLogin(get(t, v, ParkService, a.Key())); l.AccessToken != tokB+"x" {
		t.Errorf("not replaced: %q", l.AccessToken)
	}
	blocks, _ := b.Blocks()
	if n := slices.IndexFunc(blocks, func(x Account) bool { return x.Email == "setup-token-3@token.local" }); n >= 0 {
		t.Error("re-adding made a new account")
	}
}

func TestProbe(t *testing.T) {
	b, _, _, f := switchBook(t)
	reset := t0.Add(2 * time.Hour)
	f.probe[tokA+" "+probeModel] = probeAnswer{headers: headers("0.42", "0.1", reset)}
	f.probe[tokA+" claude-fable-5-1"] = probeAnswer{headers: map[string]string{"anthropic-ratelimit-unified-7d_oi-utilization": "0.5"}}
	got, err := b.API.Probe(ctx, tokA, t0)
	must(t, err)
	want := []Limit{{Kind: "session", Percent: 42, ResetsAt: reset}, {Kind: "weekly_all", Percent: 10, ResetsAt: reset}, {Kind: "weekly_scoped", Scope: "Fable", Percent: 50}}
	if !slices.Equal(got, want) {
		t.Errorf("got %+v", got)
	}
	f.probe[tokA+" claude-fable-5-1"] = probeAnswer{status: 429, headers: map[string]string{"anthropic-ratelimit-unified-status": "rejected", "Retry-After": "60"}}
	got, _ = b.API.Probe(ctx, tokA, t0)
	if got[2] != (Limit{Kind: "weekly_scoped", Scope: "Fable", Percent: 100, ResetsAt: t0.Add(time.Minute), Refused: true}) {
		t.Errorf("refused Fable: %+v", got)
	}
	f.probe[tokA+" "+probeModel] = probeAnswer{status: 429, headers: map[string]string{"anthropic-ratelimit-unified-status": "rejected", "anthropic-ratelimit-unified-reset": "1793016000"}}
	f.took()
	got, _ = b.API.Probe(ctx, tokA, t0)
	if len(got) != 1 || got[0].Percent != 100 || got[0].ResetsAt.Unix() != 1793016000 || !got[0].Refused || got[0].Label() != "Session (5h) · refused" {
		t.Errorf("refused: %+v", got)
	}
	if c := f.took(); len(c) != 1 {
		t.Errorf("probed further after a refusal: %v", c)
	}
	f.probe[tokA+" "+probeModel] = probeAnswer{status: 429}
	if _, err := b.API.Probe(ctx, tokA, t0); !errors.As(err, new(*RateLimited)) {
		t.Errorf("bare 429: %v", err)
	}
	if _, err := b.API.Probe(ctx, "other", t0); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("401: %v", err)
	}
	f.probe[tokA+" "+probeModel] = probeAnswer{headers: map[string]string{"anthropic-ratelimit-unified-5h-utilization": "-0.1"}}
	delete(f.probe, tokA+" claude-fable-5-1")
	if _, err := b.API.Probe(ctx, tokA, t0); err == nil {
		t.Error("no usable window read as a reading")
	}
}

// Off, a setup-token account asks nothing; on, the probe reads it.
func TestRefreshSetupToken(t *testing.T) {
	b, _, _, f := switchBook(t)
	f.usage["alice-at"], f.usage["bob-at"] = 10, 10
	_, err := b.AddToken(tokA, "ci@example.com")
	must(t, err)
	f.took()
	got, _ := b.Refresh(ctx, true)
	if a := blockOf(t, got, "ci@example.com"); a.Err != errNoProbe.Error() {
		t.Errorf("off: %+v", a)
	}
	for _, c := range f.took() {
		if strings.Contains(c, tokA) {
			t.Errorf("asked with the probe off: %s", c)
		}
	}
	s := DefaultAuto()
	s.Probe = true
	must(t, b.SetAuto(s))
	f.probe[tokA+" "+probeModel] = probeAnswer{headers: headers("0.3", "0.2", t0.Add(time.Hour))}
	got, _ = b.Refresh(ctx, true)
	if a := blockOf(t, got, "ci@example.com"); a.Err != "" || a.Limits[0].Percent != 30 {
		t.Errorf("on: %+v", a)
	}
}

// Switching onto a setup-token, then away: the token names its own
// account, never another with no refresh token.
func TestSwitchSetupTokens(t *testing.T) {
	b, v, _, f := switchBook(t)
	_, err := b.AddToken(tokA, "a@token.local")
	must(t, err)
	_, err = b.AddToken(tokB, "b@token.local")
	must(t, err)
	must(t, b.Switch(ctx, tokenUUID+"b@token.local/"))
	if l, _ := parseLogin(get(t, v, liveDefault, "me")); l.AccessToken != tokB {
		t.Fatalf("live %q", l.AccessToken)
	}
	s := DefaultAuto()
	s.Probe = true
	must(t, b.SetAuto(s))
	f.probe[tokB+" "+probeModel] = probeAnswer{headers: headers("0.3", "0.2", t0.Add(time.Hour))}
	f.took()
	got, _ := b.Refresh(ctx, true)
	if a := blockOf(t, got, "b@token.local"); !a.InUse || a.Err != "" {
		t.Errorf("token in use: %+v", a)
	}
	if slices.Contains(f.took(), "profile "+tokB) {
		t.Error("asked whose a setup-token is")
	}
	must(t, b.Switch(ctx, bob.Key()))
	if l, _ := parseLogin(get(t, v, ParkService, tokenUUID+"a@token.local/")); l.AccessToken != tokA {
		t.Errorf("a's token replaced by %q", l.AccessToken)
	}
	if l, _ := parseLogin(get(t, v, ParkService, tokenUUID+"b@token.local/")); l.AccessToken != tokB {
		t.Errorf("b's token %q", l.AccessToken)
	}
	blocks, _ := b.Blocks()
	if a := blockOf(t, blocks, "b@token.local"); !a.Token {
		t.Error("b is no longer a setup-token after being switched off")
	}
}

// A login with no refresh token that is no setup-token's is nobody's: it
// is never filed under a setup-token account.
func TestSwitchUnknownTokenlessLogin(t *testing.T) {
	b, v, _, _ := switchBook(t)
	_, err := b.AddToken(tokA, "a@token.local")
	must(t, err)
	must(t, v.Put(liveDefault, "me", []byte(`{"claudeAiOauth":{"accessToken":"sk-ant-oat01-gone"}}`)))
	must(t, b.Switch(ctx, bob.Key()))
	if kept, _ := b.UnclaimedLogins(); len(kept) != 1 {
		t.Fatalf("not kept as unclaimed: %+v", kept)
	}
	if l, _ := parseLogin(get(t, v, ParkService, tokenUUID+"a@token.local/")); l.AccessToken != tokA {
		t.Errorf("a's token replaced by %q", l.AccessToken)
	}
	if p := b.tokenOwner(Login{AccessToken: tokA, RefreshToken: "rt"}); p != nil {
		t.Error("a login with a refresh token taken for a setup-token")
	}
}

func TestUnclaimedKeptOrNothingSwitched(t *testing.T) {
	b, v, _, _ := switchBook(t)
	must(t, v.Put(liveDefault, "me", []byte(`{"claudeAiOauth":{"accessToken":"sk-ant-oat01-gone"}}`)))
	v.Fail = errors.New("locked")
	if err := b.Switch(ctx, bob.Key()); err == nil || !strings.Contains(err.Error(), "could not be kept") {
		t.Fatalf("err = %v", err)
	}
	v.Fail = nil
	if l, _ := parseLogin(get(t, v, liveDefault, "me")); l.AccessToken != "sk-ant-oat01-gone" {
		t.Error("switched without keeping the login")
	}
	must(t, b.Switch(ctx, bob.Key()))
	kept, _ := b.UnclaimedLogins()
	if len(kept) != 1 || kept[0].Why == "" || !kept[0].At.Equal(t0) {
		t.Fatalf("kept %+v", kept)
	}
	if err := b.PurgeUnclaimed("nope"); !errors.Is(err, errNoUnclaimed) {
		t.Errorf("purge nope: %v", err)
	}
	again := &Book{Dir: b.Dir, Vault: v, Now: b.Now}
	if k, _ := again.UnclaimedLogins(); len(k) != 1 {
		t.Errorf("not saved: %+v", k)
	}
	must(t, b.PurgeUnclaimed(kept[0].ID))
	if _, err := v.Get(UnclaimedService, kept[0].ID); !errors.Is(err, ErrNotFound) {
		t.Error("purged login left in the keychain")
	}
	if kept, _ := b.UnclaimedLogins(); len(kept) != 0 {
		t.Errorf("still listed: %+v", kept)
	}
}

// A failure to tell whose the login is, other than its lapse, refuses the
// switch: nothing is kept as unclaimed.
func TestUnclaimedOnlyForAnUnknownLogin(t *testing.T) {
	b, v, _, _ := switchBook(t)
	b.API.Base = "http://127.0.0.1:1"
	if err := b.Switch(ctx, bob.Key()); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v", err)
	}
	if kept, _ := b.UnclaimedLogins(); len(kept) != 0 {
		t.Errorf("kept %+v", kept)
	}
	if l, _ := parseLogin(get(t, v, liveDefault, "me")); l.AccessToken != "alice-at" {
		t.Error("switched")
	}
}

// A keychain that can't be read is said so, not taken for a lapsed token.
func TestReadTokenLockedKeychain(t *testing.T) {
	b, v, _, _ := newBook(t, newAnthropic())
	must(t, b.load())
	b.auto.Probe = true
	a := &Account{AccountUUID: tokenUUID + "t@x", Email: "t@x", Token: true}
	if _, err := b.readToken(ctx, a, t0); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("no saved token: %v", err)
	}
	b.Vault = lockedVault{v}
	if _, err := b.readToken(ctx, a, t0); !errors.Is(err, ErrLocked) {
		t.Fatalf("locked: %v", err)
	}
}

// A setup-token in use whose usage isn't read, by choice, is no sign of
// trouble: auto-switch holds on it, as on an API key, where claude-swap
// fails over after its unhealthy ticks.
func TestAutoHoldsOnAnUnprobedToken(t *testing.T) {
	b, _, _, f := switchBook(t)
	f.usage["bob-at"] = 10 // bob has room: failover would take him
	_, err := b.AddToken(tokB, "b@token.local")
	must(t, err)
	must(t, b.Switch(ctx, tokenUUID+"b@token.local/"))
	s := DefaultAuto()
	s.On = true
	must(t, b.SetAuto(s))
	var d Decision
	for m := 1; m <= 10; m++ {
		b.Now = func() time.Time { return t0.Add(time.Duration(m) * time.Minute) }
		b.Refresh(ctx, true)
		if d, _ = b.AutoSwitch(ctx); d.To != "" {
			t.Fatalf("moved off the token at minute %d: %+v", m, d)
		}
	}
	if d.Why != errTokenUnread.Error() {
		t.Errorf("why %q", d.Why)
	}
}
