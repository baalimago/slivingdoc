package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/baalimago/slivingdoc/internal/crypt"
	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// ServiceConfig is the validated service configuration the app resolves
// from flags and the environment (architecture/config.md). Process
// scenarios and the integration harness build it directly; production
// derives it from the resolved config through serviceConfig.
type ServiceConfig struct {
	Bucket string
	// SpaceID is the hosted server's id of Bucket's space, part of the
	// storage identity; empty for S3 and for a server that names none.
	SpaceID             string
	Prefix              string
	Region              string
	Endpoint            string
	PathStyle           bool
	WorkspaceRoot       string
	PrivateRoot         string
	PackCacheRoot       string
	CommitRetries       int
	CheckpointPacks     int
	RetainedCheckpoints int
	// ReadOnlyPaths are the read-only entries
	// (architecture/product-contract.md, Read-only and writable paths).
	ReadOnlyPaths []string
	// WritablePaths are the writable entries; a non-empty set protects
	// every unmatched path (architecture/product-contract.md, Read-only and
	// writable paths).
	WritablePaths []string
	// Ignore are the operator's ignore patterns, added to
	// workspace.DefaultIgnore.
	Ignore []string
}

// probeProofs locates the store compatibility proof of this configuration:
// below the identity's shared pack-cache directory, or disabled when the
// process has no shared cache (architecture/storage.md).
func (cfg config) probeProofs(now func() time.Time) probeProofStore {
	if cfg.packCacheRoot == "" {
		return probeProofStore{}
	}
	return probeProofStore{
		dir:     filepath.Join(cfg.packCacheRoot, workspace.SharedCacheDirName(cfg.serviceConfig().identity())),
		version: Version,
		store:   probedStore{cfg.endpoint, cfg.effectiveRegion(), cfg.bucket, cfg.prefix, cfg.pathStyle},
		now:     now,
	}
}

// serviceConfig converts the resolved process configuration into the
// exported service configuration.
func (cfg config) serviceConfig() ServiceConfig {
	return ServiceConfig{
		Bucket:              cfg.bucket,
		SpaceID:             string(cfg.spaceID),
		Prefix:              cfg.prefix,
		Region:              cfg.effectiveRegion(),
		Endpoint:            cfg.endpoint,
		PathStyle:           cfg.pathStyle,
		WorkspaceRoot:       cfg.workspaceRoot,
		PrivateRoot:         cfg.privateRoot,
		PackCacheRoot:       cfg.packCacheRoot,
		CommitRetries:       cfg.commitRetries,
		CheckpointPacks:     cfg.checkpointPacks,
		RetainedCheckpoints: cfg.retainedCheckpoints,
		ReadOnlyPaths:       cfg.readOnlyPaths,
		WritablePaths:       cfg.writablePaths,
		Ignore:              cfg.ignore,
	}
}

// ServiceHooks are the injectable failpoints of one service. The
// integration harness installs and removes hooks per scenario; production
// leaves them nil. The workspace hooks mutate in place, so the harness can
// change them between calls of one service.
type ServiceHooks struct {
	// Workspace is the workspace mutation failpoints.
	Workspace *workspace.Failpoints
	// Notebook is the notebook orchestration failpoints.
	Notebook *notebook.Failpoints
}

// Service is the notebook view of the MCP server and the one-shot commands:
// one notebook path resolves to one workspace and notebook, opened lazily on
// first use and kept open until Close. The MCP server always passes Root;
// the CLI passes its positional path. Calls for one path serialize on that
// workspace's operation lock; distinct paths operate independently
// (architecture/workspace.md).
//
// The service holds the raw store it was given and, when it was given a
// keyring, serves every notebook operation through the encrypted decorator
// (architecture/encryption.md). The encrypt commands read and rewrite the
// stored wrapper itself, so they ask for the raw view.
type Service struct {
	engine git.Engine
	store  storage.ObjectStore
	ring   *crypt.Keyring
	cfg    ServiceConfig
	hooks  *ServiceHooks
	policy git.PathPolicy
	ignore workspace.Ignore

	mu     sync.Mutex // guards opened and closed
	opened map[string]*openedNotebook
	closed bool
}

// openedNotebook is one opened path: the workspace (the resource owner:
// repository, operation lock, workspace root) and its two notebook views,
// the decorated one every operation uses and the raw one the encrypt
// commands use.
type openedNotebook struct {
	workspace *workspace.Workspace
	notebook  *notebook.Notebook
	raw       *notebook.Notebook
}

// storedCurrentState classifies the stored current without opening it, which
// is the question the secret prompt of pull and commit asks first.
type storedCurrentState uint8

const (
	// storedCurrentUnknown means the store gave no answer here: the
	// operation that follows reports its own failure.
	storedCurrentUnknown storedCurrentState = iota
	// storedCurrentAbsent is a store that holds no current yet.
	storedCurrentAbsent
	// storedCurrentPlaintext is a notebook with no envelope.
	storedCurrentPlaintext
	// storedCurrentEncrypted is an envelope this process holds no secret for.
	storedCurrentEncrypted
)

