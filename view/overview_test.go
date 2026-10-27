package view

import "testing"

func TestOverviewOpensScreen(t *testing.T) {
	a, tt, _, _ := newApp(t)
	for _, name := range []string{"projects", "paste-cache", "settings.json"} {
		if !tt.HasText(name) {
			t.Errorf("overview lacks %q", name)
		}
	}
	must(t, tt.Click("projects"))
	must(t, tt.Click("Open in Sessions"))
	if a.page != "sessions" {
		t.Errorf("page %q after opening projects, want sessions", a.page)
	}
}
