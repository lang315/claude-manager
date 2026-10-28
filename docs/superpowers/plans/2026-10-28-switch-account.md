# Switch Account Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A "Switch to This Account" item on the Limits page puts Claude Code on another account, by running Pitboard's `pitboard use claude/<label> --json`.

**Architecture:**
- `account.ReadPitboard` reads each account's Pitboard label when Pitboard has a parked login for it, and `blocks()` carries that label onto the matching block.
- A new `account/switch.go` finds the `pitboard` command and runs it with a deadline, in a process group of its own. It parses Pitboard's JSON envelope (v1) into `Switched` or a typed error.
- `view/limits.go` adds the menu item, a "Switching…" state, a toast, and the "Replace Claude Code's sign-in?" dialog.

**Tech Stack:** Go 1.27.3; MyGo `ui` (`../mygo`); `os/exec`; tests with `ui.Tester` and fake shell scripts.

**Spec:** `docs/superpowers/specs/2026-10-28-switch-account-design.md`

## Global Constraints

- The app never writes `~/.claude`, `~/.claude.json` or Claude Code's keychain item, and never reads or renews Pitboard's parked logins. Only Pitboard's command switches.
- Command: `pitboard use claude/<label> --json`, plus `--yes` only after the user chose **Replace**. The label is one argv entry, never passed through a shell.
- Finding `pitboard`: `zsh -lc 'command -v pitboard'`, then `/opt/homebrew/bin/pitboard`, `/usr/local/bin/pitboard`, `/Applications/Pitboard.app/Contents/Helpers/pitboard`.
- Deadline: 60 s by default (`Book.SwitchTimeout`), then the process group is killed.
- Pitboard's standard error is never shown. The app reads only the envelope on standard output.
- Copy, exact:
  - the menu item: "Switch to This Account";
  - while it runs: "Switching…";
  - success: "Switched to <email>. Running sessions follow within N seconds.";
  - success with no follow time: "Switched to <email>. Running sessions keep the old account until they're started again.";
  - nothing changed: "<email> is already in use.";
  - no envelope: "Pitboard gave no answer.";
  - deadline: "Pitboard took too long.";
  - not found: "Pitboard not found.";
  - dialog title: "Replace Claude Code's sign-in?";
  - dialog body: "Claude Code's sign-in no longer works, so it can't be put away. Put <email>'s sign-in in its place?";
  - dialog buttons: "Cancel" and "Replace".
- Tests never run the real Pitboard. They never touch the real keychain, `~/.claude` or `~/.pitboard`, and never reach the network.
- Commits end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. No session links anywhere.
- Gate per task: `gofmt -l account view main.go` is empty, `go vet ./...` passes, and `go test ./...` passes.

## Review Focus

1. Pitboard exits 1 with a valid envelope (a refusal). The envelope's message must show, not "exit status 1". This is pinned in Task 2 (`TestSwitchRefused`).
2. A non-ASCII label (`Lãng`) and the `ã` escapes in the answer must round-trip intact. Pinned in Task 2 (`TestSwitchArgs`).
3. Pitboard hangs. The command must be killed at the deadline, the item must come back, and no goroutine may block the UI. Pinned in Task 2 (`TestSwitchTimesOut`) and Task 3 (the switching state clears on error).
4. A double click while a switch runs must not start a second one. Pinned in Task 3 (`TestLimitsSwitch`: the item is disabled while switching).
5. An account the app added itself that Pitboard also follows must still be switchable: the label survives the merge. Pinned in Task 1 (`TestPitboardLabelSurvivesMerge`).

---

### Task 1: Pitboard's label on the account

**Files:**
- Modify: `account/book.go` (the `Account` struct and the merge loop in `blocks()`)
- Modify: `account/pitboard.go` (read `label` and `parked`)
- Test: `account/pitboard_test.go`

**Interfaces:**
- Produces: `Account.PitboardLabel string` (`json:"-"`). It is set when Pitboard holds a parked login for the account; otherwise it is "".

- [ ] **Step 1: Write the failing tests** (append to `account/pitboard_test.go`)

