// Package git defines the Go-facing contract for the native Git engine.
//
// Only internal/git2 implements this contract with real libgit2. Higher
// packages consume Engine and Repository through this seam, so tests can
// substitute a deterministic fake without native calls. The seam is a
// contract, not a second Git implementation.
package git

import (
	"fmt"
	"io"
	"strings"
)

// Engine is the lifecycle and capability view of the native Git engine.
//
// Open must succeed before any other call, and the caller owns Close. A
// runtime libgit2 release that differs from the pinned release refuses to
// open with a VersionMismatchError.
type Engine interface {
	Open() error
	Close() error
	Version() (string, error)
	Features() (Features, error)
	CreateRepo(path string) (Repository, error)
	OpenRepo(path string) (Repository, error)
}

// Repository is an open local repository. All methods are safe for
// concurrent use; Close releases the native resources deterministically.
//
// The interface exposes only narrow native operations. Policy — snapshot
// validation, deterministic tree building, conflict structuring, pack
// planning, and history validation — lives in the functions of this package
// (BuildTree, ReadSnapshot, Merge, ExportIncrement, and so on).
type Repository interface {
	WriteBlob(data []byte) (OID, error)
	// ReadBlob returns the raw object bytes for the given ID. Policy
	// callers pass IDs of blob-mode tree entries; the engine does not
	// interpret the object type.
	ReadBlob(id OID) ([]byte, error)
	// ReadCommit returns the tree, parent chain, and message of a commit.
	ReadCommit(id OID) (Commit, error)
	// ReadTree returns the entries of a tree in Git tree order.
	ReadTree(id OID) ([]TreeEntry, error)
	// WriteTree writes a tree from its entries. Entries must use modes
	// accepted by the native boundary; the boundary sorts defensively.
	WriteTree(entries []TreeEntry) (OID, error)
	// CreateCommit writes a commit with the fixed slivingdoc identity and
	// the spec time in UTC with one-second precision and offset zero.
	CreateCommit(spec CommitSpec) (OID, error)
	// MergeTrees merges three explicit trees without history-based
	// merge-base discovery. Tree in the result is the zero OID when the
	// index contains conflicts.
	MergeTrees(base, local, remote OID) (MergeIndex, error)
	// MergeFile performs a file-level three-way merge with the exact
	// `local` and `remote` conflict-marker labels. An empty side stands
	// for a deleted file.
	MergeFile(base, local, remote []byte) (MergeFileResult, error)
	// WritePack writes one complete pack containing exactly the listed
	// objects and returns the number of objects it contains. The pack has
	// no external delta bases.
	WritePack(objects []OID, w io.Writer) (int, error)
	// ImportPack validates and imports a complete pack into the
	// repository object store.
	ImportPack(data []byte) error
	// MarkShallow records a commit as a shallow history boundary: its
	// parents can be absent.
	MarkShallow(oid OID) error
	Close() error
}

// OID is a Git object identifier (SHA-1).
type OID [20]byte

// String returns the lowercase 40-character hexadecimal form.
func (o OID) String() string {
	const hex = "0123456789abcdef"
	b := make([]byte, 0, 40)
	for _, c := range o {
		b = append(b, hex[c>>4], hex[c&0x0f])
	}
	return string(b)
}

// Features is the compiled feature set of the native engine.
type Features struct {
	Threads       bool // thread-aware and safe from multiple threads
	HTTPS         bool // HTTPS remotes
	SSH           bool // SSH remotes
	NSEC          bool // sub-second index timestamp resolution
	HTTPParser    bool // HTTP parsing (always available)
	Regex         bool // regular expression support (always available)
	I18N          bool // filename translation support
	AuthNTLM      bool // NTLM authentication over HTTPS
	AuthNegotiate bool // Kerberos (SPNEGO) authentication over HTTPS
	Compression   bool // zlib support (always available)
	SHA1          bool // SHA-1 object support
	SHA256        bool // SHA-256 object support
}

// featureNames lists the display names in the same order as the struct
// fields, so Features.String stays stable.
var featureNames = []string{
	"threads",
	"https",
	"ssh",
	"nsec",
	"http-parser",
	"regex",
	"i18n",
	"auth-ntlm",
	"auth-negotiate",
	"compression",
	"sha1",
	"sha256",
}

// String returns the comma-separated list of enabled features.
func (f Features) String() string {
	enabled := []bool{
		f.Threads, f.HTTPS, f.SSH, f.NSEC, f.HTTPParser, f.Regex,
		f.I18N, f.AuthNTLM, f.AuthNegotiate, f.Compression, f.SHA1, f.SHA256,
	}
	names := make([]string, 0, 12)
	for i, on := range enabled {
		if on {
			names = append(names, featureNames[i])
		}
	}
	return strings.Join(names, ", ")
}

// FeaturesFromMask decodes a libgit2 feature bitmask into Features.
func FeaturesFromMask(mask uint32) Features {
	return Features{
		Threads:       mask&(1<<0) != 0,
		HTTPS:         mask&(1<<1) != 0,
		SSH:           mask&(1<<2) != 0,
		NSEC:          mask&(1<<3) != 0,
		HTTPParser:    mask&(1<<4) != 0,
		Regex:         mask&(1<<5) != 0,
		I18N:          mask&(1<<6) != 0,
		AuthNTLM:      mask&(1<<7) != 0,
		AuthNegotiate: mask&(1<<8) != 0,
		Compression:   mask&(1<<9) != 0,
		SHA1:          mask&(1<<10) != 0,
		SHA256:        mask&(1<<11) != 0,
	}
}

// VersionMismatchError reports a runtime libgit2 release that differs from
// the pinned release the binary was built and tested against.
type VersionMismatchError struct {
	Pinned  string
	Runtime string
}

func (e *VersionMismatchError) Error() string {
	return fmt.Sprintf("git: runtime libgit2 %s does not match pinned %s", e.Runtime, e.Pinned)
}

// NativeError preserves a failed libgit2 operation together with the native
// error detail. The detail is copied at failure time, so the error stays
// valid after the native handles are released.
type NativeError struct {
	Op      string
	Class   int
	Message string
}

func (e *NativeError) Error() string {
	if e.Message == "" {
		return "git: " + e.Op + " failed"
	}
	return "git: " + e.Op + ": " + e.Message
}
