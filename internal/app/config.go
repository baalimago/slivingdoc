package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/pathutil"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// config is the fully resolved process configuration (architecture section
// 17). Flags override environment variables, which override defaults; the
// endpoint is normalized and both roots are absolute and disjoint before
// any engine or S3 work.
type config struct {
	bucket              string
	prefix              string
	region              string
	endpoint            string
	pathStyle           bool
	workspaceRoot       string
	privateRoot         string
	commitRetries       int
	checkpointPacks     int
	retainedCheckpoints int

	// sessionDir is the process-owned parent of both roots when neither is
	// configured (architecture section 17). It is removed at shutdown; the
	// notebook itself lives in S3.
	sessionDir string
}

// Flags are the serve-command flags (architecture section 17). Binding and
// resolution are separate so the command router can parse the flag set
// before the process body resolves it against the environment.
type Flags struct {
	bucket              stringFlag
	prefix              stringFlag
	region              stringFlag
	endpoint            stringFlag
	workspaceRoot       stringFlag
	privateRoot         stringFlag
	pathStyle           boolFlag
	commitRetries       intFlag
	checkpointPacks     intFlag
	retainedCheckpoints intFlag
}

// NewFlags returns an unbound flag holder for the serve command.
func NewFlags() *Flags { return &Flags{} }

// Bind registers every serve flag on fs. The flag set is the single
// definition of the command line; loadConfig and the serve command both
// resolve the same holder.
func (f *Flags) Bind(fs *flag.FlagSet) {
	fs.Var(&f.bucket, "bucket", "S3 bucket (required)")
	fs.Var(&f.prefix, "prefix", "S3 object prefix")
	fs.Var(&f.region, "region", "S3 region")
	fs.Var(&f.endpoint, "endpoint", "S3-compatible endpoint URL")
	fs.Var(&f.pathStyle, "path-style", "force S3 path-style addressing")
	fs.Var(&f.workspaceRoot, "workspace-root", "visible workspace root")
	fs.Var(&f.privateRoot, "private-root", "private state root")
	fs.Var(&f.commitRetries, "commit-retries", "CAS retries after the first attempt")
	fs.Var(&f.checkpointPacks, "checkpoint-packs", "active tail length that schedules a checkpoint")
	fs.Var(&f.retainedCheckpoints, "retained-checkpoints", "retained previous checkpoint generations")
}

// The documented numeric bounds and defaults (architecture section 17).
// The ranges come from the notebook package, which owns them, so the flag
// validation and the notebook validation cannot drift.
const (
	defaultCommitRetries       = notebook.DefaultRetryLimit
	maxCommitRetries           = notebook.MaxRetryLimit
	defaultCheckpointPacks     = notebook.DefaultCheckpointPacks
	defaultRetainedCheckpoints = notebook.DefaultRetainedCheckpoints
	maxRetainedCheckpoints     = notebook.MaxRetainedCheckpoints
)

// loadConfig resolves one validated configuration for the process
// (architecture section 17). The flags are already parsed when the command
// router owns the command line; otherwise p.args is parsed here. Any parse
// or validation failure returns a diagnostic that never echoes credentials
// or private values.
func loadConfig(p process) (config, error) {
	f := p.flags
	if f == nil {
		f = NewFlags()
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		fs.SetOutput(io.Discard) // diagnostics are formatted by this package
		f.Bind(fs)
		if err := fs.Parse(p.args); err != nil {
			return config{}, err
		}
	}
	return f.resolve(p.env, p.cwd, p.cacheDir, p.ephemeral, p.newSessionDir)
}

