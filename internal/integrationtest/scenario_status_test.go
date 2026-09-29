package integrationtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScenarioCLIStatusAndLog proves the read-only commands over one-shot
// processes: status lists exactly what a commit would publish and changes
// nothing, log lists the accepted publications newest first with their
// messages, and both fail with a clear pointer before the first pull.
func TestScenarioCLIStatusAndLog(t *testing.T) {
	t.Parallel()
	env, root, _ := realCLIEnv(t, "integrationtest-status")
	notes := filepath.Join(root, "notes")

	code, stdout, _ := runCLI(t, "real", env, "status", notes)
	if code != 0 || !strings.Contains(stdout, "not pulled yet") {
		t.Fatalf("status before pull = exit %d %q, want exit 0 pointing at pull", code, stdout)
	}

	runCLIOK(t, "real", env, nil, "pull", notes)
	writeCLIFile(t, filepath.Join(notes, "a.md"), "one\ntwo\n")
	writeCLIFile(t, filepath.Join(notes, "keep.md"), "keep\n")
	runCLIOK(t, "real", env, nil, "commit", notes, "-m", "first notes")

	writeCLIFile(t, filepath.Join(notes, "a.md"), "one\nthree\nfour\n")
	writeCLIFile(t, filepath.Join(notes, "new.md"), "fresh\n")
	writeCLIFile(t, filepath.Join(notes, ".DS_Store"), "\x00junk")
	if err := os.Remove(filepath.Join(notes, "keep.md")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCLI(t, "real", env, "status", notes)
	if code != 0 {
		t.Fatalf("status = exit %d, stderr %s", code, stderr)
	}
	for _, want := range []string{
		"generation 1\n",
		"  modified  a.md  +2 -1\n",
		"  added     new.md  +1\n",
		"  deleted   keep.md  -1\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("status = %q, want it to contain %q", stdout, want)
		}
	}
	if strings.Contains(stdout, "DS_Store") || strings.Contains(stdout, "\x1b[") {
		t.Fatalf("status = %q, want ignored files and colour absent", stdout)
	}
	if got, err := os.ReadFile(filepath.Join(notes, "new.md")); err != nil || string(got) != "fresh\n" {
		t.Fatalf("new.md = %q, %v: status must not change the directory", got, err)
	}

	runCLIOK(t, "real", env, nil, "commit", notes, "-m", "second edit\n\nlong body")
	code, stdout, stderr = runCLI(t, "real", env, "log", notes)
	if code != 0 {
		t.Fatalf("log = exit %d, stderr %s", code, stderr)
	}
	first := strings.Index(stdout, "second edit …")
	second := strings.Index(stdout, "first notes")
	if first < 0 || second < first {
		t.Fatalf("log = %q, want the second edit before the first notes", stdout)
	}
	code, stdout, _ = runCLI(t, "real", env, "log", "--limit", "1", notes)
	if code != 0 || strings.Contains(stdout, "first notes") || !strings.Contains(stdout, "older publications exist") {
		t.Fatalf("log --limit 1 = exit %d %q, want one entry and the older-publications note", code, stdout)
	}
	if code, _, stderr = runCLI(t, "real", env, "log", "--limit", "0", notes); code == 0 || !strings.Contains(stderr, "--limit must be at least 1") {
		t.Fatalf("log --limit 0 = exit %d %q, want a refusal", code, stderr)
	}
}

// TestScenarioCLIVersionFlag proves the root --version flag prints the same
// single line as the version command, before any dependency loads.
func TestScenarioCLIVersionFlag(t *testing.T) {
	t.Parallel()
	env, _ := cliRoots(t)
	_, want, _ := runCLI(t, "fake", env, "version")
	code, got, stderr := runCLI(t, "fake", env, "--version")
	if code != 0 || got != want || !strings.HasPrefix(got, "slivingdoc ") {
		t.Fatalf("--version = exit %d %q (stderr %q), want %q", code, got, stderr, want)
	}
}
