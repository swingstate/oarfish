package metrics

import "syscall"

// Statfs is the part of a statfs(2) result the dashboard uses.
type Statfs struct {
	BlockSize uint64
	Blocks    uint64 // total blocks
	Free      uint64 // free blocks, including those reserved for root
	Available uint64 // free blocks available to an unprivileged user
}

// StatfsFunc reads the size and use of the file system mounted at path.
type StatfsFunc func(path string) (Statfs, error)

// HostStatfs is the production StatfsFunc. It is a read-only system call on a
// mount point taken from /proc/self/mountinfo after validation; it uses no cgo.
func HostStatfs(path string) (Statfs, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return Statfs{}, err
	}
	// The field types differ between platforms (int64 or uint32); a negative
	// block size would be a kernel bug and is converted to a huge number that
	// the caller's overflow check rejects.
	return Statfs{BlockSize: uint64(st.Bsize), Blocks: uint64(st.Blocks), Free: uint64(st.Bfree), Available: uint64(st.Bavail)}, nil
}
