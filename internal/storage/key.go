package storage

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// PackKind distinguishes the two immutable pack namespaces.
type PackKind string

const (
	// KindCheckpoint marks a checkpoint pack. Its generation is the
	// through-generation of the compacted state.
	KindCheckpoint PackKind = "checkpoint"
	// KindIncrement marks an incremental pack. Its generation is the target
	// generation of the published head.
	KindIncrement PackKind = "increment"
)

// Valid reports whether k is one of the two protocol pack kinds.
func (k PackKind) Valid() bool { return k == KindCheckpoint || k == KindIncrement }

// ErrInvalidKey reports a string that is not a valid protocol object key.
var ErrInvalidKey = errors.New("storage: invalid protocol key")

// ErrInvalidPrefix reports a configured S3 prefix that violates the
// architecture 9.1 grammar.
var ErrInvalidPrefix = errors.New("storage: invalid prefix")

// Key is a validated protocol object key relative to the configured S3
// prefix (architecture section 9.1):
//
//	packs/checkpoints/<throughGeneration>-<checkpoint-id>.pack
//	packs/increments/<generation>-<publication-id>.pack
//
// The key grammar embeds the target or through generation and a unique
// publication or checkpoint ID, so every pack key exposes its generation
// for bounded cleanup. The manifest descriptor is authoritative; Key
// values are built from it or parsed from stored manifests.
type Key struct {
	Kind       PackKind
	Generation uint64
	ID         UUID
}

// String returns the canonical protocol key form. It returns the empty
// string for a kind other than the two protocol pack kinds; callers build
// keys from validated descriptors or ParseKey, and the manifest validator
// rejects an invalid kind before any Key reaches the wire.
func (k Key) String() string {
	switch k.Kind {
	case KindCheckpoint:
		return fmt.Sprintf("packs/checkpoints/%d-%s.pack", k.Generation, k.ID)
	case KindIncrement:
		return fmt.Sprintf("packs/increments/%d-%s.pack", k.Generation, k.ID)
	default:
		return ""
	}
}

// MarshalJSON renders the canonical protocol key as a JSON string.
func (k Key) MarshalJSON() ([]byte, error) {
	s, err := k.string()
	if err != nil {
		return nil, err
	}
	b := make([]byte, 0, len(s)+2)
	b = append(b, '"')
	b = append(b, s...)
	b = append(b, '"')
	return b, nil
}

func (k Key) string() (string, error) {
	switch k.Kind {
	case KindCheckpoint:
		return fmt.Sprintf("packs/checkpoints/%d-%s.pack", k.Generation, k.ID), nil
	case KindIncrement:
		return fmt.Sprintf("packs/increments/%d-%s.pack", k.Generation, k.ID), nil
	default:
		return "", fmt.Errorf("%w: invalid pack kind %q", ErrInvalidKey, k.Kind)
	}
}

// ParseKey parses a canonical protocol key and returns its kind,
// generation, and ID. A key with the wrong namespace, a non-canonical
// generation, a malformed ID, or a non-canonical layout is rejected.
func ParseKey(s string) (Key, error) {
	segs := strings.Split(s, "/")
	if len(segs) != 3 || segs[0] != "packs" {
		return Key{}, fmt.Errorf("%w: %q", ErrInvalidKey, s)
	}
	kind, ok := map[string]PackKind{
		"checkpoints": KindCheckpoint,
		"increments":  KindIncrement,
	}[segs[1]]
	if !ok {
		return Key{}, fmt.Errorf("%w: %q", ErrInvalidKey, s)
	}
	body, ok := strings.CutSuffix(segs[2], ".pack")
	if !ok || body == "" {
		return Key{}, fmt.Errorf("%w: %q", ErrInvalidKey, s)
	}
	genStr, idStr, ok := strings.Cut(body, "-")
	if !ok || genStr == "" || idStr == "" {
		return Key{}, fmt.Errorf("%w: %q", ErrInvalidKey, s)
	}
	for _, c := range []byte(genStr) {
		if c < '0' || c > '9' {
			return Key{}, fmt.Errorf("%w: %q", ErrInvalidKey, s)
		}
	}
	gen, err := strconv.ParseUint(genStr, 10, 64)
	if err != nil {
		return Key{}, fmt.Errorf("%w: %q", ErrInvalidKey, s)
	}
	if genStr != strconv.FormatUint(gen, 10) {
		return Key{}, fmt.Errorf("%w: %q has a non-canonical generation", ErrInvalidKey, s)
	}
	id, err := ParseUUIDv7(idStr)
	if err != nil {
		return Key{}, fmt.Errorf("%w: %q", ErrInvalidKey, s)
	}
	return Key{Kind: kind, Generation: gen, ID: id}, nil
}

// ValidatePrefix rejects a configured S3 key prefix that violates the
// architecture 9.1 grammar: empty is valid; otherwise the prefix is a
// slash-separated relative key prefix with no leading slash, trailing
// slash, empty segment, backslash, "." segment, or ".." segment.
func ValidatePrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	if strings.HasPrefix(prefix, "/") {
		return fmt.Errorf("%w: %q has a leading slash", ErrInvalidPrefix, prefix)
	}
	if strings.HasSuffix(prefix, "/") {
		return fmt.Errorf("%w: %q has a trailing slash", ErrInvalidPrefix, prefix)
	}
	for seg := range strings.SplitSeq(prefix, "/") {
		if seg == "" {
			return fmt.Errorf("%w: %q has an empty segment", ErrInvalidPrefix, prefix)
		}
		if seg == "." || seg == ".." {
			return fmt.Errorf("%w: %q has a %q segment", ErrInvalidPrefix, prefix, seg)
		}
		if strings.ContainsRune(seg, '\\') {
			return fmt.Errorf("%w: %q contains a backslash", ErrInvalidPrefix, prefix)
		}
	}
	return nil
}

// JoinKey joins a validated prefix and a protocol key with one slash
// (architecture section 9.1). The object-store adapter owns this join; a
// nonempty prefix and a protocol key never produce an escaped key.
func JoinKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "/" + key
}
