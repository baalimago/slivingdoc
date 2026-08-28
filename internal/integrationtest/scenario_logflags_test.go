package integrationtest

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestScenarioCLILogFlags proves the log-configuration flags end to end
// over whole one-shot processes: --log-level enables a module's DEBUG
// records, --log-timestamp=false removes exactly the time= field, and the
// timestamp stays on by default — so an embedder's log pipeline can stamp
// and route slivingdoc's stderr itself.
func TestScenarioCLILogFlags(t *testing.T) {
	t.Parallel()
	env, root := cliRoots(t)
	env = append(env, "NO_COLOR=1")
	notes := filepath.Join(root, "notes")

	code, _, stderr := runCLI(t, "fake", env,
		"pull", notes, "--log-level", "app=debug", "--log-timestamp=false")
	if code != 0 {
		t.Fatalf("pull with log flags = exit %d, want 0; stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, `msg="native engine open"`) {
		t.Fatalf("stderr = %q, want the app DEBUG record enabled by --log-level", stderr)
	}
	if strings.Contains(stderr, "time=") {
		t.Fatalf("stderr = %q, want no time= field with --log-timestamp=false", stderr)
	}

	code, _, stderr = runCLI(t, "fake", env, "pull", notes, "--log-level", "app=debug")
	if code != 0 {
		t.Fatalf("pull with --log-level = exit %d, want 0; stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "time=") {
		t.Fatalf("stderr = %q, want the default timestamp kept", stderr)
	}
}
