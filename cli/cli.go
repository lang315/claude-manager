// Package cli is Claude Manager from the command line, as claude-swap's
// cswap: the same accounts, readings and switching as the app, for
// scripts and cron. `claude-manager <command>` runs one and exits; with no
// command the app opens.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lang315/claude-manager/account"
	"github.com/lang315/claude-manager/store"
)

// Exit codes of auto --once, as cswap's: switched, error, nothing to do,
// blocked (it should move and no account has room).
const (
	ok      = 0
	failed  = 1
	nothing = 2
	blocked = 3
)

// Commands are the commands Run knows; main opens the app for anything
// else.
var Commands = []string{"list", "status", "switch", "auto", "add-token", "remove", "purge", "export", "import", "import-usage", "run", "alias", "map", "unmap", "move", "swap", "unclaimed", "add", "disable", "enable", "threshold", "priority", "menubar", "history", "config", "upgrade", "version", "--version", "help", "-h", "--help"}

// Upgrade installs a newer version of the app, if there is one, and says
// what it did; main sets it.
var Upgrade = func(ctx context.Context) (string, error) {
	return "", errors.New("This build doesn't update itself.")
}

// Version is the app's, for version.
var Version = "dev"

// Run runs the command in args (without the program name) on b, writing
// to out and errs, and returns its exit code.
func Run(ctx context.Context, args []string, b *account.Book, in io.Reader, out, errs io.Writer) int {
	if len(args) == 0 || !slices.Contains(Commands, args[0]) {
		fmt.Fprintln(errs, usage)
		return failed
	}
	c := &cmd{b: b, in: in, out: out, errs: errs}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(errs)
	jsonOut := fs.Bool("json", false, "print JSON")
	dry := fs.Bool("dry-run", false, "auto: say what it would do, switch nothing")
	once := fs.Bool("once", false, "auto: one check")
	email := fs.String("email", "", "add-token: the account's label")
	full := fs.Bool("full", false, "export: a full backup for this Mac")
	hold := fs.Int("hold", -1, "import: don't read the accounts read on the other Mac for this many seconds (0 lifts a hold)")
	yes := fs.Bool("yes", false, "purge: really remove every account")
	noShare := fs.Bool("no-share", false, "run: don't share settings, CLAUDE.md, skills and MCP servers")
	noHistory := fs.Bool("no-share-history", false, "run: don't share chat history")
	unset := fs.Bool("unset", false, "alias: take an account's alias away")
	debug := fs.Bool("debug", false, "print each request made to Anthropic (never a token)")
	purgeID := fs.String("purge", "", "unclaimed: delete the kept login with this id")
	slot := fs.Int("slot", 0, "add: the slot to put the account in")
	aliasFlag := fs.String("alias", "", "add: an alias for the account")
	strategy := fs.String("strategy", "", "switch: rotate, best or next-available; auto: a strategy for this run")
	threshold := fs.Float64("threshold", 0, "auto: the five-hour line for this run")
	weeklyThreshold := fs.Float64("weekly-threshold", 0, "auto: the weekly line for this run")
	model := fs.String("model", "", "auto: model windows for this run")
	interval := fs.Float64("interval", 0, "auto: seconds between checks for this run")
	keys := fs.Bool("include-api-key-accounts", false, "auto: fall back on an API key account")
	tokenStatus := fs.Bool("token-status", false, "list: where each login is and when it lapses")
	_ = fs.Bool("require-session", false, "run: never the account in use (always so here)")
	exportAccount := fs.String("account", "", "export: only this account")
	force := fs.Bool("force", false, "import: replace saved logins even when not older; switch: put the saved login in use even for the account in use")
	svcStatus := fs.Bool("service-status", false, "menubar: does the app start at login, is auto-switch on")
	svcInstall := fs.Bool("install-service", false, "menubar: start the app at login")
	svcUninstall := fs.Bool("uninstall-service", false, "menubar: stop starting the app at login")
	autoOn := fs.Bool("auto-switch", false, "menubar: turn auto-switch on")
	autoOff := fs.Bool("no-auto-switch", false, "menubar: turn auto-switch off")
	shareHistory := fs.Bool("share-history", false, "run: share chat history, whatever run.shareHistory says")
	if err := fs.Parse(reorder(args[1:])); err != nil {
		return failed
	}
	rest := fs.Args()
	if slices.Contains(mutating, args[0]) && c.inSessionShell() && !(args[0] == "auto" && *dry) {
		fmt.Fprintln(errs, "This shell is a terminal opened as an account (its CLAUDE_CONFIG_DIR is a Claude Manager profile): run this from a normal shell.")
		return failed
	}
	if *debug && b.API != nil {
		b.API.Debug = errs
	}
	var err error
	code := ok
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprintln(out, usage)
	case "version", "--version":
		fmt.Fprintln(out, "claude-manager", Version)
	case "alias":
		err = c.alias(rest, *unset)
	case "map":
		err = c.mapDir(rest)
	case "unmap":
		err = c.unmap(one(rest))
	case "config":
		err = c.config(rest)
	case "history":
		var hs []account.Switched
		if hs, err = b.Switches(); err == nil {
			for _, h := range hs {
				how := "by hand"
				if h.Auto {
					how = "auto"
				}
				why := ""
				if h.Auto || h.Why != "by hand" {
					why = ": " + h.Why
				}
				from := h.From
				if from == "" {
					from = "(none)"
				}
				fmt.Fprintf(out, "%s  %s -> %s  %s%s\n", h.At.Local().Format("2006-01-02 15:04"), from, h.To, how, why)
			}
		}
	case "unclaimed":
		err = c.unclaimed(*purgeID)
	case "move", "swap":
		err = c.reslot(args[0], rest)
	case "upgrade":
		var said string
		if said, err = Upgrade(ctx); err == nil {
			fmt.Fprintln(out, said)
		}
	case "list":
		if *tokenStatus {
			var accts []account.Account
			if accts, err = c.read(ctx); len(accts) > 0 {
				err = nil
				c.tokenStatus(accts)
			}
			break
		}
		err = c.list(ctx, *jsonOut)
	case "add":
		err = c.add(ctx, *slot, *aliasFlag, *yes)
	case "disable", "enable", "threshold", "priority":
		err = c.tune(args[0], rest, *unset)
	case "menubar":
		err = c.menubar(*svcStatus, *svcInstall, *svcUninstall, *autoOn, *autoOff)
	case "status":
		err = c.status(ctx, *jsonOut)
	case "switch":
		name := one(rest)
		if name == "" {
			var a account.Account
			if a, err = c.pick(ctx, *strategy); err != nil {
				break
			}
			name = a.Key()
		}
		err = c.switchTo(ctx, name, *jsonOut, *force)
	case "auto":
		if err = c.autoFlags(*threshold, *weeklyThreshold, *model, *strategy, *interval, *keys); err != nil {
			break
		}
		if !*once {
			err = c.autoLoop(ctx, *dry, *jsonOut)
			break
		}
		code, err = c.auto(ctx, *dry, *jsonOut)
	case "add-token":
		err = c.addToken(one(rest), *email, *slot, *yes)
	case "remove":
		err = c.remove(one(rest), *yes)
	case "purge":
		err = c.purge(*yes)
	case "export":
		path := one(rest)
		var keys []string
		if *exportAccount != "" {
			accts, _ := c.b.Blocks()
			var a account.Account
			if a, err = find(accts, *exportAccount); err != nil {
				break
			}
			keys = []string{a.Key()}
		}
		if path != "-" {
			err = c.export(path, *full, keys...)
			break
		}
		var done func()
		if path, done, err = stdioFile(nil); err != nil {
			break
		}
		defer done()
		if _, _, err = b.Export(path, *full, keys...); err == nil {
			var f *os.File
			if f, err = os.Open(path); err == nil {
				_, err = io.Copy(out, f)
				f.Close()
			}
		}
	case "import", "import-usage":
		path := one(rest)
		if path == "-" {
			var done func()
			if path, done, err = stdioFile(in); err != nil {
				break
			}
			defer done()
		}
		err = c.importFile(path, *hold, *force, args[0] == "import-usage")
	case "run":
		history := !*noHistory
		if p, perr := b.Prefs(); perr == nil && p.NoShareHistory && !*shareHistory {
			history = false
		}
		err = c.run(rest, account.Share{Setup: !*noShare, History: history})
	}
	if err != nil {
		if *jsonOut {
			json.NewEncoder(out).Encode(map[string]any{"schemaVersion": 1, "error": map[string]string{"message": err.Error()}})
		} else {
			fmt.Fprintln(errs, err)
		}
		return failed
	}
	return code
}

