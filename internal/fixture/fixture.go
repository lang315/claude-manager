// Package fixture writes a synthetic Claude Code config directory for tests.
// Its sessions have the shape of real ones but no real data.
package fixture

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Now is the tests' clock.
var Now = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

const (
	Old           = "11111111-1111-4111-8111-111111111111" // 60 days old, titled "Login bug fix", one broken line, one subagent
	Late          = "22222222-2222-4222-8222-222222222222" // 2 days old, its ai-title past the first 64 KB
	Live          = "33333333-3333-4333-8333-333333333333" // 60 days old, live: sessions/4242.json, PID 4242 alive
	OrphanDir     = "44444444-4444-4444-8444-444444444444" // side dir under Proj without a .jsonl
	Recent        = "55555555-5555-4555-8555-555555555555" // in Other, modified a minute before Now
	OrphanEnv     = "66666666-6666-4666-8666-666666666666" // session-env entry without a session
	OrphanHistory = "99999999-9999-4999-8999-999999999999" // file-history entry without a session
	NoSession     = "77777777-7777-4777-8777-777777777777" // a prompt's session that is not on disk

	Proj  = "projects/-Users-me-proj"
	Other = "projects/-Users-me-other"
)

// Alive is the tests' PID check: only PID 4242 runs.
func Alive(pid int) bool { return pid == 4242 }

// Dirs are where New wrote the config directory and .claude.json.
type Dirs struct{ Root, ClaudeJSON string }

var oldSession = strings.Join([]string{
	`{"type":"permission-mode","permissionMode":"default","sessionId":"` + Old + `"}`,
	`{"type":"user","isMeta":true,"cwd":"/Users/me/proj","gitBranch":"main","version":"2.1.300","timestamp":"2026-08-27T10:00:00.000Z","message":{"role":"user","content":"<local-command-caveat>ignore</local-command-caveat>"}}`,
	`{"type":"user","cwd":"/Users/me/proj","gitBranch":"main","version":"2.1.300","timestamp":"2026-08-27T10:00:01.000Z","message":{"role":"user","content":"Fix the login bug\nIt fails on Safari"}}`,
	`{"type":"assistant","timestamp":"2026-08-27T10:00:02.000Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"Look at auth.go"},{"type":"text","text":"Reading **auth.go**."},{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"auth.go"}}]}}`,
	`{"type":"user","timestamp":"2026-08-27T10:00:03.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"package auth"}]}]}}`,
	`{not json`,
	`{"type":"attachment","attachment":{"type":"hook"}}`,
	`{"type":"ai-title","aiTitle":"Login bug fix","sessionId":"` + Old + `"}`,
	`{"type":"user","timestamp":"2026-08-27T10:00:04.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"plain result"}]}}`,
}, "\n") + "\n"

var lateSession = fmt.Sprintf(`{"type":"user","cwd":"/Users/me/proj","timestamp":"2026-10-24T08:00:00.000Z","message":{"role":"user","content":"Paste: %s"}}`, strings.Repeat("x", 70000)) +
	"\n" + `{"type":"ai-title","aiTitle":"Late title","sessionId":"` + Late + `"}` + "\n"

// history is history.jsonl: prompts in two projects, newest on line 2, a
// broken line 3, an inline paste and one stored only by hash, an image
// placeholder and a prompt without a time.
var history = strings.Join([]string{
	`{"display":"Old idea","pastedContents":{},"timestamp":1787824800000,"project":"/Users/me/proj","sessionId":"` + Old + `"}`,
	`{"display":"Fix the login bug","pastedContents":{},"timestamp":1793008800000,"project":"/Users/me/proj","sessionId":"` + Old + `"}`,
	`{not json`,
	`{"display":"Review this:\n[Pasted text #1 +2 lines]","pastedContents":{"1":{"id":1,"type":"text","content":"func main() {\n}"},"3":{"id":3,"type":"text","content":"unused paste"}},"timestamp":1792746000000,"project":"/Users/me/other","sessionId":"` + Recent + `"}`,
	`{"display":"Use this [Pasted text #2 +40 lines] and [Image #1]","pastedContents":{"2":{"id":2,"type":"text","contentHash":"abc123"}},"timestamp":1791277200000,"project":"/Users/me/proj","sessionId":"` + NoSession + `"}`,
	`{"display":"No time prompt","pastedContents":{},"project":"/Users/me/other","sessionId":"` + Recent + `"}`,
}, "\n") + "\n"

