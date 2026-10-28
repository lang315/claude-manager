# Account Limits Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Limits page that shows how much of each Claude Code account's five-hour, weekly and per-model weekly limits is used, and when each resets. It covers the account in use and the accounts the user adds through Claude Code's own sign-in.

**Architecture:**
- **The `account` package (new):**
  - talks to Anthropic (`usage`, `profile`, `token`);
  - keeps added accounts' logins in the login keychain through `/usr/bin/security`;
  - runs `claude auth login` in a private folder;
  - keeps a book of accounts and their last readings in `~/Library/Application Support/Claude Manager/accounts.json`.
- **The view** gets a Limits page that reads the book off the main thread, as the other pages read the store.
- **The login in use** is read and never written, stored or renewed.

**Tech Stack:**
- Go 1.27.3, `net/http`, `os/exec`, `crypto/sha256`.
- MyGo UI (`ui.Meter`, `ui.Modal`, `ui.MenuButton`, `ui.AlertDialog`).
- No new module dependency.

**Spec:** `docs/superpowers/specs/2026-10-28-account-limits-design.md`

## Global Constraints

- **Never written or renewed:** `~/.claude`, `~/.claude.json`, and Claude Code's keychain item (`Claude Code-credentials…`). Claude Code's login is never renewed.
- **Tokens:**
  - Tokens live only in keychain items: `claude-manager-park` for added accounts, plus Claude Code's own item, which is only read.
  - A token never goes to a file, log, toast, error message or screenshot.
  - The token endpoint's answer body never goes into an error.
- **Requests:**
  - Requests go only to `https://api.anthropic.com` and `https://platform.claude.com`.
  - Timeout is 5 s.
  - `User-Agent: claude-manager/<version>`, where the version is `mygo.json`'s (`0.1.0`).
  - API requests also send `anthropic-beta: oauth-2025-04-20`.
- **Keychain:** reads and writes go only through `/usr/bin/security`. Secrets go on stdin (`security -i`), never on an argument line.
- **App folder:** `accounts.json` is 0600 in a 0700 folder, written atomically, and holds no secret.
- **Tests:**
  - No test touches the real keychain, the network or `~/.claude`.
  - Tests use `account.Memory`, `httptest`, and stand-in scripts for `security` and `claude`.
- **UI copy (English):**
  - **Limits page:** **Limits**, **Add Account…**, **Refresh**, **In use**, **Limit reached**, **Sign In Again**, **Remove…**.
  - **Add Account dialog:** **Copy Link**, **Open Browser**, **Submit Code**, **Try Again**, **Cancel**.
  - **Status messages:**
    - `Not signed in to Claude Code.`
    - `Keychain is locked.`
    - `Claude Code's sign-in has expired. Open Claude Code to renew it.`
    - `Sign-in expired.`
    - `Couldn't reach Anthropic.`
    - `Rate limited by Anthropic.`
- **Gate before every commit:** `gofmt -l .` prints nothing, `go vet ./...` exits 0, and `CGO_ENABLED=1 go test -race ./...` exits 0.
- **Commits** end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`, and never carry a session URL.

## Plan Decisions

These are rulings on points the spec leaves open. Each says what it costs if wrong.

- **Logins are kept as the fields the app reads** (`Login`), not as Claude Code's raw object.
  - Cost if wrong: a field Claude Code adds later is not kept. Nothing reads it.
- **One `Book` mutex serialises refreshes, additions and removals.** It is also the per-account renewal lock.
  - A refresh that comes during another waits for it, then runs. The 3-minute floor makes the second one cheap.
  - Cost if wrong: a forced Refresh after a tick waits up to one refresh (≤ 5 s per request).
- **The 5-minute refresh is `App.LimitsTick`**, which `main` calls on a ticker. It refreshes only while the page is shown and the app is active.
  - It does not use a `screens` bit: the root's watch never hints at limits.
- **A malformed usage answer shows in the block's error line**, like other errors, not as a toast.
- **The sign-in lives on `Book`** (`Book.SignIn`, with `Claude` and `SignInTimeout` fields), not on a separate type.
- **Meter colours:** a meter's normal colour is selection graphite. It is set through `c.SetTheme` around the `ui.Meter` call, so green stays the running lamp's.
  - Warning (from 80%) and Danger (from 95%) are MyGo's theme colours. They are recorded in DESIGN.md.
- **The sign-in's keychain item is looked up two ways:** under the folder's path as given, and under the path with symlinks resolved.

## Review Focus

These are failure modes the spec implies but its test list does not name. Each one gets a test in the task named.

1. **Claude Code names the sign-in's keychain item from the resolved path** (macOS's `/var` → `/private/var`, or a symlinked folder). The app must still find the login and delete the item. → Task 7, `TestSignInFindsTheItemOfTheResolvedPath`.
2. **`security -i` fails a command on stderr and still exits 0.** A renewed login would count as saved and then be lost. `Put` must fail. → Task 2, `TestKeychainPutFailsOnStderr`.
3. **claude wraps its link in terminal escapes** (bold and OSC 8 hyperlinks). The link the dialog copies and opens must be the bare URL. → Task 7, `TestSignInAddsTheAccount`, through the escaped link of the stand-in.
4. **A forced Refresh comes while a tick's refresh runs.** It must run once the first lands, still forced, not be dropped. → Task 8, `TestLimitsKeepsAForcedLoadAskedDuringOne`.
5. **An old sign-in's late events, after Cancel or Try Again,** must not fail or close the dialog of the new one. → Task 9, `TestLimitsIgnoresAnOldSignIn`.

---

### Task 1: Parse the usage answer

**Files:**
- Create: `account/limits.go`
- Test: `account/limits_test.go`

**Interfaces:**
- Produces:
  - `type Limit struct { Kind, Scope string; Percent float64; ResetsAt time.Time }`
  - `func (l Limit) Label() string`
  - `func ParseUsage(body []byte) ([]Limit, error)`
  - `var errNoLimits`

- [ ] **Step 1: Write the failing tests**

`account/limits_test.go`:

```go
package account

import (
	"errors"
	"testing"
	"time"
)

