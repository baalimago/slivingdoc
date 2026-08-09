package storage

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ErrInvalidUUID reports a string that is not a canonical lowercase
// RFC 9562 UUID in version 7 with the RFC 4122 variant.
var ErrInvalidUUID = errors.New("storage: invalid uuid")

// UUID is a validated RFC 9562 UUID value. The storage protocol accepts
// only the canonical lowercase text form; every protocol ID must be version
// 7 with the RFC 4122 variant (architecture section 9.1).
type UUID [16]byte

// NewUUIDv7 returns a new version-7 UUID from the current time and a
// cryptographically random source. The value always validates as a
// canonical version-7 UUID with the RFC 4122 variant.
func NewUUIDv7() (UUID, error) {
	u, err := uuid.NewV7()
	if err != nil {
		return UUID{}, fmt.Errorf("storage: generate uuidv7: %w", err)
	}
	return UUID(u), nil
}

// ParseUUIDv7 parses the canonical 8-4-4-4-12 lowercase hexadecimal text
// form and requires version 7 and the RFC 4122 variant. Uppercase
// characters, other versions, other variants, and any non-canonical layout
// are rejected.
func ParseUUIDv7(s string) (UUID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return UUID{}, fmt.Errorf("%w: %q", ErrInvalidUUID, s)
	}
	if u.Version() != 7 {
		return UUID{}, fmt.Errorf("%w: %q has version %d, want 7", ErrInvalidUUID, s, u.Version())
	}
	if u.Variant() != uuid.RFC4122 {
		return UUID{}, fmt.Errorf("%w: %q has a non-RFC 4122 variant", ErrInvalidUUID, s)
	}
	if u.String() != s {
		return UUID{}, fmt.Errorf("%w: %q is not in canonical lowercase form", ErrInvalidUUID, s)
	}
	return UUID(u), nil
}

// String returns the canonical lowercase 8-4-4-4-12 text form.
func (u UUID) String() string { return uuid.UUID(u).String() }

// IsZero reports whether the UUID is the all-zero value.
func (u UUID) IsZero() bool { return u == UUID{} }

// MarshalJSON renders the canonical text form as a JSON string.
func (u UUID) MarshalJSON() ([]byte, error) {
	b := make([]byte, 0, 38)
	b = append(b, '"')
	b = append(b, u.String()...)
	b = append(b, '"')
	return b, nil
}

var _ json.Marshaler = UUID{}