const usage = `claude-manager <command> [flags]

  list [--json]                    every account and its limits
  status [--json]                  the account in use
  switch <email> [--json]          put Claude Code on an added account
  auto --once [--dry-run] [--json] one auto-switch check (exit 0 switched, 1 error, 2 nothing to do, 3 blocked)
  add-token <token|-> [--email E]  add a setup-token or a Console API key
  remove <email>                   remove an added account
  purge --yes                      remove every added account
  export <file> [--full]           write the added accounts to a file
  import <file> [--hold SECONDS]   add an export's accounts, or take a readings file (cswap list --json)
  run [<email>] [folder] [--no-share] [--no-share-history]
                                   open Claude Code in Terminal as an account; with none
                                   named, the one mapped to the folder
  alias [<email> <name> | <email> --unset]
                                   a short name for an account, or list them
  map [<email> [folder]]           map a folder (and those below) to an account, or list
  unmap [folder]                   take a folder's mapping away
  config [list | get <key> | set <key> <value> | unset <key>]
                                   settings, by claude-swap's names
  upgrade                          install a newer version of the app
  unclaimed [--purge ID]           logins in use kept as nobody's when switched off
  move <from> <to> | swap <a> <b>  renumber accounts (move swaps when <to> is taken)
  add [--slot N] [--alias A]       add the account Claude Code is signed in to
  disable | enable <account>       hold an account out of auto-switch, or put it back
  threshold [<account> <pct> | <account> --unset]   an account's own line, or list them
  priority [<account> <tier> | <account> --unset]   an account's tier, or list them
  switch [--strategy rotate|best|next-available]    with no account: the next one
  auto [--once] [--threshold P] [--weekly-threshold P] [--model M] [--strategy S]
       [--interval S] [--include-api-key-accounts]  without --once: every interval until stopped
  list --token-status              where each login is and when it lapses
  run <account> [folder] -- <claude args>
  export - | import -              to stdout, from stdin
  menubar [--install-service | --uninstall-service | --service-status | --auto-switch | --no-auto-switch]
  tui | watch | menubar [--foreground]   open the app
  history                          the recent switches
  version
  --debug                          with any command: print each request made to Anthropic

With no command the app opens.`

