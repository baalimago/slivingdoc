package app

import (
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/credentials"
)

const (
	loginToken  = "sld_1111111111111111_c3RvcmVkLWxvZ2luLXRva2VuLWZvci1hcHAtdGVzdHMteHg"
	otherToken  = "sld_2222222222222222_b3RoZXItbG9naW4tdG9rZW4tZm9yLWFwcC10ZXN0cy14eHg"
	thirdToken  = "sld_3333333333333333_dGhpcmQtbG9naW4tdG9rZW4tZm9yLWFwcC10ZXN0cy14eHh4"
	devEndpoint = "https://api.dev.slivingdoc.dev"
	longAgo     = "2001-01-01T00:00:00Z"
)

// storedLogin is one login entry of a test credentials file.
type storedLogin struct {
	Site      string `json:"site"`
	Endpoint  string `json:"endpoint"`
	Key       string `json:"key"`
	Access    string `json:"access"`
	ExpiresAt string `json:"expiresAt,omitempty"`
	Account   string `json:"account"`
}

// storedDefault is one default space of a test credentials file.
type storedDefault struct {
	Endpoint string `json:"endpoint"`
	Space    string `json:"space"`
}

func entry(endpoint, key string) storedLogin {
	return storedLogin{Site: "https://www.slivingdoc.dev", Endpoint: endpoint, Key: key, Access: "write", Account: "ada@example.test"}
}

// defaults pairs endpoints with their default spaces.
func defaults(endpointSpace ...string) []storedDefault {
	var out []storedDefault
	for i := 0; i+1 < len(endpointSpace); i += 2 {
		out = append(out, storedDefault{Endpoint: endpointSpace[i], Space: endpointSpace[i+1]})
	}
	return out
}

// writeRawLogins writes data as the credentials file of a new private
// directory and returns the SLIVINGDOC_CONFIG_DIR entry that selects it.
func writeRawLogins(t *testing.T, data []byte) string {
	t.Helper()
	// A directory of our own, 0700 whatever the umask: Load refuses one
	// that group or other can write.
	dir := filepath.Join(t.TempDir(), "cfg")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, credentials.FileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return credentials.DirEnv + "=" + dir
}

// writeLogins writes a version 2 credentials file in the documented format
// (architecture/login.md) and returns the SLIVINGDOC_CONFIG_DIR entry that
// selects it.
func writeLogins(t *testing.T, defs []storedDefault, logins ...storedLogin) string {
	t.Helper()
	data, err := json.Marshal(struct {
		Version  int             `json:"version"`
		Logins   []storedLogin   `json:"logins"`
		Defaults []storedDefault `json:"defaultSpaces,omitempty"`
	}{2, logins, defs})
	if err != nil {
		t.Fatal(err)
	}
	return writeRawLogins(t, data)
}

// outdatedLogins writes a version 1 credentials file, as an earlier build
// did.
func outdatedLogins(t *testing.T) string {
	t.Helper()
	return writeRawLogins(t, []byte(`{"version":1,"logins":[{"endpoint":"https://api.slivingdoc.dev","space":"notes",`+
		`"site":"https://www.slivingdoc.dev","token":"`+loginToken+`","access":"write"}]}`))
}

