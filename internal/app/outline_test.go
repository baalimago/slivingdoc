package app

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/mcp"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/tui"
)

func TestStripWidthsSplitTheColumns(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name         string
		shares       []int
		cols         int
		largestFirst bool
	}{
		{"the largest entries get the widest segments", []int{658, 308, 282, 27, 8, 1}, 20, true},
		{"a dominating entry keeps the others visible", []int{1000, 1, 1, 1, 1, 1, 1, 1}, 20, true},
		{"a single entry fills the room", []int{1}, 20, true},
		{"more entries than columns drop the smallest", []int{1, 2, 3, 4, 5, 6}, 3, false},
		{"no room leaves every segment empty", []int{4, 4}, 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tops := make([]outlineRow, len(tt.shares))
			for i, s := range tt.shares {
				tops[i].files = s
			}
			widths := stripWidths(tops, tt.cols)
			sum := 0
			for _, w := range widths {
				if w < 0 || (tt.cols >= len(tt.shares) && w < 1) {
					t.Fatalf("widths = %v, want no negative segment and one column for every entry", widths)
				}
				sum += w
			}
			if sum != tt.cols {
				t.Fatalf("widths = %v, want them to fill %d columns", widths, tt.cols)
			}
			if tt.largestFirst && widths[0] < widths[len(widths)-1] {
				t.Fatalf("widths = %v, want the largest entry widest", widths)
			}
		})
	}
}

var escape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestWriteSuccessStyledOutlineFitsTenRowsOf150Columns(t *testing.T) {
	t.Parallel()
	var contents []git.FileLines
	for i := range 12 {
		for j := range 40 - 3*i {
			contents = append(contents, git.FileLines{
				Path: fmt.Sprintf("team-%02d/a-rather-long-note-name-%03d.md", i, j), Lines: 5,
			})
		}
	}
	var out bytes.Buffer
	writeSuccess(&out, mcp.MapSuccess(notebook.Result{Generation: 9, Contents: contents}, "/nb"), tui.New(tui.Styled, tui.TrueColor))
	lines := strings.Split(strings.TrimSuffix(escape.ReplaceAllString(out.String(), ""), "\n"), "\n")
	if len(lines) != 12 {
		t.Fatalf("styled report has %d lines, want 12:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	for _, line := range lines[1:] {
		if n := utf8.RuneCountInString(line); n > outlineWidth {
			t.Fatalf("line is %d columns, want at most %d: %q", n, outlineWidth, line)
		}
	}
	if strip := lines[1]; utf8.RuneCountInString(strip) != outlineWidth || !strings.HasSuffix(strip, "  282 files · 1410 lines") {
		t.Fatalf("strip line = %q, want a %d-column strip ending with the totals", strip, outlineWidth)
	}
	if lines[2] != "" || !strings.HasPrefix(lines[3], "    entry ") {
		t.Fatalf("lines after the strip = %q, %q, want a blank line and the column header", lines[2], lines[3])
	}
	if !strings.HasPrefix(lines[11], "  ■ +5 smaller") {
		t.Fatalf("last row = %q, want the folded row", lines[11])
	}
}

// TestWriteOutlineFitsTheWidthItIsCutFor: the report is cut for the terminal
// that prints it, so no line is wider than the width it was given and the
// styled strip fills it, down to a narrow terminal (architecture/running.md,
// The report).
func TestWriteOutlineFitsTheWidthItIsCutFor(t *testing.T) {
	t.Parallel()
	var contents []git.FileLines
	for i := range 12 {
		for j := range 40 - 3*i {
			contents = append(contents, git.FileLines{
				Path: fmt.Sprintf("team-%02d/a-rather-long-note-name-%03d.md", i, j), Lines: 5,
			})
		}
	}
	contents = append(contents, git.FileLines{Path: "a-top-level-name-that-outgrows-the-column/a.md", Lines: 1})
	outline := mcp.MapSuccess(notebook.Result{Generation: 9, Contents: contents}, "/nb").Outline
	for _, width := range []int{outlineMinWidth, 60, 80, outlineWidth} {
		for _, style := range []struct {
			name string
			s    tui.Style
		}{
			{"styled", tui.New(tui.Styled, tui.TrueColor)},
			{"plain", tui.New(tui.Plain, tui.Basic)},
		} {
			t.Run(fmt.Sprintf("%s/%d", style.name, width), func(t *testing.T) {
				t.Parallel()
				var b strings.Builder
				writeOutline(&b, outline, style.s, width)
				lines := strings.Split(strings.TrimSuffix(escape.ReplaceAllString(b.String(), ""), "\n"), "\n")
				for _, line := range lines {
					if n := utf8.RuneCountInString(line); n > width {
						t.Fatalf("a line is %d columns wide, want at most %d: %q", n, width, line)
					}
				}
				if style.s.Mode() != tui.Styled {
					return
				}
				if strip := lines[0]; utf8.RuneCountInString(strip) != width || !strings.Contains(strip, "█") {
					t.Fatalf("strip line = %q, want it to fill all %d columns", strip, width)
				}
			})
		}
	}
}
