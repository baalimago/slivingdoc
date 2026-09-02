package workspace

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"strings"
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
	writeLengthPrefixed(h, canonicalPath)
	writeIdentity(h, id)
	return hex.EncodeToString(h.Sum(nil))
}

// sharedCacheDigestLen is the hexadecimal length of the identity digest in a
// shared cache directory name. The name only organizes entries — every entry
// is verified against its manifest descriptor on read — so 16 hex characters
// disambiguate identities without pushing the readable components out.
const sharedCacheDigestLen = 16

// SharedCacheDirName returns the shared pack-cache directory name of one
// storage identity: the sanitized bucket and prefix for human recognition,
// then a truncated identity digest for disambiguation. The digest is
// DerivedKey without the canonical visible path, so every workspace of one
// notebook maps to one directory while distinct prefixes, buckets, or
// endpoints never share. The name carries no correctness weight: cache
// entries verify against the manifest descriptor on every read.
func SharedCacheDirName(id Identity) string {
	h := sha256.New()
	writeIdentity(h, id)
	digest := hex.EncodeToString(h.Sum(nil))[:sharedCacheDigestLen]
	return sanitizeCacheComponent(id.Bucket) + "-" + sanitizeCacheComponent(id.Prefix) + "-" + digest
}

// sanitizeCacheComponentMax bounds one readable component of a shared cache
// directory name, keeping the whole name well below common path limits.
const sanitizeCacheComponentMax = 32

// sanitizeCacheComponent maps one identity component to a filesystem-safe
// lowercase form: letters and digits pass through, every other byte becomes
// one hyphen, and the result is bounded. The digest suffix carries the
// disambiguation, so lossy sanitization is safe.
func sanitizeCacheComponent(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if b.Len() >= sanitizeCacheComponentMax {
			break
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// writeLengthPrefixed writes one length-prefixed component to the digest, so
// no concatenation of inputs can collide under ambiguous separators.
func writeLengthPrefixed(h hash.Hash, s string) {
	var lenBuf [8]byte
	binary.BigEndian.PutUint64(lenBuf[:], uint64(len(s)))
	h.Write(lenBuf[:])
	h.Write([]byte(s))
}

// writeIdentity writes every storage-identity component to the digest in
// the canonical order shared by DerivedKey and SharedCacheDirName.
func writeIdentity(h hash.Hash, id Identity) {
	writeLengthPrefixed(h, id.Endpoint)
	writeLengthPrefixed(h, id.Region)
	writeLengthPrefixed(h, id.Bucket)
	writeLengthPrefixed(h, id.Prefix)
	var versionBuf [8]byte
	binary.BigEndian.PutUint64(versionBuf[:], uint64(id.ManifestVersion))
	h.Write(versionBuf[:])
}
