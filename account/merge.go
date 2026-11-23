package account

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// mergeHistory moves a profile's own history, at dest, into ~/.claude's,
// at src, before the profile links to it, as claude-swap's
// _merge_history_into_source: so a conversation kept while history was
// not shared shows in claude --resume. Nothing is lost: a file already
// there that holds every byte of the profile's makes it a duplicate; any
// other collision keeps the shared copy and parks the profile's under
// conflicts. history.jsonl takes the profile's lines it lacks.
func mergeHistory(src, dest, conflicts string) (moved, parked int, err error) {
	return mergeHistoryLive(src, dest, conflicts, func(string) bool { return true })
}

// mergeHistoryLive is mergeHistory where, of two diverged transcripts, the later
// stays shared (claude-swap's _profile_copy_wins), unless live reports a
// session writing the shared copy at rel. The other is parked under
// shared/ or profile/, for where it came from.
func mergeHistoryLive(src, dest, conflicts string, live func(rel string) bool) (moved, parked int, err error) {
	fi, err := os.Lstat(dest)
	if errors.Is(err, fs.ErrNotExist) || err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		return 0, 0, nil // nothing of its own, or already the link
	}
	if err != nil {
		return 0, 0, err
	}
	if same, _ := os.Stat(src); same != nil && os.SameFile(same, fi) {
		return 0, 0, errors.New("the profile's history is the shared one")
	}
	if !fi.IsDir() {
		return 0, 0, appendLines(src, dest)
	}
	var files []string
	err = filepath.WalkDir(dest, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink == 0 && !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	stamp := time.Now().UTC().Format("2006-01-02T15-04-05")
	for _, p := range files {
		rel, _ := filepath.Rel(dest, p)
		target := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return moved, parked, err
		}
		have, terr := os.ReadFile(target)
		switch {
		case errors.Is(terr, fs.ErrNotExist):
			if err := os.Rename(p, target); err != nil {
				return moved, parked, err
			}
			moved++
		case terr != nil:
			return moved, parked, terr
		default:
			mine, err := os.ReadFile(p)
			if err != nil {
				return moved, parked, err
			}
			if bytes.HasPrefix(have, mine) { // the shared copy holds it all
				if err := os.Remove(p); err != nil {
					return moved, parked, err
				}
				continue
			}
			run := filepath.Join(conflicts, stamp+"-"+filepath.Base(filepath.Dir(dest)))
			loser, side := p, "profile"
			if filepath.Ext(p) == ".jsonl" && profileWins(target, p) && !live(rel) { // claude-swap: transcripts only
				loser, side = target, "shared"
			}
			if err := park(loser, filepath.Join(run, side, rel)); err != nil {
				return moved, parked, err
			}
			if side == "shared" {
				if err := os.Rename(p, target); err != nil {
					return moved, parked, err
				}
			}
			parked++
		}
	}
	// The folders left are empty: deepest first.
	var dirs []string
	filepath.WalkDir(dest, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			dirs = append(dirs, p)
		}
		return nil
	})
	slices.Reverse(dirs)
	for _, d := range dirs {
		if err := os.Remove(d); err != nil {
			return moved, parked, err
		}
	}
	return moved, parked, nil
}

// appendLines adds to src the lines of dest it lacks, then removes dest.
func appendLines(src, dest string) error {
	have := map[string]bool{}
	if data, err := os.ReadFile(src); err == nil {
		for _, l := range strings.Split(string(data), "\n") {
			have[l] = true
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		return err
	}
	var add []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		if l := sc.Text(); l != "" && !have[l] {
			add, have[l] = append(add, l), true
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if len(add) > 0 {
		f, err := os.OpenFile(src, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		prefix := ""
		if fi, _ := f.Stat(); fi != nil && fi.Size() > 0 {
			if last, _ := os.ReadFile(src); len(last) > 0 && last[len(last)-1] != '\n' {
				prefix = "\n"
			}
		}
		_, werr := f.WriteString(prefix + strings.Join(add, "\n") + "\n")
		if err := errors.Join(werr, f.Close()); err != nil {
			return err
		}
	}
	return os.Remove(dest)
}

// park moves path to dest, or dest.2, dest.3...: never over a file.
func park(path, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	to := dest
	for n := 2; ; n++ {
		if _, err := os.Lstat(to); errors.Is(err, fs.ErrNotExist) {
			return os.Rename(path, to)
		}
		to = dest + "." + strconv.Itoa(n)
	}
}

// profileWins reports whether the profile's copy of a transcript should
// replace the shared one: the later last timestamp, then having one at
// all, then more lines; the shared copy otherwise.
func profileWins(shared, profile string) bool {
	s, p := lastTimestamp(shared), lastTimestamp(profile)
	if s != p {
		return s == "" || p != "" && p > s
	}
	return lineCount(profile) > lineCount(shared)
}

// lastTimestamp is the last "timestamp" in a JSONL transcript, read from
// the end: Claude closes most with untimestamped lines. ISO-8601 Zulu, so
// it compares as text.
func lastTimestamp(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		var e struct {
			Timestamp string `json:"timestamp"`
		}
		if json.Unmarshal([]byte(lines[i]), &e) == nil && e.Timestamp != "" {
			return e.Timestamp
		}
	}
	return ""
}

func lineCount(path string) int {
	data, _ := os.ReadFile(path)
	return bytes.Count(data, []byte("\n"))
}
