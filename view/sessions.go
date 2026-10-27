package view

import (
	"cmp"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/lang315/claude-manager/store"
)

type sessions struct {
	projects []store.Project
	err      string
	split    float32
	ptable   ui.DataTableState[store.Project, string]
	table    ui.DataTableState[store.Session, string]
	shown    string // Dir of the project whose sessions the table shows
	rows     []store.Session
	asking   bool
	pending  []store.Session
	skipped  int

	// The transcript shown, nil while the tables show (transcript.go).
	open    *store.Session
	msgs    []msgRow
	bad     int
	loading bool
	gen     int // which open the messages arriving belong to
	loadGen int // which load the projects arriving belong to
	list    *ui.ListState
	md      markdown.Options
}

const liveSuffix = " (live)"

func sessionLabel(x *store.Session) string {
	label := cmp.Or(x.Title, x.FirstPrompt, "(untitled)")
	if x.Err != "" {
		label = "(unreadable: " + x.Err + ")"
	}
	if x.Live {
		label += liveSuffix
	}
	return label
}

func (s *sessions) init() {
	s.split = 360
	bySize := func(x, y *store.Project) int { return cmp.Compare(x.Size, y.Size) }
	s.ptable.Key = func(p *store.Project) string { return p.Dir }
	s.ptable.NoSearchBar = true
	s.ptable.Columns = []ui.DataColumn[store.Project]{
		{TableColumn: ui.TableColumn{Title: "Project", Width: 150}, Text: func(p *store.Project) string { return p.Path },
			Cell:    func(c *ui.Context, p *store.Project) { ui.Text(c, shortPath(p.Path)).SingleLine().Tooltip(p.Path) },
			Compare: func(x, y *store.Project) int { return cmp.Compare(x.Path, y.Path) }},
		{TableColumn: ui.TableColumn{Title: "Sessions", Width: 90, Align: ui.End}, Text: func(p *store.Project) string { return strconv.Itoa(len(p.Sessions)) },
			Compare: func(x, y *store.Project) int { return cmp.Compare(len(x.Sessions), len(y.Sessions)) }},
		{TableColumn: ui.TableColumn{Title: "Size", Width: 80, Align: ui.End}, Text: func(p *store.Project) string { return humanSize(p.Size) }, Compare: bySize},
	}

	s.table.Key = func(x *store.Session) string { return x.ID }
	s.table.Footer = true
	s.table.SearchDelay = 200 * time.Millisecond
	s.table.Columns = []ui.DataColumn[store.Session]{
		{TableColumn: ui.TableColumn{Title: "Session", MinWidth: 120}, Text: sessionLabel,
			Match: func(x *store.Session, kw string) bool {
				return containsFold(x.Title, kw) || containsFold(x.FirstPrompt, kw)
			},
			Cell: func(c *ui.Context, x *store.Session) {
				if !x.Live {
					ui.Text(c, sessionLabel(x)).SingleLine()
					return
				}
				// A live session can't be trashed: dim it and say why.
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, strings.TrimSuffix(sessionLabel(x), liveSuffix)).SingleLine().TextColor(c.Theme().TextMuted).Grow(1).MinWidth(0)
					ui.Badge(c, "Live").Tooltip("Running now; it can't be moved to the Trash")
				})
			}},
		{TableColumn: ui.TableColumn{Title: "Modified", Width: 140}, Text: func(x *store.Session) string { return fmtTime(x.Modified) }, NoSearch: true,
			Compare: func(x, y *store.Session) int { return x.Modified.Compare(y.Modified) }},
		{TableColumn: ui.TableColumn{Title: "Size", Width: 80, Align: ui.End}, Text: func(x *store.Session) string { return humanSize(x.Size) }, NoSearch: true,
			Compare: func(x, y *store.Session) int { return cmp.Compare(x.Size, y.Size) }},
		{TableColumn: ui.TableColumn{Title: "Subagents", Width: 90, Align: ui.End}, Text: func(x *store.Session) string { return strconv.Itoa(x.Subagents) }, NoSearch: true,
			Compare: func(x, y *store.Session) int { return cmp.Compare(x.Subagents, y.Subagents) }},
	}
}

