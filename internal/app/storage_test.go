package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/credentials"
)

const (
	loginToken  = "sld_1111111111111111_c3RvcmVkLWxvZ2luLXRva2VuLWZvci1hcHAtdGVzdHMteHg"
	otherToken  = "sld_2222222222222222_b3RoZXItbG9naW4tdG9rZW4tZm9yLWFwcC10ZXN0cy14eHg"
	devEndpoint = "https://api.dev.slivingdoc.dev"
	longAgo     = "2001-01-01T00:00:00Z"
)

// storedLogin is one entry of a test credentials file.
type storedLogin struct {
	Endpoint  string `json:"endpoint"`
	Space     string `json:"space"`
	Site      string `json:"site"`
	Token     string `json:"token"`
	Access    string `json:"access"`
	ExpiresAt string `json:"expiresAt,omitempty"`
}

type storedKey struct {
	Endpoint string `json:"endpoint"`
	Space    string `json:"space"`
}

func entry(endpoint, space, token string) storedLogin {
	return storedLogin{Endpoint: endpoint, Space: space, Site: "https://www.slivingdoc.dev", Token: token, Access: "write"}
}

// writeLogins writes a version 1 credentials file in the documented format
// (architecture/login.md) and returns the SLIVINGDOC_CONFIG_DIR entry that
// selects it. A nil def stores no default.
func writeLogins(t *testing.T, def *storedKey, logins ...storedLogin) string {
	t.Helper()
	dir := t.TempDir()
	data, err := json.Marshal(struct {
		Version int           `json:"version"`
		Default *storedKey    `json:"default,omitempty"`
		Logins  []storedLogin `json:"logins"`
	}{1, def, logins})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, credentials.FileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return credentials.DirEnv + "=" + dir
}

