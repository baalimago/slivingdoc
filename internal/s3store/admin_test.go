package s3store

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/storage"
)

// writeAWSFile writes one shared AWS configuration or credentials file and
// returns its path.
func writeAWSFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

const (
	devProfileConfig = "[profile dev]\nregion = eu-north-1\nservices = dev-services\n\n[services dev-services]\ns3 =\n  endpoint_url = http://profile.invalid:9000\n"
	devProfileRegion = "eu-north-1"
	devProfileSite   = "http://profile.invalid:9000"
)

// useProfile points the SDK at one shared configuration file for the test.
func useProfile(t *testing.T, body string) {
	t.Helper()
	isolateAWSEnv(t)
	t.Setenv("AWS_CONFIG_FILE", writeAWSFile(t, t.TempDir(), "config", body))
}

// TestProfileSelectsTheSharedSection proves a profile is a selector: its
// region and services endpoint reach the resolved identity, and its endpoint
// implies path style.
func TestProfileSelectsTheSharedSection(t *testing.T) {
	useProfile(t, devProfileConfig)
	got, err := Resolve(context.Background(), Config{Profile: "dev"})
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	want := Resolved{Profile: "dev", Endpoint: devProfileSite, Region: devProfileRegion, PathStyle: true}
	if got != want {
		t.Fatalf("Resolve() = %+v, want %+v", got, want)
	}
}

// TestBareConfigSectionSuppliesNoRegion proves the documented shared-config
// form: only a [profile <name>] section of ~/.aws/config defines a profile, so
// a bare [<name>] section there leaves the region empty while the credentials
// file's [<name>] section still supplies the key (architecture/s3store.md).
func TestBareConfigSectionSuppliesNoRegion(t *testing.T) {
	isolateAWSEnv(t)
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", writeAWSFile(t, dir, "config", "[dev]\nregion = eu-north-1\n"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", writeAWSFile(t, dir, "credentials", "[dev]\naws_access_key_id = AKIABARE\naws_secret_access_key = secret\n"))
	got, err := Resolve(context.Background(), Config{Profile: "dev"})
	if err != nil {
		t.Fatalf("Resolve(bare section) = %v", err)
	}
	if got.Region != "" {
		t.Fatalf("Resolve(bare section) region = %q, want empty: a bare section is not a profile", got.Region)
	}
	admin, err := NewAdmin(context.Background(), Config{Profile: "dev"})
	if err != nil {
		t.Fatalf("NewAdmin(bare section) = %v", err)
	}
	identity, err := admin.Identity(context.Background())
	if err != nil {
		t.Fatalf("Identity(bare section) = %v", err)
	}
	if identity.AccessKeyID != "AKIABARE" || identity.Region != DefaultRegion {
		t.Fatalf("Identity(bare section) = %+v, want the credentials section's key and the %s fallback", identity, DefaultRegion)
	}
}

// TestExplicitProfileBeatsAWSProfile proves the configured profile wins over
// AWS_PROFILE, and the variable is the negative control that it still applies
// when nothing overrides it.
func TestExplicitProfileBeatsAWSProfile(t *testing.T) {
	useProfile(t, devProfileConfig+"[profile ambient]\nregion = us-west-2\n")
	t.Setenv("AWS_PROFILE", "ambient")
	got, err := Resolve(context.Background(), Config{Profile: "dev"})
	if err != nil {
		t.Fatalf("Resolve(explicit profile) = %v", err)
	}
	if got.Profile != "dev" || got.Region != devProfileRegion {
		t.Fatalf("Resolve(explicit profile) = %+v, want profile dev in %s", got, devProfileRegion)
	}
	got, err = Resolve(context.Background(), Config{})
	if err != nil {
		t.Fatalf("Resolve(AWS_PROFILE) = %v", err)
	}
	if got.Profile != "" || got.Region != "us-west-2" {
		t.Fatalf("Resolve(AWS_PROFILE) = %+v, want the ambient profile's region", got)
	}
}

// TestProfileRegionWins proves a profile's region applies when no flag or
// environment names one, and that New keeps it rather than its default.
func TestProfileRegionWins(t *testing.T) {
	useProfile(t, devProfileConfig)
	resolved, err := Resolve(context.Background(), Config{Profile: "dev"})
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	if resolved.Region != devProfileRegion {
		t.Fatalf("Resolve region = %q, want the profile's %q", resolved.Region, devProfileRegion)
	}
	st, err := New(context.Background(), Config{Bucket: "bucket", Prefix: "prefix", Profile: "dev"})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if got := st.Resolved(); got != resolved {
		t.Fatalf("Store.Resolved() = %+v, want the resolved identity %+v", got, resolved)
	}
}

