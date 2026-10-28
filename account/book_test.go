package account

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBookKeepSavesNoSecret(t *testing.T) {
	b, v, _, _ := newBook(t, newAnthropic())
	l := login("bob-at", "bob-rt", t0.Add(time.Hour))
	l.RefreshTokenExpiresAt = t0.Add(30 * 24 * time.Hour).UnixMilli()
	a, err := b.keep(l, bob)
	if err != nil {
		t.Fatal(err)
	}
	if a.Email != "bob@example.com" || !a.Added || a.Plan != "max" || !a.GoodUntil.Equal(t0.Add(30*24*time.Hour)) {
		t.Fatalf("kept %+v", a)
	}
	if blob, err := v.Get(ParkService, "bob/org-b"); err != nil || !strings.Contains(string(blob), "bob-rt") {
		t.Fatalf("the vault holds %q, %v", blob, err)
	}
	path := filepath.Join(b.Dir, "accounts.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if s := string(data); !strings.Contains(s, "bob@example.com") || strings.Contains(s, "bob-at") || strings.Contains(s, "bob-rt") {
		t.Fatalf("accounts.json = %s", s)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("accounts.json mode = %v, %v", fi.Mode(), err)
	}
	if fi, err := os.Stat(b.Dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("folder mode = %v, %v", fi.Mode(), err)
	}
}

func TestBookReadsWhatItSaved(t *testing.T) {
	b, v, _, _ := newBook(t, newAnthropic())
	if _, err := b.keep(login("bob-at", "bob-rt", t0), bob); err != nil {
		t.Fatal(err)
	}
	again := &Book{Dir: b.Dir, Vault: v}
	got, err := again.Blocks()
	if err != nil || len(got) != 1 || got[0].Email != "bob@example.com" || !got[0].Added {
		t.Fatalf("Blocks = %+v, %v", got, err)
	}
}

func TestBookKeepReplacesTheSameAccount(t *testing.T) {
	b, v, _, _ := newBook(t, newAnthropic())
	for _, at := range []string{"at-1", "at-2"} {
		if _, err := b.keep(login(at, "rt", t0), bob); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := b.Blocks()
	if len(got) != 1 {
		t.Fatalf("Blocks = %+v", got)
	}
	if blob, _ := v.Get(ParkService, "bob/org-b"); !strings.Contains(string(blob), "at-2") {
		t.Fatalf("the vault holds %s", blob)
	}
}

func TestBookRemove(t *testing.T) {
	b, v, _, _ := newBook(t, newAnthropic())
	if _, err := b.keep(login("bob-at", "bob-rt", t0), bob); err != nil {
		t.Fatal(err)
	}
	if err := b.Remove("bob/org-b"); err != nil {
		t.Fatal(err)
	}
	if got, _ := b.Blocks(); len(got) != 0 || v.Len() != 0 {
		t.Fatalf("after Remove: %+v, %d items", got, v.Len())
	}
}

func TestBookUnreadableFile(t *testing.T) {
	b, _, _, _ := newBook(t, newAnthropic())
	if err := os.WriteFile(filepath.Join(b.Dir, "accounts.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := b.Blocks(); err == nil || len(got) != 0 {
		t.Fatalf("Blocks = %+v, %v", got, err)
	}
}
