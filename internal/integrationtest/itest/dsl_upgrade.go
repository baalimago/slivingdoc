package itest

import (
	"context"
	"testing"

	"github.com/baalimago/slivingdoc/internal/storage"
)

// currentETag returns the ETag of the stored manifest.
func CurrentETag(t *testing.T, h *Harness) storage.ETag {
	t.Helper()
	rc, info, err := h.Raw().ReadObject(context.Background(), storage.CurrentKey)
	if err != nil {
		t.Fatalf("read current: %v", err)
	}
	_ = rc.Close()
	return info.ETag
}
