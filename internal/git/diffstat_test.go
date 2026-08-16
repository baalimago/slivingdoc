package git

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

func TestDiffStatIdenticalSnapshots(t *testing.T) {
	base := fakeSnapshot(map[string]string{
		"a.md":   "one\ntwo\n",
		"b/deep": "x",
		"empty":  "",
	})
	got := DiffSnapshots(base, base)
	if len(got.Files) != 0 || got.Insertions != 0 || got.Deletions != 0 {
		t.Fatalf("DiffSnapshots(identical) = %+v, want empty", got)
	}
}

func TestDiffStatAddedFiles(t *testing.T) {
	base := fakeSnapshot(map[string]string{"keep.md": "kept\n"})
	cur := fakeSnapshot(map[string]string{
		"keep.md":  "kept\n",
		"notes/a":  "one\ntwo\nthree", // no trailing LF: three lines
		"notes/cr": "x\r\ny\r\n",      // CRLF: two lines
	})
	got := DiffSnapshots(base, cur)
	want := DiffStat{
		Files: []FileStat{
			{Path: "notes/a", Insertions: 3},
			{Path: "notes/cr", Insertions: 2},
		},
		Insertions: 5,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DiffSnapshots(added) = %+v, want %+v", got, want)
	}
}

func TestDiffStatDeletedFiles(t *testing.T) {
	base := fakeSnapshot(map[string]string{
		"keep.md": "kept\n",
		"old/a":   "one\ntwo\n",
		"old/b":   "lone", // no trailing LF: one line
	})
	cur := fakeSnapshot(map[string]string{"keep.md": "kept\n"})
	got := DiffSnapshots(base, cur)
	want := DiffStat{
		Files: []FileStat{
			{Path: "old/a", Deletions: 2},
			{Path: "old/b", Deletions: 1},
		},
		Deletions: 3,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DiffSnapshots(deleted) = %+v, want %+v", got, want)
	}
}

func TestDiffStatModifiedFile(t *testing.T) {
	base := fakeSnapshot(map[string]string{"notes/today.md": "alpha\nbeta\ngamma\nzeta\ndelta\nomega\n"})
	cur := fakeSnapshot(map[string]string{"notes/today.md": "alpha\nbeta\nGAMMA\nDELTA\nzeta\ndelta\nomega\n"})
	got := DiffSnapshots(base, cur)
	want := DiffStat{
		Files: []FileStat{
			{Path: "notes/today.md", Insertions: 2, Deletions: 1},
		},
		Insertions: 2,
		Deletions:  1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DiffSnapshots(modified) = %+v, want %+v", got, want)
	}
}

func TestDiffStatCRLFAndLFCompareEqual(t *testing.T) {
	// The only change between the two versions is the line terminator; the
	// comparison lines are identical, so the file carries no change.
	base := fakeSnapshot(map[string]string{"notes/crlf.md": "a\r\nb\r\n"})
	cur := fakeSnapshot(map[string]string{"notes/crlf.md": "a\nb\n"})
	got := DiffSnapshots(base, cur)
	if len(got.Files) != 0 || got.Insertions != 0 || got.Deletions != 0 {
		t.Fatalf("DiffSnapshots(CRLF->LF) = %+v, want empty", got)
	}

	// A CRLF file gains one LF line: exactly one insertion.
	base = fakeSnapshot(map[string]string{"notes/crlf.md": "a\r\nb\r\n"})
	cur = fakeSnapshot(map[string]string{"notes/crlf.md": "a\nb\nc\n"})
	got = DiffSnapshots(base, cur)
	want := DiffStat{
		Files:      []FileStat{{Path: "notes/crlf.md", Insertions: 1}},
		Insertions: 1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DiffSnapshots(CRLF->LF+c) = %+v, want %+v", got, want)
	}
}

func TestDiffStatNoTrailingNewline(t *testing.T) {
	base := fakeSnapshot(map[string]string{"notes/eof.md": "a\nb"})
	cur := fakeSnapshot(map[string]string{"notes/eof.md": "a\nb\nc"})
	got := DiffSnapshots(base, cur)
	want := DiffStat{
		Files:      []FileStat{{Path: "notes/eof.md", Insertions: 1}},
		Insertions: 1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DiffSnapshots(eof) = %+v, want %+v", got, want)
	}
}

