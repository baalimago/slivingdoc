package integrationtest

import (
	"path/filepath"
	"strings"
	"testing"
)

// logLines returns the helper's stderr split into records, dropping the
// container noise a shared process may emit.
func logLines(t *testing.T, h *helperProc) []string {
	t.Helper()
	var out []string
	for line := range strings.SplitSeq(h.stderrText(t), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// TestScenarioLoggingRecordShape proves the process writes structured,
// timestamped, module-tagged records to stderr and nothing but protocol to
// stdout (architecture section 17, L1093). The record shape is the
// operator's only view of a server an MCP host started.
func TestScenarioLoggingRecordShape(t *testing.T) {
	t.Parallel()
	h := spawnHelper(t, "fake", []string{"NO_COLOR=1"}, "serve")
	cs := h.connectClient(t)
	assertProcessCallOK(t, cs, toolPull, filepath.Join(h.workspaceRoot, "notes"), "")
	if err := cs.Close(); err != nil {
		t.Fatalf("close MCP client: %v", err)
	}
	if code := h.waitExit(t); code != 0 {
		t.Fatalf("process exit = %d, want 0; stderr: %s", code, h.stderrText(t))
	}

	lines := logLines(t, h)
	if len(lines) == 0 {
		t.Fatal("the process logged nothing; an operator would have no view of it")
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "time=") {
			t.Fatalf("record %q does not start with a timestamp", line)
		}
		if !strings.Contains(line, "level=") || !strings.Contains(line, "msg=") {
			t.Fatalf("record %q is not the structured shape", line)
		}
		if !strings.Contains(line, "module=") {
			t.Fatalf("record %q carries no module, so LOG_LEVEL could not address it", line)
		}
	}

	joined := strings.Join(lines, "\n")
	// This helper explicitly sets NO_COLOR, so it is also the process-level
	// proof that the convention suppresses ANSI escapes. Keeping that
	// assertion here avoids starting an otherwise identical helper solely for
	// the second half of the colour scenario.
	if strings.Contains(joined, "\033[") {
		t.Fatalf("NO_COLOR helper stderr carries ANSI escapes: %q", joined)
	}
	// The tool call is correlated: its module and its request id are both on
	// the record, which is what makes a concurrent log readable.
	if !strings.Contains(joined, "module=mcp") || !strings.Contains(joined, "mcpReqID=") {
		t.Fatalf("log = %q, want a correlated mcp record", joined)
	}
	if !strings.Contains(joined, "module=app") {
		t.Fatalf("log = %q, want the startup record from the app module", joined)
	}
}

// TestScenarioLoggingPerModuleLevels proves LOG_LEVEL addresses modules
// independently through a real process: silencing one module leaves the
// others logging, which is the whole point of the grammar.
func TestScenarioLoggingPerModuleLevels(t *testing.T) {
	t.Parallel()
	h := spawnHelper(t, "fake", []string{"NO_COLOR=1", "LOG_LEVEL=mcp=error,info"}, "serve")
	cs := h.connectClient(t)
	assertProcessCallOK(t, cs, toolPull, filepath.Join(h.workspaceRoot, "notes"), "")
	if err := cs.Close(); err != nil {
		t.Fatalf("close MCP client: %v", err)
	}
	if code := h.waitExit(t); code != 0 {
		t.Fatalf("process exit = %d, want 0; stderr: %s", code, h.stderrText(t))
	}

	joined := strings.Join(logLines(t, h), "\n")
	// The successful tool call logs at info and warn only, so raising mcp to
	// error removes it entirely.
	if strings.Contains(joined, "module=mcp") {
		t.Fatalf("log = %q, want the mcp module silenced at error", joined)
	}
	// The default still applies to every other module: this is what proves
	// the entry was scoped rather than global.
	if !strings.Contains(joined, "module=app") {
		t.Fatalf("log = %q, want the app module still at the info default", joined)
	}
}

// TestScenarioLoggingColor proves ANSI level color is on by default. Its
// companion assertion in TestScenarioLoggingRecordShape exercises the same
// server with NO_COLOR=1 and proves that the convention disables colour.
func TestScenarioLoggingColor(t *testing.T) {
	t.Parallel()
	h := spawnHelper(t, "fake", []string{"NO_COLOR="}, "serve")
	cs := h.connectClient(t)
	assertProcessCallOK(t, cs, toolPull, filepath.Join(h.workspaceRoot, "notes"), "")
	if err := cs.Close(); err != nil {
		t.Fatalf("close MCP client: %v", err)
	}
	if code := h.waitExit(t); code != 0 {
		t.Fatalf("process exit = %d, want 0", code)
	}
	if !strings.Contains(h.stderrText(t), "\033[") {
		t.Fatalf("default-colour stderr carries no ANSI escape: %q", h.stderrText(t))
	}
}

// TestScenarioLoggingInvalidLevelStillServes proves a malformed LOG_LEVEL
// reports itself and falls back to the info default instead of refusing
// startup. Diagnostic plumbing must not be able to take the server down.
func TestScenarioLoggingInvalidLevelStillServes(t *testing.T) {
	t.Parallel()
	h := spawnHelper(t, "fake", []string{"NO_COLOR=1", "LOG_LEVEL=mcp=verbose"}, "serve")
	cs := h.connectClient(t)
	assertProcessCallOK(t, cs, toolPull, filepath.Join(h.workspaceRoot, "notes"), "")
	if err := cs.Close(); err != nil {
		t.Fatalf("close MCP client: %v", err)
	}
	if code := h.waitExit(t); code != 0 {
		t.Fatalf("process exit = %d, want 0; a bad LOG_LEVEL must not refuse startup", code)
	}
	stderr := h.stderrText(t)
	if !strings.Contains(stderr, "falling back to the default log level") {
		t.Fatalf("stderr = %q, want the fallback warning", stderr)
	}
	if !strings.Contains(stderr, "module=mcp") {
		t.Fatalf("stderr = %q, want the info default still logging the tool call", stderr)
	}
}
