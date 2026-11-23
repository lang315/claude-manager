// Package logfile is claude-swap's rotating log: a file that, past Max
// bytes, moves to .1 (and .1 to .2, …, Keep of them) before a new one.
package logfile

import (
	"fmt"
	"os"
	"sync"
)

// File is the log, safe to write from several goroutines.
type File struct {
	Path string
	Max  int64 // bytes before a rotation: claude-swap's 1 MB
	Keep int   // rotated files kept: claude-swap's 3
	mu   sync.Mutex
}

func (f *File) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if fi, err := os.Stat(f.Path); err == nil && fi.Size()+int64(len(p)) > f.Max {
		for i := f.Keep - 1; i >= 1; i-- {
			os.Rename(fmt.Sprintf("%s.%d", f.Path, i), fmt.Sprintf("%s.%d", f.Path, i+1))
		}
		if f.Keep > 0 {
			os.Rename(f.Path, f.Path+".1")
		} else {
			os.Remove(f.Path)
		}
	}
	w, err := os.OpenFile(f.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	n, err := w.Write(p)
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	return n, err
}
