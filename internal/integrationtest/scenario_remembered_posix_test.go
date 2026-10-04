//go:build !windows && !plan9

package integrationtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/credentials"
)

// TestScenarioSettingsFileMustBeTheUsers proves the settings file needs the
// same hardening the credentials file has, at the command line: a symbolic
// link, a FIFO, a file another user could read, a directory another user
// could write and a file over the bound refuse startup, and none of them is
// rewritten. Every row names the file and the way to run without it.
func TestScenarioSettingsFileMustBeTheUsers(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name  string
		plant func(t *testing.T, file string)
		want  string
	}{
		{name: "a symbolic link", plant: func(t *testing.T, file string) {
			moved := filepath.Join(t.TempDir(), "planted.json")
			mustDo(t, os.Rename(file, moved))
			mustDo(t, os.Symlink(moved, file))
		}, want: "is a symbolic link"},
		{name: "a FIFO", plant: func(t *testing.T, file string) {
			mustDo(t, os.Remove(file))
			mustDo(t, mkfifo(file))
		}, want: "is not a regular file"},
		{name: "a group-readable file", plant: func(t *testing.T, file string) {
			mustDo(t, os.Chmod(file, 0o640))
		}, want: "chmod 600"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			g, site, env, root := rememberedEnv(t)
			remember(t, env, filepath.Join(root, "notes"), secondSpace, hostedPrefix)
			row.plant(t, settingsPath(env))
			before := reach(g, site)
			args := []string{"pull", filepath.Join(root, "notes")}
			code, stdout, stderr := runCLI(t, "real", env, args...)
			assertRefusal(t, args, code, stdout, stderr, row.want, "remove it to choose the space with --space")
			if after := reach(g, site); after != before {
				t.Fatalf("the refusal reached a store: %q then %q", before, after)
			}
			if strings.Contains(stderr, loginKey) || strings.Contains(stderr, hostedToken) {
				t.Fatalf("stderr = %q, want no credential", stderr)
			}
		})
	}
}

// TestScenarioSettingsDirectoryMustBePrivate proves the directory rule of the
// settings file alone: a configuration directory other users can write to
// refuses a pull that holds no credentials file, which is the only way to
// reach the settings check before the credentials file's own check. Another
// user could plant both files there.
func TestScenarioSettingsDirectoryMustBePrivate(t *testing.T) {
	t.Parallel()
	g, site, env, root := rememberedEnv(t)
	dir := filepath.Join(t.TempDir(), "cfg")
	mustDo(t, os.MkdirAll(dir, 0o700))
	exposed := append(append([]string(nil), env...), credentials.DirEnv+"="+dir)
	remember(t, exposed, filepath.Join(root, "notes"), secondSpace, hostedPrefix)
	mustDo(t, os.Chmod(dir, 0o770))
	before := reach(g, site)
	args := []string{"pull", filepath.Join(root, "notes")}
	code, stdout, stderr := runCLI(t, "real", exposed, args...)
	assertRefusal(t, args, code, stdout, stderr, "chmod go-w", "remove it to choose the space with --space")
	if after := reach(g, site); after != before {
		t.Fatalf("the refusal reached a store: %q then %q", before, after)
	}
}
