package fixture

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Claude scripts a stand-in for `claude auth login --claudeai`: the test
// binary, linked as claude and run again, so it also runs where there is no sh.
type Claude struct {
	Link         string // the sign-in link it prints
	Escapes      bool   // wrapped in bold and an OSC 8 hyperlink, as a terminal gets it
	Login        string // a file it copies to .credentials.json on the code "good"
	OAuthAccount string // written to .claude.json on the code "good"
	Sleep        bool   // prints the link, then waits to be killed
}

// FakeClaude returns the path of a claude that behaves as c says.
func FakeClaude(t *testing.T, c Claude) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	if runtime.GOOS == "windows" {
		bin += ".exe" // exec finds no other name
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Link(self, bin); err != nil { // another volume: copy
		in, err := os.ReadFile(self)
		if err == nil {
			err = os.WriteFile(bin, in, 0o755)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	cfg, _ := json.Marshal(c)
	if err := os.WriteFile(filepath.Join(dir, "claude.json"), cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	return bin
}

// RunFakeClaude is TestMain's first line: when the binary was started as a
// FakeClaude, it plays claude and exits.
func RunFakeClaude() {
	if strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe") != "claude" {
		return
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(os.Args[0]), "claude.json"))
	var c Claude
	if err != nil || json.Unmarshal(raw, &c) != nil {
		return
	}
	if strings.Join(os.Args[1:], " ") != "auth login --claudeai" {
		fmt.Fprintln(os.Stderr, "bad args:", strings.Join(os.Args[1:], " "))
		os.Exit(2)
	}
	if c.Escapes {
		fmt.Println("Opening browser to sign in…")
		fmt.Printf("Visit: \x1b[1m\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\\x1b[0m\n", c.Link, c.Link)
	} else {
		fmt.Println("Visit: " + c.Link)
	}
	if c.Sleep {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	fmt.Print("Paste code here if prompted > ")
	code, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.TrimSpace(code) != "good" {
		fmt.Fprintln(os.Stderr, "Invalid code")
		os.Exit(1)
	}
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if c.Login != "" {
		err := copyFile(c.Login, filepath.Join(dir, ".credentials.json"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if c.OAuthAccount != "" {
		os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"oauthAccount":`+c.OAuthAccount+`}`+"\n"), 0o600)
	}
	if c.Escapes {
		fmt.Println("Login successful.")
	}
	os.Exit(0)
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}
