package store

import (
	"encoding/json"
	"strings"
	"time"
)

// Message is one block of a user or assistant turn.
type Message struct {
	Role string // "user" or "assistant"
	Kind string // "text", "thinking", "tool_use" or "tool_result"
	Text string
	Time time.Time
}

// record is one line of a session's JSONL. Fields whose shape varies by
// record type stay raw, so that a type mismatch never fails the line.
type record struct {
	Type      string          `json:"type"`
	IsMeta    bool            `json:"isMeta"`
	Cwd       string          `json:"cwd"`
	GitBranch string          `json:"gitBranch"`
	Version   string          `json:"version"`
	Timestamp json.RawMessage `json:"timestamp"`
	AITitle   string          `json:"aiTitle"`
	Message   json.RawMessage `json:"message"`
}

type block struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Content  json.RawMessage `json:"content"`
}

func (r record) time() time.Time {
	var s string
	if json.Unmarshal(r.Timestamp, &s) != nil {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// messages returns the blocks of a user or assistant record. Records Claude
// Code injects (isMeta) and every other record type have none.
func (r record) messages() []Message {
	if (r.Type != "user" && r.Type != "assistant") || r.IsMeta {
		return nil
	}
	var m struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(r.Message, &m) != nil {
		return nil
	}
	var out []Message
	for _, b := range blocks(m.Content) {
		msg := Message{Role: r.Type, Kind: b.Type, Time: r.time()}
		switch b.Type {
		case "text":
			msg.Text = b.Text
		case "thinking":
			msg.Text = b.Thinking
		case "tool_use":
			msg.Text = b.Name + " " + string(b.Input)
		case "tool_result":
			msg.Text = plain(b.Content)
		default:
			continue
		}
		out = append(out, msg)
	}
	return out
}

// blocks reads content that is either a string or a list of blocks.
func blocks(raw json.RawMessage) []block {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []block{{Type: "text", Text: s}}
	}
	var bs []block
	json.Unmarshal(raw, &bs)
	return bs
}

// plain joins the text of content that is a string or a list of blocks.
func plain(raw json.RawMessage) string {
	var b strings.Builder
	for _, x := range blocks(raw) {
		b.WriteString(x.Text)
	}
	return b.String()
}
