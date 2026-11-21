package view

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/account"
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
			l.fail(a, err)
		})
	})
}

// importUI is the Import dialog: whether readings from another Mac are
// held, and for how many minutes.
type importUI struct {
	open, hold bool
	minutes    float64
}

// startImport opens the Import dialog.
func (l *limits) startImport(a *App) { l.importing = importUI{open: true, minutes: 10} }

func (l *limits) importDialog(c *ui.Context, a *App) {
	u := &l.importing
	ui.Modal(c, &u.open, func() {
		ui.Column(c).Gap(12).Padding(20).Width(480).Children(func() {
			ui.Text(c, "Import").FontSize(17).Bold()
			ui.Text(c, "An accounts file exported by Claude Manager or claude-swap adds its accounts. "+twoMacs).Wrap()
			ui.Text(c, "A readings file from another Mac (cswap list --json) takes its limits where they are newer, so the two Macs don't both ask Anthropic.").Wrap()
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Checkbox(c, &u.hold, "Don't read those accounts here for")
				ui.NumberInput(c, &u.minutes, 1, 60, 1).Disabled(!u.hold)
				ui.Text(c, "minutes")
			})
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if ui.Button(c, "Cancel").Clicked() {
					u.open = false
				}
				if ui.PrimaryButton(c, "Choose File…").Clicked() {
					u.open = false
					hold := time.Duration(-1)
					if u.hold {
						hold = time.Duration(u.minutes) * time.Minute
					}
					l.importFrom(a, hold)
				}
			})
		})
	})
}

// importFrom asks for a file, then adds its accounts or takes its readings.
func (l *limits) importFrom(a *App, hold time.Duration) {
	a.Go(func() {
		path, err := a.OpenFile(twoMacs)
		if err != nil || path == "" {
			a.Update(func() { l.toast(a, err) })
			return
		}
		if data, err := os.ReadFile(path); err == nil && account.IsReadings(data) {
			got, err := a.Accounts.ImportUsage(path, hold)
			blocks, _ := a.Accounts.Blocks() // off the main thread: it waits for a refresh under way
			a.Update(func() {
				if err == nil {
					a.notice = fmt.Sprintf("Readings: %d taken, %d kept (newer here), %d skipped.", got.Adopted, got.Kept, got.Skipped)
				}
				l.toast(a, err)
				if got.Adopted > 0 {
					l.blocks = blocks
				}
			})
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
			l.fail(a, err)
			if got.Added+got.Updated > 0 {
				l.load(a, true)
			}
		})
	})
}
