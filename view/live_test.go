package view

import "testing"

func TestScreensFor(t *testing.T) {
	for path, want := range map[string]screens{
		".":                       allScreens,
		"projects":                sessionsScreen | usageScreen | overviewScreen,
		"projects/-Users-me-proj": sessionsScreen | usageScreen | overviewScreen,
		"projects/-Users-me-proj/55555555-5555-4555-8555-555555555555/subagents": sessionsScreen | usageScreen | overviewScreen,
		"projects/-Users-me-proj/memory":                                         sessionsScreen | usageScreen | overviewScreen | configScreen,
		"sessions":                                                               sessionsScreen,
		"plugins/marketplaces/x":                                                 configScreen | overviewScreen,
		"skills/demo":                                                            configScreen | overviewScreen,
		"commands":                                                               configScreen | overviewScreen,
		"agents":                                                                 configScreen | overviewScreen,
		"file-history/abc":                                                       sessionsScreen | overviewScreen,
		"debug":                                                                  sessionsScreen | overviewScreen,
	} {
		if got := screensFor(path); got != want {
			t.Errorf("screensFor(%q) = %05b, want %05b", path, got, want)
		}
	}
}

// newLive is a live whose starts are recorded, not run.
func newLive() (*live, *[]screens) {
	var started []screens
	return &live{active: true, start: func(s screens) { started = append(started, s) }}, &started
}

func TestLiveHintStarts(t *testing.T) {
	l, started := newLive()
	l.hint(sessionsScreen | usageScreen)
	if len(*started) != 1 || (*started)[0] != sessionsScreen|usageScreen || l.inflight != sessionsScreen|usageScreen {
		t.Errorf("started %v, inflight %05b", *started, l.inflight)
	}
}

// A hint for a screen that is loading waits for it to land, then runs once.
func TestLiveHintWhileLoading(t *testing.T) {
	l, started := newLive()
	l.hint(sessionsScreen)
	l.hint(sessionsScreen | historyScreen)
	l.hint(allScreens)
	if len(*started) != 3 || (*started)[1] != historyScreen || (*started)[2] != overviewScreen|usageScreen|configScreen {
		t.Fatalf("started %v: a loading screen started twice", *started)
	}
	l.landed(sessionsScreen)
	if len(*started) != 4 || (*started)[3] != sessionsScreen {
		t.Errorf("after it landed, started %v; want sessions once more", *started)
	}
	l.landed(sessionsScreen)
	if len(*started) != 4 {
		t.Errorf("a second landing started %v", *started)
	}
}

// In the background, hints pile up and run once when the app is back.
func TestLiveHoldsInBackground(t *testing.T) {
	l, started := newLive()
	l.resign()
	for range 10 {
		l.hint(sessionsScreen)
		l.hint(configScreen)
	}
	if len(*started) != 0 {
		t.Fatalf("started %v in the background", *started)
	}
	l.activate()
	if len(*started) != 1 || (*started)[0] != sessionsScreen|configScreen {
		t.Errorf("on activation, started %v; want sessions and config once", *started)
	}
}
