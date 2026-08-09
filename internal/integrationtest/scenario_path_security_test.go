package integrationtest

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestScenarioPathSecurityOverlappingRoots proves the portable half of the
// path-security catalog: a private root at or below the workspace root is
// refused at startup, before any transport serves a call (architecture
// section 17, L1040; section 18.2, L1131).
//
// The two roots must stay disjoint because P is server-owned state that a
// caller may never observe or edit through L. The check therefore belongs
// to configuration, not to a request path, and the process must refuse to
// start rather than serve calls over an unsafe layout.
func TestScenarioPathSecurityOverlappingRoots(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name    string
		private func(root string) string
	}{
		{name: "private root below workspace root", private: func(root string) string { return filepath.Join(root, "private") }},
		{name: "private root equals workspace root", private: func(root string) string { return root }},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			h := spawnHelper(t, "fake", nil, "serve",
				"--workspace-root="+root,
				"--private-root="+row.private(root))
			code, stdout, stderr := h.runStdioProcess(t, nil)
			if code != 1 {
				t.Fatalf("overlapping roots exit = %d, want 1", code)
			}
			if strings.TrimSpace(stdout) != "" {
				t.Fatalf("overlapping roots wrote stdout: %q", stdout)
			}
			if !strings.Contains(stderr, "invalid configuration") || !strings.Contains(stderr, "private root") {
				t.Fatalf("overlapping roots stderr = %q, want one configuration diagnostic", stderr)
			}
		})
	}
}
