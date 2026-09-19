package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/git"
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
		readOnlyPaths:       []string{},
		writablePaths:       []string{},
		logTimestamp:        true,
	}
	// config holds a slice, so compare with reflect.DeepEqual.
	if !reflect.DeepEqual(cfg, want) {
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
		{name: "invalid read-only path", env: []string{"SLIVINGDOC_BUCKET=b", "SLIVINGDOC_READ_ONLY_PATHS=.."}},
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

	// An explicitly empty flag must clear an inherited environment value.
	cfg, err := loadConfig(testProcess([]string{
		"SLIVINGDOC_BUCKET=b", "SLIVINGDOC_READ_ONLY_PATHS=docs",
	}, "--read-only-paths="))
	if err != nil {
		t.Fatalf("loadConfig() = %v", err)
	}
	if got := cfg.readOnlyPaths; len(got) != 0 {
		t.Fatalf("readOnlyPaths = %v, want the empty set, not the inherited environment value", got)
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

// TestLoadConfigReadOnlyPaths checks precedence, splitting, trimming, and
// empty-piece rules (architecture section 17).
func TestLoadConfigReadOnlyPaths(t *testing.T) {
	cases := []struct {
		name string
		env  []string
		args []string
		want []string
	}{
		{name: "default is the empty set", want: []string{}},
		{
			name: "environment sets the set",
			env:  []string{"SLIVINGDOC_READ_ONLY_PATHS=docs"},
			want: []string{"docs"},
		},
		{
			name: "flag wins over environment",
			env:  []string{"SLIVINGDOC_READ_ONLY_PATHS=docs"},
			args: []string{"--read-only-paths=notes"},
			want: []string{"notes"},
		},
		{
			name: "explicitly empty flag beats an inherited environment value",
			env:  []string{"SLIVINGDOC_READ_ONLY_PATHS=docs"},
			args: []string{"--read-only-paths="},
			want: []string{},
		},
		{
			name: "splitting, trimming, and dropping empty pieces; nested entries collapse",
			args: []string{"--read-only-paths=notes,docs/,docs/faq.md, ,"},
			want: []string{"docs", "notes"},
		},
		{
			name: "a malformed environment value never parses when the flag wins",
			env:  []string{"SLIVINGDOC_READ_ONLY_PATHS=.."},
			args: []string{"--read-only-paths=docs"},
			want: []string{"docs"},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			env := append([]string{"SLIVINGDOC_BUCKET=b"}, tt.env...)
			cfg, err := loadConfig(testProcess(env, tt.args...))
			if err != nil {
				t.Fatalf("loadConfig() = %v", err)
			}
			if !reflect.DeepEqual(cfg.readOnlyPaths, tt.want) {
				t.Fatalf("readOnlyPaths = %v, want %v", cfg.readOnlyPaths, tt.want)
			}
		})
	}
}

// TestLoadConfigReadOnlyPathsInvalid checks every invalid entry refuses startup
// with a diagnostic naming the entry and no package prefix.
func TestLoadConfigReadOnlyPathsInvalid(t *testing.T) {
	overLimit := make([]string, 0, 2050)
	for range 2050 {
		overLimit = append(overLimit, "a")
	}
	cases := []struct {
		name  string
		value string
		want  string // exact text after "read-only paths: "; empty skips the exact check
	}{
		{
			name:  "dot-dot segment",
			value: "..",
			want:  `invalid read-only path "..": invalid path "..": ".." segment is not allowed`,
		},
		{
			name:  "absolute path",
			value: "/abs",
			want:  `invalid read-only path "/abs": invalid path "/abs": must not start or end with a slash`,
		},
		{name: "git segment", value: "docs/.git"},
		{name: "over the path byte bound", value: strings.Join(overLimit, "/")},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadConfig(testProcess([]string{"SLIVINGDOC_BUCKET=b"}, "--read-only-paths="+tt.value))
			if err == nil {
				t.Fatalf("loadConfig(--read-only-paths=%s) = nil, want a refusal", tt.name)
			}
			const prefix = "read-only paths: "
			if !strings.Contains(err.Error(), "read-only paths:") {
				t.Fatalf("loadConfig() error = %v, want it prefixed %q", err, "read-only paths:")
			}
			if strings.Contains(err.Error(), "git:") {
				t.Fatalf("loadConfig() error = %v, want no `git:` package prefix", err)
			}
			if tt.want != "" {
				if got := strings.TrimPrefix(err.Error(), prefix); got != tt.want {
					t.Fatalf("loadConfig() error = %v, want %q%s", err, prefix, tt.want)
				}
			}
		})
	}
}

