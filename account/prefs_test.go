package account

import "testing"

func TestSettings(t *testing.T) {
	b, _, _, _ := switchBook(t)
	for key, bad := range map[string]string{
		"autoswitch.threshold": "40", "autoswitch.intervalSeconds": "5", "autoswitch.strategy": "worst",
		"ui.theme": "pink", "updates.check": "maybe", "autoswitch.unhealthyTicks": "0", "nope": "1",
		"menubar.showAccountName": "maybe", "menubar.titleScoped": "2",
	} {
		if err := b.Set(key, bad); err == nil {
			t.Errorf("%s=%s taken", key, bad)
		}
	}
	for key, v := range map[string]string{
		"autoswitch.threshold": "80", "autoswitch.weeklyThreshold": "97", "autoswitch.intervalSeconds": "30",
		"autoswitch.cooldownSeconds": "0", "autoswitch.hysteresisPct": "5", "autoswitch.strategy": WeeklyFirst,
		"autoswitch.includeApiKeyAccounts": "true", "autoswitch.unhealthyTicks": "5", "autoswitch.model": "Fable",
		"autoswitch.enabled": "true", "ui.theme": "dark", "run.shareHistory": "false", "updates.check": "false",
		"usage.headerProbe": "true", "menubar.showAccountName": "false", "menubar.titleScoped": "yes",
	} {
		must(t, b.Set(key, v))
	}
	got := map[string]string{}
	all, err := b.Settings()
	must(t, err)
	for _, s := range all {
		got[s.Key] = s.Value
	}
	if got["autoswitch.threshold"] != "80" || got["autoswitch.weeklyThreshold"] != "97" || got["autoswitch.intervalSeconds"] != "30" ||
		got["autoswitch.cooldownSeconds"] != "0" || got["autoswitch.strategy"] != WeeklyFirst || got["autoswitch.unhealthyTicks"] != "5" ||
		got["ui.theme"] != "dark" || got["run.shareHistory"] != "false" || got["updates.check"] != "false" || got["usage.headerProbe"] != "true" ||
		got["autoswitch.enabled"] != "true" || got["autoswitch.includeApiKeyAccounts"] != "true" || got["autoswitch.model"] != "Fable" || got["autoswitch.hysteresisPct"] != "5" ||
		got["menubar.showAccountName"] != "false" || got["menubar.titleScoped"] != "true" {
		t.Errorf("settings %v", got)
	}
	again := &Book{Dir: b.Dir, Vault: b.Vault, Now: b.Now}
	p, _ := again.Prefs()
	if p.Theme != "dark" || !p.NoShareHistory || !p.NoUpdates || !p.NoTrayName || !p.TrayScoped {
		t.Errorf("prefs read back %+v", p)
	}
	for _, k := range settingKeys {
		must(t, b.Set(k, ""))
	}
	all, _ = b.Settings()
	for _, s := range all {
		if s.Value != s.Default {
			t.Errorf("unset %s = %q, default %q", s.Key, s.Value, s.Default)
		}
	}
	if err := b.SetPrefs(Prefs{Theme: "pink"}); err == nil {
		t.Error("theme pink")
	}
}

func isSetOf(t *testing.T, b *Book, key string) bool {
	t.Helper()
	all, err := b.Settings()
	must(t, err)
	for _, s := range all {
		if s.Key == key {
			return s.IsSet
		}
	}
	t.Fatalf("no setting %s", key)
	return false
}

// cswap: a setting is set when its key is in the file, whatever its value;
// unset removes the key.
func TestSettingIsSet(t *testing.T) {
	b, _, _, _ := switchBook(t)
	if isSetOf(t, b, "ui.theme") {
		t.Error("fresh book: ui.theme set")
	}
	must(t, b.Set("ui.theme", "auto")) // the default, stored
	must(t, b.Set("autoswitch.threshold", "80"))
	must(t, b.Set("menubar.showAccountName", "true")) // the default, inverted field
	for _, k := range []string{"ui.theme", "autoswitch.threshold", "menubar.showAccountName"} {
		if !isSetOf(t, b, k) {
			t.Errorf("%s not set", k)
		}
	}
	again := &Book{Dir: b.Dir, Vault: b.Vault, Now: b.Now}
	if !isSetOf(t, again, "ui.theme") || isSetOf(t, again, "updates.check") {
		t.Error("set keys not read back")
	}
	must(t, b.Set("ui.theme", ""))
	if isSetOf(t, b, "ui.theme") || !isSetOf(t, b, "autoswitch.threshold") {
		t.Error("unset kept or dropped the wrong key")
	}
}

// The GUI's setters store a key too, when they change its value.
func TestSettingIsSetByGUI(t *testing.T) {
	b, _, _, _ := switchBook(t)
	s, _ := b.AutoInEffect()
	s.Line = 70
	must(t, b.SetAuto(s))
	p, _ := b.Prefs()
	p.TrayTitle = "7d"
	must(t, b.SetPrefs(p))
	if !isSetOf(t, b, "autoswitch.threshold") || !isSetOf(t, b, "menubar.title") || isSetOf(t, b, "ui.theme") {
		t.Error("GUI setters not recorded")
	}
}

// A file from before the record: set when the value is not the default; the
// first set must not lose those.
func TestSettingIsSetOldFile(t *testing.T) {
	b, _, _, _ := switchBook(t)
	must(t, b.SetPrefs(Prefs{Theme: "dark"}))
	b.mu.Lock()
	b.prefs.Stored = nil // as if written before the record
	must(t, b.save())
	b.mu.Unlock()
	if !isSetOf(t, b, "ui.theme") || isSetOf(t, b, "updates.check") {
		t.Error("old file: value != default is not the rule")
	}
	must(t, b.Set("updates.check", "true"))
	if !isSetOf(t, b, "ui.theme") || !isSetOf(t, b, "updates.check") {
		t.Error("first set lost the old file's keys")
	}
}

// The menu bar title's default is compact, stored as ""; every other mode is
// stored by name, so a file from before compact keeps an explicit "both".
func TestMenubarTitleModes(t *testing.T) {
	b, _, _, _ := switchBook(t)
	get := func() Setting {
		t.Helper()
		all, err := b.Settings()
		must(t, err)
		for _, s := range all {
			if s.Key == "menubar.title" {
				return s
			}
		}
		t.Fatal("no menubar.title")
		return Setting{}
	}
	if s := get(); s.Value != "compact" || s.Default != "compact" || s.Range != "compact, both, 5h, 7d, off" {
		t.Errorf("default %+v", s)
	}
	for v, stored := range map[string]string{"both": "both", "7d": "7d", "compact": "", "off": "off"} {
		must(t, b.Set("menubar.title", v))
		if p, _ := b.Prefs(); p.TrayTitle != stored {
			t.Errorf("%s stored %q, want %q", v, p.TrayTitle, stored)
		}
		if s := get(); s.Value != v {
			t.Errorf("%s reads back %q", v, s.Value)
		}
	}
	if err := b.Set("menubar.title", "bad"); err == nil || err.Error() != "menubar.title is compact, both, 5h, 7d or off." {
		t.Errorf("bad: %v", err)
	}
}
