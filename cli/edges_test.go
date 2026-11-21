package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lang315/claude-manager/account"
)

func TestRemoveConfirms(t *testing.T) {
	a := newApp(t)
	if code, _, _ := a.run(t, "n\n", "remove", "bob@example.com"); code != failed {
		t.Error("removed on no")
	}
	if code, _, _ := a.run(t, "", "remove", "bob@example.com"); code != failed {
		t.Error("removed with no answer")
	}
	a.run(t, "", "list") // alice is followed
	_, _, errs := a.run(t, "", "remove", "alice@example.com")
	if !strings.Contains(errs, "is the account Claude Code uses") {
		t.Errorf("no warning: %q", errs)
	}
	if code, out, _ := a.run(t, "", "remove", "bob@example.com", "--yes"); code != ok || !strings.Contains(out, "Removed bob") {
		t.Errorf("--yes: %d %s", code, out)
	}
}

func TestSwitchNotices(t *testing.T) {
	a := newApp(t)
	a.run(t, "", "list")
	if _, out, _ := a.run(t, "", "switch", "alice@example.com"); !strings.Contains(out, "Already on alice@example.com") {
		t.Errorf("already %q", out)
	}
	if _, out, _ := a.run(t, "", "switch", "bob@example.com"); !strings.Contains(out, "within about 30 seconds") {
		t.Errorf("restart hint %q", out)
	}
	b := newApp(t)
	must(t, b.v.Put("Claude Code-credentials", "me", []byte(`{"claudeAiOauth":{"accessToken":"nobody-at","refreshToken":"nobody-rt","expiresAt":1}}`)))
	if _, _, errs := b.run(t, "", "switch", "bob@example.com"); !strings.Contains(errs, "kept as unclaimed") {
		t.Errorf("unclaimed notice %q", errs)
	}
}

func TestAmbiguousAsks(t *testing.T) {
	a := newApp(t)
	must(t, os.WriteFile(filepath.Join(a.b.Dir, "accounts.json"), []byte(`{"accounts":[`+
		`{"accountUuid":"bob","orgUuid":"org","email":"bob@example.com","slot":1,"oauthAccount":{"accountUuid":"bob","organizationUuid":"org"}},`+
		`{"accountUuid":"bob","orgUuid":"org2","email":"bob@example.com","slot":2,"oauthAccount":{"accountUuid":"bob","organizationUuid":"org2"}}]}`), 0o600))
	if code, _, _ := a.run(t, "2\ny\n", "remove", "bob@example.com"); code != ok {
		t.Fatal("not removed by number")
	}
	accts, _ := a.b.Blocks()
	for _, x := range accts {
		if x.OrgUUID == "org2" {
			t.Error("the second wasn't the one removed")
		}
	}
	b := newApp(t)
	must(t, os.WriteFile(filepath.Join(b.b.Dir, "accounts.json"), []byte(`{"accounts":[{"accountUuid":"x","orgUuid":"1","email":"e@x"},{"accountUuid":"x","orgUuid":"2","email":"e@x"}]}`), 0o600))
	if code, _, errs := b.run(t, "9\n", "switch", "e@x"); code != failed || !strings.Contains(errs, "no account chosen") {
		t.Errorf("bad number: %d %s", code, errs)
	}
}

func TestListTagsAndWarnings(t *testing.T) {
	a := newApp(t)
	must(t, a.v.Put(account.ParkService, "carol/org", login("bob")))
	must(t, os.WriteFile(filepath.Join(a.b.Dir, "accounts.json"), []byte(`{"accounts":[`+
		`{"accountUuid":"bob","orgUuid":"org","email":"bob@example.com","alias":"dev","slot":1,"oauthAccount":{"accountUuid":"bob","organizationUuid":"org"}},`+
		`{"accountUuid":"carol","orgUuid":"org","email":"carol@example.com","orgName":"Acme","plan":"team","slot":2}]}`), 0o600))
	_, out, _ := a.run(t, "", "list")
	if !strings.Contains(out, "bob@example.com (dev) [personal]") || !strings.Contains(out, "carol@example.com [Acme]") {
		t.Errorf("tags %q", out)
	}
	if !strings.Contains(out, "hold the same login") {
		t.Errorf("no duplicate warning: %q", out)
	}
	c := newApp(t)
	must(t, c.b.Remove("bob/org"))
	must(t, c.v.Delete("Claude Code-credentials", "me"))
	if _, out, _ := c.run(t, "", "list"); !strings.Contains(out, "No accounts are added yet") {
		t.Errorf("empty %q", out)
	}
}

func TestSessionShellRefused(t *testing.T) {
	a := newApp(t)
	getenv = func(k string) string {
		if k == "CLAUDE_CONFIG_DIR" {
			return filepath.Join(account.ProfilesDir(a.b.Dir), "abc")
		}
		return ""
	}
	t.Cleanup(func() { getenv = os.Getenv })
	if code, _, errs := a.run(t, "", "switch", "bob@example.com"); code != failed || !strings.Contains(errs, "normal shell") {
		t.Errorf("switch: %d %s", code, errs)
	}
	if code, _, _ := a.run(t, "", "list"); code != ok {
		t.Error("list refused")
	}
	// auto switches this profile's login in place, as switch would.
	if code, _, errs := a.run(t, "", "auto", "--once"); code != failed || !strings.Contains(errs, "normal shell") {
		t.Errorf("auto: %d %s", code, errs)
	}
	if _, _, errs := a.run(t, "", "auto", "--once", "--dry-run"); strings.Contains(errs, "normal shell") {
		t.Errorf("auto --dry-run refused: %s", errs)
	}
}

func TestAddSlotAsks(t *testing.T) {
	a := newApp(t)
	if code, _, _ := a.run(t, "n\n", "add", "--slot", "1"); code != failed {
		t.Error("moved bob without asking")
	}
	if code, out, _ := a.run(t, "y\n", "add", "--slot", "1"); code != ok || !strings.Contains(out, "slot 1") {
		t.Errorf("yes: %d %s", code, out)
	}
	if code, out, _ := a.run(t, "", "add-token", "sk-ant-oat01-x", "--slot", "1", "--yes"); code != ok || !strings.Contains(out, "Added") {
		t.Errorf("add-token --slot: %d %s", code, out)
	}
	accts, _ := a.b.Blocks()
	for _, x := range accts {
		if x.Token && x.Slot != 1 {
			t.Errorf("token in slot %d", x.Slot)
		}
	}
	// The token and the answer down one pipe: the token's read keeps the
	// answer for the question.
	b := newApp(t)
	if code, out, errs := b.run(t, "sk-ant-oat01-y\ny\n", "add-token", "-", "--slot", "1"); code != ok || strings.Contains(out, "in slot") {
		t.Errorf("piped add-token --slot: %d %s %s", code, out, errs)
	}
}
