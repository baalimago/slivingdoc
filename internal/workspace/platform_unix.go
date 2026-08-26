//go:build unix

package workspace

import "golang.org/x/sys/unix"

// noFollowFlag rejects a substituted symlink or a blockable special file
// at the final path component: O_NOFOLLOW fails the open on a symlink and
// O_NONBLOCK keeps a substituted FIFO or device from blocking the scan or
// the in-place write. The opened descriptor is re-checked with fstat
// before any read or write.
const noFollowFlag = unix.O_NOFOLLOW | unix.O_NONBLOCK
