package account

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"
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
	// File, when set, is the keychain file every command reads and writes,
	// rather than the user's: the end-to-end test's own keychain.
	File string
}

func (k Keychain) Get(service, acct string) ([]byte, error) {
	out, err := k.run("the read", "", k.in(args("find-generic-password", service, acct, "-w"))...)
	for try := 0; try < 2 && err != nil && !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrKeychainSlow); try++ {
		time.Sleep(300 * time.Millisecond) // claude-swap's retry of a read that failed once
		out, err = k.run("the read", "", k.in(args("find-generic-password", service, acct, "-w"))...)
	}
	if err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(out, []byte("\n")), nil
}

// stdinLimit is the longest line security -i reads whole.
const stdinLimit = 4096 - 64

// Put sends its command to security -i on stdin, so the secret is on no
// argument line another process could read. A secret too long for one
// line goes on the argument line, hex-encoded, as claude-swap does: the
// alternative is a cut-off login.
func (k Keychain) Put(service, acct string, secret []byte) error {
	if strings.ContainsAny(service+acct, "\n\r") {
		return fmt.Errorf("The keychain item %q %q can't be written.", service, acct)
	}
	hexed := hex.EncodeToString(secret)
	file := ""
	if k.File != "" {
		file = " " + quote(k.File)
	}
	cmd := fmt.Sprintf("add-generic-password -U -s %s -a %s -X %s%s\n", quote(service), quote(acct), hexed, file)
	if len(cmd) > stdinLimit {
		_, err := k.run("the write", "", k.in([]string{"add-generic-password", "-U", "-s", service, "-a", acct, "-X", hexed})...)
		return err
	}
	_, err := k.run("the write", cmd, "-i")
	return err
}

// quote quotes s for a security -i line, which is split as a shell would.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func (k Keychain) Delete(service, acct string) error {
	_, err := k.run("the removal", "", k.in(args("delete-generic-password", service, acct))...)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// in names k.File last on a command line, as security takes a keychain.
func (k Keychain) in(a []string) []string {
	if k.File == "" {
		return a
	}
	return append(a, k.File)
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
	// A test on a keychain of its own sets the guard to that keychain's
	// path: a command naming no such keychain would reach the user's.
	if g := os.Getenv("CLAUDE_MANAGER_KEYCHAIN_GUARD"); g != "" && !slices.Contains(arg, g) && !strings.Contains(stdin, quote(g)) {
		return nil, fmt.Errorf("The keychain command names no %s: refused, to keep the user's keychain out of a test.", g)
	}
	ctx, cancel := context.WithTimeout(context.Background(), keychainTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, arg...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, msg bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &msg
	err := cmd.Run()
	text := msg.String()
	switch {
	case ctx.Err() != nil:
		return nil, ErrKeychainSlow
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

// keychainTimeout bounds a security call, as claude-swap's: a prompt
// nobody answers can't hold the book.
var keychainTimeout = 5 * time.Second

var ErrKeychainSlow = errors.New("The keychain didn't answer in time: it may be locked, or waiting on a prompt.")

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
