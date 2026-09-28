package integrationtest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// memoryRoot is where the suite's scratch directory goes when the host has
// a memory filesystem with room for it.
const memoryRoot = "/dev/shm"

// memoryScratchPrefix names the scratch directories, followed by the
// owning test process's PID, so a later run can remove the directory of a
// run that was killed (a timeout's panic skips TestMain's cleanup).
const memoryScratchPrefix = "slivingdoc-integration-"

// memoryScratchMinFree is the free space a memory filesystem must offer; a
// small one (a container's 64 MiB /dev/shm) would fail scenarios with
// ENOSPC instead of speeding them up.
const memoryScratchMinFree = 1 << 30

// scratchUnavailable says why the suite keeps the default temporary
// directory.
type scratchUnavailable struct{ reason string }

func (e *scratchUnavailable) Error() string { return "memory scratch unavailable: " + e.reason }

// memoryScratch creates the suite's scratch directory on a memory
// filesystem (architecture/testing.md, Scratch on a memory filesystem). It
// returns a *scratchUnavailable when the host has none that fits; the
// suite then runs on the default temporary directory.
func memoryScratch() (string, error) {
	var fs unix.Statfs_t
	if err := unix.Statfs(memoryRoot, &fs); err != nil {
		return "", &scratchUnavailable{reason: fmt.Sprintf("%s: %v", memoryRoot, err)}
	}
	if fs.Type != unix.TMPFS_MAGIC {
		return "", &scratchUnavailable{reason: memoryRoot + " is not a memory filesystem"}
	}
	if free := fs.Bavail * uint64(fs.Bsize); free < memoryScratchMinFree {
		return "", &scratchUnavailable{reason: fmt.Sprintf("%s has %d bytes free, want %d", memoryRoot, free, memoryScratchMinFree)}
	}
	removeAbandonedScratch()
	dir, err := os.MkdirTemp(memoryRoot, memoryScratchPrefix+strconv.Itoa(os.Getpid())+"-")
	if err != nil {
		return "", &scratchUnavailable{reason: err.Error()}
	}
	return dir, nil
}

// removeAbandonedScratch deletes the scratch directories of test processes
// that no longer run. It is best effort: a directory it cannot read or
// remove is left for the next run.
func removeAbandonedScratch() {
	entries, err := os.ReadDir(memoryRoot)
	if err != nil {
		return
	}
	for _, entry := range entries {
		rest, ok := strings.CutPrefix(entry.Name(), memoryScratchPrefix)
		if !ok || !entry.IsDir() {
			continue
		}
		pidText, _, _ := strings.Cut(rest, "-")
		pid, err := strconv.Atoi(pidText)
		if err != nil || processRuns(pid) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(memoryRoot, entry.Name()))
	}
}

// processRuns reports whether a process with this PID exists; EPERM means
// it exists under another user.
func processRuns(pid int) bool {
	err := unix.Kill(pid, 0)
	return err == nil || errors.Is(err, unix.EPERM)
}
