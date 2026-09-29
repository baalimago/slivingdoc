package editorfixture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// Engine is what the tool needs of the native Git engine: repositories to
// create, open and close.
type Engine interface {
	workspace.Engine
}

// ExpectedFile is the name of the description written beside each scenario.
const ExpectedFile = "expected.json"

// Expected is what a reader must reconstruct from a scenario directory:
// the accepted generation and head, the files of the head, and the commits
// from the head down to the active checkpoint, newest first.
type Expected struct {
	Generation uint64            `json:"generation"`
	Head       string            `json:"head"`
	Files      map[string]string `json:"files"`
	History    []ExpectedCommit  `json:"history"`
}

// ExpectedCommit is one commit of the active chain.
type ExpectedCommit struct {
	ID      string `json:"id"`
	Message string `json:"message"`
	// Time is the commit time in seconds since the Unix epoch.
	Time int64 `json:"time"`
}

// ErrScenario reports a scenario that did not behave as declared, such as a
// step that changed nothing.
var ErrScenario = errors.New("editorfixture: scenario")

var fixtureEpoch = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

// Generate runs every scenario and writes its store under out/<name>/.
// work is scratch space for the workspaces; both directories must exist.
func Generate(ctx context.Context, eng Engine, work, out string, scenarios []Scenario) error {
	for _, sc := range scenarios {
		if err := generateOne(ctx, eng, work, out, sc); err != nil {
			return fmt.Errorf("editorfixture: generate %s: %w", sc.Name, err)
		}
	}
	return nil
}

func generateOne(ctx context.Context, eng Engine, work, out string, sc Scenario) error {
	root := filepath.Join(work, sc.Name, "workspace")
	private := filepath.Join(work, sc.Name, "private")
	for _, dir := range []string{root, private} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	visible := filepath.Join(root, "notes")
	ws, err := workspace.Open(ctx, workspace.Config{
		WorkspaceRoot: root,
		Path:          visible,
		PrivateRoot:   private,
		Identity: workspace.Identity{
			Endpoint:        "https://fixtures.invalid",
			Bucket:          sc.Name,
			ManifestVersion: workspace.ManifestVersion,
		},
		Engine: eng,
	})
	if err != nil {
		return fmt.Errorf("open workspace: %w", err)
	}
	defer ws.Close()

	var step atomic.Int64
	var nextID atomic.Uint64
	store := fake.New("")
	nb, err := notebook.New(notebook.Config{
		Workspace:           ws,
		Store:               store,
		RetryLimit:          notebook.DefaultRetryLimit,
		CheckpointPacks:     checkpointPacks(sc),
		RetainedCheckpoints: sc.RetainedCheckpoints,
		NewID:               func() (storage.UUID, error) { return sequentialUUID(nextID.Add(1)), nil },
		Now:                 func() time.Time { return commitTime(step.Load()) },
	})
	if err != nil {
		return fmt.Errorf("new notebook: %w", err)
	}
	if _, err := nb.Pull(ctx); err != nil {
		return fmt.Errorf("first pull: %w", err)
	}
	stepOf := make(map[string]int, len(sc.Steps))
	for i, s := range sc.Steps {
		step.Store(int64(i + 1))
		if err := applyStep(visible, s); err != nil {
			return fmt.Errorf("step %d: %w", i+1, err)
		}
		res, err := nb.Commit(ctx, s.Message)
		if err != nil {
			return fmt.Errorf("step %d: commit: %w", i+1, err)
		}
		head, err := currentHead(ctx, store)
		if err != nil {
			return fmt.Errorf("step %d: %w", i+1, err)
		}
		if _, dup := stepOf[head]; dup {
			return fmt.Errorf("%w: step %d published head %s again (generation %d)", ErrScenario, i+1, head, res.Generation)
		}
		stepOf[head] = i + 1
	}
	dest := filepath.Join(out, sc.Name)
	if err := dumpStore(ctx, store, dest); err != nil {
		return err
	}
	return writeExpected(dest, visible, sc, stepOf)
}

