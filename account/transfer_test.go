package account

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// exportOf is the export file at path, decoded.
func exportOf(t *testing.T, path string) exportFile {
	t.Helper()
	data, err := os.ReadFile(path)
	must(t, err)
	var f exportFile
	must(t, json.Unmarshal(data, &f))
	return f
}

func entryOf(t *testing.T, f exportFile, email string) exportEntry {
	t.Helper()
	for _, e := range f.Accounts {
		if e.Email == email {
			return e
		}
	}
	t.Fatalf("no %s in the export", email)
	return exportEntry{}
}

// writeExport writes f to a new file and returns its path.
func writeExport(t *testing.T, f any) string {
	t.Helper()
	data, err := json.Marshal(f)
	must(t, err)
	path := filepath.Join(t.TempDir(), "accounts.json")
	must(t, os.WriteFile(path, data, 0o600))
	return path
}

func TestExportImportRoundTrip(t *testing.T) {
	b, _, _, _ := switchBook(t)
	must(t, b.SetAccountAuto(bob.Key(), true, 0, 0))
	path := filepath.Join(t.TempDir(), "out.json")
	n, err := b.Export(path)
	must(t, err)
	if n != 1 {
		t.Fatalf("exported %d accounts, want bob only", n)
	}
	fi, err := os.Stat(path)
	must(t, err)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	f := exportOf(t, path)
	e := entryOf(t, f, "bob@example.com")
	if f.Version != 1 || f.Encrypted || e.Number != 1 || e.UUID != "bob" || e.OrgUUID != "org-b" {
		t.Errorf("export = %+v", f)
	}

	c, v, _, _ := switchBook(t)
	must(t, c.Remove(bob.Key()))
	got, err := c.Import(path)
	must(t, err)
	if got != (Imported{Added: 1}) {
		t.Fatalf("imported %+v", got)
	}
	if l, _ := parseLogin(get(t, v, ParkService, bob.Key())); l.RefreshToken != "bob-rt" {
		t.Errorf("bob's login %q", l.RefreshToken)
	}
	blocks, _ := c.Blocks()
	if a := blockOf(t, blocks, "bob@example.com"); !a.Added || keyOf(a.OAuthAccount) != bob.Key() {
		t.Errorf("bob = %+v", a)
	}
}

// The account in use goes out with Claude Code's login when that is newer,
// and without Claude Code's MCP logins.
func TestExportTakesNewerLiveLogin(t *testing.T) {
	b, v, _, _ := switchBook(t)
	b.Refresh(ctx, true)
	must(t, v.Put(liveDefault, "me", withUUIDs(t, stored(t, login("alice-at2", "alice-rt2", t0.Add(5*time.Hour))), alice)))
	path := filepath.Join(t.TempDir(), "out.json")
	_, err := b.Export(path)
	must(t, err)
	e := entryOf(t, exportOf(t, path), "alice@example.com")
	if l, _ := parseLogin(e.Credentials); l.RefreshToken != "alice-rt2" {
		t.Errorf("alice exported with %q", l.RefreshToken)
	}
	if strings.Contains(string(e.Credentials), "mcpOAuth") {
		t.Error("MCP logins exported")
	}
}

func TestExportLiveLoginOnlyWhenItsOwn(t *testing.T) {
	b, v, _, _ := switchBook(t)
	b.Refresh(ctx, true)
	must(t, v.Put(liveDefault, "me", withUUIDs(t, stored(t, login("x-at", "x-rt", t0.Add(5*time.Hour))), carol)))
	path := filepath.Join(t.TempDir(), "out.json")
	_, err := b.Export(path)
	must(t, err)
	if l, _ := parseLogin(entryOf(t, exportOf(t, path), "alice@example.com").Credentials); l.RefreshToken != "alice-rt" {
		t.Errorf("alice exported with %q", l.RefreshToken)
	}
}

func TestExportKeepsNewerSavedLogin(t *testing.T) {
	b, v, _, _ := switchBook(t)
	must(t, v.Put(liveDefault, "me", withUUIDs(t, stored(t, login("alice-at", "alice-rt", t0.Add(time.Hour))), alice)))
	_, err := b.keep(login("alice-new", "alice-rt-new", t0.Add(5*time.Hour)).Marshal(), alice, json.RawMessage(aliceAccount))
	must(t, err)
	path := filepath.Join(t.TempDir(), "out.json")
	_, err = b.Export(path)
	must(t, err)
	if l, _ := parseLogin(entryOf(t, exportOf(t, path), "alice@example.com").Credentials); l.RefreshToken != "alice-rt-new" {
		t.Errorf("alice exported with %q", l.RefreshToken)
	}
}

