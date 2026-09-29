package storage

import (
	"fmt"
	"strconv"
)

// Metadata field names (architecture/storage.md). S3 carries them as user
// metadata without the x-amz-meta- prefix; the hosted storage API carries
// them as HTTP headers of the same names.
const (
	MetaSHA256     = "slivingdoc-sha256"
	MetaSize       = "slivingdoc-size"
	MetaKind       = "slivingdoc-kind"
	MetaGeneration = "slivingdoc-generation"
)

// Fields renders the metadata as its four named string fields.
func (m Metadata) Fields() map[string]string {
	return map[string]string{
		MetaSHA256:     m.SHA256.String(),
		MetaSize:       strconv.FormatUint(m.Size, 10),
		MetaKind:       string(m.Kind),
		MetaGeneration: strconv.FormatUint(m.Generation, 10),
	}
}

// ParseMetadata decodes the named metadata fields. Absent fields leave the
// zero value; a present but malformed field is an error because the object
// claims to be a protocol pack it cannot describe.
func ParseMetadata(fields map[string]string) (Metadata, error) {
	var meta Metadata
	if v, ok := fields[MetaSHA256]; ok {
		h, err := ParseSHA256(v)
		if err != nil {
			return meta, fmt.Errorf("storage: metadata %s: %w", MetaSHA256, err)
		}
		meta.SHA256 = h
	}
	if v, ok := fields[MetaSize]; ok {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return meta, fmt.Errorf("storage: metadata %s: %w", MetaSize, err)
		}
		meta.Size = n
	}
	if v, ok := fields[MetaKind]; ok {
		if !PackKind(v).Valid() {
			return meta, fmt.Errorf("storage: metadata %s: invalid kind %q", MetaKind, v)
		}
		meta.Kind = PackKind(v)
	}
	if v, ok := fields[MetaGeneration]; ok {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return meta, fmt.Errorf("storage: metadata %s: %w", MetaGeneration, err)
		}
		meta.Generation = n
	}
	return meta, nil
}
