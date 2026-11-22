package account

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Prefs are the app's own settings beside auto-switch's, claude-swap's
// ui.theme, updates.check and run.shareHistory.
type Prefs struct {
	Theme          string `json:"theme,omitempty"`          // "auto" (as macOS), "light" or "dark"; "" is auto
	NoUpdates      bool   `json:"noUpdates,omitempty"`      // don't look for a newer version
	NoShareHistory bool   `json:"noShareHistory,omitempty"` // Open in Terminal shares no chat history unless asked
	TrayTitle      string `json:"trayTitle,omitempty"`      // the menu bar title: "" both windows, "5h", "7d" or "off" (claude-swap's title percentage)
	TrayEvery      int    `json:"trayEvery,omitempty"`      // seconds between the menu bar's refreshes; 0 is 300
}

// TrayInterval is how often the menu bar refreshes.
func (p Prefs) TrayInterval() time.Duration {
	if p.TrayEvery <= 0 {
		return 5 * time.Minute
	}
	return time.Duration(p.TrayEvery) * time.Second
}

// Prefs returns the app's settings.
func (b *Book) Prefs() (Prefs, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	err := b.load()
	return b.prefs, err
}

// SetPrefs saves the app's settings.
func (b *Book) SetPrefs(p Prefs) error {
	if !slices.Contains([]string{"", "auto", "light", "dark"}, p.Theme) {
		return fmt.Errorf("The theme is auto, light or dark, not %q.", p.Theme)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return err
	}
	b.prefs = p
	return b.save()
}

// Setting is one setting by claude-swap's name, for `claude-manager config`.
type Setting struct {
	Key, Value, Default, Range string
}

// settingKeys are the settings `config` knows, in claude-swap's names.
var settingKeys = []string{
	"autoswitch.threshold", "autoswitch.weeklyThreshold", "autoswitch.intervalSeconds", "autoswitch.cooldownSeconds",
	"autoswitch.hysteresisPct", "autoswitch.strategy", "autoswitch.includeApiKeyAccounts", "autoswitch.unhealthyTicks",
	"autoswitch.model", "autoswitch.enabled", "ui.theme", "run.shareHistory", "updates.check", "usage.headerProbe",
	"menubar.title", "menubar.refreshSeconds",
}

// Settings lists every setting with its value, default and range.
func (b *Book) Settings() ([]Setting, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return nil, err
	}
	var out []Setting
	for _, k := range settingKeys {
		out = append(out, settingOf(k, b.auto, b.prefs))
	}
	return out, nil
}

func settingOf(key string, s Auto, p Prefs) Setting {
	d, dp := DefaultAuto(), Prefs{}
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	bo := strconv.FormatBool
	ticks := func(a Auto) int {
		if a.Ticks == 0 {
			return 3
		}
		return a.Ticks
	}
	weekly := func(a Auto) string {
		if a.WeeklyLine == 0 {
			return ""
		}
		return f(a.WeeklyLine)
	}
	theme := func(p Prefs) string {
		if p.Theme == "" {
			return "auto"
		}
		return p.Theme
	}
	switch key {
	case "autoswitch.threshold":
		return Setting{key, f(s.Line), f(d.Line), "50–99.9"}
	case "autoswitch.weeklyThreshold":
		return Setting{key, weekly(s), "", "50–99.9, or unset for the threshold"}
	case "autoswitch.intervalSeconds":
		return Setting{key, f(s.Every().Seconds()), "60", "15–3600"}
	case "autoswitch.cooldownSeconds":
		return Setting{key, strconv.Itoa(s.Cooldown), strconv.Itoa(d.Cooldown), "0–86400"}
	case "autoswitch.hysteresisPct":
		return Setting{key, f(s.Margin), f(d.Margin), "0–50"}
	case "autoswitch.strategy":
		return Setting{key, s.Strategy, d.Strategy, strings.Join([]string{Best, WeeklyFirst, ConsumeFirst, ByPriority}, ", ")}
	case "autoswitch.includeApiKeyAccounts":
		return Setting{key, bo(s.Keys), "false", "true, false"}
	case "autoswitch.unhealthyTicks":
		return Setting{key, strconv.Itoa(ticks(s)), "3", "1–100"}
	case "autoswitch.model":
		return Setting{key, s.Model, "", "model names, comma separated, or all"}
	case "autoswitch.enabled":
		return Setting{key, bo(s.On), "false", "true, false"}
	case "ui.theme":
		return Setting{key, theme(p), theme(dp), "auto, light, dark"}
	case "run.shareHistory":
		return Setting{key, bo(!p.NoShareHistory), "true", "true, false"}
	case "updates.check":
		return Setting{key, bo(!p.NoUpdates), "true", "true, false"}
	case "menubar.title":
		t := p.TrayTitle
		if t == "" {
			t = "both"
		}
		return Setting{key, t, "both", "both, 5h, 7d, off"}
	case "menubar.refreshSeconds":
		return Setting{key, fmt.Sprint(int(p.TrayInterval().Seconds())), "300", "30, 60, 300"}
	case "usage.headerProbe":
		return Setting{key, bo(s.Probe), "false", "true, false"}
	}
	return Setting{}
}

var errNoSetting = errors.New("No such setting: claude-manager config lists them.")