func TestDiffStatEmptyFiles(t *testing.T) {
	// An empty file added, an empty file deleted, and an empty file kept:
	// none of them carries a line change.
	base := fakeSnapshot(map[string]string{
		"kept-empty":   "",
		"deleted-file": "",
	})
	cur := fakeSnapshot(map[string]string{
		"kept-empty": "",
		"added-file": "",
	})
	got := DiffSnapshots(base, cur)
	if len(got.Files) != 0 || got.Insertions != 0 || got.Deletions != 0 {
		t.Fatalf("DiffSnapshots(empty files) = %+v, want empty", got)
	}

	// An empty file replaced by one empty line is one insertion: empty
	// content has zero lines, a lone LF is one empty line.
	got = DiffSnapshots(
		fakeSnapshot(map[string]string{"f": ""}),
		fakeSnapshot(map[string]string{"f": "\n"}),
	)
	want := DiffStat{Files: []FileStat{{Path: "f", Insertions: 1}}, Insertions: 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DiffSnapshots(empty->LF) = %+v, want %+v", got, want)
	}
}

func TestDiffStatSortedAndTotals(t *testing.T) {
	base := fakeSnapshot(map[string]string{
		"zeta.md":  "z\n",
		"a.md":     "a\n",
		"mid/deep": "m\nn\no\n",
	})
	cur := fakeSnapshot(map[string]string{
		"zeta.md":  "z\nchanged\n",
		"a.md":     "a\n",
		"mid/deep": "m\nn\no\np\n",
		"added.md": "x\ny\n",
	})
	got := DiffSnapshots(base, cur)
	for i, f := range got.Files {
		if i > 0 && got.Files[i-1].Path >= f.Path {
			t.Fatalf("Files not sorted: %+v", got.Files)
		}
	}
	var ins, del int
	for _, f := range got.Files {
		ins += f.Insertions
		del += f.Deletions
	}
	if got.Insertions != ins || got.Deletions != del {
		t.Fatalf("totals %d/+%d don't match per-file sums %d/+%d", got.Insertions, got.Deletions, ins, del)
	}
	want := DiffStat{
		Files: []FileStat{
			{Path: "added.md", Insertions: 2},
			{Path: "mid/deep", Insertions: 1},
			{Path: "zeta.md", Insertions: 1},
		},
		Insertions: 4,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DiffSnapshots(sorted) = %+v, want %+v", got, want)
	}
}

func TestDiffStatDeterministic(t *testing.T) {
	base := fakeSnapshot(map[string]string{"a.md": "one\ntwo\nthree\n"})
	cur := fakeSnapshot(map[string]string{"a.md": "one\nTWO\nthree\nfour\n"})
	first := DiffSnapshots(base, cur)
	for i := 0; i < 5; i++ {
		if got := DiffSnapshots(base, cur); !reflect.DeepEqual(got, first) {
			t.Fatalf("DiffSnapshots not deterministic: %+v != %+v", got, first)
		}
	}
}

func TestDiffStatInputOrderIndependent(t *testing.T) {
	// DiffSnapshots must not rely on the snapshot contract's path sorting.
	base := fakeSnapshot(map[string]string{"a.md": "a\n", "b.md": "b\n", "c.md": "c\n"})
	cur := fakeSnapshot(map[string]string{"a.md": "A\n", "b.md": "b\n", "d.md": "d\n"})
	shuffledBase := Snapshot{Files: []File{{Path: "c.md", Data: []byte("c\n")}, {Path: "a.md", Data: []byte("a\n")}, {Path: "b.md", Data: []byte("b\n")}}}
	shuffledCur := Snapshot{Files: []File{{Path: "d.md", Data: []byte("d\n")}, {Path: "b.md", Data: []byte("b\n")}, {Path: "a.md", Data: []byte("A\n")}}}
	if got, want := DiffSnapshots(shuffledBase, shuffledCur), DiffSnapshots(base, cur); !reflect.DeepEqual(got, want) {
		t.Fatalf("DiffSnapshots input-order dependent: %+v != %+v", got, want)
	}
}