// resolve applies the documented precedence — an explicitly set flag over
// the environment over the default — and validates the result. When
// ephemeral is set and neither root is configured, the process owns a
// temporary session directory holding both roots as siblings. A refusal
// after that directory exists removes it again: a startup refusal must
// leave nothing behind.
func (f *Flags) resolve(environment []string, cwd, cacheDir string, ephemeral bool, newSessionDir func() (string, error)) (cfgOut config, errOut error) {
	var session string
	defer func() {
		if errOut != nil {
			removeSessionDir(session)
		}
	}()
	env := environ(environment)
	cfg := config{
		bucket:   resolveString(&f.bucket, env["SLIVINGDOC_BUCKET"], ""),
		prefix:   resolveString(&f.prefix, env["SLIVINGDOC_PREFIX"], "slivingdoc"),
		region:   resolveString(&f.region, env["AWS_REGION"], "us-east-1"),
		endpoint: resolveString(&f.endpoint, env["AWS_ENDPOINT_URL_S3"], ""),
	}
	wsRoot, wsSet := resolveRoot(&f.workspaceRoot, env["SLIVINGDOC_WORKSPACE_ROOT"])
	privRoot, privSet := resolveRoot(&f.privateRoot, env["SLIVINGDOC_PRIVATE_ROOT"])
	switch {
	case ephemeral && !wsSet && !privSet:
		if newSessionDir == nil {
			newSessionDir = defaultSessionDir
		}
		created, err := newSessionDir()
		if err != nil {
			return config{}, fmt.Errorf("create session directory: %w", err)
		}
		session = created
		cfg.sessionDir = session
		cfg.workspaceRoot = filepath.Join(session, sessionNotebookDir)
		cfg.privateRoot = filepath.Join(session, sessionPrivateDir)
	default:
		cfg.workspaceRoot = cwd
		if wsSet {
			cfg.workspaceRoot = wsRoot
		}
		cfg.privateRoot = filepath.Join(cacheDir, "slivingdoc")
		if privSet {
			cfg.privateRoot = privRoot
		}
	}
	var err error
	if cfg.pathStyle, err = resolveBool(&f.pathStyle, env["SLIVINGDOC_PATH_STYLE"]); err != nil {
		return config{}, err
	}
	if cfg.commitRetries, err = resolveInt(&f.commitRetries, env["SLIVINGDOC_COMMIT_RETRIES"], defaultCommitRetries); err != nil {
		return config{}, err
	}
	if cfg.checkpointPacks, err = resolveInt(&f.checkpointPacks, env["SLIVINGDOC_CHECKPOINT_PACKS"], defaultCheckpointPacks); err != nil {
		return config{}, err
	}
	if cfg.retainedCheckpoints, err = resolveInt(&f.retainedCheckpoints, env["SLIVINGDOC_RETAINED_CHECKPOINTS"], defaultRetainedCheckpoints); err != nil {
		return config{}, err
	}
	return cfg.finish(cwd)
}

// finish validates the resolved configuration: required bucket, valid
// prefix, normalized endpoint, absolute and disjoint roots, and the
// numeric bounds. The endpoint and roots normalize before any engine or S3
// work; diagnostics never echo credentials or private values.
func (cfg config) finish(cwd string) (config, error) {
	if cfg.bucket == "" {
		return config{}, errors.New("bucket is required")
	}
	if err := storage.ValidatePrefix(cfg.prefix); err != nil {
		return config{}, err
	}
	if cfg.region == "" {
		return config{}, errors.New("region is required")
	}
	endpoint, err := normalizeEndpoint(cfg.endpoint)
	if err != nil {
		return config{}, err
	}
	cfg.endpoint = endpoint

	if cfg.workspaceRoot, err = absolute(cwd, cfg.workspaceRoot); err != nil {
		return config{}, fmt.Errorf("workspace root: %w", err)
	}
	if cfg.privateRoot, err = absolute(cwd, cfg.privateRoot); err != nil {
		return config{}, fmt.Errorf("private root: %w", err)
	}
	if workspace.RootsOverlap(cfg.privateRoot, cfg.workspaceRoot) {
		return config{}, errors.New("private root must not be at or below the workspace root")
	}

	if cfg.commitRetries > maxCommitRetries {
		return config{}, fmt.Errorf("commit retries %d is outside 0..%d", cfg.commitRetries, maxCommitRetries)
	}
	if cfg.checkpointPacks < notebook.MinCheckpointPacks {
		return config{}, fmt.Errorf("checkpoint packs threshold %d must be at least %d", cfg.checkpointPacks, notebook.MinCheckpointPacks)
	}
	if cfg.retainedCheckpoints > maxRetainedCheckpoints {
		return config{}, fmt.Errorf("retained checkpoints %d is outside 0..%d", cfg.retainedCheckpoints, maxRetainedCheckpoints)
	}
	return cfg, nil
}

