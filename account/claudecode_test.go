package account

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDropKey(t *testing.T) {
	for _, c := range []struct{ in, key, want string }{
		{`{"a":1,"primaryApiKey":"k","b":2}`, "primaryApiKey", `{"a":1,"b":2}`},
		{"{\n  \"primaryApiKey\": \"k\",\n  \"b\": 2\n}", "primaryApiKey", "{\n  \"b\": 2\n}"},
		{`{"a":1,"primaryApiKey":"k"}`, "primaryApiKey", `{"a":1}`},
		{`{"primaryApiKey":"k"}`, "primaryApiKey", `{}`},
		{`{"a":{"primaryApiKey":1}}`, "primaryApiKey", `{"a":{"primaryApiKey":1}}`},
	} {
		got, err := dropKey([]byte(c.in), c.key)
		if err != nil || string(got) != c.want {
			t.Errorf("dropKey(%s) = %s, %v; want %s", c.in, got, err, c.want)
		}
	}
	if _, err := dropKey([]byte(`[]`), "a"); err == nil {
		t.Error("not an object")
	}
}

// Claude Code 2.1.299's pick, as claude-swap's _pick_global_config: a
// legacy <config home>/.config.json that is a non-empty file wins over
// .claude.json; so does an empty writable one when .claude.json is absent.
func TestGlobalConfig(t *testing.T) {
	home := t.TempDir()
	root, global := filepath.Join(home, ".claude"), filepath.Join(home, ".claude.json")
	must(t, os.Mkdir(root, 0o700))
	if got := GlobalConfig(root, global); got != global {
		t.Errorf("no legacy file: %s", got)
	}
	legacy := filepath.Join(root, ".config.json")
	must(t, os.WriteFile(legacy, nil, 0o600))
	if got := GlobalConfig(root, global); got != legacy {
		t.Errorf("an empty legacy file, no .claude.json: %s", got)
	}
	must(t, os.WriteFile(global, []byte("{}"), 0o600))
	if got := GlobalConfig(root, global); got != global {
		t.Errorf("an empty legacy file beside .claude.json: %s", got)
	}
	must(t, os.WriteFile(legacy, []byte("{}"), 0o600))
	if got := GlobalConfig(root, global); got != legacy {
		t.Errorf("a non-empty legacy file: %s", got)
	}
}

// claude-swap makes a lock's parent folder: a first switch with no
// ~/.claude yet locks as well as any.
func TestLockMakesItsFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "none", "yet", ".lock")
	free, err := lock(t.Context(), dir, configStale)
	if err != nil {
		t.Fatal(err)
	}
	free()
}
