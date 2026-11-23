package account

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FileVault keeps the app's items in one file readable by the user only,
// where there is no macOS keychain: on Linux, WSL and Windows, Claude Code
// itself keeps its login in ~/.claude/.credentials.json, as claude-swap
// keeps its backups in files there.
type FileVault struct {
	Path string // the file: the app's folder's vault.json
	mu   sync.Mutex
}

func (f *FileVault) read() (map[string][]byte, error) {
	m := map[string][]byte{}
	data, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, errors.New("The app's vault file is not JSON.")
	}
	return m, nil
}

func (f *FileVault) write(m map[string][]byte) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return err
	}
	return writeAtomic(f.Path, data, 0o600)
}

func vaultKey(service, acct string) string { return service + "\x00" + acct }

// Get returns the item; acct "" takes the service's first.
func (f *FileVault) Get(service, acct string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.read()
	if err != nil {
		return nil, err
	}
	if acct != "" {
		if v, ok := m[vaultKey(service, acct)]; ok {
			return v, nil
		}
		return nil, ErrNotFound
	}
	for k, v := range m {
		if strings.HasPrefix(k, service+"\x00") {
			return v, nil
		}
	}
	return nil, ErrNotFound
}

func (f *FileVault) Put(service, acct string, secret []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.read()
	if err != nil {
		return err
	}
	m[vaultKey(service, acct)] = secret
	return f.write(m)
}

// Delete removes the items: acct "" removes the service's every one.
func (f *FileVault) Delete(service, acct string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.read()
	if err != nil {
		return err
	}
	for k := range m {
		if k == vaultKey(service, acct) || acct == "" && strings.HasPrefix(k, service+"\x00") {
			delete(m, k)
		}
	}
	return f.write(m)
}
