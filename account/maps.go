package account

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Aliases and directory mappings, claude-swap's `alias` and `map`, for the
// command line: an alias is a short name for an added account; a folder
// mapped to one makes it the account `claude-manager run` opens there and
// in the folders below.

var aliasRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// normalizeAlias is claude-swap's models.normalize_alias.
func normalizeAlias(name string) (string, error) {
	n := strings.ToLower(strings.TrimSpace(name))
	switch {
	case n == "":
		return "", errors.New("An alias can't be empty.")
	case strings.Trim(n, "0123456789") == "":
		return "", fmt.Errorf("The alias %q can't be all digits.", name)
	case !aliasRE.MatchString(n):
		return "", fmt.Errorf("The alias %q must start with a letter or digit and hold only letters, digits, '-', '_' and '.'.", name)
	}
	return n, nil
}

// SetAlias gives the added account key the alias name, or takes its alias
// away when name is "".
func (b *Book) SetAlias(key, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return err
	}
	i := slices.IndexFunc(b.added, func(a Account) bool { return a.Key() == key })
	if i < 0 {
		return errNotAdded
	}
	alias := ""
	if name != "" {
		var err error
		if alias, err = normalizeAlias(name); err != nil {
			return err
		}
		if j := slices.IndexFunc(b.added, func(a Account) bool { return a.Alias == alias }); j >= 0 && j != i {
			return fmt.Errorf("%s already has the alias %q.", b.added[j].Email, alias)
		}
	}
	b.added[i].Alias = alias
	return b.save()
}

// Map maps the folder dir, and those below it, to the added account key.
func (b *Book) Map(key, dir string) error {
	if !filepath.IsAbs(dir) {
		return errors.New("Map a folder by its full path.")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return err
	}
	if !slices.ContainsFunc(b.added, func(a Account) bool { return a.Key() == key }) {
		return errNotAdded
	}
	if b.maps == nil {
		b.maps = map[string]string{}
	}
	b.maps[realDir(dir)] = key
	return b.save()
}

// Unmap takes dir's mapping away; it reports whether there was one.
func (b *Book) Unmap(dir string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return false, err
	}
	dir = realDir(dir)
	if _, ok := b.maps[dir]; !ok {
		return false, nil
	}
	delete(b.maps, dir)
	return true, b.save()
}

// Maps is every mapping: a folder to an account key.
func (b *Book) Maps() (map[string]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	err := b.load()
	return maps.Clone(b.maps), err
}

// Mapped is the account key mapped to dir or its nearest mapped parent,
// and that folder; "" when none is.
func (b *Book) Mapped(dir string) (key, at string, err error) {
	m, err := b.Maps()
	if err != nil {
		return "", "", err
	}
	for d := realDir(dir); ; d = filepath.Dir(d) {
		if k, ok := m[d]; ok {
			return k, d, nil
		}
		if d == filepath.Dir(d) {
			return "", "", nil
		}
	}
}

// realDir is dir with its symlinks resolved, as claude-swap's
// normalize_path: one folder, by whatever path it is reached. A folder
// that is gone is taken as written.
func realDir(dir string) string {
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		return r
	}
	return filepath.Clean(dir)
}
