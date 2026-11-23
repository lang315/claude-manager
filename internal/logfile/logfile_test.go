package logfile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRotates(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	f := &File{Path: p, Max: 10, Keep: 2}
	for _, s := range []string{"aaaaaaaa\n", "bbbbbbbb\n", "cccccccc\n", "dddddddd\n"} {
		if _, err := f.Write([]byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	read := func(s string) string { b, _ := os.ReadFile(s); return string(b) }
	if read(p) != "dddddddd\n" || read(p+".1") != "cccccccc\n" || read(p+".2") != "bbbbbbbb\n" {
		t.Errorf("%q %q %q", read(p), read(p+".1"), read(p+".2"))
	}
	if _, err := os.Stat(p + ".3"); err == nil {
		t.Error("kept more than Keep")
	}
	if fi, _ := os.Stat(p); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 || strings.Contains(read(p), "a") {
		t.Error("mode or content")
	}
}
