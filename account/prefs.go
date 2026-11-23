package account

import (
	"cmp"
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
	TrayTitle      string `json:"trayTitle,omitempty"`      // the menu bar title beside its icon: see TrayMode (claude-swap's title percentage)
	TrayEvery      int    `json:"trayEvery,omitempty"`      // seconds between the menu bar's refreshes; 0 is 300
	NoTrayName     bool   `json:"noTrayName,omitempty"`     // leave the account name out of the menu bar title (claude-swap's show_account_name, on by default)
	TrayScoped     bool   `json:"trayScoped,omitempty"`     // add each model's weekly limit to the menu bar title (claude-swap's title_scoped)
	// Stored names the settings explicitly set, whatever their value: a
	// setting is set when its key is in cswap's settings file. nil is a file
	// from before the record: set then means value != default.
	Stored []string `json:"stored,omitempty"`
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
	p.Stored = storedAfter(b.auto, b.prefs, b.auto, p)
	b.prefs = p
	return b.save()
}

// TrayMode is the menu bar title mode: "compact" (the fuller window's
// share), "both", "5h", "7d" or "off". Unset is compact; but "" with
// menubar.title set explicitly is from before compact, when "" stored both.
func (p Prefs) TrayMode() string {
	if p.TrayTitle == "" && slices.Contains(p.Stored, "menubar.title") {
		return "both"
	}
	return cmp.Or(p.TrayTitle, "compact")
}

// storedAfter is the record of explicitly set keys after settings change from
// (os, op) to (ns, np): every key whose value changed joins it. A file from
// before the record starts from its non-default keys.
func storedAfter(os Auto, op Prefs, ns Auto, np Prefs) []string {
	stored := slices.Clone(op.Stored)
	if op.Stored == nil {
		for _, k := range settingKeys {
			if st := settingOf(k, os, op); st.Value != st.Default {
				stored = append(stored, k)
			}
		}
	}
	for _, k := range settingKeys {
		if settingOf(k, os, op).Value != settingOf(k, ns, np).Value && !slices.Contains(stored, k) {
			stored = append(stored, k)
		}
	}
	return stored
}

// Setting is one setting by claude-swap's name, for `claude-manager config`.
type Setting struct {
	Key, Value, Default, Range string
	IsSet                      bool // cswap: the key is in the settings file, even at its default
}

// settingKeys are the settings `config` knows, in claude-swap's names.
var settingKeys = []string{
	"autoswitch.threshold", "autoswitch.weeklyThreshold", "autoswitch.intervalSeconds", "autoswitch.cooldownSeconds",
	"autoswitch.hysteresisPct", "autoswitch.strategy", "autoswitch.includeApiKeyAccounts", "autoswitch.unhealthyTicks",
	"autoswitch.model", "autoswitch.enabled", "ui.theme", "run.shareHistory", "updates.check", "usage.headerProbe",
	"menubar.title", "menubar.refreshSeconds", "menubar.showAccountName", "menubar.titleScoped",
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
		st := settingOf(k, b.auto, b.prefs)
		st.IsSet = st.Value != st.Default
		if b.prefs.Stored != nil {
			st.IsSet = slices.Contains(b.prefs.Stored, k)
		}
		out = append(out, st)
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
		return Setting{key, f(s.Line), f(d.Line), "50–99.9", false}
	case "autoswitch.weeklyThreshold":
		return Setting{key, weekly(s), "", "50–99.9, or unset for the threshold", false}
	case "autoswitch.intervalSeconds":
		return Setting{key, f(s.Every().Seconds()), "60", "15–3600", false}
	case "autoswitch.cooldownSeconds":
		return Setting{key, strconv.Itoa(s.Cooldown), strconv.Itoa(d.Cooldown), "0–86400", false}
	case "autoswitch.hysteresisPct":
		return Setting{key, f(s.Margin), f(d.Margin), "0–50", false}
	case "autoswitch.strategy":
		return Setting{key, s.Strategy, d.Strategy, strings.Join([]string{Best, WeeklyFirst, ConsumeFirst, ByPriority}, ", "), false}
	case "autoswitch.includeApiKeyAccounts":
		return Setting{key, bo(s.Keys), "false", "true, false", false}
	case "autoswitch.unhealthyTicks":
		return Setting{key, strconv.Itoa(ticks(s)), "3", "1–100", false}
	case "autoswitch.model":
		return Setting{key, s.Model, "", "model names, comma separated, or all", false}
	case "autoswitch.enabled":
		return Setting{key, bo(s.On), "false", "true, false", false}
	case "ui.theme":
		return Setting{key, theme(p), theme(dp), "auto, light, dark", false}
	case "run.shareHistory":
		return Setting{key, bo(!p.NoShareHistory), "true", "true, false", false}
	case "updates.check":
		return Setting{key, bo(!p.NoUpdates), "true", "true, false", false}
	case "menubar.title":
		return Setting{key, p.TrayMode(), "compact", "compact, both, 5h, 7d, off", false}
	case "menubar.refreshSeconds":
		return Setting{key, fmt.Sprint(int(p.TrayInterval().Seconds())), "300", "30, 60, 300", false}
	case "menubar.showAccountName":
		return Setting{key, bo(!p.NoTrayName), "true", "true, false", false}
	case "menubar.titleScoped":
		return Setting{key, bo(p.TrayScoped), "false", "true, false", false}
	case "usage.headerProbe":
		return Setting{key, bo(s.Probe), "false", "true, false", false}
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
		if !unset {
			if !slices.Contains([]string{"compact", "both", "5h", "7d", "off"}, value) {
				err = fmt.Errorf("%s is compact, both, 5h, 7d or off.", key)
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
	case "menubar.showAccountName":
		v := true
		if !unset {
			v, err = boolean()
		}
		p.NoTrayName = !v
	case "menubar.titleScoped":
		p.TrayScoped = false
		if !unset {
			p.TrayScoped, err = boolean()
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
	p.Stored = storedAfter(b.auto, b.prefs, s.clamped(), p)
	if unset { // cswap's unset removes the key; set stores it even at the default
		p.Stored = slices.DeleteFunc(p.Stored, func(k string) bool { return k == key })
	} else if !slices.Contains(p.Stored, key) {
		p.Stored = append(p.Stored, key)
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