```go
func TestReadPitboardLabel(t *testing.T) {
	got, err := ReadPitboard(writePitboard(t, pitboardState, ""))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].PitboardLabel != "work" {
		t.Errorf("bob's label = %q, want work: Pitboard parks his login", got[0].PitboardLabel)
	}
	if got[1].PitboardLabel != "" {
		t.Errorf("dan's label = %q, want none: nothing is parked", got[1].PitboardLabel)
	}
}

func TestPitboardLabelSurvivesMerge(t *testing.T) {
	b, _, _, _ := aliceInUse(t) // alice at 62%, read at t0
	b.Pitboard = func() ([]Account, error) {
		return []Account{{AccountUUID: "alice", OrgUUID: "org-a", Email: "alice@example.com", Source: "Pitboard",
			ReadAt: t0.Add(-time.Minute), PitboardLabel: "home"}}, nil
	}
	got, _ := b.Refresh(ctx, false)
	if a := got[0]; a.PitboardLabel != "home" || a.Source != "" {
		t.Fatalf("alice = %+v: our numbers stay, Pitboard's label joins them", a)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./account -run 'TestReadPitboardLabel|TestPitboardLabelSurvivesMerge'`
Expected: FAIL to compile: `got[0].PitboardLabel undefined`.

- [ ] **Step 3: Implement**

In `account/book.go`, add a field to `Account` after `Source`, aligned by gofmt:

```go
	PitboardLabel string `json:"-"` // Pitboard's label, when it holds a parked login for the account
```

In `account/pitboard.go`, the `st.Accounts` element gains:

```go
			Label       string          `json:"label"`
			Parked      json.RawMessage `json:"parked"`
```

and the account is built with:

```go
		a := Account{AccountUUID: p.AccountUUID, OrgUUID: p.OrgUUID, Email: p.Email, Plan: p.Plan, Source: "Pitboard"}
		if len(p.Parked) > 0 && string(p.Parked) != "null" {
			a.PitboardLabel = p.Label
		}
```

In `account/book.go` `blocks()`, the merge loop becomes:

```go
	for _, p := range b.others {
		i := slices.IndexFunc(out, func(a Account) bool { return a.Key() == p.Key() })
		if i < 0 {
			out = append(out, p)
			continue
		}
		out[i].PitboardLabel = p.PitboardLabel
		if len(p.Limits) > 0 && p.ReadAt.After(out[i].ReadAt) {
			out[i].Limits, out[i].ReadAt, out[i].Source = p.Limits, p.ReadAt, p.Source
		}
	}
```

- [ ] **Step 4: Run them and watch them pass**

