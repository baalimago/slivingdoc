package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/httpstore"
	"github.com/baalimago/slivingdoc/internal/httpstore/gatewaytest"
	"github.com/baalimago/slivingdoc/internal/storage"
)

const hostedTestToken = "sld_0123456789abcdef_dG9rZW4tZm9yLWFwcC10ZXN0cw"

func TestLoadConfigHosted(t *testing.T) {
	token := "SLIVINGDOC_TOKEN=" + hostedTestToken
	tests := []struct {
		name         string
		env          []string
		args         []string
		wantEndpoint string
	}{
		{
			name:         "token selects the default hosted endpoint",
			env:          []string{token, "SLIVINGDOC_BUCKET=notes"},
			wantEndpoint: DefaultHostedEndpoint,
		},
		{
			name:         "S3 variables never redirect a token",
			env:          []string{token, "SLIVINGDOC_BUCKET=notes", "SLIVINGDOC_STORAGE=hosted", "AWS_ENDPOINT_URL_S3=https://s3.example.test", "AWS_REGION=eu-north-1"},
			wantEndpoint: DefaultHostedEndpoint,
		},
		{
			name:         "SLIVINGDOC_ENDPOINT overrides the default",
			env:          []string{token, "SLIVINGDOC_BUCKET=notes", "SLIVINGDOC_ENDPOINT=https://Storage.Example.Test/"},
			wantEndpoint: "https://storage.example.test",
		},
		{
			name:         "the flag wins over SLIVINGDOC_ENDPOINT",
			env:          []string{token, "SLIVINGDOC_ENDPOINT=https://env.example.test"},
			args:         []string{"--storage", "hosted", "--bucket", "notes", "--endpoint", "https://flag.example.test"},
			wantEndpoint: "https://flag.example.test",
		},
		{
			name:         "plain http to loopback is allowed",
			env:          []string{token, "SLIVINGDOC_BUCKET=notes", "SLIVINGDOC_ENDPOINT=http://127.0.0.1:8787"},
			wantEndpoint: "http://127.0.0.1:8787",
		},
		{
			name:         "plain http to localhost is allowed",
			env:          []string{token, "SLIVINGDOC_BUCKET=notes", "SLIVINGDOC_ENDPOINT=http://localhost:8787"},
			wantEndpoint: "http://localhost:8787",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadConfig(testProcess(tt.env, tt.args...))
			if err != nil {
				t.Fatalf("loadConfig() = %v", err)
			}
			if !cfg.hosted() || cfg.token != hostedTestToken {
				t.Fatal("the token did not select hosted mode")
			}
			if cfg.endpoint != tt.wantEndpoint {
				t.Fatalf("endpoint = %q, want %q", cfg.endpoint, tt.wantEndpoint)
			}
			if cfg.region != "" {
				t.Fatalf("region = %q, want none in hosted mode", cfg.region)
			}
		})
	}
}

func TestLoadConfigHostedRefusals(t *testing.T) {
	token := "SLIVINGDOC_TOKEN=" + hostedTestToken
	tests := []struct {
		name string
		env  []string
		want string
	}{
		{"invalid space", []string{token, "SLIVINGDOC_BUCKET=Team_Notes"}, "invalid space name"},
		{"plain http remote", []string{token, "SLIVINGDOC_BUCKET=notes", "SLIVINGDOC_ENDPOINT=http://api.example.test"}, "must use https"},
		{"token with white space", []string{"SLIVINGDOC_TOKEN=sld bad token", "SLIVINGDOC_BUCKET=notes"}, "SLIVINGDOC_TOKEN must be printable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadConfig(testProcess(tt.env))
			if err == nil {
				t.Fatal("loadConfig() = nil, want a refusal")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("loadConfig() = %q, want it to contain %q", err, tt.want)
			}
			if strings.Contains(err.Error(), hostedTestToken) || strings.Contains(err.Error(), "bad token") {
				t.Fatalf("loadConfig() = %q leaks the token", err)
			}
		})
	}
}

func TestLoadConfigHostedSpaceIsOptional(t *testing.T) {
	cfg, err := loadConfig(testProcess([]string{"SLIVINGDOC_TOKEN=" + hostedTestToken}))
	if err != nil {
		t.Fatalf("loadConfig() without a space = %v, want the token's space resolved later", err)
	}
	if cfg.bucket != "" {
		t.Fatalf("bucket = %q, want empty until the startup lookup", cfg.bucket)
	}
	if _, err := loadConfig(testProcess(nil)); err == nil || !strings.Contains(err.Error(), "bucket is required") {
		t.Fatalf("loadConfig() without a token or bucket = %v, want bucket is required", err)
	}
}