// TestResolveReportsEffectiveValues proves the explicit settings win over the
// profile and that an unset endpoint leaves the default AWS endpoint with
// virtual-host addressing.
func TestResolveReportsEffectiveValues(t *testing.T) {
	useProfile(t, devProfileConfig)
	const configured = "http://configured.example:8333"
	got, err := Resolve(context.Background(), Config{
		Profile: "dev", Region: "eu-west-1", Endpoint: configured,
	})
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	want := Resolved{Profile: "dev", Endpoint: configured, Region: "eu-west-1", PathStyle: true}
	if got != want {
		t.Fatalf("Resolve() = %+v, want %+v", got, want)
	}
	got, err = Resolve(context.Background(), Config{Region: "us-east-1"})
	if err != nil {
		t.Fatalf("Resolve(default endpoint) = %v", err)
	}
	if got.Endpoint != "" || got.PathStyle {
		t.Fatalf("Resolve(default endpoint) = %+v, want the default AWS endpoint in virtual-host style", got)
	}
}

// TestResolveReadsServiceEndpoint proves the SDK's own AWS_ENDPOINT_URL_S3
// order reaches the resolved identity and implies path style.
func TestResolveReadsServiceEndpoint(t *testing.T) {
	isolateAWSEnv(t)
	const endpoint = "http://env.invalid:9000"
	t.Setenv("AWS_ENDPOINT_URL_S3", endpoint)
	got, err := Resolve(context.Background(), Config{Region: "us-east-1"})
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	if got.Endpoint != endpoint || !got.PathStyle {
		t.Fatalf("Resolve() = %+v, want %s in path style", got, endpoint)
	}
}

// TestResolveDoesNotRetrieveCredentials proves the resolved identity comes
// without a credential retrieval: a profile whose only source is a web
// identity token file that does not exist still resolves, and only Identity
// then fails.
func TestResolveDoesNotRetrieveCredentials(t *testing.T) {
	useProfile(t, "[profile lazy]\nrole_arn = arn:aws:iam::123456789012:role/test\nweb_identity_token_file = /nonexistent/token\nregion = eu-north-1\n")
	got, err := Resolve(context.Background(), Config{Profile: "lazy"})
	if err != nil {
		t.Fatalf("Resolve() = %v, want no credential retrieval", err)
	}
	if got.Region != "eu-north-1" {
		t.Fatalf("Resolve region = %q, want the profile's region", got.Region)
	}
	admin, err := NewAdmin(context.Background(), Config{Profile: "lazy"})
	if err != nil {
		t.Fatalf("NewAdmin() = %v, want no credential retrieval", err)
	}
	if _, err := admin.Identity(context.Background()); !errors.Is(err, storage.ErrAccessDenied) {
		t.Fatalf("Identity() = %v, want the credential refusal", err)
	}
}

// TestResolveUnknownProfile proves an unknown profile is the typed refusal the
// space command turns into its own line.
func TestResolveUnknownProfile(t *testing.T) {
	useProfile(t, devProfileConfig)
	_, err := Resolve(context.Background(), Config{Profile: "nope"})
	if !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("Resolve(unknown profile) = %v, want ErrProfileNotFound", err)
	}
	if !strings.Contains(err.Error(), "s3store: load AWS configuration") {
		t.Fatalf("Resolve(unknown profile) = %v, want the configuration context", err)
	}
}

// TestResolveUnreadableConfig proves an unreadable shared configuration is the
// SDK's configuration error.
func TestResolveUnreadableConfig(t *testing.T) {
	isolateAWSEnv(t)
	t.Setenv("AWS_CONFIG_FILE", t.TempDir())
	if _, err := Resolve(context.Background(), Config{}); err == nil {
		t.Fatal("Resolve(unreadable config) = nil, want an error")
	}
}