// reorder puts flags before arguments, so "switch a@b --json" parses.
func reorder(args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--": // what follows goes to claude, "--" kept to tell it apart
			rest = append(rest, args[i:]...)
			i = len(args)
		case strings.HasPrefix(a, "-") && a != "-":
			flags = append(flags, a)
			if !strings.Contains(a, "=") && takesValue(a) && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
		default:
			rest = append(rest, a)
		}
	}
	return append(append(flags, "--"), rest...) // its own end of flags, so a -- for claude stays
}

func takesValue(f string) bool {
	return slices.Contains([]string{"email", "hold", "purge", "slot", "alias", "strategy", "threshold", "weekly-threshold", "model", "interval", "account"}, strings.TrimLeft(f, "-"))
}

func one(rest []string) string {
	if len(rest) == 0 {
		return ""
	}
	return rest[0]
}

type cmd struct {
	b         *account.Book
	in        io.Reader
	br        *bufio.Reader
	out, errs io.Writer
}

// find is the account email (or its key) names among accts.
func find(accts []account.Account, name string) (account.Account, error) {
	if name == "" {
		return account.Account{}, errors.New("name an account by its email")
	}
	var hits []account.Account
	for _, a := range accts {
		if a.Email == name || a.Key() == name || a.Alias != "" && strings.EqualFold(a.Alias, name) || a.Slot != 0 && strconv.Itoa(a.Slot) == name {
			hits = append(hits, a)
		}
	}
	switch len(hits) {
	case 0:
		return account.Account{}, fmt.Errorf("no account %s", name)
	case 1:
		return hits[0], nil
	}
	return account.Account{}, &ambiguous{hits}
}

// window is a usage window in cswap's list --json.
type window struct {
	Pct      float64  `json:"pct"`
	ResetsAt string   `json:"resetsAt,omitempty"`
	Name     string   `json:"name,omitempty"`
	Used     *float64 `json:"used,omitempty"`
	Limit    *float64 `json:"limit,omitempty"`
	Currency string   `json:"currency,omitempty"`
	// Pace on weekly windows, as cswap's: expectedPct and aheadOfPace once
	// a day into the week; the projections, for --json only.
	Expected  *float64 `json:"expectedPct,omitempty"`
	Ahead     *bool    `json:"aheadOfPace,omitempty"`
	Exhausted string   `json:"projectedExhaustionAt,omitempty"`
	Lasts     *bool    `json:"willLastToReset,omitempty"`
}

