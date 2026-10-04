package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/baalimago/slivingdoc/internal/strictjson"
)

// The JSON field names of the file (architecture/config.md).
const (
	fieldVersion = "version"
	fieldEntries = "entries"
	fieldPath    = "path"
	fieldTarget  = "target"
	fieldSpace   = "space"
	fieldPrefix  = "prefix"
)

type fileJSON struct {
	Version int         `json:"version"`
	Entries []entryJSON `json:"entries"`
}

type entryJSON struct {
	Path   string     `json:"path"`
	Target targetJSON `json:"target"`
}

type targetJSON struct {
	Space  string `json:"space"`
	Prefix string `json:"prefix"`
}

// encode writes the set as compact JSON in the normative field order,
// with HTML escaping disabled and no trailing newline, the same shape
// the workspace private-state record uses (internal/workspace).
func encode(s Set) ([]byte, error) {
	out := fileJSON{Version: FormatVersion, Entries: make([]entryJSON, 0, len(s.entries))}
	for _, e := range s.entries {
		out.Entries = append(out.Entries, entryJSON{
			Path:   e.Path,
			Target: targetJSON{Space: e.Target.Space, Prefix: e.Target.Prefix},
		})
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// decode reads the file strictly, like every other slivingdoc protocol
// record: unknown, duplicate, missing and null fields are refused, and a
// version other than FormatVersion is a versionError, never rewritten.
func decode(data []byte) (Set, error) {
	root, err := strictjson.Parse(data)
	if err != nil {
		return Set{}, err
	}
	if root.Kind != strictjson.Object {
		return Set{}, errors.New("the top level is not an object")
	}
	ver, ok := root.Field(fieldVersion)
	if !ok || ver.Kind != strictjson.Number {
		return Set{}, errors.New("missing numeric version")
	}
	if ver.Num != FormatVersion {
		return Set{}, &versionError{got: ver.Num}
	}
	if err := root.RejectUnknown(fieldVersion, fieldEntries); err != nil {
		return Set{}, err
	}
	list, ok := root.Field(fieldEntries)
	if !ok || list.Kind != strictjson.Array {
		return Set{}, errors.New("missing entries array")
	}
	var set Set
	for i, item := range list.Arr {
		entry, err := decodeEntry(item)
		if err != nil {
			return Set{}, fmt.Errorf("entries[%d]: %w", i, err)
		}
		if _, dup := set.Lookup(entry.Path); dup {
			return Set{}, fmt.Errorf("entries[%d]: a second entry for the same directory", i)
		}
		set.entries = append(set.entries, entry)
	}
	return set, nil
}

func decodeEntry(v strictjson.Value) (Entry, error) {
	if v.Kind != strictjson.Object {
		return Entry{}, errors.New("not an object")
	}
	if err := v.RejectUnknown(fieldPath, fieldTarget); err != nil {
		return Entry{}, err
	}
	path, err := stringField(v, fieldPath)
	if err != nil {
		return Entry{}, err
	}
	if path == "" {
		return Entry{}, fmt.Errorf("field %q is empty", fieldPath)
	}
	t, ok := v.Field(fieldTarget)
	if !ok {
		return Entry{}, fmt.Errorf("missing field %q", fieldTarget)
	}
	target, err := decodeTarget(t)
	if err != nil {
		return Entry{}, fmt.Errorf("%s: %w", fieldTarget, err)
	}
	return Entry{Path: path, Target: target}, nil
}

// decodeTarget reads the remembered notebook. It names the two fields
// only: an unknown field, a token among them included, is a malformed
// file, so no credential can be stored beside the space.
func decodeTarget(v strictjson.Value) (Target, error) {
	if v.Kind != strictjson.Object {
		return Target{}, errors.New("not an object")
	}
	if err := v.RejectUnknown(fieldSpace, fieldPrefix); err != nil {
		return Target{}, err
	}
	space, err := stringField(v, fieldSpace)
	if err != nil {
		return Target{}, err
	}
	if space == "" {
		return Target{}, fmt.Errorf("field %q is empty", fieldSpace)
	}
	prefix, err := stringField(v, fieldPrefix)
	if err != nil {
		return Target{}, err
	}
	return Target{Space: space, Prefix: prefix}, nil
}

func stringField(v strictjson.Value, name string) (string, error) {
	f, ok := v.Field(name)
	if !ok {
		return "", fmt.Errorf("missing field %q", name)
	}
	if f.Kind != strictjson.String {
		return "", fmt.Errorf("field %q is not a string", name)
	}
	return f.Str, nil
}

// versionError is ErrUnsupportedVersion for a file of version got.
type versionError struct {
	got uint64
}

func (e *versionError) Error() string {
	return fmt.Sprintf("%s: it is version %d, and this slivingdoc reads version %d only", ErrUnsupportedVersion, e.got, FormatVersion)
}

func (e *versionError) Unwrap() error { return ErrUnsupportedVersion }

// withFix adds the fix for the file at path: a newer build's file is
// kept and slivingdoc updated; an older one is removed, and the
// directory's notebook is remembered again by the next successful pull
// or commit.
func (e *versionError) withFix(path string) error {
	if e.got > FormatVersion {
		return fmt.Errorf("%w; a newer slivingdoc wrote it (%s); update slivingdoc, and keep the file", e, path)
	}
	return fmt.Errorf("%w; remove %s and pull again", e, path)
}
