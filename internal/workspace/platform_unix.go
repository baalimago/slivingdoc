//go:build unix

package workspace

import (
	"errors"

	"golang.org/x/sys/unix"
)

// noFollowFlag rejects a substituted symlink or a blockable special file
// at the final path component: O_NOFOLLOW fails the open on a symlink and
// O_NONBLOCK keeps a substituted FIFO or device from blocking the scan or
// the copy fallback. The opened descriptor is re-checked with fstat before
// any read or write.
const noFollowFlag = unix.O_NOFOLLOW | unix.O_NONBLOCK

// isCrossDevice reports whether a rename failed because the source and
// destination live on different devices, which triggers the copy fallback.
func isCrossDevice(err error) bool {
	return errors.Is(err, unix.EXDEV)
}
