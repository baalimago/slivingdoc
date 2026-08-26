package pathutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExpandHome expands the current user's home directory for paths beginning
// with ~ or ~/ (and the platform equivalent). Other paths are unchanged.
// User-name expansions such as ~alice are intentionally not supported because
// their lookup is not portable across operating systems.
func ExpandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") &&
		!strings.HasPrefix(path, "~"+string(filepath.Separator)) {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}
