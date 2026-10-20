package storage

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidNewSpaceName reports a name outside the portable creation
// grammar.
var ErrInvalidNewSpaceName = errors.New("storage: invalid new space name")

// newSpaceNameMin and newSpaceNameMax bound the portable creation grammar.
const (
	newSpaceNameMin = 3
	newSpaceNameMax = 63
)

// reservedNamePrefixes and reservedNameSuffixes are the names S3 keeps for
// itself, so a bucket created under one of them is refused by the service.
var (
	reservedNamePrefixes = []string{"xn--", "sthree-"}
	reservedNameSuffixes = []string{"-s3alias", "--ol-s3", "--x-s3", "--table-s3"}
)

// ValidateNewSpaceName reports whether name may be used to create a space
// or a bucket on hosted storage, S3 and R2 alike: 3 to 63 characters,
// lowercase ASCII letters, digits, and inner hyphens, a letter or digit at
// each end, and no S3 reserved name. Listing, selecting, and minting keep
// the wider hosted accept grammar (architecture/storage.md).
func ValidateNewSpaceName(name string) error {
	if len(name) < newSpaceNameMin || len(name) > newSpaceNameMax || !newSpaceNameChars(name) {
		return fmt.Errorf("%w: %q must be 3 to 63 lowercase letters, digits, or inner hyphens", ErrInvalidNewSpaceName, name)
	}
	for _, prefix := range reservedNamePrefixes {
		if strings.HasPrefix(name, prefix) {
			return fmt.Errorf("%w: %q must not start with %q, which S3 reserves", ErrInvalidNewSpaceName, name, prefix)
		}
	}
	for _, suffix := range reservedNameSuffixes {
		if strings.HasSuffix(name, suffix) {
			return fmt.Errorf("%w: %q must not end with %q, which S3 reserves", ErrInvalidNewSpaceName, name, suffix)
		}
	}
	return nil
}

// newSpaceNameChars reports whether name has only the creation grammar's
// characters, with a letter or digit at each end.
func newSpaceNameChars(name string) bool {
	for i := 0; i < len(name); i++ {
		c := name[i]
		alnum := c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
		if i == 0 || i == len(name)-1 {
			if !alnum {
				return false
			}
			continue
		}
		if !alnum && c != '-' {
			return false
		}
	}
	return true
}
