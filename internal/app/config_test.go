package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
)

// testProcess builds a deterministic process environment for config tests:
// a stable working directory, empty environment, a fake engine, and a fake
// store factory.
func testProcess(env []string, args ...string) process {
	return process{
		args:     args,
		env:      env,
		cwd:      "/work",
		cacheDir: "/cache",
		engine:   &fakeEngine{},
		stdout:   discardWriter{},
		stderr:   discardWriter{},
		signals:  make(chan os.Signal, 1),
		storeFactory: func(context.Context, config) (storage.ObjectStore, error) {
			return fake.New(""), nil
		},
	}
}

// discardWriter is an io.Writer that drops everything, so process tests
// never touch real descriptors.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := loadConfig(testProcess([]string{
		"SLIVINGDOC_BUCKET=my-bucket",
	}))
	if err != nil {
		t.Fatalf("loadConfig() = %v", err)
	}
	want := config{
		bucket:              "my-bucket",
		prefix:              "slivingdoc",
		region:              "us-east-1",
		endpoint:            "",
		pathStyle:           false,
		workspaceRoot:       "/work",
		privateRoot:         "/cache/slivingdoc",
		commitRetries:       8,
		checkpointPacks:     1024,
		retainedCheckpoints: 1,
	}
	if cfg != want {
		t.Fatalf("config = %+v, want %+v", cfg, want)
	}
}

// TestLoadConfigFlagOverEnvOverDefault proves the precedence rule: flags
// override environment variables, which override defaults.
func TestLoadConfigFlagOverEnvOverDefault(t *testing.T) {
	cfg, err := loadConfig(testProcess([]string{
		"SLIVINGDOC_BUCKET=env-bucket",
		"SLIVINGDOC_COMMIT_RETRIES=99",
		"SLIVINGDOC_CHECKPOINT_PACKS=7",
		"SLIVINGDOC_RETAINED_CHECKPOINTS=3",
		"SLIVINGDOC_PATH_STYLE=true",
	}, "--bucket", "flag-bucket", "--commit-retries", "3", "--checkpoint-packs", "5"))
	if err != nil {
		t.Fatalf("loadConfig() = %v", err)
	}
	if cfg.bucket != "flag-bucket" {
		t.Fatalf("bucket = %q, want the flag value", cfg.bucket)
	}
	if cfg.commitRetries != 3 {
		t.Fatalf("commit retries = %d, want the flag value 3", cfg.commitRetries)
	}
	if cfg.checkpointPacks != 5 {
		t.Fatalf("checkpoint packs = %d, want the flag value 5", cfg.checkpointPacks)
	}
	if cfg.retainedCheckpoints != 3 {
		t.Fatalf("retained checkpoints = %d, want the env value 3", cfg.retainedCheckpoints)
	}
	if !cfg.pathStyle {
		t.Fatal("path style = false, want the env value true")
	}
}

// TestLoadConfigEmptyFlagDoesNotFallBackToEnv proves that an explicitly
// empty flag value does not fall back to an environment value.
func TestLoadConfigEmptyFlagDoesNotFallBackToEnv(t *testing.T) {
	_, err := loadConfig(testProcess([]string{
		"SLIVINGDOC_BUCKET=env-bucket",
	}, "--bucket="))
	if err == nil {
		t.Fatal("loadConfig() = nil, want the required-bucket error")
	}
}

func TestLoadConfigBucketRequired(t *testing.T) {
	for _, p := range []process{
		testProcess(nil),
		testProcess([]string{"SLIVINGDOC_BUCKET="}),
	} {
		_, err := loadConfig(p)
		if err == nil {
			t.Fatalf("loadConfig(%q) = nil, want a required-bucket error", p.env)
		}
	}
}

func TestLoadConfigRegionRequired(t *testing.T) {
	_, err := loadConfig(testProcess([]string{
		"SLIVINGDOC_BUCKET=bucket",
	}, "--region="))
	if err == nil {
		t.Fatal("loadConfig() = nil, want a region error")
	}
}

// TestLoadConfigNumericBounds proves the documented ranges and that
// integer values never accept a sign.
func TestLoadConfigNumericBounds(t *testing.T) {
	cases := []struct {
		name string
		args []string
		env  string
		want bool // true when the configuration is valid
	}{
		{name: "commit retries 100", args: []string{"--commit-retries", "100"}, want: true},
		{name: "commit retries 101", args: []string{"--commit-retries", "101"}, want: false},
		{name: "commit retries negative", args: []string{"--commit-retries", "-1"}, want: false},
		{name: "commit retries plus sign", args: []string{"--commit-retries", "+1"}, want: false},
		{name: "checkpoint packs 1", args: []string{"--checkpoint-packs", "1"}, want: true},
		{name: "checkpoint packs 0", args: []string{"--checkpoint-packs", "0"}, want: false},
		{name: "retained 0", args: []string{"--retained-checkpoints", "0"}, want: true},
		{name: "retained 64", args: []string{"--retained-checkpoints", "64"}, want: true},
		{name: "retained 65", args: []string{"--retained-checkpoints", "65"}, want: false},
		{name: "retained negative", args: []string{"--retained-checkpoints", "-1"}, want: false},
		{name: "env commit retries 5", env: "SLIVINGDOC_COMMIT_RETRIES=5", want: true},
		{name: "env commit retries negative", env: "SLIVINGDOC_COMMIT_RETRIES=-5", want: false},
		{name: "env checkpoint packs 0", env: "SLIVINGDOC_CHECKPOINT_PACKS=0", want: false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			env := []string{"SLIVINGDOC_BUCKET=bucket"}
			if tt.env != "" {
				env = append(env, tt.env)
			}
			_, err := loadConfig(testProcess(env, tt.args...))
			if got := err == nil; got != tt.want {
				t.Fatalf("loadConfig() error = %v, valid = %v, want %v", err, got, tt.want)
			}
		})
	}
}

