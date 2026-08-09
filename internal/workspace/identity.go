package workspace

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// ManifestVersion is the protocol manifest version that is part of the
// notebook storage identity (architecture section 9.2). The identity binds
// one private directory to one visible path and one remote notebook; a
// different manifest protocol is a different remote.
const ManifestVersion = 1

// Identity is the notebook storage identity (architecture section 7.2):
// the normalized S3 endpoint, region, bucket, prefix, and manifest version
// the notebook is bound to. The endpoint must already be in the normalized
// configuration form (architecture section 17): lowercased scheme and host,
// no trailing slash, no user information. Configuration owns that
// normalization; DerivedKey consumes it as given.
type Identity struct {
	Endpoint string
	Region   string
	Bucket   string
	Prefix   string
	// ManifestVersion is the storage manifest protocol version. The
	// workspace constant ManifestVersion is the only supported value.
	ManifestVersion int
}

// DerivedKey returns the private-directory key: the lowercase hexadecimal
// SHA-256 of a length-prefixed encoding of the canonical visible path and
// the storage identity (architecture section 7.2). Every component carries
// its own length prefix, so no concatenation of inputs can collide under
// ambiguous separators. The digest does not expose the caller's path.
func DerivedKey(canonicalPath string, id Identity) string {
	h := sha256.New()
	writeString := func(s string) {
		var lenBuf [8]byte
		binary.BigEndian.PutUint64(lenBuf[:], uint64(len(s)))
		h.Write(lenBuf[:])
		h.Write([]byte(s))
	}
	writeString(canonicalPath)
	writeString(id.Endpoint)
	writeString(id.Region)
	writeString(id.Bucket)
	writeString(id.Prefix)
	var versionBuf [8]byte
	binary.BigEndian.PutUint64(versionBuf[:], uint64(id.ManifestVersion))
	h.Write(versionBuf[:])
	return hex.EncodeToString(h.Sum(nil))
}
