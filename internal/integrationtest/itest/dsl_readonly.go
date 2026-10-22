package itest

import "testing"

// seedReadOnlyBaseline publishes docs/faq.md and notes/a.md at generation 1
// through a writer harness with no read-only set.
func SeedReadOnlyBaseline(t *testing.T) (writer *Harness, pathWriter string) {
	t.Helper()
	writer = NewFakeHarness(t, HarnessConfig{})
	pathWriter = writer.WorkspaceRoot()
	writer.WriteFile(pathWriter+"/docs/faq.md", "answer: 1\n")
	writer.WriteFile(pathWriter+"/notes/a.md", "x\n")
	writer.AssertOK(t, writer.Pull(""))
	writer.AssertOK(t, writer.Commit("", "seed"))
	return writer, pathWriter
}

// newReadOnlyAgent wires an agent harness with entries over the writer's store.
func NewReadOnlyAgent(t *testing.T, writer *Harness, entries []string) (agent *Harness, pathAgent string) {
	t.Helper()
	agent = NewSharedHarness(t, writer.Raw(), writer.Cfg.Prefix, HarnessConfig{ReadOnlyPaths: entries})
	pathAgent = agent.WorkspaceRoot()
	return agent, pathAgent
}