// New writes the config directory into a temporary home. Every path is 60
// days old but a few, which are newer on purpose.
func New(t testing.TB) Dirs {
	t.Helper()
	home := t.TempDir()
	d := Dirs{Root: filepath.Join(home, ".claude"), ClaudeJSON: filepath.Join(home, ".claude.json")}
	write := func(path, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		Proj + "/" + Old + ".jsonl":                    oldSession,
		Proj + "/" + Old + "/subagents/agent-a1.jsonl": "{}\n",
		Proj + "/" + Old + "/tool-results/r.txt":       "result",
		Proj + "/" + Late + ".jsonl":                   lateSession,
		Proj + "/" + Live + ".jsonl":                   `{"type":"user","cwd":"/Users/me/proj","timestamp":"2026-08-27T09:00:00.000Z","message":{"role":"user","content":[{"type":"text","text":"Live work"}]}}` + "\n",
		Proj + "/" + OrphanDir + "/tool-results/a.txt": "orphan",
		Proj + "/memory/MEMORY.md":                     "# Memory\n- likes Go\n",
		Proj + "/mygo-memory/MEMORY.md":                "# mygo\n",
		Proj + "/x.wakatime":                           "",
		Other + "/" + Recent + ".jsonl":                `{"type":"user","cwd":"/Users/me/other","timestamp":"2026-10-26T11:58:00.000Z","message":{"role":"user","content":"Recent thing"}}` + "\n",
		"sessions/4242.json":                           `{"pid":4242,"sessionId":"` + Live + `"}`,
		"sessions/9999.json":                           `{"pid":9999,"sessionId":"` + Old + `"}`,
		"file-history/" + Old + "/v1":                  "a",
		"file-history/" + OrphanHistory + "/v1":        "b",
		"file-history/.DS_Store":                       "x",
		"session-env/" + Old + "/env":                  "1",
		"session-env/" + Live + "/env":                 "2",
		"session-env/" + OrphanEnv + "/env":            "3",
		"paste-cache/p1.txt":                           "paste",
		"shell-snapshots/s.sh":                         "snap",
		"debug/d.txt":                                  "debug",
		"transcripts/old.jsonl":                        "{}\n",
		"transcripts/new.jsonl":                        "{}\n",
		"settings.json":                                `{"env":{"API_TOKEN":"secret123","EDITOR":"vim"},"hooks":{"Stop":[{"hooks":[{"type":"command","command":"say done"}]}]},"mcpServers":{"local":{"command":"local-mcp"}}}`,
		"history.jsonl":                                history,
		"settings.json.bak":                            "{}",
		"settings.json.bak-1":                          "{}",
		"skills/demo/SKILL.md":                         "---\nname: demo\ndescription: A demo skill\n---\n# Demo\n",
		"commands/hello.md":                            "Say hello\n",
		"CLAUDE.md":                                    "# Global rules\n",
		"plugins/installed_plugins.json":               `{"plugins":{"demo@market":[{"version":"1.0.0"}]}}`,
		"plugins/known_marketplaces.json":              `{"market":{"source":"github"}}`,
	}
	for rel, content := range files {
		write(filepath.Join(d.Root, rel), content)
	}
	write(d.ClaudeJSON, `{"mcpServers":{"remote":{"type":"http","url":"https://example.com/mcp","headers":{"Authorization":"Bearer abc"}}}}`)

	old := Now.AddDate(0, 0, -60)
	err := filepath.WalkDir(d.Root, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(p, old, old)
	})
	if err != nil {
		t.Fatal(err)
	}
	for rel, mod := range map[string]time.Time{
		Proj + "/" + Late + ".jsonl":    Now.AddDate(0, 0, -2),
		Other + "/" + Recent + ".jsonl": Now.Add(-time.Minute),
		"transcripts/new.jsonl":         Now.AddDate(0, 0, -2),
	} {
		if err := os.Chtimes(filepath.Join(d.Root, rel), mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	return d
}
