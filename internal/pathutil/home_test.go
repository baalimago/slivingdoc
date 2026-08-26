package pathutil

import (
	"path/filepath"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("HOME", home)
	for _, tt := range []struct {
		name string
		path string
		want string
	}{
		{name: "bare home", path: "~", want: home},
		{name: "home child", path: "~/notes", want: filepath.Join(home, "notes")},
		{name: "relative unchanged", path: "notes", want: "notes"},
		{name: "named user unchanged", path: "~alice/notes", want: "~alice/notes"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExpandHome(tt.path)
			if err != nil {
				t.Fatalf("ExpandHome(%q) = %v", tt.path, err)
			}
			if got != tt.want {
				t.Fatalf("ExpandHome(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