// refusingEngine fails on every call and records that it was touched. A
// startup refusal that happens before the engine opens therefore fails the
// test rather than passing quietly.
type refusingEngine struct{ touched bool }

var errEngineMustNotBeTouched = errors.New("app test: the native engine must not be touched")

func (e *refusingEngine) Open() error  { e.touched = true; return errEngineMustNotBeTouched }
func (e *refusingEngine) Close() error { e.touched = true; return errEngineMustNotBeTouched }

func (e *refusingEngine) Version() (string, error) {
	e.touched = true
	return "", errEngineMustNotBeTouched
}

func (e *refusingEngine) Features() (git.Features, error) {
	e.touched = true
	return git.Features{}, errEngineMustNotBeTouched
}

func (e *refusingEngine) CreateRepo(string) (git.Repository, error) {
	e.touched = true
	return nil, errEngineMustNotBeTouched
}

func (e *refusingEngine) OpenRepo(string) (git.Repository, error) {
	e.touched = true
	return nil, errEngineMustNotBeTouched
}

// refusingProcess is testProcess with an engine and a store factory that
// fail when they are called at all, so the ordering of a startup refusal is
// asserted directly rather than inferred.
func refusingProcess(env []string, args ...string) (process, *refusingEngine, *bool) {
	p := testProcess(env, args...)
	engine := &refusingEngine{}
	built := false
	p.engine = engine
	p.storeFactory = func(context.Context, config) (storage.ObjectStore, error) {
		built = true
		return nil, errors.New("app test: the object store must not be constructed")
	}
	return p, engine, &built
}

// TestFlagsWritablePathsResolution checks the documented precedence of the
// writable set: the flag beats the environment, which beats the empty
// default (architecture section 17).
func TestFlagsWritablePathsResolution(t *testing.T) {
	for _, tt := range []struct {
		name string
		env  []string
		args []string
		want []string
	}{
		{
			name: "flag set, environment set",
			env:  []string{"SLIVINGDOC_WRITABLE_PATHS=docs"},
			args: []string{"--writable-paths=notes"},
			want: []string{"notes"},
		},
		{
			name: "flag unset, environment set",
			env:  []string{"SLIVINGDOC_WRITABLE_PATHS=docs"},
			want: []string{"docs"},
		},
		{
			name: "flag unset, environment unset",
			want: []string{},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := append([]string{"SLIVINGDOC_BUCKET=b"}, tt.env...)
			cfg, err := loadConfig(testProcess(env, tt.args...))
			if err != nil {
				t.Fatalf("loadConfig() = %v", err)
			}
			if !reflect.DeepEqual(cfg.writablePaths, tt.want) {
				t.Fatalf("writablePaths = %v, want %v", cfg.writablePaths, tt.want)
			}
		})
	}
}

// TestFlagsWritablePathsExplicitEmptyIgnoresEnvironment checks the idiom a
// caller uses to defeat an inherited environment value: an explicitly empty
// flag resolves to the empty set instead of falling through to the
// environment, which would silently confine a process that asked not to be.
func TestFlagsWritablePathsExplicitEmptyIgnoresEnvironment(t *testing.T) {
	cfg, err := loadConfig(testProcess([]string{
		"SLIVINGDOC_BUCKET=b", "SLIVINGDOC_WRITABLE_PATHS=notes",
	}, "--writable-paths="))
	if err != nil {
		t.Fatalf("loadConfig() = %v", err)
	}
	if got := cfg.writablePaths; len(got) != 0 {
		t.Fatalf("writablePaths = %v, want the empty set, not the inherited environment value", got)
	}
}

