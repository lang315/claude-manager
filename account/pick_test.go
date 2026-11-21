package account

import (
	"errors"
	"testing"
)

// claude-swap's _select_best_switchable: best moves only onto strictly more
// room than the account in use has, else stays and says why.
func TestPickBestComparesTheAccountInUse(t *testing.T) {
	for _, c := range []struct {
		alice, bob int
		to, stay   string
	}{
		{50, 10, "bob@example.com", ""},
		{10, 50, "", "already-best"},
		{30, 30, "", "already-best"}, // a tie stays
		{100, 100, "", "candidates-exhausted"},
		{-1, 10, "", "usage-unavailable"},
	} {
		b, _, _, f := switchBook(t)
		if c.alice >= 0 {
			f.usage["alice-at"] = c.alice
		} else {
			f.status["alice-at"] = 500
		}
		f.usage["bob-at"] = c.bob
		b.Refresh(ctx, true)
		got, err := b.Pick("best")
		var stay *Stay
		switch {
		case c.to != "" && (err != nil || got.Email != c.to):
			t.Errorf("%+v: %v %v", c, got.Email, err)
		case c.stay != "" && (!errors.As(err, &stay) || stay.Reason != c.stay):
			t.Errorf("%+v: got %q, %v", c, got.Email, err)
		}
	}
}

// --model's windows count in the comparison.
func TestHeadroomCountsModelWindows(t *testing.T) {
	a := Account{Limits: []Limit{{Kind: "session", Percent: 10}, {Kind: "weekly_scoped", Scope: "Fable", Percent: 95}}}
	if h, ok := headroom(a, Auto{}); !ok || h != 90 {
		t.Errorf("without a model: %v %v", h, ok)
	}
	if h, _ := headroom(a, Auto{Model: "fable"}); h != 5 {
		t.Errorf("with fable: %v", h)
	}
}