// awsHomeDir is a home directory holding both shared AWS files.
func awsHomeDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".aws"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"credentials", "config"} {
		if err := os.WriteFile(filepath.Join(home, ".aws", name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func TestResolveStorage(t *testing.T) {
	awsHome := awsHomeDir(t)
	notes := writeLogins(t, defaults(DefaultHostedEndpoint, "notes"), entry(DefaultHostedEndpoint, loginToken))
	dev := writeLogins(t, nil, entry(devEndpoint, loginToken))
	two := writeLogins(t, defaults(DefaultHostedEndpoint, "notes", devEndpoint, "team"), entry(DefaultHostedEndpoint, loginToken), entry(devEndpoint, otherToken))
	token := "SLIVINGDOC_TOKEN=" + hostedTestToken
	// A umask of 0002 leaves a config directory group-writable; with no
	// credentials file in it, an S3 process never reads it.
	shared := filepath.Join(t.TempDir(), "cfg")
	if err := os.Mkdir(shared, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o775); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name         string
		env          []string
		args         []string
		wantHosted   bool
		wantToken    string
		wantKey      string
		wantOrigin   tokenOrigin
		wantBucket   string
		wantFrom     bucketSource
		wantEndpoint string
	}{
		{
			name: "auto: the token selects hosted", env: []string{token, "SLIVINGDOC_BUCKET=notes"},
			wantHosted: true, wantToken: hostedTestToken, wantOrigin: originEnv, wantBucket: "notes", wantFrom: bucketFromEnv, wantEndpoint: DefaultHostedEndpoint,
		},
		{
			name: "auto: the token wins over a login", env: []string{token, notes, "SLIVINGDOC_BUCKET=notes"},
			wantHosted: true, wantToken: hostedTestToken, wantOrigin: originEnv, wantBucket: "notes", wantFrom: bucketFromEnv, wantEndpoint: DefaultHostedEndpoint,
		},
		{
			name: "hosted: the token with an endpoint flag and AWS settings", env: []string{token, "AWS_PROFILE=p", "SLIVINGDOC_BUCKET=notes"},
			args:       []string{"--storage", "hosted", "--endpoint", devEndpoint},
			wantHosted: true, wantToken: hostedTestToken, wantOrigin: originEnv, wantBucket: "notes", wantFrom: bucketFromEnv, wantEndpoint: devEndpoint,
		},
		{
			name: "auto: the default space takes the login even with AWS settings", env: []string{notes, "AWS_PROFILE=p", "AWS_REGION=eu-north-1"},
			wantHosted: true, wantKey: loginToken, wantOrigin: originLogin, wantBucket: "notes", wantFrom: bucketFromLogin, wantEndpoint: DefaultHostedEndpoint,
		},
		{
			name: "auto: an explicit space takes the login when nothing configures S3", env: []string{notes, "HOME=" + t.TempDir()},
			args:       []string{"--space", "team"},
			wantHosted: true, wantKey: loginToken, wantOrigin: originLogin, wantBucket: "team", wantFrom: bucketFromSpaceFlag, wantEndpoint: DefaultHostedEndpoint,
		},
		{
			name: "auto: SLIVINGDOC_SPACE beats the default space", env: []string{notes, "SLIVINGDOC_SPACE=team"},
			wantHosted: true, wantKey: loginToken, wantOrigin: originLogin, wantBucket: "team", wantFrom: bucketFromSpaceEnv, wantEndpoint: DefaultHostedEndpoint,
		},
		{
			name: "auto: the token ignores a region, AWS credentials and the shared AWS files",
			env: []string{
				token, "SLIVINGDOC_BUCKET=notes", "AWS_REGION=eu-north-1", "AWS_DEFAULT_REGION=eu-north-1",
				"AWS_PROFILE=p", "AWS_ACCESS_KEY_ID=k", "HOME=" + awsHome,
			},
			args:       []string{"--region", "eu-north-1"},
			wantHosted: true, wantToken: hostedTestToken, wantOrigin: originEnv, wantBucket: "notes", wantFrom: bucketFromEnv, wantEndpoint: DefaultHostedEndpoint,
		},
		{
			name: "auto: the token with the hosted endpoint variable", env: []string{token, "SLIVINGDOC_BUCKET=notes", "SLIVINGDOC_ENDPOINT=" + devEndpoint},
			wantHosted: true, wantToken: hostedTestToken, wantOrigin: originEnv, wantBucket: "notes", wantFrom: bucketFromEnv, wantEndpoint: devEndpoint,
		},
		{
			name: "auto: the login is used at its own endpoint", env: []string{dev, "SLIVINGDOC_BUCKET=notes"},
			wantHosted: true, wantKey: loginToken, wantOrigin: originLogin, wantBucket: "notes", wantFrom: bucketFromEnv, wantEndpoint: devEndpoint,
		},
		{
			name: "auto: an endpoint chooses among two logins, and its default space", env: []string{two, "SLIVINGDOC_ENDPOINT=" + devEndpoint},
			wantHosted: true, wantKey: otherToken, wantOrigin: originLogin, wantBucket: "team", wantFrom: bucketFromLogin, wantEndpoint: devEndpoint,
		},
		{
			name: "auto: a token leaves the bucket to its own space", env: []string{notes, token},
			wantHosted: true, wantToken: hostedTestToken, wantOrigin: originEnv, wantBucket: "", wantEndpoint: DefaultHostedEndpoint,
		},
		{
			name: "auto: no login keeps S3", env: []string{credentials.DirEnv + "=" + t.TempDir(), "SLIVINGDOC_BUCKET=notes"},
			wantBucket: "notes", wantFrom: bucketFromEnv,
		},
		{
			name: "auto: a group-writable directory without a file keeps S3", env: []string{credentials.DirEnv + "=" + shared, "SLIVINGDOC_BUCKET=notes"},
			wantBucket: "notes", wantFrom: bucketFromEnv,
		},
		{
			name: "auto: no configuration directory keeps S3", env: []string{"SLIVINGDOC_BUCKET=notes"},
			wantBucket: "notes", wantFrom: bucketFromEnv,
		},
		{
			name: "auto: an endpoint without a login keeps S3", env: []string{notes, "SLIVINGDOC_BUCKET=notes"},
			args:       []string{"--endpoint", "https://s3.example.test"},
			wantBucket: "notes", wantFrom: bucketFromEnv, wantEndpoint: "https://s3.example.test",
		},
		{
			name: "auto: the same endpoint in another spelling uses the login", env: []string{dev, "SLIVINGDOC_BUCKET=notes", "SLIVINGDOC_ENDPOINT=https://API.dev.slivingdoc.dev/"},
			wantHosted: true, wantKey: loginToken, wantOrigin: originLogin, wantBucket: "notes", wantFrom: bucketFromEnv, wantEndpoint: devEndpoint,
		},
		{
			name: "s3: the token and the login are ignored", env: []string{token, notes, "SLIVINGDOC_BUCKET=notes"}, args: []string{"--storage", "s3"},
			wantBucket: "notes", wantFrom: bucketFromEnv,
		},
		{
			name: "s3 from the environment", env: []string{token, notes, "SLIVINGDOC_BUCKET=notes", "SLIVINGDOC_STORAGE=s3"},
			wantBucket: "notes", wantFrom: bucketFromEnv,
		},
		{
			name: "hosted: the login", env: []string{notes, "SLIVINGDOC_BUCKET=notes", "AWS_ACCESS_KEY_ID=k"}, args: []string{"--storage", "hosted"},
			wantHosted: true, wantKey: loginToken, wantOrigin: originLogin, wantBucket: "notes", wantFrom: bucketFromEnv, wantEndpoint: DefaultHostedEndpoint,
		},
		{
			name: "hosted: the token without a login", env: []string{token, "SLIVINGDOC_BUCKET=notes"}, args: []string{"--storage=hosted"},
			wantHosted: true, wantToken: hostedTestToken, wantOrigin: originEnv, wantBucket: "notes", wantFrom: bucketFromEnv, wantEndpoint: DefaultHostedEndpoint,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadConfig(testProcess(tt.env, tt.args...))
			if err != nil {
				t.Fatalf("loadConfig() = %v", err)
			}
			key := ""
			if cfg.login != nil {
				key = cfg.login.Key
			}
			if cfg.hosted() != tt.wantHosted || cfg.token != tt.wantToken || key != tt.wantKey || cfg.tokenOrigin != tt.wantOrigin ||
				cfg.bucket != tt.wantBucket || cfg.bucketFrom != tt.wantFrom || cfg.endpoint != tt.wantEndpoint {
				t.Fatalf("config = hosted %v origin %q bucket %q from %v endpoint %q token set %v key set %v; want hosted %v origin %q bucket %q from %v endpoint %q",
					cfg.hosted(), cfg.tokenOrigin, cfg.bucket, cfg.bucketFrom, cfg.endpoint, cfg.token != "", key != "",
					tt.wantHosted, tt.wantOrigin, tt.wantBucket, tt.wantFrom, tt.wantEndpoint)
			}
			if !tt.wantHosted && cfg.region == "" {
				t.Fatal("an S3 configuration has no region")
			}
		})
	}
}

func TestResolveStorageRefusals(t *testing.T) {
	notes := writeLogins(t, defaults(DefaultHostedEndpoint, "notes"), entry(DefaultHostedEndpoint, loginToken))
	noDefault := writeLogins(t, nil, entry(DefaultHostedEndpoint, loginToken))
	expiredEntry := entry(DefaultHostedEndpoint, loginToken)
	expiredEntry.ExpiresAt = longAgo
	expired := writeLogins(t, nil, expiredEntry)
	two := writeLogins(t, defaults(DefaultHostedEndpoint, "notes"), entry(DefaultHostedEndpoint, loginToken), entry(devEndpoint, otherToken))
	awsHome := awsHomeDir(t)
	malformed := writeRawLogins(t, []byte(`{"version":2,"logins":[{"key":"`+loginToken+`"}]}`))
	outdated := outdatedLogins(t)
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
			[]string{"a stored login and S3 settings (AWS_ACCESS_KEY_ID)", `for "notes"`, "--storage hosted", "--storage s3"},
		},
		{
			"a login and an AWS profile and endpoint are ambiguous",
			[]string{notes, "SLIVINGDOC_BUCKET=notes", "AWS_PROFILE=p", "AWS_ENDPOINT_URL_S3=https://s3.example.test"},
			nil,
			[]string{"AWS_PROFILE, AWS_ENDPOINT_URL_S3", "--storage hosted"},
		},
		{
			"hosted without a token or a login",
			[]string{empty, "SLIVINGDOC_BUCKET=notes"},
			[]string{"--storage", "hosted"},
			[]string{"needs SLIVINGDOC_TOKEN or a stored login", "run 'slivingdoc login'"},
		},
		{
			"a login without a default space",
			[]string{noDefault},
			nil,
			[]string{"has no default space", "'slivingdoc space <name>'", "--space", "--storage s3"},
		},
		{
			"hosted at an endpoint without a login",
			[]string{notes, "SLIVINGDOC_ENDPOINT=https://other.example.test"},
			[]string{"--storage", "hosted"},
			[]string{"for https://other.example.test; the stored login is for " + DefaultHostedEndpoint, "slivingdoc login"},
		},
		{
			"an expired login",
			[]string{expired, "SLIVINGDOC_BUCKET=notes"},
			nil,
			[]string{"expired 2001-01-01 00:00 UTC", "run 'slivingdoc login' again", "--storage s3"},
		},
		{
			"two logins and no endpoint",
			[]string{two},
			nil,
			[]string{"several stored logins", "pass --endpoint", "--storage s3"},
		},
		{
			"a malformed credentials file",
			[]string{malformed, "SLIVINGDOC_BUCKET=notes"},
			nil,
			[]string{"malformed credentials file", "--storage s3"},
		},
		{
			"an earlier build's credentials file",
			[]string{outdated, "SLIVINGDOC_BUCKET=notes"},
			nil,
			[]string{"from an earlier slivingdoc", "run 'slivingdoc login' again", "--storage s3"},
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
		{
			"auto S3 never takes the default space",
			[]string{notes},
			[]string{"--endpoint", "https://minio.local"},
			[]string{"bucket is required"},
		},
		{
			"a login and a generic AWS endpoint are ambiguous",
			[]string{notes, "SLIVINGDOC_BUCKET=notes", "AWS_ENDPOINT_URL=https://s3.example.test"},
			nil,
			[]string{"AWS_ENDPOINT_URL", "--storage s3"},
		},
		{
			"a login and a web identity are ambiguous",
			[]string{notes, "SLIVINGDOC_BUCKET=notes", "AWS_WEB_IDENTITY_TOKEN_FILE=/t", "AWS_SHARED_CREDENTIALS_FILE=/c"},
			nil,
			[]string{"AWS_SHARED_CREDENTIALS_FILE, AWS_WEB_IDENTITY_TOKEN_FILE"},
		},
		{
			"a login and every other AWS variable are ambiguous",
			[]string{
				notes, "SLIVINGDOC_BUCKET=notes", "AWS_SECRET_ACCESS_KEY=s", "AWS_SESSION_TOKEN=t", "AWS_DEFAULT_REGION=r",
				"AWS_CONFIG_FILE=/c", "AWS_ROLE_ARN=arn", "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI=/v2", "AWS_CONTAINER_CREDENTIALS_FULL_URI=http://x",
			},
			nil,
			[]string{"AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN, AWS_DEFAULT_REGION, AWS_CONFIG_FILE, AWS_ROLE_ARN, AWS_CONTAINER_CREDENTIALS_FULL_URI, AWS_CONTAINER_CREDENTIALS_RELATIVE_URI"},
		},
		{
			"a login and the S3-only flags are ambiguous",
			[]string{notes, "SLIVINGDOC_BUCKET=notes"},
			[]string{"--region", "eu-north-1", "--path-style"},
			[]string{"(--region, --path-style)"},
		},
		{
			"a login and path-style from the environment are ambiguous",
			[]string{notes, "SLIVINGDOC_BUCKET=notes", "SLIVINGDOC_PATH_STYLE=true"},
			nil,
			[]string{"SLIVINGDOC_PATH_STYLE"},
		},
		{
			"a login and the shared AWS files are ambiguous",
			[]string{notes, "HOME=" + awsHome},
			[]string{"--bucket", "notes"},
			[]string{"~/.aws/credentials, ~/.aws/config"},
		},
		{
			"the token and an endpoint flag are ambiguous",
			[]string{"SLIVINGDOC_TOKEN=" + hostedTestToken, "SLIVINGDOC_BUCKET=notes"},
			[]string{"--endpoint", "https://minio.local"},
			[]string{"SLIVINGDOC_TOKEN and an S3 endpoint (--endpoint)", "--storage hosted", "--storage s3"},
		},
		{
			"the token and the AWS endpoint variables are ambiguous",
			[]string{"SLIVINGDOC_TOKEN=" + hostedTestToken, "SLIVINGDOC_BUCKET=notes", "AWS_ENDPOINT_URL=https://minio.local", "AWS_ENDPOINT_URL_S3=https://minio.local", "AWS_REGION=eu-north-1"},
			nil,
			[]string{"SLIVINGDOC_TOKEN and an S3 endpoint (AWS_ENDPOINT_URL, AWS_ENDPOINT_URL_S3)"},
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
				t.Fatalf("loadConfig() = %q echoes a key", err)
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

func TestS3SignalsFindTheSharedAWSFiles(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".aws"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".aws", "config"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		env  map[string]string
		in   storageInputs
		want []string
	}{
		{"HOME", map[string]string{"HOME": home}, storageInputs{goos: "linux"}, []string{"~/.aws/config"}},
		{"USERPROFILE on Windows, any case", map[string]string{"userprofile": home, "HOME": "/nowhere"}, storageInputs{goos: "windows"}, []string{"~/.aws/config"}},
		{"a relative HOME", map[string]string{"HOME": "home"}, storageInputs{goos: "linux"}, nil},
		{"no home", nil, storageInputs{goos: "linux"}, nil},
		{"a failing stat", map[string]string{"HOME": home}, storageInputs{goos: "linux", stat: func(string) (fs.FileInfo, error) {
			return nil, fs.ErrPermission
		}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s3Signals(&Flags{}, tt.env, tt.in); !slices.Equal(got, tt.want) {
				t.Fatalf("s3Signals() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestResolveBucketSpellings proves --space and SLIVINGDOC_SPACE are the
// same setting as --bucket and SLIVINGDOC_BUCKET: a flag beats the
// environment, an explicitly empty flag does not fall back, the spelling
// used is recorded, and two spellings of one layer must agree.
func TestResolveBucketSpellings(t *testing.T) {
	for _, row := range []struct {
		name     string
		args     []string
		env      []string
		want     string
		wantFrom bucketSource
		wantErr  string
	}{
		{name: "--space", args: []string{"--space", "notes"}, want: "notes", wantFrom: bucketFromSpaceFlag},
		{name: "--bucket", args: []string{"--bucket", "notes"}, want: "notes", wantFrom: bucketFromFlag},
		{name: "both flags agreeing", args: []string{"--bucket", "notes", "--space", "notes"}, want: "notes", wantFrom: bucketFromSpaceFlag},
		{name: "both flags differing", args: []string{"--bucket", "notes", "--space", "other"}, wantErr: `--bucket "notes" and --space "other" name different spaces`},
		{name: "an empty --space against a --bucket", args: []string{"--bucket", "notes", "--space", ""}, wantErr: `--bucket "notes" and --space "" name different spaces`},
		{name: "SLIVINGDOC_SPACE", env: []string{"SLIVINGDOC_SPACE=notes"}, want: "notes", wantFrom: bucketFromSpaceEnv},
		{name: "SLIVINGDOC_BUCKET", env: []string{"SLIVINGDOC_BUCKET=notes"}, want: "notes", wantFrom: bucketFromEnv},
		{name: "both variables agreeing", env: []string{"SLIVINGDOC_BUCKET=notes", "SLIVINGDOC_SPACE=notes"}, want: "notes", wantFrom: bucketFromSpaceEnv},
		{name: "both variables differing", env: []string{"SLIVINGDOC_BUCKET=notes", "SLIVINGDOC_SPACE=other"}, wantErr: `SLIVINGDOC_BUCKET "notes" and SLIVINGDOC_SPACE "other" name different spaces`},
		{name: "a flag beats the variables", args: []string{"--space", "flag"}, env: []string{"SLIVINGDOC_BUCKET=notes", "SLIVINGDOC_SPACE=other"}, want: "flag", wantFrom: bucketFromSpaceFlag},
		{name: "--bucket beats SLIVINGDOC_SPACE", args: []string{"--bucket", "flag"}, env: []string{"SLIVINGDOC_SPACE=other"}, want: "flag", wantFrom: bucketFromFlag},
		{name: "an empty --space does not fall back", args: []string{"--space", ""}, env: []string{"SLIVINGDOC_SPACE=notes"}, want: "", wantFrom: bucketNone},
		{name: "an empty --bucket does not fall back", args: []string{"--bucket="}, env: []string{"SLIVINGDOC_SPACE=notes"}, want: "", wantFrom: bucketNone},
		{name: "nothing", want: "", wantFrom: bucketNone},
	} {
		t.Run(row.name, func(t *testing.T) {
			f := NewFlags()
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			f.Bind(fs)
			if err := fs.Parse(row.args); err != nil {
				t.Fatal(err)
			}
			got, from, err := resolveBucket(f, environ(row.env))
			if row.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), row.wantErr) {
					t.Fatalf("resolveBucket() = %v, want it to contain %q", err, row.wantErr)
				}
				if _, err := loadConfig(testProcess(row.env, row.args...)); err == nil || !strings.Contains(err.Error(), row.wantErr) {
					t.Fatalf("loadConfig() = %v, want the same refusal", err)
				}
				return
			}
			if err != nil || got != row.want || from != row.wantFrom {
				t.Fatalf("resolveBucket() = %q from %v, %v; want %q from %v", got, from, err, row.want, row.wantFrom)
			}
		})
	}
	for from, want := range map[bucketSource]string{
		bucketFromFlag: "--bucket", bucketFromSpaceFlag: "--space", bucketFromEnv: "SLIVINGDOC_BUCKET",
		bucketFromSpaceEnv: "SLIVINGDOC_SPACE", bucketFromLogin: "default space", bucketFromToken: "token",
		bucketNone: "none", bucketCleared: "none",
	} {
		if from.String() != want {
			t.Fatalf("%d.String() = %q, want %q", int(from), from, want)
		}
	}
}