func TestParseUsageRows(t *testing.T) {
	body := `{"five_hour":{"utilization":1,"resets_at":"2026-09-20T22:20:00Z"},"limits":[
		{"kind":"session","group":"session","percent":62,"resets_at":"2026-09-20T22:20:00.095287+00:00","scope":null,"is_active":true},
		{"kind":"weekly_all","percent":48,"resets_at":"2026-09-27T02:00:00Z","scope":null},
		{"kind":"weekly_scoped","percent":0,"resets_at":"2026-09-27T02:00:00+00:00","scope":{"model":{"id":null,"display_name":"Fable"}}},
		{"kind":"weekly_surface","percent":7,"scope":{"surface":{"display_name":"Claude Code"}}}]}`
	got, err := ParseUsage([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		label   string
		percent float64
		resets  time.Time
	}{
		{"Session (5h)", 62, time.Date(2026, 9, 20, 22, 20, 0, 95287000, time.UTC)},
		{"Weekly", 48, time.Date(2026, 9, 27, 2, 0, 0, 0, time.UTC)},
		{"Weekly · Fable", 0, time.Date(2026, 9, 27, 2, 0, 0, 0, time.UTC)},
		{"weekly_surface · Claude Code", 7, time.Time{}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d limits, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Label() != w.label || got[i].Percent != w.percent || !got[i].ResetsAt.Equal(w.resets) {
			t.Errorf("limit %d = %q %v %v, want %q %v %v", i, got[i].Label(), got[i].Percent, got[i].ResetsAt, w.label, w.percent, w.resets)
		}
	}
}

func TestParseUsageSkipsBadRows(t *testing.T) {
	body := `{"limits":[{"kind":"session","percent":"x"},{"percent":5},{"kind":"weekly_all","percent":-1},
		{"kind":"weekly_all","percent":130,"resets_at":"soon"}]}`
	got, err := ParseUsage([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Percent != 100 || !got[0].ResetsAt.IsZero() {
		t.Fatalf("got %+v, want one weekly limit at 100%% with no reset", got)
	}
}

func TestParseUsageFallsBackToWindows(t *testing.T) {
	for _, body := range []string{
		`{"five_hour":{"utilization":62,"resets_at":"2026-09-20T22:20:00Z"},"seven_day":{"utilization":48,"resets_at":"2026-09-27T02:00:00Z"}}`,
		`{"five_hour":{"utilization":62,"resets_at":"2026-09-20T22:20:00Z"},"seven_day":{"utilization":48,"resets_at":"2026-09-27T02:00:00Z"},"limits":[{"kind":"session"}]}`,
	} {
		got, err := ParseUsage([]byte(body))
		if err != nil || len(got) != 2 || got[0].Label() != "Session (5h)" || got[0].Percent != 62 || got[1].Label() != "Weekly" || got[1].Percent != 48 {
			t.Errorf("%s: got %+v, %v", body, got, err)
		}
	}
}

func TestParseUsageWithoutLimits(t *testing.T) {
	for _, body := range []string{`{}`, `not json`, `{"limits":[]}`} {
		if _, err := ParseUsage([]byte(body)); !errors.Is(err, errNoLimits) {
			t.Errorf("%s: err = %v, want errNoLimits", body, err)
		}
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./account/ -run TestParseUsage`
Expected: FAIL to build: `undefined: ParseUsage` (the package has no other file yet).

- [ ] **Step 3: Write `account/limits.go`**

```go
// Package account reads how much of each Claude Code account's limits is
// used, from Anthropic: with the login Claude Code holds for the account in
// use, and with logins of its own for the accounts the user adds.
package account

import (
	"encoding/json"
	"errors"
	"math"
	"time"
)

// Limit is one of an account's limits, as Anthropic's usage answer gives it.
type Limit struct {
	Kind     string    `json:"kind"`              // session, weekly_all, weekly_scoped, or another
	Scope    string    `json:"scope,omitempty"`   // the model or surface a scoped limit is for
	Percent  float64   `json:"percent"`           // used, 0 to 100
	ResetsAt time.Time `json:"resetsAt,omitzero"` // zero where the answer gave none
}

// Label names the limit as the Limits page shows it.
func (l Limit) Label() string {
	name := l.Kind
	switch l.Kind {
	case "session", "five_hour":
		name = "Session (5h)"
	case "weekly_all", "seven_day", "weekly_scoped":
		name = "Weekly"
	}
	if l.Scope != "" {
		name += " · " + l.Scope
	}
	return name
}

var errNoLimits = errors.New("Anthropic's answer holds no limits.")

type scoped struct {
	DisplayName string `json:"display_name"`
}

type row struct {
	Kind     string   `json:"kind"`
	Percent  *float64 `json:"percent"`
	ResetsAt string   `json:"resets_at"`
	Scope    *struct {
		Model   *scoped `json:"model"`
		Surface *scoped `json:"surface"`
	} `json:"scope"`
}

type window struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    string   `json:"resets_at"`
}

// ParseUsage reads the answer of /api/oauth/usage: its limits rows or,
// where none can be read, its five_hour and seven_day windows. A row it
// cannot read is left out.
func ParseUsage(body []byte) ([]Limit, error) {
	var u struct {
		FiveHour *window          `json:"five_hour"`
		SevenDay *window          `json:"seven_day"`
		Limits   []json.RawMessage `json:"limits"`
	}
	if json.Unmarshal(body, &u) != nil {
		return nil, errNoLimits
	}
	var out []Limit
	for _, raw := range u.Limits {
		var r row
		if json.Unmarshal(raw, &r) != nil || r.Kind == "" || !valid(r.Percent) {
			continue
		}
		l := Limit{Kind: r.Kind, Percent: math.Min(*r.Percent, 100), ResetsAt: when(r.ResetsAt)}
		if s := r.Scope; s != nil {
			if s.Model != nil && s.Model.DisplayName != "" {
				l.Scope = s.Model.DisplayName
			} else if s.Surface != nil {
				l.Scope = s.Surface.DisplayName
			}
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		for _, w := range []struct {
			kind string
			w    *window
		}{{"five_hour", u.FiveHour}, {"seven_day", u.SevenDay}} {
			if w.w != nil && valid(w.w.Utilization) {
				out = append(out, Limit{Kind: w.kind, Percent: math.Min(*w.w.Utilization, 100), ResetsAt: when(w.w.ResetsAt)})
			}
		}
	}
	if len(out) == 0 {
		return nil, errNoLimits
	}
	return out, nil
}

func valid(p *float64) bool { return p != nil && *p >= 0 && !math.IsInf(*p, 0) }

// when reads an RFC 3339 time, with or without fractions; zero if it can't.
func when(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
```

- [ ] **Step 4: Run them to see them pass**

Run: `go test ./account/ -run TestParseUsage -v`
Expected: PASS for all four tests.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add account/limits.go account/limits_test.go
git commit -m "feat(account): parse Anthropic's usage answer

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: The keychain vault

**Files:**
- Create: `account/vault.go`
- Test: `account/vault_test.go`

**Interfaces:**
- Produces:
  - `type Vault interface { Get(service, acct string) ([]byte, error); Put(service, acct string, secret []byte) error; Delete(service, acct string) error }`
  - `var ErrNotFound, ErrLocked error`
  - `type Keychain struct { Bin string }`
  - `type Memory struct { Fail error; … }`, with `func NewMemory() *Memory` and `func (m *Memory) Len() int`

- [ ] **Step 1: Write the failing tests**

`account/vault_test.go`:

```go
package account

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryVault(t *testing.T) {
	m := NewMemory()
	if _, err := m.Get("s", "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get of nothing: %v", err)
	}
	if err := m.Put("s", "a", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if got, err := m.Get("s", ""); err != nil || string(got) != "x" {
		t.Fatalf("Get of any account = %q, %v", got, err)
	}
	if err := m.Delete("s", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get("s", "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete: %v", err)
	}
	if err := m.Delete("s", "a"); err != nil {
		t.Fatalf("Delete of nothing: %v", err)
	}
	m.Fail = errors.New("refused")
	if err := m.Put("s", "a", []byte("x")); err == nil {
		t.Fatal("Put worked with Fail set")
	}
}

// fakeSecurity stands in for /usr/bin/security: it records its arguments
// and stdin in its folder, then runs then.
func fakeSecurity(t *testing.T, then string) (Keychain, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "security")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + dir + "/args'\ncat > '" + dir + "/stdin'\n" + then + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return Keychain{Bin: bin}, dir
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestKeychainGet(t *testing.T) {
	k, dir := fakeSecurity(t, `printf '{"a":1}\n'`)
	got, err := k.Get("svc", "acct")
	if err != nil || string(got) != `{"a":1}` {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if args := readFile(t, dir+"/args"); args != "find-generic-password\n-s\nsvc\n-a\nacct\n-w\n" {
		t.Fatalf("args = %q", args)
	}
	k, dir = fakeSecurity(t, `printf 'x\n'`)
	if _, err := k.Get("svc", ""); err != nil {
		t.Fatal(err)
	}
	if args := readFile(t, dir+"/args"); args != "find-generic-password\n-s\nsvc\n-w\n" {
		t.Fatalf("args without an account = %q", args)
	}
}

func TestKeychainErrors(t *testing.T) {
	const missing = "echo 'security: SecKeychainSearchCopyNext: The specified item could not be found in the keychain.' >&2; exit 44"
	for _, c := range []struct {
		then string
		want error
	}{
		{missing, ErrNotFound},
		{"echo 'security: SecKeychainItemCopyContent: User interaction is not allowed.' >&2; exit 36", ErrLocked},
	} {
		k, _ := fakeSecurity(t, c.then)
		if _, err := k.Get("svc", ""); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.then, err, c.want)
		}
	}
	k, _ := fakeSecurity(t, missing)
	if err := k.Delete("svc", ""); err != nil {
		t.Fatalf("Delete of nothing: %v", err)
	}
}

func TestKeychainPutUsesStdin(t *testing.T) {
	k, dir := fakeSecurity(t, ":")
	if err := k.Put("claude-manager-park", "acc/org", []byte("secret")); err != nil {
		t.Fatal(err)
	}
	if args := readFile(t, dir+"/args"); args != "-i\n" {
		t.Fatalf("args = %q: the secret must not be on the command line", args)
	}
	want := "add-generic-password -U -s claude-manager-park -a acc/org -X 736563726574\n"
	if in := readFile(t, dir+"/stdin"); in != want {
		t.Fatalf("stdin = %q, want %q", in, want)
	}
}

// security -i reports a failed command on stderr and still exits 0.
func TestKeychainPutFailsOnStderr(t *testing.T) {
	k, _ := fakeSecurity(t, "echo 'security: SecKeychainItemCreateFromContent: The user name or passphrase you entered is not correct.' >&2")
	err := k.Put("claude-manager-park", "acc/org", []byte("secret"))
	if err == nil {
		t.Fatal("Put worked")
	}
	if strings.Contains(err.Error(), "736563726574") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("the error carries the secret: %v", err)
	}
}

func TestKeychainPutRefusesSpaces(t *testing.T) {
	k, _ := fakeSecurity(t, ":")
	if err := k.Put("two words", "a", []byte("x")); err == nil {
		t.Fatal("Put took a service with a space")
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./account/ -run 'TestMemoryVault|TestKeychain'`
Expected: FAIL to build: `undefined: NewMemory`, `undefined: Keychain`.

- [ ] **Step 3: Write `account/vault.go`**

```go
package account

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

var (
	// ErrNotFound says the keychain holds no such item.
	ErrNotFound = errors.New("Not in the keychain.")
	// ErrLocked says the keychain is locked and macOS can't ask to unlock it.
	ErrLocked = errors.New("Keychain is locked.")
)

// Vault holds secrets as generic passwords, by service and account. An
// empty account matches any: Claude Code files its login under the user's
// name.
type Vault interface {
	Get(service, acct string) ([]byte, error)
	Put(service, acct string, secret []byte) error
	// Delete removes the items; none is no error.
	Delete(service, acct string) error
}

// Keychain is the login keychain, through /usr/bin/security: the one
// program Claude Code's item trusts, and the one that creates the app's
// own items, so no read asks for a password.
type Keychain struct {
	Bin string // tests point it at a stand-in; "" is /usr/bin/security
}

func (k Keychain) Get(service, acct string) ([]byte, error) {
	out, err := k.run("the read", "", args("find-generic-password", service, acct, "-w")...)
	if err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(out, []byte("\n")), nil
}

// Put sends its command to security -i on stdin, so the secret is on no
// argument line another process could read.
func (k Keychain) Put(service, acct string, secret []byte) error {
	if strings.ContainsAny(service+acct, " \t\n\"'") {
		return fmt.Errorf("The keychain item %q %q can't be written.", service, acct)
	}
	cmd := fmt.Sprintf("add-generic-password -U -s %s -a %s -X %s\n", service, acct, hex.EncodeToString(secret))
	_, err := k.run("the write", cmd, "-i")
	return err
}

func (k Keychain) Delete(service, acct string) error {
	_, err := k.run("the removal", "", args("delete-generic-password", service, acct)...)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func args(cmd, service, acct string, more ...string) []string {
	a := []string{cmd, "-s", service}
	if acct != "" {
		a = append(a, "-a", acct)
	}
	return append(a, more...)
}

func (k Keychain) run(what, stdin string, arg ...string) ([]byte, error) {
	bin := k.Bin
	if bin == "" {
		bin = "/usr/bin/security"
	}
	cmd := exec.Command(bin, arg...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, msg bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &msg
	err := cmd.Run()
	text := msg.String()
	switch {
	case strings.Contains(text, "could not be found"):
		return nil, ErrNotFound
	case strings.Contains(text, "User interaction is not allowed"), strings.Contains(text, "locked"):
		return nil, ErrLocked
	case err != nil, stdin != "" && strings.TrimSpace(text) != "":
		// security -i reports a failed command on stderr and still exits 0.
		// Its words stay out: they can echo the command, secret and all.
		return nil, fmt.Errorf("The keychain refused %s.", what)
	}
	return out.Bytes(), nil
}

// Memory is a Vault in memory, for tests. Fail, when set, fails every Put.
type Memory struct {
	Fail  error
	mu    sync.Mutex
	items map[[2]string][]byte
}

func NewMemory() *Memory { return &Memory{items: map[[2]string][]byte{}} }

func (m *Memory) Get(service, acct string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range m.items {
		if k[0] == service && (acct == "" || k[1] == acct) {
			return bytes.Clone(v), nil
		}
	}
	return nil, ErrNotFound
}

func (m *Memory) Put(service, acct string, secret []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Fail != nil {
		return m.Fail
	}
	m.items[[2]string{service, acct}] = bytes.Clone(secret)
	return nil
}

func (m *Memory) Delete(service, acct string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.items {
		if k[0] == service && (acct == "" || k[1] == acct) {
			delete(m.items, k)
		}
	}
	return nil
}

// Len counts the items held.
func (m *Memory) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.items)
}
```

- [ ] **Step 4: Run them to see them pass**

Run: `go test ./account/ -run 'TestMemoryVault|TestKeychain' -v`
Expected: PASS for all six tests.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add account/vault.go account/vault_test.go
git commit -m "feat(account): keychain vault through security on stdin

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Logins, and the login in use

**Files:**
- Create: `account/login.go`, `account/live.go`
- Test: `account/live_test.go`

**Interfaces:**
- Consumes: `Vault`, `ErrNotFound`, `ErrLocked`, `NewMemory` (Task 2).
- Produces:
  - `type Login struct { AccessToken, RefreshToken string; ExpiresAt, RefreshTokenExpiresAt int64; Scopes []string; SubscriptionType, RateLimitTier, ClientID string }`
  - `func (l Login) Marshal() []byte`
  - `func parseLogin([]byte) (Login, error)`
  - `func (l Login) expiresBy(time.Time) bool`
  - `func liveService(configDir string) string`
  - `func ReadLive(v Vault, configDir, root string) (Login, error)`
  - `var ErrNotSignedIn`

- [ ] **Step 1: Write the failing tests**

`account/live_test.go`:

```go
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
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./account/ -run 'TestLiveService|TestReadLive|TestLogin'`
Expected: FAIL to build: `undefined: liveService`, `undefined: ReadLive`, `undefined: Login`.

- [ ] **Step 3: Write `account/login.go` and `account/live.go`**

`account/login.go`:

```go
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
```

`account/live.go`:

```go
package account

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrNotSignedIn says Claude Code holds no login.
var ErrNotSignedIn = errors.New("Not signed in to Claude Code.")

// liveService is the keychain service Claude Code keeps its login under
// for a config directory: configDir is $CLAUDE_CONFIG_DIR as set, "" for
// the default one.
func liveService(configDir string) string {
	if configDir == "" {
		return "Claude Code-credentials"
	}
	sum := sha256.Sum256([]byte(configDir))
	return "Claude Code-credentials-" + hex.EncodeToString(sum[:4])
}

// ReadLive reads the login Claude Code uses: from the keychain, else from
// root's .credentials.json, where Claude Code writes it when the keychain
// refuses it. It only reads: the login is Claude Code's to renew.
func ReadLive(v Vault, configDir, root string) (Login, error) {
	blob, err := v.Get(liveService(configDir), "")
	if errors.Is(err, ErrNotFound) {
		blob, err = os.ReadFile(filepath.Join(root, ".credentials.json"))
		if errors.Is(err, fs.ErrNotExist) {
			return Login{}, ErrNotSignedIn
		}
	}
	if err != nil {
		return Login{}, err
	}
	return parseLogin(blob)
}
```

- [ ] **Step 4: Run them to see them pass**

Run: `go test ./account/ -run 'TestLiveService|TestReadLive|TestLogin' -v`
Expected: PASS for all nine tests.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add account/login.go account/live.go account/live_test.go
git commit -m "feat(account): read the login Claude Code uses, never write it

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Ask Anthropic

**Files:**
- Create: `account/api.go`
- Create: `account/helpers_test.go` (the shared clock and `login` helper; Task 5 adds to it)
- Test: `account/api_test.go`

**Interfaces:**
- Consumes: `Limit`, `ParseUsage` (Task 1); `Login` (Task 3).
- Produces:
  - `type API struct { Base, AuthBase, Agent string; Client *http.Client }`
  - `func NewAPI(version string) *API`
  - `func (a *API) Usage(ctx, token string) ([]Limit, error)`
  - `func (a *API) Profile(ctx, token string) (Profile, error)`
  - `func (a *API) Renew(ctx, l Login, now time.Time) (Login, error)`
  - `type Profile struct { AccountUUID, Email, OrgUUID, OrgName string }`, with `func (p Profile) Key() string`
  - `ErrUnauthorized`, `ErrInvalidGrant`, `ErrUnreachable`, `*Refused{Message}`, `*RateLimited{Wait}`

- [ ] **Step 1: Write the shared helpers and the failing tests**

`account/helpers_test.go`:

```go
package account

import "time"

// t0 is the tests' clock.
var t0 = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

// login is a login whose access token at lapses at expires.
func login(at, rt string, expires time.Time) Login {
	return Login{AccessToken: at, RefreshToken: rt, ExpiresAt: expires.UnixMilli(),
		Scopes: []string{"user:inference", "user:profile"}, SubscriptionType: "max"}
}
```

`account/api_test.go`:

```go
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
			refusedWith("OAuth authentication is currently not allowed for this organization.")},
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
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./account/ -run 'TestUsageRequest|TestProfile|TestRefusals|TestRateLimited|TestUnreachable|TestRenew'`
Expected: FAIL to build: `undefined: NewAPI`, `undefined: Profile`, `undefined: claudeCodeClient`.

- [ ] **Step 3: Write `account/api.go`**

```go
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
			UUID  string `json:"uuid"`
			Email string `json:"email_address"`
		} `json:"account"`
		Organization struct {
			UUID string `json:"uuid"`
			Name string `json:"name"`
		} `json:"organization"`
	}
	if json.Unmarshal(body, &p) != nil || p.Account.UUID == "" || p.Organization.UUID == "" {
		return Profile{}, errors.New("Anthropic's profile names no account.")
	}
	return Profile{AccountUUID: p.Account.UUID, Email: p.Account.Email, OrgUUID: p.Organization.UUID, OrgName: p.Organization.Name}, nil
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
```

- [ ] **Step 4: Run them to see them pass**

Run: `go test ./account/ -run 'TestUsageRequest|TestProfile|TestRefusals|TestRateLimited|TestUnreachable|TestRenew' -v`
Expected: PASS for all nine tests.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add account/api.go account/api_test.go account/helpers_test.go
git commit -m "feat(account): ask Anthropic for usage, profile and fresh tokens

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: The book of accounts

**Files:**
- Create: `account/book.go`
- Modify: `account/helpers_test.go` (add the fake Anthropic and `newBook`)
- Test: `account/book_test.go`

**Interfaces:**
- Consumes: `Vault`, `Memory` (Task 2); `Login`, `parseLogin` (Task 3); `API`, `Profile` (Task 4).
- Produces:
  - `const ParkService = "claude-manager-park"`
  - `type Account struct { AccountUUID, OrgUUID, Email, OrgName, Plan string; GoodUntil time.Time; Limits []Limit; ReadAt time.Time; Err string; WaitUntil time.Time; InUse, Added bool }`, with `func (a Account) Key() string`
  - `type Book struct { Dir string; Vault Vault; API *API; Live func() (Login, error); Now func() time.Time; Claude func() (string, error); SignInTimeout time.Duration; … }`
  - `func (b *Book) Blocks() ([]Account, error)` and `func (b *Book) Remove(key string) error`
  - `func (b *Book) keep(l Login, p Profile) (Account, error)` and `func (b *Book) load() error`, `save() error`, `blocks() []Account`
- Test helpers for Tasks 6–7:
  - `newAnthropic() *anthropic`, with fields `who`, `usage`, `status` and `renewed`, and `took() []string`;
  - `newBook(t, f) (*Book, *Memory, *time.Time, *Login)`;
  - `blockOf(t, []Account, email) Account`;
  - the profiles `alice`, `bob`, `carol`.

- [ ] **Step 1: Add the fake Anthropic to `account/helpers_test.go`**

Replace the file with:

```go
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
	mu      sync.Mutex
	who     map[string]Profile
	usage   map[string]int
	status  map[string]int
	renewed map[string]string
	calls   []string // "profile <token>", "usage <token>", "renew <refresh token>"
}

func newAnthropic() *anthropic {
	return &anthropic{who: map[string]Profile{}, usage: map[string]int{}, status: map[string]int{}, renewed: map[string]string{}}
}

func (f *anthropic) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	switch r.URL.Path {
	case "/api/oauth/profile":
		f.calls = append(f.calls, "profile "+tok)
		p, ok := f.who[tok]
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprintf(w, `{"account":{"uuid":%q,"email_address":%q},"organization":{"uuid":%q,"name":%q}}`, p.AccountUUID, p.Email, p.OrgUUID, p.OrgName)
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
		fmt.Fprintf(w, `{"limits":[{"kind":"session","percent":%d,"resets_at":"2026-10-26T15:00:00Z"}]}`, pct)
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
```

- [ ] **Step 2: Write the failing tests**

`account/book_test.go`:

```go
package account

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBookKeepSavesNoSecret(t *testing.T) {
	b, v, _, _ := newBook(t, newAnthropic())
	l := login("bob-at", "bob-rt", t0.Add(time.Hour))
	l.RefreshTokenExpiresAt = t0.Add(30 * 24 * time.Hour).UnixMilli()
	a, err := b.keep(l, bob)
	if err != nil {
		t.Fatal(err)
	}
	if a.Email != "bob@example.com" || !a.Added || a.Plan != "max" || !a.GoodUntil.Equal(t0.Add(30*24*time.Hour)) {
		t.Fatalf("kept %+v", a)
	}
	if blob, err := v.Get(ParkService, "bob/org-b"); err != nil || !strings.Contains(string(blob), "bob-rt") {
		t.Fatalf("the vault holds %q, %v", blob, err)
	}
	path := filepath.Join(b.Dir, "accounts.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if s := string(data); !strings.Contains(s, "bob@example.com") || strings.Contains(s, "bob-at") || strings.Contains(s, "bob-rt") {
		t.Fatalf("accounts.json = %s", s)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("accounts.json mode = %v, %v", fi.Mode(), err)
	}
	if fi, err := os.Stat(b.Dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("folder mode = %v, %v", fi.Mode(), err)
	}
}

func TestBookReadsWhatItSaved(t *testing.T) {
	b, v, _, _ := newBook(t, newAnthropic())
	if _, err := b.keep(login("bob-at", "bob-rt", t0), bob); err != nil {
		t.Fatal(err)
	}
	again := &Book{Dir: b.Dir, Vault: v}
	got, err := again.Blocks()
	if err != nil || len(got) != 1 || got[0].Email != "bob@example.com" || !got[0].Added {
		t.Fatalf("Blocks = %+v, %v", got, err)
	}
}

func TestBookKeepReplacesTheSameAccount(t *testing.T) {
	b, v, _, _ := newBook(t, newAnthropic())
	for _, at := range []string{"at-1", "at-2"} {
		if _, err := b.keep(login(at, "rt", t0), bob); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := b.Blocks()
	if len(got) != 1 {
		t.Fatalf("Blocks = %+v", got)
	}
	if blob, _ := v.Get(ParkService, "bob/org-b"); !strings.Contains(string(blob), "at-2") {
		t.Fatalf("the vault holds %s", blob)
	}
}

func TestBookRemove(t *testing.T) {
	b, v, _, _ := newBook(t, newAnthropic())
	if _, err := b.keep(login("bob-at", "bob-rt", t0), bob); err != nil {
		t.Fatal(err)
	}
	if err := b.Remove("bob/org-b"); err != nil {
		t.Fatal(err)
	}
	if got, _ := b.Blocks(); len(got) != 0 || v.Len() != 0 {
		t.Fatalf("after Remove: %+v, %d items", got, v.Len())
	}
}

func TestBookUnreadableFile(t *testing.T) {
	b, _, _, _ := newBook(t, newAnthropic())
	if err := os.WriteFile(filepath.Join(b.Dir, "accounts.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := b.Blocks(); err == nil || len(got) != 0 {
		t.Fatalf("Blocks = %+v, %v", got, err)
	}
}
```

- [ ] **Step 3: Run them to see them fail**

Run: `go test ./account/ -run TestBook`
Expected: FAIL to build: `undefined: Book`, `undefined: ParkService`.

- [ ] **Step 4: Write `account/book.go`**

```go
package account

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// ParkService names the keychain items that hold added accounts' logins.
const ParkService = "claude-manager-park"

// Account is an account the Limits page shows, with its last reading. It
// holds no secret: the book saves it as is.
type Account struct {
	AccountUUID string    `json:"accountUuid,omitempty"`
	OrgUUID     string    `json:"orgUuid,omitempty"`
	Email       string    `json:"email,omitempty"`
	OrgName     string    `json:"orgName,omitempty"`
	Plan        string    `json:"plan,omitempty"`      // the login's subscriptionType: pro, max, team…
	GoodUntil   time.Time `json:"goodUntil,omitzero"`  // when an added account's sign-in lapses
	Limits      []Limit   `json:"limits,omitempty"`    // as last read
	ReadAt      time.Time `json:"readAt,omitzero"`     // when Limits were read
	Err         string    `json:"err,omitempty"`       // why the last ask failed; "" once one works
	WaitUntil   time.Time `json:"waitUntil,omitzero"`  // Anthropic asked to wait until then
	InUse       bool      `json:"-"`                   // Claude Code is signed in to it
	Added       bool      `json:"-"`                   // the app holds a login of its own for it
}

// Key names the account by Anthropic's account and organisation.
func (a Account) Key() string { return a.AccountUUID + "/" + a.OrgUUID }

// Book is the accounts the Limits page shows: the one Claude Code is
// signed in to, and those added. One mutex runs its work one at a time, so
// no login is renewed twice at once.
type Book struct {
	Dir   string // the app's folder: accounts.json and sign-in folders
	Vault Vault
	API   *API
	Live  func() (Login, error) // reads the login Claude Code uses
	Now   func() time.Time
	// Claude finds the claude binary; SignInTimeout bounds a sign-in, 10
	// minutes when zero.
	Claude        func() (string, error)
	SignInTimeout time.Duration

	mu     sync.Mutex
	loaded bool
	added  []Account
	inUse  *Account             // the account in use, when it is not an added one
	nobody *Account             // stands in for it when the login in use can't be read
	who    map[[32]byte]Profile // an access token's SHA-256: whose it is
}

type saved struct {
	Accounts []Account `json:"accounts"`
	InUse    *Account  `json:"inUse,omitempty"`
}

func (b *Book) path() string { return filepath.Join(b.Dir, "accounts.json") }

// load reads accounts.json once.
func (b *Book) load() error {
	if b.loaded {
		return nil
	}
	b.loaded = true
	data, err := os.ReadFile(b.path())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	var s saved
	if err == nil {
		err = json.Unmarshal(data, &s)
	}
	if err != nil {
		return fmt.Errorf("accounts.json could not be read: %w", err)
	}
	b.added, b.inUse = s.Accounts, s.InUse
	return nil
}

// save writes accounts.json atomically, private to the user.
func (b *Book) save() error {
	if err := os.MkdirAll(b.Dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(b.Dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(saved{Accounts: b.added, InUse: b.inUse}, "", "  ")
	if err != nil {
		return err
	}
	tmp := b.path() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, b.path())
}

// Blocks returns the accounts as last read: the one in use first, then the
// added ones by email. The first call reads accounts.json.
func (b *Book) Blocks() ([]Account, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	err := b.load()
	return b.blocks(), err
}

func (b *Book) blocks() []Account {
	var out []Account
	switch {
	case b.inUse != nil:
		a := *b.inUse
		a.InUse = true
		out = append(out, a)
	case b.nobody != nil:
		out = append(out, *b.nobody)
	}
	for _, a := range b.added {
		a.Added = true
		out = append(out, a)
	}
	for i := range out {
		out[i].Limits = slices.Clone(out[i].Limits)
	}
	slices.SortStableFunc(out, func(x, y Account) int {
		if x.InUse != y.InUse {
			if x.InUse {
				return -1
			}
			return 1
		}
		return strings.Compare(x.Email, y.Email)
	})
	return out
}

// keep saves l as the login of the account p names, replacing any it had,
// and lists the account. A sign-in again keeps its last numbers.
func (b *Book) keep(l Login, p Profile) (Account, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return Account{}, err
	}
	if err := b.Vault.Put(ParkService, p.Key(), l.Marshal()); err != nil {
		return Account{}, err
	}
	a := Account{AccountUUID: p.AccountUUID, OrgUUID: p.OrgUUID, Email: p.Email, OrgName: p.OrgName, Plan: l.SubscriptionType}
	if l.RefreshTokenExpiresAt > 0 {
		a.GoodUntil = time.UnixMilli(l.RefreshTokenExpiresAt)
	}
	if i := slices.IndexFunc(b.added, func(x Account) bool { return x.Key() == a.Key() }); i >= 0 {
		a.Limits, a.ReadAt = b.added[i].Limits, b.added[i].ReadAt
		b.added[i] = a
	} else {
		b.added = append(b.added, a)
	}
	a.Added = true
	return a, b.save()
}

// Remove deletes an added account's login from the keychain and drops the
// account. The login is not revoked: it stays valid at Anthropic until it
// expires.
func (b *Book) Remove(key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return err
	}
	if err := b.Vault.Delete(ParkService, key); err != nil {
		return err
	}
	b.added = slices.DeleteFunc(b.added, func(a Account) bool { return a.Key() == key })
	return b.save()
}
```

- [ ] **Step 5: Run them to see them pass**

Run: `go test ./account/ -run TestBook -v`
Expected: PASS for all five tests.

- [ ] **Step 6: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add account/book.go account/book_test.go account/helpers_test.go
git commit -m "feat(account): a book of accounts, saved without secrets

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Refresh

**Files:**
- Create: `account/refresh.go`
- Test: `account/refresh_test.go`

**Interfaces:**
- Consumes: everything in Tasks 1–5.
- Produces:
  - `func (b *Book) Refresh(ctx context.Context, force bool) ([]Account, error)`
  - `const LiveExpired = "Claude Code's sign-in has expired. Open Claude Code to renew it."`

- [ ] **Step 1: Write the failing tests**

`account/refresh_test.go`:

```go
package account

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"
)

var ctx = context.Background()

// aliceInUse is a fake on which alice is signed in to Claude Code with
// token "live", at 62% of her session limit.
func aliceInUse(t *testing.T) (*Book, *anthropic, *time.Time, *Login) {
	f := newAnthropic()
	f.who["live"], f.usage["live"] = alice, 62
	b, _, now, live := newBook(t, f)
	*live = login("live", "live-rt", t0.Add(time.Hour))
	return b, f, now, live
}

func TestRefreshAccountInUse(t *testing.T) {
	b, f, _, _ := aliceInUse(t)
	got, err := b.Refresh(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].InUse || got[0].Added || got[0].Email != "alice@example.com" || got[0].Plan != "max" {
		t.Fatalf("got %+v", got)
	}
	if a := got[0]; len(a.Limits) != 1 || a.Limits[0].Percent != 62 || !a.ReadAt.Equal(t0) || a.Err != "" {
		t.Fatalf("reading %+v", a)
	}
	if c := f.took(); !slices.Equal(c, []string{"profile live", "usage live"}) {
		t.Fatalf("calls %v", c)
	}
}

func TestRefreshAsksWhoseOncePerToken(t *testing.T) {
	b, f, _, _ := aliceInUse(t)
	b.Refresh(ctx, true)
	b.Refresh(ctx, true)
	if c := f.took(); !slices.Equal(c, []string{"profile live", "usage live", "usage live"}) {
		t.Fatalf("calls %v", c)
	}
}

func TestRefreshSendsNothingForALapsedLogin(t *testing.T) {
	b, f, _, live := aliceInUse(t)
	live.ExpiresAt = t0.UnixMilli()
	got, _ := b.Refresh(ctx, true)
	if len(got) != 1 || !got[0].InUse || got[0].Err != LiveExpired || got[0].Email != "" {
		t.Fatalf("got %+v", got)
	}
	if c := f.took(); len(c) != 0 {
		t.Fatalf("calls %v", c)
	}
}

func TestRefreshNeverRenewsTheLoginInUse(t *testing.T) {
	b, f, _, _ := aliceInUse(t)
	delete(f.usage, "live") // refused: 401
	f.renewed["live-rt"] = "fresh"
	got, _ := b.Refresh(ctx, true)
	if got[0].Err != LiveExpired {
		t.Fatalf("got %+v", got)
	}
	if c := f.took(); !slices.Equal(c, []string{"profile live", "usage live"}) {
		t.Fatalf("calls %v: the login in use is Claude Code's to renew", c)
	}
}

func TestRefreshNotSignedIn(t *testing.T) {
	b, _, _, _ := newBook(t, newAnthropic())
	got, err := b.Refresh(ctx, false)
	if err != nil || len(got) != 1 || !got[0].InUse || got[0].Err != ErrNotSignedIn.Error() {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestRefreshKeepsTheInUseNumbersWhenItsLoginLapses(t *testing.T) {
	b, _, now, live := aliceInUse(t)
	b.Refresh(ctx, false)
	*now = t0.Add(10 * time.Minute)
	live.ExpiresAt = now.UnixMilli()
	got, _ := b.Refresh(ctx, false)
	a := blockOf(t, got, "alice@example.com")
	if a.Err != LiveExpired || len(a.Limits) != 1 || a.Limits[0].Percent != 62 {
		t.Fatalf("got %+v", a)
	}
}

func TestRefreshRenewsAnAddedLoginFirst(t *testing.T) {
	f := newAnthropic()
	f.usage["bob-2"], f.renewed["bob-rt"] = 30, "bob-2"
	b, v, _, _ := newBook(t, f)
	if _, err := b.keep(login("bob-1", "bob-rt", t0.Add(time.Minute)), bob); err != nil { // lapses within 5 minutes
		t.Fatal(err)
	}
	got, _ := b.Refresh(ctx, false)
	if c := f.took(); !slices.Equal(c, []string{"renew bob-rt", "usage bob-2"}) {
		t.Fatalf("calls %v", c)
	}
	blob, _ := v.Get(ParkService, "bob/org-b")
	l, _ := parseLogin(blob)
	if l.AccessToken != "bob-2" || l.RefreshToken != "bob-rt+" || l.ExpiresAt != t0.Add(8*time.Hour).UnixMilli() {
		t.Fatalf("saved %+v", l)
	}
	a := blockOf(t, got, "bob@example.com")
	if len(a.Limits) != 1 || a.Limits[0].Percent != 30 || !a.GoodUntil.Equal(t0.Add(30*24*time.Hour)) || !a.Added {
		t.Fatalf("got %+v", a)
	}
}

func TestRefreshLosesALoginItCannotSave(t *testing.T) {
	f := newAnthropic()
	f.usage["bob-2"], f.renewed["bob-rt"] = 30, "bob-2"
	b, v, _, _ := newBook(t, f)
	if _, err := b.keep(login("bob-1", "bob-rt", t0.Add(time.Minute)), bob); err != nil {
		t.Fatal(err)
	}
	v.Fail = errors.New("the keychain is locked")
	got, _ := b.Refresh(ctx, false)
	if a := blockOf(t, got, "bob@example.com"); a.Err != ErrInvalidGrant.Error() || len(a.Limits) != 0 {
		t.Fatalf("got %+v", a)
	}
	if c := f.took(); !slices.Equal(c, []string{"renew bob-rt"}) {
		t.Fatalf("calls %v: an unsaved login must not be used", c)
	}
}

func TestRefreshSignInExpired(t *testing.T) {
	b, _, _, _ := newBook(t, newAnthropic())
	if _, err := b.keep(login("bob-1", "bob-rt", t0), bob); err != nil {
		t.Fatal(err)
	}
	got, _ := b.Refresh(ctx, false)
	if a := blockOf(t, got, "bob@example.com"); a.Err != "Sign-in expired." {
		t.Fatalf("got %+v", a)
	}
}

func TestRefreshRenewsOnceWhenRefused(t *testing.T) {
	f := newAnthropic()
	f.usage["bob-2"], f.renewed["bob-rt"] = 30, "bob-2" // bob-1 is refused
	b, _, _, _ := newBook(t, f)
	if _, err := b.keep(login("bob-1", "bob-rt", t0.Add(time.Hour)), bob); err != nil {
		t.Fatal(err)
	}
	got, _ := b.Refresh(ctx, false)
	if c := f.took(); !slices.Equal(c, []string{"usage bob-1", "renew bob-rt", "usage bob-2"}) {
		t.Fatalf("calls %v", c)
	}
	if a := blockOf(t, got, "bob@example.com"); a.Err != "" || a.Limits[0].Percent != 30 {
		t.Fatalf("got %+v", a)
	}
}

func TestRefreshShowsAnAddedAccountInUseOnce(t *testing.T) {
	b, f, _, _ := aliceInUse(t)
	f.usage["alice-own"] = 1
	if _, err := b.keep(login("alice-own", "alice-rt", t0.Add(time.Hour)), alice); err != nil {
		t.Fatal(err)
	}
	got, _ := b.Refresh(ctx, false)
	if len(got) != 1 || !got[0].InUse || !got[0].Added || got[0].Limits[0].Percent != 62 {
		t.Fatalf("got %+v", got)
	}
	if c := f.took(); !slices.Equal(c, []string{"profile live", "usage live"}) {
		t.Fatalf("calls %v: the account in use is read with the login in use", c)
	}
}

func TestRefreshFloor(t *testing.T) {
	b, f, now, _ := aliceInUse(t)
	b.Refresh(ctx, false)
	f.took()
	*now = t0.Add(2 * time.Minute)
	b.Refresh(ctx, false)
	if c := f.took(); len(c) != 0 {
		t.Fatalf("read again within 3 minutes: %v", c)
	}
	b.Refresh(ctx, true)
	if c := f.took(); !slices.Equal(c, []string{"usage live"}) {
		t.Fatalf("a forced refresh: %v", c)
	}
	*now = t0.Add(5 * time.Minute)
	b.Refresh(ctx, false)
	if c := f.took(); !slices.Equal(c, []string{"usage live"}) {
		t.Fatalf("3 minutes on: %v", c)
	}
}

func TestRefreshWaitsWhenRateLimited(t *testing.T) {
	b, f, now, _ := aliceInUse(t)
	f.status["live"] = http.StatusTooManyRequests // Retry-After: 600
	got, _ := b.Refresh(ctx, false)
	if got[0].Err != "Rate limited by Anthropic." {
		t.Fatalf("got %+v", got)
	}
	f.took()
	*now = t0.Add(5 * time.Minute)
	b.Refresh(ctx, true)
	if c := f.took(); len(c) != 0 {
		t.Fatalf("asked within the wait, forced: %v", c)
	}
	delete(f.status, "live")
	*now = t0.Add(11 * time.Minute)
	got, _ = b.Refresh(ctx, false)
	if c := f.took(); !slices.Equal(c, []string{"usage live"}) || got[0].Err != "" {
		t.Fatalf("after the wait: %v, %+v", c, got)
	}
}

func TestRefreshKeepsNumbersOnError(t *testing.T) {
	b, f, _, _ := aliceInUse(t)
	b.Refresh(ctx, false)
	f.status["live"] = http.StatusInternalServerError
	got, _ := b.Refresh(ctx, true)
	if a := got[0]; a.Err != "Anthropic answered 500." || len(a.Limits) != 1 || a.Limits[0].Percent != 62 || !a.ReadAt.Equal(t0) {
		t.Fatalf("got %+v", a)
	}
}

func TestRefreshOrder(t *testing.T) {
	b, f, _, _ := aliceInUse(t)
	for _, p := range []Profile{carol, bob} {
		f.usage[p.AccountUUID] = 1
		if _, err := b.keep(login(p.AccountUUID, "rt", t0.Add(time.Hour)), p); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := b.Refresh(ctx, false)
	var emails []string
	for _, a := range got {
		emails = append(emails, a.Email)
	}
	if !slices.Equal(emails, []string{"alice@example.com", "bob@example.com", "carol@example.com"}) {
		t.Fatalf("order %v", emails)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./account/ -run TestRefresh`
Expected: FAIL to build: `b.Refresh undefined`, `undefined: LiveExpired`.

- [ ] **Step 3: Write `account/refresh.go`**

```go
package account

import (
	"context"
	"crypto/sha256"
	"errors"
	"slices"
	"sync"
	"time"
)

// LiveExpired is the status of the account in use when Claude Code's login
// has lapsed: the app never renews it; Claude Code does, as it runs.
const LiveExpired = "Claude Code's sign-in has expired. Open Claude Code to renew it."

const (
	floor      = 3 * time.Minute // an account is read at most this often, unless forced
	renewAhead = 5 * time.Minute // an added login is renewed this long before it lapses
)

var errLiveExpired = errors.New(LiveExpired)

// Refresh asks Anthropic for the limits of the account in use and of each
// added account, and saves what it learns. An account read in the last 3
// minutes is skipped unless force; one Anthropic asked to wait is skipped
// until then, force or not. An account's failure is in its Err; the error
// is the book's own.
func (b *Book) Refresh(ctx context.Context, force bool) ([]Account, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return b.blocks(), err
	}
	now := b.Now()
	live, who, liveErr := b.identify(ctx, now)

	type job struct {
		acct  *Account
		token string // the login in use's; "" for an added account's own login
	}
	var jobs []job
	for i := range b.added {
		a := &b.added[i]
		a.InUse = who != nil && a.Key() == who.Key()
		token := ""
		if a.InUse {
			token = live.AccessToken
		}
		jobs = append(jobs, job{a, token})
	}
	b.nobody = nil
	switch {
	case who == nil && b.inUse != nil:
		b.inUse.Err = liveErr.Error()
	case who == nil:
		b.nobody = &Account{InUse: true, Err: liveErr.Error()}
	case slices.ContainsFunc(b.added, func(a Account) bool { return a.InUse }):
		b.inUse = nil
	default:
		if b.inUse == nil || b.inUse.Key() != who.Key() {
			b.inUse = &Account{}
		}
		u := b.inUse
		u.AccountUUID, u.OrgUUID, u.Email, u.OrgName, u.Plan = who.AccountUUID, who.OrgUUID, who.Email, who.OrgName, live.SubscriptionType
		jobs = append(jobs, job{u, live.AccessToken})
	}

	var wg sync.WaitGroup
	for _, j := range jobs {
		a := j.acct
		if now.Before(a.WaitUntil) || !force && !a.ReadAt.IsZero() && now.Sub(a.ReadAt) < floor {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.read(ctx, a, j.token, now)
		}()
	}
	wg.Wait()
	return b.blocks(), b.save()
}

// identify reads the login in use and asks whose it is, once per token.
func (b *Book) identify(ctx context.Context, now time.Time) (Login, *Profile, error) {
	l, err := b.Live()
	if err != nil {
		return l, nil, err
	}
	if l.expiresBy(now) {
		return l, nil, errLiveExpired
	}
	k := sha256.Sum256([]byte(l.AccessToken))
	if p, ok := b.who[k]; ok {
		return l, &p, nil
	}
	p, err := b.API.Profile(ctx, l.AccessToken)
	if errors.Is(err, ErrUnauthorized) {
		err = errLiveExpired
	}
	if err != nil {
		return l, nil, err
	}
	if b.who == nil {
		b.who = map[[32]byte]Profile{}
	}
	b.who[k] = p
	return l, &p, nil
}

// read asks for one account's limits: with token, the login in use's, or
// else with the account's own login.
func (b *Book) read(ctx context.Context, a *Account, token string, now time.Time) {
	var limits []Limit
	var err error
	if token != "" {
		limits, err = b.API.Usage(ctx, token)
		if errors.Is(err, ErrUnauthorized) {
			err = errLiveExpired // Claude Code's to renew, never the app's
		}
	} else {
		limits, err = b.readOwn(ctx, a, now)
	}
	if err != nil {
		a.Err = err.Error()
		var rl *RateLimited
		if errors.As(err, &rl) {
			a.WaitUntil = now.Add(rl.Wait)
		}
		return
	}
	a.Limits, a.ReadAt, a.Err, a.WaitUntil = limits, now, "", time.Time{}
}

// readOwn reads an added account's limits with its own login: renewed
// first when it is about to lapse, and once more when Anthropic refuses it.
func (b *Book) readOwn(ctx context.Context, a *Account, now time.Time) ([]Limit, error) {
	blob, err := b.Vault.Get(ParkService, a.Key())
	if errors.Is(err, ErrNotFound) {
		return nil, ErrInvalidGrant
	}
	if err != nil {
		return nil, err
	}
	l, err := parseLogin(blob)
	if err != nil {
		return nil, ErrInvalidGrant
	}
	if l.expiresBy(now.Add(renewAhead)) {
		if l, err = b.renew(ctx, a, l, now); err != nil {
			return nil, err
		}
	}
	limits, err := b.API.Usage(ctx, l.AccessToken)
	if errors.Is(err, ErrUnauthorized) {
		if l, err = b.renew(ctx, a, l, now); err != nil {
			return nil, err
		}
		limits, err = b.API.Usage(ctx, l.AccessToken)
	}
	return limits, err
}

// renew renews an added account's login and saves it before it is used. A
// save that fails loses the login: the old refresh token may be spent.
func (b *Book) renew(ctx context.Context, a *Account, l Login, now time.Time) (Login, error) {
	fresh, err := b.API.Renew(ctx, l, now)
	if err != nil {
		return l, err
	}
	if err := b.Vault.Put(ParkService, a.Key(), fresh.Marshal()); err != nil {
		return l, ErrInvalidGrant
	}
	if fresh.RefreshTokenExpiresAt > 0 {
		a.GoodUntil = time.UnixMilli(fresh.RefreshTokenExpiresAt)
	}
	return fresh, nil
}
```

- [ ] **Step 4: Run them to see them pass**

Run: `go test -race ./account/ -run TestRefresh -v`
Expected: PASS for all fourteen tests, with no race reported.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add account/refresh.go account/refresh_test.go
git commit -m "feat(account): refresh limits, renewing only the app's own logins

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Sign in through Claude Code

**Files:**
- Create: `account/signin.go`
- Modify: `account/book.go`: `load` removes leftovers.
- Test: `account/signin_test.go`

**Interfaces:**
- Consumes: `Book`, `keep`, `load` (Task 5); `liveService`, `parseLogin` (Task 3); `API.Profile` (Task 4).
- Produces:
  - `func (b *Book) SignIn(ctx context.Context, on func(Event)) (*SignIn, error)`
  - `type Event struct { Link string; AsksCode, Done bool; Account Account; Err error }`
  - `type SignIn` with `Code(string) error` and `Cancel()`
  - `var ErrCanceled`
  - `func FindClaude() (string, error)`

- [ ] **Step 1: Write the failing tests**

`account/signin_test.go`:

```go
package account

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const signLink = "https://claude.ai/oauth/authorize?code=true&client_id=9d1c250a&state=abc"

// fakeClaude stands in for `claude auth login --claudeai`. It prints the
// link wrapped in the terminal's escapes (bold, and an OSC 8 hyperlink)
// and asks for a code with no newline. On "good" it copies loginFile, if
// any, to its config folder, as claude falls back to .credentials.json.
func fakeClaude(t *testing.T, loginFile string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "claude")
	script := `#!/bin/sh
[ "$*" = "auth login --claudeai" ] || { echo "bad args: $*" >&2; exit 2; }
echo "Opening browser to sign in…"
printf 'Visit: \033[1m\033]8;;%s\033\\%s\033]8;;\033\\\033[0m\n' "` + signLink + `" "` + signLink + `"
printf 'Paste code here if prompted > '
read code
[ "$code" = "good" ] || { echo "Invalid code" >&2; exit 1; }
if [ -n '` + loginFile + `' ]; then cp '` + loginFile + `' "$CLAUDE_CONFIG_DIR/.credentials.json"; fi
echo "Login successful."
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// sleepyClaude prints a link, then waits for good.
func sleepyClaude(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho \"Visit: https://claude.ai/x\"\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// signInBook is a book on a fake Anthropic that knows bob's token bob-1.
func signInBook(t *testing.T, claude string) (*Book, *Memory) {
	t.Helper()
	f := newAnthropic()
	f.who["bob-1"] = bob
	b, v, _, _ := newBook(t, f)
	b.Claude = func() (string, error) { return claude, nil }
	return b, v
}

// bobsLoginFile writes bob's login as claude would leave it.
func bobsLoginFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "login.json")
	if err := os.WriteFile(p, login("bob-1", "bob-rt", t0.Add(time.Hour)).Marshal(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// start starts a sign-in and returns its events as they come.
func start(t *testing.T, b *Book) (*SignIn, func() Event) {
	t.Helper()
	evs := make(chan Event, 8)
	s, err := b.SignIn(ctx, func(e Event) { evs <- e })
	if err != nil {
		t.Fatal(err)
	}
	return s, func() Event {
		t.Helper()
		select {
		case e := <-evs:
			return e
		case <-time.After(10 * time.Second):
			t.Fatal("no event in 10 s")
			return Event{}
		}
	}
}

func leftovers(t *testing.T, b *Book) []string {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(b.Dir, "signin-*"))
	if err != nil {
		t.Fatal(err)
	}
	return dirs
}

func TestSignInAddsTheAccount(t *testing.T) {
	b, v := signInBook(t, fakeClaude(t, bobsLoginFile(t)))
	s, next := start(t, b)
	if e := next(); e.Link != signLink {
		t.Fatalf("link = %q, want %q", e.Link, signLink)
	}
	if e := next(); !e.AsksCode {
		t.Fatalf("got %+v, want a request for the code", e)
	}
	if err := s.Code(" good "); err != nil {
		t.Fatal(err)
	}
	e := next()
	if !e.Done || e.Err != nil || e.Account.Email != "bob@example.com" || !e.Account.Added {
		t.Fatalf("got %+v", e)
	}
	if _, err := v.Get(ParkService, "bob/org-b"); err != nil {
		t.Fatalf("bob's login was not kept: %v", err)
	}
	if left := leftovers(t, b); len(left) != 0 {
		t.Fatalf("left %v", left)
	}
}

func TestSignInReadsTheKeychainItem(t *testing.T) {
	b, v := signInBook(t, fakeClaude(t, ""))
	s, next := start(t, b)
	next()
	next()
	dirs := leftovers(t, b)
	if len(dirs) != 1 {
		t.Fatalf("sign-in folders %v", dirs)
	}
	// As claude keeps the login when the keychain takes it.
	if err := v.Put(liveService(dirs[0]), "me", login("bob-1", "bob-rt", t0.Add(time.Hour)).Marshal()); err != nil {
		t.Fatal(err)
	}
	if err := s.Code("good"); err != nil {
		t.Fatal(err)
	}
	if e := next(); e.Err != nil || e.Account.Email != "bob@example.com" {
		t.Fatalf("got %+v", e)
	}
	if _, err := v.Get(liveService(dirs[0]), ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("claude's keychain item was left behind")
	}
}

func TestSignInFindsTheItemOfTheResolvedPath(t *testing.T) {
	b, v := signInBook(t, fakeClaude(t, ""))
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	b.Dir = link
	s, next := start(t, b)
	next()
	next()
	dirs := leftovers(t, b)
	resolved, err := filepath.EvalSymlinks(dirs[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Put(liveService(resolved), "me", login("bob-1", "bob-rt", t0.Add(time.Hour)).Marshal()); err != nil {
		t.Fatal(err)
	}
	if err := s.Code("good"); err != nil {
		t.Fatal(err)
	}
	if e := next(); e.Err != nil || e.Account.Email != "bob@example.com" {
		t.Fatalf("got %+v", e)
	}
	if _, err := v.Get(liveService(resolved), ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("claude's keychain item was left behind")
	}
}

func TestSignInFailsWithClaudesWords(t *testing.T) {
	b, v := signInBook(t, fakeClaude(t, bobsLoginFile(t)))
	s, next := start(t, b)
	next()
	next()
	if err := s.Code("bad"); err != nil {
		t.Fatal(err)
	}
	if e := next(); !e.Done || e.Err == nil || e.Err.Error() != "Invalid code" {
		t.Fatalf("got %+v", e)
	}
	if left := leftovers(t, b); len(left) != 0 || v.Len() != 0 {
		t.Fatalf("left %v and %d items", left, v.Len())
	}
}

func TestSignInCancel(t *testing.T) {
	b, _ := signInBook(t, sleepyClaude(t))
	s, next := start(t, b)
	next()
	s.Cancel()
	if e := next(); !e.Done || !errors.Is(e.Err, ErrCanceled) {
		t.Fatalf("got %+v", e)
	}
	if left := leftovers(t, b); len(left) != 0 {
		t.Fatalf("left %v", left)
	}
}

func TestSignInTimesOut(t *testing.T) {
	b, _ := signInBook(t, sleepyClaude(t))
	b.SignInTimeout = 300 * time.Millisecond
	_, next := start(t, b)
	next()
	if e := next(); !e.Done || e.Err == nil || e.Err.Error() != "The sign-in took too long." {
		t.Fatalf("got %+v", e)
	}
}

func TestSignInWithoutClaude(t *testing.T) {
	b, _ := signInBook(t, "")
	b.Claude = func() (string, error) { return "", errors.New("Claude Code not found.") }
	if _, err := b.SignIn(ctx, func(Event) {}); err == nil || err.Error() != "Claude Code not found." {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadRemovesLeftovers(t *testing.T) {
	b, v := signInBook(t, "")
	left := filepath.Join(b.Dir, "signin-old")
	if err := os.MkdirAll(left, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := v.Put(liveService(left), "me", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Blocks(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(left); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the folder stays: %v", err)
	}
	if v.Len() != 0 {
		t.Fatal("its keychain item stays")
	}
}

func TestSignInEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "leak")
	env := strings.Join(signInEnv("/d", "/opt/x/bin/claude"), "\n")
	if !strings.Contains(env, "CLAUDE_CONFIG_DIR=/d") || !strings.Contains(env, "PATH=/opt/x/bin:") || strings.Contains(env, "leak") {
		t.Fatalf("env:\n%s", env)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./account/ -run 'TestSignIn|TestLoadRemovesLeftovers'`
Expected: FAIL to build: `b.SignIn undefined`, `undefined: Event`, `undefined: signInEnv`.

- [ ] **Step 3: Write `account/signin.go`**

```go
package account

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ErrCanceled ends a sign-in the user canceled.
var ErrCanceled = errors.New("The sign-in was canceled.")

var (
	errTooLong = errors.New("The sign-in took too long.")
	errNoLogin = errors.New("The sign-in left no login.")
	// The terminal's escapes around claude's words: OSC 8 links and colours.
	escapes = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b\[[0-9;?]*[A-Za-z]`)
	linkRe  = regexp.MustCompile(`https://\S+`)
)

// Event is what a sign-in reports: its link, that it asks for a code, and
// how it ended. Account is the account added when Done and Err is nil.
type Event struct {
	Link     string
	AsksCode bool
	Done     bool
	Account  Account
	Err      error
}

// SignIn is a sign-in under way.
type SignIn struct {
	cancel context.CancelFunc
	mu     sync.Mutex
	stdin  io.WriteCloser
}

// Code types the code the browser shows back to claude.
func (s *SignIn) Code(code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := io.WriteString(s.stdin, strings.TrimSpace(code)+"\n")
	return err
}

// Cancel stops the sign-in, which ends with ErrCanceled.
func (s *SignIn) Cancel() { s.cancel() }

// SignIn runs Claude Code's own sign-in, `claude auth login --claudeai`, in
// a private folder, so the account Claude Code uses stays signed in. on
// gets each event, from another goroutine. The login it ends with is kept
// as an added account. The folder, and the keychain item claude made for
// it, are removed however it ends.
func (b *Book) SignIn(ctx context.Context, on func(Event)) (*SignIn, error) {
	b.mu.Lock()
	_ = b.load() // clears leftovers first, so they can't be taken for this one
	b.mu.Unlock()
	bin, err := b.Claude()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(b.Dir, 0o700); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(b.Dir, "signin-")
	if err != nil {
		return nil, err
	}
	timeout := b.SignInTimeout
	if timeout == 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	cmd := exec.CommandContext(ctx, bin, "auth", "login", "--claudeai")
	cmd.Dir, cmd.Env = dir, signInEnv(dir, bin)
	// Its own process group, so a cancel also stops what claude started.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	r, w, err := os.Pipe()
	if err != nil {
		cancel()
		b.forget(dir)
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = w, w
	stdin, err := cmd.StdinPipe()
	if err == nil {
		err = cmd.Start()
	}
	w.Close()
	if err != nil {
		r.Close()
		cancel()
		b.forget(dir)
		return nil, err
	}
	go func() {
		last := watch(r, on)
		err := cmd.Wait()
		r.Close()
		ev := Event{Done: true}
		switch {
		case errors.Is(ctx.Err(), context.Canceled):
			ev.Err = ErrCanceled
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			ev.Err = errTooLong
		case err != nil && last != "":
			ev.Err = errors.New(last)
		case err != nil:
			ev.Err = errors.New("Claude Code's sign-in failed.")
		default:
			ev.Account, ev.Err = b.adopt(dir)
		}
		b.forget(dir)
		cancel()
		on(ev)
	}()
	return &SignIn{cancel: cancel, stdin: stdin}, nil
}

// watch reads claude's output until it ends, reports its link and its
// request for a code once each, and returns the last line it wrote. The
// request ends with no newline: claude waits on that line.
func watch(r io.Reader, on func(Event)) (last string) {
	var linked, asked bool
	see := func(s string, whole bool) {
		s = strings.TrimSpace(escapes.ReplaceAllString(s, ""))
		if s == "" {
			return
		}
		if whole {
			last = s
			if m := linkRe.FindString(s); m != "" && !linked {
				linked = true
				on(Event{Link: m})
			}
		}
		if strings.Contains(s, "Paste code here") && !asked {
			asked = true
			on(Event{AsksCode: true})
		}
	}
	buf := make([]byte, 4096)
	var pending string
	for {
		n, err := r.Read(buf)
		pending += string(buf[:n])
		for {
			i := strings.IndexByte(pending, '\n')
			if i < 0 {
				break
			}
			see(pending[:i], true)
			pending = pending[i+1:]
		}
		see(pending, false)
		if strings.Contains(pending, "Paste code here") {
			pending = "" // the prompt is read; what claude writes next starts its own line
		}
		if err != nil {
			break
		}
	}
	see(pending, true)
	return last
}

// signInEnv is all claude gets: its folder and the user's basics. claude's
// own folder leads PATH, for a claude that runs on a node installed beside
// it.
func signInEnv(dir, bin string) []string {
	env := []string{"CLAUDE_CONFIG_DIR=" + dir, "PATH=" + filepath.Dir(bin) + string(os.PathListSeparator) + os.Getenv("PATH")}
	for _, k := range []string{"HOME", "USER", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// adopt keeps the login claude left for dir: in the keychain item it named
// after dir, or in dir's .credentials.json.
func (b *Book) adopt(dir string) (Account, error) {
	var blob []byte
	err := ErrNotFound
	for _, s := range services(dir) {
		if blob, err = b.Vault.Get(s, ""); !errors.Is(err, ErrNotFound) {
			break
		}
	}
	if errors.Is(err, ErrNotFound) {
		blob, err = os.ReadFile(filepath.Join(dir, ".credentials.json"))
	}
	if err != nil {
		return Account{}, errNoLogin
	}
	l, err := parseLogin(blob)
	if err != nil {
		return Account{}, errNoLogin
	}
	p, err := b.API.Profile(context.Background(), l.AccessToken)
	if err != nil {
		return Account{}, err
	}
	return b.keep(l, p)
}

// services are the names claude may give dir's keychain item: from the
// path as given, and from it with symlinks resolved.
func services(dir string) []string {
	s := []string{liveService(dir)}
	if real, err := filepath.EvalSymlinks(dir); err == nil && real != dir {
		s = append(s, liveService(real))
	}
	return s
}

// forget removes a sign-in's folder and the keychain item claude made for it.
func (b *Book) forget(dir string) {
	for _, s := range services(dir) {
		_ = b.Vault.Delete(s, "")
	}
	_ = os.RemoveAll(dir)
}

// cleanLeftovers removes what a sign-in the app did not finish left.
func (b *Book) cleanLeftovers() {
	dirs, _ := filepath.Glob(filepath.Join(b.Dir, "signin-*"))
	for _, d := range dirs {
		b.forget(d)
	}
}

// FindClaude finds the claude binary as a login shell would, else where its
// installers put it.
func FindClaude() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "/bin/zsh", "-lc", "command -v claude").Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if p := strings.TrimSpace(lines[len(lines)-1]); filepath.IsAbs(p) {
			return p, nil
		}
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{filepath.Join(home, ".local", "bin", "claude"), "/opt/homebrew/bin/claude", "/usr/local/bin/claude"} {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("Claude Code not found.")
}
```

In `account/book.go`, make `load` remove leftovers first:

```go
// load reads accounts.json once, after removing what an unfinished
// sign-in left.
func (b *Book) load() error {
	if b.loaded {
		return nil
	}
	b.loaded = true
	b.cleanLeftovers()
	data, err := os.ReadFile(b.path())
```

The rest of `load` is unchanged.

- [ ] **Step 4: Run them to see them pass**

Run: `go test -race ./account/ -run 'TestSignIn|TestLoadRemovesLeftovers' -v`
Expected: PASS for all nine tests, with no race reported. `TestSignInCancel` must finish in well under 30 s, which proves the process group was killed.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add account/signin.go account/signin_test.go account/book.go
git commit -m "feat(account): add accounts through Claude Code's own sign-in

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: The Limits page

**Files:**
- Create: `view/limits.go`
- Modify:
  - `view/app.go`: the fields, the sidebar item, the page case, the header, the page-open load, `SetActive`
  - `view/icons.go`: `iconLimits`
- Test: `view/limits_test.go`

**Interfaces:**
- Consumes: `account.Book` with `Blocks`, `Refresh`, `Remove`; `account.Account`; `account.Limit` with `Label()`; `account.LiveExpired`, `account.NewMemory`, `account.NewAPI`, `account.Login.Marshal`, `account.ParkService`.
- Produces:
  - `App.Accounts *account.Book`, `App.OpenURL func(string) error`, `App.CopyText func(string)`
  - `func (a *App) LimitsTick()`
  - the type `limits`, with `load(a *App, force bool)`, `view`, `subtitle`
  - the helpers `clock`, `ago`, `goodUntil`, `resets`, `planText`, `meter`
  - test helpers `limitsApp(t) *lim` and `waitFor`

- [ ] **Step 1: Write the failing tests**

`view/limits_test.go`:

```go
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
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./view/ -run 'TestLimits'`
Expected: FAIL to build: `a.Accounts undefined`, `undefined: clock`, `a.LimitsTick undefined`.

- [ ] **Step 3: Write `view/limits.go`**

```go
package view

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/account"
)

// limits is the Limits page: how much of each account's limits is used,
// read from Anthropic.
type limits struct {
	blocks  []account.Account
	loading bool
	loaded  bool      // a refresh has landed
	again   bool      // a load was asked during one
	force   bool      // and it was forced
	updated time.Time // when the last refresh landed
}

// load shows the book's saved readings, then refreshes them from
// Anthropic. force is the Refresh button's: it skips the 3-minute floor.
func (l *limits) load(a *App, force bool) {
	if a.Accounts == nil {
		return
	}
	if l.loading {
		l.again, l.force = true, l.force || force
		return
	}
	l.loading = true
	a.Go(func() {
		saved, err := a.Accounts.Blocks()
		a.Update(func() {
			if !l.loaded {
				l.blocks = saved
			}
			l.toast(a, err)
		})
		blocks, err := a.Accounts.Refresh(context.Background(), force)
		a.Update(func() {
			l.loading, l.loaded = false, true
			l.blocks, l.updated = blocks, a.Store.Now()
			l.toast(a, err)
			if l.again {
				f := l.force
				l.again, l.force = false, false
				l.load(a, f)
			}
		})
	})
}

// toast shows err, unless a notice is waiting already.
func (l *limits) toast(a *App, err error) {
	if err != nil && a.notice == "" {
		a.notice = err.Error()
	}
}

// LimitsTick refreshes the Limits page while it is shown and the app is in
// front. main calls it every 5 minutes.
func (a *App) LimitsTick() {
	a.Update(func() {
		if a.page == "limits" && a.live.active {
			a.limits.load(a, false)
		}
	})
}

func (l *limits) subtitle(now time.Time) string {
	switch {
	case l.loading:
		return "Updating…"
	case l.updated.IsZero():
		return "Not updated yet"
	}
	return "Updated " + ago(l.updated, now)
}

func (l *limits) view(c *ui.Context, a *App, pal *palette) {
	switch {
	case a.Accounts == nil:
		ui.Text(c, "Limits are unavailable.").TextColor(pal.muted)
	case !l.loaded && len(l.blocks) == 0:
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c).Label("Reading limits")
			ui.Text(c, "Reading limits…").TextColor(pal.muted)
		})
	case len(l.blocks) == 0:
		ui.Text(c, "No accounts yet. Add one with Add Account…").TextColor(pal.muted)
	default:
		now := a.Store.Now()
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Gap(14).Children(func() {
				for _, b := range l.blocks {
					l.block(c, a, pal, b, now)
				}
			})
		})
	}
}

// block is one account: its name, then a meter per limit, or why there are
// no figures.
func (l *limits) block(c *ui.Context, a *App, pal *palette, b account.Account, now time.Time) {
	face(c, pal).Padding(14, 16).Gap(10).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			name := b.Email
			if name == "" {
				name = "Claude Code"
			}
			ui.Text(c, name).Bold().SingleLine()
			if s := planText(b); s != "" {
				ui.Text(c, s).TextColor(pal.muted).SingleLine()
			}
			if b.InUse {
				ui.Text(c, "In use").FontSize(11).Bold().TextColor(pal.muted)
			}
			ui.Spacer(c)
		})
		switch {
		case len(b.Limits) == 0 && b.Err != "":
			ui.Text(c, b.Err).TextColor(pal.muted).Wrap()
		case len(b.Limits) == 0:
			ui.Text(c, "No figures yet.").TextColor(pal.muted)
		}
		for _, x := range b.Limits {
			limitRow(c, pal, x, now)
		}
		if len(b.Limits) > 0 && b.Err != "" {
			ui.Text(c, b.Err+" Showing numbers from "+clock(b.ReadAt, now)+".").TextColor(pal.muted).Wrap()
		}
		if b.Added && !b.GoodUntil.IsZero() {
			ui.Text(c, goodUntil(b.GoodUntil, now)).FontSize(12).TextColor(pal.muted)
		}
	})
}

