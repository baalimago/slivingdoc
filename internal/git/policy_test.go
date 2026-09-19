package git

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// mustPolicy builds a policy the test expects to be valid.
func mustPolicy(t *testing.T, readOnly, writable []string) PathPolicy {
	t.Helper()
	p, err := NewPolicy(readOnly, writable)
	if err != nil {
		t.Fatalf("NewPolicy(%v, %v) = %v, want nil", readOnly, writable, err)
	}
	return p
}

// policyTree writes a tree for the files and returns its OID.
func policyTree(t *testing.T, repo *fakeRepository, files map[string]string) OID {
	t.Helper()
	tree, err := BuildTree(repo, fakeSnapshot(files))
	if err != nil {
		t.Fatalf("BuildTree(%v) = %v", files, err)
	}
	return tree
}

// changedProtected runs detection and fails on an unexpected error.
func changedProtected(t *testing.T, p PathPolicy, repo *fakeRepository, local, base OID) []string {
	t.Helper()
	got, err := p.ChangedProtected(repo, local, base)
	if err != nil {
		t.Fatalf("ChangedProtected() = %v, want nil", err)
	}
	return got
}

// restoreProtected runs the restore and fails on an unexpected error.
func restoreProtected(t *testing.T, p PathPolicy, repo *fakeRepository, base OID, local Snapshot, changed []string) Snapshot {
	t.Helper()
	got, err := p.RestoreProtected(repo, base, local, changed)
	if err != nil {
		t.Fatalf("RestoreProtected(%v) = %v, want nil", changed, err)
	}
	return got
}

// TestNormalizeEntriesMatchesPreviousBehaviour checks both policy sets
// normalize exactly as the read-only set does on its own.
func TestNormalizeEntriesMatchesPreviousBehaviour(t *testing.T) {
	cases := []struct {
		name    string
		entries []string
		want    []string
	}{
		{"empty", nil, []string{}},
		{"trims trailing slash", []string{"docs/"}, []string{"docs"}},
		{"drops covered entry", []string{"docs", "docs/sub"}, []string{"docs"}},
		{"sorts the result", []string{"notes", "docs", "faq.md"}, []string{"docs", "faq.md", "notes"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			set, err := NormalizeEntries(c.entries)
			if err != nil {
				t.Fatalf("NormalizeEntries(%v) = %v", c.entries, err)
			}
			policyReadOnly := mustPolicy(t, c.entries, nil).ReadOnly()
			policyWritable := mustPolicy(t, nil, c.entries).Writable()
			for name, got := range map[string][]string{
				"NormalizeEntries": set.Entries(),
				"ReadOnly":         policyReadOnly,
				"Writable":         policyWritable,
			} {
				if !reflect.DeepEqual(got, c.want) {
					t.Errorf("%s(%v) = %v, want %v", name, c.entries, got, c.want)
				}
			}
		})
	}
}

// TestNewPolicyKeepsCrossSetCoverage checks an entry covered by the other
// set survives: the composition is the feature.
func TestNewPolicyKeepsCrossSetCoverage(t *testing.T) {
	cases := []struct {
		name             string
		readOnly         []string
		writable         []string
		wantRO, wantWrit []string
	}{
		{"writable below read-only", []string{"notes"}, []string{"notes/agent-a"}, []string{"notes"}, []string{"notes/agent-a"}},
		{"read-only below writable", []string{"notes/locked"}, []string{"notes"}, []string{"notes/locked"}, []string{"notes"}},
		{
			"read-only entry the writable set splits from its own ancestor",
			[]string{"notes", "notes/agent-a/locked"},
			[]string{"notes/agent-a"},
			[]string{"notes", "notes/agent-a/locked"},
			[]string{"notes/agent-a"},
		},
		{
			"writable entry the read-only set splits from its own ancestor",
			[]string{"a/b"},
			[]string{"a", "a/b/c"},
			[]string{"a/b"},
			[]string{"a", "a/b/c"},
		},
		{
			"covered entry with no other-set entry between it and its ancestor is still dropped",
			[]string{"notes", "notes/agent-a/locked"},
			[]string{"other"},
			[]string{"notes"},
			[]string{"other"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := mustPolicy(t, c.readOnly, c.writable)
			if got := p.ReadOnly(); !reflect.DeepEqual(got, c.wantRO) {
				t.Errorf("ReadOnly() = %v, want %v", got, c.wantRO)
			}
			if got := p.Writable(); !reflect.DeepEqual(got, c.wantWrit) {
				t.Errorf("Writable() = %v, want %v", got, c.wantWrit)
			}
		})
	}
}

// TestNewPolicyRejectsExactOverlap checks a path named by both sets refuses
// construction and the error names both entries.
func TestNewPolicyRejectsExactOverlap(t *testing.T) {
	for _, writable := range [][]string{{"notes"}, {"notes/"}} {
		_, err := NewPolicy([]string{"notes"}, writable)
		var overlap *OverlapError
		if !errors.As(err, &overlap) {
			t.Fatalf("NewPolicy([notes], %v) = %v, want *OverlapError", writable, err)
		}
		if overlap.ReadOnly != "notes" || overlap.Writable != "notes" {
			t.Fatalf("OverlapError = %+v, want both entries %q", overlap, "notes")
		}
		if !strings.Contains(overlap.Error(), "notes") {
			t.Fatalf("OverlapError.Error() = %q, want the path named", overlap.Error())
		}
	}
}