func TestResolveStorage(t *testing.T) {
	notes := writeLogins(t, &storedKey{DefaultHostedEndpoint, "notes"}, entry(DefaultHostedEndpoint, "notes", loginToken))
	dev := writeLogins(t, nil, entry(devEndpoint, "notes", loginToken))
	token := "SLIVINGDOC_TOKEN=" + hostedTestToken
	tests := []struct {
		name         string
		env          []string
		args         []string
		wantHosted   bool
		wantToken    string
		wantOrigin   tokenOrigin
		wantBucket   string
		wantEndpoint string
	}{
		{
			name: "auto: the token selects hosted", env: []string{token, "SLIVINGDOC_BUCKET=notes"},
			wantHosted: true, wantToken: hostedTestToken, wantOrigin: originEnv, wantBucket: "notes", wantEndpoint: DefaultHostedEndpoint,
		},
		{
			name: "auto: the token wins over a login and AWS settings", env: []string{token, notes, "AWS_PROFILE=p", "SLIVINGDOC_BUCKET=notes"},
			wantHosted: true, wantToken: hostedTestToken, wantOrigin: originEnv, wantBucket: "notes", wantEndpoint: DefaultHostedEndpoint,
		},
		{
			name: "auto: a login selects hosted", env: []string{notes, "SLIVINGDOC_BUCKET=notes"},
			wantHosted: true, wantToken: loginToken, wantOrigin: originLogin, wantBucket: "notes", wantEndpoint: DefaultHostedEndpoint,
		},
		{
			name: "auto: the login is used at the endpoint it was issued for", env: []string{dev, "SLIVINGDOC_BUCKET=notes"},
			wantHosted: true, wantToken: loginToken, wantOrigin: originLogin, wantBucket: "notes", wantEndpoint: devEndpoint,
		},
		{
			name: "auto: the bucket defaults to the default login's space", env: []string{notes},
			wantHosted: true, wantToken: loginToken, wantOrigin: originLogin, wantBucket: "notes", wantEndpoint: DefaultHostedEndpoint,
		},
		{
			name: "auto: the bucket defaults with a token too", env: []string{notes, token},
			wantHosted: true, wantToken: hostedTestToken, wantOrigin: originEnv, wantBucket: "notes", wantEndpoint: DefaultHostedEndpoint,
		},
		{
			name: "auto: a login for another space keeps S3", env: []string{notes, "SLIVINGDOC_BUCKET=other"},
			wantBucket: "other",
		},
		{
			name: "auto: no login keeps S3", env: []string{credentials.DirEnv + "=" + t.TempDir(), "SLIVINGDOC_BUCKET=notes"},
			wantBucket: "notes",
		},
		{
			name: "auto: no configuration directory keeps S3", env: []string{"SLIVINGDOC_BUCKET=notes"},
			wantBucket: "notes",
		},
		{
			name: "auto: an endpoint the login was not issued for keeps S3", env: []string{notes, "SLIVINGDOC_BUCKET=notes"},
			args:       []string{"--endpoint", "https://s3.example.test"},
			wantBucket: "notes", wantEndpoint: "https://s3.example.test",
		},
		{
			name: "auto: the same endpoint in another spelling uses the login", env: []string{dev, "SLIVINGDOC_BUCKET=notes", "SLIVINGDOC_ENDPOINT=https://API.dev.slivingdoc.dev/"},
			wantHosted: true, wantToken: loginToken, wantOrigin: originLogin, wantBucket: "notes", wantEndpoint: devEndpoint,
		},
		{
			name: "s3: the token and the login are ignored", env: []string{token, notes, "SLIVINGDOC_BUCKET=notes"}, args: []string{"--storage", "s3"},
			wantBucket: "notes",
		},
		{
			name: "s3 from the environment", env: []string{token, notes, "SLIVINGDOC_BUCKET=notes", "SLIVINGDOC_STORAGE=s3"},
			wantBucket: "notes",
		},
		{
			name: "hosted: the login", env: []string{notes, "SLIVINGDOC_BUCKET=notes", "AWS_ACCESS_KEY_ID=k"}, args: []string{"--storage", "hosted"},
			wantHosted: true, wantToken: loginToken, wantOrigin: originLogin, wantBucket: "notes", wantEndpoint: DefaultHostedEndpoint,
		},
		{
			name: "hosted: the token without a login", env: []string{token, "SLIVINGDOC_BUCKET=notes"}, args: []string{"--storage=hosted"},
			wantHosted: true, wantToken: hostedTestToken, wantOrigin: originEnv, wantBucket: "notes", wantEndpoint: DefaultHostedEndpoint,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadConfig(testProcess(tt.env, tt.args...))
			if err != nil {
				t.Fatalf("loadConfig() = %v", err)
			}
			if cfg.hosted() != tt.wantHosted || cfg.token != tt.wantToken || cfg.tokenOrigin != tt.wantOrigin ||
				cfg.bucket != tt.wantBucket || cfg.endpoint != tt.wantEndpoint {
				t.Fatalf("config = hosted %v origin %q bucket %q endpoint %q token set %v; want hosted %v origin %q bucket %q endpoint %q",
					cfg.hosted(), cfg.tokenOrigin, cfg.bucket, cfg.endpoint, cfg.token != "",
					tt.wantHosted, tt.wantOrigin, tt.wantBucket, tt.wantEndpoint)
			}
			if !tt.wantHosted && cfg.region == "" {
				t.Fatal("an S3 configuration has no region")
			}
		})
	}
}

