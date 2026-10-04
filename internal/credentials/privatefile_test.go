package credentials

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigDir proves the configuration directory is resolved by one
// rule for every configuration file, and that a directory which does not
// resolve names the cause without naming a package.
func TestConfigDir(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(name string) string { return vars[name] }
	}
	for _, tt := range []struct {
		name string
		vars map[string]string
		goos string
		want string
	}{
		{"override", map[string]string{DirEnv: "/cfg/x/"}, "linux", "/cfg/x"},
		{"xdg", map[string]string{"XDG_CONFIG_HOME": "/xdg"}, "linux", "/xdg/slivingdoc"},
		{"darwin", map[string]string{"HOME": "/Users/u"}, "darwin", "/Users/u/Library/Application Support/slivingdoc"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir, uid, err := ConfigDir(env(tt.vars), tt.goos)
			if err != nil {
				t.Fatalf("ConfigDir() = %v", err)
			}
			if want := filepath.FromSlash(tt.want); dir != want {
				t.Fatalf("ConfigDir() = %q, want %q", dir, want)
			}
			if uid < 0 {
				t.Fatalf("ConfigDir() user = %d, want the effective one", uid)
			}
		})
	}
	for _, tt := range []struct {
		name string
		vars map[string]string
		goos string
	}{
		{"relative override", map[string]string{DirEnv: "cfg"}, "linux"},
		{"nothing", nil, "linux"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ConfigDir(env(tt.vars), tt.goos)
			var cause NoConfigDir
			if !errors.As(err, &cause) {
				t.Fatalf("ConfigDir() = %v, want a NoConfigDir", err)
			}
			wanted := NoDirectory(ErrNoConfigDir, err)
			if !errors.Is(wanted, ErrNoConfigDir) || !strings.Contains(wanted.Error(), cause.Reason) ||
				strings.Contains(wanted.Error(), "privatefile") {
				t.Fatalf("NoDirectory() = %v, want ErrNoConfigDir naming %q", wanted, cause.Reason)
			}
			if !strings.Contains(cause.Error(), cause.Reason) {
				t.Fatalf("NoConfigDir.Error() = %q, want the reason", cause.Error())
			}
		})
	}
}

// TestNoDirectoryKeepsAnotherCause proves a failure that is not the
// configuration directory keeps its own chain.
func TestNoDirectoryKeepsAnotherCause(t *testing.T) {
	cause := errors.New("the disk is on fire")
	err := NoDirectory(ErrNoConfigDir, cause)
	if !errors.Is(err, ErrNoConfigDir) || !errors.Is(err, cause) {
		t.Fatalf("NoDirectory() = %v, want both errors in the chain", err)
	}
}
