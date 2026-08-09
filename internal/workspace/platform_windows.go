//go:build windows

package workspace

import (
	"errors"

	"golang.org/x/sys/windows"
)

// noFollowFlag is the Windows open flag set. os.Root already prevents a
// substituted symlink from escaping the root; the scan rejects symlinks by
// Lstat before any open.
const noFollowFlag = 0

// isCrossDevice reports whether a rename failed because the source and
// destination live on different volumes, which triggers the copy fallback.
// Go maps the Win32 ERROR_NOT_SAME_DEVICE to EXDEV; both forms are
// accepted defensively.
func isCrossDevice(err error) bool {
	return errors.Is(err, windows.EXDEV) || errors.Is(err, windows.ERROR_NOT_SAME_DEVICE)
}