// limitRow is one limit: its name, a meter, the share used and the reset.
func limitRow(c *ui.Context, pal *palette, x account.Limit, now time.Time) {
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, x.Label()).Width(150).SingleLine()
		meter(c, pal, x.Percent).Grow(1).Label(x.Label())
		num(c, fmt.Sprintf("%.0f%%", x.Percent)).Width(44)
		ui.Text(c, resets(x.ResetsAt, now)).TextColor(pal.muted).Width(150).SingleLine()
		if x.Percent >= 100 {
			ui.Text(c, "Limit reached").Bold().SingleLine()
		}
	})
}

// meter is a limit's bar: graphite, then MyGo's warning colour from 80%
// and danger colour from 95%. Green stays the running lamp's.
func meter(c *ui.Context, pal *palette, pct float64) ui.Element {
	t := c.Theme()
	m := *t
	m.Success = pal.sel
	c.SetTheme(&m)
	e := ui.Meter(c, pct, 0, 100, &ui.MeterLevels{Warning: 80, Critical: 95})
	c.SetTheme(t)
	return e
}

// planText names the plan, and the organisation of a team's.
func planText(b account.Account) string {
	if b.Plan == "" {
		return ""
	}
	plan := strings.ToUpper(b.Plan[:1]) + b.Plan[1:]
	if (b.Plan == "team" || b.Plan == "enterprise") && b.OrgName != "" {
		plan += " · " + b.OrgName
	}
	return plan
}

