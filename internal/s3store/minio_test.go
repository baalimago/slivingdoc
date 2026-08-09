package s3store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/contract"
	"github.com/baalimago/slivingdoc/internal/testminio"
)

// The MinIO suite is the real-HTTP evidence for the storage protocol. It
// runs against one pinned MinIO container per go test invocation (shared
// through internal/testminio), isolates every test by configured prefix,
// and reuses the shared contract suite so the fake and the real adapter
// must agree. The tests skip only when Docker is unavailable or the
// container cannot start, and they always name the unavailable dependency;
// the CI integration job treats any skip as failure.

// TestMain terminates the shared MinIO container after the whole suite.
func TestMain(m *testing.M) {
	code := m.Run()
	testminio.Terminate()
	os.Exit(code)
}

// newMinioStore builds a Store for a fresh per-test prefix below the test
// bucket, plus the raw client for direct assertions.
func newMinioStore(t *testing.T, namespace string, opts ...Options) (*Store, *s3.Client, string) {
	t.Helper()
	suite := testminio.Ensure(t)
	prefix := suite.FreshPrefix(namespace)
	st, err := New(suite.Config(), testminio.Bucket, prefix, opts...)
	if err != nil {
		t.Fatalf("New(%q): %v", prefix, err)
	}
	return st, suite.Raw, prefix
}

// TestMinioContractSuite runs the shared storage contract suite against
// MinIO. MinIO buckets default to versioning disabled; the probe subtest
// proves the protocol starts without versioning.
func TestMinioContractSuite(t *testing.T) {
	suite := testminio.Ensure(t)
	contract.Run(t, func(t *testing.T) storage.ObjectStore {
		st, err := New(suite.Config(), testminio.Bucket, suite.FreshPrefix("contract"))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return st
	})
}

// TestMinioMultipartUpload proves that a body above the part size uploads
// through real S3 multipart (threshold 1, part size 5 MiB, body 5 MiB+1)
// and that SHA-256, size, and metadata survive the round trip.
func TestMinioMultipartUpload(t *testing.T) {
	st, raw, prefix := newMinioStore(t, "multipart", Options{
		MultipartThreshold: 1,
		MultipartPartSize:  5 << 20,
	})
	ctx := context.Background()

	body := make([]byte, 5<<20+1)
	for i := range body {
		body[i] = byte(i % 251)
	}
	id, err := storage.NewUUIDv7()
	if err != nil {
		t.Fatalf("NewUUIDv7: %v", err)
	}
	key := storage.Key{Kind: storage.KindCheckpoint, Generation: 1, ID: id}
	meta := storage.Metadata{
		SHA256:     storage.SHA256(sha256.Sum256(body)),
		Size:       uint64(len(body)),
		Kind:       key.Kind,
		Generation: key.Generation,
	}
	if err := st.PutObject(ctx, key.String(), bytes.NewReader(body), meta); err != nil {
		t.Fatalf("multipart put: %v", err)
	}

	rc, info, err := st.ReadObject(ctx, key.String())
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	got := readAll(t, rc)
	if !bytes.Equal(got, body) {
		t.Fatalf("read back %d bytes, want %d", len(got), len(body))
	}
	if info.Meta != meta {
		t.Fatalf("read metadata = %+v, want %+v", info.Meta, meta)
	}

	out, err := raw.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(testminio.Bucket),
		Key:    aws.String(storage.JoinKey(prefix, key.String())),
	})
	if err != nil {
		t.Fatalf("raw get: %v", err)
	}
	rawBody := readAll(t, out.Body)
	if !bytes.Equal(rawBody, body) {
		t.Fatalf("raw object has %d bytes, want %d", len(rawBody), len(body))
	}
	// A two-part multipart upload has an ETag of the form
	// <md5-of-part-md5s>-2; a single-PUT ETag is a plain md5. S3 returns
	// the ETag header quoted, so strip the quotes before comparing.
	etag := strings.Trim(aws.ToString(out.ETag), `"`)
	if !strings.HasSuffix(etag, "-2") {
		t.Fatalf("object ETag %q does not prove a two-part multipart upload", etag)
	}
}

// TestMinioPrefixIsolation proves that the configured prefix owns the key
// namespace: the same protocol key under two prefixes stores independent
// objects, and a raw LIST under one prefix never sees the other.
func TestMinioPrefixIsolation(t *testing.T) {
	st1, raw, prefix1 := newMinioStore(t, "iso-a")
	st2, _, _ := newMinioStore(t, "iso-b")
	ctx := context.Background()

	if _, err := st1.CreateObject(ctx, storage.CurrentKey, []byte("one")); err != nil {
		t.Fatalf("create under prefix 1: %v", err)
	}
	if _, err := st2.CreateObject(ctx, storage.CurrentKey, []byte("two")); err != nil {
		t.Fatalf("create under prefix 2: %v", err)
	}

	rc, _, err := st1.ReadObject(ctx, storage.CurrentKey)
	if err != nil {
		t.Fatalf("read prefix 1: %v", err)
	}
	if got := string(readAll(t, rc)); got != "one" {
		t.Fatalf("prefix 1 bytes = %q, want %q", got, "one")
	}
	rc, _, err = st2.ReadObject(ctx, storage.CurrentKey)
	if err != nil {
		t.Fatalf("read prefix 2: %v", err)
	}
	if got := string(readAll(t, rc)); got != "two" {
		t.Fatalf("prefix 2 bytes = %q, want %q", got, "two")
	}

	out, err := raw.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(testminio.Bucket),
		Prefix: aws.String(prefix1 + "/"),
	})
	if err != nil {
		t.Fatalf("raw list: %v", err)
	}
	var keys []string
	for _, obj := range out.Contents {
		keys = append(keys, aws.ToString(obj.Key))
	}
	if len(keys) != 1 || !strings.HasPrefix(keys[0], prefix1+"/") {
		t.Fatalf("list under prefix 1 = %v, want exactly one key below %q", keys, prefix1)
	}
}

// TestMinioConcurrentCAS races many writers that all hold the same observed
// ETag against real HTTP conditional writes: exactly one replacement wins
// and the losers get the semantic precondition failure.
func TestMinioConcurrentCAS(t *testing.T) {
	st, _, _ := newMinioStore(t, "cas")
	ctx := context.Background()
	// The seed bytes must differ from every race payload so no winning
	// write reproduces the seed ETag (the fake suite has the same rule).
	etag, err := st.CreateObject(ctx, storage.CurrentKey, []byte("seed"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	const n = 8
	start := make(chan struct{})
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := st.ReplaceObject(ctx, storage.CurrentKey, etag, fmt.Appendf(nil, "v%d", i))
			errs <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)

	wins := 0
	for err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, storage.ErrPreconditionFailed):
		default:
			t.Fatalf("unexpected replace error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("winners = %d, want exactly 1", wins)
	}
}

func readAll(t *testing.T, rc io.ReadCloser) []byte {
	t.Helper()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	return got
}
