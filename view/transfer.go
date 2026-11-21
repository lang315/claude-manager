package view

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

const (
	exportWarning = "This file holds your sign-ins, unencrypted. Keep it private and delete it after import."
	twoMacs       = "Use each account on one Mac only: two Macs renewing one sign-in break it."
)

// exportUI is the Export dialog: whether it is open, and whether the file
// is a full backup for this Mac.
type exportUI struct{ open, full bool }

// startExport opens the Export dialog.
func (l *limits) startExport(a *App) { l.export = exportUI{open: true} }

func (l *limits) exportDialog(c *ui.Context, a *App) {
	u := &l.export
	ui.Modal(c, &u.open, func() {
		ui.Column(c).Gap(12).Padding(20).Width(460).Children(func() {
			ui.Text(c, "Export Accounts").FontSize(17).Bold()
			ui.Text(c, exportWarning+" "+twoMacs).Wrap()
			ui.Checkbox(c, &u.full, "Full backup for this Mac")
			ui.Text(c, "Also the account in use's MCP sign-ins and the whole of ~/.claude.json, as claude-swap's --full. Importing takes only the sign-ins either way.").FontSize(12).Wrap()
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if ui.Button(c, "Cancel").Clicked() {
					u.open = false
				}
				if ui.PrimaryButton(c, "Choose File…").Clicked() {
					u.open = false
					l.exportTo(a, u.full)
				}
			})
		})
	})
}

// exportTo asks where to save the added accounts, then writes them.
func (l *limits) exportTo(a *App, full bool) {
	a.Go(func() {
		path, err := a.SaveFile("Claude accounts.json", exportWarning+" "+twoMacs)
		if err != nil || path == "" {
			a.Update(func() { l.toast(a, err) })
			return
		}
		n, left, err := a.Accounts.Export(path, full)
		a.Update(func() {
			if err == nil {
				a.notice = fmt.Sprintf("Exported %s. ", plural(n, "account"))
				if left > 0 {
					a.notice += fmt.Sprintf("Left out %d: sign in again to it first. ", left)
				}
				a.notice += exportWarning + " " + twoMacs
			}
			l.toast(a, err)
		})
	})
}

// startImport asks for an exported file, then adds its accounts.
func (l *limits) startImport(a *App) {
	a.Go(func() {
		path, err := a.OpenFile(twoMacs)
		if err != nil || path == "" {
			a.Update(func() { l.toast(a, err) })
			return
		}
		got, err := a.Accounts.Import(path)
		a.Update(func() {
			var did []string
			if got.Added > 0 {
				did = append(did, fmt.Sprintf("added %s", plural(got.Added, "account")))
			}
			if got.Updated > 0 {
				did = append(did, fmt.Sprintf("updated %d", got.Updated))
			}
			if got.Skipped > 0 {
				did = append(did, fmt.Sprintf("left %d as they were", got.Skipped))
			}
			if err == nil && len(did) > 0 {
				a.notice = "Imported: " + strings.Join(did, ", ") + ". " + twoMacs
			}
			l.toast(a, err)
			if got.Added+got.Updated > 0 {
				l.load(a, true)
			}
		})
	})
}