func TestResolveHostedSpace(t *testing.T) {
	g := gatewaytest.Start(t)
	g.AddSpace("notes", 1<<20)
	g.AddSpace("other", 1<<20)
	g.Grant(hostedTestToken, "notes", false)
	resolve := func(bucket, token string) (config, error) {
		return resolveHostedSpace(context.Background(), config{endpoint: g.URL(), token: token, bucket: bucket}, noLogins)
	}
	for _, bucket := range []string{"", "notes"} {
		cfg, err := resolve(bucket, hostedTestToken)
		if err != nil || cfg.bucket != "notes" {
			t.Fatalf("resolve(bucket %q) = %q, %v; want the token's space notes", bucket, cfg.bucket, err)
		}
	}
	for _, row := range []struct {
		name, bucket, token, want string
	}{
		{"another space", "other", hostedTestToken, `the token reaches hosted space "notes", not "other"`},
		{"unknown token", "", "sld_unknown", "refused the token"},
	} {
		_, err := resolve(row.bucket, row.token)
		if err == nil || !strings.Contains(err.Error(), row.want) {
			t.Fatalf("resolve(%s) = %v, want it to contain %q", row.name, err, row.want)
		}
		if strings.Contains(err.Error(), row.token) {
			t.Fatalf("resolve(%s) = %q leaks the token", row.name, err)
		}
	}

	g.DisableTokenLookup()
	if cfg, err := resolve("notes", hostedTestToken); err != nil || cfg.bucket != "notes" {
		t.Fatalf("resolve against an older server with --bucket = %q, %v; want --bucket kept", cfg.bucket, err)
	}
	if _, err := resolve("", hostedTestToken); err == nil || !strings.Contains(err.Error(), "pass the space name as --bucket") {
		t.Fatalf("resolve against an older server without --bucket = %v, want a refusal asking for --bucket", err)
	}
}

// TestSetupHostedRefusalRemovesSessionDir proves a hosted startup refusal
// after the session directory exists removes it, like a configuration
// refusal does.
func TestSetupHostedRefusalRemovesSessionDir(t *testing.T) {
	g := gatewaytest.Start(t)
	g.AddSpace("notes", 1<<20)
	g.AddSpace("other", 1<<20)
	g.Grant(hostedTestToken, "notes", false)
	for _, row := range []struct {
		name string
		env  []string
	}{
		{"unknown token", []string{"SLIVINGDOC_TOKEN=sld_unknown"}},
		{"another space", []string{"SLIVINGDOC_TOKEN=" + hostedTestToken, "SLIVINGDOC_BUCKET=other"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			session := filepath.Join(t.TempDir(), "session")
			if err := os.MkdirAll(session, 0o700); err != nil {
				t.Fatalf("MkdirAll() = %v", err)
			}
			env := append([]string{"SLIVINGDOC_ENDPOINT=" + g.URL()}, row.env...)
			if _, err := setup(ephemeralProcess(session, env)); err == nil {
				t.Fatal("setup() = nil, want a hosted startup refusal")
			}
			if _, err := os.Stat(session); !os.IsNotExist(err) {
				t.Fatalf("Stat(session) = %v, want the session directory removed", err)
			}
		})
	}
}

