package s3store

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	smithy "github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/baalimago/slivingdoc/internal/storage"
)

// TestNewValidatesBucket proves that the bucket is required before any
// client is built.
func TestNewValidatesBucket(t *testing.T) {
	if _, err := New(context.Background(), Config{}); err == nil {
		t.Fatal("New with an empty bucket succeeded, want an error")
	}
}

// TestNewValidatesPrefix proves that the configured prefix must satisfy
// the architecture 9.1 grammar; the adapter owns the join, so a bad prefix
// must never reach the wire.
func TestNewValidatesPrefix(t *testing.T) {
	bad := []string{"/leading", "trailing/", "a//b", "a/./b", "a/../b", `a\b`}
	for _, prefix := range bad {
		if _, err := New(context.Background(), Config{Bucket: "bucket", Prefix: prefix}); err == nil {
			t.Fatalf("New with prefix %q succeeded, want an error", prefix)
		}
	}
	for _, prefix := range []string{"", "ok", "ok/prefix"} {
		if _, err := New(context.Background(), Config{Bucket: "bucket", Prefix: prefix}); err != nil {
			t.Fatalf("New with prefix %q: %v", prefix, err)
		}
	}
}

// TestNewValidatesPartSize proves that a multipart part size below the S3
// minimum is rejected at construction.
func TestNewValidatesPartSize(t *testing.T) {
	if _, err := New(context.Background(), Config{Bucket: "bucket"}, Options{MultipartPartSize: 1 << 20}); err == nil {
		t.Fatal("New with a part size below the S3 minimum succeeded, want an error")
	}
	if _, err := New(context.Background(), Config{Bucket: "bucket"}, Options{MultipartPartSize: 5 << 20}); err != nil {
		t.Fatalf("New with the S3 minimum part size: %v", err)
	}
}

// TestWithDefaults proves the upload-strategy tuning: the zero value uses
// the defaults, explicit values override them, and the part size is always
// clamped to the S3 minimum.
func TestWithDefaults(t *testing.T) {
	threshold, part, err := (Options{}).withDefaults()
	if err != nil {
		t.Fatalf("defaults: %v", err)
	}
	if threshold != defaultMultipartThreshold {
		t.Fatalf("default threshold = %d, want %d", threshold, defaultMultipartThreshold)
	}
	if part != defaultMultipartPartSize {
		t.Fatalf("default part size = %d, want %d", part, defaultMultipartPartSize)
	}

	threshold, part, err = (Options{MultipartThreshold: 1, MultipartPartSize: 5 << 20}).withDefaults()
	if err != nil {
		t.Fatalf("explicit options: %v", err)
	}
	if threshold != 1 || part != 5<<20 {
		t.Fatalf("explicit options = (%d, %d), want (1, %d)", threshold, part, 5<<20)
	}

	if _, _, err := (Options{MultipartPartSize: 4 << 20}).withDefaults(); err == nil {
		t.Fatal("part size below the S3 minimum succeeded, want an error")
	}
}

// TestFullKeyJoin proves that the adapter owns the prefix join: protocol
// keys stay relative and the configured prefix is joined with one slash
// (architecture section 9.1).
func TestFullKeyJoin(t *testing.T) {
	if got := (&Store{prefix: "nb"}).fullKey(storage.CurrentKey); got != "nb/current" {
		t.Fatalf("fullKey = %q, want %q", got, "nb/current")
	}
	if got := (&Store{}).fullKey(storage.CurrentKey); got != storage.CurrentKey {
		t.Fatalf("fullKey without prefix = %q, want %q", got, storage.CurrentKey)
	}
}

// TestMapError proves the semantic error mapping: S3 codes become the
// stable storage categories and everything else is a transport failure.
// A non-semantic API error keeps its server code and message in the text
// so the startup probe can surface the real reason.
func TestMapError(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		want     error
		wantText string // empty skips the text assertion
	}{
		{"NoSuchKey", &smithy.GenericAPIError{Code: "NoSuchKey"}, storage.ErrNotFound, ""},
		{"NotFound", &smithy.GenericAPIError{Code: "NotFound"}, storage.ErrNotFound, ""},
		{"PreconditionFailed", &smithy.GenericAPIError{Code: "PreconditionFailed"}, storage.ErrPreconditionFailed, ""},
		{"AccessDenied", &smithy.GenericAPIError{Code: "AccessDenied"}, storage.ErrTransport, "AccessDenied"},
		{"auth code and message", &smithy.GenericAPIError{Code: "InvalidAccessKeyId", Message: "The access key does not exist"}, storage.ErrTransport, "InvalidAccessKeyId: The access key does not exist"},
		{"InternalError", &smithy.GenericAPIError{Code: "InternalError"}, storage.ErrTransport, "InternalError"},
		{"transport", errors.New("connection reset"), storage.ErrTransport, "connection reset"},
		{"credential resolution", errors.New("operation error S3: PutObject, failed to sign request: failed to retrieve credentials"), storage.ErrTransport, "failed to retrieve credentials"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := mapError("test", tt.err)
			if !errors.Is(got, tt.want) {
				t.Fatalf("mapError = %v, want a %v error", got, tt.want)
			}
			if tt.wantText != "" && !strings.Contains(got.Error(), tt.wantText) {
				t.Fatalf("mapError = %q, want it to contain %q", got, tt.wantText)
			}
		})
	}
	if err := mapError("test", nil); err != nil {
		t.Fatalf("mapError(nil) = %v, want nil", err)
	}
}