Run: `go test ./account`
Expected: PASS (all account tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w account && go vet ./... && go test ./...
git add account/book.go account/pitboard.go account/pitboard_test.go
git commit -m "feat(account): keep Pitboard's label for accounts it parks

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Running `pitboard use`

**Files:**
- Create: `account/switch.go`
- Modify: `account/signin.go` (`FindClaude` shares a finder with `FindPitboard`)
- Modify: `account/book.go` (the `Book` fields `PitboardCLI`, `SwitchTimeout`)
- Test: `account/switch_test.go`

**Interfaces:**
- Consumes: `Account.PitboardLabel` (Task 1).
- Produces:
  - `func FindPitboard() (string, error)`
  - `var ErrNoPitboard`, `var ErrLiveRefused`
  - `type SwitchError struct{ Code, Message string }` (`Error()` returns `Message`)
  - `type Switched struct{ To string; Changed bool; Follow time.Duration }`
  - `func (b *Book) CanSwitch() bool`
  - `func (b *Book) Switch(ctx context.Context, label string, replace bool) (Switched, error)`
  - the `Book` fields `PitboardCLI func() (string, error)` and `SwitchTimeout time.Duration`

- [ ] **Step 1: Write the failing tests** (`account/switch_test.go`)

```go
package account

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakePitboard is a pitboard that writes its arguments, one per line, to
// args, then runs body.
func fakePitboard(t *testing.T, body string) (bin, args string) {
	t.Helper()
	dir := t.TempDir()
	bin, args = filepath.Join(dir, "pitboard"), filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + args + "'\n" + body + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, args
}

func switchBook(bin string) *Book {
	return &Book{PitboardCLI: func() (string, error) { return bin, nil }}
}

const switched = `{"v":1,"command":"use","ok":true,"data":{"from":"work","to":"Lãng","provider":"claude","changed":true,"parked_at":1,"adoption_ceiling_seconds":33,"adoption":{}},"warnings":[],"error":null}`

func TestSwitchArgs(t *testing.T) {
	bin, args := fakePitboard(t, "echo '"+switched+"'")
	s, err := switchBook(bin).Switch(ctx, "Lãng", false)
	if err != nil {
		t.Fatal(err)
	}
	if s != (Switched{To: "Lãng", Changed: true, Follow: 33 * time.Second}) {
		t.Errorf("got %+v", s)
	}
	got, _ := os.ReadFile(args)
	if string(got) != "use\nclaude/Lãng\n--json\n" {
		t.Errorf("args %q", got)
	}
	if _, err := switchBook(bin).Switch(ctx, "Lãng", true); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(args)
	if string(got) != "use\nclaude/Lãng\n--json\n--yes\n" {
		t.Errorf("args with replace %q", got)
	}
}

func TestSwitchFollow(t *testing.T) {
	for _, c := range []struct{ data string; changed bool; follow time.Duration }{
		{`"changed":true,"adoption_ceiling_seconds":null`, true, 0},
		{`"changed":true`, true, 0},
		{`"changed":false`, false, 0},
	} {
		bin, _ := fakePitboard(t, `echo '{"v":1,"command":"use","ok":true,"data":{"to":"w",`+c.data+`},"warnings":[],"error":null}'`)
		s, err := switchBook(bin).Switch(ctx, "w", false)
		if err != nil || s.Changed != c.changed || s.Follow != c.follow {
			t.Errorf("%s: %+v, %v", c.data, s, err)
		}
	}
}

func TestSwitchRefused(t *testing.T) {
	bin, _ := fakePitboard(t, `echo '{"v":1,"command":"use","ok":false,"data":null,"warnings":[],"error":{"code":"parked_login_refused","message":"Its saved sign-in no longer works.","cause":null}}'
echo 'error: secret path' >&2
exit 1`)
	_, err := switchBook(bin).Switch(ctx, "w", false)
	var se *SwitchError
	if !errors.As(err, &se) || se.Code != "parked_login_refused" || err.Error() != "Its saved sign-in no longer works." {
		t.Fatalf("err = %v", err)
	}
	bin, _ = fakePitboard(t, `echo '{"v":1,"command":"use","ok":false,"data":null,"warnings":[],"error":{"code":"live_login_refused","message":"x","cause":null}}'; exit 1`)
	if _, err := switchBook(bin).Switch(ctx, "w", false); !errors.Is(err, ErrLiveRefused) {
		t.Fatalf("live_login_refused: err = %v", err)
	}
}

func TestSwitchNoAnswer(t *testing.T) {
	for _, body := range []string{"exit 0", "echo 'not json'; exit 3", "echo '{\"v\":1,\"ok\":true}'"} {
		bin, _ := fakePitboard(t, body)
		if _, err := switchBook(bin).Switch(ctx, "w", false); err == nil || err.Error() != "Pitboard gave no answer." {
			t.Errorf("%q: err = %v", body, err)
		}
	}
}

func TestSwitchTimesOut(t *testing.T) {
	bin, _ := fakePitboard(t, "sleep 30")
	b := switchBook(bin)
	b.SwitchTimeout = 200 * time.Millisecond
	start := time.Now()
	_, err := b.Switch(ctx, "w", false)
	if err == nil || err.Error() != "Pitboard took too long." {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("took %v: the sleep was not killed", d)
	}
}

func TestCanSwitch(t *testing.T) {
	if (&Book{}).CanSwitch() {
		t.Error("no PitboardCLI, yet it can switch")
	}
	b := &Book{PitboardCLI: func() (string, error) { return "", ErrNoPitboard }}
	if b.CanSwitch() {
		t.Error("pitboard not found, yet it can switch")
	}
	if _, err := b.Switch(ctx, "w", false); !errors.Is(err, ErrNoPitboard) {
		t.Errorf("err = %v", err)
	}
	if !switchBook("/bin/true").CanSwitch() {
		t.Error("pitboard found, yet it can't switch")
	}
	if !strings.Contains(ErrNoPitboard.Error(), "Pitboard") {
		t.Error(ErrNoPitboard)
	}
}
```

(`ctx` is the package test context already defined in the account tests. If it is not, add `var ctx = context.Background()` to `switch_test.go` and record a Ruling.)

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./account -run 'TestSwitch|TestCanSwitch'`
Expected: FAIL to compile: `undefined: Switched` and others.

- [ ] **Step 3: Implement**

`account/book.go` adds these `Book` fields beside `Pitboard`:

```go
	// PitboardCLI finds Pitboard's command, which switches accounts.
	PitboardCLI   func() (string, error)
	SwitchTimeout time.Duration // how long a switch may take; 0 means 60 s
```

`account/signin.go`: `FindClaude` becomes a call to a shared finder.

```go
// FindClaude finds the claude binary as a login shell would, else where its
// installers put it.
func FindClaude() (string, error) {
	home, _ := os.UserHomeDir()
	return findTool("claude", "Claude Code not found.",
		filepath.Join(home, ".local", "bin", "claude"), "/opt/homebrew/bin/claude", "/usr/local/bin/claude")
}

// findTool finds name as a login shell would, else at the first of places
// that is a file.
func findTool(name, notFound string, places ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "/bin/zsh", "-lc", "command -v "+name).Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if p := strings.TrimSpace(lines[len(lines)-1]); filepath.IsAbs(p) {
			return p, nil
		}
	}
	for _, p := range places {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", errors.New(notFound)
}
```

`account/switch.go`:

```go
package account

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"syscall"
	"time"
)

