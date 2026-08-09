package git

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// Path limits from architecture section 7.1. Limits are byte counts of the
// UTF-8 encoding, not rune counts.
const (
	maxPathBytes    = 4096
	maxSegmentBytes = 255
)

// invalidSegmentChars are the characters rejected inside a path segment on
// every host. Slash is the separator and never appears inside a segment.
const invalidSegmentChars = `\:*?"<>|`

// ValidatePath validates one normalized internal path against the
// architecture 7.1 rules: valid UTF-8 in NFC form, slash-separated relative
// segments, bounded lengths, no control characters, no reserved characters,
// no trailing space or dot, no `.`/`..`/`.git` segments, and no Windows
// device names. A path must not start or end with a slash and must not
// contain an empty segment.
func ValidatePath(path string) error {
	switch {
	case path == "":
		return fmt.Errorf("invalid path: empty path")
	case len(path) > maxPathBytes:
		return fmt.Errorf("invalid path %q: exceeds %d bytes", path, maxPathBytes)
	case !utf8.ValidString(path):
		return fmt.Errorf("invalid path %q: not valid UTF-8", path)
	case !norm.NFC.IsNormalString(path):
		return fmt.Errorf("invalid path %q: not in Unicode NFC form", path)
	case strings.HasPrefix(path, "/"), strings.HasSuffix(path, "/"):
		return fmt.Errorf("invalid path %q: must not start or end with a slash", path)
	}
	for seg := range strings.SplitSeq(path, "/") {
		if err := validateSegment(seg, path); err != nil {
			return err
		}
	}
	return nil
}

func validateSegment(seg, path string) error {
	switch {
	case seg == "":
		return fmt.Errorf("invalid path %q: empty segment", path)
	case seg == "." || seg == "..":
		return fmt.Errorf("invalid path %q: %q segment is not allowed", path, seg)
	case strings.EqualFold(seg, ".git"):
		return fmt.Errorf("invalid path %q: .git segment is not allowed", path)
	case len(seg) > maxSegmentBytes:
		return fmt.Errorf("invalid path %q: segment %q exceeds %d bytes", path, seg, maxSegmentBytes)
	case strings.HasSuffix(seg, " ") || strings.HasSuffix(seg, "."):
		return fmt.Errorf("invalid path %q: segment %q must not end in a space or dot", path, seg)
	case isWindowsDeviceName(seg):
		return fmt.Errorf("invalid path %q: segment %q is a reserved device name", path, seg)
	}
	for _, r := range seg {
		switch {
		case unicode.IsControl(r):
			return fmt.Errorf("invalid path %q: segment %q contains a control character", path, seg)
		case strings.ContainsRune(invalidSegmentChars, r):
			return fmt.Errorf("invalid path %q: segment %q contains a reserved character", path, seg)
		}
	}
	return nil
}

// isWindowsDeviceName reports whether a segment is reserved by Windows
// (CON, PRN, AUX, NUL, COM1..COM9, LPT1..LPT9, with or without an
// extension). The engine rejects them on every host so one accepted
// notebook stays portable across supported hosts.
func isWindowsDeviceName(seg string) bool {
	base := seg
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	upper := strings.ToUpper(base)
	switch upper {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(upper) == 4 {
		switch {
		case strings.HasPrefix(upper, "COM"), strings.HasPrefix(upper, "LPT"):
			return '1' <= upper[3] && upper[3] <= '9'
		}
	}
	return false
}

// ValidateContent rejects content that is not valid UTF-8 text without the
// U+0000 character (architecture section 7.1). Bytes and line endings are
// preserved; nothing is normalized.
func ValidateContent(data []byte) error {
	switch {
	case !utf8.Valid(data):
		return fmt.Errorf("invalid content: not valid UTF-8")
	case bytes.IndexByte(data, 0) >= 0:
		return fmt.Errorf("invalid content: contains U+0000")
	}
	return nil
}

// ValidateSnapshot validates every path and content of a snapshot and
// rejects paths that collide under Unicode case folding. The check runs
// against the full snapshot because collisions only exist between two
// different paths.
func ValidateSnapshot(snap Snapshot) error {
	fold := cases.Fold()
	seen := make(map[string]string, len(snap.Files)) // folded path -> first path
	for _, f := range snap.Files {
		if err := ValidatePath(f.Path); err != nil {
			return err
		}
		if err := ValidateContent(f.Data); err != nil {
			return fmt.Errorf("invalid snapshot: path %q: %w", f.Path, err)
		}
		folded := fold.String(f.Path)
		if first, ok := seen[folded]; ok {
			if first != f.Path {
				return fmt.Errorf("invalid snapshot: paths %q and %q collide under Unicode case folding", first, f.Path)
			}
			return fmt.Errorf("invalid snapshot: duplicate path %q", f.Path)
		}
		seen[folded] = f.Path
	}
	return nil
}
