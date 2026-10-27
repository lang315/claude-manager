package store

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"io"
	"strconv"
	"time"
)

// call is one API call a transcript records.
type call struct {
	key   string // message.id/requestId, "" when the line lacks either
	time  time.Time
	model string
	callTokens
}

// usageFile is what a scan read of one transcript.
type usageFile struct {
	calls []call
	index map[string]int // a key's index in calls
	cwd   string         // the first cwd its calls carry
	bad   int            // usage lines that are not JSON
}

// usageFields are the counts of a usage record, at its top or in one of its
// iterations.
type usageFields struct {
	Input      int64 `json:"input_tokens"`
	Output     int64 `json:"output_tokens"`
	CacheRead  int64 `json:"cache_read_input_tokens"`
	CacheWrite int64 `json:"cache_creation_input_tokens"`
	CacheSplit *struct {
		Write5m int64 `json:"ephemeral_5m_input_tokens"`
		Write1h int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
	ServerTools struct {
		WebSearches int64 `json:"web_search_requests"`
	} `json:"server_tool_use"`
	Speed string `json:"speed"`
	Geo   string `json:"inference_geo"`
}

// usageLine is what a scan reads of a transcript line.
type usageLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
	RequestID string `json:"requestId"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			usageFields
			Iterations []struct {
				Type  string `json:"type"`
				Model string `json:"model"`
				usageFields
			} `json:"iterations"`
		} `json:"usage"`
	} `json:"message"`
}

// tokens are the counts of a usage record, negatives as 0. Without the
// 5m/1h split, every cache write counts as a 5-minute one.
func (u usageFields) tokens() callTokens {
	t := callTokens{
		Input: max(u.Input, 0), Output: max(u.Output, 0), CacheRead: max(u.CacheRead, 0),
		WebSearches: max(u.ServerTools.WebSearches, 0),
		Fast:        u.Speed == "fast", US: u.Geo == "us",
	}
	if s := u.CacheSplit; s != nil && s.Write5m+s.Write1h > 0 {
		t.CacheWrite5m, t.CacheWrite1h = max(s.Write5m, 0), max(s.Write1h, 0)
	} else {
		t.CacheWrite5m = max(u.CacheWrite, 0)
	}
	return t
}

var usageMark, assistantMark = []byte(`"usage"`), []byte(`"assistant"`)

// read reads the calls of r, which starts at a line boundary, and returns
// the bytes of the whole lines it read. A last line without "\n" is left for
// the next scan: Claude Code may still be writing it.
func (f *usageFile) read(r io.Reader) (int64, error) {
	br := bufio.NewReaderSize(r, 1<<20) // not a Scanner: a line can be megabytes
	var n int64
	for {
		line, err := br.ReadBytes('\n')
		if err == io.EOF {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		n += int64(len(line))
		// About one line in seven holds usage: the others are not decoded.
		if bytes.Contains(line, usageMark) && bytes.Contains(line, assistantMark) {
			f.line(line)
		}
	}
}

func (f *usageFile) line(b []byte) {
	var x usageLine
	if json.Unmarshal(b, &x) != nil {
		f.bad++
		return
	}
	m := x.Message
	if x.Type != "assistant" || m.Usage == nil || m.Model == "" || m.Model == "<synthetic>" {
		return
	}
	if f.cwd == "" {
		f.cwd = x.Cwd
	}
	t, _ := time.Parse(time.RFC3339Nano, x.Timestamp)
	key := ""
	if m.ID != "" && x.RequestID != "" {
		key = m.ID + "/" + x.RequestID
	}
	f.add(call{key, t, m.Model, m.Usage.tokens()})
	// An advisor's tokens are not in the top-level counts: each is a call of
	// its own, under the advisor's model.
	n := 0
	for _, it := range m.Usage.Iterations {
		if it.Type != "advisor_message" {
			continue
		}
		k := ""
		if key != "" {
			k = key + ":advisor:" + strconv.Itoa(n)
		}
		n++
		tk := it.tokens()
		tk.Fast = tk.Fast || it.Speed == "" && m.Usage.Speed == "fast"
		f.add(call{k, t, cmp.Or(it.Model, m.Model), tk})
	}
}

// add puts c among the file's calls, merged with an earlier copy of its key.
func (f *usageFile) add(c call) {
	if c.key == "" {
		f.calls = append(f.calls, c)
		return
	}
	if f.index == nil {
		f.index = map[string]int{}
	}
	if i, ok := f.index[c.key]; ok {
		f.calls[i] = merge(f.calls[i], c)
		return
	}
	f.index[c.key] = len(f.calls)
	f.calls = append(f.calls, c)
}

// merge returns the copy of a call to keep from a and a later copy b. A
// streaming partial comes first with a few output tokens, so the copy with
// the larger output has the final usage; b wins a tie. The time is the
// earlier one: when the call started.
func merge(a, b call) call {
	keep := a
	if b.Output >= a.Output {
		keep = b
	}
	keep.time = a.time
	if a.time.IsZero() || !b.time.IsZero() && b.time.Before(a.time) {
		keep.time = b.time
	}
	return keep
}