var (
	ErrNoPitboard  = errors.New("Pitboard not found.")
	ErrLiveRefused = errors.New("Claude Code's sign-in no longer works.")
	errNoAnswer    = errors.New("Pitboard gave no answer.")
	errTooLong     = errors.New("Pitboard took too long.")
)

// SwitchError is Pitboard's refusal: its code, and its words for a person.
type SwitchError struct{ Code, Message string }

func (e *SwitchError) Error() string { return e.Message }

// Switched is what a switch did. Follow is how long sessions already
// running take to follow; 0 means only once they're started again.
type Switched struct {
	To      string
	Changed bool
	Follow  time.Duration
}

// FindPitboard finds Pitboard's command line, which its app bundle holds.
func FindPitboard() (string, error) {
	p, err := findTool("pitboard", ErrNoPitboard.Error(), "/opt/homebrew/bin/pitboard", "/usr/local/bin/pitboard",
		"/Applications/Pitboard.app/Contents/Helpers/pitboard")
	if err != nil {
		return "", ErrNoPitboard
	}
	return p, nil
}

// CanSwitch says whether Pitboard's command is here. It may run a login
// shell: call it off the main thread.
func (b *Book) CanSwitch() bool {
	_, err := b.pitboard()
	return err == nil
}

func (b *Book) pitboard() (string, error) {
	if b.PitboardCLI == nil {
		return "", ErrNoPitboard
	}
	return b.PitboardCLI()
}

