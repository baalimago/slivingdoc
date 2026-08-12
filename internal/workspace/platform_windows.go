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
// os.Rename surfaces the raw Win32 error (ERROR_NOT_SAME_DEVICE); Go does
// not map it to a POSIX errno on Windows, so there is no EXDEV form to
// match.
func isCrossDevice(err error) bool {
	return errors.Is(err, windows.ERROR_NOT_SAME_DEVICE)
}
