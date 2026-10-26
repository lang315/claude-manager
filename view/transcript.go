package view

import (
	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/store"
)

type msgRow struct {
	store.Message
	open bool
}

func (s *sessions) openTranscript(a *App, x store.Session) {}

func (s *sessions) transcript(c *ui.Context) {}