// TestLoadConfigEndpointNormalization proves the endpoint contract: scheme
// and host lowercased, trailing slash removed, non-root path preserved,
// and user information, query, and fragment rejected.
func TestLoadConfigEndpointNormalization(t *testing.T) {
	cases := []struct {
		name  string
		env   string
		want  string
		valid bool
	}{
		{name: "empty stays empty", env: "", want: "", valid: true},
		{name: "lowercase and trailing slash", env: "HTTPS://Example.COM:8333/", want: "https://example.com:8333", valid: true},
		{name: "non-root path preserved", env: "http://s3.local:8333/s3/", want: "http://s3.local:8333/s3", valid: true},
		{name: "user information rejected", env: "http://user:pass@host:8333", want: "", valid: false},
		{name: "query rejected", env: "http://host:8333?x=1", want: "", valid: false},
		{name: "fragment rejected", env: "http://host:8333#f", want: "", valid: false},
		{name: "relative rejected", env: "host:8333", want: "", valid: false},
		{name: "ftp rejected", env: "ftp://host", want: "", valid: false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			env := []string{"SLIVINGDOC_BUCKET=bucket"}
			if tt.env != "" {
				env = append(env, "AWS_ENDPOINT_URL_S3="+tt.env)
			}
			cfg, err := loadConfig(testProcess(env))
			if tt.valid {
				if err != nil {
					t.Fatalf("loadConfig() = %v", err)
				}
				if cfg.endpoint != tt.want {
					t.Fatalf("endpoint = %q, want %q", cfg.endpoint, tt.want)
				}
			} else if err == nil {
				t.Fatal("loadConfig() = nil, want an endpoint error")
			}
		})
	}
}

// TestLoadConfigRootsBecomeAbsolute proves that relative roots resolve
// against the working directory and that an overlapping private root is
// rejected.
func TestLoadConfigRootsBecomeAbsolute(t *testing.T) {
	cfg, err := loadConfig(testProcess([]string{
		"SLIVINGDOC_BUCKET=bucket",
	}, "--workspace-root", "notes", "--private-root", "priv"))
	if err != nil {
		t.Fatalf("loadConfig() = %v", err)
	}
	if cfg.workspaceRoot != "/work/notes" {
		t.Fatalf("workspace root = %q, want /work/notes", cfg.workspaceRoot)
	}
	if cfg.privateRoot != "/work/priv" {
		t.Fatalf("private root = %q, want /work/priv", cfg.privateRoot)
	}

	_, err = loadConfig(testProcess([]string{
		"SLIVINGDOC_BUCKET=bucket",
	}, "--workspace-root", "/work", "--private-root", "/work/notes"))
	if err == nil {
		t.Fatal("loadConfig() = nil, want the overlapping-roots error")
	}
}

func TestLoadConfigExpandsHomeRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg, err := loadConfig(testProcess([]string{
		"SLIVINGDOC_BUCKET=bucket",
	}, "--workspace-root", "~/notes", "--private-root", "~/private"))
	if err != nil {
		t.Fatalf("loadConfig() = %v", err)
	}
	if want := filepath.Join(home, "notes"); cfg.workspaceRoot != want {
		t.Fatalf("workspace root = %q, want %q", cfg.workspaceRoot, want)
	}
	if want := filepath.Join(home, "private"); cfg.privateRoot != want {
		t.Fatalf("private root = %q, want %q", cfg.privateRoot, want)
	}
}

// TestLoadConfigUnknownFlagRejected proves that an unknown flag is a
// configuration error, not help.
func TestLoadConfigUnknownFlagRejected(t *testing.T) {
	_, err := loadConfig(testProcess(nil, "--frobnicate"))
	if err == nil {
		t.Fatal("loadConfig() = nil, want an unknown-flag error")
	}
}

// TestConfigErrorNeverEchoesEndpoint proves that an endpoint with user
// information fails without echoing the credential in the diagnostic.
func TestConfigErrorNeverEchoesEndpoint(t *testing.T) {
	_, err := loadConfig(testProcess([]string{
		"SLIVINGDOC_BUCKET=bucket",
		"AWS_ENDPOINT_URL_S3=http://user:supersecret@host:8333",
	}))
	if err == nil {
		t.Fatal("loadConfig() = nil, want an endpoint error")
	}
	if strings.Contains(err.Error(), "supersecret") || strings.Contains(err.Error(), "user:pass") {
		t.Fatalf("diagnostic leaks the endpoint credential: %v", err)
	}
}

// TestConfigErrorIsRedacted proves the redaction of the run-level
// diagnostic: run wraps config errors in the redactor.
func TestConfigErrorIsRedacted(t *testing.T) {
	var out discardWriter
	p := testProcess([]string{
		"SLIVINGDOC_BUCKET=bucket",
		"AWS_ENDPOINT_URL_S3=http://user:supersecret@host:8333",
	})
	p.stdout = &out
	err := run(p)
	if err == nil {
		t.Fatal("run() = nil, want a configuration error")
	}
	if strings.Contains(err.Error(), "supersecret") {
		t.Fatalf("run diagnostic leaks the endpoint credential: %v", err)
	}
}
