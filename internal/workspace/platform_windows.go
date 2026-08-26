//go:build windows

package workspace

// noFollowFlag is the Windows open flag set. os.Root already prevents a
// substituted symlink from escaping the root; the scan rejects symlinks by
// Lstat before any open.
const noFollowFlag = 0
