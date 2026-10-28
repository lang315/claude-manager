package account

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// pitboardState is a Pitboard state.json of schema 8, made up: bob is a
// Claude Code account with a reading, dan one without, x a Codex account.
const pitboardState = `{"schema":8,"accounts":[
	{"id":"p1","provider":"claude","label":"work","email":"bob@example.com","plan":"Max 5x","account_uuid":"bob","organization_uuid":"org-b",
	 "parked":{"service":"pitboard-park-x","refresh_fingerprint":"f","access_expires_at":1,"refresh_expires_at":2,"parked_at":3}},
	{"id":"p2","provider":"codex","email":"x@example.com","account_uuid":"x","organization_uuid":"y"},
	{"id":"p3","provider":"claude","email":"dan@example.com","plan":"Pro","account_uuid":"dan","organization_uuid":"org-d"}],
	"in_use":{"claude":{"owner":{"account_uuid":"bob","organization_uuid":"org-b"}}}}`

// pitboardUsage is its usage.json: bob's reading at t0, with one window
// it can't read.
func pitboardUsage() string {
	return fmt.Sprintf(`{"p1":{"observed_at":%d,"answered_at":%d,"source":"live","lists_every_limit":true,"windows":[
		{"kind":"session","scope":null,"percent":40.0,"resets_at":%d,"length_seconds":18000,"is_active":true},
		{"kind":"weekly_scoped","scope":"Fable","percent":3.5,"resets_at":%d},
		{"kind":"weekly_all","percent":"bad"}]},
		"p2":{"observed_at":%d,"windows":[{"kind":"session","percent":9}]}}`,
		t0.Unix(), t0.Unix(), t0.Add(2*time.Hour).Unix(), t0.Add(48*time.Hour).Unix(), t0.Unix())
}

func writePitboard(t *testing.T, state, usage string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"state.json": state, "usage.json": usage} {
		if body == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestReadPitboard(t *testing.T) {
	got, err := ReadPitboard(writePitboard(t, pitboardState, pitboardUsage()))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Email != "bob@example.com" || got[1].Email != "dan@example.com" {
		t.Fatalf("got %+v, want bob and dan: no Codex account", got)
	}
	b := got[0]
	if b.Key() != "bob/org-b" || b.Plan != "Max 5x" || b.Source != "Pitboard" || !b.ReadAt.Equal(t0) || b.Added || b.InUse {
		t.Fatalf("bob = %+v", b)
	}
	if len(b.Limits) != 2 || b.Limits[0].Label() != "Session (5h)" || b.Limits[0].Percent != 40 || !b.Limits[0].ResetsAt.Equal(t0.Add(2*time.Hour)) ||
		b.Limits[1].Label() != "Weekly · Fable" || b.Limits[1].Percent != 3.5 {
		t.Fatalf("bob's limits = %+v", b.Limits)
	}
	if d := got[1]; len(d.Limits) != 0 || !d.ReadAt.IsZero() || d.Source != "Pitboard" {
		t.Fatalf("dan = %+v", d)
	}
}

func TestReadPitboardMissing(t *testing.T) {
	if got, err := ReadPitboard(filepath.Join(t.TempDir(), "none")); got != nil || err != nil {
		t.Fatalf("no folder: %+v, %v", got, err)
	}
	got, err := ReadPitboard(writePitboard(t, pitboardState, ""))
	if err != nil || len(got) != 2 || len(got[0].Limits) != 0 {
		t.Fatalf("no usage.json: %+v, %v", got, err)
	}
}

func TestReadPitboardBroken(t *testing.T) {
	for _, c := range [][2]string{{"{", ""}, {pitboardState, "{"}} {
		if _, err := ReadPitboard(writePitboard(t, c[0], c[1])); !errors.Is(err, errPitboard) {
			t.Errorf("state %q usage %q: err = %v", c[0][:1], c[1], err)
		}
	}
}

// bobOnPitboard is what Pitboard read of bob, at, and carol, whom only it
// follows.
func bobOnPitboard(at time.Time, pct float64) func() ([]Account, error) {
	return func() ([]Account, error) {
		return []Account{
			{AccountUUID: "alice", OrgUUID: "org-a", Email: "alice@example.com", Source: "Pitboard", ReadAt: at,
				Limits: []Limit{{Kind: "session", Percent: pct}}},
			{AccountUUID: "carol", OrgUUID: "org-c", Email: "carol@example.com", Source: "Pitboard", ReadAt: at,
				Limits: []Limit{{Kind: "session", Percent: 5}}},
		}, nil
	}
}

func TestPitboardFillsNewerNumbers(t *testing.T) {
	b, _, _, _ := aliceInUse(t) // alice at 62%, read at t0
	b.Pitboard = bobOnPitboard(t0.Add(time.Minute), 70)
	got, err := b.Refresh(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Email != "alice@example.com" || got[1].Email != "carol@example.com" {
		t.Fatalf("got %+v", got)
	}
	if a := got[0]; !a.InUse || a.Limits[0].Percent != 70 || a.Source != "Pitboard" || !a.ReadAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("alice = %+v: Pitboard's newer reading should show", a)
	}
	if c := got[1]; c.Limits[0].Percent != 5 || c.Added || c.InUse {
		t.Fatalf("carol = %+v", c)
	}
}

func TestPitboardKeepsOurNewerNumbers(t *testing.T) {
	b, _, _, _ := aliceInUse(t)
	b.Pitboard = bobOnPitboard(t0.Add(-time.Minute), 70)
	got, _ := b.Refresh(ctx, false)
	if a := got[0]; a.Limits[0].Percent != 62 || a.Source != "" {
		t.Fatalf("alice = %+v: our newer reading should stay", a)
	}
	saved, _ := b.Blocks()
	if a := saved[0]; a.Limits[0].Percent != 62 || len(saved) != 2 {
		t.Fatalf("Blocks = %+v", saved)
	}
}

func TestPitboardErrorIsANote(t *testing.T) {
	b, _, _, _ := aliceInUse(t)
	b.Pitboard = func() ([]Account, error) { return nil, errPitboard }
	got, err := b.Refresh(ctx, false)
	if err != nil || len(got) != 1 || b.Note() != errPitboard.Error() {
		t.Fatalf("got %+v, %v, note %q", got, err, b.Note())
	}
	b.Pitboard = bobOnPitboard(t0, 1)
	b.Blocks()
	if b.Note() != "" {
		t.Fatalf("note %q after Pitboard read again", b.Note())
	}
}

func TestReadPitboardLabel(t *testing.T) {
	got, err := ReadPitboard(writePitboard(t, pitboardState, ""))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].PitboardLabel != "work" {
		t.Errorf("bob's label = %q, want work: Pitboard parks his login", got[0].PitboardLabel)
	}
	if got[1].PitboardLabel != "" {
		t.Errorf("dan's label = %q, want none: nothing is parked", got[1].PitboardLabel)
	}
}

func TestPitboardLabelSurvivesMerge(t *testing.T) {
	b, _, _, _ := aliceInUse(t) // alice at 62%, read at t0
	b.Pitboard = func() ([]Account, error) {
		return []Account{{AccountUUID: "alice", OrgUUID: "org-a", Email: "alice@example.com", Source: "Pitboard",
			ReadAt: t0.Add(-time.Minute), PitboardLabel: "home"}}, nil
	}
	got, _ := b.Refresh(ctx, false)
	if a := got[0]; a.PitboardLabel != "home" || a.Source != "" {
		t.Fatalf("alice = %+v: our numbers stay, Pitboard's label joins them", a)
	}
}
