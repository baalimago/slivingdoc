package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/strictjson"
)

// The JSON field names of the file (architecture/config.md).
const (
	fieldVersion   = "version"
	fieldEntries   = "entries"
	fieldDefaults  = "defaults"
	fieldPath      = "path"
	fieldTarget    = "target"
	fieldKind      = "kind"
	fieldEndpoint  = "endpoint"
	fieldRegion    = "region"
	fieldProfile   = "profile"
	fieldName      = "name"
	fieldPrefix    = "prefix"
	fieldPathStyle = "pathStyle"
	// fieldSpace is version 1's name for Target.Name.
	fieldSpace = "space"
)

type fileJSON struct {
	Version  int          `json:"version"`
	Entries  []entryJSON  `json:"entries"`
	Defaults []targetJSON `json:"defaults"`
}

type entryJSON struct {
	Path   string     `json:"path"`
	Target targetJSON `json:"target"`
}

type targetJSON struct {
	Kind      string `json:"kind"`
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Profile   string `json:"profile"`
	Name      string `json:"name"`
	Prefix    string `json:"prefix"`
	PathStyle bool   `json:"pathStyle"`
}

// encode writes the set as compact JSON in the normative field order,
// with HTML escaping disabled and no trailing newline, the same shape
// the workspace private-state record uses (internal/workspace). The
// defaults list is written even when it is empty, so every version-2 file
// has the same shape.
func encode(s Set) ([]byte, error) {
	out := fileJSON{
		Version:  FormatVersion,
		Entries:  make([]entryJSON, 0, len(s.entries)),
		Defaults: make([]targetJSON, 0, len(s.defaults)),
	}
	for _, e := range s.entries {
		out.Entries = append(out.Entries, entryJSON{Path: e.Path, Target: encodeTarget(e.Target)})
	}
	for _, t := range s.defaults {
		out.Defaults = append(out.Defaults, encodeTarget(t))
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func encodeTarget(t Target) targetJSON {
	return targetJSON{
		Kind:      string(t.Kind),
		Endpoint:  t.Endpoint,
		Region:    t.Region,
		Profile:   t.Profile,
		Name:      t.Name,
		Prefix:    t.Prefix,
		PathStyle: t.PathStyle,
	}
}

// decode reads the file strictly, like every other slivingdoc protocol
// record: unknown, duplicate, missing and null fields are refused at every
// level. Version 1 is read through its own path and never rewritten; a
// version this build does not read is a versionError, read no further.
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
	switch ver.Num {
	case 1:
		return decodeV1(root)
	case FormatVersion:
		return decodeV2(root)
	default:
		return Set{}, &versionError{got: ver.Num}
	}
}

// decodeV1 reads the version-1 shape: one hosted space and a prefix per
// directory, and no defaults. The target reads as a hosted target with an
// empty endpoint, because the selected login supplies the site when the
// record is resolved.
func decodeV1(root strictjson.Value) (Set, error) {
	if err := root.RejectUnknown(fieldVersion, fieldEntries); err != nil {
		return Set{}, err
	}
	return decodeEntries(root, decodeV1Entry)
}

func decodeV1Entry(v strictjson.Value) (Entry, error) { return decodeEntry(v, decodeV1Target) }

// decodeV1Target reads version 1's remembered notebook. It names the two
// version-1 fields only, so a token or a key beside them is an unknown
// field and the whole file is malformed.
func decodeV1Target(v strictjson.Value) (Target, error) {
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
	prefix, err := decodePrefix(v)
	if err != nil {
		return Target{}, err
	}
	return Target{Kind: KindHosted, Name: space, Prefix: prefix}, nil
}

// decodeV2 reads the backend-aware shape: entries and one target per store
// key in defaults. A hosted default is refused, because a hosted default
// lives in credentials.json and two sources must not disagree.
func decodeV2(root strictjson.Value) (Set, error) {
	if err := root.RejectUnknown(fieldVersion, fieldEntries, fieldDefaults); err != nil {
		return Set{}, err
	}
	set, err := decodeEntries(root, decodeV2Entry)
	if err != nil {
		return Set{}, err
	}
	defaults, ok := root.Field(fieldDefaults)
	if !ok || defaults.Kind != strictjson.Array {
		return Set{}, errors.New("missing defaults array")
	}
	for i, item := range defaults.Arr {
		target, err := decodeTarget(item)
		if err != nil {
			return Set{}, fmt.Errorf("defaults[%d]: %w", i, err)
		}
		if target.Kind == KindHosted {
			return Set{}, fmt.Errorf("defaults[%d]: a hosted default belongs in credentials.json", i)
		}
		if _, dup := set.Default(target.Key()); dup {
			return Set{}, fmt.Errorf("defaults[%d]: a second default for the same store", i)
		}
		set.defaults = append(set.defaults, target)
	}
	return set, nil
}

func decodeV2Entry(v strictjson.Value) (Entry, error) { return decodeEntry(v, decodeTarget) }

// decodeEntry reads one entry through the target decoder of its version.
func decodeEntry(v strictjson.Value, targetOf func(strictjson.Value) (Target, error)) (Entry, error) {
	if v.Kind != strictjson.Object {
		return Entry{}, errors.New("not an object")
	}
	if err := v.RejectUnknown(fieldPath, fieldTarget); err != nil {
		return Entry{}, err
	}
	path, err := decodePath(v)
	if err != nil {
		return Entry{}, err
	}
	t, err := targetField(v)
	if err != nil {
		return Entry{}, err
	}
	target, err := targetOf(t)
	if err != nil {
		return Entry{}, fmt.Errorf("%s: %w", fieldTarget, err)
	}
	return Entry{Path: path, Target: target}, nil
}

// decodeTarget reads the backend-aware target. It names the backend fields
// only, so a token or a key beside them is an unknown field and the whole
// file is malformed. A field is either meaningful for the kind or absent:
// a hosted target that carries an S3 field is refused, never silently
// ignored.
func decodeTarget(v strictjson.Value) (Target, error) {
	if v.Kind != strictjson.Object {
		return Target{}, errors.New("not an object")
	}
	if err := v.RejectUnknown(fieldKind, fieldEndpoint, fieldRegion, fieldProfile,
		fieldName, fieldPrefix, fieldPathStyle); err != nil {
		return Target{}, err
	}
	kindName, err := stringField(v, fieldKind)
	if err != nil {
		return Target{}, err
	}
	kind := Kind(kindName)
	if kind != KindHosted && kind != KindS3 {
		return Target{}, fmt.Errorf("field %q is not hosted or s3", fieldKind)
	}
	name, err := stringField(v, fieldName)
	if err != nil {
		return Target{}, err
	}
	if name == "" {
		return Target{}, fmt.Errorf("field %q is empty", fieldName)
	}
	prefix, err := decodePrefix(v)
	if err != nil {
		return Target{}, err
	}
	endpoint, err := stringField(v, fieldEndpoint)
	if err != nil {
		return Target{}, err
	}
	region, err := stringField(v, fieldRegion)
	if err != nil {
		return Target{}, err
	}
	profile, err := stringField(v, fieldProfile)
	if err != nil {
		return Target{}, err
	}
	pathStyle, err := boolField(v, fieldPathStyle)
	if err != nil {
		return Target{}, err
	}
	target := Target{
		Kind:      kind,
		Endpoint:  endpoint,
		Region:    region,
		Profile:   profile,
		Name:      name,
		Prefix:    prefix,
		PathStyle: pathStyle,
	}
	if target.Kind == KindHosted && (target.Region != "" || target.Profile != "" || target.PathStyle) {
		return Target{}, errors.New("a hosted target names no region, profile or path style")
	}
	return target, nil
}

// decodeEntries reads the entries array through one version's entry
// decoder, refusing two entries for one directory.
func decodeEntries(root strictjson.Value, entryOf func(strictjson.Value) (Entry, error)) (Set, error) {
	list, ok := root.Field(fieldEntries)
	if !ok || list.Kind != strictjson.Array {
		return Set{}, errors.New("missing entries array")
	}
	var set Set
	for i, item := range list.Arr {
		entry, err := entryOf(item)
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

func decodePath(v strictjson.Value) (string, error) {
	path, err := stringField(v, fieldPath)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", fmt.Errorf("field %q is empty", fieldPath)
	}
	return path, nil
}

func decodePrefix(v strictjson.Value) (string, error) {
	prefix, err := stringField(v, fieldPrefix)
	if err != nil {
		return "", err
	}
	if err := storage.ValidatePrefix(prefix); err != nil {
		return "", fmt.Errorf("field %q: %w", fieldPrefix, err)
	}
	return prefix, nil
}

func targetField(v strictjson.Value) (strictjson.Value, error) {
	t, ok := v.Field(fieldTarget)
	if !ok {
		return strictjson.Value{}, fmt.Errorf("missing field %q", fieldTarget)
	}
	return t, nil
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

func boolField(v strictjson.Value, name string) (bool, error) {
	f, ok := v.Field(name)
	if !ok {
		return false, fmt.Errorf("missing field %q", name)
	}
	if f.Kind != strictjson.Bool {
		return false, fmt.Errorf("field %q is not a bool", name)
	}
	return f.B, nil
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
