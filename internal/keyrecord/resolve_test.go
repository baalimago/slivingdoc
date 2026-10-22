package keyrecord

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/crypt"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// TestKeyRecordCandidateOrder proves the resolver returns the environment
// value, then the value its envfile holds, then the stored key-record value,
// and only the sources that are present.
func TestKeyRecordCandidateOrder(t *testing.T) {
	dir := testDir(t)
	id := testIdentity("order")
	envFile := filepath.Join(testDir(t), "env")
	writeFile(t, envFile, "SLIVINGDOC_PASSPHRASE=from-file\n", 0o600)
	file, err := Locate(lookup(map[string]string{credentials.DirEnv: dir}), runtime.GOOS)
	if err != nil {
		t.Fatalf("Locate() = %v", err)
	}
	entry, err := Passphrase(workspace.IdentityDigest(id), "from-record")
	if err != nil {
		t.Fatalf("Passphrase() = %v", err)
	}
	if err := file.Update(context.Background(), entry); err != nil {
		t.Fatalf("Update() = %v", err)
	}
	tests := []struct {
		name        string
		env         map[string]string
		wantSources []crypt.CandidateSource
		wantSecrets []string
	}{
		{
			"environment, envfile, then record",
			map[string]string{credentials.DirEnv: dir, PassphraseEnv: "from-env", PassphraseFileEnv: envFile},
			[]crypt.CandidateSource{crypt.SourceEnvironment, crypt.SourceEnvironmentFile, crypt.SourceKeyRecord},
			[]string{"from-env", "from-file", "from-record"},
		},
		{
			"environment before the record",
			map[string]string{credentials.DirEnv: dir, PassphraseEnv: "from-env"},
			[]crypt.CandidateSource{crypt.SourceEnvironment, crypt.SourceKeyRecord},
			[]string{"from-env", "from-record"},
		},
		{
			"envfile before the record",
			map[string]string{credentials.DirEnv: dir, PassphraseFileEnv: envFile},
			[]crypt.CandidateSource{crypt.SourceEnvironmentFile, crypt.SourceKeyRecord},
			[]string{"from-file", "from-record"},
		},
		{
			"record only",
			map[string]string{credentials.DirEnv: dir},
			[]crypt.CandidateSource{crypt.SourceKeyRecord},
			[]string{"from-record"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates, err := Resolve(id, lookup(tt.env))
			if err != nil {
				t.Fatalf("Resolve() = %v", err)
			}
			if len(candidates) != len(tt.wantSources) {
				t.Fatalf("Resolve() = %d candidates, want %d", len(candidates), len(tt.wantSources))
			}
			for i, want := range tt.wantSources {
				if candidates[i].Source != want || string(candidates[i].Secret) != tt.wantSecrets[i] {
					t.Fatalf("candidate %d = source %d secret %q, want source %d secret %q",
						i, candidates[i].Source, candidates[i].Secret, want, tt.wantSecrets[i])
				}
			}
		})
	}
	t.Run("no candidate", func(t *testing.T) {
		candidates, err := Resolve(id, lookup(map[string]string{credentials.DirEnv: testDir(t)}))
		if err != nil {
			t.Fatalf("Resolve() = %v", err)
		}
		if len(candidates) != 0 {
			t.Fatalf("Resolve() = %d candidates, want none", len(candidates))
		}
	})
	t.Run("no configuration directory keeps the environment candidate", func(t *testing.T) {
		candidates, err := Resolve(id, lookup(map[string]string{PassphraseEnv: "from-env"}))
		if err != nil {
			t.Fatalf("Resolve() without a configuration directory = %v", err)
		}
		if len(candidates) != 1 || candidates[0].Source != crypt.SourceEnvironment {
			t.Fatalf("Resolve() = %d candidates, want the environment one alone", len(candidates))
		}
	})
	t.Run("a record that names an envfile resolves its value", func(t *testing.T) {
		other := testIdentity("named-file")
		entry, err := PassphraseFile(workspace.IdentityDigest(other), envFile)
		if err != nil {
			t.Fatalf("PassphraseFile() = %v", err)
		}
		if err := file.Update(context.Background(), entry); err != nil {
			t.Fatalf("Update() = %v", err)
		}
		candidates, err := Resolve(other, lookup(map[string]string{credentials.DirEnv: dir}))
		if err != nil {
			t.Fatalf("Resolve() = %v", err)
		}
		if len(candidates) != 1 || candidates[0].Source != crypt.SourceKeyRecord || string(candidates[0].Secret) != "from-file" {
			t.Fatalf("Resolve() = %+v, want one record candidate of the envfile value", candidates)
		}
	})
}