// TestDiffLinesMatchesLCS proves the Myers counts against an independent
// longest-common-subsequence computation: every shortest edit script of
// a -> b has exactly len(b)-L insertions and len(a)-L deletions, so a wrong
// edit distance is caught exactly.
func TestDiffLinesMatchesLCS(t *testing.T) {
	exhaustive := func(alphabet []string, maxLen int) {
		seqs := allSequences(alphabet, maxLen)
		for _, a := range seqs {
			for _, b := range seqs {
				checkDiffLines(t, a, b)
			}
		}
	}
	exhaustive([]string{"a", "b"}, 6)

	rng := rand.New(rand.NewSource(1))
	alphabet := []string{"a", "b", "c", "d"}
	for i := 0; i < 2000; i++ {
		a := randomLines(rng, alphabet, 0, 30)
		b := randomLines(rng, alphabet, 0, 30)
		checkDiffLines(t, a, b)
	}
	// Large random inputs with mostly-unique lines: large edit distances
	// exercise the middle-snake bisect over many diagonals.
	big := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		big = append(big, "line-"+string(rune('a'+rng.Intn(26))))
	}
	for i := 0; i < 50; i++ {
		rng.Shuffle(len(big), func(i, j int) { big[i], big[j] = big[j], big[i] })
		checkDiffLines(t, strings.Join(big, "\n")+"\n", strings.Join(big[:150], "\n")+"\n")
	}
}

func TestDiffLinesAllDifferentAndReversed(t *testing.T) {
	var a, b []string
	for i := 0; i < 300; i++ {
		a = append(a, "a-"+string(rune('a'+i%26)))
		b = append(b, "b-"+string(rune('a'+i%26)))
	}
	checkDiffLines(t, strings.Join(a, "\n")+"\n", strings.Join(b, "\n")+"\n")
	checkDiffLines(t, strings.Join(a, "\n")+"\n", joinReversed(a))
}

func TestDiffLinesLineEndingEdgeCases(t *testing.T) {
	checkDiffLines(t, "", "")
	checkDiffLines(t, "", "a\n")
	checkDiffLines(t, "a\n", "")
	checkDiffLines(t, "\n", "\n\n")
	checkDiffLines(t, "a\r\nb\r\n", "a\nb\n")
	checkDiffLines(t, "a\r\nb", "a\nb\nc\r")
	checkDiffLines(t, "a", "a\r")
	checkDiffLines(t, "x\ny", "x\nz")
	checkDiffLines(t, "same\nlines\n", "same\nlines\n")
}

// checkDiffLines asserts that diffLines(old, new) equals the counts implied
// by the longest common subsequence of the comparison lines.
func checkDiffLines(t *testing.T, old, new string) {
	t.Helper()
	ins, del := diffLines([]byte(old), []byte(new))
	a, b := splitLines([]byte(old)), splitLines([]byte(new))
	l := lcsLength(a, b)
	if wantIns, wantDel := len(b)-l, len(a)-l; ins != wantIns || del != wantDel {
		t.Errorf("diffLines(%q, %q) = (%d, %d), want (%d, %d)", old, new, ins, del, wantIns, wantDel)
	}
}

// allSequences returns every sequence over alphabet with length up to
// maxLen, one per line of the returned content strings.
func allSequences(alphabet []string, maxLen int) []string {
	var seqs []string
	var build func(prefix []string, depth int)
	build = func(prefix []string, depth int) {
		seqs = append(seqs, strings.Join(prefix, "\n")+"\n")
		if depth == maxLen {
			return
		}
		for _, s := range alphabet {
			build(append(append([]string(nil), prefix...), s), depth+1)
		}
	}
	build(nil, 0)
	return seqs
}

func randomLines(rng *rand.Rand, alphabet []string, minLen, maxLen int) string {
	n := minLen + rng.Intn(maxLen-minLen+1)
	lines := make([]string, n)
	for i := range lines {
		lines[i] = alphabet[rng.Intn(len(alphabet))]
	}
	return strings.Join(lines, "\n") + "\n"
}

func joinReversed(lines []string) string {
	rev := make([]string, len(lines))
	for i, l := range lines {
		rev[len(lines)-1-i] = l
	}
	return strings.Join(rev, "\n") + "\n"
}

// lcsLength computes the length of the longest common subsequence of a and
// b with a plain O(n*m) dynamic program, independent of the Myers code.
func lcsLength(a, b []string) int {
	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	return dp[len(a)][len(b)]
}
