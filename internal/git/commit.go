package git

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// CreateCommit creates a commit with the supplied message, tree, and zero or
// more parents. The engine writes the fixed slivingdoc identity and the spec
// time in UTC with one-second precision and offset zero. A root commit has
// no parents; a normal commit has exactly the observed remote head as its
// single parent.
func CreateCommit(repo Repository, spec CommitSpec) (OID, error) {
	if err := ValidateCommitMessage(spec.Message); err != nil {
		return OID{}, fmt.Errorf("git: create commit: %w", err)
	}
	if spec.Tree.IsZero() {
		return OID{}, fmt.Errorf("git: create commit: tree is required")
	}
	id, err := repo.CreateCommit(spec)
	if err != nil {
		return OID{}, fmt.Errorf("git: create commit: %w", err)
	}
	return id, nil
}

// ValidateCommitMessage rejects messages the native commit call cannot store
// faithfully: they must be valid UTF-8 without U+0000. The check runs before
// any native mutation.
func ValidateCommitMessage(message string) error {
	switch {
	case !utf8.ValidString(message):
		return fmt.Errorf("invalid commit message: not valid UTF-8")
	case strings.IndexByte(message, 0) >= 0:
		return fmt.Errorf("invalid commit message: contains U+0000")
	}
	return nil
}
