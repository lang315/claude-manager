package view

import "testing"

func TestLimitsAddToken(t *testing.T) {
	l := limitsApp(t)
	canSwitch(t, l)
	l.open(t)
	waitFor(t, l.tt, func() bool { return l.tt.HasText("bob@example.com") })
	must(t, l.tt.Click("Add Token…"))
	waitFor(t, l.tt, func() bool { return l.tt.HasText("Add Setup Token") })
	if err := l.tt.Click("Add"); err == nil {
		l.tt.Frame()
		if l.tt.HasText("Added the setup-token.") {
			t.Fatal("added without a token")
		}
	}
	l.tt.Update(func() { l.a.limits.token.token, l.a.limits.token.email = "sk-ant-oat01-view", "ci@example.com" })
	l.tt.Frame()
	must(t, l.tt.Click("Read setup-token usage"))
	l.tt.Frame()
	must(t, l.tt.Click("Add"))
	waitFor(t, l.tt, func() bool {
		return l.tt.HasText("ci@example.com") && l.tt.HasText("Setup-token") && l.tt.HasText("37%")
	})
	if s, _ := l.book.AutoSettings(); !s.Probe {
		t.Error("probe not turned on")
	}
	clickMenu(t, l.tt, "ci@example.com")
	for _, item := range []string{"Sign In Again", "Open in Terminal…"} {
		if err := l.tt.ChooseMenuItem(item); err == nil {
			t.Errorf("%s offered for a setup-token", item)
		}
	}
}