// resets says when a limit resets.
func resets(at, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	return "resets " + clock(at, now)
}

// clock is t as the page prints it: the time within a day of now, else the
// date and time.
func clock(t, now time.Time) string {
	t = t.In(now.Location())
	if d := t.Sub(now); d > -24*time.Hour && d < 24*time.Hour {
		return t.Format("15:04")
	}
	return t.Format("Jan 2, 15:04")
}

// ago says how long before now t was: "just now", "4 min ago", or the time.
func ago(t, now time.Time) string {
	switch d := now.Sub(t); {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d/time.Minute))
	}
	return "at " + t.In(now.Location()).Format("15:04")
}

// goodUntil says how long an added account's saved sign-in lasts.
func goodUntil(t, now time.Time) string {
	const day = 24 * time.Hour
	switch d := t.Sub(now); {
	case d <= 0:
		return "Sign-in expired"
	case d < day:
		return "Sign-in expires today"
	case d < 3*day:
		return fmt.Sprintf("Sign-in expires in %d days", int(d/day)+1)
	}
	return "Sign-in good until " + t.In(now.Location()).Format("Jan 2")
}
```

- [ ] **Step 4: Wire the page into `view/app.go` and `view/icons.go`**

In `view/icons.go`, update the comment, then add the icon after `iconUsage`:

```go
// Sidebar icons, from Lucide (ISC license): pie-chart, message-square,
// history, chart-column, gauge, sliders-horizontal, trash-2.
```

```go
	iconLimits   = icon(`<path d="m12 14 4-4"/><path d="M3.34 19a10 10 0 1 1 17.32 0"/>`)
