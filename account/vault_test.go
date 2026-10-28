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
