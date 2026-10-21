package itest

import (
	"path/filepath"
	"testing"
)

// writableRefusal is the refusal text of a policy whose writable set is the
// given entries: it names where the agent may write, not the protected
// region.
func WritableRefusal(writable string) string {
	return "Only " + writable + " is writable in this server. Your changes elsewhere were discarded and the files reset. " +
		"Write under the writable paths, then commit again."
}

// seedWritableBaseline publishes the fixture the writable scenarios start
// from at generation 1, through a writer with no policy configured.
func SeedWritableBaseline(t *testing.T) (writer *Harness, pathWriter string) {
	t.Helper()
	writer = NewFakeHarness(t, HarnessConfig{})
	pathWriter = writer.WorkspaceRoot()
	for name, data := range WritableBaseline() {
		writer.WriteFile(pathWriter+"/"+name, data)
	}
	writer.AssertOK(t, writer.Pull(""))
	writer.AssertOK(t, writer.Commit("", "seed"))
	return writer, pathWriter
}

// writableBaseline is the accepted state every writable scenario starts
// from: a protected directory holding a writable subdirectory, a writable
// directory holding a protected subdirectory, one sibling of each, and a
// third level below both, so a set can hold an entry below another one.
func WritableBaseline() map[string]string {
	return map[string]string{
		"docs/faq.md":                    "answer: 1\n",
		"docs/open/c.md":                 "open: 1\n",
		"docs/open/deep/e.md":            "deep: 1\n",
		"notes/a.md":                     "x\n",
		"notes/agent-a/free.md":          "free: 1\n",
		"notes/agent-a/locked/secret.md": "secret: 1\n",
		"notes/locked/d.md":              "locked: 1\n",
		"team/b.md":                      "t\n",
	}
}

// newWritableAgent wires an agent harness with the two entry sets over the
// writer's store and pulls the accepted state into its visible directory.
func NewWritableAgent(t *testing.T, writer *Harness, readOnly, writable []string) (agent *Harness, pathAgent string) {
	t.Helper()
	agent = NewSharedHarness(t, writer.Raw(), writer.Cfg.Prefix, HarnessConfig{
		ReadOnlyPaths: readOnly,
		WritablePaths: writable,
	})
	pathAgent = agent.WorkspaceRoot()
	agent.AssertOK(t, agent.Pull(""))
	return agent, pathAgent
}

// assertAddedPathRefused commits one file the writable set does not cover
// and asserts the refusal, the removal of the added file, and an unchanged
// generation.
func AssertAddedPathRefused(t *testing.T, relPath, content, message string) {
	t.Helper()
	writer, _ := SeedWritableBaseline(t)
	agent, pathAgent := NewWritableAgent(t, writer, nil, []string{"notes"})

	generationBefore := agent.Manifest().Generation
	agent.WriteFile(pathAgent+"/"+relPath, content)

	agent.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathAgent, Message: message,
		Expect: CallExpectation{
			ErrorCode: "INVALID_REQUEST",
			Reason:    "READ_ONLY_PATH",
			Files:     []FileExpectation{{Path: relPath, Reason: "READ_ONLY", Ranges: []RangeExpectation{}}},
		},
	}, agent.Commit("", message))
	AssertVisibleFiles(t, agent, WritableBaseline())
	if got := agent.Manifest().Generation; got != generationBefore {
		t.Fatalf("manifest generation changed by the refused commit: %d -> %d", generationBefore, got)
	}
}

// seedWritableCLIBaseline publishes generation 1 on the given real-backend
// prefix: one file inside the writable set and one outside it. The writer
// publishes through the in-process harness, since the property under test
// lives in the one-shot CLI processes rather than in the seeding.
func SeedWritableCLIBaseline(t *testing.T, prefix string) *Harness {
	t.Helper()
	writer := NewHarness(t, HarnessConfig{Prefix: prefix})
	seed := writer.WorkspaceRoot()
	writer.WriteFile(filepath.Join(seed, "work", "a.md"), "x\n")
	writer.WriteFile(filepath.Join(seed, "shared", "b.md"), "t\n")
	writer.AssertOK(t, writer.Pull(""))
	writer.AssertOK(t, writer.Commit("", "seed"))
	return writer
}

// wantWritableInstructions and wantWritableDescription are the sentences a
// process configured with the single writable entry "notes" adds to its
// instructions and to both tool descriptions.
const (
	WantWritableInstructions = " Writable paths: notes. notes_commit refuses any change elsewhere, " +
		"resets those files, and reports READ_ONLY_PATH; write only under them."
	WantWritableDescription = " Writable paths in this server: notes; changes elsewhere are refused and reset."
)
