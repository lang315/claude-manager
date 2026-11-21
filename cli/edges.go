package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lang315/claude-manager/account"
)

// claude-swap's prompts and notices on the command line, from
// switcher.py: confirmations, the choice between accounts one email
// names, and the words after a switch.

// ask asks question on errs and reads the answer from in: def is the
// answer to an empty line; no input reads as no.
func (c *cmd) ask(question string, def bool) bool {
	if c.in == nil {
		return false
	}
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	fmt.Fprintf(c.errs, "%s %s ", question, hint)
	line, err := c.reader().ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "":
		return def
	case "y", "yes":
		return true
	}
	return false
}

func (c *cmd) reader() *bufio.Reader {
	if c.br == nil {
		c.br = bufio.NewReader(c.in)
	}
	return c.br
}

// ambiguous is find's error when an email names several accounts.
type ambiguous struct{ hits []account.Account }

func (e *ambiguous) Error() string {
	var keys []string
	for _, a := range e.hits {
		keys = append(keys, a.Key())
	}
	return fmt.Sprintf("%s names %d accounts; name one by its number or key: %s", e.hits[0].Email, len(e.hits), strings.Join(keys, ", "))
}

// pickOne is find, asking which when the name is ambiguous, as
// claude-swap's "Enter account number".
func (c *cmd) pickOne(accts []account.Account, name, verb string) (account.Account, error) {
	a, err := find(accts, name)
	var amb *ambiguous
	if !errors.As(err, &amb) || c.in == nil {
		return a, err
	}
	fmt.Fprintf(c.errs, "%s is several accounts:\n", name)
	for i, h := range amb.hits {
		org := h.OrgName
		if org == "" {
			org = h.OrgUUID
		}
		fmt.Fprintf(c.errs, "  %d. %s [%s]\n", i+1, h.Email, org)
	}
	fmt.Fprintf(c.errs, "Enter the number of the one to %s: ", verb)
	line, _ := c.reader().ReadString('\n')
	n, perr := strconv.Atoi(strings.TrimSpace(line))
	if perr != nil || n < 1 || n > len(amb.hits) {
		return account.Account{}, errors.New("no account chosen")
	}
	return amb.hits[n-1], nil
}

// tag is claude-swap's display tag: the organization for a team seat,
// else personal.
func tag(a account.Account) string {
	switch {
	case a.APIKey:
		return "API key"
	case a.Token:
		return "setup-token"
	case (a.PlanOrg == "claude_team" || a.PlanOrg == "claude_enterprise" || a.Plan == "team" || a.Plan == "enterprise") && a.OrgName != "":
		return a.OrgName
	}
	return "personal"
}

// inSessionShell reports whether this shell is a terminal opened as an
// account, its CLAUDE_CONFIG_DIR a profile: changing accounts from there
// would act on the wrong login (claude-swap's _refuse_session_shell).
func (c *cmd) inSessionShell() bool {
	dir := getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		return false
	}
	rel, err := filepath.Rel(account.ProfilesDir(c.b.Dir), dir)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
}

var getenv = os.Getenv

// mutating are the commands that change accounts or Claude Code's login.
var mutating = []string{"switch", "auto", "add", "add-token", "remove", "purge", "import", "import-usage", "move", "swap", "run",
	"disable", "enable", "threshold", "priority", "alias", "map", "unmap", "unclaimed"}

const restartHint = "Running Claude Code sessions take it within about 30 seconds (the keychain cache), or when started again."

// fileHint is restartHint where Claude Code keeps its login in a file
// (claude-swap's file backend): read again on the next message.
const fileHint = "Running Claude Code sessions take it on their next message: no restart needed."

// Newest is the newest version of the app on its update feed, "" when this
// one is; main sets it. nil: this build doesn't update itself.
var Newest func(ctx context.Context) (string, error)

// updateCheck is the cached answer of the update feed, as cswap's
// update_check: asked at most daily, an hour after a failed ask.
type updateCheck struct {
	CheckedAt time.Time `json:"checkedAt,omitzero"`
	FailedAt  time.Time `json:"failedAt,omitzero"`
	Latest    string    `json:"latest,omitempty"`
}

// updateHint is a line saying a newer version is out, or "".
func (c *cmd) updateHint(ctx context.Context) string {
	if Newest == nil {
		return ""
	}
	if p, err := c.b.Prefs(); err != nil || p.NoUpdates {
		return ""
	}
	path := filepath.Join(c.b.Dir, "update-check.json")
	var u updateCheck
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &u)
	}
	now := time.Now()
	if now.Sub(u.CheckedAt) >= 24*time.Hour && now.Sub(u.FailedAt) >= time.Hour {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		latest, err := Newest(ctx)
		cancel()
		if err != nil {
			u.FailedAt = now
		} else {
			u = updateCheck{CheckedAt: now, Latest: latest}
		}
		if data, err := json.Marshal(u); err == nil {
			_ = os.WriteFile(path, data, 0o600)
		}
	}
	if u.Latest == "" || u.Latest == Version {
		return ""
	}
	return "Claude Manager " + u.Latest + " is out: claude-manager upgrade installs it."
}
