package account

import (
	"encoding/json"
	"errors"
	"time"
)

// Login is the claudeAiOauth object Claude Code keeps for a login: its
// tokens and what came with them. Only the fields the app reads are kept.
type Login struct {
	AccessToken           string   `json:"accessToken"`
	RefreshToken          string   `json:"refreshToken"`
	ExpiresAt             int64    `json:"expiresAt"` // epoch ms
	RefreshTokenExpiresAt int64    `json:"refreshTokenExpiresAt,omitempty"`
	Scopes                []string `json:"scopes"`
	SubscriptionType      string   `json:"subscriptionType,omitempty"`
	RateLimitTier         string   `json:"rateLimitTier,omitempty"`
	ClientID              string   `json:"clientId,omitempty"`
}

var errNoToken = errors.New("The saved login holds no access token.")

// parseLogin reads a login as Claude Code stores it: {"claudeAiOauth": {…}}.
func parseLogin(blob []byte) (Login, error) {
	var d struct {
		OAuth *Login `json:"claudeAiOauth"`
	}
	if json.Unmarshal(blob, &d) != nil || d.OAuth == nil || d.OAuth.AccessToken == "" {
		return Login{}, errNoToken
	}
	return *d.OAuth, nil
}

// Marshal writes l as Claude Code stores a login.
func (l Login) Marshal() []byte {
	b, _ := json.Marshal(struct {
		OAuth Login `json:"claudeAiOauth"`
	}{l})
	return b
}

// expiresBy reports whether the access token has lapsed by t.
func (l Login) expiresBy(t time.Time) bool { return !t.Before(time.UnixMilli(l.ExpiresAt)) }