// TestNewPolicyRejectsCaseFoldedOverlap checks entries differing only by
// letter case are the same path.
func TestNewPolicyRejectsCaseFoldedOverlap(t *testing.T) {
	_, err := NewPolicy([]string{"Notes"}, []string{"notes"})
	var overlap *OverlapError
	if !errors.As(err, &overlap) {
		t.Fatalf("NewPolicy([Notes], [notes]) = %v, want *OverlapError", err)
	}
	if overlap.ReadOnly != "Notes" || overlap.Writable != "notes" {
		t.Fatalf("OverlapError = %+v, want the raw entry of each set", overlap)
	}
	msg := overlap.Error()
	if !strings.Contains(msg, "Notes") || !strings.Contains(msg, "notes") {
		t.Fatalf("OverlapError.Error() = %q, want both entries named", msg)
	}
}

// TestNewPolicyRejectsCollapsedOverlap checks the overlap test runs over
// the entries the operator wrote: an entry covered by another entry of its
// own set is still refused when the other set names it too.
func TestNewPolicyRejectsCollapsedOverlap(t *testing.T) {
	cases := []struct {
		name             string
		readOnly         []string
		writable         []string
		wantRO, wantWrit string
	}{
		{"read-only ancestor beside the overlap", []string{"docs", "docs/open"}, []string{"docs/open"}, "docs/open", "docs/open"},
		{"writable ancestor beside the overlap", []string{"notes/locked"}, []string{"notes", "notes/locked"}, "notes/locked", "notes/locked"},
		{"case-folded, collapsed by its own ancestor", []string{"Docs", "Docs/Open"}, []string{"docs/open"}, "Docs/Open", "docs/open"},
		{"trailing slash, collapsed by its own ancestor", []string{"docs", "docs/open/"}, []string{"docs/open"}, "docs/open", "docs/open"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := NewPolicy(c.readOnly, c.writable)
			var overlap *OverlapError
			if !errors.As(err, &overlap) {
				t.Fatalf("NewPolicy(%v, %v) = %v, want *OverlapError", c.readOnly, c.writable, err)
			}
			if overlap.ReadOnly != c.wantRO || overlap.Writable != c.wantWrit {
				t.Fatalf("OverlapError = %+v, want {%q %q}", overlap, c.wantRO, c.wantWrit)
			}
			if p.Configured() {
				t.Fatalf("NewPolicy(%v, %v) returned a configured policy beside the error", c.readOnly, c.writable)
			}
		})
	}
}

// TestNewPolicyRejectsInvalidEntry checks a failing ValidatePath refuses
// construction and names the set the entry came from.
func TestNewPolicyRejectsInvalidEntry(t *testing.T) {
	cases := []struct {
		name     string
		readOnly []string
		writable []string
		want     string
	}{
		{"read-only set", []string{".."}, nil, `invalid read-only path "..": invalid path "..": ".." segment is not allowed`},
		{"writable set", nil, []string{"/abs"}, `invalid writable path "/abs": invalid path "/abs": must not start or end with a slash`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewPolicy(c.readOnly, c.writable)
			if err == nil || err.Error() != c.want {
				t.Fatalf("NewPolicy(%v, %v) = %v, want %q", c.readOnly, c.writable, err, c.want)
			}
		})
	}
}

// TestNewPolicyRejectsRootEntry checks neither set can name the notebook
// root, with the message ValidatePath produces today.
func TestNewPolicyRejectsRootEntry(t *testing.T) {
	cases := []struct {
		name     string
		readOnly []string
		writable []string
		want     string
	}{
		{"empty read-only entry", []string{""}, nil, `invalid read-only path "": invalid path: empty path`},
		{"dot read-only entry", []string{"."}, nil, `invalid read-only path ".": invalid path ".": "." segment is not allowed`},
		{"empty writable entry", nil, []string{""}, `invalid writable path "": invalid path: empty path`},
		{"dot writable entry", nil, []string{"."}, `invalid writable path ".": invalid path ".": "." segment is not allowed`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewPolicy(c.readOnly, c.writable)
			if err == nil || err.Error() != c.want {
				t.Fatalf("NewPolicy(%v, %v) = %v, want %q", c.readOnly, c.writable, err, c.want)
			}
		})
	}
}

