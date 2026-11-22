//go:build windows

package store

import (
	"syscall"
	"time"
	"unsafe"
)

var queryImage = syscall.NewLazyDLL("kernel32.dll").NewProc("QueryFullProcessImageNameW")

// ps answers what SameProcess asks of ps: "lstart=" is the start in ps's
// layout, UTC; "comm=" is the image path.
func ps(pid int, field ...string) (string, error) {
	h, err := syscall.OpenProcess(0x1000, false, uint32(pid)) // PROCESS_QUERY_LIMITED_INFORMATION
	if err != nil {
		return "", err
	}
	defer syscall.CloseHandle(h)
	if field[0] == "lstart=" {
		var created, exited, kernel, user syscall.Filetime
		if err := syscall.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
			return "", err
		}
		return time.Unix(0, created.Nanoseconds()).UTC().Format("Mon Jan _2 15:04:05 2006"), nil
	}
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if r, _, err := queryImage.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))); r == 0 {
		return "", err
	}
	return syscall.UTF16ToString(buf[:n]), nil
}
