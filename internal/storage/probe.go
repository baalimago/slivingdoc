package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// Probe proves that a store enforces the conditional-write and
// read-after-write behavior the protocol requires (architecture section
// 9.4). It uses a unique disposable probe/<uuidv7> key below the configured
// prefix, runs the exact create, stale replace, matching replace, immediate
// read, and cleanup sequence, and deletes the probe key on success and
// after any recoverable failure. Any deviation returns ErrIncompatible; an
// operational failure (create, read, or replace) also wraps its cause, so
// the startup diagnostic can name the real reason without weakening
// errors.Is against ErrIncompatible.
//
// The probe verifies If-None-Match: * (a second create fails), If-Match
// (a wrong ETag fails without mutation), and read-after-write (the
// replacement is readable immediately with a new ETag).
func Probe(ctx context.Context, s ObjectStore) error {
	id, err := NewUUIDv7()
	if err != nil {
		return fmt.Errorf("storage: probe: %w", err)
	}
	key := "probe/" + id.String()
	defer func() {
		// Best-effort cleanup on success and after any recoverable
		// failure; a denied delete must not fail startup.
		_ = s.DeleteObjects(ctx, []string{key})
	}()

	const first = "slivingdoc probe: conditional create"
	const second = "slivingdoc probe: conditional replace"

	if _, err := s.CreateObject(ctx, key, []byte(first)); err != nil {
		return fmt.Errorf("storage: probe: conditional create %s: %w: %w", key, ErrIncompatible, err)
	}
	if _, err := s.CreateObject(ctx, key, []byte(second)); !errors.Is(err, ErrPreconditionFailed) {
		return fmt.Errorf("storage: probe: If-None-Match is not enforced: %w", ErrIncompatible)
	}
	rc, info, err := s.ReadObject(ctx, key)
	if err != nil {
		return fmt.Errorf("storage: probe: read %s: %w: %w", key, ErrIncompatible, err)
	}
	got, readErr := io.ReadAll(rc)
	closeErr := rc.Close()
	if readErr != nil || closeErr != nil || string(got) != first {
		return fmt.Errorf("storage: probe: read-after-create mismatch: %w", ErrIncompatible)
	}
	if info.ETag == "" {
		return fmt.Errorf("storage: probe: create returned no etag: %w", ErrIncompatible)
	}
	if _, err := s.ReplaceObject(ctx, key, "wrong-etag", []byte(second)); !errors.Is(err, ErrPreconditionFailed) {
		return fmt.Errorf("storage: probe: If-Match is not enforced: %w", ErrIncompatible)
	}
	rc, _, err = s.ReadObject(ctx, key)
	if err != nil {
		return fmt.Errorf("storage: probe: read %s after rejected replace: %w: %w", key, ErrIncompatible, err)
	}
	got, readErr = io.ReadAll(rc)
	closeErr = rc.Close()
	if readErr != nil || closeErr != nil || string(got) != first {
		return fmt.Errorf("storage: probe: rejected replace mutated the object: %w", ErrIncompatible)
	}
	newETag, err := s.ReplaceObject(ctx, key, info.ETag, []byte(second))
	if err != nil {
		return fmt.Errorf("storage: probe: matching replace %s: %w: %w", key, ErrIncompatible, err)
	}
	if newETag == "" || newETag == info.ETag {
		return fmt.Errorf("storage: probe: replace did not produce a new etag: %w", ErrIncompatible)
	}
	rc, _, err = s.ReadObject(ctx, key)
	if err != nil {
		return fmt.Errorf("storage: probe: immediate read %s: %w: %w", key, ErrIncompatible, err)
	}
	got, readErr = io.ReadAll(rc)
	closeErr = rc.Close()
	if readErr != nil || closeErr != nil || string(got) != second {
		return fmt.Errorf("storage: probe: read-after-write mismatch: %w", ErrIncompatible)
	}
	return nil
}