// TestStoreResolved proves New reports what it built with, including the
// us-east-1 fallback when nothing names a region.
func TestStoreResolved(t *testing.T) {
	useProfile(t, devProfileConfig)
	st, err := New(context.Background(), Config{
		Bucket: "bucket", Prefix: "prefix", Region: "eu-west-1", Endpoint: "http://custom.example:9000",
	}, Options{ForcePathStyle: true})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	want := Resolved{Endpoint: "http://custom.example:9000", Region: "eu-west-1", PathStyle: true}
	if got := st.Resolved(); got != want {
		t.Fatalf("Store.Resolved() = %+v, want %+v", got, want)
	}
	st, err = New(context.Background(), Config{Bucket: "bucket", Prefix: "prefix"})
	if err != nil {
		t.Fatalf("New(no region) = %v", err)
	}
	if got := st.Resolved(); got.Region != DefaultRegion || got.Endpoint != "" || got.PathStyle {
		t.Fatalf("Store.Resolved() = %+v, want the default region and AWS endpoint", got)
	}
}

// TestNoRegionFallsBackToUSEast1 proves a configuration that names no region
// reaches the SDK as us-east-1, so CreateBucket sends no location constraint.
func TestNoRegionFallsBackToUSEast1(t *testing.T) {
	admin, tr := newRecordingAdmin(t, Config{}, listBucketsAnswer(t))
	if got := admin.resolved.Region; got != DefaultRegion {
		t.Fatalf("region = %q, want %s", got, DefaultRegion)
	}
	if err := admin.CreateBucket(context.Background(), "analytics"); err != nil {
		t.Fatalf("CreateBucket() = %v", err)
	}
	req := tr.only(t)
	if len(req.body) != 0 {
		t.Fatalf("create body = %q, want none in us-east-1", req.body)
	}
}

// TestNewAdminIgnoresBucketAndPrefix proves administration needs no notebook
// configuration: an invalid bucket and prefix are never validated, and no
// request is sent by construction.
func TestNewAdminIgnoresBucketAndPrefix(t *testing.T) {
	admin, tr := newRecordingAdmin(t, Config{Bucket: "bucket", Prefix: "/not/a/prefix"}, listBucketsAnswer(t))
	if admin == nil {
		t.Fatal("NewAdmin() = nil")
	}
	if len(tr.requests) != 0 {
		t.Fatalf("NewAdmin requests = %d, want 0", len(tr.requests))
	}
}

// TestAdminNeverCreates proves New, NewAdmin, and Resolve send no create
// request and validate no bucket: only Admin.CreateBucket creates.
func TestAdminNeverCreates(t *testing.T) {
	isolateAWSEnv(t)
	tr := &adminTransport{answer: listBucketsAnswer(t)}
	cfg := Config{
		Bucket: "bucket", Prefix: "prefix",
		AccessKey: "AKIAIOSFODNN7EXAMPLE", SecretKey: "secret",
		httpClient: &http.Client{Transport: tr}, retryMaxAttempts: 1,
	}
	if _, err := New(context.Background(), cfg); err != nil {
		t.Fatalf("New() = %v", err)
	}
	if _, err := NewAdmin(context.Background(), cfg); err != nil {
		t.Fatalf("NewAdmin() = %v", err)
	}
	if _, err := Resolve(context.Background(), cfg); err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	if _, err := NewAdmin(context.Background(), Config{}); err != nil {
		t.Fatalf("NewAdmin(no bucket) = %v, want no validation", err)
	}
	if len(tr.requests) != 0 {
		t.Fatalf("construction requests = %d, want 0", len(tr.requests))
	}
}

// TestAdminListBuckets proves the answer maps to the plain bucket values in
// one request.
func TestAdminListBuckets(t *testing.T) {
	admin, tr := newRecordingAdmin(t, Config{}, listBucketsAnswer(t))
	buckets, err := admin.ListBuckets(context.Background())
	if err != nil {
		t.Fatalf("ListBuckets() = %v", err)
	}
	want := []Bucket{{Name: "analytics", Created: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}}
	if !slices.Equal(buckets, want) {
		t.Fatalf("ListBuckets() = %v, want %v", buckets, want)
	}
	req := tr.only(t)
	if req.method != http.MethodGet || req.path != "/" {
		t.Fatalf("request = %s %s, want GET /", req.method, req.path)
	}
}

