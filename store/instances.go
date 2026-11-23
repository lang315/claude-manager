package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Instance is a running Claude Code, as claude-swap's status lists them:
// where it runs and from what (CLI, VS Code, Desktop…), and the profile
// it runs from when it isn't ~/.claude.
type Instance struct {
	PID     int
	Kind    string // claude-swap's entrypoint label, or an IDE's name
	Folder  string
	Profile string // "" for ~/.claude
	IDE     bool
}

// entrypoints are claude-swap's printer._ENTRYPOINT_LABELS.
var entrypoints = map[string]string{"cli": "CLI", "claude-vscode": "VS Code", "claude-desktop": "Desktop", "sdk-cli": "SDK",
	"sdk-ts": "SDK", "sdk-py": "SDK", "mcp": "MCP", "local-agent": "Agent", "remote": "Remote"}

// Instances lists the running sessions, from sessions/<pid>.json in Root
// and in each profile, and the IDEs connected, from ide/<port>.lock.
func (s *Store) Instances() []Instance {
	var out []Instance
	roots := []string{s.Root}
	if s.Profiles != "" {
		ps, _ := os.ReadDir(s.Profiles)
		for _, p := range ps {
			roots = append(roots, filepath.Join(s.Profiles, p.Name()))
		}
	}
	for _, root := range roots {
		profile := ""
		if root != s.Root {
			profile = root
		}
		for _, f := range listFiles(filepath.Join(root, "sessions"), ".json") {
			var r struct {
				PID        int             `json:"pid"`
				CWD        string          `json:"cwd"`
				Entrypoint string          `json:"entrypoint"`
				ProcStart  json.RawMessage `json:"procStart"`
			}
			if b, err := os.ReadFile(f); err != nil || json.Unmarshal(b, &r) != nil || !s.Alive(r.PID) || !SameProcess(r.PID, Stamp(r.ProcStart)) {
				continue
			}
			kind := entrypoints[r.Entrypoint]
			if kind == "" {
				kind = r.Entrypoint
			}
			out = append(out, Instance{PID: r.PID, Kind: kind, Folder: r.CWD, Profile: profile})
		}
	}
	for _, f := range listFiles(filepath.Join(s.Root, "ide"), ".lock") {
		var r struct {
			PID     int      `json:"pid"`
			IDE     string   `json:"ideName"`
			Folders []string `json:"workspaceFolders"`
		}
		if b, err := os.ReadFile(f); err != nil || json.Unmarshal(b, &r) != nil || !s.Alive(r.PID) {
			continue
		}
		name := r.IDE
		if name == "Visual Studio Code" {
			name = "VS Code"
		}
		if name == "" {
			name = "Unknown IDE"
		}
		out = append(out, Instance{PID: r.PID, Kind: name, Folder: strings.Join(r.Folders, ", "), IDE: true})
	}
	return out
}
