package account

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestLiveService(t *testing.T) {
	if got := liveService(""); got != "Claude Code-credentials" {
		t.Errorf("default = %q", got)
	}
	// The first eight hex digits of `printf %s /Users/lang/.claude | shasum -a 256`.
	if got := liveService("/Users/lang/.claude"); got != "Claude Code-credentials-67dddc3e" {
		t.Errorf("for a config dir = %q", got)
	}
}

// realShape is a login shaped as the user's Mac stores one (2026-10-28),
// with made-up values.
const realShape = `{"claudeAiOauth":{"accessToken":"at","refreshToken":"rt","expiresAt":1793000000000,` +
	`"refreshTokenExpiresAt":1795000000000,"scopes":["user:inference","user:profile"],` +
	`"subscriptionType":"max","subscriptionTypeSource":"x","rateLimitTier":"default_claude_max_20x"}}`

func TestReadLiveFromKeychain(t *testing.T) {
	v := NewMemory()
	if err := v.Put("Claude Code-credentials", "lang", []byte(realShape)); err != nil {
		t.Fatal(err)
	}
	got, err := ReadLive(v, "", t.TempDir())
	if err != nil || got.AccessToken != "at" || got.RefreshToken != "rt" || got.SubscriptionType != "max" || got.ExpiresAt != 1793000000000 {
		t.Fatalf("ReadLive = %+v, %v", got, err)
	}
}

func TestReadLiveForAConfigDir(t *testing.T) {
	v := NewMemory()
	if err := v.Put(liveService("/x"), "lang", []byte(realShape)); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := ReadLive(v, "/x", root); err != nil {
		t.Fatalf("with CLAUDE_CONFIG_DIR=/x: %v", err)
	}
	if _, err := ReadLive(v, "", root); !errors.Is(err, ErrNotSignedIn) {
		t.Fatalf("the default dir read another dir's login: %v", err)
	}
}

func TestReadLiveFromFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".credentials.json"), []byte(realShape), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadLive(NewMemory(), "", root); err != nil || got.AccessToken != "at" {
		t.Fatalf("ReadLive = %+v, %v", got, err)
	}
}

func TestReadLiveNotSignedIn(t *testing.T) {
	if _, err := ReadLive(NewMemory(), "", t.TempDir()); !errors.Is(err, ErrNotSignedIn) {
		t.Fatalf("err = %v", err)
	}
}

type lockedVault struct{ Vault }

func (lockedVault) Get(string, string) ([]byte, error) { return nil, ErrLocked }

func TestReadLiveLocked(t *testing.T) {
	if _, err := ReadLive(lockedVault{NewMemory()}, "", t.TempDir()); !errors.Is(err, ErrLocked) {
		t.Fatalf("err = %v", err)
	}
}

func TestReadLiveWithoutToken(t *testing.T) {
	v := NewMemory()
	if err := v.Put("Claude Code-credentials", "lang", []byte(`{"claudeAiOauth":{"refreshToken":"rt"}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLive(v, "", t.TempDir()); !errors.Is(err, errNoToken) {
		t.Fatalf("err = %v", err)
	}
}

func TestLoginRoundTrip(t *testing.T) {
	l := Login{AccessToken: "at", RefreshToken: "rt", ExpiresAt: 5, RefreshTokenExpiresAt: 6, Scopes: []string{"user:profile"}, SubscriptionType: "pro", RateLimitTier: "x", ClientID: "c"}
	got, err := parseLogin(l.Marshal())
	if err != nil || !reflect.DeepEqual(got, l) {
		t.Fatalf("round trip = %+v, %v", got, err)
	}
}

func TestLoginExpiresBy(t *testing.T) {
	at := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	l := Login{ExpiresAt: at.UnixMilli()}
	if l.expiresBy(at.Add(-time.Millisecond)) || !l.expiresBy(at) {
		t.Fatal("a login lapses at its expiresAt, not before")
	}
}
