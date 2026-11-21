package account

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func apiOn(t *testing.T, h http.HandlerFunc) *API {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	a := NewAPI("9.9.9")
	a.Base, a.AuthBase = srv.URL, srv.URL
	return a
}

func TestUsageRequest(t *testing.T) {
	a := apiOn(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/oauth/usage" || r.Header.Get("Authorization") != "Bearer tok" ||
			r.Header.Get("anthropic-beta") != "oauth-2025-04-20" || r.Header.Get("User-Agent") != "claude-manager/9.9.9" {
			t.Errorf("request %s %s %v", r.Method, r.URL.Path, r.Header)
		}
		io.WriteString(w, `{"limits":[{"kind":"session","percent":12}]}`)
	})
	got, err := a.Usage(context.Background(), "tok")
	if err != nil || len(got) != 1 || got[0].Percent != 12 {
		t.Fatalf("Usage = %+v, %v", got, err)
	}
}

func TestProfile(t *testing.T) {
	a := apiOn(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/oauth/profile" {
			t.Errorf("path %s", r.URL.Path)
		}
		io.WriteString(w, `{"account":{"uuid":"acc","email_address":"me@example.com","full_name":"Me"},"organization":{"uuid":"org","name":"Acme"}}`)
	})
	got, err := a.Profile(context.Background(), "tok")
	want := Profile{AccountUUID: "acc", Email: "me@example.com", OrgUUID: "org", OrgName: "Acme"}
	if err != nil || got != want || got.Key() != "acc/org" {
		t.Fatalf("Profile = %+v, %v", got, err)
	}
	// As /api/oauth/profile answered on the user's Mac, 2026-10-28: "email".
	a = apiOn(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"account":{"uuid":"acc","email":"me@example.com","display_name":"Me"},"organization":{"uuid":"org","name":"Acme","rate_limit_tier":"x"}}`)
	})
	want.Tier = "x"
	if got, err := a.Profile(context.Background(), "tok"); err != nil || got != want {
		t.Fatalf("Profile of the real shape = %+v, %v", got, err)
	}
	a = apiOn(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{}`) })
	if _, err := a.Profile(context.Background(), "tok"); err == nil {
		t.Fatal("a profile with no account worked")
	}
}

func is(target error) func(error) bool {
	return func(err error) bool { return errors.Is(err, target) }
}

func refusedWith(msg string) func(error) bool {
	return func(err error) bool {
		var r *Refused
		return errors.As(err, &r) && r.Message == msg
	}
}

func TestRefusals(t *testing.T) {
	for _, c := range []struct {
		status int
		body   string
		ok     func(error) bool
	}{
		{401, `{"type":"error","error":{"type":"authentication_error","message":"OAuth access token is invalid."}}`, is(ErrUnauthorized)},
		{403, `{"type":"error","error":{"type":"permission_error","message":"OAuth token does not meet scope requirement user:profile"}}`, is(ErrUnauthorized)},
		{403, `{"type":"error","error":{"type":"permission_error","message":"Verify your account","details":{"error_code":"verification_required"}}}`, refusedWith("Verify your account")},
		{401, `{"type":"error","error":{"type":"authentication_error","message":"OAuth authentication is currently not allowed for this organization."}}`,
			refusedWith("OAuth authentication is currently not allowed for this organization." + orgNote)},
		{500, `{"leak":"SECRET"}`, func(err error) bool { return err.Error() == "Anthropic answered 500." }},
	} {
		a := apiOn(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			io.WriteString(w, c.body)
		})
		if _, err := a.Usage(context.Background(), "tok"); err == nil || !c.ok(err) {
			t.Errorf("%d %s: err = %v", c.status, c.body, err)
		}
	}
}

