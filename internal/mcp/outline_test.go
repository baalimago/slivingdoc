package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/git"
)

func files(paths ...string) []git.FileLines {
	out := make([]git.FileLines, len(paths))
	for i, p := range paths {
		out[i] = git.FileLines{Path: p, Lines: 10}
	}
	return out
}

func encode(t *testing.T, o *Outline) string {
	t.Helper()
	data, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestOutlinePromotesTheLargestHiddenDirectoryWhileEntriesRemain(t *testing.T) {
	t.Parallel()
	got := outline([]git.FileLines{
		{Path: "README.md", Lines: 35},
		{Path: "people/ana.md", Lines: 20},
		{Path: "people/bo.md", Lines: 18},
		{Path: "projects/alpha/README.md", Lines: 40},
		{Path: "projects/alpha/docs/api.md", Lines: 80},
		{Path: "projects/alpha/docs/design.md", Lines: 120},
		{Path: "projects/alpha/notes/a.md", Lines: 12},
		{Path: "projects/alpha/notes/b.md", Lines: 12},
		{Path: "projects/alpha/notes/c.md", Lines: 12},
		{Path: "projects/beta/plan.md", Lines: 60},
		{Path: "projects/beta/risks.md", Lines: 25},
	})
	want := `{"files":11,"lines":434,"entries":[` +
		`{"path":"projects/","files":8,"share":73},` +
		`{"path":"projects/alpha/","files":6,"share":55,"largest":[{"path":"projects/alpha/README.md","lines":40}]},` +
		`{"path":"projects/alpha/notes/","files":3,"share":27,"largest":[{"path":"projects/alpha/notes/a.md","lines":12},` +
		`{"path":"projects/alpha/notes/b.md","lines":12},{"path":"projects/alpha/notes/c.md","lines":12}]},` +
		`{"path":"projects/alpha/docs/","files":2,"share":18,"largest":[{"path":"projects/alpha/docs/design.md","lines":120},` +
		`{"path":"projects/alpha/docs/api.md","lines":80}]},` +
		`{"path":"projects/beta/","files":2,"share":18,"largest":[{"path":"projects/beta/plan.md","lines":60},` +
		`{"path":"projects/beta/risks.md","lines":25}]},` +
		`{"path":"people/","files":2,"share":18,"largest":[{"path":"people/ana.md","lines":20},{"path":"people/bo.md","lines":18}]},` +
		`{"path":"README.md","lines":35}]}`
	if text := encode(t, got); text != want {
		t.Fatalf("outline =\n%s\nwant\n%s", text, want)
	}
}

func TestOutlineFoldsTheSmallestTopLevelEntries(t *testing.T) {
	t.Parallel()
	var paths []string
	for i := range 12 {
		for j := range 12 - i {
			paths = append(paths, fmt.Sprintf("team-%02d/n%02d.md", i, j))
		}
	}
	got := outline(files(paths...))
	if len(got.Entries) != outlineEntries {
		t.Fatalf("entries = %d, want %d", len(got.Entries), outlineEntries)
	}
	last := encode(t, &Outline{Entries: got.Entries[outlineEntries-1:]})
	if want := `{"files":0,"lines":0,"entries":[{"folded":["team-07/","team-08/","team-09/","team-10/","team-11/"],"files":15,"share":19}]}`; last != want {
		t.Fatalf("folded entry = %s, want %s", last, want)
	}
	if first := got.Entries[0]; first.Path != "team-00/" || first.Files != 12 {
		t.Fatalf("first entry = %+v, want the largest", first)
	}
}

func TestOutlineListsAnEntrysLargestChildrenAndCountsTheRest(t *testing.T) {
	t.Parallel()
	var paths []string
	for i := range 300 {
		paths = append(paths, fmt.Sprintf("log/f%03d.md", i))
	}
	got := outline(files(paths...)).Entries[0]
	if len(got.Largest) != outlineLargest || got.More != 300-outlineLargest || !strings.HasSuffix(got.Largest[0].Path, "f000.md") {
		t.Fatalf("log entry lists %d with %d more, first %q; want %d largest and the rest counted",
			len(got.Largest), got.More, got.Largest[0].Path, outlineLargest)
	}
}
