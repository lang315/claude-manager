package account

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestSetAlias(t *testing.T) {
	b, _, _, _ := switchBook(t)
	for _, bad := range []string{"123", "-x", "a b", "a/b", " "} {
		if err := b.SetAlias(bob.Key(), bad); err == nil {
			t.Errorf("%q taken", bad)
		}
	}
	must(t, b.SetAlias(bob.Key(), " Dev "))
	blocks, _ := b.Blocks()
	if a := blockOf(t, blocks, "bob@example.com"); a.Alias != "dev" {
		t.Errorf("alias %q", a.Alias)
	}
	k, err := b.AddAPIKey(key1, "")
	must(t, err)
	if err := b.SetAlias(k.Key(), "dev"); err == nil {
		t.Error("two accounts with one alias")
	}
	must(t, b.SetAlias(bob.Key(), ""))
	must(t, b.SetAlias(k.Key(), "dev"))
	if err := b.SetAlias(alice.Key(), "x"); err == nil {
		t.Error("an alias for an account not added")
	}
}

func TestMaps(t *testing.T) {
	b, _, _, _ := switchBook(t)
	root := t.TempDir()
	work := filepath.Join(root, "work")
	if err := b.Map(bob.Key(), "rel"); err == nil {
		t.Error("mapped a relative path")
	}
	must(t, b.Map(bob.Key(), work+"/"))
	for dir, want := range map[string]string{work: bob.Key(), filepath.Join(work, "a", "b"): bob.Key(), root: ""} {
		if k, _, err := b.Mapped(dir); err != nil || k != want {
			t.Errorf("%s: %q %v", dir, k, err)
		}
	}
	if ok, _ := b.Unmap(root); ok {
		t.Error("unmapped what wasn't mapped")
	}
	must(t, b.Map(bob.Key(), root))
	must(t, b.Remove(bob.Key()))
	if m, _ := b.Maps(); len(m) != 0 {
		t.Errorf("maps outlive their account: %v", m)
	}
}

func TestAliasTravels(t *testing.T) {
	b, _, _, _ := switchBook(t)
	must(t, b.SetAlias(bob.Key(), "dev"))
	path := filepath.Join(t.TempDir(), "out.json")
	_, _, err := b.Export(path, false)
	must(t, err)
	c, _, _, _ := switchBook(t)
	must(t, c.Remove(bob.Key()))
	_, err = c.Import(path)
	must(t, err)
	blocks, _ := c.Blocks()
	if i := slices.IndexFunc(blocks, func(a Account) bool { return a.Alias == "dev" }); i < 0 || blocks[i].Email != "bob@example.com" {
		t.Errorf("alias lost: %+v", blocks)
	}
}