type usageJSON struct {
	FiveHour *window  `json:"fiveHour,omitempty"`
	SevenDay *window  `json:"sevenDay,omitempty"`
	Scoped   []window `json:"scoped,omitempty"`
	Spend    *window  `json:"spend,omitempty"`
}

// row is an account as cswap's list --json gives it, so cswap
// import-usage, and this one, take it.
type row struct {
	Number      int        `json:"number"`
	Email       string     `json:"email"`
	OrgUUID     string     `json:"organizationUuid"`
	Key         string     `json:"key"`
	Active      bool       `json:"active"`
	Added       bool       `json:"added"`
	Status      string     `json:"usageStatus"`
	FetchedAt   string     `json:"usageFetchedAt,omitempty"`
	Age         *float64   `json:"usageAgeSeconds,omitempty"`
	Usage       *usageJSON `json:"usage"`
	Error       string     `json:"usageError,omitempty"`
	RetryAt     string     `json:"usageRetryAt,omitempty"`
	LastGood    *usageJSON `json:"lastGoodUsage,omitempty"`
	LastGoodAt  string     `json:"lastGoodFetchedAt,omitempty"`
	LastGoodAge *float64   `json:"lastGoodAgeSeconds,omitempty"`
	Disabled    bool       `json:"disabled,omitempty"`
	Threshold   float64    `json:"threshold,omitempty"`
	LoginExpiry string     `json:"loginExpiresAt,omitempty"`
	Plan        *planJSON  `json:"plan,omitempty"`
}

