package view

import (
	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/account"
)

// tokenUI is the Add Token dialog.
type tokenUI struct {
	open         bool
	token, email string
	probe        bool
}

func (l *limits) startToken() {
	l.token = tokenUI{open: true, probe: l.auto.s.Probe}
}

func (l *limits) tokenDialog(c *ui.Context, a *App) {
	u := &l.token
	ui.Modal(c, &u.open, func() {
		ui.Column(c).Gap(12).Padding(20).Width(480).Children(func() {
			ui.Text(c, "Add Setup Token").FontSize(17).Bold()
			ui.Text(c, "A long-lived token from claude setup-token, added without a browser sign-in. Nothing is asked of Anthropic.").Wrap()
			ui.TextInput(c, &u.token).Placeholder("sk-ant-oat01-…").Password()
			ui.TextInput(c, &u.email).Placeholder("Email (optional): a label, as the token carries none")
			ui.Checkbox(c, &u.probe, "Read setup-token usage")
			ui.Text(c, "Anthropic's usage endpoint doesn't answer a setup-token: this reads its limits from 1-token requests, a Haiku one and a Fable one, each time it is read. Each is a real request on the account.").FontSize(12).Wrap()
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if ui.Button(c, "Cancel").Clicked() {
					u.open = false
				}
				if ui.PrimaryButton(c, "Add").Disabled(u.token == "").Clicked() {
					token, email, probe := u.token, u.email, u.probe
					u.open, u.token = false, ""
					a.Go(func() {
						_, err := a.Accounts.AddToken(token, email)
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
								a.notice = "Added the setup-token."
							}
							l.toast(a, err)
							l.load(a, true)
						})
					})
				}
			})
		})
	})
}

// tokenPlan is a setup-token account's plan, as the block shows it.
func tokenPlan(b account.Account) string {
	if b.Token {
		return "Setup-token"
	}
	return ""
}