// TestCreateBucketBody proves the create configuration is sent only on the
// default AWS endpoint outside us-east-1.
func TestCreateBucketBody(t *testing.T) {
	for _, tt := range []struct {
		name        string
		cfg         Config
		wantBody    []string
		wantNoBody  bool
		wantPath    string
		wantHostSub string
	}{
		{
			name:     "default endpoint in eu-north-1",
			cfg:      Config{Region: "eu-north-1"},
			wantBody: []string{"CreateBucketConfiguration", "LocationConstraint", "eu-north-1"},
		},
		{
			name: "default endpoint in us-east-1", cfg: Config{Region: "us-east-1"}, wantNoBody: true,
		},
		{
			name: "custom endpoint", cfg: Config{Endpoint: "http://custom.invalid:9000", Region: "auto"},
			wantNoBody: true, wantPath: "/analytics", wantHostSub: "custom.invalid:9000",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			admin, tr := newRecordingAdmin(t, tt.cfg, listBucketsAnswer(t))
			if err := admin.CreateBucket(context.Background(), "analytics"); err != nil {
				t.Fatalf("CreateBucket() = %v", err)
			}
			req := tr.only(t)
			if req.method != http.MethodPut {
				t.Fatalf("method = %s, want PUT", req.method)
			}
			if tt.wantNoBody && len(req.body) != 0 {
				t.Fatalf("create body = %q, want none", req.body)
			}
			for _, want := range tt.wantBody {
				if !strings.Contains(string(req.body), want) {
					t.Fatalf("create body = %q, want %q", req.body, want)
				}
			}
			if tt.wantPath != "" && req.path != tt.wantPath {
				t.Fatalf("request path = %q, want %q", req.path, tt.wantPath)
			}
			if tt.wantHostSub != "" && req.host != tt.wantHostSub {
				t.Fatalf("request host = %q, want %q", req.host, tt.wantHostSub)
			}
		})
	}
}

// TestCreateBucketErrorMapping proves the create refusals map to the stable
// sentinels, and that a denial or a transport failure stays the storage
// category.
func TestCreateBucketErrorMapping(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		code    string
		message string
		want    error
		notWant error
	}{
		{name: "exists", status: http.StatusConflict, code: "BucketAlreadyExists", want: ErrBucketExists, notWant: ErrBucketOwned},
		{name: "owned", status: http.StatusConflict, code: "BucketAlreadyOwnedByYou", want: ErrBucketOwned, notWant: ErrBucketExists},
		{name: "denied", status: http.StatusForbidden, code: "AccessDenied", want: storage.ErrAccessDenied},
	} {
		t.Run(tt.name, func(t *testing.T) {
			admin, _ := newRecordingAdmin(t, Config{Region: "eu-north-1"}, func(*http.Request, []byte) (*http.Response, error) {
				return xmlResponse(tt.status, "<Error><Code>"+tt.code+"</Code><Message>"+tt.message+"</Message></Error>"), nil
			})
			err := admin.CreateBucket(context.Background(), "analytics")
			if !errors.Is(err, tt.want) {
				t.Fatalf("CreateBucket() = %v, want %v", err, tt.want)
			}
			if tt.notWant != nil && errors.Is(err, tt.notWant) {
				t.Fatalf("CreateBucket() = %v, want a refusal other than %v", err, tt.notWant)
			}
		})
	}
	admin, _ := newRecordingAdmin(t, Config{Region: "eu-north-1"}, func(*http.Request, []byte) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})
	if err := admin.CreateBucket(context.Background(), "analytics"); !errors.Is(err, storage.ErrTransport) {
		t.Fatalf("CreateBucket(transport failure) = %v, want ErrTransport", err)
	}
}

// TestCreateBucketEmptyName proves an empty name never becomes a request.
func TestCreateBucketEmptyName(t *testing.T) {
	admin, tr := newRecordingAdmin(t, Config{Region: "eu-north-1"}, listBucketsAnswer(t))
	if err := admin.CreateBucket(context.Background(), ""); err == nil {
		t.Fatal("CreateBucket(\"\") = nil, want a refusal")
	}
	if len(tr.requests) != 0 {
		t.Fatalf("requests = %d, want 0", len(tr.requests))
	}
}

// TestAdminDenied proves a denied listing is the access-denial category.
func TestAdminDenied(t *testing.T) {
	admin, _ := newRecordingAdmin(t, Config{}, func(*http.Request, []byte) (*http.Response, error) {
		return xmlResponse(http.StatusForbidden, "<Error><Code>AccessDenied</Code><Message>no</Message></Error>"), nil
	})
	if _, err := admin.ListBuckets(context.Background()); !errors.Is(err, storage.ErrAccessDenied) {
		t.Fatalf("ListBuckets() = %v, want ErrAccessDenied", err)
	}
}