// Switch puts Claude Code on the account Pitboard parks under label, by
// running pitboard use. replace answers yes to Pitboard's one question:
// whether to replace a sign-in Anthropic refuses for good. Pitboard does
// every write; the app reads its JSON answer, and never its stderr.
func (b *Book) Switch(ctx context.Context, label string, replace bool) (Switched, error) {
	bin, err := b.pitboard()
	if err != nil {
		return Switched{}, err
	}
	limit := b.SwitchTimeout
	if limit == 0 {
		limit = time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	args := []string{"use", "claude/" + label, "--json"}
	if replace {
		args = append(args, "--yes")
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	out, _ := cmd.Output() // the exit status says less than the envelope
	if ctx.Err() == context.DeadlineExceeded {
		return Switched{}, errTooLong
	}
	var env struct {
		OK   *bool `json:"ok"`
		Data *struct {
			To      string   `json:"to"`
			Changed bool     `json:"changed"`
			Follow  *float64 `json:"adoption_ceiling_seconds"`
		} `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(bytes.TrimSpace(out), &env) != nil || env.OK == nil {
		return Switched{}, errNoAnswer
	}
	switch {
	case !*env.OK && env.Error != nil && env.Error.Code == "live_login_refused":
		return Switched{}, ErrLiveRefused
	case !*env.OK && env.Error != nil:
		return Switched{}, &SwitchError{Code: env.Error.Code, Message: env.Error.Message}
	case !*env.OK || env.Data == nil:
		return Switched{}, errNoAnswer
	}
	s := Switched{To: env.Data.To, Changed: env.Data.Changed}
	if f := env.Data.Follow; f != nil && *f > 0 {
		s.Follow = time.Duration(*f * float64(time.Second))
	}
	return s, nil
}
```

The third body in `TestSwitchNoAnswer` (`{"v":1,"ok":true}`, which has no data) hits the `env.Data == nil` case.

- [ ] **Step 4: Run them and watch them pass**

Run: `go test ./account`
Expected: PASS, including the sign-in tests that use `FindClaude`'s callers.

- [ ] **Step 5: Commit**

```bash
gofmt -w account && go vet ./... && go test ./...
git add account/switch.go account/switch_test.go account/signin.go account/book.go
git commit -m "feat(account): switch through Pitboard's pitboard use

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: The Switch item on the Limits page

**Files:**
- Modify: `view/limits.go` (state, `load`, `block`, `menu`, `dialogs`, new `startSwitch` and `switchedText`)
- Modify: `view/app.go:162-167` (header buttons off while switching)
- Modify: `main.go` (`PitboardCLI: account.FindPitboard`)
- Modify: `DESIGN.md` (the Account Block's menu)
- Test: `view/limits_test.go`

**Interfaces:**
- Consumes: `Account.PitboardLabel`, `Book.CanSwitch`, `Book.Switch`, `Switched`, `ErrLiveRefused`, `Book.PitboardCLI` (Tasks 1 and 2).

- [ ] **Step 1: Write the failing tests** (append to `view/limits_test.go`)

```go
// carolOnPitboard lists carol as an account Pitboard parks under "Lãng",
// with pitboard the given script.
func carolOnPitboard(t *testing.T, l *lim, body string) (args string) {
	t.Helper()
	dir := t.TempDir()
	bin, args := filepath.Join(dir, "pitboard"), filepath.Join(dir, "args")
	must(t, os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" >> '"+args+"'\n"+body+"\n"), 0o755))
	l.book.PitboardCLI = func() (string, error) { return bin, nil }
	l.book.Pitboard = func() ([]account.Account, error) {
		return []account.Account{{AccountUUID: "carol", OrgUUID: "org", Email: "carol@example.com",
			Source: "Pitboard", PitboardLabel: "Lãng"}}, nil
	}
	return args
}

const pitboardOK = `echo '{"v":1,"command":"use","ok":true,"data":{"to":"Lãng","changed":true,"adoption_ceiling_seconds":33},"warnings":[],"error":null}'`

func TestLimitsSwitch(t *testing.T) {
	l := limitsApp(t)
	args := carolOnPitboard(t, l, "sleep 0.3\n"+pitboardOK)
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("carol@example.com") })
	must(t, l.tt.Click("⋯"))
	must(t, l.tt.ChooseMenuItem("Switch to This Account"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Switching…") })
	must(t, l.tt.Click("⋯"))
	if err := l.tt.ChooseMenuItem("Switch to This Account"); err == nil {
		t.Error("a second switch started while one runs")
	}
	waitFor(t, l.tt, func() bool {
		return l.tt.HasText("Switched to carol@example.com. Running sessions follow within 33 seconds.")
	})
	got, _ := os.ReadFile(args)
	if string(got) != "use\nclaude/Lãng\n--json\n" {
		t.Errorf("pitboard ran with %q", got)
	}
}

func TestLimitsSwitchNotOffered(t *testing.T) {
	l := limitsApp(t)
	carolOnPitboard(t, l, pitboardOK)
	l.book.PitboardCLI = func() (string, error) { return "", account.ErrNoPitboard }
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("carol@example.com") })
	if l.tt.HasText("⋯") {
		t.Error("a menu without Pitboard's command")
	}
}

