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
	e := next()
	for !e.Done { // under load the timeout can come before the link
		e = next()
	}
	if e.Err == nil || e.Err.Error() != "The sign-in took too long." {
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