func TestResolveStorageRefusals(t *testing.T) {
	notes := writeLogins(t, &storedKey{DefaultHostedEndpoint, "notes"}, entry(DefaultHostedEndpoint, "notes", loginToken))
	expiredEntry := entry(DefaultHostedEndpoint, "notes", loginToken)
	expiredEntry.ExpiresAt = longAgo
	expired := writeLogins(t, nil, expiredEntry)
	twoEndpoints := writeLogins(t, &storedKey{DefaultHostedEndpoint, "team"},
		entry(DefaultHostedEndpoint, "notes", loginToken), entry(devEndpoint, "notes", otherToken), entry(DefaultHostedEndpoint, "team", otherToken))
	malformedDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(malformedDir, credentials.FileName), []byte(`{"version":1,"logins":[{"token":"`+loginToken+`"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	malformed := credentials.DirEnv + "=" + malformedDir
	empty := credentials.DirEnv + "=" + t.TempDir()
	tests := []struct {
		name string
		env  []string
		args []string
		want []string
	}{
		{
			"a login and AWS keys are ambiguous",
			[]string{notes, "SLIVINGDOC_BUCKET=notes", "AWS_ACCESS_KEY_ID=k"},
			nil,
			[]string{`stored login for space "notes"`, "AWS_ACCESS_KEY_ID", "--storage hosted", "--storage s3"},
		},
		{
			"a login and an AWS profile and endpoint are ambiguous",
			[]string{notes, "AWS_PROFILE=p", "AWS_ENDPOINT_URL_S3=https://s3.example.test"},
			nil,
			[]string{"AWS_PROFILE, AWS_ENDPOINT_URL_S3", "--storage hosted"},
		},
		{
			"hosted without a token or a login",
			[]string{empty, "SLIVINGDOC_BUCKET=notes"},
			[]string{"--storage", "hosted"},
			[]string{"needs SLIVINGDOC_TOKEN or a stored login", "slivingdoc login --bucket notes"},
		},
		{
			"hosted without a space",
			[]string{empty},
			[]string{"--storage", "hosted"},
			[]string{"needs a space", "slivingdoc login"},
		},
		{
			"hosted at an endpoint the login was not issued for",
			[]string{notes, "SLIVINGDOC_ENDPOINT=https://other.example.test"},
			[]string{"--storage", "hosted"},
			[]string{"at https://other.example.test", "issued for " + DefaultHostedEndpoint, "slivingdoc login"},
		},
		{
			"an expired login",
			[]string{expired, "SLIVINGDOC_BUCKET=notes"},
			nil,
			[]string{"expired 2001-01-01 00:00 UTC", "run 'slivingdoc login --bucket notes'"},
		},
		{
			"two endpoints for one space",
			[]string{twoEndpoints, "SLIVINGDOC_BUCKET=notes"},
			nil,
			[]string{"several stored logins", "pass --endpoint"},
		},
		{
			"a malformed credentials file",
			[]string{malformed, "SLIVINGDOC_BUCKET=notes"},
			nil,
			[]string{"malformed credentials file", "--storage s3"},
		},
		{
			"an unknown storage",
			[]string{"SLIVINGDOC_BUCKET=notes"},
			[]string{"--storage", "gcs"},
			[]string{`storage "gcs" is not one of auto, hosted, s3`},
		},
		{
			"an invalid endpoint",
			[]string{"SLIVINGDOC_BUCKET=notes", "SLIVINGDOC_ENDPOINT=ftp://x"},
			[]string{"--storage", "hosted"},
			[]string{"absolute http or https"},
		},
		{
			"s3 never takes the default space",
			[]string{notes},
			[]string{"--storage", "s3"},
			[]string{"bucket is required"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadConfig(testProcess(tt.env, tt.args...))
			if err == nil {
				t.Fatal("loadConfig() = nil, want a refusal")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("loadConfig() = %q, want it to contain %q", err, want)
				}
			}
			if strings.Contains(err.Error(), loginToken) || strings.Contains(err.Error(), otherToken) {
				t.Fatalf("loadConfig() = %q echoes a token", err)
			}
		})
	}
	// With the choice made explicit, the same ambiguity is resolved.
	cfg, err := loadConfig(testProcess([]string{notes, "SLIVINGDOC_BUCKET=notes", "AWS_ACCESS_KEY_ID=k"}, "--storage", "s3"))
	if err != nil || cfg.hosted() {
		t.Fatalf("--storage s3 = hosted %v, %v; want S3", cfg.hosted(), err)
	}
	cfg, err = loadConfig(testProcess([]string{malformed, "SLIVINGDOC_BUCKET=notes"}, "--storage", "s3"))
	if err != nil || cfg.hosted() {
		t.Fatalf("--storage s3 with a malformed file = hosted %v, %v; want S3 without reading it", cfg.hosted(), err)
	}
}

func TestStorageDescribesItsChoices(t *testing.T) {
	for _, s := range []string{"auto", "hosted", "s3"} {
		if _, err := parseStorageMode(s); err != nil {
			t.Fatalf("parseStorageMode(%q) = %v", s, err)
		}
	}
	for _, want := range []string{"--storage string", "SLIVINGDOC_STORAGE", "SLIVINGDOC_CONFIG_DIR"} {
		if !strings.Contains(FlagReference, want) {
			t.Fatalf("FlagReference does not document %s", want)
		}
	}
}
