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
		checkpointPacks:     256,
		retainedCheckpoints: 1,
		logTimestamp:        true,
	}
	if cfg != want {
		t.Fatalf("config = %+v, want %+v", cfg, want)
	}
}

// ephemeralProcess is testProcess with the serve command's ephemeral
// default and a deterministic session directory.
func ephemeralProcess(session string, env []string, args ...string) process {
	p := testProcess(env, args...)
	p.ephemeral = true
	p.newSessionDir = func() (string, error) { return session, nil }
	return p
}

// TestLoadConfigEphemeralRoots proves the transparent serve default: with
// no configured root the process owns one session directory holding both
// roots as siblings, so the roots cannot overlap and no two servers can
// select the same private state.
func TestLoadConfigEphemeralRoots(t *testing.T) {
	session := t.TempDir()
	cfg, err := loadConfig(ephemeralProcess(session, []string{"SLIVINGDOC_BUCKET=my-bucket"}))
	if err != nil {
		t.Fatalf("loadConfig() = %v", err)
	}
	if cfg.sessionDir != session {
		t.Fatalf("sessionDir = %q, want %q", cfg.sessionDir, session)
	}
	if want := filepath.Join(session, "notebook"); cfg.workspaceRoot != want {
		t.Fatalf("workspaceRoot = %q, want %q", cfg.workspaceRoot, want)
	}
	if want := filepath.Join(session, "private"); cfg.privateRoot != want {
		t.Fatalf("privateRoot = %q, want %q", cfg.privateRoot, want)
	}
}

// TestLoadConfigEphemeralYieldsToConfiguredRoots proves that configuring
// either root keeps the shared-directory behaviour: an operator who names a
// workspace root never gets a temporary one, and the private root falls
// back to the user cache directory as before.
func TestLoadConfigEphemeralYieldsToConfiguredRoots(t *testing.T) {
	session := t.TempDir()
	for _, row := range []struct {
		name        string
		env         []string
		wantWs      string
		wantPrivate string
	}{
		{
			name:        "workspace root flag",
			env:         []string{"SLIVINGDOC_BUCKET=b", "SLIVINGDOC_WORKSPACE_ROOT=/notes"},
			wantWs:      "/notes",
			wantPrivate: "/cache/slivingdoc",
		},
		{
			name:        "private root only",
			env:         []string{"SLIVINGDOC_BUCKET=b", "SLIVINGDOC_PRIVATE_ROOT=/state"},
			wantWs:      "/work",
			wantPrivate: "/state",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			cfg, err := loadConfig(ephemeralProcess(session, row.env))
			if err != nil {
				t.Fatalf("loadConfig() = %v", err)
			}
			if cfg.sessionDir != "" {
				t.Fatalf("sessionDir = %q, want no session directory", cfg.sessionDir)
			}
			if cfg.workspaceRoot != row.wantWs || cfg.privateRoot != row.wantPrivate {
				t.Fatalf("roots = %q/%q, want %q/%q",
					cfg.workspaceRoot, cfg.privateRoot, row.wantWs, row.wantPrivate)
			}
		})
	}
}

// TestLoadConfigEphemeralEmptyFlagStillRefuses proves an explicitly empty
// root is a refusal, not a silent fall-through to the temporary default.
func TestLoadConfigEphemeralEmptyFlagStillRefuses(t *testing.T) {
	p := ephemeralProcess(t.TempDir(), []string{"SLIVINGDOC_BUCKET=b"}, "--workspace-root=")
	if _, err := loadConfig(p); err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("loadConfig() = %v, want the empty-root refusal", err)
	}
}

// TestLoadConfigRefusalRemovesSessionDir proves a startup refusal leaves
// nothing behind: the session directory is created while resolving, before
// the bucket, prefix, endpoint, and numeric rules can refuse, so the
// creator removes it again on every failure path.
func TestLoadConfigRefusalRemovesSessionDir(t *testing.T) {
	for _, row := range []struct {
		name string
		env  []string
	}{
		{name: "missing bucket", env: nil},
		{name: "invalid prefix", env: []string{"SLIVINGDOC_BUCKET=b", "SLIVINGDOC_PREFIX=../escape"}},
		{name: "invalid endpoint", env: []string{"SLIVINGDOC_BUCKET=b", "AWS_ENDPOINT_URL_S3=ftp://example.invalid"}},
		{name: "invalid integer", env: []string{"SLIVINGDOC_BUCKET=b", "SLIVINGDOC_COMMIT_RETRIES=-1"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			session := filepath.Join(t.TempDir(), "session")
			if err := os.MkdirAll(session, 0o700); err != nil {
				t.Fatalf("MkdirAll() = %v", err)
			}
			if _, err := loadConfig(ephemeralProcess(session, row.env)); err == nil {
				t.Fatal("loadConfig() = nil, want a configuration refusal")
			}
			if _, err := os.Stat(session); !os.IsNotExist(err) {
				t.Fatalf("Stat(session) = %v, want the session directory removed", err)
			}
		})
	}
}

