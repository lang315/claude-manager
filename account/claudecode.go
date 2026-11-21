package account

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"time"
)

// liveDefault is the keychain service of Claude Code's login in its
// default folder.
const liveDefault = "Claude Code-credentials"

// Home is Claude Code's default folder and account file: what a switch
// writes. The rules follow claude-swap (github.com/realiti4/claude-swap,
// MIT), read at commit 773b585.
type Home struct {
	Root       string // ~/.claude
	ClaudeJSON string // ~/.claude.json
	User       string // the keychain account Claude Code files its login under: $USER
}

var errLockBusy = errors.New("Claude Code is busy with its sign-in. Try again in a minute.")

// Claude Code's locks are proper-lockfile's (npm): a directory whose mkdir
// takes it, touched while held, and taken over once older than stale.
const (
	credStale   = 60 * time.Second
	configStale = 10 * time.Second
	lockTouch   = 3 * time.Second
)

var lockWait = 9 * time.Second

func lock(ctx context.Context, dir string, stale time.Duration) (func(), error) {
	deadline := time.Now().Add(lockWait)
	for {
		err := os.Mkdir(dir, 0o755)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		if fi, err := os.Stat(dir); err == nil && time.Since(fi.ModTime()) > stale {
			if err := takeOver(dir, stale); err != nil {
				return nil, err
			}
		}
		if time.Now().After(deadline) {
			return nil, errLockBusy
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(lockTouch)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				now := time.Now()
				_ = os.Chtimes(dir, now, now)
			}
		}
	}()
	return func() { close(done); _ = os.Remove(dir) }, nil
}

func takeOver(dir string, stale time.Duration) error {
	t := dir + ".takeover"
	if err := os.Mkdir(t, 0o755); err != nil {
		if fi, err := os.Stat(t); err == nil && time.Since(fi.ModTime()) > 5*time.Second {
			_ = os.Remove(t)
		}
		return nil
	}
	defer os.Remove(t)
	fi, err := os.Stat(dir)
	if err != nil || time.Since(fi.ModTime()) <= stale {
		return nil
	}
	if err := os.Remove(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// lockAll takes Claude Code's credential locks, then its config lock, in
// Claude Code's order.
func (h *Home) lockAll(ctx context.Context) (func(), error) {
	var frees []func()
	free := func() {
		for i := len(frees) - 1; i >= 0; i-- {
			frees[i]()
		}
	}
	for _, l := range []struct {
		dir   string
		stale time.Duration
	}{
		{filepath.Join(h.Root, ".oauth_refresh.lock"), credStale},
		{h.Root + ".lock", credStale},
		{h.ClaudeJSON + ".lock", configStale},
	} {
		f, err := lock(ctx, l.dir, l.stale)
		if err != nil {
			free()
			return nil, err
		}
		frees = append(frees, f)
	}
	return free, nil
}

type liveBlob struct {
	data     []byte
	inVault  bool
	fileData []byte
}

func (h *Home) credentialsFile() string { return filepath.Join(h.Root, ".credentials.json") }

// readLive reads Claude Code's login as stored: the keychain item first,
// else .credentials.json.
func (h *Home) readLive(v Vault) (liveBlob, error) {
	var lb liveBlob
	file, err := os.ReadFile(h.credentialsFile())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return lb, err
	}
	lb.fileData = file
	data, err := v.Get(liveDefault, "")
	switch {
	case err == nil:
		lb.data, lb.inVault = data, true
	case errors.Is(err, ErrNotFound):
		lb.data = file
	default:
		return lb, err
	}
	return lb, nil
}

// writeLive puts data where Claude Code reads its login: in the keychain
// item unless Claude Code keeps it in .credentials.json alone. An existing
// .credentials.json gets the same bytes, so a running Claude Code sees a
// new mtime; it is never created.
func (h *Home) writeLive(v Vault, was liveBlob, data []byte) error {
	if was.inVault || was.fileData == nil {
		if err := v.Put(liveDefault, h.User, data); err != nil {
			return err
		}
	}
	if was.fileData != nil {
		return writeAtomic(h.credentialsFile(), data, 0o600)
	}
	return nil
}

func (h *Home) restoreLive(v Vault, was liveBlob) error {
	var err error
	switch {
	case was.inVault:
		err = v.Put(liveDefault, h.User, was.data)
	case was.fileData == nil:
		err = v.Delete(liveDefault, h.User)
	}
	if was.fileData != nil {
		err = errors.Join(err, writeAtomic(h.credentialsFile(), was.fileData, 0o600))
	}
	return err
}

// withOAuth is blob with its claudeAiOauth replaced by oauth, every other
// key kept (mcpOAuth, pluginSecrets…).
func withOAuth(blob []byte, oauth json.RawMessage) ([]byte, error) {
	m := map[string]json.RawMessage{}
	if json.Unmarshal(blob, &m) != nil || m == nil {
		m = map[string]json.RawMessage{}
	}
	m["claudeAiOauth"] = oauth
	return json.Marshal(m)
}

func oauthOf(blob []byte) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(blob, &m) != nil {
		return nil
	}
	if o := m["claudeAiOauth"]; len(o) > 0 && string(o) != "null" {
		return o
	}
	return nil
}

func withTokens(blob []byte, fresh Login) []byte {
	o := map[string]json.RawMessage{}
	_ = json.Unmarshal(oauthOf(blob), &o)
	f := map[string]json.RawMessage{}
	b, _ := json.Marshal(fresh)
	_ = json.Unmarshal(b, &f)
	maps.Copy(o, f)
	raw, _ := json.Marshal(o)
	out, _ := withOAuth(nil, raw)
	return out
}

func oauthAccountOf(data []byte) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	if o := m["oauthAccount"]; len(o) > 0 && string(o) != "null" {
		return o
	}
	return nil
}

func (h *Home) accountKey() string {
	data, err := os.ReadFile(h.ClaudeJSON)
	if err != nil {
		return ""
	}
	return keyOf(oauthAccountOf(data))
}

func spliceKey(doc []byte, key string, val json.RawMessage) ([]byte, error) {
	bad := fmt.Errorf("%s is not a JSON object.", "~/.claude.json")
	dec := json.NewDecoder(bytes.NewReader(doc))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, bad
	}
	seen := false
	for dec.More() {
		seen = true
		t, err := dec.Token()
		if err != nil {
			return nil, bad
		}
		afterKey := dec.InputOffset()
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, bad
		}
		if t != key {
			continue
		}
		end := int(dec.InputOffset())
		start := end - len(v) // Decode leaves the offset right after the value
		if start < int(afterKey) || !bytes.Equal(doc[start:end], v) {
			return nil, bad
		}
		return bytes.Join([][]byte{doc[:start], val, doc[end:]}, nil), nil
	}
	if t, err := dec.Token(); err != nil || t != json.Delim('}') {
		return nil, bad
	}
	brace := int(dec.InputOffset()) - 1
	entry, _ := json.Marshal(key)
	entry = append(append(entry, ':'), val...)
	if seen {
		entry = append([]byte{','}, entry...)
	}
	return bytes.Join([][]byte{doc[:brace], entry, doc[brace:]}, nil), nil
}

var rename = os.Rename

func writeAtomic(path string, data []byte, mode fs.FileMode) error {
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	err = errors.Join(err, f.Chmod(mode), f.Close())
	if err == nil {
		err = rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}
