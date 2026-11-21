package account

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

// A Console API key account (claude-swap's add-token with a key): Claude
// Code's other way in, a managed key in the keychain item "Claude Code"
// (else ~/.claude.json's primaryApiKey), approved by its last 20
// characters in customApiKeyResponses. A key has no subscription limits:
// nothing is read for it.

const (
	keyUUID        = "apikey:"     // an account key's prefix for an API key account
	managedService = "Claude Code" // Claude Code's keychain item for a managed key
)

var (
	errNotAPIKey = errors.New("That isn't a Console API key: one starts sk-ant- (and not sk-ant-oat, a setup-token).")
	errKeyInUse  = errors.New("An API key is in use: it has no limits for auto-switch to go by.")
)

// IsAPIKey reports whether s is a Console API key rather than an OAuth
// token, as claude-swap's looks_like_api_key.
func IsAPIKey(s string) bool {
	return strings.HasPrefix(s, "sk-ant-") && !strings.HasPrefix(s, "sk-ant-oat") && !strings.ContainsAny(s, " \t\r\n\"")
}

func isEmail(s string) bool { return strings.Contains(s, "@") && !strings.ContainsAny(s, " \t\r\n\"") }

// AddAPIKey adds the Console API key key as an account labelled email, or
// api-key-N@token.local when email is "". Adding one already added
// replaces its key. Nothing is asked of Anthropic.
func (b *Book) AddAPIKey(key, email string) (Account, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return Account{}, err
	}
	a, err := b.addKeyLocked(key, email)
	if err != nil {
		return Account{}, err
	}
	return a, b.save()
}

func (b *Book) addKeyLocked(key, email string) (Account, error) {
	key = strings.TrimSpace(key)
	if !IsAPIKey(key) {
		return Account{}, errNotAPIKey
	}
	email = strings.TrimSpace(email)
	switch {
	case email == "":
		for n := 1; ; n++ {
			email = fmt.Sprintf("api-key-%d@token.local", n)
			if !slices.ContainsFunc(b.added, func(a Account) bool { return a.AccountUUID == keyUUID+email }) {
				break
			}
		}
	case !isEmail(email):
		return Account{}, errTokenEmail
	}
	if slices.ContainsFunc(b.added, func(x Account) bool { return x.Email == email && !x.APIKey }) {
		return Account{}, fmt.Errorf("%s is already an account signed in to with OAuth: give the API key a distinct email.", email)
	}
	a := Account{AccountUUID: keyUUID + email, Email: email, APIKey: true,
		OAuthAccount: json.RawMessage(fmt.Sprintf(`{"emailAddress":%q,"accountUuid":"","organizationUuid":null,"organizationName":null}`, email))}
	if err := b.Vault.Put(ParkService, a.Key(), []byte(key)); err != nil {
		return Account{}, err
	}
	if i := slices.IndexFunc(b.added, func(x Account) bool { return x.Key() == a.Key() }); i >= 0 {
		was := b.added[i]
		a.AutoOff, a.Line, a.Priority, a.Alias, a.Slot = was.AutoOff, was.Line, was.Priority, was.Alias, was.Slot
		b.added[i] = a
	} else {
		b.added = append(b.added, a)
	}
	a.Added = true
	return a, nil
}

// managedKey is the managed key Claude Code has, from its keychain item,
// else ~/.claude.json's primaryApiKey; "" for none.
func (b *Book) managedKey(conf []byte) (string, error) {
	k, err := b.Vault.Get(managedService, b.user())
	if err == nil {
		return strings.TrimSpace(string(k)), nil
	}
	if !errors.Is(err, ErrNotFound) {
		return "", err
	}
	var m struct {
		Key string `json:"primaryApiKey"`
	}
	_ = json.Unmarshal(conf, &m)
	return m.Key, nil
}

// keyOwner is the API key account whose key Claude Code has; "" for none.
func (b *Book) keyOwner(conf []byte) string {
	k, err := b.managedKey(conf)
	if err != nil || k == "" {
		return ""
	}
	for _, a := range b.added {
		if saved, err := b.Vault.Get(ParkService, a.Key()); a.APIKey && err == nil && string(saved) == k {
			return a.Key()
		}
	}
	return ""
}

// keyInUse is keyOwner of ~/.claude.json as it is.
func (b *Book) keyInUse() string {
	if b.Home == nil {
		return ""
	}
	conf, _ := os.ReadFile(b.Home.ClaudeJSON)
	return b.keyOwner(conf)
}

// approveKey records key as approved in conf, as Claude Code does on
// saving one (customApiKeyResponses.approved, its last 20 characters).
func approveKey(conf []byte, key string) ([]byte, error) {
	var m struct {
		R map[string]json.RawMessage `json:"customApiKeyResponses"`
	}
	_ = json.Unmarshal(conf, &m)
	if m.R == nil {
		m.R = map[string]json.RawMessage{}
	}
	var approved []string
	_ = json.Unmarshal(m.R["approved"], &approved)
	if tail := key[max(0, len(key)-20):]; !slices.Contains(approved, tail) {
		approved = append(approved, tail)
	}
	m.R["approved"], _ = json.Marshal(approved)
	if _, ok := m.R["rejected"]; !ok {
		m.R["rejected"] = json.RawMessage(`[]`)
	}
	r, err := json.Marshal(m.R)
	if err != nil {
		return nil, err
	}
	return spliceKey(conf, "customApiKeyResponses", r)
}

// withoutOAuth is blob with no claudeAiOauth, every other key (MCP
// sign-ins) kept: Claude Code then goes by the managed key.
func withoutOAuth(blob []byte) ([]byte, error) {
	m := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(blob)) > 0 && json.Unmarshal(blob, &m) != nil {
		return nil, errors.New("Claude Code's login is not a JSON object.")
	}
	delete(m, "claudeAiOauth")
	return json.Marshal(m)
}
