package view

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

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
		if len(parts) >= 3 && strings.HasSuffix(parts[2], "memory") { // a project's memory files, as Config reads them
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

// refreshEvery is the most often the watch reloads the screens.
const refreshEvery = 5 * time.Second

// hints is what follow reads: a mygo watch, or a test's fake.
type hints interface {
	Next(context.Context) ([]mygo.FileEvent, error)
	Err() error
}

// follow hands the screens each batch of changes affects to live, on the
// main thread, until the watch ends. It takes a batch at most once every
// interval: the watch's debounce lets a session that writes every second
// through each time, and the watch keeps gathering changes meanwhile. A
// watch that ends with an error says so once; Rescan still works.
func (a *App) follow(ctx context.Context, w hints, affects func(string) screens, every time.Duration) {
	var last time.Time // when the last batch was taken
	for {
		if wait := every - time.Since(last); !last.IsZero() && wait > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
		events, err := w.Next(ctx)
		last = time.Now()
		if err != nil {
			if werr := w.Err(); werr != nil {
				a.Update(func() { a.notice = "Live refresh is off: " + werr.Error() })
			}
			return
		}
		var s screens
		for _, e := range events {
			s |= affects(e.Path)
		}
		if s != 0 {
			a.Update(func() { a.live.hint(s) })
		}
	}
}

// Watch follows root, and claudeJSON when it lies outside root, until ctx
// ends. A watch that can't start says so in a toast; Rescan stays. Call it
// once the app is ready, off the main thread: it waits for the watch to
// start.
func (a *App) Watch(ctx context.Context, root, claudeJSON string) {
	opts := mygo.FileWatchOptions{Recursive: true, HintsOnly: true, Debounce: time.Second, MaxDelay: 5 * time.Second}
	if w, err := mygo.WatchFiles(ctx, root, opts); err != nil {
		a.Update(func() { a.notice = "Live refresh is off: " + err.Error() })
	} else {
		go a.follow(ctx, w, screensFor, refreshEvery)
	}
	// ~/.claude.json lies beside the root, unless CLAUDE_CONFIG_DIR puts it in.
	if rel, err := filepath.Rel(root, claudeJSON); err == nil && !strings.HasPrefix(rel, "..") {
		return
	}
	if _, err := os.Stat(claudeJSON); err != nil {
		return // not there: a Rescan reads it once it is
	}
	w, err := mygo.WatchFiles(ctx, claudeJSON, mygo.FileWatchOptions{Debounce: time.Second, MaxDelay: 5 * time.Second})
	if err != nil {
		a.Update(func() { a.notice = "Live refresh is off: " + err.Error() })
		return
	}
	go a.follow(ctx, w, func(string) screens { return configScreen }, refreshEvery)
}
