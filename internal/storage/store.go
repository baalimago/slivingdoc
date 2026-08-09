package storage

import (
	"context"
	"errors"
	"io"
)

// Semantic errors returned by every ObjectStore implementation. The
// notebook layer maps these categories to the stable MCP error taxonomy;
// the text of an error can change, its category cannot. AWS SDK types never
// cross this boundary.
var (
	// ErrNotFound reports that an object does not exist. Reading the
	// current manifest with ErrNotFound is the implicit empty-notebook
	// state at generation 0.
	ErrNotFound = errors.New("storage: object not found")
	// ErrPreconditionFailed reports that a conditional write lost the
	// race: another writer created or replaced the object first.
	ErrPreconditionFailed = errors.New("storage: precondition failed")
	// ErrTransport reports that an object-store operation failed without
	// a known accepted result. The operation may or may not have landed.
	ErrTransport = errors.New("storage: transport failure")
	// ErrIncompatible reports that a store failed the startup capability
	// probe and cannot serve the protocol.
	ErrIncompatible = errors.New("storage: incompatible store")
)

// ETag is an opaque concurrency token for conditional replacement. It is
// never a content digest (architecture section 9.3): pack integrity comes
// from the descriptor SHA-256 and size.
type ETag string

// Metadata is the slivingdoc user metadata written with every pack upload
// (architecture section 9.1): the pack SHA-256, byte size, kind, and target
// or through generation. The manifest descriptor is authoritative; metadata
// only diagnoses and resumes uploads.
type Metadata struct {
	SHA256     SHA256
	Size       uint64
	Kind       PackKind
	Generation uint64
}

// ObjectInfo is the metadata of one stored object returned with a streamed
// read. Size is the physical object size; Meta carries the slivingdoc
// upload metadata when the object was written by the protocol.
type ObjectInfo struct {
	Size int64
	ETag ETag
	Meta Metadata
}

// ObjectStore is the smallest semantic object-store boundary consumed by
// notebook storage. Implementations own a configured S3 prefix: methods
// take protocol keys relative to that prefix (architecture section 9.1) and
// the store joins them. Implementations must be safe for concurrent use.
//
// The interface expresses reads with metadata, uniquely-owned immutable
// uploads, conditional creation and replacement, and list and delete
// operations for later cleanup. Correctness never uses LIST to discover
// accepted state: the current manifest is the only authority.
type ObjectStore interface {
	// ReadObject streams an object's bytes and returns its metadata. The
	// caller owns the returned reader and must close it. ErrNotFound
	// reports an absent object.
	ReadObject(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error)
	// PutObject stores immutable bytes at a uniquely-owned key, streaming
	// the body, and writes the slivingdoc metadata. Large bodies may use
	// multipart upload; a failed upload aborts it. A transport error is
	// ambiguous: the bytes may have been accepted.
	PutObject(ctx context.Context, key string, r io.Reader, meta Metadata) error
	// CreateObject conditionally creates an object (If-None-Match: *).
	// ErrPreconditionFailed reports that the key already exists.
	CreateObject(ctx context.Context, key string, data []byte) (ETag, error)
	// ReplaceObject conditionally replaces an object by its ETag
	// (If-Match). ErrPreconditionFailed reports that the ETag is stale
	// and the object was not mutated.
	ReplaceObject(ctx context.Context, key string, etag ETag, data []byte) (ETag, error)
	// ListObjects invokes fn for every object key under the protocol
	// prefix, following every continuation. A non-nil error from fn
	// aborts the listing. The interface returns protocol keys so cleanup
	// can feed them back into DeleteObjects.
	ListObjects(ctx context.Context, prefix string, fn func(key string) error) error
	// DeleteObjects deletes a batch of keys in bounded requests. Deleting
	// an absent key is not an error. A partial or failed batch returns
	// ErrTransport and must not have mutated unrelated objects.
	DeleteObjects(ctx context.Context, keys []string) error
}
