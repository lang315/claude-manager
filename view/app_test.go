package view

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/lang315/claude-manager/store"
)

func TestSidebarChoosesPage(t *testing.T) {
	a := New(&store.Store{Root: t.TempDir()})
	tt := ui.NewTester(a.View, 1200, 800)
	for _, label := range []string{"Overview", "Sessions", "Config", "Cleanup"} {
		if !tt.HasText(label) {
			t.Errorf("sidebar lacks %q", label)
		}
	}
	if err := tt.Click("Cleanup"); err != nil {
		t.Fatal(err)
	}
	if a.page != "cleanup" {
		t.Errorf("page %q after clicking Cleanup", a.page)
	}
}
