//go:build !unix

package credentials

import (
	"errors"
	"os"
)

// openFlags open the credentials file; this platform has no O_NOFOLLOW, so
// Load's Lstat is the only symbolic link check.
const openFlags = os.O_RDONLY

// hasOwners: this build reads no POSIX owners or permission bits, so
// Load checks neither (Windows relies on the profile directory's ACL,
// architecture/login.md).
const hasOwners = false

var errNoOwners = errors.New("the platform has no file owners")

// fileOwner and pathOwner answer errNoOwners. File.private is false in
// this build (checksOwners), so Load never calls them; a File built by
// hand with private set is refused rather than trusted.
func fileOwner(*os.File) (int, error) { return 0, errNoOwners }

func pathOwner(string) (int, error) { return 0, errNoOwners }
