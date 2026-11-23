package account

import (
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// row is a `cswap list --json` row for p, read age seconds ago.
func listRow(p Profile, status string, age float64, usage string) string {
	return `{"email":"` + p.Email + `","organizationUuid":"` + p.OrgUUID + `","usageStatus":"` + status +
		`","usageAgeSeconds":` + strconv.FormatFloat(age, 'f', -1, 64) + `,"usage":` + usage + `}`
}

func readings(t *testing.T, rows ...string) string {
	t.Helper()
	return writeRaw(t, `{"schemaVersion":1,"activeAccountNumber":1,"accounts":[`+strings.Join(rows, ",")+`]}`)
}

func writeRaw(t *testing.T, s string) string {
	t.Helper()
	path := t.TempDir() + "/list.json"
	must(t, os.WriteFile(path, []byte(s), 0o600))
	return path
}

const bobUsage = `{"fiveHour":{"pct":33,"resetsAt":"2026-10-26T15:00:00Z"},"sevenDay":{"pct":20,"resetsAt":"2026-10-30T00:00:00Z"},` +
	`"scoped":[{"name":"Fable","pct":5,"resetsAt":"2026-10-30T00:00:00Z"}],` +
	`"spend":{"pct":10,"used":5,"limit":50,"currency":"USD","resetsAt":"2026-11-01T00:00:00Z"}}`

func TestImportUsage(t *testing.T) {
	b, _, _, f := switchBook(t)
	f.usage["alice-at"], f.usage["bob-at"] = 50, 10
	b.Refresh(ctx, true)
	now := t0.Add(10 * time.Minute)
	b.Now = func() time.Time { return now }
	path := readings(t,
		listRow(bob, "ok", 60, bobUsage),
		listRow(alice, "ok", 900, `{"fiveHour":{"pct":1}}`),
		listRow(carol, "ok", 1, `{"fiveHour":{"pct":1}}`),
		`{"email":"x@y","organizationUuid":"","usageStatus":"token_expired"}`,
	)
	got, err := b.ImportUsage(path, -1)
	must(t, err)
	if got != (ReadingsImported{Adopted: 1, Kept: 1, Skipped: 2}) {
		t.Fatalf("got %+v", got)
	}
	blocks, _ := b.Blocks()
	a := blockOf(t, blocks, "bob@example.com")
	want := []Limit{
		{Kind: "session", Percent: 33, ResetsAt: t0.Add(3 * time.Hour)},
		{Kind: "weekly_all", Percent: 20, ResetsAt: time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)},
		{Kind: "weekly_scoped", Scope: "Fable", Percent: 5, ResetsAt: time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)},
		{Kind: "extra_usage", Percent: 10, ResetsAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Used: 5, Cap: 50, Currency: "USD"},
	}
	if !slices.Equal(a.Limits, want) || !a.ReadAt.Equal(now.Add(-time.Minute)) {
		t.Errorf("bob = %+v", a)
	}
	if a := blockOf(t, blocks, "alice@example.com"); a.Limits[0].Percent != 50 {
		t.Errorf("alice's newer reading replaced: %+v", a.Limits)
	}
}

// --hold: the app doesn't read the account itself meanwhile, and trusts
// the reading, never past its earliest reset nor an hour after it.
func TestImportUsageHold(t *testing.T) {
	b, _, _, f := switchBook(t)
	f.usage["alice-at"], f.usage["bob-at"] = 50, 10
	b.Refresh(ctx, true)
	now := t0.Add(10 * time.Minute)
	b.Now = func() time.Time { return now }
	_, err := b.ImportUsage(readings(t, listRow(bob, "ok", 60, bobUsage)), 30*time.Minute)
	must(t, err)
	now = now.Add(20 * time.Minute)
	f.took()
	got, _ := b.Refresh(ctx, true)
	if slices.Contains(f.took(), "usage bob-at") {
		t.Error("read a held account")
	}
	if !usable(blockOf(t, got, "bob@example.com"), now) {
		t.Error("held reading not trusted")
	}
	now = now.Add(11 * time.Minute)
	if usable(blockOf(t, got, "bob@example.com"), now) {
		t.Error("trusted past the hold")
	}
	b.Refresh(ctx, false)
	if !slices.Contains(f.took(), "usage bob-at") {
		t.Error("not read once the hold lapsed")
	}

	soon := strings.ReplaceAll(bobUsage, "2026-10-26T15:00:00Z", "2026-10-26T12:15:00Z")
	_, err = b.ImportUsage(readings(t, listRow(bob, "ok", 0, soon)), time.Hour)
	must(t, err)
	blocks, _ := b.Blocks()
	if h := blockOf(t, blocks, "bob@example.com").HeldUntil; !h.Equal(now.Add(time.Hour)) && !h.Before(now.Add(time.Hour)) {
		t.Errorf("held until %v", h)
	}
	_, err = b.ImportUsage(readings(t, listRow(bob, "ok", 0, bobUsage)), 3*time.Hour)
	must(t, err)
	blocks, _ = b.Blocks()
	if h := blockOf(t, blocks, "bob@example.com").HeldUntil; !h.Equal(now.Add(time.Hour)) {
		t.Errorf("held until %v, want an hour after the reading", h)
	}
	_, err = b.ImportUsage(readings(t, listRow(bob, "ok", 0, bobUsage)), 0)
	must(t, err)
	blocks, _ = b.Blocks()
	if h := blockOf(t, blocks, "bob@example.com").HeldUntil; !h.IsZero() {
		t.Errorf("hold 0 kept %v", h)
	}
}