// TestMapErrorNonJSONResponse proves that an HTTP response the SDK could
// not deserialize (an HTML error page, not JSON) surfaces its status code
// and a short body fragment instead of the raw JSON syntax error.
func TestMapErrorNonJSONResponse(t *testing.T) {
	body := []byte("<!DOCTYPE html><html><head><title>Access Denied</title></head><body>no access allowed</body></html>")
	err := &smithyhttp.ResponseError{
		Response: &smithyhttp.Response{Response: &http.Response{StatusCode: 400}},
		Err: &smithy.DeserializationError{
			Err:      errors.New("failed to decode response body, invalid character '<' looking for beginning of value"),
			Snapshot: body,
		},
	}
	got := mapError("create key", err)
	if !errors.Is(got, storage.ErrTransport) {
		t.Fatalf("mapError = %v, want ErrTransport", got)
	}
	for _, want := range []string{"HTTP 400", "Access Denied", "no access allowed"} {
		if !strings.Contains(got.Error(), want) {
			t.Fatalf("mapError = %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got.Error(), "invalid character") {
		t.Fatalf("mapError = %q, want the raw JSON error replaced", got)
	}
}

// TestMetadataRoundTrip proves the slivingdoc metadata headers survive an
// encode/decode round trip and that present-but-malformed headers are
// rejected.
func TestMetadataRoundTrip(t *testing.T) {
	meta := storage.Metadata{
		SHA256:     sha256Value(t, "pack bytes"),
		Size:       12345,
		Kind:       storage.KindCheckpoint,
		Generation: 7,
	}
	got, err := decodeMeta(encodeMeta(meta))
	if err != nil {
		t.Fatalf("decodeMeta(encodeMeta(meta)): %v", err)
	}
	if got != meta {
		t.Fatalf("metadata round trip = %+v, want %+v", got, meta)
	}

	// Absent metadata decodes to the zero value: headers are diagnostic,
	// the manifest descriptor is authoritative.
	got, err = decodeMeta(nil)
	if err != nil {
		t.Fatalf("decodeMeta(nil): %v", err)
	}
	if got != (storage.Metadata{}) {
		t.Fatalf("decodeMeta(nil) = %+v, want the zero value", got)
	}

	malformed := []map[string]string{
		{metaSHA256: "not-hex"},
		{metaSHA256: "ABCDEF"},
		{metaSize: "big"},
		{metaSize: "-1"},
		{metaKind: "bogus"},
		{metaGeneration: "one"},
	}
	for _, md := range malformed {
		if _, err := decodeMeta(md); err == nil {
			t.Fatalf("decodeMeta(%v) succeeded, want an error", md)
		}
	}
}

func sha256Value(t *testing.T, s string) storage.SHA256 {
	t.Helper()
	sum := sha256.Sum256([]byte(s))
	return storage.SHA256(sum)
}

// recordingTransport records the request URL and fails the request, so a
// store's addressing mode is observable without any endpoint or network.
type recordingTransport struct {
	urls []string
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.urls = append(t.urls, req.URL.String())
	return nil, errors.New("recording transport: request not sent")
}

// TestAddressingProvesPathStyle proves that --path-style addressing is
// honored without a custom endpoint: a forced store addresses the bucket in
// the URL path, while the default store uses virtual-host addressing.
func TestAddressingProvesPathStyle(t *testing.T) {
	for _, tt := range []struct {
		name        string
		force       bool
		wantPath    string // URL path prefix of the first request
		wantHostSub string // host substring proving the addressing mode
	}{
		{name: "default virtual host", force: false, wantPath: "/prefix/key", wantHostSub: "bucket.s3."},
		{name: "forced path style", force: true, wantPath: "/bucket/prefix/key", wantHostSub: "s3."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recordingTransport{}
			cfg := Config{
				Bucket: "bucket", Prefix: "prefix", Region: "us-east-1",
				AccessKey: "AKIAIOSFODNN7EXAMPLE", SecretKey: "secret",
				httpClient:       &http.Client{Transport: rec},
				retryMaxAttempts: 1, // the recording transport fails; one attempt proves the URL
			}
			st, err := New(context.Background(), cfg, Options{ForcePathStyle: tt.force})
			if err != nil {
				t.Fatalf("New() = %v", err)
			}
			err = st.PutObject(context.Background(), "key", strings.NewReader("data"), storage.Metadata{Size: 4})
			if err == nil {
				t.Fatal("PutObject() succeeded, want the recording transport error")
			}
			if len(rec.urls) != 1 {
				t.Fatalf("requests = %d, want 1", len(rec.urls))
			}
			u, err := url.Parse(rec.urls[0])
			if err != nil {
				t.Fatalf("parse %q: %v", rec.urls[0], err)
			}
			if u.Path != tt.wantPath {
				t.Fatalf("request path = %q, want %q", u.Path, tt.wantPath)
			}
			if !strings.Contains(u.Host, tt.wantHostSub) {
				t.Fatalf("request host = %q, want a host containing %q", u.Host, tt.wantHostSub)
			}
		})
	}
}
