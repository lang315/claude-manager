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
	ownGroup(cmd)
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
// after dir, or in dir's .credentials.json; and the oauthAccount claude
// wrote in dir's .claude.json.
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
	conf, _ := os.ReadFile(filepath.Join(dir, ".claude.json"))
	acct := oauthAccountOf(conf)
	if keyOf(acct) != p.Key() {
		acct = nil
	}
	return b.keep(blob, p, acct)
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
	home, _ := os.UserHomeDir()
	return findTool("claude", "Claude Code not found.",
		filepath.Join(home, ".local", "bin", "claude"), "/opt/homebrew/bin/claude", "/usr/local/bin/claude")
}

// findTool finds name as a login shell would, else at the first of places
// that is a file.
func findTool(name, notFound string, places ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/zsh", "-lc", "command -v "+name)
	cmd.WaitDelay = time.Second // a child the profile leaves may hold stdout open
	if out, err := cmd.Output(); err == nil {
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
