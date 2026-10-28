package account

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrNotSignedIn says Claude Code holds no login.
var ErrNotSignedIn = errors.New("Not signed in to Claude Code.")

// liveService is the keychain service Claude Code keeps its login under
// for a config directory: configDir is $CLAUDE_CONFIG_DIR as set, "" for
// the default one.
func liveService(configDir string) string {
	if configDir == "" {
		return "Claude Code-credentials"
	}
	sum := sha256.Sum256([]byte(configDir))
	return "Claude Code-credentials-" + hex.EncodeToString(sum[:4])
}

// ReadLive reads the login Claude Code uses: from the keychain, else from
// root's .credentials.json, where Claude Code writes it when the keychain
// refuses it. It only reads: the login is Claude Code's to renew.
func ReadLive(v Vault, configDir, root string) (Login, error) {
	blob, err := v.Get(liveService(configDir), "")
	if errors.Is(err, ErrNotFound) {
		blob, err = os.ReadFile(filepath.Join(root, ".credentials.json"))
		if errors.Is(err, fs.ErrNotExist) {
			return Login{}, ErrNotSignedIn
		}
	}
	if err != nil {
		return Login{}, err
	}
	return parseLogin(blob)
}
