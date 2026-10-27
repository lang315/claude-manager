package view

import "testing"

func TestOverviewOpensScreen(t *testing.T) {
	a, tt, _, _ := newApp(t)
	for _, name := range []string{"projects", "paste-cache", "settings.json"} {
		if !tt.HasText(name) {
			t.Errorf("overview lacks %q", name)
		}
	}
	tt.SetSize(1200, 1600)        // the folder list sits below the first screen
	must(t, tt.Click("projects")) // the folder's row opens its screen
	if a.page != "sessions" {
		t.Errorf("page %q after opening projects, want sessions", a.page)
	}
}
