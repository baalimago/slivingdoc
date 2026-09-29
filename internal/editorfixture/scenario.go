package editorfixture

import (
	"fmt"
	"strings"
)

// Step is one accepted commit: files written or removed in the visible
// directory, then a commit with Message.
type Step struct {
	Message string
	Write   map[string]string
	Remove  []string
}

// Scenario is a named sequence of commits and the checkpoint policy it runs
// under. Every step must change the notebook. A checkpoint compaction also
// takes a generation, so generations can skip step numbers.
type Scenario struct {
	Name string
	// CheckpointPacks is the active tail length that triggers a checkpoint;
	// zero selects the notebook default.
	CheckpointPacks int
	// RetainedCheckpoints is how many replaced checkpoint chains stay in
	// the manifest.
	RetainedCheckpoints int
	Steps               []Step
}

// Scenarios returns the committed fixture set: a root checkpoint, an
// increment chain, a checkpoint compaction with retained generations, and
// delta-compressed packs.
func Scenarios() []Scenario {
	return []Scenario{
		{
			Name: "root-checkpoint",
			Steps: []Step{{
				Message: "first notes",
				Write: map[string]string{
					"README.md":           "# Notes\n\nA root checkpoint.\n",
					"a-b.md":              "dash sorts before dot\n",
					"a.md":                "dot sorts before slash\n",
					"a/x.md":              "a directory named a\n",
					"docs/guide/intro.md": "nested directories\n",
					"docs/guide/zeta.md":  "last in its directory\n",
					"empty.md":            "",
					"crlf.md":             "one\r\ntwo\r\n",
					"unicode/café.md":     "café ☃ \U0001F4DD\n",
					"spaces in name.md":   "spaces are fine inside a name\n",
				},
			}},
		},
		{
			Name: "increments",
			Steps: []Step{
				{Message: "root", Write: map[string]string{"a.md": "alpha\n", "dir/b.md": "beta\n"}},
				{Message: "add and edit", Write: map[string]string{"a.md": "alpha\nedited\n", "dir/c.md": "gamma\n"}},
				{Message: "delete and edit", Remove: []string{"dir/b.md"}, Write: map[string]string{"dir/c.md": "gamma\nmore\n"}},
				{
					Message: "rename\n\nwith a body and unicode üñî",
					Remove:  []string{"a.md"},
					Write:   map[string]string{"renamed.md": "alpha\nedited\n"},
				},
			},
		},
		{
			Name:                "compaction",
			CheckpointPacks:     3,
			RetainedCheckpoints: 2,
			Steps:               compactionSteps(11),
		},
		{
			Name: "delta",
			Steps: []Step{
				{Message: "two similar large files", Write: map[string]string{
					"big/one.md": bigText(0, ""),
					"big/two.md": bigText(0, "second"),
					"small.md":   "tiny\n",
				}},
				{Message: "edit one", Write: map[string]string{"big/one.md": bigText(1, "")}},
				{Message: "edit both", Write: map[string]string{
					"big/one.md": bigText(2, ""),
					"big/two.md": bigText(2, "second"),
				}},
			},
		},
	}
}

func compactionSteps(n int) []Step {
	steps := make([]Step, 0, n)
	for i := 1; i <= n; i++ {
		steps = append(steps, Step{
			Message: fmt.Sprintf("commit %d", i),
			Write: map[string]string{
				"log.md":                    fmt.Sprintf("entries: %d\n", i),
				fmt.Sprintf("n/%02d.md", i): fmt.Sprintf("note %d\n", i),
			},
		})
	}
	return steps
}

// bigText builds a few hundred lines of prose. Variants with the same
// generation differ in a few lines, so libgit2's pack builder stores one as
// a delta against the other.
func bigText(revision int, variant string) string {
	var b strings.Builder
	for i := range 300 {
		fmt.Fprintf(&b, "line %03d of a long note with enough words to be worth compressing\n", i)
		if variant != "" && i%75 == 0 {
			fmt.Fprintf(&b, "%s variation %d\n", variant, i)
		}
		if revision > 0 && i%100 == 0 {
			fmt.Fprintf(&b, "revision %d touched here\n", revision)
		}
	}
	return b.String()
}
