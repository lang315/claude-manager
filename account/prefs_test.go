package account

import "testing"

func TestSettings(t *testing.T) {
	b, _, _, _ := switchBook(t)
	for key, bad := range map[string]string{
		"autoswitch.threshold": "40", "autoswitch.intervalSeconds": "5", "autoswitch.strategy": "worst",
		"ui.theme": "pink", "updates.check": "maybe", "autoswitch.unhealthyTicks": "0", "nope": "1",
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
		"usage.headerProbe": "true",
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
		got["autoswitch.enabled"] != "true" || got["autoswitch.includeApiKeyAccounts"] != "true" || got["autoswitch.model"] != "Fable" || got["autoswitch.hysteresisPct"] != "5" {
		t.Errorf("settings %v", got)
	}
	again := &Book{Dir: b.Dir, Vault: b.Vault, Now: b.Now}
	p, _ := again.Prefs()
	if p.Theme != "dark" || !p.NoShareHistory || !p.NoUpdates {
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