// NewService returns a service bound to the store and the validated
// configuration. The engine stays owned by the caller; the service closes
// the workspaces it opens. hooks can be nil.
func NewService(engine git.Engine, store storage.ObjectStore, cfg ServiceConfig, hooks *ServiceHooks, ring *crypt.Keyring) (*Service, error) {
	if engine == nil {
		return nil, errors.New("app: engine is required")
	}
	if store == nil {
		return nil, errors.New("app: store is required")
	}
	policy, err := git.NewPolicy(cfg.ReadOnlyPaths, cfg.WritablePaths)
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	ignore, err := workspace.NewIgnore(append(slices.Clone(workspace.DefaultIgnore), cfg.Ignore...))
	if err != nil {
		return nil, fmt.Errorf("app: ignore: %w", err)
	}
	return &Service{
		engine: engine,
		store:  store,
		ring:   ring,
		cfg:    cfg,
		hooks:  hooks,
		policy: policy,
		ignore: ignore,
		opened: map[string]*openedNotebook{},
	}, nil
}

// Root is the one notebook directory the MCP server serves: the configured
// workspace root, or the process-owned temporary notebook directory when no
// root was configured (architecture/config.md).
func (s *Service) Root() string { return s.cfg.WorkspaceRoot }

// ReadOnlyPaths returns the normalized, sorted read-only entries; never nil.
func (s *Service) ReadOnlyPaths() []string { return s.policy.ReadOnly() }

// WritablePaths returns the normalized, sorted writable entries; never nil.
func (s *Service) WritablePaths() []string { return s.policy.Writable() }

// Pull pulls the notebook at the service's own directory and returns the
// operation result: the accepted generation and the pull delta diffstat.
// The MCP server serves exactly that one directory.
func (s *Service) Pull(ctx context.Context) (notebook.Result, error) {
	return s.PullAt(ctx, s.Root())
}

// PullAt resolves path to its notebook, pulls it, and returns the
// operation result. The CLI passes the directory it was given.
func (s *Service) PullAt(ctx context.Context, path string) (notebook.Result, error) {
	nb, err := s.notebookFor(ctx, path)
	if err != nil {
		return notebook.Result{}, err
	}
	return nb.Pull(ctx)
}

// Commit publishes the caller's changes at the service's own directory
// with message and returns the operation result.
func (s *Service) Commit(ctx context.Context, message string) (notebook.Result, error) {
	return s.CommitAt(ctx, s.Root(), message)
}

// CommitAt resolves path to its notebook, commits message, and returns the
// operation result: the accepted generation and the published-increment
// diffstat.
func (s *Service) CommitAt(ctx context.Context, path, message string) (notebook.Result, error) {
	nb, err := s.notebookFor(ctx, path)
	if err != nil {
		return notebook.Result{}, err
	}
	return nb.Commit(ctx, message)
}

// Status resolves path to its notebook and reports its local state.
func (s *Service) Status(ctx context.Context, path string) (notebook.Status, error) {
	nb, err := s.notebookFor(ctx, path)
	if err != nil {
		return notebook.Status{}, err
	}
	return nb.Status(ctx)
}

// Log resolves path to its notebook and returns up to limit recent
// publications.
func (s *Service) Log(ctx context.Context, path string, limit int) (notebook.History, error) {
	nb, err := s.notebookFor(ctx, path)
	if err != nil {
		return notebook.History{}, err
	}
	return nb.Log(ctx, limit)
}

// notebookFor returns the notebook for the notebook path, opening its
// workspace and notebook on first use. The open runs under the map lock so
// concurrent first use of the same path cannot open two workspaces. The
// map key is the cleaned path, so spellings of one directory such as
// /ws/a and /ws/a/ share one workspace and its operation lock.
func (s *Service) notebookFor(ctx context.Context, path string) (*notebook.Notebook, error) {
	opened, err := s.open(ctx, path)
	if err != nil {
		return nil, err
	}
	return opened.notebook, nil
}

// rawNotebookFor returns the notebook view over the undecorated store, which
// the encrypt commands use: they classify, open and rewrite the stored
// current wrapper themselves, so the decorator must not decrypt it under
// them.
func (s *Service) rawNotebookFor(ctx context.Context, path string) (*notebook.Notebook, error) {
	opened, err := s.open(ctx, path)
	if err != nil {
		return nil, err
	}
	return opened.raw, nil
}

// notebookStore is the store every operation uses: the decorator when the
// service was given a keyring, the raw store otherwise.
func (s *Service) notebookStore() storage.ObjectStore {
	if s.ring == nil {
		return s.store
	}
	return storage.Encrypted(s.store, s.ring)
}

