package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
var mutating = []string{"switch", "add", "add-token", "remove", "purge", "import", "import-usage", "move", "swap", "run",
	"disable", "enable", "threshold", "priority", "alias", "map", "unmap", "unclaimed"}

const restartHint = "Running Claude Code sessions take it within about 30 seconds (the keychain cache), or when started again."
