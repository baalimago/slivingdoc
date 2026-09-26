package s3store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
// (architecture/storage.md).
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
			isolateAWSEnv(t)
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

// TestConfiguredEndpointBeatsServiceEndpointSettings proves that a
// configured endpoint addresses every request even when the SDK's own
// settings name another one or tell it to ignore configured endpoints:
// AWS_ENDPOINT_URL_S3, a profile's services s3 endpoint_url,
// AWS_IGNORE_CONFIGURED_ENDPOINT_URLS, and a profile's
// ignore_configured_endpoint_urls. The negative-control rows leave the
// endpoint unset and show each ambient setting really redirects traffic,
// so the positive rows cannot pass by accident. An SDK upgrade that
// reorders the sources must fail here (architecture/config.md).
func TestConfiguredEndpointBeatsServiceEndpointSettings(t *testing.T) {
	dir := t.TempDir()
	writeProfile := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write profile: %v", err)
		}
		return path
	}
	services := writeProfile("services", "[default]\nservices = elsewhere\n\n[services elsewhere]\ns3 =\n  endpoint_url = http://profile.invalid:9000\n")
	ignoring := writeProfile("ignoring", "[default]\nignore_configured_endpoint_urls = true\n")
	const configured = "http://configured.example:8333"
	for _, tt := range []struct {
		name     string
		env      map[string]string
		endpoint string
		wantHost string // exact host, or the suffix of a virtual host
	}{
		{name: "AWS_ENDPOINT_URL_S3", env: map[string]string{"AWS_ENDPOINT_URL_S3": "http://env.invalid:9000"}, endpoint: configured, wantHost: "configured.example:8333"},
		{name: "profile endpoint_url", env: map[string]string{"AWS_CONFIG_FILE": services, "AWS_PROFILE": "default"}, endpoint: configured, wantHost: "configured.example:8333"},
		{name: "AWS_IGNORE_CONFIGURED_ENDPOINT_URLS", env: map[string]string{"AWS_IGNORE_CONFIGURED_ENDPOINT_URLS": "true"}, endpoint: configured, wantHost: "configured.example:8333"},
		{name: "profile ignore_configured_endpoint_urls", env: map[string]string{"AWS_CONFIG_FILE": ignoring, "AWS_PROFILE": "default"}, endpoint: configured, wantHost: "configured.example:8333"},
		{name: "control: AWS_ENDPOINT_URL_S3 without an endpoint", env: map[string]string{"AWS_ENDPOINT_URL_S3": "http://env.invalid:9000"}, wantHost: "env.invalid:9000"},
		{name: "control: profile endpoint_url without an endpoint", env: map[string]string{"AWS_CONFIG_FILE": services, "AWS_PROFILE": "default"}, wantHost: "profile.invalid:9000"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			isolateAWSEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			rec := &recordingTransport{}
			st, err := New(context.Background(), Config{
				Bucket: "bucket", Prefix: "prefix", Region: "us-east-1",
				Endpoint:  tt.endpoint,
				AccessKey: "AKIAIOSFODNN7EXAMPLE", SecretKey: "secret",
				httpClient:       &http.Client{Transport: rec},
				retryMaxAttempts: 1,
			})
			if err != nil {
				t.Fatalf("New() = %v", err)
			}
			if _, _, err := st.ReadObject(context.Background(), "key"); err == nil {
				t.Fatal("ReadObject() succeeded, want the recording transport error")
			}
			if len(rec.urls) != 1 {
				t.Fatalf("requests = %d, want 1", len(rec.urls))
			}
			u, err := url.Parse(rec.urls[0])
			if err != nil {
				t.Fatalf("parse %q: %v", rec.urls[0], err)
			}
			if u.Host != tt.wantHost && !strings.HasSuffix(u.Host, "."+tt.wantHost) {
				t.Fatalf("request = %s, want host %s", rec.urls[0], tt.wantHost)
			}
			if tt.endpoint != "" && u.Path != "/bucket/prefix/key" {
				t.Fatalf("request = %s, want the configured endpoint in path style", rec.urls[0])
			}
		})
	}
}

