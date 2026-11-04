package main

import (
	"syscall"
	"unsafe"
)

// clockBoottime is CLOCK_BOOTTIME of <linux/time.h>; package syscall does not
// export it.
const clockBoottime = 7

// bootSeconds returns the whole seconds since boot, including time spent
// suspended, from clock_gettime(2) through the standard library.
func bootSeconds() (int64, error) {
	var ts syscall.Timespec
	_, _, errno := syscall.Syscall(syscall.SYS_CLOCK_GETTIME, clockBoottime, uintptr(unsafe.Pointer(&ts)), 0)
	if errno != 0 {
		return 0, errno
	}
	return int64(ts.Sec), nil
}
