//go:build linux

// fadviseWarm queues the whole file into the page cache
// (POSIX_FADV_WILLNEED) and returns without waiting for the IO. On
// NFS this is the client's async readahead; errors are swallowed
// because warming is advisory and the cold read path always works.
package stream

import (
	"os"

	"golang.org/x/sys/unix"
)

func fadviseWarm(absPath string) {
	f, err := os.Open(absPath)
	if err != nil {
		return // advisory: the real request will surface errors
	}
	defer f.Close()
	// Fd() pins the fd to the runtime poller once; for a single
	// blocking use it is fine.
	_ = unix.Fadvise(int(f.Fd()), 0, 0, unix.FADV_WILLNEED)
}
