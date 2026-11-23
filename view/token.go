package view

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/account"
)

// tokenUI is the Add Token dialog.
type tokenUI struct {
	open         bool
	token, email string
	probe        bool
	slot         float64 // 0 for the next free one
	overwrite    bool    // the slot is taken: asking before moving its account
}

func (l *limits) startToken() {
	l.token = tokenUI{open: true, probe: l.auto.s.Probe}
}

func (l *limits) tokenDialog(c *ui.Context, a *App) {
	u := &l.token
	ui.Modal(c, &u.open, func() {
		ui.Column(c).Gap(12).Padding(20).Width(480).Children(func() {
			ui.Text(c, "Add a Token or API Key").FontSize(17).Bold()
			ui.Text(c, "A long-lived token from claude setup-token, or a Console API key, added without a browser sign-in. Nothing is asked of Anthropic. An API key has no subscription limits: nothing is read for it.").Wrap()
			ui.TextInput(c, &u.token).Placeholder("sk-ant-oat01-… or sk-ant-api03-…").Password()
			ui.TextInput(c, &u.email).Placeholder("Email (optional): a label, as the token carries none")
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Slot (0 for the next free)")
				ui.NumberInput(c, &u.slot, 0, 99, 1)
			})
			ui.Checkbox(c, &u.probe, "Read setup-token usage")
			ui.Text(c, "Anthropic's usage endpoint doesn't answer a setup-token: this reads its limits from 1-token requests, a Haiku one and a Fable one, each time it is read. Each is a real request on the account.").FontSize(12).Wrap()
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if ui.Button(c, "Cancel").Clicked() {
					u.open = false
				}
				if ui.PrimaryButton(c, "Add").Disabled(u.token == "").Clicked() {
					if n := int(u.slot); n > 0 && slices.ContainsFunc(l.blocks, func(b account.Account) bool { return b.Slot == n }) {
						u.overwrite = true // claude-swap asks: "Overwrite slot N?"
						return
					}
					l.addToken(a)
				}
			})
		})
	})
	if ui.AlertDialog(c, &u.overwrite, fmt.Sprintf("Slot %d is taken", int(u.slot)), "Its account moves to the slot the new one would have had.", "Cancel", "Move It") == 1 {
		l.addToken(a)
	}
}

func (l *limits) addToken(a *App) {
	u := &l.token
	token, email, probe, slot := u.token, u.email, u.probe, int(u.slot)
	u.open, u.token = false, ""
	a.Go(func() {
		var err error
		var added account.Account
		if account.IsAPIKey(strings.TrimSpace(token)) {
			added, err = a.Accounts.AddAPIKey(token, email)
		} else {
			added, err = a.Accounts.AddToken(token, email)
		}
		if err == nil && slot > 0 {
			blocks, _ := a.Accounts.Blocks()
			if i := slices.IndexFunc(blocks, func(b account.Account) bool { return b.Key() == added.Key() }); i >= 0 && blocks[i].Slot != slot {
				err = a.Accounts.Move(blocks[i].Slot, slot)
			}
		}
		if err == nil {
			if s, serr := a.Accounts.AutoSettings(); serr == nil && s.Probe != probe {
				s.Probe = probe
				err = a.Accounts.SetAuto(s)
			}
		}
		s, _ := a.Accounts.AutoSettings()
		a.Update(func() {
			l.auto.s = s
			if err == nil {
				a.notice = "Added."
			}
			l.fail(a, err)
			l.load(a, true)
		})
	})
}

// tokenPlan is a setup-token account's plan, as the block shows it.
func tokenPlan(b account.Account) string {
	switch {
	case b.Token:
		return "Setup-token"
	case b.APIKey:
		return "API key · no subscription limits"
	}
	return ""
}
