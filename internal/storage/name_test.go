package storage

import (
	"errors"
	"strings"
	"testing"
)

// TestValidateNewSpaceName checks the portable creation grammar over the
// same name vectors as the cloud mirror, cloud/src/keys.ts.
func TestValidateNewSpaceName(t *testing.T) {
	tests := []struct {
		name string
		ok   bool
	}{
		{"notes", true},
		{"a1b", true},
		{"team-notes", true},
		{"my-notes-2", true},
		{"a" + strings.Repeat("b", 61) + "c", true},
		{"", false},
		{"a", false},
		{"ab", false},
		{"-abc", false},
		{"abc-", false},
		{"Abc", false},
		{"a_b", false},
		{"a.b", false},
		{strings.Repeat("x", 64), false},
		{"xn--notes", false},
		{"sthree-notes", false},
		{"notes-s3alias", false},
		{"notes--ol-s3", false},
		{"notes--x-s3", false},
		{"notes--table-s3", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNewSpaceName(tt.name)
			if tt.ok {
				if err != nil {
					t.Fatalf("ValidateNewSpaceName(%q) = %v", tt.name, err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidNewSpaceName) {
				t.Fatalf("ValidateNewSpaceName(%q) = %v, want ErrInvalidNewSpaceName", tt.name, err)
			}
		})
	}
}
