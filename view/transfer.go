package view

import (
	"fmt"
	"strings"
)

const (
	exportWarning = "This file holds your sign-ins, unencrypted. Keep it private and delete it after import."
	twoMacs       = "Use each account on one Mac only: two Macs renewing one sign-in break it."
)

// startExport asks where to save the added accounts, then writes them.
func (l *limits) startExport(a *App) {
	a.Go(func() {
		path, err := a.SaveFile("Claude accounts.json", exportWarning+" "+twoMacs)
		if err != nil || path == "" {
			a.Update(func() { l.toast(a, err) })
			return
		}
		n, left, err := a.Accounts.Export(path)
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