// TestFlagsWritablePathsSplitting checks the value splits on the comma
// exactly as the read-only value does: surrounding white space trimmed,
// empty pieces dropped, a trailing slash trimmed, covered entries
// collapsed, and a value that is entirely separators resolving to the empty
// set rather than to an invalid entry.
func TestFlagsWritablePathsSplitting(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value string
		want  []string
	}{
		{
			name:  "trimmed, non-empty entries; nested entries collapse",
			value: "notes, docs/ ,docs/faq.md, ,",
			want:  []string{"docs", "notes"},
		},
		{name: "entirely separators", value: ",,,", want: []string{}},
		{name: "entirely white space", value: "  ,  ", want: []string{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadConfig(testProcess([]string{"SLIVINGDOC_BUCKET=b"}, "--writable-paths="+tt.value))
			if err != nil {
				t.Fatalf("loadConfig(--writable-paths=%q) = %v", tt.value, err)
			}
			if !reflect.DeepEqual(cfg.writablePaths, tt.want) {
				t.Fatalf("writablePaths = %v, want %v", cfg.writablePaths, tt.want)
			}
		})
	}
}

// TestSetupRejectsInvalidWritableEntry checks an entry that fails path
// validation refuses startup, naming the entry and the setting it came
// from, before the engine opens and before the store is built.
func TestSetupRejectsInvalidWritableEntry(t *testing.T) {
	for _, tt := range []struct{ name, value string }{
		{name: "dot-dot segment", value: ".."},
		{name: "absolute path", value: "/abs"},
		{name: "git segment", value: "docs/.git"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, engine, built := refusingProcess([]string{"SLIVINGDOC_BUCKET=b"}, "--writable-paths="+tt.value)
			rt, err := setup(p)
			if err == nil {
				_ = rt.Close()
				t.Fatalf("setup(--writable-paths=%s) = nil, want a refusal", tt.value)
			}
			for _, want := range []string{"writable paths:", "invalid writable path", tt.value} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("setup() error = %v, want it to name %q", err, want)
				}
			}
			if strings.Contains(err.Error(), "git:") {
				t.Fatalf("setup() error = %v, want no `git:` package prefix", err)
			}
			assertUntouched(t, engine, built)
		})
	}
}

// TestSetupRejectsInvalidEntryFromEnvironment checks a value inherited from
// the environment refuses startup with the same message as the flag path:
// the entry text identifies the source, and the setting is named either way.
func TestSetupRejectsInvalidEntryFromEnvironment(t *testing.T) {
	fromFlag, _, _ := refusingProcess([]string{"SLIVINGDOC_BUCKET=b"}, "--writable-paths=..")
	flagErr := setupRefusal(t, fromFlag)

	p, engine, built := refusingProcess([]string{"SLIVINGDOC_BUCKET=b", "SLIVINGDOC_WRITABLE_PATHS=.."})
	envErr := setupRefusal(t, p)
	if envErr != flagErr {
		t.Fatalf("environment refusal = %q, want the flag refusal %q", envErr, flagErr)
	}
	assertUntouched(t, engine, built)
}

// TestSetupRejectsOverlapNamingBothSettings checks a path named by both
// sets refuses startup before the engine and the store, with a message
// naming the path and both settings so the operator knows which to change.
func TestSetupRejectsOverlapNamingBothSettings(t *testing.T) {
	p, engine, built := refusingProcess([]string{"SLIVINGDOC_BUCKET=b"},
		"--read-only-paths=docs,notes", "--writable-paths=notes")
	err := setupRefusal(t, p)
	for _, want := range []string{"notes", "--read-only-paths", "--writable-paths", "name the same path"} {
		if !strings.Contains(err, want) {
			t.Fatalf("setup() error = %q, want it to name %q", err, want)
		}
	}
	assertUntouched(t, engine, built)
}