// TestPolicyProtectsResolutionTable checks the longest matching entry
// decides, whichever set it came from.
func TestPolicyProtectsResolutionTable(t *testing.T) {
	cases := []struct {
		name     string
		readOnly []string
		writable []string
		path     string
		want     bool
	}{
		{"matched only by a read-only entry", []string{"docs"}, nil, "docs/a.md", true},
		{"matched only by a writable entry", nil, []string{"notes"}, "notes/a.md", false},
		{"both match, read-only entry longer", []string{"notes/locked"}, []string{"notes"}, "notes/locked/a.md", true},
		{"both match, writable entry longer", []string{"docs"}, []string{"docs/open"}, "docs/open/a.md", false},
		{
			"three levels, read-only under a writable under a read-only ancestor",
			[]string{"notes", "notes/agent-a/locked"},
			[]string{"notes/agent-a"},
			"notes/agent-a/locked/secret.md", true,
		},
		{
			"three levels, the writable middle still resolves",
			[]string{"notes", "notes/agent-a/locked"},
			[]string{"notes/agent-a"},
			"notes/agent-a/draft.md", false,
		},
		{
			"three levels, the read-only ancestor still resolves beside it",
			[]string{"notes", "notes/agent-a/locked"},
			[]string{"notes/agent-a"},
			"notes/other.md", true,
		},
		{
			"three levels, writable under a read-only under a writable ancestor",
			[]string{"a/b"},
			[]string{"a", "a/b/c"},
			"a/b/c/f.md", false,
		},
		{
			"three levels, the read-only middle still resolves",
			[]string{"a/b"},
			[]string{"a", "a/b/c"},
			"a/b/f.md", true,
		},
		{
			"three levels under case folding",
			[]string{"Notes", "Notes/Agent-A/Locked"},
			[]string{"notes/agent-a"},
			"NOTES/agent-a/LOCKED/secret.md", true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mustPolicy(t, c.readOnly, c.writable).Protects(c.path); got != c.want {
				t.Fatalf("Protects(%q) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}

// TestPolicyProtectsUnmatchedDefault checks an unmatched path follows the
// writable set's emptiness.
func TestPolicyProtectsUnmatchedDefault(t *testing.T) {
	cases := []struct {
		name     string
		readOnly []string
		writable []string
		want     bool
	}{
		{"writable set empty", []string{"docs"}, nil, false},
		{"writable set non-empty", nil, []string{"notes"}, true},
		{"both sets non-empty", []string{"docs"}, []string{"notes"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := mustPolicy(t, c.readOnly, c.writable)
			for _, path := range []string{"root.md", "elsewhere/a.md"} {
				if got := p.Protects(path); got != c.want {
					t.Errorf("Protects(%q) = %v, want %v", path, got, c.want)
				}
			}
		})
	}
}

// TestPolicyConfigured checks an empty policy is unconfigured and protects
// nothing.
func TestPolicyConfigured(t *testing.T) {
	cases := []struct {
		name     string
		readOnly []string
		writable []string
		want     bool
	}{
		{"both sets empty", nil, nil, false},
		{"read-only only", []string{"docs"}, nil, true},
		{"writable only", nil, []string{"notes"}, true},
		{"both sets", []string{"docs"}, []string{"notes"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mustPolicy(t, c.readOnly, c.writable).Configured(); got != c.want {
				t.Fatalf("Configured() = %v, want %v", got, c.want)
			}
		})
	}

	var zero PathPolicy
	if zero.Configured() {
		t.Fatal("zero PathPolicy Configured() = true, want false")
	}
	for _, path := range []string{"docs/a.md", "root.md"} {
		if zero.Protects(path) {
			t.Errorf("zero PathPolicy Protects(%q) = true, want false", path)
		}
	}
}

// TestPolicyProtectsSegmentBoundary checks a prefix that is not a segment
// boundary never matches, so the path falls to the default.
func TestPolicyProtectsSegmentBoundary(t *testing.T) {
	// The writable set is non-empty, so the default is protected and a
	// wrong match would be visible as a writable answer.
	p := mustPolicy(t, []string{"docs"}, []string{"notes"})
	cases := map[string]bool{
		"docs/a.md":       true,
		"docsAdjacent.md": true,
		"notes/a.md":      false,
		"notesAdjacent":   true,
	}
	for path, want := range cases {
		if got := p.Protects(path); got != want {
			t.Errorf("Protects(%q) = %v, want %v", path, got, want)
		}
	}
}

// TestPolicyProtectsCaseFolding checks matching folds letter case in both
// sets.
func TestPolicyProtectsCaseFolding(t *testing.T) {
	p := mustPolicy(t, []string{"Docs"}, []string{"Notes"})
	cases := map[string]bool{
		"DOCS":       true,
		"docs/a.md":  true,
		"notes/a.md": false,
		"NOTES/a.md": false,
	}
	for path, want := range cases {
		if got := p.Protects(path); got != want {
			t.Errorf("Protects(%q) = %v, want %v", path, got, want)
		}
	}
}

// writtenComposition is one configuration as the operator wrote it, beside
// the paths the oracles resolve over it.
type writtenComposition struct {
	name               string
	readOnly, writable []string
	paths              []string
}

// writtenCompositions carries the two-level composition and both three-level
// nestings, the shape where an entry sits below an ancestor of its own set
// with an entry of the other set between them (review 2, R2-01).
func writtenCompositions() []writtenComposition {
	return []writtenComposition{
		{
			name:     "two levels, disjoint and nested pairs",
			readOnly: []string{"notes", "docs/locked", "faq.md"},
			writable: []string{"notes/agent-a", "docs", "Notes/agent-b"},
			paths: []string{
				"notes", "notes/a.md", "notes/agent-a/x.md", "notes/agent-b/y.md",
				"docs", "docs/a.md", "docs/locked", "docs/locked/deep/z.md",
				"faq.md", "faq.md.bak", "root.md", "NOTES/AGENT-A/x.md",
			},
		},
		{
			name:     "three levels, read-only under a writable under a read-only ancestor",
			readOnly: []string{"notes", "notes/agent-a/locked"},
			writable: []string{"notes/agent-a"},
			paths: []string{
				"notes", "notes/other.md", "notes/agent-a", "notes/agent-a/draft.md",
				"notes/agent-a/locked", "notes/agent-a/locked/secret.md",
				"NOTES/agent-a/LOCKED/secret.md", "root.md",
			},
		},
		{
			name:     "three levels, writable under a read-only under a writable ancestor",
			readOnly: []string{"a/b"},
			writable: []string{"a", "a/b/c"},
			paths: []string{
				"a", "a/x.md", "a/b", "a/b/f.md", "a/b/c", "a/b/c/f.md",
				"a/b/c/deep/g.md", "root.md",
			},
		},
	}
}

// TestPolicyProtectsLongestMatchIsUnique pins the argument resolution rests
// on, over the entries the operator wrote: two entries matching across the
// sets never have equal folded length, so no tie-break exists, and Protects
// answers from the longest written match. The oracle reads the written
// entries rather than ReadOnly() and Writable(), which the collapse has
// already normalized, so it can observe an entry the collapse removes
// (review 2, R2-01).
func TestPolicyProtectsLongestMatchIsUnique(t *testing.T) {
	for _, c := range writtenCompositions() {
		t.Run(c.name, func(t *testing.T) {
			p := mustPolicy(t, c.readOnly, c.writable)
			for _, path := range c.paths {
				ro := longestWrittenMatch(c.readOnly, path)
				wr := longestWrittenMatch(c.writable, path)
				if ro != "" && wr != "" && len(entryFold.String(ro)) == len(entryFold.String(wr)) {
					t.Fatalf("path %q matched %q and %q of equal folded length, want a strict longest match", path, ro, wr)
				}
				want := protectedByWritten(ro, wr, c.writable)
				if got := p.Protects(path); got != want {
					t.Errorf("Protects(%q) = %v, want %v from written read-only %q and writable %q",
						path, got, want, ro, wr)
				}
			}
		})
	}
}

// TestPolicyRebuiltFromAccessorsResolvesAlike checks the entry accessors are
// a lossless input for resolution. The production path rebuilds the policy
// from the previous one's accessors twice more before enforcement reads it
// (review 2, R2-03), so a collapse that dropped an entry resolution needs
// would survive the first construction and be lost by the third.
func TestPolicyRebuiltFromAccessorsResolvesAlike(t *testing.T) {
	for _, c := range writtenCompositions() {
		t.Run(c.name, func(t *testing.T) {
			p := mustPolicy(t, c.readOnly, c.writable)
			rebuilt := p
			for range 3 {
				rebuilt = mustPolicy(t, rebuilt.ReadOnly(), rebuilt.Writable())
			}
			if got, want := rebuilt.ReadOnly(), p.ReadOnly(); !reflect.DeepEqual(got, want) {
				t.Errorf("rebuilt ReadOnly() = %v, want %v", got, want)
			}
			if got, want := rebuilt.Writable(), p.Writable(); !reflect.DeepEqual(got, want) {
				t.Errorf("rebuilt Writable() = %v, want %v", got, want)
			}
			for _, path := range c.paths {
				if got, want := rebuilt.Protects(path), p.Protects(path); got != want {
					t.Errorf("rebuilt Protects(%q) = %v, want %v", path, got, want)
				}
			}
		})
	}
}

// longestWrittenMatch returns the longest entry covering path, matched over
// the entries as the operator wrote them rather than over the policy's own
// normalized set.
func longestWrittenMatch(entries []string, path string) string {
	foldedPath := entryFold.String(path)
	best := ""
	for _, e := range entries {
		trimmed := strings.TrimSuffix(e, "/")
		folded := entryFold.String(trimmed)
		if foldedPath != folded && !isEntryAncestor(folded, foldedPath) {
			continue
		}
		if best == "" || len(folded) > len(entryFold.String(best)) {
			best = trimmed
		}
	}
	return best
}

// protectedByWritten is the resolution rule of README invariant 5 spelled
// out over the written entries: the longest match decides, and an unmatched
// path follows the writable set's emptiness.
func protectedByWritten(readOnly, writable string, writableSet []string) bool {
	switch {
	case readOnly != "" && writable != "":
		return len(entryFold.String(readOnly)) > len(entryFold.String(writable))
	case readOnly != "":
		return true
	case writable != "":
		return false
	default:
		return len(writableSet) > 0
	}
}

// TestPolicyEntriesAreCopies checks a caller cannot mutate the policy
// through the accessors.
func TestPolicyEntriesAreCopies(t *testing.T) {
	p := mustPolicy(t, []string{"docs"}, []string{"notes"})
	p.ReadOnly()[0] = "hacked"
	p.Writable()[0] = "hacked"
	if got := p.ReadOnly(); !reflect.DeepEqual(got, []string{"docs"}) {
		t.Errorf("ReadOnly() = %v, want [docs]", got)
	}
	if got := p.Writable(); !reflect.DeepEqual(got, []string{"notes"}) {
		t.Errorf("Writable() = %v, want [notes]", got)
	}

	var zero PathPolicy
	if zero.ReadOnly() == nil || zero.Writable() == nil {
		t.Fatal("zero PathPolicy accessors returned nil, want empty non-nil slices")
	}
}

// TestChangedProtectedSkipsEqualSubtrees checks descent stops where the two
// sides share a tree ID, at the root and below it.
func TestChangedProtectedSkipsEqualSubtrees(t *testing.T) {
	repo := newFakeRepository()
	p := mustPolicy(t, []string{"keep", "change"}, nil)
	base := policyTree(t, repo, map[string]string{"keep/deep/x.md": "x", "change/y.md": "y"})
	local := policyTree(t, repo, map[string]string{"keep/deep/x.md": "x", "change/y.md": "z"})

	repo.treeReads = 0
	got := changedProtected(t, p, repo, local, base)
	if want := []string{"change/y.md"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ChangedProtected() = %v, want %v", got, want)
	}
	// Both roots and both sides of change/ only: keep/ and keep/deep/ are
	// never opened.
	if repo.treeReads != 4 {
		t.Fatalf("ReadTree calls = %d, want 4 (two roots and both sides of the differing subtree)", repo.treeReads)
	}

	t.Run("identical trees are skipped whole", func(t *testing.T) {
		repo.treeReads = 0
		if got := changedProtected(t, p, repo, base, base); len(got) != 0 {
			t.Fatalf("ChangedProtected(base, base) = %v, want none", got)
		}
		if repo.treeReads != 0 {
			t.Fatalf("ReadTree calls = %d, want 0", repo.treeReads)
		}
	})
}

// TestChangedProtectedReadsNoBlob checks detection proves a difference from
// object IDs alone.
func TestChangedProtectedReadsNoBlob(t *testing.T) {
	repo := newFakeRepository()
	p := mustPolicy(t, []string{"docs"}, nil)
	base := policyTree(t, repo, map[string]string{
		"docs/a.md": "a", "docs/gone.md": "g", "docs/thing": "file", "notes/x.md": "x",
	})
	local := policyTree(t, repo, map[string]string{
		"docs/a.md": "A", "docs/added.md": "n", "docs/thing/inner.md": "i", "notes/x.md": "X",
	})

	repo.blobReads = 0
	got := changedProtected(t, p, repo, local, base)
	if len(got) == 0 {
		t.Fatal("ChangedProtected() reported nothing, want the protected differences")
	}
	if repo.blobReads != 0 {
		t.Fatalf("ReadBlob calls = %d, want 0", repo.blobReads)
	}
}

// TestChangedProtectedReportsAdditions checks a protected path present only
// on the local side is a difference.
func TestChangedProtectedReportsAdditions(t *testing.T) {
	repo := newFakeRepository()
	p := mustPolicy(t, []string{"docs"}, nil)
	base := policyTree(t, repo, map[string]string{"docs/a.md": "a", "notes/x.md": "x"})
	local := policyTree(t, repo, map[string]string{
		"docs/a.md": "a", "docs/new.md": "n", "docs/sub/deep.md": "d", "notes/new.md": "n",
	})
	got := changedProtected(t, p, repo, local, base)
	want := []string{"docs/new.md", "docs/sub/deep.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChangedProtected() = %v, want %v", got, want)
	}
}

// TestChangedProtectedReportsRemovals checks a protected path present only
// on the base side is a difference.
func TestChangedProtectedReportsRemovals(t *testing.T) {
	repo := newFakeRepository()
	p := mustPolicy(t, []string{"docs"}, nil)
	base := policyTree(t, repo, map[string]string{
		"docs/a.md": "a", "docs/gone.md": "g", "docs/sub/deep.md": "d", "notes/gone.md": "g",
	})
	local := policyTree(t, repo, map[string]string{"docs/a.md": "a"})
	got := changedProtected(t, p, repo, local, base)
	want := []string{"docs/gone.md", "docs/sub/deep.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChangedProtected() = %v, want %v", got, want)
	}
}

// TestChangedProtectedReportsModifications checks differing blob IDs at one
// path are a difference, reported once.
func TestChangedProtectedReportsModifications(t *testing.T) {
	repo := newFakeRepository()
	p := mustPolicy(t, []string{"docs"}, nil)
	base := policyTree(t, repo, map[string]string{"docs/a.md": "a", "docs/b.md": "b"})
	local := policyTree(t, repo, map[string]string{"docs/a.md": "changed", "docs/b.md": "b"})
	got := changedProtected(t, p, repo, local, base)
	want := []string{"docs/a.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChangedProtected() = %v, want %v", got, want)
	}
}

// TestChangedProtectedIgnoresWritableDifferences checks the policy filters
// every reported difference.
func TestChangedProtectedIgnoresWritableDifferences(t *testing.T) {
	repo := newFakeRepository()
	p := mustPolicy(t, []string{"docs"}, nil)
	base := policyTree(t, repo, map[string]string{"docs/a.md": "a", "notes/x.md": "x"})
	local := policyTree(t, repo, map[string]string{"docs/a.md": "a", "notes/x.md": "X", "root.md": "r"})
	if got := changedProtected(t, p, repo, local, base); len(got) != 0 {
		t.Fatalf("ChangedProtected() = %v, want none", got)
	}
}

// TestChangedProtectedReportsThreeLevelProtection checks detection resolves
// the composition the feature exists for: a read-only entry the operator
// wrote below a writable entry, itself below a read-only ancestor. Under the
// collapse that decided resolution this returned nothing and the commit
// published (review 2, R2-01).
func TestChangedProtectedReportsThreeLevelProtection(t *testing.T) {
	cases := []struct {
		name               string
		readOnly, writable []string
		want               []string
	}{
		{
			"read-only under a writable under a read-only ancestor",
			[]string{"notes", "notes/agent-a/locked"},
			[]string{"notes/agent-a"},
			[]string{"notes/agent-a/locked/secret.md", "notes/other.md"},
		},
		{
			"writable under a read-only under a writable ancestor",
			[]string{"notes/agent-a"},
			[]string{"notes", "notes/agent-a/open"},
			[]string{"notes/agent-a/locked/secret.md", "notes/agent-a/notes.md"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := newFakeRepository()
			p := mustPolicy(t, c.readOnly, c.writable)
			base := policyTree(t, repo, map[string]string{
				"notes/agent-a/locked/secret.md": "original",
				"notes/agent-a/open/draft.md":    "draft",
				"notes/agent-a/notes.md":         "notes",
				"notes/other.md":                 "other",
			})
			local := policyTree(t, repo, map[string]string{
				"notes/agent-a/locked/secret.md": "TAMPERED",
				"notes/agent-a/open/draft.md":    "DRAFT",
				"notes/agent-a/notes.md":         "NOTES",
				"notes/other.md":                 "OTHER",
			})
			if got := changedProtected(t, p, repo, local, base); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("ChangedProtected() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestChangedProtectedDescendsMixedSubtree checks descent is decided by ID
// equality and never by the policy.
func TestChangedProtectedDescendsMixedSubtree(t *testing.T) {
	repo := newFakeRepository()
	p := mustPolicy(t, []string{"mixed"}, []string{"mixed/free.md"})
	base := policyTree(t, repo, map[string]string{"mixed/free.md": "a", "mixed/locked.md": "b"})
	local := policyTree(t, repo, map[string]string{"mixed/free.md": "A", "mixed/locked.md": "B"})
	got := changedProtected(t, p, repo, local, base)
	want := []string{"mixed/locked.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChangedProtected() = %v, want %v", got, want)
	}
}

// TestChangedProtectedReportsTypeChange checks a path that is a blob on one
// side and a tree on the other is reported on both sides.
func TestChangedProtectedReportsTypeChange(t *testing.T) {
	repo := newFakeRepository()
	p := mustPolicy(t, []string{"docs"}, nil)
	base := policyTree(t, repo, map[string]string{"docs/thing": "file"})
	local := policyTree(t, repo, map[string]string{"docs/thing/inner.md": "i", "docs/thing/sub/deep.md": "d"})

	got := changedProtected(t, p, repo, local, base)
	want := []string{"docs/thing", "docs/thing/inner.md", "docs/thing/sub/deep.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChangedProtected(blob to tree) = %v, want %v", got, want)
	}
	got = changedProtected(t, p, repo, base, local)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChangedProtected(tree to blob) = %v, want %v", got, want)
	}
}

// TestChangedProtectedSortedResult checks the result is sorted and holds no
// duplicate.
func TestChangedProtectedSortedResult(t *testing.T) {
	repo := newFakeRepository()
	p := mustPolicy(t, nil, []string{"free"})
	base := policyTree(t, repo, map[string]string{
		"zeta.md": "z", "alpha/a.md": "a", "mid/b.md": "b", "mid/thing": "t", "free/f.md": "f",
	})
	local := policyTree(t, repo, map[string]string{
		"alpha/a.md": "A", "beta.md": "b", "mid/b.md": "B", "mid/thing/inner.md": "i", "free/f.md": "F",
	})
	got := changedProtected(t, p, repo, local, base)
	want := []string{"alpha/a.md", "beta.md", "mid/b.md", "mid/thing", "mid/thing/inner.md", "zeta.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChangedProtected() = %v, want %v", got, want)
	}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("ChangedProtected() = %v, want sorted", got)
	}
	seen := map[string]bool{}
	for _, path := range got {
		if seen[path] {
			t.Fatalf("ChangedProtected() = %v, want no duplicate of %q", got, path)
		}
		seen[path] = true
	}
}

// TestChangedProtectedUnconfiguredWalksNothing checks an unconfigured
// policy opens no object at all.
func TestChangedProtectedUnconfiguredWalksNothing(t *testing.T) {
	repo := newFakeRepository()
	base := policyTree(t, repo, map[string]string{"docs/a.md": "a"})
	local := policyTree(t, repo, map[string]string{"docs/a.md": "changed"})

	repo.treeReads, repo.blobReads = 0, 0
	var zero PathPolicy
	if got := changedProtected(t, zero, repo, local, base); got != nil {
		t.Fatalf("ChangedProtected(unconfigured) = %v, want nil", got)
	}
	if repo.treeReads != 0 || repo.blobReads != 0 {
		t.Fatalf("ReadTree calls = %d, ReadBlob calls = %d, want 0 and 0", repo.treeReads, repo.blobReads)
	}
}

// TestChangedProtectedRepositoryFailure checks a failed tree read names the
// directory and returns no partial result.
func TestChangedProtectedRepositoryFailure(t *testing.T) {
	repo := newFakeRepository()
	p := mustPolicy(t, []string{"docs"}, nil)
	base := policyTree(t, repo, map[string]string{"docs/a.md": "a"})
	local := policyTree(t, repo, map[string]string{"docs/a.md": "changed"})
	entries, err := repo.ReadTree(base)
	if err != nil {
		t.Fatalf("ReadTree(base) = %v", err)
	}
	delete(repo.trees, entries[0].ID) // the base docs/ subtree

	got, err := p.ChangedProtected(repo, local, base)
	if err == nil {
		t.Fatal("ChangedProtected(unreadable subtree) = nil, want an error")
	}
	if got != nil {
		t.Fatalf("ChangedProtected() = %v with an error, want no partial result", got)
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "git: changed protected paths: ") || !strings.Contains(msg, `tree "docs"`) {
		t.Fatalf("ChangedProtected() error = %q, want the package prefix and the tree path", msg)
	}
}

// TestChangedProtectedUnsupportedMode checks a tree entry the notebook
// cannot store fails detection and names the path.
func TestChangedProtectedUnsupportedMode(t *testing.T) {
	repo := newFakeRepository()
	p := mustPolicy(t, []string{"docs"}, nil)
	blob, err := repo.WriteBlob([]byte("target"))
	if err != nil {
		t.Fatalf("WriteBlob() = %v", err)
	}
	sub, err := repo.WriteTree([]TreeEntry{{Name: "link", Mode: 0o120000, ID: blob}})
	if err != nil {
		t.Fatalf("WriteTree() = %v", err)
	}
	local, err := repo.WriteTree([]TreeEntry{{Name: "docs", Mode: ModeTree, ID: sub}})
	if err != nil {
		t.Fatalf("WriteTree() = %v", err)
	}
	base := policyTree(t, repo, map[string]string{"notes/x.md": "x"})

	got, err := p.ChangedProtected(repo, local, base)
	if err == nil {
		t.Fatal("ChangedProtected(unsupported mode) = nil, want an error")
	}
	if got != nil {
		t.Fatalf("ChangedProtected() = %v with an error, want no partial result", got)
	}
	var unsupported *UnsupportedModeError
	if !errors.As(err, &unsupported) {
		t.Fatalf("ChangedProtected() error = %v, want *UnsupportedModeError", err)
	}
	if unsupported.Name != "docs/link" {
		t.Fatalf("UnsupportedModeError.Name = %q, want %q", unsupported.Name, "docs/link")
	}
}

// TestRestoreProtectedReplacesContent checks a named path present on both
// sides takes the base content.
func TestRestoreProtectedReplacesContent(t *testing.T) {
	repo := newFakeRepository()
	p := mustPolicy(t, []string{"docs"}, nil)
	base := policyTree(t, repo, map[string]string{"docs/a.md": "base", "notes/x.md": "base-x"})
	local := fakeSnapshot(map[string]string{"docs/a.md": "local", "notes/x.md": "local-x"})

	got := restoreProtected(t, p, repo, base, local, []string{"docs/a.md"})
	want := fakeSnapshot(map[string]string{"docs/a.md": "base", "notes/x.md": "local-x"})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RestoreProtected() = %+v, want %+v", got.Files, want.Files)
	}
}

// TestRestoreProtectedRestoresDeletion checks a named path absent from the
// local snapshot is added back.
func TestRestoreProtectedRestoresDeletion(t *testing.T) {
	repo := newFakeRepository()
	p := mustPolicy(t, []string{"docs"}, nil)
	base := policyTree(t, repo, map[string]string{"docs/a.md": "base-a", "docs/sub/b.md": "base-b"})
	local := fakeSnapshot(map[string]string{"notes/x.md": "x"})

	got := restoreProtected(t, p, repo, base, local, []string{"docs/a.md", "docs/sub/b.md"})
	want := fakeSnapshot(map[string]string{"docs/a.md": "base-a", "docs/sub/b.md": "base-b", "notes/x.md": "x"})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RestoreProtected() = %+v, want %+v", got.Files, want.Files)
	}
}

