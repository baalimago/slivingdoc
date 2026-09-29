package editorfixture

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/storage"
)

// Report counts what Conform checked.
type Report struct {
	// Manifests is the number of `current` files decoded.
	Manifests int
	// Chains is the number of checkpoint chains walked: the active chain and
	// every retained chain of each manifest.
	Chains int
	// Packs is the number of pack files imported on their own.
	Packs int
}

// Conform proves a directory against the reference implementation. Every
// `.pack` file must import into an empty repository; every `current` file
// must decode as a manifest, and each of its chains (active and retained)
// must have its packs present with the size and SHA-256 the descriptor
// records, import checkpoint first with the shallow boundary recorded, pass
// ValidateHistory and read as a valid text snapshot. All failures are
// reported together; the error is nil only when everything conformed.
// work is scratch space for the repositories and must exist.
func Conform(eng Engine, work, dir string) (Report, error) {
	var (
		report    Report
		manifests []string
		packs     []string
	)
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		switch {
		case d.Name() == storage.CurrentKey:
			manifests = append(manifests, path)
		case strings.HasSuffix(d.Name(), ".pack"):
			packs = append(packs, path)
		}
		return nil
	})
	if walkErr != nil {
		return report, fmt.Errorf("editorfixture: conform: walk %s: %w", dir, walkErr)
	}
	c := checker{eng: eng, work: work}
	var errs []error
	for _, path := range packs {
		report.Packs++
		if err := c.importAlone(path); err != nil {
			errs = append(errs, err)
		}
	}
	for _, path := range manifests {
		report.Manifests++
		chains, err := c.manifest(path)
		report.Chains += chains
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return report, fmt.Errorf("editorfixture: conform %s: %w", dir, errors.Join(errs...))
	}
	return report, nil
}

type checker struct {
	eng  Engine
	work string
}

func (c checker) newRepo() (git.Repository, error) {
	path, err := os.MkdirTemp(c.work, "repo-*")
	if err != nil {
		return nil, fmt.Errorf("create repository directory: %w", err)
	}
	repo, err := c.eng.CreateRepo(path)
	if err != nil {
		return nil, fmt.Errorf("create repository: %w", err)
	}
	return repo, nil
}

func (c checker) importAlone(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	repo, err := c.newRepo()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	defer repo.Close()
	if err := git.ImportPack(repo, data); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func (c checker) manifest(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	m, err := storage.DecodeManifest(data)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	root := filepath.Dir(path)
	var errs []error
	chains := 0
	chains++
	if err := c.chain(root, m.Head, m.Checkpoint, m.Increments); err != nil {
		errs = append(errs, fmt.Errorf("%s: active chain: %w", path, err))
	}
	for _, r := range m.Retained {
		chains++
		if err := c.chain(root, r.Head, r.Checkpoint, r.Increments); err != nil {
			errs = append(errs, fmt.Errorf("%s: retained chain at generation %d: %w", path, r.RetiredAtGeneration, err))
		}
	}
	return chains, errors.Join(errs...)
}

func (c checker) chain(root string, head git.OID, cp storage.Checkpoint, incs []storage.Increment) error {
	repo, err := c.newRepo()
	if err != nil {
		return err
	}
	defer repo.Close()
	if err := c.importPack(root, repo, cp.Key, cp.SHA256, cp.Size); err != nil {
		return err
	}
	if err := git.MarkShallow(repo, cp.Head); err != nil {
		return err
	}
	for _, inc := range incs {
		if err := c.importPack(root, repo, inc.Key, inc.SHA256, inc.Size); err != nil {
			return err
		}
	}
	if err := git.ValidateHistory(repo, head, cp.Head); err != nil {
		return err
	}
	commit, err := repo.ReadCommit(head)
	if err != nil {
		return fmt.Errorf("read head commit: %w", err)
	}
	if _, err := git.ReadSnapshot(repo, commit.Tree); err != nil {
		return err
	}
	return nil
}

func (c checker) importPack(root string, repo git.Repository, key storage.Key, sum storage.SHA256, size uint64) error {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(key.String())))
	if err != nil {
		return fmt.Errorf("read pack %s: %w", key, err)
	}
	if uint64(len(data)) != size || sha256.Sum256(data) != [32]byte(sum) {
		return fmt.Errorf("pack %s does not match its descriptor checksum and size: %w", key, storage.ErrIntegrity)
	}
	return git.ImportPack(repo, data)
}
