package storage

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrInvalidSHA256 reports a string that is not a canonical lowercase
// 64-character hexadecimal SHA-256 value.
var ErrInvalidSHA256 = errors.New("storage: invalid sha256")

// SHA256 is a validated pack-content checksum (architecture section 9.3).
// It is the complete pack-byte digest recorded in manifest descriptors; an
// S3 ETag is never used for this purpose.
type SHA256 [32]byte

// ParseSHA256 parses the canonical form: exactly 64 lowercase hexadecimal
// characters. Uppercase hex and any other representation are rejected.
func ParseSHA256(s string) (SHA256, error) {
	var h SHA256
	if len(s) != 64 {
		return h, fmt.Errorf("%w: %q has length %d, want 64", ErrInvalidSHA256, s, len(s))
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		var nibble byte
		switch {
		case '0' <= c && c <= '9':
			nibble = c - '0'
		case 'a' <= c && c <= 'f':
			nibble = c - 'a' + 10
		default:
			return h, fmt.Errorf("%w: %q", ErrInvalidSHA256, s)
		}
		h[i/2] |= nibble << (4 * (1 - i%2))
	}
	return h, nil
}

// String returns the canonical lowercase 64-character hexadecimal form.
func (h SHA256) String() string {
	b := make([]byte, 64)
	hex.Encode(b, h[:])
	return string(b)
}

// MarshalJSON renders the canonical form as a JSON string.
func (h SHA256) MarshalJSON() ([]byte, error) {
	b := make([]byte, 0, 66)
	b = append(b, '"')
	b = append(b, h.String()...)
	b = append(b, '"')
	return b, nil
}

var _ json.Marshaler = SHA256{}