// TestSetupHostedUsesTheTokensSpace proves the resolved space reaches the
// store factory and the runtime configuration.
func TestSetupHostedUsesTheTokensSpace(t *testing.T) {
	g := gatewaytest.Start(t)
	g.AddSpace("notes", 1<<20)
	g.Grant(hostedTestToken, "notes", false)
	p := testProcess([]string{"SLIVINGDOC_TOKEN=" + hostedTestToken, "SLIVINGDOC_ENDPOINT=" + g.URL()})
	var built string
	p.storeFactory = func(ctx context.Context, cfg config) (storage.ObjectStore, error) {
		built = cfg.bucket
		return realStoreFactory(ctx, cfg)
	}
	rt, err := setup(p)
	if err != nil {
		t.Fatalf("setup() = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	if built != "notes" || rt.cfg.bucket != "notes" || rt.cfg.serviceConfig().Bucket != "notes" {
		t.Fatalf("store built for %q, runtime bucket %q; want the token's space notes", built, rt.cfg.bucket)
	}
}

func TestLoadConfigWithoutTokenKeepsS3(t *testing.T) {
	cfg, err := loadConfig(testProcess([]string{"SLIVINGDOC_BUCKET=My_Bucket", "SLIVINGDOC_ENDPOINT=https://ignored.example.test"}))
	if err != nil {
		t.Fatalf("loadConfig() = %v", err)
	}
	if cfg.hosted() || cfg.endpoint != "" || cfg.region != "us-east-1" {
		t.Fatalf("config = hosted %v endpoint %q region %q, want plain S3 defaults", cfg.hosted(), cfg.endpoint, cfg.region)
	}
}

func TestRealStoreFactoryBuildsHostedStore(t *testing.T) {
	cfg, err := loadConfig(testProcess([]string{"SLIVINGDOC_TOKEN=" + hostedTestToken, "SLIVINGDOC_BUCKET=notes"}))
	if err != nil {
		t.Fatalf("loadConfig() = %v", err)
	}
	store, err := realStoreFactory(context.Background(), cfg)
	if err != nil {
		t.Fatalf("realStoreFactory() = %v", err)
	}
	if _, ok := store.(*httpstore.Store); !ok {
		t.Fatalf("realStoreFactory() = %T, want *httpstore.Store", store)
	}
}

func TestCheckStoreHosted(t *testing.T) {
	g := gatewaytest.Start(t)
	g.AddSpace("notes", 1<<20)
	g.Grant(hostedTestToken, "notes", true)
	newStore := func(t *testing.T, token string) storage.ObjectStore {
		t.Helper()
		s, err := httpstore.New(httpstore.Config{Endpoint: g.URL(), Space: "notes", Token: token})
		if err != nil {
			t.Fatalf("httpstore.New() = %v", err)
		}
		return s
	}

	if err := checkStore(context.Background(), newStore(t, hostedTestToken), originEnv); err != nil {
		t.Fatalf("checkStore with a read-only token = %v, want success without the write probe", err)
	}
	if g.Stored("notes") != 0 {
		t.Fatal("the hosted check wrote to the space")
	}

	err := checkStore(context.Background(), newStore(t, "sld_unknown"), originEnv)
	if err == nil || !strings.Contains(err.Error(), "refused the token") || strings.Contains(err.Error(), "sld_unknown") {
		t.Fatalf("checkStore with an unknown token = %v, want a redacted token refusal", err)
	}
	err = checkStore(context.Background(), newStore(t, "sld_unknown"), originLogin)
	if err == nil || !strings.Contains(err.Error(), "refused the stored login") ||
		!strings.Contains(err.Error(), "run 'slivingdoc login' again") || strings.Contains(err.Error(), "sld_unknown") {
		t.Fatalf("checkStore with an unknown stored login = %v, want a redacted refusal that says to log in again", err)
	}

	g.RefuseNext(http.MethodGet, http.StatusNotFound, "not_found")
	err = checkStore(context.Background(), newStore(t, hostedTestToken), originEnv)
	if err == nil || !strings.Contains(err.Error(), "INCOMPATIBLE_STORE") {
		t.Fatalf("checkStore against a server without /v1 = %v, want INCOMPATIBLE_STORE", err)
	}

	g.RefuseNextWithReason(http.MethodGet, http.StatusTooManyRequests, "rate_limited", "slow_reads")
	err = checkStore(context.Background(), newStore(t, hostedTestToken), originEnv)
	if err == nil || !strings.Contains(err.Error(), "hosted storage check failed") || strings.Contains(err.Error(), "INCOMPATIBLE_STORE") {
		t.Fatalf("checkStore while throttled = %v, want a check failure that is not INCOMPATIBLE_STORE", err)
	}
}

func TestCheckStoreProbesPlainStores(t *testing.T) {
	store := &refusingStore{err: storage.ErrTransport}
	err := checkStore(context.Background(), store, originEnv)
	if err == nil || !strings.Contains(err.Error(), "S3 compatibility probe failed") {
		t.Fatalf("checkStore on a plain store = %v, want the probe diagnostic", err)
	}
}

// refusingStore fails every conditional create, so the probe fails first.
type refusingStore struct {
	storage.ObjectStore
	err error
}

func (s *refusingStore) CreateObject(context.Context, string, []byte) (storage.ETag, error) {
	return "", s.err
}

func (s *refusingStore) DeleteObjects(context.Context, []string) error { return nil }

// TestResolveHostedSpaceNamesWhereTheSpaceCameFrom proves a space from the
// default login, or a stored login's own space, that the token does not
// reach is refused with the fix that applies, never silently replaced.
func TestResolveHostedSpaceNamesWhereTheSpaceCameFrom(t *testing.T) {
	g := gatewaytest.Start(t)
	g.AddSpace("notes", 1<<20)
	g.Grant(hostedTestToken, "notes", false)
	for _, row := range []struct {
		name string
		cfg  config
		want string
	}{
		{
			"SLIVINGDOC_BUCKET",
			config{bucket: "team", bucketFrom: bucketFromEnv, tokenOrigin: originEnv},
			`not "team" from SLIVINGDOC_BUCKET; unset SLIVINGDOC_BUCKET to use the token's space`,
		},
		{
			"a stored login whose token reaches another space",
			config{bucket: "team", bucketFrom: bucketFromLogin, tokenOrigin: originLogin},
			`the stored login for space "team" holds a token that reaches hosted space "notes"; run 'slivingdoc login --bucket team' again`,
		},
		{
			"a named bucket",
			config{bucket: "team", bucketFrom: bucketFromFlag, tokenOrigin: originEnv},
			`not "team" from --bucket; drop --bucket to use the token's space`,
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			cfg := row.cfg
			cfg.endpoint, cfg.token = g.URL(), hostedTestToken
			_, err := resolveHostedSpace(context.Background(), cfg, noLogins)
			if err == nil || !strings.Contains(err.Error(), row.want) {
				t.Fatalf("resolveHostedSpace() = %v, want it to contain %q", err, row.want)
			}
		})
	}
	cfg, err := resolveHostedSpace(context.Background(), config{endpoint: g.URL(), token: hostedTestToken, bucket: "notes", bucketFrom: bucketFromLogin, tokenOrigin: originLogin}, noLogins)
	if err != nil || cfg.bucket != "notes" {
		t.Fatalf("resolveHostedSpace() of an agreeing login = %q, %v", cfg.bucket, err)
	}
}

