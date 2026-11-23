package view

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
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

// revealLabel names Reveal's button: Finder is macOS's file manager.
var revealLabel = map[bool]string{true: "Show in Finder", false: "Show in Folder"}[runtime.GOOS == "darwin"]

// maxShown is how much of a tool call or result the transcript shows.
const maxShown = 20000

// clip cuts text the transcript shows to at most maxShown bytes, on a rune.
func clip(s string) string {
	if len(s) <= maxShown {
		return s
	}
	n := maxShown
	for i := 0; i < utf8.UTFMax && n > 0 && !utf8.RuneStart(s[n]); i++ { // not past one rune: binary has long runs
		n--
	}
	return s[:n] + fmt.Sprintf("\n… %s more", humanSize(int64(len(s)-n)))
}

// commas writes n with thousands separators: 15,086.
func commas(n int64) string {
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0 && s[i-1] != '-'; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// money is a cost in dollars and cents: $1,234.56, or <$0.01 for a cost
// under a cent.
func money(f float64) string {
	if f > 0 && f < 0.01 {
		return "<$0.01"
	}
	c := int64(math.Round(f * 100))
	return fmt.Sprintf("$%s.%02d", commas(c/100), c%100)
}

// tokenCount is a count of tokens, short: 950, 12.3K, 820.4M, 2.1B. The unit
// is picked after rounding, so 999,950 is 1.0M, not 1000.0K.
func tokenCount(n int64) string {
	if n < 1000 {
		return strconv.FormatInt(n, 10)
	}
	for _, u := range []struct {
		div float64
		sym string
	}{{1e3, "K"}, {1e6, "M"}} {
		if math.Round(float64(n)/u.div*10) < 10000 {
			return fmt.Sprintf("%.1f%s", float64(n)/u.div, u.sym)
		}
	}
	return fmt.Sprintf("%.1fB", float64(n)/1e9)
}