// An account added before the app kept oauthAccount can't be switched to
// on the other Mac either.
func TestExportSkipsAccountWithoutOAuthAccount(t *testing.T) {
	b, _, _, _ := switchBook(t)
	_, err := b.keep(login("carol-at", "carol-rt", t0.Add(time.Hour)).Marshal(), carol, nil)
	must(t, err)
	path := filepath.Join(t.TempDir(), "out.json")
	n, err := b.Export(path)
	must(t, err)
	if n != 1 || exportOf(t, path).Accounts[0].Email != "bob@example.com" {
		t.Errorf("exported %d: %+v", n, exportOf(t, path).Accounts)
	}
}

func TestExportMakesReplacedFilePrivate(t *testing.T) {
	b, _, _, _ := switchBook(t)
	path := filepath.Join(t.TempDir(), "out.json")
	must(t, os.WriteFile(path, []byte("old"), 0o644))
	_, err := b.Export(path)
	must(t, err)
	fi, err := os.Stat(path)
	must(t, err)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
}

func TestExportRefusesLink(t *testing.T) {
	b, _, _, _ := switchBook(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	must(t, os.WriteFile(target, []byte("keep"), 0o644))
	link := filepath.Join(dir, "out.json")
	must(t, os.Symlink(target, link))
	if _, err := b.Export(link); err == nil {
		t.Fatal("exported through a link")
	}
	if data, _ := os.ReadFile(target); string(data) != "keep" {
		t.Error("the link's target changed")
	}
}

func TestExportSkipsAccountWithoutLogin(t *testing.T) {
	b, v, _, _ := switchBook(t)
	must(t, v.Delete(ParkService, bob.Key()))
	if _, err := b.Export(filepath.Join(t.TempDir(), "out.json")); !errors.Is(err, errNothingToExport) {
		t.Fatalf("err = %v", err)
	}
}

// claudeSwap is an export as claude-swap's transfer.py writes it, with
// --full's extra keys.
func claudeSwap(t *testing.T, entries ...map[string]any) map[string]any {
	t.Helper()
	return map[string]any{"version": 1, "exportedAt": "2026-11-21T00:00:00Z", "exportedFrom": "macos",
		"swapVersion": "1.0", "encrypted": false, "activeAccountNumber": 1, "accounts": entries}
}

func swapEntry(t *testing.T, n int, p Profile, l Login, acct string) map[string]any {
	t.Helper()
	var creds, conf map[string]any
	must(t, json.Unmarshal(stored(t, l), &creds))
	must(t, json.Unmarshal([]byte(`{"numStartups":1,"oauthAccount":`+acct+`}`), &conf))
	return map[string]any{"number": n, "email": p.Email, "uuid": p.AccountUUID, "organizationUuid": p.OrgUUID,
		"organizationName": p.OrgName, "added": "2026-11-01T00:00:00Z", "credentials": creds, "config": conf}
}

const carolAccount = `{"accountUuid":"carol","emailAddress":"carol@example.com","organizationUuid":"org-c"}`

func TestImportClaudeSwapExport(t *testing.T) {
	b, v, _, _ := switchBook(t)
	path := writeExport(t, claudeSwap(t,
		swapEntry(t, 1, carol, login("carol-at", "carol-rt", t0.Add(time.Hour)), carolAccount),
		map[string]any{"number": 2, "email": "key@example.com", "kind": "api_key", "credentials": "sk-ant-api03-x", "config": map[string]any{}},
	))
	got, err := b.Import(path)
	must(t, err)
	if got != (Imported{Added: 1, Skipped: 1}) {
		t.Fatalf("imported %+v", got)
	}
	saved := get(t, v, ParkService, carol.Key())
	if strings.Contains(string(saved), "mcpOAuth") {
		t.Error("kept the export's MCP logins")
	}
	blocks, _ := b.Blocks()
	if a := blockOf(t, blocks, "carol@example.com"); string(a.OAuthAccount) != carolAccount {
		t.Errorf("carol's oauthAccount %s", a.OAuthAccount)
	}
}

func TestImportKeepsNewerSavedLogin(t *testing.T) {
	b, v, _, _ := switchBook(t)
	must(t, b.SetAccountAuto(bob.Key(), true, 95, 3))
	path := writeExport(t, claudeSwap(t,
		swapEntry(t, 1, bob, login("bob-old", "bob-rt-old", t0.Add(time.Minute)), bobAccount)))
	got, err := b.Import(path)
	must(t, err)
	if got != (Imported{Skipped: 1}) {
		t.Fatalf("imported %+v", got)
	}
	path = writeExport(t, claudeSwap(t,
		swapEntry(t, 1, bob, login("bob-new", "bob-rt-new", t0.Add(5*time.Hour)), bobAccount)))
	got, err = b.Import(path)
	must(t, err)
	if got != (Imported{Updated: 1}) {
		t.Fatalf("imported %+v", got)
	}
	if l, _ := parseLogin(get(t, v, ParkService, bob.Key())); l.RefreshToken != "bob-rt-new" {
		t.Errorf("bob's login %q", l.RefreshToken)
	}
	blocks, _ := b.Blocks()
	if a := blockOf(t, blocks, "bob@example.com"); !a.AutoOff || a.Line != 95 || a.Priority != 3 {
		t.Errorf("bob's auto-switch settings lost: %+v", a)
	}
}

// Claude Code's own login is never replaced from a file, added or not.
func TestImportSkipsAccountInUse(t *testing.T) {
	b, v, _, _ := switchBook(t)
	path := writeExport(t, claudeSwap(t,
		swapEntry(t, 1, alice, login("alice-x", "alice-rt-x", t0.Add(9*time.Hour)), aliceAccount)))
	got, err := b.Import(path)
	must(t, err)
	if got != (Imported{Skipped: 1}) {
		t.Fatalf("imported %+v", got)
	}
	if _, err := v.Get(ParkService, alice.Key()); !errors.Is(err, ErrNotFound) {
		t.Error("saved a login for the account in use")
	}
}

const aliceAccount = `{"accountUuid":"alice","emailAddress":"alice@example.com","organizationUuid":"org-a"}`

func TestImportRejectsBadFileWritingNothing(t *testing.T) {
	good := func(t *testing.T) map[string]any {
		return swapEntry(t, 1, carol, login("carol-at", "carol-rt", t0.Add(time.Hour)), carolAccount)
	}
	cases := map[string]func(t *testing.T) any{
		"not JSON":  func(t *testing.T) any { return "x" },
		"version 2": func(t *testing.T) any { f := claudeSwap(t, good(t)); f["version"] = 2; return f },
		"encrypted": func(t *testing.T) any { f := claudeSwap(t, good(t)); f["encrypted"] = true; return f },
		"no accounts": func(t *testing.T) any {
			return claudeSwap(t)
		},
		"no email": func(t *testing.T) any { e := good(t); delete(e, "email"); return claudeSwap(t, e) },
		"no refresh token": func(t *testing.T) any {
			return claudeSwap(t, good(t), swapEntry(t, 2, bob, login("bob-at", "", t0.Add(time.Hour)), bobAccount))
		},
		"no oauthAccount": func(t *testing.T) any {
			e := good(t)
			e["config"] = map[string]any{}
			delete(e, "uuid")
			delete(e, "organizationUuid")
			return claudeSwap(t, e)
		},
		"uuid disagrees": func(t *testing.T) any { e := good(t); e["uuid"] = "bob"; return claudeSwap(t, e) },
		"org disagrees":  func(t *testing.T) any { e := good(t); e["organizationUuid"] = "org-b"; return claudeSwap(t, e) },
		"login names another account": func(t *testing.T) any {
			e := swapEntry(t, 1, carol, Login{}, carolAccount)
			var creds map[string]any
			must(t, json.Unmarshal(withUUIDs(t, login("carol-at", "carol-rt", t0.Add(time.Hour)).Marshal(), bob), &creds))
			e["credentials"] = creds
			return claudeSwap(t, e)
		},
		"duplicate": func(t *testing.T) any { return claudeSwap(t, good(t), good(t)) },
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			b, v, _, _ := switchBook(t)
			before, _ := os.ReadFile(b.path())
			_, err := b.Import(writeExport(t, f(t)))
			if err == nil {
				t.Fatal("imported a bad file")
			}
			if strings.Contains(err.Error(), "-at") || strings.Contains(err.Error(), "-rt") {
				t.Errorf("error shows a token: %v", err)
			}
			if _, err := v.Get(ParkService, carol.Key()); !errors.Is(err, ErrNotFound) {
				t.Error("wrote carol's login")
			}
			if after, _ := os.ReadFile(b.path()); string(after) != string(before) {
				t.Error("accounts.json changed")
			}
		})
	}
}