// TestRestoreProtectedDropsAddition checks a named path absent from the
// base tree leaves the snapshot.
func TestRestoreProtectedDropsAddition(t *testing.T) {
	repo := newFakeRepository()
	p := mustPolicy(t, []string{"docs"}, nil)
	base := policyTree(t, repo, map[string]string{"docs/a.md": "base-a"})
	local := fakeSnapshot(map[string]string{"docs/a.md": "base-a", "docs/added.md": "new"})

	got := restoreProtected(t, p, repo, base, local, []string{"docs/added.md"})
	want := fakeSnapshot(map[string]string{"docs/a.md": "base-a"})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RestoreProtected() = %+v, want %+v", got.Files, want.Files)
	}
}

// TestRestoreProtectedRestoresTypeChange checks a base file standing where
// the local snapshot holds a directory leaves neither shape twice.
func TestRestoreProtectedRestoresTypeChange(t *testing.T) {
	repo := newFakeRepository()
	p := mustPolicy(t, []string{"docs"}, nil)
	base := policyTree(t, repo, map[string]string{"docs/thing": "file"})
	local := fakeSnapshot(map[string]string{"docs/thing/inner.md": "i", "notes/x.md": "x"})

	got := restoreProtected(t, p, repo, base, local, []string{"docs/thing", "docs/thing/inner.md"})
	want := fakeSnapshot(map[string]string{"docs/thing": "file", "notes/x.md": "x"})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RestoreProtected() = %+v, want %+v", got.Files, want.Files)
	}
}

