package view

import "strings"

// screens is a set of the pages whose loads read ~/.claude. Cleanup is not
// one: the Sessions load reloads it.
type screens uint8

const (
	overviewScreen screens = 1 << iota
	sessionsScreen
	historyScreen
	usageScreen
	configScreen

	allScreens = overviewScreen | sessionsScreen | historyScreen | usageScreen | configScreen
)

// screensFor maps a folder the root's watch hints at, relative to the root
// and slash-separated, to the pages that read it. "." is a file at the
// root, or the system asking to look at everything.
func screensFor(path string) screens {
	parts := strings.Split(path, "/")
	switch parts[0] {
	case ".":
		return allScreens
	case "projects":
		s := sessionsScreen | usageScreen | overviewScreen
		if len(parts) >= 3 && parts[2] == "memory" { // a project's memory files: Config shows them
			s |= configScreen
		}
		return s
	case "sessions":
		return sessionsScreen
	case "plugins", "skills", "commands", "agents":
		return configScreen | overviewScreen
	}
	return sessionsScreen | overviewScreen
}

// live paces the reloads the watch asks for. While the app is active it
// starts them; in the background it holds them until the app is back. It
// never starts a screen still loading: that one waits, and runs once when
// it lands. It runs on the main thread.
type live struct {
	active            bool
	pending, inflight screens
	start             func(screens) // starts the quiet loads of a set
}

// hint asks for the screens of s to reload.
func (l *live) hint(s screens) {
	l.pending |= s
	l.run()
}

// landed says the quiet loads of s are done.
func (l *live) landed(s screens) {
	l.inflight &^= s
	l.run()
}

func (l *live) activate() {
	l.active = true
	l.run()
}

func (l *live) resign() { l.active = false }

func (l *live) run() {
	if !l.active {
		return
	}
	s := l.pending &^ l.inflight
	if s == 0 {
		return
	}
	l.pending &^= s
	l.inflight |= s
	l.start(s)
}
