//go:build !linux

package integrationtest

// scratchUnavailable says why the suite keeps the default temporary
// directory.
type scratchUnavailable struct{ reason string }

func (e *scratchUnavailable) Error() string { return "memory scratch unavailable: " + e.reason }

// memoryScratch reports that only Linux hosts get a memory-filesystem
// scratch directory (architecture/testing.md, Scratch on a memory
// filesystem); the suite runs on the default temporary directory.
func memoryScratch() (string, error) {
	return "", &scratchUnavailable{reason: "only Linux hosts have a known memory filesystem"}
}