// planJSON is cswap's plan object.
type planJSON struct {
	Label     string `json:"label"`
	Tier      string `json:"tier"`
	OrgType   string `json:"organizationType,omitempty"`
	Status    string `json:"subscriptionStatus,omitempty"`
	Name      string `json:"planDisplayName,omitempty"`
	Expired   bool   `json:"expired"`
	FetchedAt string `json:"fetchedAt"`
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func toRow(a account.Account, now time.Time) row {
	r := row{Number: a.Slot, Email: a.Email, OrgUUID: a.OrgUUID, Key: a.Key(), Active: a.InUse, Added: a.Added,
		Disabled: a.AutoOff, Threshold: a.Line, LoginExpiry: stamp(a.GoodUntil), Status: "unavailable", Error: a.Err}
	if a.PlanTier != "" {
		r.Plan = &planJSON{Label: account.PlanLabel(a), Tier: a.PlanTier, OrgType: a.PlanOrg, Status: a.PlanStatus,
			Name: a.PlanName, Expired: account.PlanLapsed(a), FetchedAt: stamp(a.PlanAt)}
	}
	age := now.Sub(a.ReadAt).Seconds()
	if !a.Usable(now) {
		// Display-grade, never decision-grade: cswap's lastGood fields.
		if len(a.Limits) > 0 && !a.ReadAt.IsZero() {
			r.LastGood, r.LastGoodAt, r.LastGoodAge = usageOf(a), stamp(a.ReadAt), &age
		}
		if !a.RetryAt().IsZero() {
			r.RetryAt = stamp(a.RetryAt())
		}
		return r
	}
	r.Status, r.Usage, r.Age, r.FetchedAt, r.Error = "ok", usageOf(a), &age, stamp(a.ReadAt), ""
	return r
}

func usageOf(a account.Account) *usageJSON {
	u := &usageJSON{}
	for _, l := range a.Limits {
		w := &window{Pct: l.Percent, ResetsAt: stamp(l.ResetsAt)}
		if e, ahead, ok := l.Pace(a.ReadAt); ok {
			w.Expected, w.Ahead = &e, &ahead
			if at, lasts, ok := l.Projection(a.ReadAt); ok {
				if !at.IsZero() {
					w.Exhausted = stamp(at)
				}
				w.Lasts = &lasts
			}
		}
		switch l.Kind {
		case "session", "five_hour":
			u.FiveHour = w
		case "weekly_all", "seven_day":
			u.SevenDay = w
		case "weekly_scoped":
			w.Name = l.Scope
			u.Scoped = append(u.Scoped, *w)
		case "extra_usage":
			used, limit := l.Used, l.Cap
			w.Used, w.Limit, w.Currency = &used, &limit, l.Currency
			u.Spend = w
		}
	}
	return u
}

// read refreshes the readings and lists the accounts by slot, as cswap's
// list; the account in use when not added comes first, numberless.
func (c *cmd) read(ctx context.Context) ([]account.Account, error) {
	accts, err := c.b.Refresh(ctx, false)
	slices.SortStableFunc(accts, func(x, y account.Account) int { return x.Slot - y.Slot })
	return accts, err
}

func (c *cmd) list(ctx context.Context, asJSON bool) error {
	accts, err := c.read(ctx)
	if err != nil && len(accts) == 0 {
		return err
	}
	if n := account.StoreNote(os.Getenv); n != "" {
		fmt.Fprintln(c.errs, n)
	}
	now := c.b.Now()
	if asJSON {
		doc := map[string]any{"schemaVersion": 1, "activeAccountNumber": nil}
		var rows []row
		for _, a := range accts {
			if a.Email == "" {
				continue // a login nobody could be named for: an importer refuses a row without an email
			}
			r := toRow(a, now)
			if r.Active && r.Number > 0 {
				doc["activeAccountNumber"] = r.Number
			}
			rows = append(rows, r)
		}
		doc["accounts"] = rows
		return json.NewEncoder(c.out).Encode(doc)
	}
	w := bufio.NewWriter(c.out)
	defer w.Flush()
	if !slices.ContainsFunc(accts, func(a account.Account) bool { return a.Added }) {
		fmt.Fprintln(w, "No accounts are added yet: claude-manager add adds the one Claude Code uses; Add Account in the app signs in to another.")
	}
	for _, a := range accts {
		mark := " "
		if a.InUse {
			mark = "*"
		}
		plan := account.PlanLabel(a)
		if account.PlanLapsed(a) {
			plan = "Free (lapsed)"
		}
		if plan != "" {
			plan = " · " + plan
		}
		alias := ""
		if a.Alias != "" {
			alias = " (" + a.Alias + ")"
		}
		fmt.Fprintf(w, "%s %2s %s%s [%s]%s\n", mark, slotOf(a), a.Email, alias, tag(a), plan)
		for _, l := range a.Limits {
			fmt.Fprintf(w, "    %-22s %5.0f%%\n", l.Label(), l.Percent)
		}
		if a.Err != "" {
			fmt.Fprintf(w, "    %s\n", a.Err)
		}
	}
	for _, d := range c.b.Duplicates() {
		fmt.Fprintf(w, "Warning: %s and %s hold the same login: one renewing it spends the other's.\n", d[0], d[1])
	}
	if s, err := c.b.AutoSettings(); err == nil {
		if u := s.Unseen(accts); len(u) > 0 {
			fmt.Fprintf(c.errs, "autoswitch.model: %s matches no account's windows: only the 5h and 7d limits count for it (typo?).\n", strings.Join(u, ", "))
		}
	}
	c.instances(w)
	return nil
}

func (c *cmd) instances(w io.Writer) {
	if Instances == nil {
		return
	}
	if ins := Instances(); len(ins) > 0 {
		fmt.Fprintln(w, "Running instances:")
		for _, in := range ins {
			where := in.Folder
			if in.Profile != "" {
				where += " (a terminal's profile)"
			}
			fmt.Fprintf(w, "  %-8s %s\n", in.Kind, where)
		}
	}
}

func (c *cmd) status(ctx context.Context, asJSON bool) error {
	accts, err := c.read(ctx)
	if err != nil && len(accts) == 0 {
		return err
	}
	i := slices.IndexFunc(accts, func(a account.Account) bool { return a.InUse })
	if asJSON {
		doc := map[string]any{"schemaVersion": 1, "active": nil}
		if i >= 0 {
			doc["active"] = toRow(accts[i], c.b.Now())
		}
		return json.NewEncoder(c.out).Encode(doc)
	}
	if i < 0 {
		fmt.Fprintln(c.out, "Not signed in to Claude Code.")
	} else {
		fmt.Fprintln(c.out, accts[i].Email)
	}
	c.instances(c.out)
	return nil
}

// Instances lists the running Claude Codes for status; main sets it.
var Instances func() []store.Instance

func (c *cmd) inUse() string {
	accts, _ := c.b.Blocks()
	if i := slices.IndexFunc(accts, func(a account.Account) bool { return a.InUse }); i >= 0 {
		return accts[i].Email
	}
	return ""
}

func (c *cmd) switchTo(ctx context.Context, name string, asJSON, force bool) error {
	accts, _ := c.b.Blocks()
	a, err := c.pickOne(accts, name, "switch to")
	if err != nil {
		return err
	}
	from := c.inUse()
	if a.InUse && !force && !asJSON {
		fmt.Fprintf(c.out, "Already on %s. switch --force puts its saved login in use.\n", a.Email)
		return nil
	}
	kept, _ := c.b.UnclaimedLogins()
	sw := c.b.Switch
	if force {
		sw = c.b.SwitchForce
	}
	if err := sw(ctx, a.Key()); err != nil {
		return err
	}
	if asJSON {
		return json.NewEncoder(c.out).Encode(map[string]any{"schemaVersion": 1, "switched": from != a.Email, "from": from, "to": a.Email, "reason": "asked"})
	}
	if now, _ := c.b.UnclaimedLogins(); len(now) > len(kept) {
		fmt.Fprintln(c.errs, "Claude Code's sign-in named no added account; it was kept as unclaimed (claude-manager unclaimed).")
	}
	fmt.Fprintf(c.out, "Switched to %s.\n%s\n", a.Email, restartHint)
	return nil
}

func (c *cmd) auto(ctx context.Context, dry, asJSON bool) (int, error) {
	if _, err := c.read(ctx); err != nil {
		return failed, err
	}
	from := c.inUse()
	d, err := c.b.Once(ctx, dry)
	if err != nil {
		return failed, err
	}
	if s, serr := c.b.AutoInEffect(); serr == nil { // --model's names too
		accts, _ := c.b.Blocks()
		if u := s.Unseen(accts); len(u) > 0 {
			fmt.Fprintf(c.errs, "autoswitch.model: %s matches no account's windows (typo?).\n", strings.Join(u, ", "))
		}
	}
	code := nothing
	switch {
	case d.To != "" && !dry:
		code = ok
	case d.Blocked:
		code = blocked
	}
	if asJSON {
		event := map[string]any{"schemaVersion": 1, "ts": c.b.Now().UTC().Format(time.RFC3339), "event": "no-switch", "reason": d.Why}
		if d.To != "" {
			event["event"], event["from"], event["to"], event["dryRun"] = "switch", from, d.Email, dry
		}
		return code, json.NewEncoder(c.out).Encode(event)
	}
	switch {
	case d.To != "" && dry:
		fmt.Fprintf(c.out, "Would switch to %s: %s.\n", d.Email, d.Why)
	case d.To != "":
		fmt.Fprintf(c.out, "Switched to %s: %s.\n", d.Email, d.Why)
	default:
		fmt.Fprintln(c.out, d.Why)
	}
	return code, nil
}

func (c *cmd) addToken(token, email string, slot int, yes bool) error {
	if token == "-" || token == "" {
		line, err := c.reader().ReadString('\n')
		if err != nil && line == "" {
			return errors.New("give a token, or - and the token on stdin")
		}
		token = line
	}
	token = strings.TrimSpace(token)
	var a account.Account
	var err error
	if account.IsAPIKey(token) {
		a, err = c.b.AddAPIKey(token, email)
	} else {
		a, err = c.b.AddToken(token, email)
	}
	if err != nil {
		return err
	}
	if slot > 0 {
		accts, _ := c.b.Blocks()
		i := slices.IndexFunc(accts, func(x account.Account) bool { return x.Key() == a.Key() })
		if j := slices.IndexFunc(accts, func(x account.Account) bool { return x.Slot == slot && x.Key() != a.Key() }); j >= 0 && !yes &&
			!c.ask(fmt.Sprintf("Slot %d holds %s: move it to slot %d?", slot, accts[j].Email, accts[i].Slot), false) {
			fmt.Fprintf(c.out, "Added %s in slot %d.\n", a.Email, accts[i].Slot)
			return nil
		}
		if err := c.b.Move(accts[i].Slot, slot); err != nil {
			return err
		}
	}
	fmt.Fprintf(c.out, "Added %s.\n", a.Email)
	return nil
}

func (c *cmd) remove(name string, yes bool) error {
	accts, _ := c.b.Blocks()
	a, err := c.pickOne(slices.DeleteFunc(accts, func(a account.Account) bool { return !a.Added }), name, "remove")
	if err != nil {
		return err
	}
	if a.InUse {
		fmt.Fprintf(c.errs, "%s is the account Claude Code uses: in its default folder the app adds it again at the next refresh.\n", a.Email)
	}
	if !yes && !c.ask(fmt.Sprintf("Remove %s for good? Its saved login leaves the keychain (it stays valid at Anthropic until it lapses).", a.Email), false) {
		return errors.New("nothing removed")
	}
	if err := c.b.Remove(a.Key()); err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Removed %s.\n", a.Email)
	return nil
}

func (c *cmd) purge(yes bool) error {
	if !yes {
		return errors.New("purge removes every added account and its saved login from the keychain: give --yes")
	}
	accts, err := c.b.Blocks()
	if err != nil {
		return err
	}
	var errs []error
	n := 0
	for _, a := range accts {
		if !a.Added {
			continue
		}
		if err := c.b.Remove(a.Key()); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", a.Email, err))
			continue
		}
		n++
	}
	fmt.Fprintf(c.out, "Removed %d accounts.\n", n)
	return errors.Join(errs...)
}

