package app

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// ServiceConfig is the validated service configuration the app resolves
// from flags and the environment (architecture section 17). Process
// scenarios and the integration harness build it directly; production
// derives it from the resolved config through serviceConfig.
type ServiceConfig struct {
	Bucket              string
	Prefix              string
	Region              string
	Endpoint            string
	PathStyle           bool
	WorkspaceRoot       string
	PrivateRoot         string
	CommitRetries       int
	CheckpointPacks     int
	RetainedCheckpoints int
}

// serviceConfig converts the resolved process configuration into the
// exported service configuration.
func (cfg config) serviceConfig() ServiceConfig {
	return ServiceConfig{
		Bucket:              cfg.bucket,
		Prefix:              cfg.prefix,
		Region:              cfg.region,
		Endpoint:            cfg.endpoint,
		PathStyle:           cfg.pathStyle,
		WorkspaceRoot:       cfg.workspaceRoot,
		PrivateRoot:         cfg.privateRoot,
		CommitRetries:       cfg.commitRetries,
		CheckpointPacks:     cfg.checkpointPacks,
		RetainedCheckpoints: cfg.retainedCheckpoints,
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

// Service is the MCP service view: one requested visible path resolves to
// one workspace and notebook, opened lazily on first use and kept open
// until Close. Calls for one path serialize on that workspace's operation
// lock; distinct paths operate independently (architecture section 7.2).
type Service struct {
	engine git.Engine
	store  storage.ObjectStore
	cfg    ServiceConfig
	hooks  *ServiceHooks

	mu     sync.Mutex // guards opened and closed
	opened map[string]*openedNotebook
	closed bool
}

// openedNotebook is one opened path: the workspace (the resource owner:
// repository, operation lock, workspace root) and the notebook bound to
// it.
type openedNotebook struct {
	workspace *workspace.Workspace
	notebook  *notebook.Notebook
}

// NewService returns a service bound to the store and the validated
// configuration. The engine stays owned by the caller; the service closes
// the workspaces it opens. hooks may be nil.
func NewService(engine git.Engine, store storage.ObjectStore, cfg ServiceConfig, hooks *ServiceHooks) (*Service, error) {
	if engine == nil {
		return nil, errors.New("app: engine is required")
	}
	if store == nil {
		return nil, errors.New("app: store is required")
	}
	return &Service{
		engine: engine,
		store:  store,
		cfg:    cfg,
		hooks:  hooks,
		opened: map[string]*openedNotebook{},
	}, nil
}

// Pull resolves path to its notebook and pulls it.
func (s *Service) Pull(ctx context.Context, path string) error {
	nb, err := s.notebookFor(ctx, path)
	if err != nil {
		return err
	}
	return nb.Pull(ctx)
}

// Commit resolves path to its notebook and commits message.
func (s *Service) Commit(ctx context.Context, path, message string) error {
	nb, err := s.notebookFor(ctx, path)
	if err != nil {
		return err
	}
	return nb.Commit(ctx, message)
}

// notebookFor returns the notebook for the request path, opening its
// workspace and notebook on first use. The open runs under the map lock so
// concurrent first use of the same path cannot open two workspaces.
func (s *Service) notebookFor(ctx context.Context, path string) (*notebook.Notebook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("app: service is closed")
	}
	if opened, ok := s.opened[path]; ok {
		return opened.notebook, nil
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
		Identity:      s.identity(),
		Engine:        s.engine,
		Failpoints:    wsFailpoints,
	})
	if err != nil {
		return nil, err
	}
	nb, err := notebook.New(notebook.Config{
		Workspace:           ws,
		Store:               s.store,
		RetryLimit:          s.cfg.CommitRetries,
		CheckpointPacks:     s.cfg.CheckpointPacks,
		RetainedCheckpoints: s.cfg.RetainedCheckpoints,
		Failpoints:          nbFailpoints,
	})
	if err != nil {
		ws.Close()
		return nil, fmt.Errorf("app: open notebook: %w", err)
	}
	s.opened[path] = &openedNotebook{workspace: ws, notebook: nb}
	return nb, nil
}

// identity is the storage identity derived from the normalized
// configuration (architecture sections 7.2 and 17): the endpoint, region,
// bucket, prefix, and the manifest protocol version.
func (s *Service) identity() workspace.Identity {
	return workspace.Identity{
		Endpoint:        s.cfg.Endpoint,
		Region:          s.cfg.Region,
		Bucket:          s.cfg.Bucket,
		Prefix:          s.cfg.Prefix,
		ManifestVersion: workspace.ManifestVersion,
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
