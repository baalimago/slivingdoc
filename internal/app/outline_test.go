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

func TestStripWidthsFillTheColumnsWithEveryEntryVisible(t *testing.T) {
	t.Parallel()
	for _, shares := range [][]int{
		{658, 308, 282, 27, 8, 1},
		{1000, 1, 1, 1, 1, 1, 1, 1},
		{1},
	} {
		tops := make([]outlineRow, len(shares))
		for i, s := range shares {
			tops[i].files = s
		}
		widths := stripWidths(tops, 20)
		sum := 0
		for i, w := range widths {
			if w < 1 {
				t.Fatalf("shares %v: segment %d is %d columns wide, want at least 1", shares, i, w)
			}
			sum += w
		}
		if sum != 20 || widths[0] < widths[len(widths)-1] {
			t.Fatalf("shares %v: widths = %v, want 20 columns with the largest entry widest", shares, widths)
		}
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
