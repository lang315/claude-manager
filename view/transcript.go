package view

import (
	"fmt"
	"sync/atomic"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/lang315/claude-manager/store"
)

type msgRow struct {
	store.Message
	open  bool   // a collapsed block (thinking, tool call, tool result) is expanded
	label string // the collapsed block's label, built once
}

// newRow builds a row, cutting the text it labels at 512 bytes first: a tool
// result can be megabytes long and the label is drawn every frame.
func newRow(m store.Message) msgRow {
	p := m.Text
	if len(p) > 512 {
		n := 512
		for !utf8.RuneStart(p[n]) {
			n--
		}
		p = p[:n]
	}
	return msgRow{Message: m, label: m.Kind + " · " + store.FirstLine(p, 120)}
}

// openTranscript shows a session's transcript, its messages coming in as
// they are read.
func (s *sessions) openTranscript(a *App, x store.Session) {
	s.open, s.msgs, s.bad, s.loading = &x, nil, 0, true
	s.list = &ui.ListState{}
	s.gen++
	gen := s.gen
	s.stopReading()
	stop := new(atomic.Bool)
	s.stop = stop
	a.run(func() func() {
		bad, err := store.ReadTranscript(x.File, stop.Load, func(ms []store.Message) {
			a.Update(func() {
				if s.gen != gen {
					return
				}
				for _, m := range ms {
					s.msgs = append(s.msgs, newRow(m))
				}
			})
		})
		return func() {
			if s.gen != gen {
				return
			}
			s.bad, s.loading = bad, false
			if err != nil {
				a.notice = err.Error()
			}
		}
	})
}

// stopReading stops the read of the transcript open, which frees Move to
// Trash as soon as the read returns.
func (s *sessions) stopReading() {
	if s.stop != nil {
		s.stop.Store(true)
	}
}

func (s *sessions) transcript(c *ui.Context, pal *palette) {
	t := c.Theme()
	x := s.open
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		if ui.Button(c, "Back").Clicked() {
			s.open, s.msgs, s.bad, s.loading = nil, nil, 0, false
			s.gen++ // messages still arriving are dropped
			s.stopReading()
		}
		if x.Live {
			lamp(c, pal)
		}
		ui.Text(c, sessionName(x)).FontSize(15).Bold().SingleLine().Grow(1).MinWidth(0)
	})
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		meta := fmt.Sprintf("%s · %s · Claude Code %s · %s – %s · %s",
			x.Cwd, x.Branch, x.Version, fmtTime(x.Started), fmtTime(x.Modified), plural(len(s.msgs), "message"))
		if s.bad > 0 {
			meta += " · " + plural(s.bad, "unreadable line")
		}
		ui.Text(c, meta).TextColor(t.TextMuted).FontSize(12).SingleLine().MinWidth(0)
		if s.loading {
			ui.Spinner(c).Label("Loading messages")
		}
	})
	ui.List(c, s.list, len(s.msgs), func(i int) { s.message(c, pal, &s.msgs[i]) }).Grow(1).Gap(6)
}

func (s *sessions) message(c *ui.Context, pal *palette, m *msgRow) {
	t := c.Theme()
	who := "You"
	switch {
	case m.Kind == "tool_result":
		who = "Tool" // tool results come back in user records
	case m.Role == "assistant":
		who = "Claude"
	}
	col := ui.Column(c).Gap(4).PaddingX(14).PaddingY(10).Radius(12)
	if who == "You" {
		col.Background(pal.face).Border(1, pal.rule) // your turns stand apart from Claude's
	}
	col.Children(func() {
		ui.Row(c).Gap(6).Children(func() {
			ui.Text(c, who).FontSize(12).Bold()
			ui.Text(c, fmtTime(m.Time)).FontSize(12).TextColor(t.TextMuted)
		})
		if m.Kind == "text" {
			markdown.View(c, m.Text, &s.md)
			return
		}
		ui.Collapsible(c, m.label, &m.open, func() {
			ui.Text(c, clip(m.Text)).Font("monospace").Selectable()
		})
	})
}