func currentHead(ctx context.Context, store storage.ObjectStore) (string, error) {
	rc, _, err := store.ReadObject(ctx, storage.CurrentKey)
	if err != nil {
		return "", fmt.Errorf("read current: %w", err)
	}
	data, err := io.ReadAll(rc)
	closeErr := rc.Close()
	if err != nil {
		return "", fmt.Errorf("read current body: %w", err)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close current: %w", closeErr)
	}
	m, err := storage.DecodeManifest(data)
	if err != nil {
		return "", fmt.Errorf("decode current: %w", err)
	}
	return m.Head.String(), nil
}

func checkpointPacks(sc Scenario) int {
	if sc.CheckpointPacks == 0 {
		return notebook.DefaultCheckpointPacks
	}
	return sc.CheckpointPacks
}

func commitTime(step int64) time.Time {
	return fixtureEpoch.Add(time.Duration(step) * time.Minute)
}

// sequentialUUID builds a version 7 UUID from a counter, so every run
// produces the same protocol keys.
func sequentialUUID(n uint64) storage.UUID {
	var u storage.UUID
	for i := range 6 {
		u[i] = byte(n >> (8 * (5 - i)))
	}
	u[6] = 0x70
	u[8] = 0x80
	return u
}

func applyStep(visible string, s Step) error {
	for _, rel := range s.Remove {
		if err := os.Remove(filepath.Join(visible, filepath.FromSlash(rel))); err != nil {
			return fmt.Errorf("remove %s: %w", rel, err)
		}
	}
	for rel, data := range s.Write {
		path := filepath.Join(visible, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return fmt.Errorf("create directory for %s: %w", rel, err)
		}
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", rel, err)
		}
	}
	return nil
}

func dumpStore(ctx context.Context, store storage.ObjectStore, dest string) error {
	keys := []string{storage.CurrentKey}
	err := store.ListObjects(ctx, "packs/", func(key string) error {
		keys = append(keys, key)
		return nil
	})
	if err != nil {
		return fmt.Errorf("list packs: %w", err)
	}
	for _, key := range keys {
		rc, _, err := store.ReadObject(ctx, key)
		if err != nil {
			return fmt.Errorf("read %s: %w", key, err)
		}
		data, err := io.ReadAll(rc)
		closeErr := rc.Close()
		if err != nil {
			return fmt.Errorf("read %s body: %w", key, err)
		}
		if closeErr != nil {
			return fmt.Errorf("close %s: %w", key, closeErr)
		}
		path := filepath.Join(dest, filepath.FromSlash(key))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create directory for %s: %w", key, err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", key, err)
		}
	}
	return nil
}

func writeExpected(dest, visible string, sc Scenario, stepOf map[string]int) error {
	data, err := os.ReadFile(filepath.Join(dest, storage.CurrentKey))
	if err != nil {
		return fmt.Errorf("read current: %w", err)
	}
	m, err := storage.DecodeManifest(data)
	if err != nil {
		return fmt.Errorf("decode current: %w", err)
	}
	files, err := readTree(visible)
	if err != nil {
		return err
	}
	exp := Expected{Generation: m.Generation, Head: m.Head.String(), Files: files}
	var missing []error
	commit := func(head git.OID) {
		n, ok := stepOf[head.String()]
		if !ok {
			missing = append(missing, fmt.Errorf("%w: head %s was not published by a step", ErrScenario, head))
			return
		}
		exp.History = append(exp.History, ExpectedCommit{
			ID:      head.String(),
			Message: sc.Steps[n-1].Message,
			Time:    commitTime(int64(n)).Unix(),
		})
	}
	for i := len(m.Increments) - 1; i >= 0; i-- {
		commit(m.Increments[i].Head)
	}
	commit(m.Checkpoint.Head)
	if err := errors.Join(missing...); err != nil {
		return err
	}
	out, err := json.MarshalIndent(exp, "", "  ")
	if err != nil {
		return fmt.Errorf("encode expected: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dest, ExpectedFile), append(out, '\n'), 0o644); err != nil {
		return fmt.Errorf("write expected: %w", err)
	}
	return nil
}

func readTree(root string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read visible files: %w", err)
	}
	return files, nil
}