func (c *cmd) export(path string, full bool, keys ...string) error {
	if path == "" {
		return errors.New("name the file to export to")
	}
	n, left, err := c.b.Export(path, full, keys...)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Exported %d accounts to %s", n, path)
	if left > 0 {
		fmt.Fprintf(c.out, " (left out %d)", left)
	}
	fmt.Fprintln(c.out, ". It holds sign-ins, unencrypted: keep it private and delete it after import.")
	return nil
}

func (c *cmd) importFile(path string, hold int, force, usageOnly bool) error {
	if path == "" {
		return errors.New("name the file to import")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if usageOnly || account.IsReadings(data) { // ImportUsage refuses what isn't readings
		got, err := c.b.ImportUsage(path, time.Duration(hold)*time.Second)
		if err != nil {
			return err
		}
		fmt.Fprintf(c.out, "Readings: %d taken, %d kept, %d skipped.\n", got.Adopted, got.Kept, got.Skipped)
		return nil
	}
	got, err := c.b.ImportWith(path, force)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Added %d, updated %d, left %d.\n", got.Added, got.Updated, got.Skipped)
	return nil
}

func (c *cmd) run(rest []string, share account.Share) error {
	var forward []string
	if i := slices.Index(rest, "--"); i >= 0 {
		rest, forward = rest[:i], rest[i+1:]
	}
	accts, _ := c.b.Blocks()
	name, folder := one(rest), "."
	if len(rest) > 1 {
		folder = rest[1]
	} else if _, ferr := find(accts, name); ferr != nil && isDir(name) {
		name, folder = "", name // run <folder>: its mapped account; an account's name wins
	}
	folder, err := absPath(folder)
	if err != nil {
		return err
	}
	if name == "" {
		key, _, err := c.b.Mapped(folder)
		if err != nil {
			return err
		}
		if key == "" {
			return fmt.Errorf("No account is mapped to %s: run claude there, or name an account.", folder)
		}
		name = key
	}
	a, err := find(accts, name)
	if err != nil {
		return err
	}
	if err := c.b.Run(a.Key(), folder, share, forward...); err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Opened Claude Code in Terminal as %s.\n", a.Email)
	return nil
}

func absPath(p string) (string, error) { return filepath.Abs(p) }

func isDir(p string) bool {
	if p == "" {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func (c *cmd) alias(rest []string, unset bool) error {
	accts, _ := c.b.Blocks()
	if len(rest) == 0 {
		for _, a := range accts {
			if a.Alias != "" {
				fmt.Fprintf(c.out, "%s\t%s\n", a.Alias, a.Email)
			}
		}
		return nil
	}
	a, err := find(accts, rest[0])
	if err != nil {
		return err
	}
	name := one(rest[1:])
	switch {
	case unset:
		name = ""
	case name == "":
		return errors.New("give the alias, or --unset")
	}
	if err := c.b.SetAlias(a.Key(), name); err != nil {
		return err
	}
	if name == "" {
		fmt.Fprintf(c.out, "%s has no alias.\n", a.Email)
	} else {
		fmt.Fprintf(c.out, "%s is %s.\n", strings.ToLower(strings.TrimSpace(name)), a.Email)
	}
	return nil
}

func (c *cmd) mapDir(rest []string) error {
	accts, _ := c.b.Blocks()
	if len(rest) == 0 {
		m, err := c.b.Maps()
		if err != nil {
			return err
		}
		dirs := slices.Sorted(maps.Keys(m))
		for _, d := range dirs {
			email := m[d]
			if i := slices.IndexFunc(accts, func(a account.Account) bool { return a.Key() == m[d] }); i >= 0 {
				email = accts[i].Email
			}
			fmt.Fprintf(c.out, "%s\t%s\n", d, email)
		}
		return nil
	}
	a, err := find(accts, rest[0])
	if err != nil {
		return err
	}
	dir := "."
	if len(rest) > 1 {
		dir = rest[1]
	}
	if dir, err = absPath(dir); err != nil {
		return err
	}
	if err := c.b.Map(a.Key(), dir); err != nil {
		return err
	}
	fmt.Fprintf(c.out, "%s now opens as %s.\n", dir, a.Email)
	return nil
}

func (c *cmd) unmap(dir string) error {
	if dir == "" {
		dir = "."
	}
	dir, err := absPath(dir)
	if err != nil {
		return err
	}
	ok, err := c.b.Unmap(dir)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s is not mapped.", dir)
	}
	fmt.Fprintf(c.out, "Unmapped %s.\n", dir)
	return nil
}

func (c *cmd) config(rest []string) error {
	verb := one(rest)
	if verb == "" {
		verb = "list"
	}
	all, err := c.b.Settings()
	if err != nil {
		return err
	}
	get := func(key string) (account.Setting, error) {
		for _, s := range all {
			if s.Key == key {
				return s, nil
			}
		}
		return account.Setting{}, fmt.Errorf("No setting %s: claude-manager config lists them.", key)
	}
	switch verb {
	case "list":
		for _, s := range all {
			v := s.Value
			if v == s.Default {
				v += " (default)"
			}
			fmt.Fprintf(c.out, "%-34s %-24s %s\n", s.Key, v, s.Range)
		}
	case "get":
		s, err := get(one(rest[1:]))
		if err != nil {
			return err
		}
		fmt.Fprintln(c.out, s.Value)
	case "set":
		if len(rest) < 3 {
			return errors.New("config set <key> <value>")
		}
		if err := c.b.Set(rest[1], rest[2]); err != nil {
			return err
		}
	case "unset":
		if len(rest) < 2 {
			return errors.New("config unset <key>")
		}
		return c.b.Set(rest[1], "")
	default:
		return errors.New("config list, get, set or unset")
	}
	return nil
}

func slotOf(a account.Account) string {
	if a.Slot == 0 {
		return "-"
	}
	return strconv.Itoa(a.Slot)
}

func (c *cmd) reslot(verb string, rest []string) error {
	if len(rest) != 2 {
		return fmt.Errorf("%s takes two slot numbers", verb)
	}
	x, err1 := strconv.Atoi(rest[0])
	y, err2 := strconv.Atoi(rest[1])
	if err1 != nil || err2 != nil {
		return fmt.Errorf("%s takes two slot numbers", verb)
	}
	if verb == "move" {
		err := c.b.Move(x, y)
		if err == nil {
			fmt.Fprintf(c.out, "Moved slot %d to %d.\n", x, y)
		}
		return err
	}
	err := c.b.Swap(x, y)
	if err == nil {
		fmt.Fprintf(c.out, "Swapped slots %d and %d.\n", x, y)
	}
	return err
}

func (c *cmd) unclaimed(purge string) error {
	if purge != "" {
		if err := c.b.PurgeUnclaimed(purge); err != nil {
			return err
		}
		fmt.Fprintf(c.out, "Deleted %s. Sign in to its account with /login and Add Account to have it back.\n", purge)
		return nil
	}
	kept, err := c.b.UnclaimedLogins()
	if err != nil {
		return err
	}
	if len(kept) == 0 {
		fmt.Fprintln(c.out, "No unclaimed logins.")
	}
	for _, u := range kept {
		fmt.Fprintf(c.out, "%s  %s  %s\n", u.ID, u.At.Local().Format("2006-01-02 15:04"), u.Why)
	}
	return nil
}
