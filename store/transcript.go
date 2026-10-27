package store

import (
	"bufio"
	"bytes"
	"io"
	"os"
)

const batchSize = 200

// ReadTranscript reads a session's messages in file order, handing them to
// emit in batches as it goes, so a long session shows while it loads. Lines
// that are not JSON are skipped and counted in bad. It stops between lines
// once stop reports true, emitting nothing more.
func ReadTranscript(file string, stop func() bool, emit func([]Message)) (bad int, err error) {
	f, err := os.Open(file)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	var batch []Message
	for !stop() {
		line, rerr := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			if rec, ok := parseRecord(line); !ok {
				bad++
			} else {
				batch = append(batch, rec.messages()...)
			}
		}
		if len(batch) >= batchSize || (rerr != nil && len(batch) > 0) {
			emit(batch)
			batch = nil
		}
		if rerr == io.EOF {
			return bad, nil
		}
		if rerr != nil {
			return bad, rerr
		}
	}
	return bad, nil
}
