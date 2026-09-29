package editorfixture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/git2"
	"github.com/baalimago/slivingdoc/internal/storage"
)

func openEngine(t *testing.T) git.Engine {
	t.Helper()
	eng := git2.New()
	if err := eng.Open(); err != nil {
		t.Fatalf("git2.Open() = %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	return eng
}

// generated runs every scenario once per test and returns the output root.
func generated(t *testing.T, eng Engine) string {
	t.Helper()
	out := t.TempDir()
	if err := Generate(context.Background(), eng, t.TempDir(), out, Scenarios()); err != nil {
		t.Fatalf("Generate() = %v", err)
	}
	return out
}

func readManifest(t *testing.T, dir string) storage.Manifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, storage.CurrentKey))
	if err != nil {
		t.Fatalf("read current = %v", err)
	}
	m, err := storage.DecodeManifest(data)
	if err != nil {
		t.Fatalf("DecodeManifest() = %v", err)
	}
	return m
}

func TestGenerateThenConform(t *testing.T) {
	eng := openEngine(t)
	out := generated(t, eng)

	report, err := Conform(eng, t.TempDir(), out)
	if err != nil {
		t.Fatalf("Conform() = %v", err)
	}
	if report.Manifests != len(Scenarios()) || report.Chains != 6 || report.Packs != 18 {
		t.Fatalf("Conform() report = %+v, want %d manifests, 6 chains, 18 packs", report, len(Scenarios()))
	}

	compaction := readManifest(t, filepath.Join(out, "compaction"))
	if len(compaction.Retained) != 2 || len(compaction.Increments) == 0 {
		t.Fatalf("compaction manifest has %d retained chains and %d increments, want a retained history and an active tail", len(compaction.Retained), len(compaction.Increments))
	}
	root := readManifest(t, filepath.Join(out, "root-checkpoint"))
	if root.Generation != 1 || len(root.Increments) != 0 {
		t.Fatalf("root manifest = generation %d with %d increments, want the bare first checkpoint", root.Generation, len(root.Increments))
	}
}

func TestGenerateDescribesTheHead(t *testing.T) {
	out := generated(t, openEngine(t))

	data, err := os.ReadFile(filepath.Join(out, "increments", ExpectedFile))
	if err != nil {
		t.Fatalf("read expected = %v", err)
	}
	var exp Expected
	if err := json.Unmarshal(data, &exp); err != nil {
		t.Fatalf("decode expected = %v", err)
	}
	m := readManifest(t, filepath.Join(out, "increments"))
	if exp.Generation != m.Generation || exp.Head != m.Head.String() {
		t.Fatalf("expected = generation %d head %s, manifest = %d %s", exp.Generation, exp.Head, m.Generation, m.Head)
	}
	if len(exp.History) != 4 || exp.History[0].ID != exp.Head || !strings.HasPrefix(exp.History[0].Message, "rename") {
		t.Fatalf("history = %+v, want four commits newest first", exp.History)
	}
	if exp.Files["renamed.md"] != "alpha\nedited\n" || len(exp.Files) != 2 {
		t.Fatalf("files = %v, want the two files of the last step", exp.Files)
	}
}

func TestGenerateIsDeterministic(t *testing.T) {
	eng := openEngine(t)
	first := generated(t, eng)
	second := generated(t, eng)
	for _, name := range []string{"compaction", "delta"} {
		a, err := os.ReadFile(filepath.Join(first, name, storage.CurrentKey))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(second, name, storage.CurrentKey))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("%s manifest differs between runs", name)
		}
	}
}

func TestGenerateRejectsAScenarioWhoseStepChangesNothing(t *testing.T) {
	eng := openEngine(t)
	sc := Scenario{Name: "noop", Steps: []Step{
		{Message: "one", Write: map[string]string{"a.md": "x"}},
		{Message: "two", Write: map[string]string{"a.md": "x"}},
	}}
	err := Generate(context.Background(), eng, t.TempDir(), t.TempDir(), []Scenario{sc})
	if err == nil {
		t.Fatal("Generate() succeeded for a step that changes nothing")
	}
}

func TestGenerateReportsABadStep(t *testing.T) {
	eng := openEngine(t)
	sc := Scenario{Name: "bad", Steps: []Step{{Message: "remove a file that is not there", Remove: []string{"absent.md"}}}}
	err := Generate(context.Background(), eng, t.TempDir(), t.TempDir(), []Scenario{sc})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Generate() = %v, want the missing file", err)
	}
}

func TestConformReportsEveryFailure(t *testing.T) {
	eng := openEngine(t)
	out := generated(t, eng)
	m := readManifest(t, filepath.Join(out, "increments"))

	corrupt := filepath.Join(out, "increments", m.Increments[0].Key.String())
	data, err := os.ReadFile(corrupt)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 0xff
	if err := os.WriteFile(corrupt, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(out, "delta", readManifest(t, filepath.Join(out, "delta")).Increments[1].Key.String())); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "root-checkpoint", storage.CurrentKey), []byte(`{"version":2}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = Conform(eng, t.TempDir(), out)
	if err == nil {
		t.Fatal("Conform() succeeded over corrupt fixtures")
	}
	for _, want := range []string{"increments", "delta", "root-checkpoint", "unsupported version"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Conform() error lacks %q: %v", want, err)
		}
	}
	if !errors.Is(err, storage.ErrIntegrity) {
		t.Errorf("Conform() error = %v, want it to wrap ErrIntegrity", err)
	}
}

func TestConformRejectsAnEmptyPack(t *testing.T) {
	eng := openEngine(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.pack"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Conform(eng, t.TempDir(), dir)
	if !errors.Is(err, git.ErrEmptyPack) {
		t.Fatalf("Conform() = %v, want ErrEmptyPack", err)
	}
}

func TestConformFailsOnAMissingDirectory(t *testing.T) {
	_, err := Conform(openEngine(t), t.TempDir(), filepath.Join(t.TempDir(), "absent"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Conform() = %v, want the missing directory", err)
	}
}

func TestMainCommands(t *testing.T) {
	eng := openEngine(t)
	dir := filepath.Join(t.TempDir(), "out")
	var stdout bytes.Buffer

	if err := Main(context.Background(), eng, []string{"generate", dir}, &stdout); err != nil {
		t.Fatalf("Main(generate) = %v", err)
	}
	if !strings.Contains(stdout.String(), "generated 4 scenarios") {
		t.Fatalf("generate output = %q", stdout.String())
	}
	stdout.Reset()
	if err := Main(context.Background(), eng, []string{"conform", dir}, &stdout); err != nil {
		t.Fatalf("Main(conform) = %v", err)
	}
	if !strings.Contains(stdout.String(), "conformed 4 manifests, 6 chains, 18 packs") {
		t.Fatalf("conform output = %q", stdout.String())
	}
	for _, args := range [][]string{nil, {"generate"}, {"bogus", dir}, {"a", "b", "c"}} {
		if err := Main(context.Background(), eng, args, &stdout); !errors.Is(err, ErrUsage) {
			t.Errorf("Main(%v) = %v, want ErrUsage", args, err)
		}
	}
	if err := Main(context.Background(), eng, []string{"conform", filepath.Join(dir, "absent")}, &stdout); err == nil {
		t.Error("Main(conform) succeeded on a missing directory")
	}
}
