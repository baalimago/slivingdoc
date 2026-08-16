package git

import (
	"bytes"
	"sort"
)

// FileStat is the per-file line-change summary of a snapshot diff: the
// normalized internal path and its insertion and deletion counts.
type FileStat struct {
	Path       string
	Insertions int
	Deletions  int
}

// DiffStat is the complete line-change summary between two snapshots: one
// FileStat per path whose line content changed, sorted by path, and the
// totals across all files. A path whose comparison lines are unchanged —
// including byte-different content that splits to the same lines, such as a
// CRLF-to-LF rewrite — contributes no entry.
type DiffStat struct {
	Files      []FileStat
	Insertions int
	Deletions  int
}

// DiffSnapshots compares two notebook snapshots line by line and returns
// the per-file insertion and deletion counts of a deterministic shortest
// edit script.
//
// base and cur are compared by normalized relative path. A path present
// only in cur counts every line as an insertion; a path present only in
// base counts every line as a deletion; a byte-identical path is omitted; a
// path with different bytes is diffed line by line. Files is sorted by
// path, and Insertions and Deletions are the sums across Files.
//
// Line counting follows the notebook line rule: a line is an LF-terminated
// run of bytes with one trailing CR stripped for comparison and counting, a
// final run without a trailing LF counts as one line, content ending in LF
// has no phantom empty final line, and empty content has zero lines.
//
// The function is pure: it never validates content or paths (snapshots are
// validated upstream) and never touches a repository or the filesystem.
func DiffSnapshots(base, cur Snapshot) DiffStat {
	baseData := make(map[string][]byte, len(base.Files))
	for _, f := range base.Files {
		baseData[f.Path] = f.Data
	}
	curData := make(map[string][]byte, len(cur.Files))
	for _, f := range cur.Files {
		curData[f.Path] = f.Data
	}

	stats := make([]FileStat, 0, max(len(base.Files), len(cur.Files)))
	var insertions, deletions int
	add := func(path string, ins, del int) {
		if ins == 0 && del == 0 {
			return
		}
		stats = append(stats, FileStat{Path: path, Insertions: ins, Deletions: del})
		insertions += ins
		deletions += del
	}
	for path, data := range curData {
		old, ok := baseData[path]
		switch {
		case !ok:
			add(path, countLines(data), 0)
		case !bytes.Equal(old, data):
			ins, del := diffLines(old, data)
			add(path, ins, del)
		}
	}
	for path, data := range baseData {
		if _, ok := curData[path]; !ok {
			add(path, 0, countLines(data))
		}
	}
	sort.Slice(stats, func(i, j int) bool { return stats[i].Path < stats[j].Path })
	return DiffStat{Files: stats, Insertions: insertions, Deletions: deletions}
}

// countLines counts the comparison lines of content without materializing
// them: one per LF terminator, plus the final run when content does not end
// in LF. Empty content has zero lines.
func countLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	lines := bytes.Count(data, []byte{'\n'})
	if data[len(data)-1] != '\n' {
		lines++
	}
	return lines
}

// splitLines splits content into comparison lines: an LF-terminated run of
// bytes with one trailing CR stripped, or the final run when content does
// not end in LF. Content ending in LF has no phantom empty final line, and
// empty content has zero lines.
func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	lines := make([]string, 0, countLines(data))
	start := 0
	for i, b := range data {
		if b != '\n' {
			continue
		}
		lines = append(lines, stripCR(data[start:i]))
		start = i + 1
	}
	if start < len(data) {
		lines = append(lines, stripCR(data[start:]))
	}
	return lines
}

// stripCR removes a single trailing carriage return from a comparison line.
func stripCR(line []byte) string {
	if n := len(line); n > 0 && line[n-1] == '\r' {
		return string(line[:n-1])
	}
	return string(line)
}

// diffLines returns the insertion and deletion counts of a deterministic
// shortest edit script between the comparison lines of old and new content.
func diffLines(old, new []byte) (insertions, deletions int) {
	return countDiff(splitLines(old), splitLines(new))
}

