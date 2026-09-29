//go:build !linux

package scratch

// memoryScratch reports that only Linux hosts get a memory-filesystem
// scratch directory (architecture/testing.md, Scratch on a memory
// filesystem); tests run on the default temporary directory.
func memoryScratch() (string, error) {
	return "", &Unavailable{reason: "only Linux hosts have a known memory filesystem"}
}