func TestLimitsSwitchError(t *testing.T) {
	l := limitsApp(t)
	carolOnPitboard(t, l, `echo '{"v":1,"command":"use","ok":false,"data":null,"warnings":[],"error":{"code":"parked_login_refused","message":"Its saved sign-in no longer works."}}'; exit 1`)
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("carol@example.com") })
	must(t, l.tt.Click("⋯"))
	must(t, l.tt.ChooseMenuItem("Switch to This Account"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Its saved sign-in no longer works.") })
	waitFor(t, l.tt, func() bool { return !l.tt.HasText("Switching…") })
}

func TestLimitsSwitchReplaces(t *testing.T) {
	l := limitsApp(t)
	args := carolOnPitboard(t, l, `case "$*" in *--yes*) `+pitboardOK+`;;
*) echo '{"v":1,"command":"use","ok":false,"data":null,"warnings":[],"error":{"code":"live_login_refused","message":"x"}}'; exit 1;; esac`)
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("carol@example.com") })
	must(t, l.tt.Click("⋯"))
	must(t, l.tt.ChooseMenuItem("Switch to This Account"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Replace Claude Code's sign-in?") })
	must(t, l.tt.Click("Replace"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Switched to carol@example.com. Running sessions follow within 33 seconds.") })
	got, _ := os.ReadFile(args)
	if string(got) != "use\nclaude/Lãng\n--json\nuse\nclaude/Lãng\n--json\n--yes\n" {
		t.Errorf("pitboard ran with %q", got)
	}
}

func TestSwitchedText(t *testing.T) {
	for _, c := range []struct {
		s    account.Switched
		want string
	}{
		{account.Switched{Changed: true, Follow: 33 * time.Second}, "Switched to c@x. Running sessions follow within 33 seconds."},
		{account.Switched{Changed: true}, "Switched to c@x. Running sessions keep the old account until they're started again."},
		{account.Switched{}, "c@x is already in use."},
	} {
		if got := switchedText("c@x", c.s); got != c.want {
			t.Errorf("%+v: %q", c.s, got)
		}
	}
}
```

The "In use" alice block has no menu: it isn't added, and it has no label. So `Click("⋯")` reaches carol's.

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./view -run 'TestLimitsSwitch|TestSwitchedText'`
Expected: FAIL to compile: `undefined: switchedText`.

- [ ] **Step 3: Implement**

`view/limits.go`: the `limits` struct gains these fields after `removing`:

```go
	canSwitch bool            // Pitboard's command is here
	switching string          // the key of the account a switch runs to, or ""
	replacing bool            // the Replace confirmation is open
	target    account.Account // the account it would switch to
```

In `load`, the first goroutine also asks `CanSwitch`:

```go
		saved, err := a.Accounts.Blocks()
		can := a.Accounts.CanSwitch()
		a.Update(func() {
			if !l.loaded {
				l.blocks = saved
			}
			l.canSwitch = can
			l.toast(a, err)
		})
```

In `block`, after the `Source` footer:

```go
		if l.switching == b.Key() {
			ui.Text(c, "Switching…").FontSize(12).TextColor(pal.muted)
		}
```

`menu` becomes:

```go
// menu holds an account's actions: switching to it, when Pitboard parks
// it, and for an added account, signing in again and removing it.
func (l *limits) menu(c *ui.Context, a *App, b account.Account) {
	switchable := l.canSwitch && b.PitboardLabel != "" && !b.InUse
	if !b.Added && !switchable {
		return
	}
	ui.MenuButton(c, "⋯", func(m *ui.Menu) {
		if switchable && m.Item("Switch to This Account").Disabled(l.switching != "").Chosen() {
			l.startSwitch(a, b, false)
		}
		if b.Added && m.Item("Sign In Again").Chosen() {
			l.startAdd(a)
		}
		if b.Added && m.Item("Remove…").Destructive(true).Chosen() {
			l.removing, l.asking = b, true
		}
	}).Tooltip("Actions for " + b.Email)
}

// startSwitch has Pitboard put Claude Code on b's account. replace answers
// yes to replacing a sign-in Anthropic refuses for good.
func (l *limits) startSwitch(a *App, b account.Account, replace bool) {
	l.switching = b.Key()
	a.Go(func() {
		s, err := a.Accounts.Switch(context.Background(), b.PitboardLabel, replace)
		a.Update(func() {
			l.switching = ""
			switch {
			case errors.Is(err, account.ErrLiveRefused):
				l.target, l.replacing = b, true
				return
			case err != nil:
				l.toast(a, err)
			default:
				a.notice = switchedText(b.Email, s)
			}
			l.load(a, true)
		})
	})
}

// switchedText is the toast after a switch to email.
func switchedText(email string, s account.Switched) string {
	switch {
	case !s.Changed:
		return email + " is already in use."
	case s.Follow > 0:
		return fmt.Sprintf("Switched to %s. Running sessions follow within %d seconds.", email, int(s.Follow.Seconds()))
	}
	return "Switched to " + email + ". Running sessions keep the old account until they're started again."
}
```

Add `"errors"` to the imports of `view/limits.go`. In `dialogs`, after the Remove `AlertDialog`:

```go
	msg = "Claude Code's sign-in no longer works, so it can't be put away. Put " + l.target.Email + "'s sign-in in its place?"
	if ui.AlertDialog(c, &l.replacing, "Replace Claude Code's sign-in?", msg, "Cancel", "Replace") == 1 {
		l.startSwitch(a, l.target, true)
	}
```

Update the comment on `dialogs`: "dialogs are the Add Account dialog, the Remove confirmation and the Replace confirmation."

`view/app.go`: both header buttons are off while a switch runs:

```go
			if ui.Button(c, "Add Account…").Disabled(a.Accounts == nil || a.limits.switching != "").Clicked() {
				a.limits.startAdd(a)
			}
			if ui.Button(c, "Refresh").Disabled(a.Accounts == nil || a.limits.loading || a.limits.switching != "").Clicked() {
				a.limits.load(a, true)
			}
```

`main.go`, in `accounts(...)`'s `Book` literal:

```go
		PitboardCLI:   account.FindPitboard,
```

`DESIGN.md`, Account Block header bullet: replace "and, for an added account, a "⋯" menu with Sign In Again and Remove…." with "and a "⋯" menu: Switch to This Account for an account Pitboard parks, while Pitboard is installed, and Sign In Again and Remove… for an added account." Footer: replace "A Pitboard-only account has no "⋯" menu." with "A switch under way ends the block with "Switching…", muted, at 12px."

- [ ] **Step 4: Run them and watch them pass**

Run: `go test ./view -run 'TestLimits|TestSwitchedText' -count=1`
Expected: PASS.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l account view main.go   # prints nothing
go vet ./... && go test ./... -count=1
git add view/limits.go view/limits_test.go view/app.go main.go DESIGN.md
git commit -m "feat(view): Switch to This Account on the Limits page

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Evidence (no real switch)

**Files:**
- Modify: `view/screenshot_test.go` (`TestLimitsScreenshot` shows the menu's block with "Switching…")

- [ ] **Step 1:** In `TestLimitsScreenshot`, give the book a Pitboard account with a label and a `PitboardCLI` that runs `sleep 5`. Choose Switch, and save one more image, `limits-switching.png`, once "Switching…" shows. Keep `SCREENSHOT_DIR` gating as it is.
- [ ] **Step 2:** Run `SCREENSHOT_DIR=<scratchpad>/shots-switch go test ./view -run TestLimitsScreenshot -count=1`. Expected: PASS, and the PNG exists. Look at it.
- [ ] **Step 3:** Build: `mygo build`. Expected: exit 0.
- [ ] **Step 4:** Read-only real check: run `pitboard use claude/__nope__ --json` once and record the envelope for an unknown label. Expected: `ok` false and an error code. This changes nothing, because the label is unknown. Do NOT run a real switch: the user does it.
- [ ] **Step 5:** Commit the screenshot test change:

```bash
git add view/screenshot_test.go
git commit -m "test(view): screenshot a switch under way

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
