package app

import (
	"context"
	"net/http"
	"strings"
	"testing"

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
			env:          []string{token, "SLIVINGDOC_BUCKET=notes", "AWS_ENDPOINT_URL_S3=https://s3.example.test", "AWS_REGION=eu-north-1"},
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
			args:         []string{"--bucket", "notes", "--endpoint", "https://flag.example.test"},
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
		return resolveHostedSpace(context.Background(), config{endpoint: g.URL(), token: token, bucket: bucket})
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

	if err := checkStore(context.Background(), newStore(t, hostedTestToken)); err != nil {
		t.Fatalf("checkStore with a read-only token = %v, want success without the write probe", err)
	}
	if g.Stored("notes") != 0 {
		t.Fatal("the hosted check wrote to the space")
	}

	err := checkStore(context.Background(), newStore(t, "sld_unknown"))
	if err == nil || !strings.Contains(err.Error(), "refused the token") || strings.Contains(err.Error(), "sld_unknown") {
		t.Fatalf("checkStore with an unknown token = %v, want a redacted token refusal", err)
	}

	g.RefuseNext(http.MethodGet, http.StatusNotFound, "not_found")
	err = checkStore(context.Background(), newStore(t, hostedTestToken))
	if err == nil || !strings.Contains(err.Error(), "INCOMPATIBLE_STORE") {
		t.Fatalf("checkStore against a server without /v1 = %v, want INCOMPATIBLE_STORE", err)
	}

	g.RefuseNextWithReason(http.MethodGet, http.StatusTooManyRequests, "rate_limited", "slow_reads")
	err = checkStore(context.Background(), newStore(t, hostedTestToken))
	if err == nil || !strings.Contains(err.Error(), "hosted storage check failed") || strings.Contains(err.Error(), "INCOMPATIBLE_STORE") {
		t.Fatalf("checkStore while throttled = %v, want a check failure that is not INCOMPATIBLE_STORE", err)
	}
}

func TestCheckStoreProbesPlainStores(t *testing.T) {
	store := &refusingStore{err: storage.ErrTransport}
	err := checkStore(context.Background(), store)
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
