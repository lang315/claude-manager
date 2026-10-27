package store

import "testing"

func TestScan(t *testing.T) {
	s, _, _ := newStore(t)
	entries, err := s.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Name != "projects" {
		t.Errorf("largest entry %q, want projects", entries[0].Name)
	}
	for i := 1; i < len(entries); i++ {
		if entries[i].Size > entries[i-1].Size {
			t.Errorf("%q (%d) after %q (%d): not sorted", entries[i].Name, entries[i].Size, entries[i-1].Name, entries[i-1].Size)
		}
	}
	by := map[string]Entry{}
	for _, e := range entries {
		by[e.Name] = e
	}
	if e := by["paste-cache"]; e.Files != 1 || e.Size != 5 || !e.Cleanable {
		t.Errorf("paste-cache %+v, want 1 file of 5 bytes, cleanable", e)
	}
	if by["settings.json"].Cleanable || !by["settings.json.bak"].Cleanable || by["skills"].Cleanable {
		t.Errorf("cleanable wrong: settings.json %v, settings.json.bak %v, skills %v",
			by["settings.json"].Cleanable, by["settings.json.bak"].Cleanable, by["skills"].Cleanable)
	}
}

func TestScanMissingRoot(t *testing.T) {
	s := &Store{Root: t.TempDir() + "/nope"}
	if _, err := s.Scan(); err == nil {
		t.Error("no error for a missing root")
	}
}