func noLogins() (credentials.Set, error) { return credentials.Set{}, nil }

// TestResolveHostedSpaceFallsBackToTheDefaultLogin proves a server without
// the token lookup takes the default login's space only when that login is
// for the token's endpoint, and that the stored logins are read only then.
func TestResolveHostedSpaceFallsBackToTheDefaultLogin(t *testing.T) {
	g := gatewaytest.Start(t)
	g.AddSpace("notes", 1<<20)
	g.Grant(hostedTestToken, "notes", false)
	cfg := config{endpoint: g.URL(), token: hostedTestToken, tokenOrigin: originEnv}
	read := 0
	counting := func() (credentials.Set, error) { read++; return credentials.Set{}, nil }
	if _, err := resolveHostedSpace(context.Background(), cfg, counting); err != nil || read != 0 {
		t.Fatalf("resolve with the lookup = %v after %d reads; want no read of the stored logins", err, read)
	}
	g.DisableTokenLookup()
	for _, row := range []struct {
		name     string
		endpoint string
		want     string
	}{
		{"same endpoint", g.URL(), ""},
		{"another endpoint", devEndpoint, "pass the space name as --bucket"},
	} {
		t.Run(row.name, func(t *testing.T) {
			dir := strings.TrimPrefix(writeLogins(t, &storedKey{row.endpoint, "notes"}, entry(row.endpoint, "notes", loginToken)), credentials.DirEnv+"=")
			logins := func() (credentials.Set, error) {
				return loadLogins(map[string]string{credentials.DirEnv: dir}, runtime.GOOS)
			}
			got, err := resolveHostedSpace(context.Background(), cfg, logins)
			if row.want == "" {
				if err != nil || got.bucket != "notes" || got.bucketFrom != bucketFromLogin {
					t.Fatalf("resolve = %q from %v, %v; want the default login's space", got.bucket, got.bucketFrom, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), row.want) {
				t.Fatalf("resolve = %v, want it to contain %q", err, row.want)
			}
		})
	}
	broken := func() (credentials.Set, error) { return credentials.Set{}, errors.New("malformed credentials file") }
	if _, err := resolveHostedSpace(context.Background(), cfg, broken); err == nil || !strings.Contains(err.Error(), "cannot supply it: malformed") {
		t.Fatalf("resolve with a broken file = %v, want the refusal naming it", err)
	}
}
