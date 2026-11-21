package account

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// t0 is the tests' clock.
var t0 = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

var (
	alice = Profile{AccountUUID: "alice", Email: "alice@example.com", OrgUUID: "org-a", OrgName: "Alice"}
	bob   = Profile{AccountUUID: "bob", Email: "bob@example.com", OrgUUID: "org-b", OrgName: "Acme"}
	carol = Profile{AccountUUID: "carol", Email: "carol@example.com", OrgUUID: "org-c"}
)

// login is a login whose access token at lapses at expires.
func login(at, rt string, expires time.Time) Login {
	return Login{AccessToken: at, RefreshToken: rt, ExpiresAt: expires.UnixMilli(),
		Scopes: []string{"user:inference", "user:profile"}, SubscriptionType: "max"}
}

// anthropic fakes Anthropic's three endpoints. An access token in who
// names its account. Usage answers its session limit at usage[token]
// percent, or with status[token], or else 401. A renewal of a refresh token
// in renewed gives that access token, and the refresh token with "+" added.
type anthropic struct {
	mu        sync.Mutex
	who       map[string]Profile
	usage     map[string]int
	status    map[string]int
	renewed   map[string]string
	resets    map[string]string      // a token's session reset, where not t0+3h
	probe     map[string]probeAnswer // "<token> <model>": the header probe's answer; none is 401
	plans     map[string]string      // a token's rate_limit_tier in its profile
	calls     []string               // "profile <token>", "usage <token>", "renew <refresh token>"
	onProfile func()
}

func newAnthropic() *anthropic {
	return &anthropic{who: map[string]Profile{}, usage: map[string]int{}, status: map[string]int{}, renewed: map[string]string{}, resets: map[string]string{}, probe: map[string]probeAnswer{}}
}

func (f *anthropic) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	switch r.URL.Path {
	case "/api/oauth/profile":
		f.calls = append(f.calls, "profile "+tok)
		if f.onProfile != nil {
			f.onProfile()
		}
		p, ok := f.who[tok]
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprintf(w, `{"account":{"uuid":%q,"email":%q},"organization":{"uuid":%q,"name":%q,"rate_limit_tier":%q}}`, p.AccountUUID, p.Email, p.OrgUUID, p.OrgName, f.plans[tok])
	case "/api/oauth/usage":
		f.calls = append(f.calls, "usage "+tok)
		if s, ok := f.status[tok]; ok {
			if s == http.StatusTooManyRequests {
				w.Header().Set("Retry-After", "600")
			}
			w.WriteHeader(s)
			return
		}
		pct, ok := f.usage[tok]
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		reset, ok := f.resets[tok]
		if !ok {
			reset = "2026-10-26T15:00:00Z"
		}
		fmt.Fprintf(w, `{"limits":[{"kind":"session","percent":%d,"resets_at":%q}]}`, pct, reset)
	case "/v1/messages":
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.calls = append(f.calls, "probe "+tok+" "+body.Model)
		ans, ok := f.probe[tok+" "+body.Model]
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		for k, v := range ans.headers {
			w.Header().Set(k, v)
		}
		if ans.status != 0 {
			w.WriteHeader(ans.status)
		}
		io.WriteString(w, `{}`)
	case "/v1/oauth/token":
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		rt := body["refresh_token"]
		f.calls = append(f.calls, "renew "+rt)
		at, ok := f.renewed[rt]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":"invalid_grant"}`)
			return
		}
		fmt.Fprintf(w, `{"access_token":%q,"refresh_token":%q,"expires_in":28800,"refresh_token_expires_in":2592000}`, at, rt+"+")
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// took returns the calls made since it was last called.
func (f *anthropic) took() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.calls
	f.calls = nil
	return c
}

// newBook is a book on a fake Anthropic, with a Memory vault, in a fresh
// folder, on a clock at t0 the test can move. Nobody is signed in to
// Claude Code until the test sets the login it returns.
func newBook(t *testing.T, f *anthropic) (*Book, *Memory, *time.Time, *Login) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	api := NewAPI("test")
	api.Base, api.AuthBase = srv.URL, srv.URL
	v := NewMemory()
	now := t0
	var live Login
	b := &Book{
		Dir: t.TempDir(), Vault: v, API: api,
		Now: func() time.Time { return now },
		Live: func() (Login, error) {
			if live.AccessToken == "" {
				return Login{}, ErrNotSignedIn
			}
			return live, nil
		},
	}
	return b, v, &now, &live
}

// blockOf is the block of email among got.
func blockOf(t *testing.T, got []Account, email string) Account {
	t.Helper()
	for _, a := range got {
		if a.Email == email {
			return a
		}
	}
	t.Fatalf("no block for %s in %+v", email, got)
	return Account{}
}

// probeAnswer is the fake's answer to a header probe: its status (200 when
// 0) and headers.
type probeAnswer struct {
	status  int
	headers map[string]string
}
