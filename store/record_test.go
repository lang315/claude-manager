package store

import (
	"testing"
)

func msgs(t *testing.T, line string) []Message {
	t.Helper()
	r, ok := parseRecord([]byte(line))
	if !ok {
		t.Fatalf("parseRecord(%s) not ok", line)
	}
	return r.messages()
}

func TestMessages(t *testing.T) {
	if m := msgs(t, `{"type":"user","message":{"content":"hi"}}`); len(m) != 1 || m[0] != (Message{Role: "user", Kind: "text", Text: "hi"}) {
		t.Errorf("string content: %+v", m)
	}
	m := msgs(t, `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"hm"},{"type":"text","text":"ok"},{"type":"tool_use","name":"Read","input":{"a":1}},{"type":"image"}]}}`)
	want := []Message{{Role: "assistant", Kind: "thinking", Text: "hm"}, {Role: "assistant", Kind: "text", Text: "ok"}, {Role: "assistant", Kind: "tool_use", Text: `Read {"a":1}`}}
	if len(m) != len(want) {
		t.Fatalf("list content: %+v", m)
	}
	for i := range want {
		if m[i] != want[i] {
			t.Errorf("block %d: %+v, want %+v", i, m[i], want[i])
		}
	}
	for _, c := range []string{`"plain"`, `[{"type":"text","text":"pl"},{"type":"text","text":"ain"}]`} {
		m := msgs(t, `{"type":"user","message":{"content":[{"type":"tool_result","content":`+c+`}]}}`)
		if len(m) != 1 || m[0].Kind != "tool_result" || m[0].Text != "plain" {
			t.Errorf("tool_result content %s: %+v", c, m)
		}
	}
	if m := msgs(t, `{"type":"user","isMeta":true,"message":{"content":"x"}}`); len(m) != 0 {
		t.Errorf("isMeta: %+v", m)
	}
	if m := msgs(t, `{"type":"attachment","message":{"content":"x"}}`); len(m) != 0 {
		t.Errorf("other type: %+v", m)
	}
}

func TestParseRecord(t *testing.T) {
	r, ok := parseRecord([]byte(`{"type":"file-history-snapshot","timestamp":123,"cwd":"/Users/me/proj"}`))
	if !ok || r.Cwd != "/Users/me/proj" || !r.time().IsZero() {
		t.Errorf("numeric timestamp: %+v ok=%v", r, ok)
	}
	r, ok = parseRecord([]byte(`{"type":"user","isMeta":"yes","cwd":"/x"}`))
	if !ok || r.Cwd != "/x" {
		t.Errorf("type mismatch: %+v ok=%v", r, ok)
	}
	if _, ok := parseRecord([]byte(`{not json`)); ok {
		t.Error("syntax error reads as ok")
	}
}