// TestRestoreProtectedTouchesOnlyNamedPaths checks an unnamed path is left
// alone whatever the policy says, and only the named paths are read.
func TestRestoreProtectedTouchesOnlyNamedPaths(t *testing.T) {
	repo := newFakeRepository()
	p := mustPolicy(t, []string{"docs"}, nil)
	base := policyTree(t, repo, map[string]string{
		"docs/a.md": "base-a", "docs/b.md": "base-b", "notes/x.md": "base-x",
	})
	local := fakeSnapshot(map[string]string{
		"docs/a.md": "local-a", "docs/b.md": "local-b", "notes/x.md": "local-x",
	})

	repo.blobReads = 0
	got := restoreProtected(t, p, repo, base, local, []string{"docs/a.md"})
	want := fakeSnapshot(map[string]string{
		"docs/a.md": "base-a", "docs/b.md": "local-b", "notes/x.md": "local-x",
	})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RestoreProtected() = %+v, want %+v", got.Files, want.Files)
	}
	if repo.blobReads != 1 {
		t.Fatalf("ReadBlob calls = %d, want exactly the one named path", repo.blobReads)
	}
}

// TestRestoreProtectedEmptyListReadsNothing checks an operation with no
// protected change opens no object.
func TestRestoreProtectedEmptyListReadsNothing(t *testing.T) {
	repo := newFakeRepository()
	p := mustPolicy(t, []string{"docs"}, nil)
	base := policyTree(t, repo, map[string]string{"docs/a.md": "base"})
	local := fakeSnapshot(map[string]string{"docs/a.md": "local"})

	repo.treeReads, repo.blobReads = 0, 0
	got := restoreProtected(t, p, repo, base, local, nil)
	if !reflect.DeepEqual(got, local) {
		t.Fatalf("RestoreProtected(nil) = %+v, want the snapshot unchanged %+v", got.Files, local.Files)
	}
	if repo.treeReads != 0 || repo.blobReads != 0 {
		t.Fatalf("ReadTree calls = %d, ReadBlob calls = %d, want 0 and 0", repo.treeReads, repo.blobReads)
	}
}

