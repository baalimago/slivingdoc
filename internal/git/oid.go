package git

import (
	"errors"
	"fmt"
)

// ErrInvalidOID reports a string that is not a Git SHA-1 object ID in its
// canonical form.
var ErrInvalidOID = errors.New("git: invalid object id")

// ParseOID parses a Git object ID in the canonical v1 form: exactly 40
// lowercase hexadecimal characters (architecture section 9.3). Uppercase
// hex and any other representation are rejected.
func ParseOID(s string) (OID, error) {
	if len(s) != 40 {
		return OID{}, fmt.Errorf("%w: %q", ErrInvalidOID, s)
	}
	var oid OID
	for i := 0; i < len(s); i++ {
		c := s[i]
		var nibble byte
		switch {
		case '0' <= c && c <= '9':
			nibble = c - '0'
		case 'a' <= c && c <= 'f':
			nibble = c - 'a' + 10
		default:
			return OID{}, fmt.Errorf("%w: %q", ErrInvalidOID, s)
		}
		oid[i/2] |= nibble << (4 * (1 - i%2))
	}
	return oid, nil
}

// IsZero reports whether the OID is the all-zero object ID, which no valid
// Git object has.
func (o OID) IsZero() bool { return o == OID{} }

// MarshalJSON renders the canonical 40-character lowercase hexadecimal text
// form as a JSON string. The manifest encoder relies on it to store Git
// object IDs in their normative shape (architecture section 9.2).
func (o OID) MarshalJSON() ([]byte, error) {
	b := make([]byte, 0, 42)
	b = append(b, '"')
	b = append(b, o.String()...)
	b = append(b, '"')
	return b, nil
}
