package account

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lang315/claude-manager/internal/fixture"
)

// The console runs the claude it was given, at folder, with configDir and
// without the auth overrides, cmd's & and % in the paths taken literally.
func TestLaunchCmdRunsClaude(t *testing.T) {
	base := t.TempDir()
	record := filepath.Join(base, "run.json")
	stub := fixture.FakeClaude(t, fixture.Claude{Record: record})
	dir := filepath.Join(base, "Program Files", "claude code") // spaces in claude's path
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{filepath.Base(stub), "claude.json"} {
		if err := os.Rename(filepath.Join(filepath.Dir(stub), f), filepath.Join(dir, f)); err != nil {
			t.Fatal(err)
		}
	}
	claude := filepath.Join(dir, filepath.Base(stub))
	folder := filepath.Join(base, "a & b %PATH%")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	configDir := `C:\cfg %USERNAME% & echo hi`
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-api03-fake")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "fake")

	cmd, err := launchCmd(claude, folder, configDir, "--model", "opus plan", "--resume")
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var r fixture.Run
	deadline := time.Now().Add(20 * time.Second)
	for {
		raw, err := os.ReadFile(record)
		if err == nil && json.Unmarshal(raw, &r) == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("claude never ran: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Cleanup(func() { // cmd /k stays open on folder; TempDir's removal needs it gone
		if p, err := os.FindProcess(r.PPID); err == nil {
			p.Kill()
			time.Sleep(200 * time.Millisecond)
		}
	})
	if want := []string{"--model", "opus plan", "--resume"}; !slices.Equal(r.Args, want) {
		t.Errorf("args %q, want %q", r.Args, want)
	}
	if a, err1 := os.Stat(r.Dir); err1 != nil {
		t.Errorf("dir %q: %v", r.Dir, err1)
	} else if b, _ := os.Stat(folder); !os.SameFile(a, b) {
		t.Errorf("dir %q, want %q", r.Dir, folder)
	}
	if r.ConfigDir != configDir {
		t.Errorf("CLAUDE_CONFIG_DIR %q, want %q", r.ConfigDir, configDir)
	}
	if len(r.Overrides) > 0 {
		t.Errorf("auth overrides reached claude: %v", r.Overrides)
	}
}

func TestLaunchCmdInUseKeepsEnvironment(t *testing.T) {
	cmd, err := launchCmd(`C:\claude.exe`, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Env != nil {
		t.Errorf("the account in use's console got its own environment: %d vars", len(cmd.Env))
	}
}

func TestLaunchCmdRefusesWhatCmdActsOn(t *testing.T) {
	for _, p := range []string{`C:\a&b\claude.exe`, `C:\a|b\claude.exe`, `C:\a^b\claude.exe`, `C:\%x%\claude.exe`, `C:\a"b\claude.exe`} {
		if _, err := launchCmd(p, t.TempDir(), ""); err == nil || !strings.Contains(err.Error(), "path of claude") {
			t.Errorf("path %q: %v", p, err)
		}
	}
	for _, a := range []string{"a&b", "%PATH%", "a|b", "x^y", `"q"`, "(x)", "!x!", "a\nb"} {
		if _, err := launchCmd(`C:\claude.exe`, t.TempDir(), "", a); err == nil || !strings.Contains(err.Error(), "argument") {
			t.Errorf("arg %q: %v", a, err)
		}
	}
}