// TestSetupRejectsCaseFoldedOverlap checks entries differing only in letter
// case are the same overlap, since the policy matches under case folding,
// and that both written forms reach the operator.
func TestSetupRejectsCaseFoldedOverlap(t *testing.T) {
	p, engine, built := refusingProcess([]string{"SLIVINGDOC_BUCKET=b"},
		"--read-only-paths=Notes", "--writable-paths=notes")
	err := setupRefusal(t, p)
	for _, want := range []string{"Notes", "notes", "--read-only-paths", "--writable-paths"} {
		if !strings.Contains(err, want) {
			t.Fatalf("setup() error = %q, want it to name %q", err, want)
		}
	}
	assertUntouched(t, engine, built)
}

// TestSetupRefusesBeforeEngineAndProbe checks the refusal ordering directly:
// every configuration failure of the two sets happens before the native
// engine opens and before the object store is built and probed. The last
// three rows are the overlap an operator also covered by an ancestor of its
// own setting: the entries compared are the ones written, so the refusal
// does not depend on which unrelated ancestors sit beside them, and with
// several overlaps the pair named is the first written read-only entry
// (architecture section 2, Writable paths).
func TestSetupRefusesBeforeEngineAndProbe(t *testing.T) {
	for _, tt := range []struct {
		name        string
		args        []string
		wantMessage string
	}{
		{name: "invalid writable entry", args: []string{"--writable-paths=.."}},
		{name: "invalid read-only entry", args: []string{"--read-only-paths=.."}},
		{name: "exact overlap", args: []string{"--read-only-paths=docs", "--writable-paths=docs"}},
		{name: "case-folded overlap", args: []string{"--read-only-paths=Docs", "--writable-paths=docs"}},
		{
			name:        "overlap covered by an ancestor of the read-only set",
			args:        []string{"--read-only-paths=docs,docs/open", "--writable-paths=docs/open"},
			wantMessage: `--read-only-paths "docs/open" and --writable-paths "docs/open" name the same path`,
		},
		{
			name:        "overlap covered by an ancestor of the writable set",
			args:        []string{"--read-only-paths=docs/open", "--writable-paths=docs,docs/open"},
			wantMessage: `--read-only-paths "docs/open" and --writable-paths "docs/open" name the same path`,
		},
		{
			name:        "several overlaps name the first written pair",
			args:        []string{"--read-only-paths=notes,docs", "--writable-paths=docs,notes"},
			wantMessage: `--read-only-paths "notes" and --writable-paths "notes" name the same path`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, engine, built := refusingProcess([]string{"SLIVINGDOC_BUCKET=b"}, tt.args...)
			refusal := setupRefusal(t, p)
			if tt.wantMessage != "" && !strings.Contains(refusal, tt.wantMessage) {
				t.Fatalf("setup() error = %q, want it to carry %q", refusal, tt.wantMessage)
			}
			assertUntouched(t, engine, built)
		})
	}
}

// setupRefusal runs setup, requires a refusal, and returns its text.
func setupRefusal(t *testing.T, p process) string {
	t.Helper()
	rt, err := setup(p)
	if err == nil {
		_ = rt.Close()
		t.Fatal("setup() = nil, want a startup refusal")
	}
	return err.Error()
}

// assertUntouched proves the refusal preceded both startup dependencies.
func assertUntouched(t *testing.T, engine *refusingEngine, built *bool) {
	t.Helper()
	if engine.touched {
		t.Fatal("the refusal happened after the native engine was touched")
	}
	if *built {
		t.Fatal("the refusal happened after the object store was constructed")
	}
}