// TestRemoveSessionDirIsScoped proves the shutdown cleanup removes only the
// process-owned session directory and tolerates an empty one, which is what
// every configured-root run passes.
func TestRemoveSessionDirIsScoped(t *testing.T) {
	if err := removeSessionDir(""); err != nil {
		t.Fatalf("removeSessionDir(\"\") = %v", err)
	}
	session := filepath.Join(t.TempDir(), "session")
	if err := os.MkdirAll(filepath.Join(session, "notebook"), 0o700); err != nil {
		t.Fatalf("MkdirAll() = %v", err)
	}
	if err := removeSessionDir(session); err != nil {
		t.Fatalf("removeSessionDir() = %v", err)
	}
	if _, err := os.Stat(session); !os.IsNotExist(err) {
		t.Fatalf("Stat(session) = %v, want it removed", err)
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

// TestLoadConfigLogSettings proves the logging knobs resolve with the
// documented precedence: the flag beats the environment, the timestamp
// defaults to true, and only an explicit flag or SLIVINGDOC_LOG_TIMESTAMP
// marks the logging as configured — LOG_LEVEL alone already reached the
// pre-parse logger, so it must not trigger a rebuild.
func TestLoadConfigLogSettings(t *testing.T) {
	cases := []struct {
		name           string
		env            []string
		args           []string
		wantLevel      string
		wantTimestamp  bool
		wantConfigured bool
	}{
		{name: "defaults", wantTimestamp: true},
		{
			name: "env level alone is not configured",
			env:  []string{"LOG_LEVEL=mcp=debug"}, wantLevel: "mcp=debug", wantTimestamp: true,
		},
		{
			name: "flag level beats env and configures",
			env:  []string{"LOG_LEVEL=warn"}, args: []string{"--log-level", "mcp=debug"},
			wantLevel: "mcp=debug", wantTimestamp: true, wantConfigured: true,
		},
		{
			name: "timestamp flag disables",
			args: []string{"--log-timestamp=false"}, wantConfigured: true,
		},
		{
			name: "timestamp env disables",
			env:  []string{"SLIVINGDOC_LOG_TIMESTAMP=false"}, wantConfigured: true,
		},
		{
			name: "timestamp flag beats env",
			env:  []string{"SLIVINGDOC_LOG_TIMESTAMP=false"}, args: []string{"--log-timestamp=true"},
			wantTimestamp: true, wantConfigured: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			env := append([]string{"SLIVINGDOC_BUCKET=b"}, tt.env...)
			cfg, err := loadConfig(testProcess(env, tt.args...))
			if err != nil {
				t.Fatalf("loadConfig() = %v", err)
			}
			if cfg.logLevel != tt.wantLevel {
				t.Fatalf("logLevel = %q, want %q", cfg.logLevel, tt.wantLevel)
			}
			if cfg.logTimestamp != tt.wantTimestamp {
				t.Fatalf("logTimestamp = %v, want %v", cfg.logTimestamp, tt.wantTimestamp)
			}
			if cfg.logConfigured != tt.wantConfigured {
				t.Fatalf("logConfigured = %v, want %v", cfg.logConfigured, tt.wantConfigured)
			}
		})
	}
}

// TestLoadConfigInvalidLogValues proves an explicit flag value fails fast
// like every other flag, an invalid timestamp variable is refused like
// every other boolean variable, and a malformed LOG_LEVEL environment
// value keeps its documented lenient fallback.
func TestLoadConfigInvalidLogValues(t *testing.T) {
	if _, err := loadConfig(testProcess([]string{"SLIVINGDOC_BUCKET=b"}, "--log-level", "cli=verbose")); err == nil {
		t.Fatal("loadConfig(--log-level cli=verbose) = nil, want an invalid-level error")
	}
	if _, err := loadConfig(testProcess([]string{"SLIVINGDOC_BUCKET=b", "SLIVINGDOC_LOG_TIMESTAMP=nope"})); err == nil {
		t.Fatal("loadConfig(SLIVINGDOC_LOG_TIMESTAMP=nope) = nil, want an invalid-boolean error")
	}
	if _, err := loadConfig(testProcess([]string{"SLIVINGDOC_BUCKET=b", "LOG_LEVEL=cli=verbose"})); err != nil {
		t.Fatalf("loadConfig(malformed LOG_LEVEL env) = %v, want the lenient fallback", err)
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

// TestLoadConfigSharedPackCache proves the shared pack-cache resolution:
// off by default, enabled by the flag or the environment (flag wins), the
// root below the user cache directory, and the refusals — no user cache
// directory, an invalid boolean, and a root at or below the workspace root.
func TestLoadConfigSharedPackCache(t *testing.T) {
	base := "SLIVINGDOC_BUCKET=my-bucket"
	sharedRoot := "/cache/slivingdoc/pack-cache"

	cfg, err := loadConfig(testProcess([]string{base}))
	if err != nil {
		t.Fatalf("loadConfig() = %v", err)
	}
	if cfg.packCacheRoot != "" {
		t.Fatalf("default packCacheRoot = %q, want the private per-workspace cache", cfg.packCacheRoot)
	}

	cfg, err = loadConfig(testProcess([]string{base}, "--shared-pack-cache"))
	if err != nil {
		t.Fatalf("loadConfig(--shared-pack-cache) = %v", err)
	}
	if cfg.packCacheRoot != sharedRoot {
		t.Fatalf("flag packCacheRoot = %q, want %q", cfg.packCacheRoot, sharedRoot)
	}

	cfg, err = loadConfig(testProcess([]string{base, "SLIVINGDOC_SHARED_PACK_CACHE=true"}))
	if err != nil {
		t.Fatalf("loadConfig(env) = %v", err)
	}
	if cfg.packCacheRoot != sharedRoot {
		t.Fatalf("env packCacheRoot = %q, want %q", cfg.packCacheRoot, sharedRoot)
	}

	cfg, err = loadConfig(testProcess([]string{base, "SLIVINGDOC_SHARED_PACK_CACHE=true"}, "--shared-pack-cache=false"))
	if err != nil {
		t.Fatalf("loadConfig(flag over env) = %v", err)
	}
	if cfg.packCacheRoot != "" {
		t.Fatalf("explicit false packCacheRoot = %q, want the private per-workspace cache", cfg.packCacheRoot)
	}

	if _, err := loadConfig(testProcess([]string{base, "SLIVINGDOC_SHARED_PACK_CACHE=banana"})); err == nil {
		t.Fatal("loadConfig(invalid boolean) = nil, want an error")
	}

	p := testProcess([]string{base}, "--shared-pack-cache")
	p.cacheDir = ""
	if _, err := loadConfig(p); err == nil || !strings.Contains(err.Error(), "user cache directory") {
		t.Fatalf("loadConfig(no cache dir) = %v, want the user-cache-directory refusal", err)
	}

	_, err = loadConfig(testProcess([]string{base}, "--shared-pack-cache", "--workspace-root", sharedRoot))
	if err == nil || !strings.Contains(err.Error(), "pack cache root") {
		t.Fatalf("loadConfig(overlap) = %v, want the overlapping pack-cache-root refusal", err)
	}
}

// TestLoadConfigSharedPackCacheEphemeral proves the multi-agent use case:
// an ephemeral session keeps its temporary roots while the pack cache root
// resolves below the durable user cache directory, so agents with private
// temporary state still share downloaded packs.
func TestLoadConfigSharedPackCacheEphemeral(t *testing.T) {
	session := t.TempDir()
	cfg, err := loadConfig(ephemeralProcess(session, []string{
		"SLIVINGDOC_BUCKET=my-bucket",
		"SLIVINGDOC_SHARED_PACK_CACHE=true",
	}))
	if err != nil {
		t.Fatalf("loadConfig() = %v", err)
	}
	if want := filepath.Join(session, "private"); cfg.privateRoot != want {
		t.Fatalf("privateRoot = %q, want the session-private %q", cfg.privateRoot, want)
	}
	if want := "/cache/slivingdoc/pack-cache"; cfg.packCacheRoot != want {
		t.Fatalf("packCacheRoot = %q, want the durable %q", cfg.packCacheRoot, want)
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