// TestAdminTransport proves a transport failure keeps the transport category.
func TestAdminTransport(t *testing.T) {
	admin, _ := newRecordingAdmin(t, Config{}, func(*http.Request, []byte) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})
	if _, err := admin.ListBuckets(context.Background()); !errors.Is(err, storage.ErrTransport) {
		t.Fatalf("ListBuckets() = %v, want ErrTransport", err)
	}
}

// TestAdminIdentity proves Identity reports the access key id with the resolved
// values and never the secret.
func TestAdminIdentity(t *testing.T) {
	const accessKey, secret = "AKIAIOSFODNN7EXAMPLE", "not-a-real-secret"
	admin, tr := newRecordingAdmin(t, Config{
		AccessKey: accessKey, SecretKey: secret, Region: "eu-west-1", Endpoint: "http://custom.example:9000",
	}, listBucketsAnswer(t))
	got, err := admin.Identity(context.Background())
	if err != nil {
		t.Fatalf("Identity() = %v", err)
	}
	want := Identity{AccessKeyID: accessKey, Endpoint: "http://custom.example:9000", Region: "eu-west-1"}
	if got != want {
		t.Fatalf("Identity() = %+v, want %+v", got, want)
	}
	if text := strings.Join([]string{got.Profile, got.AccessKeyID, got.Endpoint, got.Region}, " "); strings.Contains(text, secret) {
		t.Fatalf("Identity() = %q, want no secret", text)
	}
	if len(tr.requests) != 0 {
		t.Fatalf("Identity requests = %d, want 0 credential round trips to the store", len(tr.requests))
	}
}

// TestAdminIdentityDenied proves a credential that cannot be retrieved is the
// access-denial category.
func TestAdminIdentityDenied(t *testing.T) {
	useProfile(t, "[profile lazy]\nrole_arn = arn:aws:iam::123456789012:role/test\nweb_identity_token_file = /nonexistent/token\n")
	admin, err := NewAdmin(context.Background(), Config{Profile: "lazy"})
	if err != nil {
		t.Fatalf("NewAdmin() = %v", err)
	}
	if _, err := admin.Identity(context.Background()); !errors.Is(err, storage.ErrAccessDenied) {
		t.Fatalf("Identity() = %v, want ErrAccessDenied", err)
	}
}

// newRecordingAdmin builds an Admin over cfg with a recording transport that
// answers every request through answer.
func newRecordingAdmin(t *testing.T, cfg Config, answer func(*http.Request, []byte) (*http.Response, error)) (*Admin, *adminTransport) {
	t.Helper()
	isolateAWSEnv(t)
	tr := &adminTransport{answer: answer}
	cfg.AccessKey = "AKIAIOSFODNN7EXAMPLE"
	cfg.SecretKey = "secret"
	cfg.httpClient = &http.Client{Transport: tr}
	cfg.retryMaxAttempts = 1
	admin, err := NewAdmin(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewAdmin(%+v) = %v", cfg, err)
	}
	return admin, tr
}

// adminRequest is one request a test transport observed.
type adminRequest struct {
	method string
	url    string
	host   string
	path   string
	body   []byte
}

// adminTransport records every request and answers it through answer. A nil
// answer fails the request, so construction can prove it sent nothing.
type adminTransport struct {
	mu       sync.Mutex
	requests []adminRequest
	answer   func(*http.Request, []byte) (*http.Response, error)
}

func (a *adminTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		read, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		body = read
	}
	u, err := url.Parse(req.URL.String())
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.requests = append(a.requests, adminRequest{
		method: req.Method, url: req.URL.String(), host: u.Host, path: u.Path, body: body,
	})
	a.mu.Unlock()
	if a.answer == nil {
		return nil, errors.New("admin transport: no answer")
	}
	return a.answer(req, body)
}

// only returns the one request the test expects.
func (a *adminTransport) only(t *testing.T) adminRequest {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(a.requests))
	}
	return a.requests[0]
}

// listBucketsAnswer answers one ListBuckets request with the bucket the tests
// assert.
func listBucketsAnswer(t *testing.T) func(*http.Request, []byte) (*http.Response, error) {
	t.Helper()
	return func(req *http.Request, _ []byte) (*http.Response, error) {
		if req.Method == http.MethodGet && req.URL.Path == "/" {
			return xmlResponse(http.StatusOK, `<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Buckets><Bucket><Name>analytics</Name><CreationDate>2026-01-02T03:04:05.000Z</CreationDate></Bucket></Buckets></ListAllMyBucketsResult>`), nil
		}
		return xmlResponse(http.StatusOK, ""), nil
	}
}