func TestImportUsageHoldEndsAtReset(t *testing.T) {
	b, _, _, f := switchBook(t)
	f.usage["alice-at"], f.usage["bob-at"] = 50, 10
	b.Refresh(ctx, true)
	b.Now = func() time.Time { return t0.Add(time.Minute) }
	soon := strings.ReplaceAll(bobUsage, "2026-10-26T15:00:00Z", "2026-10-26T12:15:00Z")
	_, err := b.ImportUsage(readings(t, listRow(bob, "ok", 0, soon)), time.Hour)
	must(t, err)
	blocks, _ := b.Blocks()
	if h := blockOf(t, blocks, "bob@example.com").HeldUntil; !h.Equal(t0.Add(15 * time.Minute)) {
		t.Errorf("held until %v, want the five-hour reset", h)
	}
}

func TestImportUsageRejectsWritingNothing(t *testing.T) {
	for name, s := range map[string]string{
		"not JSON":     `x`,
		"schema 2":     `{"schemaVersion":2,"accounts":[]}`,
		"no email":     `{"schemaVersion":1,"accounts":[{"organizationUuid":"","usageStatus":"ok","usageAgeSeconds":1,"usage":{"fiveHour":{"pct":1}}}]}`,
		"negative age": `{"schemaVersion":1,"accounts":[` + listRow(bob, "ok", -5, `{"fiveHour":{"pct":1}}`) + `]}`,
		"no age":       `{"schemaVersion":1,"accounts":[{"email":"bob@example.com","organizationUuid":"org-b","usageStatus":"ok","usage":{"fiveHour":{"pct":1}}}]}`,
		"bad share":    `{"schemaVersion":1,"accounts":[{"email":"bob@example.com","organizationUuid":"org-b","usageStatus":"ok","usageAgeSeconds":1,"usage":{"fiveHour":{"pct":-1}}}]}`,
		"no windows":   `{"schemaVersion":1,"accounts":[{"email":"bob@example.com","organizationUuid":"org-b","usageStatus":"ok","usageAgeSeconds":1,"usage":{}}]}`,
		"bad reset":    `{"schemaVersion":1,"accounts":[{"email":"bob@example.com","organizationUuid":"org-b","usageStatus":"ok","usageAgeSeconds":1,"usage":{"fiveHour":{"pct":1,"resetsAt":"soon"}}}]}`,
		"duplicate":    `{"schemaVersion":1,"accounts":[` + listRow(bob, "ok", 1, `{"fiveHour":{"pct":1}}`) + `,` + listRow(bob, "ok", 1, `{"fiveHour":{"pct":1}}`) + `]}`,
		"bad spend":    `{"schemaVersion":1,"accounts":[{"email":"bob@example.com","organizationUuid":"org-b","usageStatus":"ok","usageAgeSeconds":1,"usage":{"spend":{"pct":1}}}]}`,
		"no org":       `{"schemaVersion":1,"accounts":[{"email":"bob@example.com","usageStatus":"ok","usageAgeSeconds":1,"usage":{"fiveHour":{"pct":1}}}]}`,
		"later bad":    `{"schemaVersion":1,"accounts":[` + listRow(bob, "ok", 1, `{"fiveHour":{"pct":99}}`) + `,{"email":"","organizationUuid":""}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			b, _, _, f := switchBook(t)
			f.usage["alice-at"], f.usage["bob-at"] = 50, 10
			b.Refresh(ctx, true)
			before, _ := os.ReadFile(b.path())
			if _, err := b.ImportUsage(writeRaw(t, s), time.Hour); err == nil {
				t.Fatal("imported")
			}
			if after, _ := os.ReadFile(b.path()); string(after) != string(before) {
				t.Error("accounts.json changed")
			}
			blocks, _ := b.Blocks()
			if a := blockOf(t, blocks, "bob@example.com"); a.Limits[0].Percent != 10 || !a.HeldUntil.IsZero() {
				t.Errorf("bob = %+v", a)
			}
		})
	}
}

func TestIsReadings(t *testing.T) {
	if !IsReadings([]byte(`{"schemaVersion":1}`)) || IsReadings([]byte(`{"version":1,"accounts":[]}`)) {
		t.Error("IsReadings")
	}
}
