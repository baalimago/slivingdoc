package tui

import (
	"strings"
	"unicode/utf8"
)

// Cell is one table cell: its text and how to paint it. Paint is applied
// after the padding is measured, so escape sequences never skew the
// alignment; a nil Paint leaves the text as it is.
type Cell struct {
	Text  string
	Paint func(string) string
}

// Columns renders rows as left-aligned columns separated by two spaces,
// each line prefixed with indent. A header row, when given, is dimmed.
// Trailing empty cells are left out and the last cell is not padded, so
// no line ends in spaces.
func (s Style) Columns(indent string, header []string, rows [][]Cell) string {
	all := make([][]Cell, 0, len(rows)+1)
	if len(header) > 0 {
		cells := make([]Cell, len(header))
		for i, h := range header {
			cells[i] = Cell{Text: h, Paint: s.Dim}
		}
		all = append(all, cells)
	}
	all = append(all, rows...)
	widths := columnWidths(all)
	var b strings.Builder
	for _, row := range all {
		b.WriteString(indent)
		last := len(row) - 1
		for last > 0 && row[last].Text == "" {
			last--
		}
		for i, c := range row[:last+1] {
			text := c.Text
			if c.Paint != nil {
				text = c.Paint(text)
			}
			b.WriteString(text)
			if i < last {
				b.WriteString(strings.Repeat(" ", widths[i]-utf8.RuneCountInString(c.Text)+2))
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func columnWidths(rows [][]Cell) []int {
	var widths []int
	for _, row := range rows {
		for i, c := range row {
			if i >= len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], utf8.RuneCountInString(c.Text))
		}
	}
	return widths
}