// AskForSecret calls ask when the stored notebook is encrypted and this
// process holds no key material, then adopts the answer as the prompted
// candidate of the process keyring. Nothing is asked when a configured
// candidate exists: a configured secret that opens no door stays
// KEY_REJECTED rather than becoming a second question named the same as the
// first. The answer stays in memory and is never written to the key record
// (architecture/encryption.md, The process keyring).
func (s *Service) AskForSecret(ctx context.Context, ask func() ([]byte, error)) error {
	if s.ring == nil || s.ring.HasCandidates() || s.storedCurrent(ctx) != storedCurrentEncrypted {
		return nil
	}
	secret, err := ask()
	if err != nil {
		return promptRefusal(err)
	}
	if err := s.ring.AddCandidate(crypt.Candidate{Source: crypt.SourcePrompt, Secret: secret}); err != nil {
		return keyMaterialInvalid(err)
	}
	return nil
}

// storedCurrent classifies the stored current of this service's notebook
// without a key. Every failure is storedCurrentUnknown, because this read only
// decides whether to ask for a secret: the operation that follows reports a
// store failure in its own taxonomy, with no raw cause on the caller's stream.
func (s *Service) storedCurrent(ctx context.Context) storedCurrentState {
	rc, _, err := s.store.ReadObject(ctx, storage.CurrentKey)
	if errors.Is(err, storage.ErrNotFound) {
		return storedCurrentAbsent
	}
	if err != nil {
		return storedCurrentUnknown
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return storedCurrentUnknown
	}
	encoding, _, err := storage.ClassifyCurrent(data)
	if err != nil {
		return storedCurrentUnknown
	}
	if encoding == storage.CurrentEncrypted {
		return storedCurrentEncrypted
	}
	return storedCurrentPlaintext
}

// open returns the opened path's workspace and its two notebook views,
// opening them on first use.
func (s *Service) open(ctx context.Context, path string) (*openedNotebook, error) {
	path = filepath.Clean(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("app: service is closed")
	}
	if opened, ok := s.opened[path]; ok {
		return opened, nil
	}
	var wsFailpoints *workspace.Failpoints
	var nbFailpoints *notebook.Failpoints
	if s.hooks != nil {
		wsFailpoints = s.hooks.Workspace
		nbFailpoints = s.hooks.Notebook
	}
	ws, err := workspace.Open(ctx, workspace.Config{
		WorkspaceRoot: s.cfg.WorkspaceRoot,
		Path:          path,
		PrivateRoot:   s.cfg.PrivateRoot,
		PackCacheRoot: s.cfg.PackCacheRoot,
		Identity:      s.identity(),
		Engine:        s.engine,
		Failpoints:    wsFailpoints,
		Ignore:        s.ignore,
	})
	if err != nil {
		return nil, err
	}
	// The notebook builds its own policy from the entry sets: each layer
	// validates what it is configured with, and the normalized entries
	// resolve exactly as the written ones do
	// (architecture/product-contract.md, Read-only and writable paths).
	nbConfig := notebook.Config{
		Workspace:           ws,
		Store:               s.notebookStore(),
		RetryLimit:          s.cfg.CommitRetries,
		CheckpointPacks:     s.cfg.CheckpointPacks,
		RetainedCheckpoints: s.cfg.RetainedCheckpoints,
		ReadOnlyPaths:       s.policy.ReadOnly(),
		WritablePaths:       s.policy.Writable(),
		Failpoints:          nbFailpoints,
	}
	nb, err := notebook.New(nbConfig)
	if err != nil {
		ws.Close()
		return nil, fmt.Errorf("app: open notebook: %w", err)
	}
	nbConfig.Store = s.store
	raw, err := notebook.New(nbConfig)
	if err != nil {
		ws.Close()
		return nil, fmt.Errorf("app: open raw notebook: %w", err)
	}
	opened := &openedNotebook{workspace: ws, notebook: nb, raw: raw}
	s.opened[path] = opened
	return opened, nil
}

// identity is the storage identity derived from the normalized
// configuration (architecture/workspace.md and config.md): the endpoint,
// region, bucket, prefix, the manifest protocol version, and a hosted
// space's server id.
func (s *Service) identity() workspace.Identity { return s.cfg.identity() }

func (c ServiceConfig) identity() workspace.Identity {
	return workspace.Identity{
		Endpoint:        c.Endpoint,
		Region:          c.Region,
		Bucket:          c.Bucket,
		Prefix:          c.Prefix,
		ManifestVersion: workspace.ManifestVersion,
		SpaceID:         c.SpaceID,
	}
}

// Close closes every opened workspace. Close is idempotent and safe for
// concurrent use; a closed service refuses new pull and commit calls.
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var firstErr error
	for path, opened := range s.opened {
		if err := opened.workspace.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("app: close workspace for %s: %w", path, err)
		}
	}
	s.opened = nil
	return firstErr
}
