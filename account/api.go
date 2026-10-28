package account

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// claudeCodeClient is the OAuth client Claude Code's logins are issued to.
const claudeCodeClient = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"

var (
	// ErrUnauthorized says Anthropic refused an access token, lapsed or
	// revoked. A renewal may mend it.
	ErrUnauthorized = errors.New("Anthropic refused the sign-in.")
	// ErrInvalidGrant says a login can't be renewed: it needs a sign-in.
	ErrInvalidGrant = errors.New("Sign-in expired.")
	// ErrUnreachable says no answer came.
	ErrUnreachable = errors.New("Couldn't reach Anthropic.")
)

// Refused is a refusal no renewal mends, in Anthropic's words.
type Refused struct{ Message string }

func (e *Refused) Error() string { return e.Message }

// RateLimited says Anthropic asked to wait before asking again.
type RateLimited struct{ Wait time.Duration }

func (e *RateLimited) Error() string { return "Rate limited by Anthropic." }

// Profile is whose a login is.
type Profile struct{ AccountUUID, Email, OrgUUID, OrgName string }

// Key names the account as Account.Key does.
func (p Profile) Key() string { return p.AccountUUID + "/" + p.OrgUUID }

// API asks Anthropic about logins. Tests point Base and AuthBase at a fake.
type API struct {
	Base     string // https://api.anthropic.com
	AuthBase string // https://platform.claude.com
	Agent    string // the User-Agent
	Client   *http.Client
}

// NewAPI asks Anthropic itself, naming the app at version, and gives each
// request 5 seconds.
func NewAPI(version string) *API {
	return &API{
		Base: "https://api.anthropic.com", AuthBase: "https://platform.claude.com",
		Agent: "claude-manager/" + version, Client: &http.Client{Timeout: 5 * time.Second},
	}
}

// Usage reads an account's limits.
func (a *API) Usage(ctx context.Context, token string) ([]Limit, error) {
	body, err := a.get(ctx, "/api/oauth/usage", token)
	if err != nil {
		return nil, err
	}
	return ParseUsage(body)
}

// Profile asks whose a login is.
func (a *API) Profile(ctx context.Context, token string) (Profile, error) {
	body, err := a.get(ctx, "/api/oauth/profile", token)
	if err != nil {
		return Profile{}, err
	}
	var p struct {
		Account struct {
			UUID         string `json:"uuid"`
			Email        string `json:"email"`
			EmailAddress string `json:"email_address"`
		} `json:"account"`
		Organization struct {
			UUID string `json:"uuid"`
			Name string `json:"name"`
		} `json:"organization"`
	}
	if json.Unmarshal(body, &p) != nil || p.Account.UUID == "" || p.Organization.UUID == "" {
		return Profile{}, errors.New("Anthropic's profile names no account.")
	}
	email := p.Account.Email
	if email == "" { // the token endpoint's answer names it so
		email = p.Account.EmailAddress
	}
	return Profile{AccountUUID: p.Account.UUID, Email: email, OrgUUID: p.Organization.UUID, OrgName: p.Organization.Name}, nil
}

// Renew asks for fresh tokens for l, as Claude Code renews its own login,
// and returns l with them. An answer without a refresh token keeps l's.
func (a *API) Renew(ctx context.Context, l Login, now time.Time) (Login, error) {
	client := l.ClientID
	if client == "" {
		client = claudeCodeClient
	}
	payload, _ := json.Marshal(map[string]string{
		"grant_type": "refresh_token", "refresh_token": l.RefreshToken,
		"client_id": client, "scope": strings.Join(l.Scopes, " "),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.AuthBase+"/v1/oauth/token", bytes.NewReader(payload))
	if err != nil {
		return l, err
	}
	req.Header.Set("Content-Type", "application/json")
	body, err := a.do(req, tokenRefusal)
	if err != nil {
		return l, err
	}
	var r struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		ExpiresIn        int64  `json:"expires_in"`
		RefreshExpiresIn int64  `json:"refresh_token_expires_in"`
		Scope            string `json:"scope"`
	}
	if json.Unmarshal(body, &r) != nil || r.AccessToken == "" || r.ExpiresIn <= 0 {
		return l, errors.New("Anthropic's token answer was malformed.")
	}
	l.AccessToken = r.AccessToken
	if r.RefreshToken != "" {
		l.RefreshToken = r.RefreshToken
	}
	l.ExpiresAt = now.Add(time.Duration(r.ExpiresIn) * time.Second).UnixMilli()
	if r.RefreshExpiresIn > 0 {
		l.RefreshTokenExpiresAt = now.Add(time.Duration(r.RefreshExpiresIn) * time.Second).UnixMilli()
	}
	if r.Scope != "" {
		l.Scopes = strings.Fields(r.Scope)
	}
	return l, nil
}

func (a *API) get(ctx context.Context, path, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.Base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	return a.do(req, refusal)
}

// do sends req and returns the body of a 200. refused reads any other
// answer but a 429.
func (a *API) do(req *http.Request, refused func(int, []byte) error) ([]byte, error) {
	req.Header.Set("User-Agent", a.Agent)
	resp, err := a.Client.Do(req)
	if err != nil {
		return nil, ErrUnreachable
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case err != nil:
		return nil, ErrUnreachable
	case resp.StatusCode == http.StatusOK:
		return body, nil
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, &RateLimited{Wait: retryAfter(resp.Header.Get("Retry-After"))}
	}
	return nil, refused(resp.StatusCode, body)
}

// retryAfter reads Retry-After in seconds; 5 minutes without one.
func retryAfter(h string) time.Duration {
	if s, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	return 5 * time.Minute
}

// refusal reads a refusal of the API, as Claude Code tells them apart: a
// verification, or an organisation that bars OAuth, is not mended by a
// renewal.
func refusal(status int, body []byte) error {
	var e struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Details struct {
				ErrorCode string `json:"error_code"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	switch {
	case status == http.StatusForbidden && e.Error.Type == "permission_error" && e.Error.Details.ErrorCode == "verification_required",
		strings.Contains(e.Error.Message, "OAuth authentication is currently not allowed for this organization"):
		return &Refused{Message: e.Error.Message}
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return ErrUnauthorized
	}
	return fmt.Errorf("Anthropic answered %d.", status)
}

// tokenRefusal reads a refusal of the token endpoint. Its body never goes
// into the error: it may echo a token.
func tokenRefusal(status int, body []byte) error {
	var e struct {
		Error json.RawMessage `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	var code string
	if json.Unmarshal(e.Error, &code) != nil {
		var o struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(e.Error, &o)
		code = o.Type
	}
	switch {
	case code == "invalid_grant" && bytes.Contains(body, []byte("account_on_hold")):
		return &Refused{Message: "Your Anthropic account is on hold."}
	case (status == http.StatusBadRequest || status == http.StatusUnauthorized) && code == "invalid_grant":
		return ErrInvalidGrant
	}
	return fmt.Errorf("Anthropic answered %d.", status)
}