// Set sets the setting key to value, checked against its range; unset
// (value "") puts it back to its default.
func (b *Book) Set(key, value string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return err
	}
	if !slices.Contains(settingKeys, key) {
		return errNoSetting
	}
	s, p, d := b.auto, b.prefs, DefaultAuto()
	unset := value == ""
	num := func(lo, hi float64) (float64, error) {
		v, err := strconv.ParseFloat(value, 64)
		if err != nil || !(v >= lo && v <= hi) { // so NaN is out too
			return 0, fmt.Errorf("%s takes a number from %g to %g.", key, lo, hi)
		}
		return v, nil
	}
	whole := func(lo, hi float64) (float64, error) { // claude-swap: "expects an integer"
		v, err := num(lo, hi)
		if err == nil && v != math.Trunc(v) {
			return 0, fmt.Errorf("%s takes a whole number from %g to %g.", key, lo, hi)
		}
		return v, err
	}
	boolean := func() (bool, error) {
		switch strings.ToLower(value) { // claude-swap's yes and no too
		case "yes":
			return true, nil
		case "no":
			return false, nil
		}
		v, err := strconv.ParseBool(value)
		if err != nil {
			return false, fmt.Errorf("%s takes true or false.", key)
		}
		return v, nil
	}
	var err error
	switch key {
	case "autoswitch.threshold":
		s.Line = d.Line
		if !unset {
			s.Line, err = num(50, 99.9)
		}
	case "autoswitch.weeklyThreshold":
		s.WeeklyLine = 0
		if !unset {
			s.WeeklyLine, err = num(50, 99.9)
		}
	case "autoswitch.intervalSeconds":
		var v float64
		if !unset {
			v, err = whole(15, 3600)
		}
		s.Interval = int(v)
	case "autoswitch.cooldownSeconds":
		v := float64(d.Cooldown)
		if !unset {
			v, err = whole(0, 86400)
		}
		s.Cooldown = int(v)
	case "autoswitch.hysteresisPct":
		s.Margin = d.Margin
		if !unset {
			s.Margin, err = num(0, 50)
		}
	case "autoswitch.strategy":
		s.Strategy = d.Strategy
		if !unset {
			if !slices.Contains([]string{Best, WeeklyFirst, ConsumeFirst, ByPriority}, value) {
				err = fmt.Errorf("%s is one of %s.", key, strings.Join([]string{Best, WeeklyFirst, ConsumeFirst, ByPriority}, ", "))
			}
			s.Strategy = value
		}
	case "autoswitch.includeApiKeyAccounts":
		s.Keys = false
		if !unset {
			s.Keys, err = boolean()
		}
	case "autoswitch.unhealthyTicks":
		var v float64
		if !unset {
			v, err = whole(1, 100)
		}
		s.Ticks = int(v)
	case "autoswitch.model":
		s.Model = strings.TrimSpace(value)
	case "autoswitch.enabled":
		s.On = false
		if !unset {
			s.On, err = boolean()
		}
	case "ui.theme":
		p.Theme = ""
		if !unset {
			if !slices.Contains([]string{"auto", "light", "dark"}, value) {
				err = fmt.Errorf("%s is auto, light or dark.", key)
			}
			p.Theme = value
		}
	case "run.shareHistory":
		v := true
		if !unset {
			v, err = boolean()
		}
		p.NoShareHistory = !v
	case "menubar.title":
		p.TrayTitle = ""
		if !unset && value != "both" {
			if !slices.Contains([]string{"5h", "7d", "off"}, value) {
				err = fmt.Errorf("%s is both, 5h, 7d or off.", key)
			}
			p.TrayTitle = value
		}
	case "menubar.refreshSeconds":
		p.TrayEvery = 0
		if !unset {
			var v float64
			if v, err = whole(30, 3600); err == nil {
				p.TrayEvery = int(v)
			}
		}
	case "updates.check":
		v := true
		if !unset {
			v, err = boolean()
		}
		p.NoUpdates = !v
	case "usage.headerProbe":
		s.Probe = false
		if !unset {
			s.Probe, err = boolean()
		}
	}
	if err != nil {
		return err
	}
	b.auto, b.prefs = s.clamped(), p
	return b.save()
}

// Override puts s in place of the saved auto-switch settings for this
// process only, as cswap auto's flags do; nothing is saved.
func (b *Book) Override(s Auto) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return err
	}
	o := s.clamped()
	b.override = &o
	return nil
}

// ClearOverride ends an Override: the saved settings hold again.
func (b *Book) ClearOverride() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.override = nil
	return nil
}

// LoginTimes is when the added account key's saved login lapses (its
// access token) and when it can no longer be renewed (its refresh token);
// zero where it doesn't say, as for a setup-token.
func (b *Book) LoginTimes(key string) (access, refresh time.Time, err error) {
	saved, err := b.Vault.Get(ParkService, key)
	if err != nil {
		return access, refresh, err
	}
	l, err := parseLogin(saved)
	if err != nil {
		return access, refresh, nil // an API key: no login
	}
	if l.ExpiresAt > 0 {
		access = time.UnixMilli(l.ExpiresAt)
	}
	if l.RefreshTokenExpiresAt > 0 {
		refresh = time.UnixMilli(l.RefreshTokenExpiresAt)
	}
	return access, refresh, nil
}
