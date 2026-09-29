package tui

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/baalimago/go_away_boilerplate/pkg/table"
)

// ErrSkipped reports that the person left a picker without choosing:
// they quit, went back, interrupted it, or the input ended.
var ErrSkipped = errors.New("tui: nothing was picked")

// Picker is one list to choose from, drawn by the go_away_boilerplate
// table: a numbered row per item, typed selection, paging and a filter.
type Picker struct {
	// Title is the question above the list.
	Title string
	// Header names the columns of Rows.
	Header []string
	Rows   [][]Cell
	// Many allows several rows ("0,2", or the range "0:2"); otherwise one.
	Many bool
	// In is where answers are read, one line each; nil reads the
	// controlling terminal and clears the list once it is answered.
	In io.Reader
	// Out is where the list and its prompt are drawn.
	Out io.Writer
}

// Pick draws the picker and returns the chosen row indices, or
// ErrSkipped.
func (s Style) Pick(p Picker) ([]int, error) {
	if len(p.Rows) == 0 {
		return nil, fmt.Errorf("tui: %w: the list is empty", ErrSkipped)
	}
	if len(p.Header) == 0 {
		return nil, errors.New("tui: a picker needs a header")
	}
	// The rows go to the table unpainted: its /text filter matches the
	// formatted row, and escape codes would match digits and letters.
	// The table's theme colours the header and the prompt instead.
	lines := strings.Split(strings.TrimSuffix(Style{}.Columns("", nil, unpainted(headerCells(p.Header), p.Rows)), "\n"), "\n")
	header, body := lines[0], lines[1:]
	// The table numbers rows by their position in the current, possibly
	// filtered, list and selects by that number, so the row formatter
	// prints the number the table hands it.
	width := len(strconv.Itoa(len(body) - 1))
	header = strings.Repeat(" ", 2+width+2) + header
	if _, err := fmt.Fprintf(p.Out, "%s%s\n", s.Mark(Brand), s.Bold(p.Title)); err != nil {
		return nil, fmt.Errorf("tui: draw the picker: %w", err)
	}
	for {
		t := table.New(table.SlicePaginator(body), func(i int, line string) (string, error) {
			return fmt.Sprintf("  %*d  %s", width, i, line), nil
		}).
			WithHeader(header).
			WithTheme(table.Theme{Primary: s.rawDim(), Secondary: s.rawBrand(), Items: 10}).
			WithBackLabel("[b]ack").
			WithWriter(p.Out)
		if p.In != nil {
			t = t.WithInput(p.In)
		}
		picked, _, err := t.Run()
		switch {
		case errors.Is(err, table.ErrUserInitiatedExit), errors.Is(err, table.ErrBack), errors.Is(err, io.EOF):
			return nil, ErrSkipped
		case err != nil:
			return nil, fmt.Errorf("tui: pick: %w", err)
		}
		picked = uniq(picked)
		notice := ""
		// The table accepts the number one past its last row, so every
		// pick is checked before a caller indexes with it; an answer
		// outside the list, or several rows where one is asked for,
		// asks again.
		if i := slices.IndexFunc(picked, func(n int) bool { return n < 0 || n >= len(p.Rows) }); i >= 0 {
			notice = fmt.Sprintf("There is no row %d; pick again.", picked[i])
		} else if !p.Many && len(picked) != 1 {
			notice = "Pick one row."
		} else {
			return picked, nil
		}
		if _, err := fmt.Fprintf(p.Out, "%s%s\n", s.Mark(Caution), notice); err != nil {
			return nil, fmt.Errorf("tui: draw the picker: %w", err)
		}
	}
}

func headerCells(header []string) []Cell {
	cells := make([]Cell, len(header))
	for i, h := range header {
		cells[i] = Cell{Text: h}
	}
	return cells
}

// unpainted is header and rows with every Paint dropped: a caller's
// Paint closes over its own Style, so a plain Columns would still paint.
func unpainted(header []Cell, rows [][]Cell) [][]Cell {
	out := make([][]Cell, 0, len(rows)+1)
	out = append(out, header)
	for _, row := range rows {
		cells := make([]Cell, len(row))
		for i, c := range row {
			cells[i] = Cell{Text: c.Text}
		}
		out = append(out, cells)
	}
	return out
}

// uniq drops repeated rows, keeping the first of each in order.
func uniq(picked []int) []int {
	out := make([]int, 0, len(picked))
	for _, n := range picked {
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

func (s Style) rawBrand() string {
	if s.mode != Styled {
		return ""
	}
	if s.depth == TrueColor {
		return hueBrand.true
	}
	return hueBrand.basic
}

func (s Style) rawDim() string {
	if s.mode != Styled {
		return ""
	}
	return codeDim
}
