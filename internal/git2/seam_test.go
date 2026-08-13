package git2

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// git2goImport is split so this scanner itself does not contain the literal
// import path it rejects.
const git2goImport = "github.com/libgit2/" + "git2go"

// TestNoGitExecutableOrGit2goImport proves the two hard boundaries of the
// native engine contract: no code in the repository invokes the Git
// executable (the process must never shell out to git) and no code imports
// the git2go binding (internal/git2 is the only libgit2 boundary). The scan
// covers every Go file below the module root except build artifacts and
// scripts/: maintainer tooling there runs on developer machines, is never
// part of the shipped binary, and cuts releases by invoking git and npm.
func TestNoGitExecutableOrGit2goImport(t *testing.T) {
	root := moduleRoot(t)
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Dot directories are tool state, not source: .build, .git,
			// and .claude (whose worktrees carry whole checkout copies).
			if strings.HasPrefix(d.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			if d.Name() == "scripts" && filepath.Dir(path) == root {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module root: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no Go files found below the module root")
	}

	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if strings.Contains(string(src), `"`+git2goImport+`"`) {
			t.Errorf("%s imports git2go; the process must never use it", file)
		}
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, src, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, imp := range parsed.Imports {
			switch strings.Trim(imp.Path.Value, `"`) {
			case "os/exec", "syscall":
				t.Errorf("%s imports %s; the process must never invoke the git executable", file, imp.Path.Value)
			}
		}
	}
}

// moduleRoot walks up from the package directory to the directory that
// contains go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() = %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("module root (go.mod) not found above the working directory")
		}
		dir = parent
	}
}
