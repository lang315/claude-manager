package account

import (
	"context"
	"errors"
	"strings"
	"time"
)

// claude-swap's subscription.py: the plan read from Anthropic's profile,
// every 6 hours, for its label and to tell a lapsed subscription.

const (
	planTTL   = 6 * time.Hour
	planRetry = 15 * time.Minute
	freeTier  = "default_claude_ai"
)

var (
	tierLabels = map[string]string{"default_claude_ai": "Free", "default_claude_max_5x": "Max 5x", "default_claude_max_20x": "Max 20x"}
	// A Team or Enterprise seat is no personal subscription, and a paying
	// Pro reports the free tier: the organization type names those.
	orgLabels = map[string]string{"claude_team": "Team", "claude_enterprise": "Enterprise", "claude_pro": "Pro"}
)

// PlanLabel is the plan's name as claude-swap's plan_label: the
// organization type where known, else the server's plan name, else the
// tier's; "" when the profile has not said.
func PlanLabel(a Account) string {
	if a.PlanTier == "" {
		return ""
	}
	if l, ok := orgLabels[a.PlanOrg]; ok {
		return l
	}
	label, ok := tierLabels[a.PlanTier]
	if !ok {
		label = a.PlanTier
	}
	if a.PlanName != "" && !strings.HasPrefix(label, a.PlanName) {
		return a.PlanName
	}
	return label
}

// PlanLapsed reports a subscription that has lapsed: canceled, and back
// on the free tier.
func PlanLapsed(a Account) bool { return a.PlanStatus == "canceled" && a.PlanTier == freeTier }

// readPlan reads a's plan with token when it is 6 hours old, and keeps it
// only when the answer is provably a's (claude-swap's matches_slot); a
// failed read is tried again in 15 minutes.
func (b *Book) readPlan(ctx context.Context, a *Account, token string, now time.Time) {
	if token == "" || a.Token || a.APIKey || now.Sub(a.PlanAt) < planTTL || now.Before(a.PlanRetry) {
		return
	}
	p, err := b.profileOf(ctx, token) // the login in use's is asked whose it is anyway
	if err != nil || p.Tier == "" || p.AccountUUID != a.AccountUUID || a.OrgUUID != "" && p.OrgUUID != a.OrgUUID {
		// claude-swap's own plan retry: 15 minutes, or as long as Anthropic
		// asks, a day at most; PlanAt keeps the plan's true age.
		wait := planRetry
		var rl *RateLimited
		if errors.As(err, &rl) {
			wait = min(max(rl.Wait, planRetry), 24*time.Hour)
		}
		a.PlanRetry = now.Add(wait)
		return
	}
	a.PlanTier, a.PlanOrg, a.PlanStatus, a.PlanName, a.PlanAt, a.PlanRetry = p.Tier, p.OrgType, p.Status, p.PlanName, now, time.Time{}
}

// StoreNote is claude-swap's store-unmirrored note: with
// CLAUDE_SECURESTORAGE_CONFIG_DIR set, Claude Code keeps its login where
// the app doesn't look.
func StoreNote(getenv func(string) string) string {
	if getenv("CLAUDE_SECURESTORAGE_CONFIG_DIR") == "" {
		return ""
	}
	return "CLAUDE_SECURESTORAGE_CONFIG_DIR is set: Claude Code keeps its sign-in elsewhere. Unset it, or open the app from a normal shell."
}