// TestRestoreProtectedResultIsValidSnapshot checks the result is sorted and
// passes the snapshot rules.
func TestRestoreProtectedResultIsValidSnapshot(t *testing.T) {
	repo := newFakeRepository()
	p := mustPolicy(t, []string{"docs"}, nil)
	base := policyTree(t, repo, map[string]string{"docs/a.md": "base-a", "docs/zeta.md": "base-z"})
	local := fakeSnapshot(map[string]string{"notes/x.md": "x", "alpha.md": "a"})

	got := restoreProtected(t, p, repo, base, local, []string{"docs/zeta.md", "docs/a.md"})
	paths := make([]string, len(got.Files))
	for i, f := range got.Files {
		paths[i] = f.Path
	}
	if !sort.StringsAreSorted(paths) {
		t.Fatalf("RestoreProtected() paths = %v, want sorted", paths)
	}
	if err := ValidateSnapshot(got); err != nil {
		t.Fatalf("ValidateSnapshot(RestoreProtected()) = %v, want nil", err)
	}
}

// TestRestoreProtectedRepositoryFailure checks a failed blob read names the
// path and returns no partial snapshot.
func TestRestoreProtectedRepositoryFailure(t *testing.T) {
	repo := newFakeRepository()
	p := mustPolicy(t, []string{"docs"}, nil)
	base := policyTree(t, repo, map[string]string{"docs/a.md": "base"})
	repo.blobs = map[OID][]byte{}
	local := fakeSnapshot(map[string]string{"docs/a.md": "local"})

	got, err := p.RestoreProtected(repo, base, local, []string{"docs/a.md"})
	if err == nil {
		t.Fatal("RestoreProtected(unreadable blob) = nil, want an error")
	}
	if len(got.Files) != 0 {
		t.Fatalf("RestoreProtected() = %+v with an error, want no partial snapshot", got.Files)
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "git: restore protected paths: ") || !strings.Contains(msg, `"docs/a.md"`) {
		t.Fatalf("RestoreProtected() error = %q, want the package prefix and the path", msg)
	}
}