// environ maps the process environment to a lookup table. The last value
// of a duplicated variable wins, matching the operating system.
func environ(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i > 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

// resolveString returns the effective string value: an explicitly set flag
// wins over the environment, which wins over the default. An explicitly
// empty flag does not fall back to the environment (architecture section
// 17); an empty environment value is treated as unset.
func resolveString(f *stringFlag, env, def string) string {
	if f.set {
		return f.value
	}
	if env != "" {
		return env
	}
	return def
}

// resolveRoot returns a configured root and whether it was configured at
// all. An explicitly empty flag counts as configured, so it still fails the
// empty-root check instead of silently taking a default.
func resolveRoot(f *stringFlag, env string) (string, bool) {
	if f.set {
		return f.value, true
	}
	if env != "" {
		return env, true
	}
	return "", false
}

// The ephemeral session layout. Both roots are siblings under one
// process-owned parent, so they can never overlap and no two processes can
// select the same private state.
const (
	sessionNotebookDir = "notebook"
	sessionPrivateDir  = "private"
	sessionDirPattern  = "slivingdoc-"
)

// defaultSessionDir creates the process-owned session directory in the
// operating-system temporary directory. The random component is what makes
// each server's notebook and private state unique.
func defaultSessionDir() (string, error) {
	return os.MkdirTemp("", sessionDirPattern)
}

// removeSessionDir removes an ephemeral session directory. An unconfigured
// root is process-owned scratch space: nothing outside this process can
// address it again, because the derived private key binds to the random
// path. The durable notebook is the remote.
func removeSessionDir(dir string) error {
	if dir == "" {
		return nil
	}
	return os.RemoveAll(dir)
}

// resolveBool returns the flag value when set, the parsed environment
// value when present and non-empty, else false.
func resolveBool(f *boolFlag, env string) (bool, error) {
	if f.set {
		return f.value, nil
	}
	if env != "" {
		v, err := strconv.ParseBool(env)
		if err != nil {
			return false, fmt.Errorf("invalid boolean value %q", env)
		}
		return v, nil
	}
	return false, nil
}

// resolveInt returns the flag value when set, the parsed unsigned
// environment value when present and non-empty, else the default.
func resolveInt(f *intFlag, env string, def int) (int, error) {
	if f.set {
		return f.value, nil
	}
	if env != "" {
		n, err := parseUnsigned(env)
		if err != nil {
			return 0, err
		}
		return n, nil
	}
	return def, nil
}

// absolute resolves a root to its clean absolute form: a leading home
// abbreviation expands first, an absolute value is cleaned, and a relative
// value is joined to the working directory. An empty root is invalid.
func absolute(cwd, root string) (string, error) {
	if root == "" {
		return "", errors.New("must not be empty")
	}
	var err error
	if root, err = pathutil.ExpandHome(root); err != nil {
		return "", err
	}
	if !filepath.IsAbs(root) {
		root = filepath.Join(cwd, root)
	}
	return filepath.Clean(root), nil
}

// normalizeEndpoint validates and normalizes a custom endpoint
// (architecture section 17): an absolute http or https URL without user
// information, query, or fragment. The scheme and host are lowercased, a
// trailing slash is removed, and a non-root path is preserved. The empty
// endpoint stays empty for normal AWS resolution.
func normalizeEndpoint(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("endpoint is not a valid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("endpoint must be an absolute http or https URL")
	}
	if u.Host == "" || u.User != nil {
		return "", errors.New("endpoint must be an absolute http or https URL without user information")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("endpoint must not contain a query or fragment")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawPath = ""
	return u.String(), nil
}

// stringFlag records whether the flag was explicitly set, so an explicitly
// empty value does not fall back to the environment (architecture section
// 17).
type stringFlag struct {
	value string
	set   bool
}

func (f *stringFlag) String() string { return f.value }
func (f *stringFlag) Set(s string) error {
	f.value = s
	f.set = true
	return nil
}

// boolFlag parses with strconv.ParseBool and records explicit setting.
type boolFlag struct {
	value bool
	set   bool
}

func (f *boolFlag) String() string { return strconv.FormatBool(f.value) }
func (f *boolFlag) Set(s string) error {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return fmt.Errorf("invalid boolean value %q", s)
	}
	f.value = v
	f.set = true
	return nil
}

// IsBoolFlag lets the flag package accept "--path-style" without a value.
func (f *boolFlag) IsBoolFlag() bool { return true }

// intFlag accepts unsigned decimal values only: integer configuration
// never carries a sign (architecture section 17).
type intFlag struct {
	value int
	set   bool
}

func (f *intFlag) String() string { return strconv.Itoa(f.value) }
func (f *intFlag) Set(s string) error {
	n, err := parseUnsigned(s)
	if err != nil {
		return err
	}
	f.value = n
	f.set = true
	return nil
}

// parseUnsigned parses an unsigned decimal integer and rejects any sign.
func parseUnsigned(s string) (int, error) {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, fmt.Errorf("invalid integer value %q", s)
		}
	}
	n, err := strconv.ParseUint(s, 10, 31)
	if err != nil {
		return 0, fmt.Errorf("invalid integer value %q", s)
	}
	return int(n), nil
}

