package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// cswap list's rows: a window's reset clock and countdown, (ahead of
// pace), (!) at 100, extra usage in money, the login's expiry, (disabled).
func TestListRows(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.Local)
	a := account.Account{Email: "a@x", Added: true, AutoOff: true, ReadAt: now, GoodUntil: now.Add(5*24*time.Hour + 12*time.Hour),
		Limits: []account.Limit{
			{Kind: "session", Percent: 62, ResetsAt: now.Add(2*time.Hour + 13*time.Minute)},
			{Kind: "weekly_scoped", Scope: "Fable", Percent: 100, ResetsAt: now.Add(26 * time.Hour)},
			{Kind: "extra_usage", Percent: 40, Used: 20, Cap: 50, Currency: "USD"},
		}}
	got := strings.Join(listRows(a, now), "\n")
	for _, want := range []string{"62%", "resets 14:13", "in 2h 13m", "(!)", "$20.00 / $50.00", "login", "in 5d 12h"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
	if !strings.Contains(listHead(a), "(disabled)") {
		t.Errorf("head %q", listHead(a))
	}
}

// cswap's lockstep warning: two accounts reading the same 5h and 7d use
// and resets are likely one account.
func TestLockstep(t *testing.T) {
	now := time.Now()
	l := []account.Limit{{Kind: "session", Percent: 33, ResetsAt: now.Add(time.Hour)}, {Kind: "weekly_all", Percent: 12, ResetsAt: now.Add(48 * time.Hour)}}
	accts := []account.Account{{Email: "a@x", Added: true, Limits: l}, {Email: "b@x", Added: true, Limits: l}, {Email: "c@x", Added: true, Limits: l[:1]}}
	if got := lockstep(accts); len(got) != 1 || got[0] != [2]string{"a@x", "b@x"} {
		t.Errorf("lockstep %v", got)
	}
}

// cswap's tolerance: equal percents, resets within 5s (_LOCKSTEP_RESET_TOLERANCE_S).
func TestLockstepTolerance(t *testing.T) {
	now := time.Now()
	mk := func(email string, pct float64, d5, d7 time.Duration) account.Account {
		return account.Account{Email: email, Added: true, Limits: []account.Limit{
			{Kind: "session", Percent: pct, ResetsAt: now.Add(time.Hour + d5)}, {Kind: "weekly_all", Percent: 12, ResetsAt: now.Add(48*time.Hour + d7)}}}
	}
	for name, c := range map[string]struct {
		b    account.Account
		want int
	}{
		"within 5s":       {mk("b@x", 33, 5*time.Second, -5*time.Second), 1},
		"5h reset 6s":     {mk("b@x", 33, 6*time.Second, 0), 0},
		"7d reset 6s":     {mk("b@x", 33, 0, 6*time.Second), 0},
		"percent differs": {mk("b@x", 34, 0, 0), 0},
	} {
		if got := lockstep([]account.Account{mk("a@x", 33, 0, 0), c.b}); len(got) != c.want {
			t.Errorf("%s: %v", name, got)
		}
	}
	// One warning per slot though several earlier ones match, as cswap's break.
	if got := lockstep([]account.Account{mk("a@x", 33, 0, 0), mk("b@x", 33, time.Second, 0), mk("c@x", 33, 2*time.Second, 0)}); len(got) != 2 || got[1] != [2]string{"a@x", "c@x"} {
		t.Errorf("break: %v", got)
	}
}

// cswap's usageStatus kinds, for scripts to tell why there is no usage.
func TestUsageStatus(t *testing.T) {
	for want, a := range map[string]account.Account{
		"api_key":              {APIKey: true},
		"token_expired":        {Err: account.LiveExpired},
		"relogin_required":     {Err: account.ErrInvalidGrant.Error()},
		"keychain_unavailable": {Err: account.ErrLocked.Error()},
		"unavailable":          {Err: "Anthropic answered 500."},
	} {
		if got := toRow(a, time.Now()).Status; got != want {
			t.Errorf("%+v: %s, want %s", a, got, want)
		}
	}
}

// cswap's config: set and unset say what they did; yes and no are
// booleans; a count takes a whole number; path names the file.
func TestConfigAsCswap(t *testing.T) {
	a := newApp(t)
	if _, out, _ := a.run(t, "", "config", "set", "autoswitch.enabled", "yes"); out != "autoswitch.enabled = true\n" {
		t.Errorf("set yes: %q", out)
	}
	if _, out, _ := a.run(t, "", "config", "unset", "autoswitch.enabled"); out != "autoswitch.enabled unset (default: false)\n" {
		t.Errorf("unset: %q", out)
	}
	if code, _, errs := a.run(t, "", "config", "set", "autoswitch.unhealthyTicks", "3.5"); code != failed || !strings.Contains(errs, "whole number") {
		t.Errorf("3.5 ticks: %d %s", code, errs)
	}
	if code, _, errs := a.run(t, "", "config", "set", "autoswitch.model", ""); code != failed || !strings.Contains(errs, "config unset") {
		t.Errorf("empty model: %d %s", code, errs)
	}
	if _, out, _ := a.run(t, "", "config", "path"); !strings.HasSuffix(strings.TrimSpace(out), "accounts.json") {
		t.Errorf("path: %q", out)
	}
}

