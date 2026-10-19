package mcp

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/baalimago/slivingdoc/internal/git"
)

// The bounds of a materialization's outline (architecture/product-contract.md,
// Materialization): the entry budget, and how many of an entry's largest
// children it lists. Depth has no bound of its own; promotion spends the
// entry budget.
const (
	outlineEntries = 8
	outlineLargest = 6
)

// Outline is a materialization at a glance: the top-level entries, largest
// first, the smallest folded into one entry when they do not all fit, and
// the largest hidden subdirectories promoted into entries of their own while
// the budget lasts.
type Outline struct {
	Files   int            `json:"files"`
	Lines   int            `json:"lines"`
	Entries []OutlineEntry `json:"entries"`
}

// OutlineEntry is a directory, a file, or the folded smallest top-level
// entries. Share is the entry's files as a rounded percentage of the
// notebook's.
type OutlineEntry struct {
	Path    string        `json:"path,omitempty"`
	Folded  []string      `json:"folded,omitempty"`
	Files   int           `json:"files,omitempty"`
	Lines   *int          `json:"lines,omitempty"`
	Share   *int          `json:"share,omitempty"`
	Largest []OutlineItem `json:"largest,omitempty"`
	More    int           `json:"more,omitempty"`
}

// OutlineItem is one of an entry's largest children without an entry of
// its own: a directory with its files, or a file with its lines.
type OutlineItem struct {
	Path  string `json:"path"`
	Files int    `json:"files,omitempty"`
	Lines *int   `json:"lines,omitempty"`
}

func outline(files []git.FileLines) *Outline {
	root := &node{dir: true}
	for _, f := range files {
		root.insert(f.Path, f.Lines)
	}
	root.settle()
	top, folded := root.children, []*node(nil)
	if len(top) > outlineEntries {
		top, folded = top[:outlineEntries-1], top[outlineEntries-1:]
	}
	listed := slices.Clone(top)
	for _, n := range listed {
		n.listed = true
	}
	free := outlineEntries - len(listed)
	if len(folded) > 0 {
		free--
	}
	for free > 0 && promote(&listed) {
		free--
	}
	o := &Outline{Files: root.files, Lines: root.lines}
	for _, n := range listed {
		o.Entries = append(o.Entries, n.entry(root.files))
	}
	if len(folded) > 0 {
		e := OutlineEntry{}
		for _, n := range folded {
			e.Folded = append(e.Folded, n.path)
			e.Files += n.files
		}
		e.Share = share(e.Files, root.files)
		o.Entries = append(o.Entries, e)
	}
	return o
}

type node struct {
	path     string
	dir      bool
	files    int
	lines    int
	children []*node
	byName   map[string]*node
	listed   bool
}

func (n *node) insert(path string, lines int) {
	name, rest, nested := strings.Cut(path, "/")
	if !nested {
		n.children = append(n.children, &node{path: n.path + name, files: 1, lines: lines})
		return
	}
	child := n.byName[name]
	if child == nil {
		child = &node{path: n.path + name + "/", dir: true}
		if n.byName == nil {
			n.byName = map[string]*node{}
		}
		n.byName[name] = child
		n.children = append(n.children, child)
	}
	child.insert(rest, lines)
}

// settle totals every directory and sorts its entries: directories first,
// larger first (files a directory holds at any depth, lines a file has),
// then by path.
func (n *node) settle() {
	for _, c := range n.children {
		if c.dir {
			c.settle()
		}
		n.files += c.files
		n.lines += c.lines
	}
	slices.SortFunc(n.children, func(a, b *node) int {
		if a.dir != b.dir {
			if a.dir {
				return -1
			}
			return 1
		}
		if c := cmp.Compare(b.size(), a.size()); c != 0 {
			return c
		}
		return strings.Compare(a.path, b.path)
	})
}

func (n *node) size() int {
	if n.dir {
		return n.files
	}
	return n.lines
}

// hiddenDir is n's largest subdirectory without an entry, if it holds more
// than one file; a single-file directory adds nothing its parent's largest
// list does not.
func (n *node) hiddenDir() *node {
	for _, c := range n.children {
		if !c.dir || c.files < 2 {
			return nil
		}
		if !c.listed {
			return c
		}
	}
	return nil
}

// promote gives the largest hidden subdirectory below any listed entry an
// entry of its own, after the entries already under its parent.
func promote(listed *[]*node) bool {
	at, best := -1, (*node)(nil)
	for i, n := range *listed {
		if c := n.hiddenDir(); c != nil && (best == nil || c.files > best.files) {
			at, best = i, c
		}
	}
	if best == nil {
		return false
	}
	best.listed = true
	parent := (*listed)[at].path
	end := at + 1
	for end < len(*listed) && strings.HasPrefix((*listed)[end].path, parent) {
		end++
	}
	*listed = slices.Insert(*listed, end, best)
	return true
}

func (n *node) entry(total int) OutlineEntry {
	if !n.dir {
		return OutlineEntry{Path: n.path, Lines: &n.lines}
	}
	e := OutlineEntry{Path: n.path, Files: n.files, Share: share(n.files, total)}
	for _, c := range n.children {
		switch {
		case c.listed:
		case len(e.Largest) == outlineLargest:
			e.More++
		case c.dir:
			e.Largest = append(e.Largest, OutlineItem{Path: c.path, Files: c.files})
		default:
			e.Largest = append(e.Largest, OutlineItem{Path: c.path, Lines: &c.lines})
		}
	}
	return e
}

func share(part, whole int) *int {
	p := (200*part + whole) / (2 * whole)
	return &p
}

func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
