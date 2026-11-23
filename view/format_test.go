package view

import (
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestHumanSize(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1536: "1.5 KB", 4_617_089_843: "4.3 GB"} {
		if got := humanSize(n); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestClipOnARune(t *testing.T) {
	s := strings.Repeat("a", maxShown-1) + "é and more"
	got := clip(s)
	if !utf8.ValidString(got) || !strings.HasPrefix(got, strings.Repeat("a", maxShown-1)+"\n… ") {
		t.Errorf("clip cut inside a rune: %q", got[maxShown-4:])
	}
	if bin := strings.Repeat("\x80", maxShown+10); len(clip(bin)) < maxShown-utf8.UTFMax {
		t.Error("clip walked back through binary to nothing")
	}
}

func TestPlural(t *testing.T) {
	for n, want := range map[int]string{0: "0 items", 1: "1 item", 2: "2 items"} {
		if got := plural(n, "item"); got != want {
			t.Errorf("plural(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	for in, want := range map[string]string{home + "/.claude": "~/.claude", home + "x/.claude": home + "x/.claude", "/tmp/x": "/tmp/x"} {
		if got := tilde(in); got != want {
			t.Errorf("tilde(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUsageFormats(t *testing.T) {
	for in, want := range map[int64]string{0: "0", 999: "999", 1000: "1,000", 15086: "15,086", 1234567: "1,234,567"} {
		if got := commas(in); got != want {
			t.Errorf("commas(%d) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[float64]string{0: "$0.00", 0.004: "<$0.01", 0.058: "$0.06", 15.488: "$15.49", 1234.5: "$1,234.50"} {
		if got := money(in); got != want {
			t.Errorf("money(%v) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[int64]string{950: "950", 999_949: "999.9K", 999_950: "1.0M", 999_950_000: "1.0B", 12345: "12.3K", 3124400: "3.1M", 820_400_000: "820.4M", 2_100_000_000: "2.1B"} {
		if got := tokenCount(in); got != want {
			t.Errorf("tokenCount(%d) = %q, want %q", in, got, want)
		}
	}
}
