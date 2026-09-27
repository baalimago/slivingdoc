//go:build !unix

package credentials

import (
	"errors"
	"os"
)

// openFlags open the credentials file; this platform has no O_NOFOLLOW, so
// Load's Lstat is the only symbolic link check.
const openFlags = os.O_RDONLY

var errNoOwners = errors.New("the platform has no file owners")

// fileOwner and pathOwner are never reached: File.private is false where
// there are no POSIX owners.
func fileOwner(*os.File) (int, error) { return 0, errNoOwners }

func pathOwner(string) (int, error) { return 0, errNoOwners }
