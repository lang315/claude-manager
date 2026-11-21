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
	"strings"
	"time"

	"github.com/lang315/claude-manager/account"
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
var Commands = []string{"list", "status", "switch", "auto", "add-token", "remove", "purge", "export", "import", "import-usage", "run", "alias", "map", "unmap", "config", "upgrade", "version", "--version", "help"}

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
	shareHistory := fs.Bool("share-history", false, "run: share chat history, whatever run.shareHistory says")
	if err := fs.Parse(reorder(args[1:])); err != nil {
		return failed
	}
	rest := fs.Args()
	if *debug && b.API != nil {
		b.API.Debug = errs
	}
	var err error
	code := ok
	switch args[0] {
	case "help":
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
	case "upgrade":
		var said string
		if said, err = Upgrade(ctx); err == nil {
			fmt.Fprintln(out, said)
		}
	case "list":
		err = c.list(ctx, *jsonOut)
	case "status":
		err = c.status(ctx, *jsonOut)
	case "switch":
		err = c.switchTo(ctx, one(rest), *jsonOut)
	case "auto":
		if !*once {
			err = errors.New("auto runs in the app; from the command line give --once")
			break
		}
		code, err = c.auto(ctx, *dry, *jsonOut)
	case "add-token":
		err = c.addToken(one(rest), *email)
	case "remove":
		err = c.remove(one(rest))
	case "purge":
		err = c.purge(*yes)
	case "export":
		err = c.export(one(rest), *full)
	case "import", "import-usage":
		err = c.importFile(one(rest), *hold)
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
  version
  --debug                          with any command: print each request made to Anthropic

With no command the app opens.`

// reorder puts flags before arguments, so "switch a@b --json" parses.
func reorder(args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			rest = append(rest, args[i+1:]...)
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
	return append(flags, rest...)
}

func takesValue(f string) bool {
	f = strings.TrimLeft(f, "-")
	return f == "email" || f == "hold"
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
	out, errs io.Writer
}

// find is the account email (or its key) names among accts.
func find(accts []account.Account, name string) (account.Account, error) {
	if name == "" {
		return account.Account{}, errors.New("name an account by its email")
	}
	var hits []account.Account
	for _, a := range accts {
		if a.Email == name || a.Key() == name || a.Alias != "" && strings.EqualFold(a.Alias, name) {
			hits = append(hits, a)
		}
	}
	switch len(hits) {
	case 0:
		return account.Account{}, fmt.Errorf("no account %s", name)
	case 1:
		return hits[0], nil
	}
	var keys []string
	for _, a := range hits {
		keys = append(keys, a.Key())
	}
	return account.Account{}, fmt.Errorf("%s names %d accounts; name one by its key: %s", name, len(hits), strings.Join(keys, ", "))
}

// window is a usage window in cswap's list --json.
type window struct {
	Pct      float64  `json:"pct"`
	ResetsAt string   `json:"resetsAt,omitempty"`
	Name     string   `json:"name,omitempty"`
	Used     *float64 `json:"used,omitempty"`
	Limit    *float64 `json:"limit,omitempty"`
	Currency string   `json:"currency,omitempty"`
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
	Disabled    bool       `json:"disabled,omitempty"`
	Threshold   float64    `json:"threshold,omitempty"`
	LoginExpiry string     `json:"loginExpiresAt,omitempty"`
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func toRow(n int, a account.Account, now time.Time) row {
	r := row{Number: n, Email: a.Email, OrgUUID: a.OrgUUID, Key: a.Key(), Active: a.InUse, Added: a.Added,
		Disabled: a.AutoOff, Threshold: a.Line, LoginExpiry: stamp(a.GoodUntil), Status: "unavailable", Error: a.Err}
	if !a.Usable(now) {
		return r
	}
	u := &usageJSON{}
	for _, l := range a.Limits {
		w := &window{Pct: l.Percent, ResetsAt: stamp(l.ResetsAt)}
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
	age := now.Sub(a.ReadAt).Seconds()
	r.Status, r.Usage, r.Age, r.FetchedAt, r.Error = "ok", u, &age, stamp(a.ReadAt), ""
	return r
}

func (c *cmd) read(ctx context.Context) ([]account.Account, error) {
	return c.b.Refresh(ctx, false)
}

func (c *cmd) list(ctx context.Context, asJSON bool) error {
	accts, err := c.read(ctx)
	if err != nil && len(accts) == 0 {
		return err
	}
	now := c.b.Now()
	if asJSON {
		doc := map[string]any{"schemaVersion": 1, "activeAccountNumber": nil}
		var rows []row
		for i, a := range accts {
			r := toRow(i+1, a, now)
			if r.Active {
				doc["activeAccountNumber"] = r.Number
			}
			rows = append(rows, r)
		}
		doc["accounts"] = rows
		return json.NewEncoder(c.out).Encode(doc)
	}
	w := bufio.NewWriter(c.out)
	defer w.Flush()
	for _, a := range accts {
		mark := " "
		if a.InUse {
			mark = "*"
		}
		fmt.Fprintf(w, "%s %s\n", mark, a.Email)
		for _, l := range a.Limits {
			fmt.Fprintf(w, "    %-22s %5.0f%%\n", l.Label(), l.Percent)
		}
		if a.Err != "" {
			fmt.Fprintf(w, "    %s\n", a.Err)
		}
	}
	return nil
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
			doc["active"] = toRow(i+1, accts[i], c.b.Now())
		}
		return json.NewEncoder(c.out).Encode(doc)
	}
	if i < 0 {
		fmt.Fprintln(c.out, "Not signed in to Claude Code.")
		return nil
	}
	fmt.Fprintln(c.out, accts[i].Email)
	return nil
}

func (c *cmd) inUse() string {
	accts, _ := c.b.Blocks()
	if i := slices.IndexFunc(accts, func(a account.Account) bool { return a.InUse }); i >= 0 {
		return accts[i].Email
	}
	return ""
}

func (c *cmd) switchTo(ctx context.Context, name string, asJSON bool) error {
	accts, _ := c.b.Blocks()
	a, err := find(accts, name)
	if err != nil {
		return err
	}
	from := c.inUse()
	if err := c.b.Switch(ctx, a.Key()); err != nil {
		return err
	}
	if asJSON {
		return json.NewEncoder(c.out).Encode(map[string]any{"schemaVersion": 1, "switched": from != a.Email, "from": from, "to": a.Email, "reason": "asked"})
	}
	fmt.Fprintf(c.out, "Switched to %s.\n", a.Email)
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

func (c *cmd) addToken(token, email string) error {
	if token == "-" || token == "" {
		line, err := bufio.NewReader(c.in).ReadString('\n')
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
	fmt.Fprintf(c.out, "Added %s.\n", a.Email)
	return nil
}

func (c *cmd) remove(name string) error {
	accts, _ := c.b.Blocks()
	a, err := find(slices.DeleteFunc(accts, func(a account.Account) bool { return !a.Added }), name)
	if err != nil {
		return err
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

func (c *cmd) export(path string, full bool) error {
	if path == "" {
		return errors.New("name the file to export to")
	}
	n, left, err := c.b.Export(path, full)
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

func (c *cmd) importFile(path string, hold int) error {
	if path == "" {
		return errors.New("name the file to import")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if account.IsReadings(data) {
		got, err := c.b.ImportUsage(path, time.Duration(hold)*time.Second)
		if err != nil {
			return err
		}
		fmt.Fprintf(c.out, "Readings: %d taken, %d kept, %d skipped.\n", got.Adopted, got.Kept, got.Skipped)
		return nil
	}
	got, err := c.b.Import(path)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Added %d, updated %d, left %d.\n", got.Added, got.Updated, got.Skipped)
	return nil
}

func (c *cmd) run(rest []string, share account.Share) error {
	accts, _ := c.b.Blocks()
	name, folder := one(rest), "."
	if len(rest) > 1 {
		folder = rest[1]
	} else if isDir(name) {
		name, folder = "", name // run <folder>: its mapped account
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
	if err := c.b.Run(a.Key(), folder, share); err != nil {
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
