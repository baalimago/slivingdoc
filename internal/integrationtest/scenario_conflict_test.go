package integrationtest

import (
	"testing"
)

// TestScenarioConflictMarkerGrammar proves the marker-rejection contract
// (architecture section 12, L763): complete marker blocks are CONTENT_CONFLICT
// with the exact path and ranges before any S3 mutation; near matches and
// indented blocks are ordinary text and publish. Every row runs on a fresh
// harness so the zero-mutation counter is exact per row.
func TestScenarioConflictMarkerGrammar(t *testing.T) {
	t.Parallel()
	block := "<<<<<<< local\na\n=======\nb\n>>>>>>> remote\n"
	rows := []struct {
		name string
		data string
		want []RangeExpectation // nil means the commit publishes
	}{
		{"lf block", block, []RangeExpectation{{Start: 1, End: 5}}},
		{"crlf block", "<<<<<<< local\r\na\r\n=======\r\nb\r\n>>>>>>> remote\r\n", []RangeExpectation{{Start: 1, End: 5}}},
		{"multiple blocks", block + "clean\n" + block, []RangeExpectation{{Start: 1, End: 5}, {Start: 7, End: 11}}},
		{"block at eof", "<<<<<<< local\n=======\n>>>>>>> remote", []RangeExpectation{{Start: 1, End: 3}}},
		{"literal example", "<<<<<<< local\nthe caller's text\n=======\nthe accepted remote text\n>>>>>>> remote\n", []RangeExpectation{{Start: 1, End: 5}}},
		{"nested opener", "<<<<<<< local\n<<<<<<< local\n=======\ntext\n>>>>>>> remote\n", []RangeExpectation{{Start: 1, End: 5}}},
		{"near match label", "<<<<<<< mine\n=======\n>>>>>>> remote\n", nil},
		{"near match missing closer", "<<<<<<< local\n=======\n", nil},
		{"near match missing separator", "<<<<<<< local\ntext\n>>>>>>> remote\n", nil},
		{"indented is text", "  <<<<<<< local\n  =======\n  >>>>>>> remote\n", nil},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			h := newFakeHarness(t, HarnessConfig{})
			path := h.Path("notes")
			h.assertOK(t, h.Pull("", path))
			h.WriteFile(path+"/a.md", row.data)
			if row.want == nil {
				// Near matches and indented blocks are ordinary text:
				// the commit publishes.
				h.assertOK(t, h.Commit("", path, "publish"))
				if m := h.Manifest(); m.Generation != 1 {
					t.Fatalf("manifest generation = %d, want the accepted publication", m.Generation)
				}
				return
			}
			res := h.Commit("", path, "markers")
			h.assertEnvelope(t, ToolCall{
				Tool: toolCommit, Path: path, Message: "markers",
				Expect: CallExpectation{
					ErrorCode: "CONTENT_CONFLICT",
					Files:     []FileExpectation{{Path: "a.md", Ranges: row.want}},
				},
			}, res)
			// Rejection happens before any Git or S3 work: the commit's
			// put/create/replace counters stay zero.
			rec := h.Recorder()
			for _, op := range []Op{OpPut, OpCreate, OpReplace, OpDelete} {
				if got := rec.Count(op); got != 0 {
					t.Fatalf("marker rejection made %d %s calls, want none", got, op)
				}
			}
		})
	}
}

// TestScenarioConflictResolutionAndRepublish proves that resolving the
// markers and committing again publishes the resolution, and a fresh pull
// observes exactly the resolved bytes with no markers in R (architecture
// section 12, L763).
func TestScenarioConflictResolutionAndRepublish(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	c := newSharedHarness(t, h.Raw(), h.cfg.Prefix, HarnessConfig{})
	pathH, pathC := h.Path("notes"), c.Path("notes")

	commitFirst(t, h, pathH, "shared.md", "base\n", "c1")
	h.assertOK(t, h.Pull("", pathH))
	h.WriteFile(pathH+"/shared.md", "<<<<<<< local\nmine\n=======\nbase\n>>>>>>> remote\n")

	res := h.Commit("", pathH, "with markers")
	h.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: pathH, Message: "with markers",
		Expect: CallExpectation{
			ErrorCode: "CONTENT_CONFLICT",
			Files:     []FileExpectation{{Path: "shared.md", Ranges: []RangeExpectation{{Start: 1, End: 5}}}},
		},
	}, res)

	h.WriteFile(pathH+"/shared.md", "resolved\n")
	h.assertOK(t, h.Commit("", pathH, "resolved"))
	if m := h.Manifest(); m.Generation != 2 {
		t.Fatalf("manifest generation = %d, want the resolution published at 2", m.Generation)
	}

	// A fresh reader observes exactly the resolved bytes: asserting the
	// exact content is what rules marker bytes out of the accepted state,
	// since any marker block changes those bytes.
	c.assertOK(t, c.Pull("", pathC))
	assertVisibleFiles(t, c, pathC, map[string]string{"shared.md": "resolved\n"})
}

// TestScenarioConflictAfterRemoteMovement proves the second-merge retry:
// the remote moves between a conflict and its resolution, and the resolved
// commit merges again against the moved remote, accepting the resolution
// and the concurrent additions (architecture section 12, L763).
func TestScenarioConflictAfterRemoteMovement(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	b := newSharedHarness(t, h.Raw(), h.cfg.Prefix, HarnessConfig{})
	pathH, pathB := h.Path("notes"), b.Path("notes")

	commitFirst(t, h, pathH, "shared.md", "base\n", "c1")
	b.assertOK(t, b.Pull("", pathB))

	// A moves shared.md forward; B edits the same line and conflicts.
	h.WriteFile(pathH+"/shared.md", "A-v2\n")
	h.assertOK(t, h.Commit("", pathH, "A v2"))
	b.WriteFile(pathB+"/shared.md", "B-v2\n")
	res := b.Commit("", pathB, "B v2")
	b.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: pathB, Message: "B v2",
		Expect: CallExpectation{
			ErrorCode: "CONTENT_CONFLICT",
			Files:     []FileExpectation{{Path: "shared.md", Ranges: []RangeExpectation{{Start: 1, End: 5}}}},
		},
	}, res)

	// The remote moves again while B resolves.
	h.WriteFile(pathH+"/d.md", "D\n")
	h.assertOK(t, h.Commit("", pathH, "add d"))
	b.WriteFile(pathB+"/shared.md", "resolved\n")
	b.assertOK(t, b.Commit("", pathB, "resolved after movement"))

	got := b.FSSnapshot(pathB)
	if got["shared.md"] != "resolved\n" || got["d.md"] != "D\n" {
		t.Fatalf("B L after the second merge = %v, want the resolution plus the moved remote's d.md", got)
	}
	assertRemoteGeneration(t, b, pathB, 4)
}
