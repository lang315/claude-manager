package view

import (
	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/account"
)

// runUI is the Open in Terminal dialog.
type runUI struct {
	open   bool
	acct   account.Account
	folder string
	share  account.Share
}

// startRun opens the Open in Terminal dialog for b.
func (l *limits) startRun(a *App, b account.Account) {
	l.run = runUI{open: true, acct: b, share: account.Share{Setup: true, History: !a.prefs.NoShareHistory}}
}

func (l *limits) runDialog(c *ui.Context, a *App) {
	u := &l.run
	ui.Modal(c, &u.open, func() {
		ui.Column(c).Gap(12).Padding(20).Width(480).Children(func() {
			ui.Text(c, "Open in Terminal as "+u.acct.Email).FontSize(17).Bold()
			ui.Text(c, "Claude Code runs as this account in one Terminal window; the rest of the Mac stays on the account in use. Until claude quits there, the account's sign-in renews in that terminal.").Wrap()
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				shown := u.folder
				if shown == "" {
					shown = "No folder chosen"
				}
				ui.Text(c, shown).Grow(1).MinWidth(0).SingleLine()
				if ui.Button(c, "Choose Folder…").Clicked() {
					a.Go(func() {
						folder, err := a.ChooseFolder("Open Claude Code in this folder as " + u.acct.Email + ".")
						a.Update(func() {
							if folder != "" {
								u.folder = folder
							}
							l.toast(a, err)
						})
					})
				}
			})
			if u.acct.Terminal.Dir != "" {
				ui.Text(c, "It is open in a terminal already: this opens its profile again as it is.").FontSize(12).Wrap()
			} else {
				ui.Checkbox(c, &u.share.Setup, "Share settings, CLAUDE.md, skills and MCP servers")
				ui.Checkbox(c, &u.share.History, "Share chat history with the other accounts")
			}
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if ui.Button(c, "Cancel").Clicked() {
					u.open = false
				}
				if ui.PrimaryButton(c, "Open").Disabled(u.folder == "").Clicked() {
					u.open = false
					b, folder, share := u.acct, u.folder, u.share
					a.Go(func() {
						err := a.Accounts.Run(b.Key(), folder, share)
						a.Update(func() {
							if err == nil {
								a.notice = "Opened Claude Code in Terminal as " + b.Email + "."
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