// cswap's update notice: after a command, on stderr, from a check made at
// most daily and retried an hour after a failure; never with --json.
func TestUpdateHint(t *testing.T) {
	a := newApp(t)
	calls := 0
	Newest = func(context.Context) (string, error) { calls++; return "9.9.9", nil }
	t.Cleanup(func() { Newest = nil })
	_, _, errs := a.run(t, "", "list")
	if !strings.Contains(errs, "9.9.9 is out") {
		t.Errorf("no notice: %q", errs)
	}
	a.run(t, "", "list")
	if calls != 1 {
		t.Errorf("asked the feed %d times in a day", calls)
	}
	if _, _, errs := a.run(t, "", "list", "--json"); strings.Contains(errs, "is out") {
		t.Error("a notice with --json")
	}
}

func TestConfigJSON(t *testing.T) {
	a := newApp(t)
	if _, out, _ := a.run(t, "", "config", "--json"); !strings.Contains(out, `"key":"autoswitch.threshold"`) || !strings.Contains(out, `"path":`) {
		t.Errorf("config --json: %s", out)
	}
}

// purge leaves nothing behind, the update notice's memo included; a
// negative --hold is refused, as cswap's.
// cswap's config --json: list names the flag isSet and has unrecognized;
// get --json is {schemaVersion, key, value, isSet}; a key set to its default
// is set, and unset clears it.
func TestConfigJSONIsSet(t *testing.T) {
	a := newApp(t)
	a.run(t, "", "config", "set", "autoswitch.threshold", "80")
	_, out, _ := a.run(t, "", "config", "list", "--json")
	var doc struct {
		Settings []struct {
			Key   string
			IsSet *bool `json:"isSet"`
			Set   *bool `json:"set"`
		}
		Unrecognized []any `json:"unrecognized"`
	}
	must(t, json.Unmarshal([]byte(out), &doc))
	if doc.Unrecognized == nil || len(doc.Unrecognized) != 0 || strings.Contains(out, `"set":`) {
		t.Errorf("list: %s", out)
	}
	for _, s := range doc.Settings {
		if s.IsSet == nil || *s.IsSet != (s.Key == "autoswitch.threshold") {
			t.Errorf("isSet of %s: %s", s.Key, out)
		}
	}
	_, out, _ = a.run(t, "", "config", "get", "autoswitch.threshold", "--json")
	if want := `{"isSet":true,"key":"autoswitch.threshold","schemaVersion":1,"value":"80"}` + "\n"; out != want {
		t.Errorf("get --json: %q", out)
	}
	_, out, _ = a.run(t, "", "config", "get", "ui.theme", "--json")
	if !strings.Contains(out, `"isSet":false`) {
		t.Errorf("get unset --json: %q", out)
	}
	a.run(t, "", "config", "set", "ui.theme", "auto") // the default, stored
	if _, out, _ = a.run(t, "", "config", "get", "ui.theme", "--json"); !strings.Contains(out, `"isSet":true`) {
		t.Errorf("get default-valued set --json: %q", out)
	}
	a.run(t, "", "config", "unset", "ui.theme")
	if _, out, _ = a.run(t, "", "config", "get", "ui.theme", "--json"); !strings.Contains(out, `"isSet":false`) {
		t.Errorf("get unset again --json: %q", out)
	}
	if _, out, _ = a.run(t, "", "config", "get", "ui.theme"); out != "auto\n" {
		t.Errorf("get plain: %q", out)
	}
}

func TestPurgeNoNoticeAndHoldNonNegative(t *testing.T) {
	a := newApp(t)
	Newest = func(context.Context) (string, error) { return "9.9.9", nil }
	t.Cleanup(func() { Newest = nil })
	fakeService(t)
	if code, _, errs := a.run(t, "", "purge", "--yes"); code != ok || strings.Contains(errs, "is out") {
		t.Errorf("purge: %d %q", code, errs)
	}
	if _, err := os.Stat(filepath.Join(a.b.Dir, "update-check.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("purge wrote the update memo back: %v", err)
	}
	path := filepath.Join(t.TempDir(), "r.json")
	must(t, os.WriteFile(path, []byte(`{"accounts":[]}`), 0o600))
	for _, h := range []string{"-1", "-5"} {
		if code, _, errs := a.run(t, "", "import-usage", path, "--hold", h); code != failed || !strings.Contains(errs, "--hold") {
			t.Errorf("--hold %s: %d %q", h, code, errs)
		}
	}
}
