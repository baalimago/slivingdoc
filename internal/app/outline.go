package app

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/baalimago/slivingdoc/internal/mcp"
	"github.com/baalimago/slivingdoc/internal/tui"
)

// outlineWidth is the terminal width a materialization's report is cut for.
const outlineWidth = 150

// outlineRow is one entry of an outline as a report line.
type outlineRow struct {
	name   string
	count  string
	peek   string
	group  int
	top    bool
	folded bool
	files  int
}

// writeOutline renders a materialization (architecture/product-contract.md,
// CLI report). On a terminal a strip splits the width over the top-level
// entries by their files, a blank line and the column header follow, and
// a box of each entry's colour leads its row; the plain form keeps the
// totals, the header and the rows and drops the strip, its blank line and
// the boxes, as it drops every other mark.
func writeOutline(b *strings.Builder, o *mcp.Outline, p tui.Style) {
	rows, nameWidth := outlineRows(o)
	totals := quantity(o.Files, "file", "files") + " · " + quantity(o.Lines, "line", "lines")
	header := fmt.Sprintf("%s%6s %4s %s", pad("entry", nameWidth), "files", "%", "largest inside, without a row of its own")
	line := func(r outlineRow) string {
		return strings.TrimRight(pad(r.name, nameWidth)+r.count+" "+r.peek, " ")
	}
	if p.Mode() != tui.Styled {
		fmt.Fprintf(b, "  %s\n  %s\n", totals, header)
		for _, r := range rows {
			fmt.Fprintf(b, "  %s\n", line(r))
		}
		return
	}
	var strip strings.Builder
	var tops []outlineRow
	for _, r := range rows {
		if r.top || r.folded {
			tops = append(tops, r)
		}
	}
	for i, w := range stripWidths(tops, outlineWidth-4-utf8.RuneCountInString(totals)) {
		strip.WriteString(paint(p, tops[i], strings.Repeat("█", w)))
	}
	fmt.Fprintf(b, "  %s  %s\n\n    %s\n", strip.String(), p.Dim(totals), p.Dim(header))
	for _, r := range rows {
		name := paint(p, r, pad(r.name, nameWidth))
		if r.top {
			name = p.Bold(name)
		}
		fmt.Fprintf(b, "%s\n", strings.TrimRight(fmt.Sprintf("  %s %s%s %s", paint(p, r, "■"), name, r.count, p.Dim(r.peek)), " "))
	}
}

// outlineRows turns the outline's entries into report lines: an entry
// below the top level is indented two spaces per level under its parent,
// and its group is the top-level entry it belongs to.
func outlineRows(o *mcp.Outline) ([]outlineRow, int) {
	rows := make([]outlineRow, 0, len(o.Entries))
	nameWidth, group := 14, -1
	for _, e := range o.Entries {
		var r outlineRow
		if e.Folded != nil {
			r = outlineRow{name: fmt.Sprintf("+%d smaller", len(e.Folded)), folded: true, files: e.Files}
		} else {
			depth := strings.Count(strings.TrimSuffix(e.Path, "/"), "/")
			if depth == 0 {
				group++
				r.top, r.files = true, max(e.Files, 1)
			}
			r.name, r.group = strings.Repeat("  ", depth)+base(e.Path), group
		}
		nameWidth = max(nameWidth, utf8.RuneCountInString(r.name))
		rows = append(rows, r)
	}
	nameWidth += 2
	room := outlineWidth - 4 - nameWidth - 6 - 1 - 4 - 1
	for i, e := range o.Entries {
		switch {
		case e.Lines != nil:
			rows[i].count = fmt.Sprintf("%6d %s", *e.Lines, unit(*e.Lines, "line", "lines"))
		case e.Folded != nil:
			rows[i].count = fmt.Sprintf("%6d %4s", e.Files, percent(*e.Share))
			rows[i].peek = fit(e.Folded, ", ", room, 0)
		default:
			items := make([]string, len(e.Largest))
			for j, item := range e.Largest {
				items[j] = base(item.Path)
				if item.Lines == nil {
					items[j] += fmt.Sprintf(" %d", item.Files)
				}
			}
			rows[i].count = fmt.Sprintf("%6d %4s", e.Files, percent(*e.Share))
			rows[i].peek = fit(items, " · ", room, e.More)
		}
	}
	return rows, nameWidth
}

// base is the last element of a notebook path, a directory keeping its slash.
func base(path string) string {
	return path[strings.LastIndex(strings.TrimSuffix(path, "/"), "/")+1:]
}

// percent renders a share; one that rounds to nothing reads <1%.
func percent(share int) string {
	if share == 0 {
		return "<1%"
	}
	return fmt.Sprintf("%d%%", share)
}

// fit joins items with sep while they fit in room; the rest, and extra
// more beyond them, become "+N more".
func fit(items []string, sep string, room, extra int) string {
	used, sepWidth := 0, utf8.RuneCountInString(sep)
	for i, item := range items {
		need := utf8.RuneCountInString(item)
		if rest := len(items) - i - 1 + extra; rest > 0 {
			need += sepWidth + len(fmt.Sprintf("+%d more", rest))
		}
		if used+need > room {
			return strings.Join(append(items[:i:i], fmt.Sprintf("+%d more", len(items)-i+extra)), sep)
		}
		used += utf8.RuneCountInString(item) + sepWidth
	}
	if extra > 0 {
		items = append(items[:len(items):len(items)], fmt.Sprintf("+%d more", extra))
	}
	return strings.Join(items, sep)
}

// paint gives a row's text its group's colour, or the dim grey of the
// folded row.
func paint(p tui.Style, r outlineRow, t string) string {
	if r.folded {
		return p.Dim(t)
	}
	return p.Segment(r.group, t)
}

// stripWidths splits cols over the top-level rows by their files, each at
// least one column wide; rounding is settled on the largest entries.
func stripWidths(tops []outlineRow, cols int) []int {
	total := 0
	for _, r := range tops {
		total += r.files
	}
	widths := make([]int, len(tops))
	used := 0
	for i, r := range tops {
		widths[i] = max(1, r.files*cols/total)
		used += widths[i]
	}
	for i := 0; used < cols; i = (i + 1) % len(widths) {
		widths[i]++
		used++
	}
	for used > cols {
		widest := 0
		for i, w := range widths {
			if w > widths[widest] {
				widest = i
			}
		}
		widths[widest]--
		used--
	}
	return widths
}

func pad(s string, width int) string {
	return s + strings.Repeat(" ", max(0, width-utf8.RuneCountInString(s)))
}

func quantity(n int, one, many string) string {
	return fmt.Sprintf("%d %s", n, unit(n, one, many))
}

func unit(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
