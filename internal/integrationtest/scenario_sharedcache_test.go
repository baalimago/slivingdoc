package integrationtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScenarioSharedPackCacheReuse proves the shared pack cache
// (architecture/pull.md and config.md): the first cold puller populates the
// identity-selected shared directory, and a second cold puller with its own
// workspace and private state imports the same state with zero pack
// downloads. Only verified pack bytes are shared; every harness keeps its
// own private repository and baseline.
func TestScenarioSharedPackCacheReuse(t *testing.T) {
	t.Parallel()
	cacheRoot := t.TempDir()
	// The writer keeps its private cache, so the shared directory is
	// populated by B's downloads alone, never by the writer's own reads.
	a := newFakeHarness(t, HarnessConfig{})
	b := newSharedHarness(t, a.Raw(), a.cfg.Prefix, HarnessConfig{PackCacheRoot: cacheRoot})
	c := newSharedHarness(t, a.Raw(), a.cfg.Prefix, HarnessConfig{PackCacheRoot: cacheRoot})
	pathA, pathB, pathC := a.Path("notes"), b.Path("notes"), c.Path("notes")

	commitFirst(t, a, pathA, "a.md", "alpha", "c1")
	commitNext(t, a, pathA, "b.md", "beta", "c2")
	m := a.Manifest()
	packKey := m.Checkpoint.Key.String()
	incKey := m.Increments[0].Key.String()

	// B's cold pull downloads every descriptor pack once and populates the
	// shared directory under the identity-selected name.
	b.assertOK(t, b.Pull("", pathB))
	for _, key := range []string{packKey, incKey} {
		if got := b.Recorder().CountKey(OpGet, key); got != 1 {
			t.Fatalf("first cold pull gets of %s = %d, want exactly one", key, got)
		}
	}
	shared := b.SharedPackCacheDir(t)
	for _, sha := range []string{m.Checkpoint.SHA256.String(), m.Increments[0].SHA256.String()} {
		if _, err := os.Stat(filepath.Join(shared, sha)); err != nil {
			t.Fatalf("shared cache entry %s: %v", sha, err)
		}
	}

	// C's cold pull is served entirely from the shared cache: zero pack
	// downloads, one current read, and the exact visible state.
	c.assertOK(t, c.Pull("", pathC))
	if got := c.Recorder().CountKeyPrefix(OpGet, "packs/"); got != 0 {
		t.Fatalf("shared-cache cold pull pack gets = %d, want 0", got)
	}
	assertVisibleFiles(t, c, pathC, map[string]string{"a.md": "alpha", "b.md": "beta"})
	assertRemoteGeneration(t, c, pathC, 2)
}

// TestScenarioSharedPackCacheUnwritable proves that the shared cache is
// only an optimization: with a cache root that cannot be created (a path
// below a regular file, standing in for a read-only mount or a permission
// wall), every pull still succeeds, the degradation is observable as a
// warning, and each new workspace simply downloads the packs it lacks.
func TestScenarioSharedPackCacheUnwritable(t *testing.T) {
	t.Parallel()
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}
	cacheRoot := filepath.Join(blocker, "pack-cache")
	a := newFakeHarness(t, HarnessConfig{})
	b := newSharedHarness(t, a.Raw(), a.cfg.Prefix, HarnessConfig{PackCacheRoot: cacheRoot})
	c := newSharedHarness(t, a.Raw(), a.cfg.Prefix, HarnessConfig{PackCacheRoot: cacheRoot})
	pathA, pathB, pathC := a.Path("notes"), b.Path("notes"), c.Path("notes")

	commitFirst(t, a, pathA, "a.md", "alpha", "c1")
	packKey := a.Manifest().Checkpoint.Key.String()

	b.assertOK(t, b.Pull("", pathB))
	assertVisibleFiles(t, b, pathB, map[string]string{"a.md": "alpha"})
	b.assertExpectations(t, Expectations{
		Logs: &LogExpectations{WarnContains: []string{"pack cache write failed"}},
	})

	// The cache never becomes a hit, and never becomes a failure: the next
	// workspace downloads the same pack again and warns again.
	c.assertOK(t, c.Pull("", pathC))
	if got := c.Recorder().CountKey(OpGet, packKey); got != 1 {
		t.Fatalf("pack gets of a second workspace with an unwritable cache = %d, want 1", got)
	}
	c.assertExpectations(t, Expectations{
		Logs: &LogExpectations{WarnContains: []string{"pack cache write failed"}},
	})
}