func (s *sessions) load(a *App) {
	days := int(a.cleanup.days)
	a.cleanup.gen++
	gen := a.cleanup.gen
	s.loadGen++
	loadGen := s.loadGen
	a.run(func() func() {
		projects, err := a.Store.Projects()
		groups := a.Store.Cleanup(projects, days)
		return func() {
			if loadGen == s.loadGen {
				s.set(projects, err)
			}
			if gen == a.cleanup.gen {
				a.cleanup.set(groups)
			}
		}
	})
}

func (s *sessions) set(projects []store.Project, err error) {
	s.projects, s.err = projects, errText(err)
	s.ptable.Reset()
	s.shown = "" // the sessions table takes the new rows on the next frame
}

func (s *sessions) view(c *ui.Context, a *App) {
	if s.open != nil {
		s.transcript(c)
		return
	}
	if s.err != "" {
		ui.Text(c, s.err).TextColor(c.Theme().Danger)
	}
	ui.Split(c, &s.split, func() {
		ui.Column(c).Fill().Children(func() {
			ui.DataTable(c, &s.ptable, s.projects).Grow(1)
		})
	}, func() {
		ui.Column(c).Fill().Gap(8).Children(func() { s.sessionTable(c, a) })
	}).Grow(1)
}

func (s *sessions) sessionTable(c *ui.Context, a *App) {
	p := s.ptable.Current()
	if p == nil {
		ui.Text(c, "Choose a project to see its sessions.").TextColor(c.Theme().TextMuted)
		return
	}
	if p.Dir != s.shown {
		s.shown, s.rows = p.Dir, p.Sessions
		s.table.Checked.Clear()
		s.table.Chosen.Clear()
		s.table.Reset()
	}
	targets, live := s.targets()
	ui.Row(c).Gap(8).Children(func() {
		cur := s.table.Current()
		if ui.Button(c, "Open Transcript").Disabled(cur == nil).Clicked() {
			s.openTranscript(a, *cur)
		}
		if ui.Button(c, "Move to Trash…").Disabled(len(targets) == 0 || a.busy > 0).Clicked() {
			s.pending, s.skipped, s.asking = targets, live, true
		}
	})
	ui.DataTable(c, &s.table, s.rows).Grow(1)
	s.confirm(c, a)
}

// targets are the sessions to trash, less the live ones, which it counts.
func (s *sessions) targets() (out []store.Session, live int) {
	for _, x := range picked(&s.table, s.rows) {
		if x.Live {
			live++
		} else {
			out = append(out, *x)
		}
	}
	return out, live
}

func (s *sessions) confirm(c *ui.Context, a *App) {
	var size int64
	for _, x := range s.pending {
		size += x.Size
	}
	msg := humanSize(size) + " moves to the Trash, with each session's subagents, file history and environment. You can put it back from the Trash."
	if s.skipped > 0 {
		msg += fmt.Sprintf(" Running sessions are left alone (%d).", s.skipped)
	}
	title := fmt.Sprintf("Move %s to the Trash?", plural(len(s.pending), "session"))
	if ui.AlertDialog(c, &s.asking, title, msg, "Cancel", "Move to Trash") == 1 {
		pending := s.pending
		a.trash(func() []string {
			var paths []string
			for _, x := range pending {
				paths = append(paths, a.Store.SessionPaths(x)...)
			}
			return paths
		})
	}
}

// shortPath keeps a project path's last two components: the long prefix is
// the same for every project.
func shortPath(p string) string {
	if d := filepath.Dir(p); d != "/" && d != "." {
		return filepath.Base(d) + "/" + filepath.Base(p)
	}
	return p
}
