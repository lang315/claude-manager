package account

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// claude-swap's atomic_write_bytes: a login file is private whatever mode
// it had, and a dotfile-managed symlink stays a link to the new content.
func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "creds.json")
	must(t, os.WriteFile(p, []byte("old"), 0o644))
	must(t, os.Chmod(p, 0o644)) // whatever the umask
	must(t, writeAtomic(p, []byte("new"), 0o600))
	if fi, _ := os.Stat(p); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	real := filepath.Join(dir, "dotfiles", "claude.json")
	must(t, os.MkdirAll(filepath.Dir(real), 0o700))
	must(t, os.WriteFile(real, []byte("{}"), 0o600))
	link := filepath.Join(dir, ".claude.json")
	must(t, os.Symlink(real, link))
	must(t, writeAtomic(link, []byte(`{"a":1}`), 0o600))
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced by a file")
	}
	if got, _ := os.ReadFile(real); string(got) != `{"a":1}` {
		t.Errorf("the link's target holds %q", got)
	}
}
