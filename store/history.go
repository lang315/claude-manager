package store

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"time"
)

// Prompt is one line of history.jsonl: a prompt typed into Claude Code.
type Prompt struct {
	Line      int       // its line in history.jsonl, from 1: the prompt's key
	Text      string    // the prompt as typed, paste placeholders included
	Time      time.Time // zero when the line has no timestamp
	Project   string    // the working directory
	SessionID string
	Pastes    []Paste // the pastes kept in the line, by ID
	Lost      int     // pastes kept only by hash, which can't be read
}

// Paste is text pasted into a prompt, which shows in it as a placeholder.
type Paste struct {
	ID      int
	Content string
}

// History reads history.jsonl, newest first; prompts without a time come
// last. Lines that are not JSON are skipped and counted in bad. A missing
// file is no prompts and no error. It only reads: Claude Code appends to the
// file and recalls prompts from it.
func (s *Store) History() (prompts []Prompt, bad int, err error) {
	f, err := os.Open(filepath.Join(s.Root, "history.jsonl"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	r := bufio.NewReader(f) // not a Scanner: a pasted text can make a line megabytes long
	for n := 1; ; n++ {
		line, rerr := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			if p, ok := parsePrompt(line); ok {
				p.Line = n
				prompts = append(prompts, p)
			} else {
				bad++
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return nil, 0, rerr
		}
	}
	// Newest first; the zero time, year 1, sorts last.
	slices.SortStableFunc(prompts, func(a, b Prompt) int { return b.Time.Compare(a.Time) })
	return prompts, bad, nil
}

// parsePrompt reads one line of history.jsonl.
func parsePrompt(line []byte) (Prompt, bool) {
	var r struct {
		Display        string `json:"display"`
		Timestamp      int64  `json:"timestamp"`
		Project        string `json:"project"`
		SessionID      string `json:"sessionId"`
		PastedContents map[string]struct {
			ID      int     `json:"id"`
			Type    string  `json:"type"`
			Content *string `json:"content"`
		} `json:"pastedContents"`
	}
	if err := json.Unmarshal(line, &r); err != nil {
		return Prompt{}, false
	}
	p := Prompt{Text: r.Display, Project: r.Project, SessionID: r.SessionID}
	if r.Timestamp > 0 {
		p.Time = time.UnixMilli(r.Timestamp)
	}
	for _, x := range r.PastedContents {
		switch {
		case x.Type != "" && x.Type != "text":
		case x.Content == nil:
			p.Lost++
		default:
			p.Pastes = append(p.Pastes, Paste{x.ID, *x.Content})
		}
	}
	slices.SortFunc(p.Pastes, func(a, b Paste) int { return cmp.Compare(a.ID, b.ID) })
	return p, true
}

// pastePlaceholder is how a pasted text shows in a prompt: [Pasted text #1
// +20 lines], or [Pasted text #1] for a short one.
var pastePlaceholder = regexp.MustCompile(`\[Pasted text #(\d+)(?: \+\d+ lines)?\]`)

// Expand returns the prompt with each paste placeholder replaced by the
// text pasted. Placeholders of lost pastes, and of images, stay.
func (p Prompt) Expand() string {
	return pastePlaceholder.ReplaceAllStringFunc(p.Text, func(m string) string {
		id, _ := strconv.Atoi(pastePlaceholder.FindStringSubmatch(m)[1])
		for _, x := range p.Pastes {
			if x.ID == id {
				return x.Content
			}
		}
		return m
	})
}