// TestKeyRecordIdentityIsolation proves key material is scoped to one identity
// digest: another notebook neither sees it nor overwrites it.
func TestKeyRecordIdentityIsolation(t *testing.T) {
	dir := testDir(t)
	a, b := testIdentity("a"), testIdentity("b")
	if workspace.IdentityDigest(a) == workspace.IdentityDigest(b) {
		t.Fatal("two identities share a digest")
	}
	file, err := Locate(lookup(map[string]string{credentials.DirEnv: dir}), runtime.GOOS)
	if err != nil {
		t.Fatalf("Locate() = %v", err)
	}
	entryA, err := Passphrase(workspace.IdentityDigest(a), "a-secret")
	if err != nil {
		t.Fatalf("Passphrase() = %v", err)
	}
	entryB, err := Passphrase(workspace.IdentityDigest(b), "b-secret")
	if err != nil {
		t.Fatalf("Passphrase() = %v", err)
	}
	if err := file.Update(context.Background(), entryA); err != nil {
		t.Fatalf("Update() = %v", err)
	}
	env := lookup(map[string]string{credentials.DirEnv: dir})
	candidates, err := Resolve(b, env)
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("Resolve(b) = %+v, want no candidate from another identity's record", candidates)
	}
	if err := file.Update(context.Background(), entryB); err != nil {
		t.Fatalf("Update() = %v", err)
	}
	set, err := file.Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if len(set.Entries()) != 2 {
		t.Fatalf("Entries() = %d, want a record per identity", len(set.Entries()))
	}
	if stored, err := set.Lookup(workspace.IdentityDigest(a)); err != nil || stored.Value() != "a-secret" {
		t.Fatalf("Lookup(a) = %q, %v; want the untouched record", stored.Value(), err)
	}
}

// TestPassphraseEnvfile proves the envfile grammar and its hardening: only the
// documented assignments are accepted, and nothing names the file's path.
func TestPassphraseEnvfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, tt := range []struct {
		name, data, want string
	}{
		{"bare", "SLIVINGDOC_PASSPHRASE=hunter2\n", "hunter2"},
		{"comment and blank lines", "# a comment\n\nSLIVINGDOC_PASSPHRASE=hunter2\n", "hunter2"},
		{"indented comment", "  # a comment\nSLIVINGDOC_PASSPHRASE=hunter2\n", "hunter2"},
		{"double quoted", `SLIVINGDOC_PASSPHRASE="hunter 2"`, "hunter 2"},
		{"single quoted", `SLIVINGDOC_PASSPHRASE='hunter 2'`, "hunter 2"},
		{"export prefix", "export SLIVINGDOC_PASSPHRASE=hunter2", "hunter2"},
		{"other variables", "OTHER=x\nSLIVINGDOC_PASSPHRASE=hunter2\n", "hunter2"},
		{"tilde expanded", "SLIVINGDOC_PASSPHRASE=~/secret", filepath.Join(home, "secret")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := envfilePassphrase([]byte(tt.data))
			if err != nil {
				t.Fatalf("envfilePassphrase(%q) = %v", tt.data, err)
			}
			if got != tt.want {
				t.Fatalf("envfilePassphrase(%q) = %q, want %q", tt.data, got, tt.want)
			}
		})
	}
	for _, tt := range []struct{ name, data string }{
		{"empty value", "SLIVINGDOC_PASSPHRASE=\n"},
		{"unbalanced quote", `SLIVINGDOC_PASSPHRASE="hunter2`},
		{"text after the closing quote", `SLIVINGDOC_PASSPHRASE="hunter2"x`},
		{"not an assignment", "SLIVINGDOC_PASSPHRASE\n"},
		{"no passphrase variable", "OTHER=x\n"},
	} {
		t.Run("refuses "+tt.name, func(t *testing.T) {
			if _, err := envfilePassphrase([]byte(tt.data)); err == nil {
				t.Fatalf("envfilePassphrase(%q) = nil, want a refusal", tt.data)
			}
		})
	}

	dir := testDir(t)
	path := filepath.Join(dir, "key.env")
	writeFile(t, path, "SLIVINGDOC_PASSPHRASE=from-file\n", 0o600)
	if got, err := readPassphraseFile(PassphraseFileEnv, path); err != nil || got != "from-file" {
		t.Fatalf("readPassphraseFile() = %q, %v; want the stored value", got, err)
	}
	missing := filepath.Join(dir, "absent.env")
	if _, err := readPassphraseFile(PassphraseFileEnv, missing); !errors.Is(err, ErrEnvfile) ||
		!strings.Contains(err.Error(), PassphraseFileEnv) || strings.Contains(err.Error(), missing) {
		t.Fatalf("readPassphraseFile(missing) = %v, want ErrEnvfile naming the setting only", err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no POSIX owners, and symbolic links need a privilege")
	}
	link := filepath.Join(dir, "link.env")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readPassphraseFile(PassphraseFileEnv, link); !errors.Is(err, ErrEnvfile) || strings.Contains(err.Error(), link) {
		t.Fatalf("readPassphraseFile(symlink) = %v, want ErrEnvfile without the path", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPassphraseFile(PassphraseFileEnv, path); !errors.Is(err, ErrEnvfile) || strings.Contains(err.Error(), path) {
		t.Fatalf("readPassphraseFile(exposed) = %v, want ErrEnvfile without the path", err)
	}
}
