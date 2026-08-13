package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// UploadUnique stores immutable pack bytes at a uniquely-owned protocol key
// and writes the slivingdoc metadata. A transport failure is ambiguous
// (architecture section 15): the request may have landed before the response
// was lost. UploadUnique resolves the ambiguity by reading the unique key
// back and proving its bytes:
//
//   - bytes match the declared digest and size: the upload landed, continue;
//   - the key is absent: the upload never landed, report a transport failure;
//   - bytes differ: report a storage-integrity error and never overwrite.
//
// Existing bytes at the unique key are reused only through that read-back.
// The streamed GET proves the size and SHA-256. Metadata alone is never
// proof (architecture section 9.1).
func UploadUnique(ctx context.Context, s ObjectStore, key Key, r io.Reader, meta Metadata) error {
	if !key.Kind.Valid() {
		return fmt.Errorf("%w: upload key has invalid kind %q", ErrIntegrity, key.Kind)
	}
	if err := s.PutObject(ctx, key.String(), r, meta); err != nil {
		if !errors.Is(err, ErrTransport) {
			return fmt.Errorf("storage: upload %s: %w", key, err)
		}
		switch err := VerifyObject(ctx, s, key.String(), meta.SHA256, meta.Size); {
		case err == nil:
			// The response was lost after the bytes were accepted.
			return nil
		case errors.Is(err, ErrIntegrity):
			return fmt.Errorf("storage: upload %s: %w", key, err)
		default:
			return fmt.Errorf("storage: upload %s: %w", key, ErrTransport)
		}
	}
	return nil
}

// VerifyObject streams an object and proves that its complete bytes match
// the declared size and SHA-256. Metadata alone is not proof, and an S3
// ETag is never a content digest. It returns ErrIntegrity for a size or
// digest mismatch (including a download that ends before the declared
// size), ErrNotFound when the object is absent, and the wrapped transport
// error when the read itself fails.
func VerifyObject(ctx context.Context, s ObjectStore, key string, want SHA256, wantSize uint64) error {
	rc, _, err := s.ReadObject(ctx, key)
	if err != nil {
		return err
	}
	defer rc.Close()
	h := sha256.New()
	n, err := io.Copy(h, rc)
	if err != nil {
		return fmt.Errorf("storage: verify %s: %w", key, ErrTransport)
	}
	if uint64(n) != wantSize || !bytes.Equal(h.Sum(nil), want[:]) {
		return fmt.Errorf("storage: verify %s: %w: got %d bytes with digest %s, want %d bytes with digest %s",
			key, ErrIntegrity, n, hex.EncodeToString(h.Sum(nil)), wantSize, want.String())
	}
	return nil
}