// TestSetupValidPolicyProceeds checks that a configuration the two sets can
// express is no refusal at all: startup continues into the engine and the
// store exactly as it does with neither set configured.
func TestSetupValidPolicyProceeds(t *testing.T) {
	p := testProcess([]string{"SLIVINGDOC_BUCKET=b"}, "--read-only-paths=docs", "--writable-paths=docs/open,notes")
	rt, err := setup(p)
	if err != nil {
		t.Fatalf("setup() = %v", err)
	}
	defer rt.Close()
	if engine, ok := p.engine.(*fakeEngine); !ok || !engine.opened {
		t.Fatal("setup() returned without opening the native engine")
	}
}

// TestSetupPassesBothSetsToNotebook checks the resolved entries reach the
// service configuration the notebook is built from, normalized and with the
// composition kept: a writable entry below a read-only entry is not
// collapsed away.
func TestSetupPassesBothSetsToNotebook(t *testing.T) {
	p := testProcess([]string{"SLIVINGDOC_BUCKET=b"},
		"--read-only-paths=docs/,team", "--writable-paths=docs/open,notes/")
	rt, err := setup(p)
	if err != nil {
		t.Fatalf("setup() = %v", err)
	}
	defer rt.Close()
	wantReadOnly, wantWritable := []string{"docs", "team"}, []string{"docs/open", "notes"}
	if got := rt.svc.cfg.ReadOnlyPaths; !reflect.DeepEqual(got, wantReadOnly) {
		t.Fatalf("service config ReadOnlyPaths = %v, want %v", got, wantReadOnly)
	}
	if got := rt.svc.cfg.WritablePaths; !reflect.DeepEqual(got, wantWritable) {
		t.Fatalf("service config WritablePaths = %v, want %v", got, wantWritable)
	}
	if got := rt.ReadOnlyPaths(); !reflect.DeepEqual(got, wantReadOnly) {
		t.Fatalf("ReadOnlyPaths() = %v, want %v", got, wantReadOnly)
	}
	if got := rt.WritablePaths(); !reflect.DeepEqual(got, wantWritable) {
		t.Fatalf("WritablePaths() = %v, want %v", got, wantWritable)
	}
}

// TestHelpTextWritablePathsLine checks the help text — the authoritative
// copy of the flag table — carries the flag in the existing column layout:
// the description and the environment variable start in the same columns as
// the read-only line above it.
func TestHelpTextWritablePathsLine(t *testing.T) {
	const want = `  --writable-paths string       comma-separated notebook paths agents may    SLIVINGDOC_WRITABLE_PATHS
                                change; every other path is then read-only
                                (default: none, every path is writable)`
	if !strings.Contains(FlagReference, want) {
		t.Fatalf("FlagReference = %q, want it to carry\n%s", FlagReference, want)
	}
	if !strings.Contains(HelpText, want) {
		t.Fatal("HelpText does not embed the flag reference line")
	}
	readOnly := flagReferenceLine(t, "--read-only-paths")
	writable := flagReferenceLine(t, "--writable-paths")
	if got, wantCol := strings.Index(writable, "comma-separated"), strings.Index(readOnly, "comma-separated"); got != wantCol {
		t.Fatalf("description column = %d, want the existing layout column %d", got, wantCol)
	}
	if got, wantCol := strings.Index(writable, "SLIVINGDOC_"), strings.Index(readOnly, "SLIVINGDOC_"); got != wantCol {
		t.Fatalf("environment column = %d, want the existing layout column %d", got, wantCol)
	}
}

// flagReferenceLine returns the first help line declaring flag.
func flagReferenceLine(t *testing.T, flag string) string {
	t.Helper()
	for line := range strings.SplitSeq(FlagReference, "\n") {
		if strings.HasPrefix(line, "  "+flag+" ") {
			return line
		}
	}
	t.Fatalf("FlagReference declares no %s line", flag)
	return ""
}