// countDiff counts the insertions and deletions in a shortest edit script
// between a and b using the linear-space Myers algorithm (the middle-snake
// refinement), so time stays O((n+m)d) and memory stays O(n+m) even for
// pathological inputs.
func countDiff(a, b []string) (insertions, deletions int) {
	return countRange(a, b, 0, len(a), 0, len(b))
}

// countRange counts the diff of a[alo:ahi] versus b[blo:bhi]. It trims the
// matching prefix and suffix, then splits the remainder at the middle snake
// of a shortest edit script and recurses on both halves.
func countRange(a, b []string, alo, ahi, blo, bhi int) (insertions, deletions int) {
	for alo < ahi && blo < bhi && a[alo] == b[blo] {
		alo++
		blo++
	}
	for alo < ahi && blo < bhi && a[ahi-1] == b[bhi-1] {
		ahi--
		bhi--
	}
	switch {
	case alo == ahi:
		return bhi - blo, 0 // every remaining new line is an insertion
	case blo == bhi:
		return 0, ahi - alo // every remaining old line is a deletion
	}
	x, y, u, v := middleSnake(a[alo:ahi], b[blo:bhi])
	x += alo
	y += blo
	u += alo
	v += blo
	ins1, del1 := countRange(a, b, alo, x, blo, y)
	ins2, del2 := countRange(a, b, u, ahi, v, bhi)
	return ins1 + ins2, del1 + del2
}

// middleSnake finds the middle snake of a shortest edit script between the
// non-empty, prefix- and suffix-trimmed sequences a and b. It runs Myers'
// forward and backward D-path searches until the paths overlap; the
// overlapping diagonal run is the middle snake, returned as its start
// (x, y) and end (u, v).
//
// At equal edit cost the search prefers the deletion move over the
// insertion move (a right move in the edit graph), so repeated calls are
// deterministic. The counts themselves are uniquely determined by the edit
// distance; the tie-break fixes the script shape. The overlap must exist
// for two different sequences, so a missing snake is a programming error.
func middleSnake(a, b []string) (x, y, u, v int) {
	n, m := len(a), len(b)
	delta := n - m
	odd := delta%2 != 0
	maxD := (n + m + 1) / 2
	offset := maxD + 1
	// vf[k] and vb[k] hold the furthest-reaching endpoints of the forward
	// and backward D-paths on diagonal k; the extra offset leaves room for
	// the sentinel diagonal 1 used before the first step.
	vf := make([]int, 2*maxD+3)
	vb := make([]int, 2*maxD+3)
	vf[offset+1] = 0
	vb[offset+1] = 0
	for d := 0; d <= maxD; d++ {
		// Forward D-paths from (0, 0).
		for k := -d; k <= d; k += 2 {
			var sx int
			if k == -d || (k != d && vf[offset+k-1] < vf[offset+k+1]) {
				sx = vf[offset+k+1]
			} else {
				sx = vf[offset+k-1] + 1
			}
			sy := sx - k
			x, y := sx, sy
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			vf[offset+k] = x
			if odd && k >= delta-(d-1) && k <= delta+(d-1) && vf[offset+k]+vb[offset+delta-k] >= n {
				return sx, sy, x, y
			}
		}
		// Backward D-paths from (n, m), measured as distances from the end.
		for k := -d; k <= d; k += 2 {
			var sx int
			if k == -d || (k != d && vb[offset+k-1] < vb[offset+k+1]) {
				sx = vb[offset+k+1]
			} else {
				sx = vb[offset+k-1] + 1
			}
			sy := sx - k
			x, y := sx, sy
			for x < n && y < m && a[n-1-x] == b[m-1-y] {
				x++
				y++
			}
			vb[offset+k] = x
			if !odd && k >= delta-d && k <= delta+d && vb[offset+k]+vf[offset+delta-k] >= n {
				return n - x, m - y, n - sx, m - sy
			}
		}
	}
	panic("git: middle snake not found")
}
