package view

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/lang315/claude-manager/store"
)

type msgRow struct {
	store.Message
	open bool // a collapsed block (thinking, tool call, tool result) is expanded
}

// openTranscript shows a session's transcript, its messages coming in as
// they are read.
func (s *sessions) openTranscript(a *App, x store.Session) {
	s.open, s.msgs, s.bad, s.loading = &x, nil, 0, true
	s.list = &ui.ListState{}
	s.gen++
	gen := s.gen
	a.run(func() func() {
		bad, err := store.ReadTranscript(x.File, func(ms []store.Message) {
			a.Update(func() {
				if s.gen != gen {
					return
				}
				for _, m := range ms {
					s.msgs = append(s.msgs, msgRow{Message: m})
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

func (s *sessions) transcript(c *ui.Context) {
	t := c.Theme()
	x := s.open
	ui.Row(c).Gap(8).Children(func() {
		if ui.Button(c, "Back").Clicked() {
			s.open = nil
			s.gen++ // messages still arriving are dropped
		}
		ui.Text(c, sessionLabel(x)).Bold().NoWrap()
		if s.bad > 0 {
			ui.Badge(c, fmt.Sprintf("%d unreadable lines", s.bad))
		}
	})
	status := ""
	if s.loading {
		status = " · loading…"
	}
	ui.Textf(c, "%s · %s · Claude Code %s · %s – %s · %d messages%s",
		x.Cwd, x.Branch, x.Version, fmtTime(x.Started), fmtTime(x.Modified), len(s.msgs), status).
		TextColor(t.TextMuted).FontSize(12)
	ui.List(c, s.list, len(s.msgs), func(i int) { s.message(c, &s.msgs[i]) }).Grow(1).Gap(8)
}

func (s *sessions) message(c *ui.Context, m *msgRow) {
	t := c.Theme()
	who := "You"
	if m.Role == "assistant" {
		who = "Claude"
	}
	ui.Column(c).Gap(4).Padding(8).Children(func() {
		ui.Text(c, who+" · "+fmtTime(m.Time)).FontSize(11).TextColor(t.TextMuted)
		if m.Kind == "text" {
			markdown.View(c, m.Text, &s.md)
			return
		}
		ui.Collapsible(c, m.Kind+" · "+store.FirstLine(m.Text, 120), &m.open, func() {
			ui.Text(c, clip(m.Text)).Font("monospace").Selectable()
		})
	})
}