// TestScenarioSharedPackCacheCorruption proves that a corrupt shared entry
// is never a false hit across agents: the next agent that needs the pack
// discards it, re-downloads the verified bytes, and heals the shared
// directory for every other agent (architecture/pull.md and config.md). An
// agent whose repository already holds the pack never consults the entry.
func TestScenarioSharedPackCacheCorruption(t *testing.T) {
	t.Parallel()
	cacheRoot := t.TempDir()
	a := newFakeHarness(t, HarnessConfig{})
	b := newSharedHarness(t, a.Raw(), a.cfg.Prefix, HarnessConfig{PackCacheRoot: cacheRoot})
	c := newSharedHarness(t, a.Raw(), a.cfg.Prefix, HarnessConfig{PackCacheRoot: cacheRoot})
	d := newSharedHarness(t, a.Raw(), a.cfg.Prefix, HarnessConfig{PackCacheRoot: cacheRoot})
	pathA, pathB, pathC, pathD := a.Path("notes"), b.Path("notes"), c.Path("notes"), d.Path("notes")

	commitFirst(t, a, pathA, "a.md", "alpha", "c1")
	m := a.Manifest()
	packKey := m.Checkpoint.Key.String()
	b.assertOK(t, b.Pull("", pathB))

	cacheFile := filepath.Join(b.SharedPackCacheDir(t), m.Checkpoint.SHA256.String())
	b.WriteFile(cacheFile, "corrupt shared entry")

	// B holds the objects and needs nothing; C is cold, discards the corrupt
	// entry, re-downloads, and heals the shared file.
	b.assertOK(t, b.Pull("", pathB))
	if got := b.Recorder().CountKeyPrefix(OpGet, "packs/"); got != 1 {
		t.Fatalf("pack gets of the agent holding the objects = %d, want only its first cold pull", got)
	}
	c.assertOK(t, c.Pull("", pathC))
	if got := c.Recorder().CountKey(OpGet, packKey); got != 1 {
		t.Fatalf("pack gets after shared corruption = %d, want the re-download", got)
	}
	wantBytes, err := c.ReadObject(packKey)
	if err != nil {
		t.Fatalf("read pack through the raw store: %v", err)
	}
	gotBytes, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatalf("shared cache entry after the pull: %v", err)
	}
	if string(gotBytes) != string(wantBytes) {
		t.Fatalf("shared cache entry is not healed to the verified pack bytes")
	}

	// The healed entry serves the next agent without a download.
	d.assertOK(t, d.Pull("", pathD))
	if got := d.Recorder().CountKeyPrefix(OpGet, "packs/"); got != 0 {
		t.Fatalf("pack gets after the healed shared cache = %d, want 0", got)
	}
	assertVisibleFiles(t, d, pathD, map[string]string{"a.md": "alpha"})
}

// TestScenarioSharedPackCacheReusesStoreProof proves the shared cache also
// carries the store compatibility proof (architecture/storage.md): the
// first one-shot process probes the store and records the proof below the
// identity-selected shared directory, a later process of the same identity
// starts without probing and says so, and a process with another user cache
// directory probes again. The proof is observable only through the process
// boundary, because the probe lives in the process body.
func TestScenarioSharedPackCacheReusesStoreProof(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	env, root := cliRoots(t)
	env = append(env, helperCacheEnv+"="+cacheDir, "NO_COLOR=1", "LOG_LEVEL=app=debug,info")
	notes := filepath.Join(root, "notes")
	const probed, reused = "store compatibility probed", "store compatibility proof reused"

	code, _, stderr := runCLI(t, "fake", env, "pull", notes)
	if code != 0 || !strings.Contains(stderr, probed) || strings.Contains(stderr, reused) {
		t.Fatalf("first pull = exit %d, want 0 and a probe record; stderr: %s", code, stderr)
	}
	proofs, err := filepath.Glob(filepath.Join(cacheDir, "slivingdoc", "pack-cache", "*", "probe-ok.json"))
	if err != nil || len(proofs) != 1 {
		t.Fatalf("proof records below the shared cache = %v, %v; want exactly one", proofs, err)
	}

	code, _, stderr = runCLI(t, "fake", env, "pull", notes)
	if code != 0 || !strings.Contains(stderr, reused) || strings.Contains(stderr, probed) {
		t.Fatalf("second pull = exit %d, want 0 and the reused proof; stderr: %s", code, stderr)
	}

	other := append(append([]string(nil), env...), helperCacheEnv+"="+t.TempDir())
	code, _, stderr = runCLI(t, "fake", other, "pull", notes)
	if code != 0 || !strings.Contains(stderr, probed) || strings.Contains(stderr, reused) {
		t.Fatalf("pull with another cache directory = exit %d, want 0 and a probe; stderr: %s", code, stderr)
	}
}