```

In `view/app.go`:

1. Imports: add `"github.com/lang315/claude-manager/account"`.
2. In `App`, after `Update func(func())`:

```go
	// Accounts holds the Limits page's accounts; nil shows the page empty.
	Accounts *account.Book
	// OpenURL and CopyText reach the system: the browser and the clipboard.
	OpenURL  func(string) error
	CopyText func(string)
```

   And after `cleanup  cleanup`:

```go
	limits   limits
```

3. In `View`, the page-change block becomes:

```go
	if a.page != a.shown { // a page opened: what it missed while hidden loads now
		a.shown = a.page
		a.live.show(pageScreens(a.page))
		if a.page == "limits" {
			a.limits.load(a, false)
		}
	}
```

4. In the sidebar, after `ui.SidebarItem(c, "usage", iconUsage, "Usage")`:

```go
			ui.SidebarItem(c, "limits", iconLimits, "Limits")
```

5. In the page switch, after the `"usage"` case:

```go
			case "limits":
				a.limits.view(c, a, pal)
```

6. In `header`, after the `"usage"` case:

```go
	case "limits":
		title, sub = "Limits", a.limits.subtitle(a.Store.Now())
```

   Replace the spinner and Rescan lines with:

```go
		if a.busy > 0 || a.page == "limits" && a.limits.loading {
			ui.Spinner(c).Label("Scanning")
			ui.Text(c, "Scanning…").TextColor(pal.muted)
		}
		if a.page == "limits" {
			if ui.Button(c, "Add Account…").Disabled(a.Accounts == nil).Clicked() {
				a.limits.startAdd(a)
			}
			if ui.Button(c, "Refresh").Disabled(a.Accounts == nil || a.limits.loading).Clicked() {
				a.limits.load(a, true)
			}
		} else if ui.Button(c, "Rescan").Disabled(a.busy > 0).Clicked() {
			a.Rescan()
		}
