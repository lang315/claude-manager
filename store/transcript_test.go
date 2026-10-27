package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lang315/claude-manager/internal/fixture"
)

func TestReadTranscript(t *testing.T) {
	_, d, _ := newStore(t)
	var got []Message
	batches := 0
	bad, err := ReadTranscript(filepath.Join(d.Root, fixture.Proj, fixture.Old+".jsonl"), never, func(ms []Message) {
		batches++
		got = append(got, ms...)
	})
	if err != nil {
		t.Fatal(err)
	}
	if bad != 1 {
		t.Errorf("bad = %d, want 1", bad)
	}
	want := []Message{
		{Role: "user", Kind: "text", Text: "Fix the login bug\nIt fails on Safari"},
		{Role: "assistant", Kind: "thinking", Text: "Look at auth.go"},
		{Role: "assistant", Kind: "text", Text: "Reading **auth.go**."},
		{Role: "assistant", Kind: "tool_use", Text: `Read {"file_path":"auth.go"}`},
		{Role: "user", Kind: "tool_result", Text: "package auth"},
		{Role: "user", Kind: "tool_result", Text: "plain result"},
	}
	if len(got) != len(want) {
		t.Fatalf("%d messages, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Role != want[i].Role || got[i].Kind != want[i].Kind || got[i].Text != want[i].Text {
			t.Errorf("message %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if got[0].Time.IsZero() {
		t.Error("message time not parsed")
	}
	if batches != 1 {
		t.Errorf("%d batches for 6 messages, want 1", batches)
	}
}

func TestReadTranscriptMissing(t *testing.T) {
	if _, err := ReadTranscript(filepath.Join(t.TempDir(), "nope.jsonl"), never, func([]Message) {}); err == nil {
		t.Error("no error for a missing file")
	}
}

func never() bool { return false }

// Once stop says so, the reader stops between lines: a transcript left with
// Back is not read to its end.
func TestReadTranscriptStops(t *testing.T) {
	f := filepath.Join(t.TempDir(), "long.jsonl")
	line := `{"type":"user","message":{"role":"user","content":"hi"}}` + "\n"
	if err := os.WriteFile(f, []byte(strings.Repeat(line, 5*batchSize)), 0o644); err != nil {
		t.Fatal(err)
	}
	got := 0
	_, err := ReadTranscript(f, func() bool { return got > 0 }, func(ms []Message) { got += len(ms) })
	if err != nil {
		t.Fatal(err)
	}
	if got != batchSize {
		t.Errorf("read %d messages after stop, want the first batch of %d", got, batchSize)
	}
}