// TestRestoreProtectedUnknownPathSkipped checks a path on neither side is
// skipped rather than failing.
func TestRestoreProtectedUnknownPathSkipped(t *testing.T) {
	repo := newFakeRepository()
	p := mustPolicy(t, []string{"docs"}, nil)
	base := policyTree(t, repo, map[string]string{"docs/a.md": "base"})
	local := fakeSnapshot(map[string]string{"notes/x.md": "x"})

	// A missing top-level name, a missing name below a tree, a directory
	// where a file was asked for, and a file used as a directory.
	named := []string{"ghost.md", "docs/ghost.md", "docs", "docs/a.md/deeper.md"}
	got := restoreProtected(t, p, repo, base, local, named)
	if !reflect.DeepEqual(got, local) {
		t.Fatalf("RestoreProtected(%v) = %+v, want the snapshot unchanged %+v", named, got.Files, local.Files)
	}
}

// TestRestoreProtectedInvalidSnapshotRejected checks base content breaking
// the content rules surfaces through the existing validation.
func TestRestoreProtectedInvalidSnapshotRejected(t *testing.T) {
	repo := newFakeRepository()
	p := mustPolicy(t, []string{"docs"}, nil)
	blob, err := repo.WriteBlob([]byte("bad\x00content"))
	if err != nil {
		t.Fatalf("WriteBlob() = %v", err)
	}
	sub, err := repo.WriteTree([]TreeEntry{{Name: "a.md", Mode: ModeBlob, ID: blob}})
	if err != nil {
		t.Fatalf("WriteTree() = %v", err)
	}
	base, err := repo.WriteTree([]TreeEntry{{Name: "docs", Mode: ModeTree, ID: sub}})
	if err != nil {
		t.Fatalf("WriteTree() = %v", err)
	}
	local := fakeSnapshot(map[string]string{"docs/a.md": "local"})

	got, err := p.RestoreProtected(repo, base, local, []string{"docs/a.md"})
	if err == nil {
		t.Fatal("RestoreProtected(invalid base content) = nil, want an error")
	}
	if len(got.Files) != 0 {
		t.Fatalf("RestoreProtected() = %+v with an error, want no partial snapshot", got.Files)
	}
	if !strings.Contains(err.Error(), "invalid content") {
		t.Fatalf("RestoreProtected() error = %q, want the snapshot validation failure", err)
	}
}