```

   `startAdd` comes in Task 9. Until then, give `limits` this stub at the end of `view/limits.go`, which Task 9 replaces:

```go
// startAdd opens the Add Account dialog. Task 9 builds it.
func (l *limits) startAdd(a *App) {}
```

7. In `SetActive`:

```go
		if on {
			a.live.activate()
			if a.page == "limits" {
				a.limits.load(a, false)
			}
		} else {
```

- [ ] **Step 5: Run the tests to see them pass**

Run: `go test -race ./view/ -run 'TestLimits' -v`
Expected: PASS for all six tests.
Then run: `go test ./view/`
Expected: PASS: the other pages' tests are unchanged. `Rescan` still shows on every page but Limits.

- [ ] **Step 6: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add view/limits.go view/limits_test.go view/app.go view/icons.go
git commit -m "feat(view): a Limits page for each account's limits

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Add, sign in again, remove

**Files:**
- Modify: `view/limits.go`: the dialog fields, `startAdd` (replacing the stub), `event`, `stopSignIn`, `dialogs`, the ⋯ menu, **Sign In Again**.
- Test: `view/limits_test.go`, extended.

**Interfaces:**
- Consumes: `account.Book.SignIn`, `account.Event`, `account.SignIn` (`Code`, `Cancel`), `account.Book.Remove`, `account.ErrInvalidGrant` (Tasks 5–7); `App.OpenURL`, `App.CopyText` (Task 8).
- Produces: the Add Account dialog, the Remove confirmation, the ⋯ menu.

- [ ] **Step 1: Write the failing tests**

Add to `view/limits_test.go` (and add `"os"`, `"path/filepath"` and `"slices"` to its imports):

```go
// seedBob lists bob as an added account, as a sign-in would have, with his
// login under token tok.
func seedBob(t *testing.T, l *lim, tok string) {
	t.Helper()
	login := account.Login{AccessToken: tok, RefreshToken: "bob-rt", ExpiresAt: fixture.Now.Add(time.Hour).UnixMilli()}
	must(t, l.vault.Put(account.ParkService, "bob/org", login.Marshal()))
	must(t, os.WriteFile(filepath.Join(l.book.Dir, "accounts.json"),
		[]byte(`{"accounts":[{"accountUuid":"bob","orgUuid":"org","email":"bob@example.com","goodUntil":"2026-11-15T12:00:00Z"}]}`), 0o600))
}

// claudeAt stands in for claude auth login: it prints a link, asks for a
// code and, on "good", leaves bob's login in its config folder.
func claudeAt(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	login := filepath.Join(dir, "login.json")
	must(t, os.WriteFile(login, account.Login{AccessToken: "bob", RefreshToken: "bob-rt", ExpiresAt: fixture.Now.Add(time.Hour).UnixMilli()}.Marshal(), 0o600))
	bin := filepath.Join(dir, "claude")
	must(t, os.WriteFile(bin, []byte(`#!/bin/sh
echo "Visit: https://claude.ai/oauth/authorize?state=abc"
printf 'Paste code here if prompted > '
read code
[ "$code" = "good" ] || { echo "Invalid code" >&2; exit 1; }
cp '`+login+`' "$CLAUDE_CONFIG_DIR/.credentials.json"
`), 0o755))
	return bin
}

func TestLimitsRemove(t *testing.T) {
	l := limitsApp(t)
	seedBob(t, l, "bob")
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") })
	if !l.tt.HasText("Sign-in good until Nov 15") {
		t.Errorf("no sign-in date in %q", l.tt.Texts())
	}
	must(t, l.tt.Click("⋯"))
	must(t, l.tt.ChooseMenuItem("Remove…"))
	l.tt.Frame()
	must(t, l.tt.Click("Remove"))
	waitFor(t, l.tt, func() bool { return !l.tt.HasText("bob@example.com") })
	if l.vault.Len() != 0 {
		t.Fatal("bob's login stays in the keychain")
	}
}

func TestLimitsOffersSignInAgain(t *testing.T) {
	l := limitsApp(t)
	seedBob(t, l, "expired")
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Sign-in expired.") })
	if !l.tt.HasText("Sign In Again") {
		t.Fatalf("texts %q", l.tt.Texts())
	}
}

func TestLimitsAddAccount(t *testing.T) {
	l := limitsApp(t)
	bin := claudeAt(t)
	l.book.Claude = func() (string, error) { return bin, nil }
	l.open(t)
	must(t, l.tt.Click("Add Account…"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Submit Code") })
	must(t, l.tt.Click("Copy Link"))
	must(t, l.tt.Click("Open Browser"))
	want := []string{"https://claude.ai/oauth/authorize?state=abc"}
	if !slices.Equal(*l.copied, want) || !slices.Equal(*l.opened, want) {
		t.Fatalf("copied %v, opened %v", *l.copied, *l.opened)
	}
	l.tt.Update(func() { l.a.limits.code = "good" })
	l.tt.Frame()
	must(t, l.tt.Click("Submit Code"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") && !l.tt.HasText("Submit Code") })
}

func TestLimitsAddAccountFails(t *testing.T) {
	l := limitsApp(t)
	bin := claudeAt(t)
	l.book.Claude = func() (string, error) { return bin, nil }
	l.open(t)
	must(t, l.tt.Click("Add Account…"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Submit Code") })
	l.tt.Update(func() { l.a.limits.code = "bad" })
	l.tt.Frame()
	must(t, l.tt.Click("Submit Code"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Invalid code") && l.tt.HasText("Try Again") })
}

func TestLimitsCancelAddAccount(t *testing.T) {
	l := limitsApp(t)
	bin := claudeAt(t)
	l.book.Claude = func() (string, error) { return bin, nil }
	l.open(t)
	must(t, l.tt.Click("Add Account…"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Submit Code") })
	must(t, l.tt.Click("Cancel"))
	waitFor(t, l.tt, func() bool {
		left, _ := filepath.Glob(filepath.Join(l.book.Dir, "signin-*"))
		return !l.tt.HasText("Submit Code") && len(left) == 0
	})
}

func TestLimitsIgnoresAnOldSignIn(t *testing.T) {
	l := limitsApp(t)
	bin := claudeAt(t)
	l.book.Claude = func() (string, error) { return bin, nil }
	l.open(t)
	must(t, l.tt.Click("Add Account…"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Submit Code") })
	defer l.a.limits.stopSignIn()
	l.tt.Update(func() { l.a.limits.startAdd(l.a) }) // Try Again: the first is canceled
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Submit Code") })
	for range 30 { // the first one's end comes late; it must change nothing
		l.tt.Frame()
		time.Sleep(10 * time.Millisecond)
	}
	if l.a.limits.failed != "" || !l.a.limits.adding || l.a.limits.signIn == nil {
		t.Fatalf("the old sign-in's end reached the new dialog: failed %q, adding %v", l.a.limits.failed, l.a.limits.adding)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./view/ -run 'TestLimitsRemove|TestLimitsOffersSignInAgain|TestLimitsAddAccount|TestLimitsCancelAddAccount|TestLimitsIgnoresAnOldSignIn'`
Expected: FAIL to build: `l.a.limits.code undefined`, `l.a.limits.failed undefined`, `l.a.limits.signIn undefined`.

- [ ] **Step 3: Build the dialog in `view/limits.go`**

1. Add these fields to `limits`, after `updated`:

```go
	// The Add Account dialog, while a sign-in runs.
	adding bool
	signIn *account.SignIn
	gen    int // counts sign-ins, so an old one's late events are ignored
	link   string
	asks   bool // claude waits for a code
	code   string
	failed string // why the sign-in failed

	asking   bool // the Remove confirmation is open
	removing account.Account
```

2. Replace the `startAdd` stub with:

```go
// startAdd opens the Add Account dialog and starts a sign-in.
func (l *limits) startAdd(a *App) {
	l.stopSignIn()
	l.adding, l.link, l.asks, l.code, l.failed = true, "", false, "", ""
	gen := l.gen
	s, err := a.Accounts.SignIn(context.Background(), func(ev account.Event) {
		a.Update(func() {
			if gen == l.gen {
				l.event(a, ev)
			}
		})
	})
	if err != nil {
		l.failed = err.Error()
		return
	}
	l.signIn = s
}

// event takes what the sign-in reports.
func (l *limits) event(a *App, ev account.Event) {
	switch {
	case ev.Link != "":
		l.link = ev.Link
	case ev.AsksCode:
		l.asks = true
	case ev.Done:
		l.signIn = nil
		if ev.Err != nil {
			l.failed = ev.Err.Error()
			return
		}
		l.adding = false
		l.load(a, true)
	}
}

// stopSignIn cancels the sign-in under way, if any. What it still reports
// is ignored.
func (l *limits) stopSignIn() {
	l.gen++
	if l.signIn != nil {
		l.signIn.Cancel()
		l.signIn = nil
	}
}

// dialogs are the Add Account dialog and the Remove confirmation.
func (l *limits) dialogs(c *ui.Context, a *App, pal *palette) {
	was := l.adding
	ui.Modal(c, &l.adding, func() {
		ui.Column(c).Gap(12).Padding(20).Width(440).Children(func() {
			ui.Text(c, "Add Account").FontSize(17).Bold()
			if l.failed != "" {
				ui.Text(c, l.failed).Wrap()
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, "Cancel").Clicked() {
						l.adding = false
					}
					if ui.PrimaryButton(c, "Try Again").Clicked() {
						l.startAdd(a)
					}
				})
				return
			}
			ui.Text(c, "Sign in, in the browser that opens, as the account to add.").Wrap()
			if l.link == "" {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Label("Starting the sign-in")
					ui.Text(c, "Starting Claude Code's sign-in…").TextColor(pal.muted)
				})
			} else {
				ui.Row(c).Gap(8).Children(func() {
					if ui.Button(c, "Copy Link").Clicked() {
						a.CopyText(l.link)
					}
					if ui.Button(c, "Open Browser").Clicked() {
						if err := a.OpenURL(l.link); err != nil {
							l.failed = err.Error()
						}
					}
				})
			}
			if l.asks {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.TextInput(c, &l.code).Placeholder("Code").Grow(1)
					submit := ui.Button(c, "Submit Code").Disabled(strings.TrimSpace(l.code) == "").Clicked()
					if submit && l.signIn != nil {
						if err := l.signIn.Code(l.code); err != nil {
							l.failed = err.Error()
						}
					}
				})
			}
			ui.Row(c).Children(func() {
				ui.Spacer(c)
				if ui.Button(c, "Cancel").Clicked() {
					l.adding = false
				}
			})
		})
	})
	if was && !l.adding { // Cancel, Escape, or a click outside
		l.stopSignIn()
	}
	title := "Remove " + l.removing.Email + "?"
	msg := "Its saved sign-in is deleted from this Mac. It is not revoked: it stays valid at Anthropic until it expires."
	if ui.AlertDialog(c, &l.asking, title, msg, "Cancel", "Remove") == 1 {
		key := l.removing.Key()
		a.Go(func() {
			err := a.Accounts.Remove(key)
			blocks, _ := a.Accounts.Blocks()
			a.Update(func() {
				l.toast(a, err)
				l.blocks = blocks
			})
		})
	}
}

// menu holds an added account's actions.
func (l *limits) menu(c *ui.Context, a *App, b account.Account) {
	if !b.Added {
		return
	}
	ui.MenuButton(c, "⋯", func(m *ui.Menu) {
		if m.Item("Sign In Again").Chosen() {
			l.startAdd(a)
		}
		if m.Item("Remove…").Destructive(true).Chosen() {
			l.removing, l.asking = b, true
		}
	}).Tooltip("Actions for " + b.Email)
}
```

3. Use them:
   - In `view`, add `l.dialogs(c, a, pal)` as the last statement, after the `switch`.
   - In `block`, after `ui.Spacer(c)` in the header row, add `l.menu(c, a, b)`.
   - In `block`, replace the first `switch` case:

```go
		case len(b.Limits) == 0 && b.Err != "":
			ui.Text(c, b.Err).TextColor(pal.muted).Wrap()
			if b.Added && b.Err == account.ErrInvalidGrant.Error() && ui.Button(c, "Sign In Again").Clicked() {
				l.startAdd(a)
			}
```

- [ ] **Step 4: Run them to see them pass**

Run: `go test -race ./view/ -run 'TestLimits' -v`
Expected: PASS for all twelve Limits tests, with no race reported.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add view/limits.go view/limits_test.go
git commit -m "feat(view): add, sign in again to, and remove accounts

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: Wire it up, document it, check it on the real Mac

**Files:**
- Modify: `main.go`, `DESIGN.md`, `docs/roadmap.md`
- Throwaway (scratchpad, never committed): `realcheck/`

**Interfaces:**
- Consumes: `account.Book`, `account.Keychain`, `account.ReadLive`, `account.NewAPI`, `account.FindClaude`; `App.Accounts`, `App.OpenURL`, `App.CopyText`, `App.LimitsTick`.

- [ ] **Step 1: Wire `main.go`**

Replace `main.go` with:

```go
// Claude Manager shows what is in ~/.claude and moves what you pick to the
// Trash.
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/account"
	"github.com/lang315/claude-manager/store"
	"github.com/lang315/claude-manager/view"
)

// version is mygo.json's, for the User-Agent of requests to Anthropic.
const version = "0.1.0"

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	configDir := os.Getenv("CLAUDE_CONFIG_DIR")
	root, claudeJSON := configDir, filepath.Join(configDir, ".claude.json")
	if root == "" {
		root, claudeJSON = filepath.Join(home, ".claude"), filepath.Join(home, ".claude.json")
	}
	a := view.New(&store.Store{
		Root:       root,
		ClaudeJSON: claudeJSON,
		Trash:      mygo.Shell.TrashItem,
		Now:        time.Now,
		Alive:      store.PIDAlive,
	})
	a.Accounts = accounts(home, configDir, root)
	a.OpenURL, a.CopyText = mygo.Shell.OpenExternal, mygo.Clipboard.WriteText
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:   "Claude Manager",
			Width:   1100,
			Height:  720,
			Content: ui.View(a.View),
		})
		a.Update = win.Update
		a.Rescan()
		go a.Watch(context.Background(), root, claudeJSON)
		mygo.App.OnDidBecomeActive(func() { a.SetActive(true) })
		mygo.App.OnDidResignActive(func() { a.SetActive(false) })
		a.SetActive(mygo.App.IsActive()) // launched in the background: hold reloads until it is in front
		go func() {
			for range time.Tick(5 * time.Minute) {
				a.LimitsTick()
			}
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// accounts is the Limits page's book, in the app's folder, with logins in
// the login keychain.
func accounts(home, configDir, root string) *account.Book {
	vault := account.Keychain{}
	return &account.Book{
		Dir:           filepath.Join(home, "Library", "Application Support", "Claude Manager"),
		Vault:         vault,
		API:           account.NewAPI(version),
		Live:          func() (account.Login, error) { return account.ReadLive(vault, configDir, root) },
		Now:           time.Now,
		Claude:        account.FindClaude,
		SignInTimeout: 10 * time.Minute,
	}
}
```

First compare this with the current `main.go`, which you read before this step: keep any line in it that this listing lacks. The listing is the `main.go` of `61767d9`, plus the account wiring.

- [ ] **Step 2: Build**

Run: `go build ./... && go vet ./...`
Expected: no output, exit 0.

- [ ] **Step 3: Document the meters and the roadmap item**

In `DESIGN.md`, under `## Colors`, after the `### Model Hues` section, add:

```markdown
### Limit Meters
- **Limit meter** (Limits page): MyGo's level indicator, 0 to 100.
  - Below 80% it is selection graphite.
  - From 80% it takes MyGo's Warning amber, and from 95% its Danger red.
  - Amber and red mean "near a limit", and appear nowhere else.
  - The percentage is always printed beside the meter, with "Limit reached" at 100%, so colour is never the only signal.
  - Green stays the lamp's.
```

In `## Components`, after `### Backups Table`, add:

```markdown
### Account Block
A face per account on the Limits page, padded 14 by 16.
- **Header:** the email in bold, the plan in muted (with the organisation for a team plan), a small bold muted "In use" on the account Claude Code is signed in to, and, for an added account, a "⋯" menu with Sign In Again and Remove….
- **Rows:** one per limit:
  - the name (150);
  - the meter, filling the width;
  - the percentage, tabular (44);
  - the muted reset time (150).
- **Footer:**
  - An added account ends with its sign-in date, muted, at 12px.
  - A block with no figures says why, in muted text.
```

In `docs/roadmap.md`, under `## v2 — search and insight`, after the `Watch ~/.claude` line, add:

```markdown
- [x] Limits of each Claude Code account: the five-hour, weekly and per-model weekly limits, for the account in use and accounts added through Claude Code's own sign-in (after [Pitboard](https://github.com/datlechin/pitboard)'s approach).
```

- [ ] **Step 4: Gate and commit**

```bash
gofmt -l . && go vet ./... && CGO_ENABLED=1 go test -race ./...
git add main.go DESIGN.md docs/roadmap.md
git commit -m "feat: wire the Limits page, document its meters

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

- [ ] **Step 5: Check the real login in use once (read-only; the user agreed)**

This sends the user's access token to `api.anthropic.com` once. It does not renew it, and prints no token.

Create the throwaway module in the scratchpad (`$SP` is the session's scratchpad directory):

```bash
mkdir -p $SP/realcheck && cd $SP/realcheck
cat > go.mod <<'EOF'
module realcheck

go 1.27.3

require github.com/lang315/claude-manager v0.0.0

replace github.com/lang315/claude-manager => /Users/lang/GolandProjects/github.com/lang315/claude-manager

replace github.com/egoist/mygo => /Users/lang/GolandProjects/github.com/lang315/mygo
EOF
cat > main.go <<'EOF'
// realcheck reads the limits of the account Claude Code is signed in to,
// once, through the app's account package. It prints no token, and keeps
// its book in a throwaway folder.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lang315/claude-manager/account"
)

func main() {
	home, _ := os.UserHomeDir()
	v := account.Keychain{}
	b := &account.Book{
		Dir: os.Args[1], Vault: v, API: account.NewAPI("0.1.0"), Now: time.Now,
		Live: func() (account.Login, error) { return account.ReadLive(v, "", filepath.Join(home, ".claude")) },
	}
	got, err := b.Refresh(context.Background(), true)
	fmt.Println("book error:", err)
	for _, a := range got {
		fmt.Printf("block: inUse=%v added=%v plan=%q err=%q limits=%d\n", a.InUse, a.Added, a.Plan, a.Err, len(a.Limits))
		for _, l := range a.Limits {
			fmt.Printf("  %-24s %5.1f%%  resets %s\n", l.Label(), l.Percent, l.ResetsAt.Local().Format("Jan 2 15:04"))
		}
	}
}
EOF
go mod tidy >/dev/null 2>&1; go run . $SP/realcheck/book
```

Expected:
- `book error: <nil>`;
- one block with `inUse=true`, `err=""`, and at least `Session (5h)` and `Weekly`, each with a percentage and a reset time.

The email is left out of the printout on purpose.

If the run fails, stop and report what it says. `Not signed in`, `Keychain is locked`, `Anthropic refused` or `Anthropic answered 4xx` each mean the facts the spec measured have changed.

Then check that nothing was written where it must not be:

```bash
security dump-keychain 2>/dev/null | grep -o '"Claude Code-credentials[^"]*"\|"claude-manager-park"' | sort | uniq -c
git -C ~/.claude status 2>/dev/null | head -1; ls -la ~/.claude.json
```

Expected:
- only `"Claude Code-credentials"`, the user's own item. There is no `claude-manager-park`, since no account was added.
- `~/.claude.json`'s modification time is not the time of the run.

- [ ] **Step 6: Build and look at the app**

Run the app from the scratchpad build:
- `$SP/mygo build`, then launch `dist/darwin-arm64/Claude Manager.app`.
- Open **Limits**.
- Take a screenshot for the user. It stays local and is never committed.

Expected:
- the block of the account in use, with its meters;
- "Updated just now".

If the window can't be driven, say so, and rely on Step 5's output.

**Adding a second account needs the user's own browser sign-in.** Ask them to:
1. click **Add Account…**;
2. sign in;
3. check that the new block appears.

Then run the check in Step 5 again (`security dump-keychain …`). Expected: one `"claude-manager-park"` item, and no `Claude Code-credentials-<hash>` item left over from the sign-in.

- [ ] **Step 7: Record the evidence for the PR**

Keep, in the scratchpad, the gate's output with its exit status, Step 5's printout (it has no email and no token), and Step 6's screenshot path. The PR body quotes the gate and Step 5, and says what Step 6 showed. Screenshots of real accounts stay local.
