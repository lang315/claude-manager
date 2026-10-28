package account

import (
	"errors"
	"testing"
	"time"
)

func TestParseUsageRows(t *testing.T) {
	body := `{"five_hour":{"utilization":1,"resets_at":"2026-09-20T22:20:00Z"},"limits":[
		{"kind":"session","group":"session","percent":62,"resets_at":"2026-09-20T22:20:00.095287+00:00","scope":null,"is_active":true},
		{"kind":"weekly_all","percent":48,"resets_at":"2026-09-27T02:00:00Z","scope":null},
		{"kind":"weekly_scoped","percent":0,"resets_at":"2026-09-27T02:00:00+00:00","scope":{"model":{"id":null,"display_name":"Fable"}}},
		{"kind":"weekly_surface","percent":7,"scope":{"surface":{"display_name":"Claude Code"}}}]}`
	got, err := ParseUsage([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		label   string
		percent float64
		resets  time.Time
	}{
		{"Session (5h)", 62, time.Date(2026, 9, 20, 22, 20, 0, 95287000, time.UTC)},
		{"Weekly", 48, time.Date(2026, 9, 27, 2, 0, 0, 0, time.UTC)},
		{"Weekly · Fable", 0, time.Date(2026, 9, 27, 2, 0, 0, 0, time.UTC)},
		{"weekly_surface · Claude Code", 7, time.Time{}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d limits, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Label() != w.label || got[i].Percent != w.percent || !got[i].ResetsAt.Equal(w.resets) {
			t.Errorf("limit %d = %q %v %v, want %q %v %v", i, got[i].Label(), got[i].Percent, got[i].ResetsAt, w.label, w.percent, w.resets)
		}
	}
}

func TestParseUsageSkipsBadRows(t *testing.T) {
	body := `{"limits":[{"kind":"session","percent":"x"},{"percent":5},{"kind":"weekly_all","percent":-1},
		{"kind":"weekly_all","percent":130,"resets_at":"soon"}]}`
	got, err := ParseUsage([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Percent != 100 || !got[0].ResetsAt.IsZero() {
		t.Fatalf("got %+v, want one weekly limit at 100%% with no reset", got)
	}
}

func TestParseUsageFallsBackToWindows(t *testing.T) {
	for _, body := range []string{
		`{"five_hour":{"utilization":62,"resets_at":"2026-09-20T22:20:00Z"},"seven_day":{"utilization":48,"resets_at":"2026-09-27T02:00:00Z"}}`,
		`{"five_hour":{"utilization":62,"resets_at":"2026-09-20T22:20:00Z"},"seven_day":{"utilization":48,"resets_at":"2026-09-27T02:00:00Z"},"limits":[{"kind":"session"}]}`,
	} {
		got, err := ParseUsage([]byte(body))
		if err != nil || len(got) != 2 || got[0].Label() != "Session (5h)" || got[0].Percent != 62 || got[1].Label() != "Weekly" || got[1].Percent != 48 {
			t.Errorf("%s: got %+v, %v", body, got, err)
		}
	}
}

func TestParseUsageWithoutLimits(t *testing.T) {
	for _, body := range []string{`{}`, `not json`, `{"limits":[]}`} {
		if _, err := ParseUsage([]byte(body)); !errors.Is(err, errNoLimits) {
			t.Errorf("%s: err = %v, want errNoLimits", body, err)
		}
	}
}
