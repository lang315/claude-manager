package view

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("2006-01-02 15:04")
}

// plural counts n of a noun whose plural adds an s: "1 item", "2 items".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// tilde writes a path under the home directory with ~.
func tilde(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home+"/") {
		return "~" + p[len(home):]
	}
	return p
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// maxShown is how much of a tool call or result the transcript shows.
const maxShown = 20000

// clip cuts text the transcript shows to maxShown bytes.
// shortcut: cuts on a byte, so a multi-byte rune at the cut may show as �; fine for logs, cut on a rune if users notice.
func clip(s string) string {
	if len(s) <= maxShown {
		return s
	}
	return s[:maxShown] + fmt.Sprintf("\n… %s more", humanSize(int64(len(s)-maxShown)))
}