// isolateAWSEnv clears, for one test, every ambient AWS setting that
// changes how the SDK builds a client: a CA bundle (which conflicts with an
// injected HTTP client), a profile and its files, endpoint overrides, and
// the FIPS and dual-stack endpoint variants.
// t.Setenv records the original values for the cleanup; the variables are
// then unset rather than left empty, since the SDK reads an empty file
// variable as the default path.
func isolateAWSEnv(t *testing.T) {
	t.Helper()
	names := []string{
		"AWS_CA_BUNDLE", "AWS_PROFILE", "AWS_DEFAULT_PROFILE", "AWS_IGNORE_CONFIGURED_ENDPOINT_URLS",
		"AWS_USE_FIPS_ENDPOINT", "AWS_USE_DUALSTACK_ENDPOINT",
	}
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "AWS_ENDPOINT_URL") {
			names = append(names, name)
		}
	}
	for _, name := range names {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
	}
	missing := filepath.Join(t.TempDir(), "absent")
	t.Setenv("AWS_CONFIG_FILE", missing)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", missing)
}

// multipartTransport answers the multipart calls of one upload: create
// returns an upload ID, a part succeeds unless failPart is set, completion
// fails with a server error, and every abort is recorded with whether its
// request context was still live.
type multipartTransport struct {
	mu          sync.Mutex
	failPart    func() // runs when a part arrives; the part then fails
	aborts      int
	abortCtxErr error
}

func (m *multipartTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		// The SDK computes the part checksum while the body streams, so
		// the body is consumed as a real server would.
		if _, err := io.Copy(io.Discard, req.Body); err != nil {
			return nil, err
		}
	}
	q := req.URL.Query()
	switch {
	case req.Method == http.MethodPost && q.Has("uploads"):
		return xmlResponse(http.StatusOK, `<InitiateMultipartUploadResult><Bucket>bucket</Bucket><Key>prefix/key</Key><UploadId>up-1</UploadId></InitiateMultipartUploadResult>`), nil
	case req.Method == http.MethodPut && q.Has("partNumber"):
		if m.failPart != nil {
			m.failPart()
			return nil, errors.New("multipart transport: part failed")
		}
		resp := xmlResponse(http.StatusOK, "")
		resp.Header.Set("ETag", `"part-etag"`)
		return resp, nil
	case req.Method == http.MethodPost && q.Has("uploadId"):
		return xmlResponse(http.StatusInternalServerError, `<Error><Code>InternalError</Code><Message>complete failed</Message></Error>`), nil
	case req.Method == http.MethodDelete && q.Has("uploadId"):
		m.mu.Lock()
		m.aborts++
		m.abortCtxErr = req.Context().Err()
		m.mu.Unlock()
		return xmlResponse(http.StatusNoContent, ""), nil
	}
	return nil, fmt.Errorf("multipart transport: unexpected %s %s", req.Method, req.URL)
}

func xmlResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/xml"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// TestMultipartAbortsOnEveryFailure proves that a multipart upload is
// aborted when its completion fails, and that the abort still reaches the
// store on a live context when the caller's context was cancelled during a
// part (architecture/s3store.md).
func TestMultipartAbortsOnEveryFailure(t *testing.T) {
	for _, tt := range []struct {
		name       string
		cancelPart bool
	}{
		{name: "complete fails"},
		{name: "part fails after the request context is cancelled", cancelPart: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			isolateAWSEnv(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			tr := &multipartTransport{}
			if tt.cancelPart {
				tr.failPart = cancel
			}
			st, err := New(context.Background(), Config{
				Bucket: "bucket", Prefix: "prefix", Region: "us-east-1",
				AccessKey: "AKIAIOSFODNN7EXAMPLE", SecretKey: "secret",
				httpClient:       &http.Client{Transport: tr},
				retryMaxAttempts: 1,
			}, Options{MultipartThreshold: 1})
			if err != nil {
				t.Fatalf("New() = %v", err)
			}
			if err := st.PutObject(ctx, "key", strings.NewReader("data"), storage.Metadata{Size: 4}); err == nil {
				t.Fatal("PutObject() succeeded, want the multipart failure")
			}
			tr.mu.Lock()
			defer tr.mu.Unlock()
			if tr.aborts != 1 {
				t.Fatalf("aborts = %d, want 1", tr.aborts)
			}
			if tr.abortCtxErr != nil {
				t.Fatalf("abort request context = %v, want a live context", tr.abortCtxErr)
			}
		})
	}
}