func TestRateLimited(t *testing.T) {
	for header, want := range map[string]time.Duration{"120": 2 * time.Minute, "": 5 * time.Minute} {
		a := apiOn(t, func(w http.ResponseWriter, r *http.Request) {
			if header != "" {
				w.Header().Set("Retry-After", header)
			}
			w.WriteHeader(http.StatusTooManyRequests)
		})
		_, err := a.Usage(context.Background(), "tok")
		var rl *RateLimited
		if !errors.As(err, &rl) || rl.Wait != want {
			t.Errorf("Retry-After %q: err = %v, want a wait of %v", header, err, want)
		}
	}
}

func TestUnreachable(t *testing.T) {
	a := NewAPI("9.9.9")
	a.Base = "http://127.0.0.1:1"
	if _, err := a.Usage(context.Background(), "tok"); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v", err)
	}
}

func TestRenew(t *testing.T) {
	a := apiOn(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		want := map[string]string{"grant_type": "refresh_token", "refresh_token": "rt", "client_id": claudeCodeClient, "scope": "user:inference user:profile"}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/oauth/token" || !maps.Equal(body, want) || r.Header.Get("User-Agent") != "claude-manager/9.9.9" {
			t.Errorf("request %s %s %v", r.Method, r.URL.Path, body)
		}
		io.WriteString(w, `{"access_token":"new","expires_in":28800,"refresh_token_expires_in":2592000}`)
	})
	got, err := a.Renew(context.Background(), login("old", "rt", t0), t0)
	if err != nil || got.AccessToken != "new" || got.RefreshToken != "rt" || got.ExpiresAt != t0.Add(8*time.Hour).UnixMilli() ||
		got.RefreshTokenExpiresAt != t0.Add(30*24*time.Hour).UnixMilli() || got.SubscriptionType != "max" {
		t.Fatalf("Renew = %+v, %v", got, err)
	}
}

func TestRenewAsTheLoginsClient(t *testing.T) {
	a := apiOn(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["client_id"] != "other" {
			t.Errorf("client_id = %q", body["client_id"])
		}
		io.WriteString(w, `{"access_token":"new","refresh_token":"rt2","expires_in":60}`)
	})
	l := login("old", "rt", t0)
	l.ClientID = "other"
	if got, err := a.Renew(context.Background(), l, t0); err != nil || got.RefreshToken != "rt2" {
		t.Fatalf("Renew = %+v, %v", got, err)
	}
}

func TestRenewRefusals(t *testing.T) {
	for _, c := range []struct {
		status int
		body   string
		ok     func(error) bool
	}{
		{400, `{"error":"invalid_grant","error_description":"Refresh token not found or invalid"}`, is(ErrInvalidGrant)},
		{401, `{"error":{"type":"invalid_grant"}}`, is(ErrInvalidGrant)},
		{400, `{"error":"invalid_grant","error_description":"account_on_hold"}`, refusedWith("Your Anthropic account is on hold.")},
		{500, `{"access_token":"SECRET"}`, func(err error) bool { return err.Error() == "Anthropic answered 500." }},
		{200, `{}`, func(err error) bool { return err != nil && !strings.Contains(err.Error(), "SECRET") }},
	} {
		a := apiOn(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			io.WriteString(w, c.body)
		})
		if _, err := a.Renew(context.Background(), login("old", "rt", t0), t0); err == nil || !c.ok(err) {
			t.Errorf("%d %s: err = %v", c.status, c.body, err)
		}
	}
}

// claude-swap's tls-cert note: a refused certificate says so, not "couldn't reach".
func TestTLSRefused(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(srv.Close)
	a := NewAPI("test")
	a.Base = srv.URL
	if _, err := a.Usage(context.Background(), "tok"); !errors.Is(err, ErrTLS) {
		t.Errorf("err = %v", err)
	}
	a.Base = "http://127.0.0.1:1"
	if _, err := a.Usage(context.Background(), "tok"); !errors.Is(err, ErrUnreachable) {
		t.Errorf("err = %v", err)
	}
}

func TestStoreNote(t *testing.T) {
	if StoreNote(func(string) string { return "" }) != "" || StoreNote(func(string) string { return "/x" }) == "" {
		t.Error("StoreNote")
	}
}
