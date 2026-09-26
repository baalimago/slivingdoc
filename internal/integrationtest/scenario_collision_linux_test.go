//go:build linux

package integrationtest

import (
	"testing"

	"golang.org/x/text/unicode/norm"
)

// TestScenarioPathCollisionsAreInvalidContent proves that two visible
// entries mapping to one notebook path are refused as the caller's content,
// naming the paths, before any store mutation (architecture/workspace.md):
// names equal under Unicode case folding, and an NFD and an NFC spelling of
// one name. Linux file systems keep both spellings apart, which is the
// capability the build tag names.
func TestScenarioPathCollisionsAreInvalidContent(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name  string
		files []string
		want  []FileExpectation
	}{
		{
			name:  "case folding",
			files: []string{"Notes.md", "notes.md"},
			want: []FileExpectation{
				{Path: "Notes.md", Reason: "INVALID_CONTENT", Ranges: []RangeExpectation{}},
				{Path: "notes.md", Reason: "INVALID_CONTENT", Ranges: []RangeExpectation{}},
			},
		},
		{
			name:  "NFC duplicate",
			files: []string{norm.NFD.String("café.md"), norm.NFC.String("café.md")},
			want:  []FileExpectation{{Path: norm.NFC.String("café.md"), Reason: "INVALID_CONTENT", Ranges: []RangeExpectation{}}},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			h := newFakeHarness(t, HarnessConfig{})
			path := h.Path("notes")
			h.assertOK(t, h.Pull("", path))
			before := h.Recorder().Snapshot()

			for _, name := range row.files {
				h.WriteFile(path+"/"+name, "text\n")
			}
			res := h.Commit("", path, "publish colliding names")
			h.assertEnvelope(t, ToolCall{
				Tool: toolCommit, Path: path, Message: "publish colliding names",
				Expect: CallExpectation{
					ErrorCode: "INVALID_REQUEST", Retryable: new(false),
					Reason: "INVALID_CONTENT", Action: "EDIT_FILES",
					Files: row.want,
				},
			}, res)

			after := h.Recorder().Snapshot()
			for _, op := range []Op{OpPut, OpCreate, OpReplace, OpDelete} {
				if after[op] != before[op] {
					t.Fatalf("colliding names mutated the store: %s %d -> %d", op, before[op], after[op])
				}
			}
		})
	}
}