// FlagReference documents every shared configuration flag, its environment
// variable, and its default (architecture section 17). serve, pull, and
// commit embed it in their help output.
const FlagReference = `  --bucket string               S3 bucket (required)                         SLIVINGDOC_BUCKET
  --prefix string               S3 object prefix (default "slivingdoc")      SLIVINGDOC_PREFIX
  --region string               S3 region (default "us-east-1")              AWS_REGION
  --endpoint string             S3-compatible endpoint URL (empty for AWS)   AWS_ENDPOINT_URL_S3
  --path-style                  force S3 path-style addressing               SLIVINGDOC_PATH_STYLE
  --workspace-root string       visible workspace root (serve default: a     SLIVINGDOC_WORKSPACE_ROOT
                                per-process temporary directory; pull and
                                commit default to the working directory)
  --private-root string         private state root (default: beside the      SLIVINGDOC_PRIVATE_ROOT
                                temporary workspace root, else
                                <user-cache-dir>/slivingdoc)
  --commit-retries int          CAS retries after the first attempt          SLIVINGDOC_COMMIT_RETRIES
                                (default 8, range 0..100)
  --checkpoint-packs int        active tail length that schedules one        SLIVINGDOC_CHECKPOINT_PACKS
                                checkpoint (default 1024, minimum 1)
  --retained-checkpoints int    retained previous checkpoint generations     SLIVINGDOC_RETAINED_CHECKPOINTS
                                (default 1, range 0..64)
`

// HelpText is the serve-command help (architecture section 17).
const HelpText = `slivingdoc serve - shared UTF-8 text notebook over MCP stdio

Usage:
  slivingdoc serve [flags]

With no configured workspace root, the server owns one temporary notebook
directory for its lifetime and removes it at shutdown; the notebook itself
is durable in the bucket. notes_pull and notes_commit then need no path,
and the resolved directory is reported in the server instructions and in
every tool result. Setting --workspace-root keeps the shared-directory
behaviour instead.

Flags take precedence over environment variables, which override defaults.
An explicitly empty flag value does not fall back to an environment value.

` + FlagReference
