package git

import (
	"reflect"
	"testing"
)

// TestNormalizeReadOnly checks trimming, collapsing, sorting, and refusal of
// invalid entries (architecture section 2, Read-only paths).
func TestNormalizeReadOnly(t *testing.T) {
	cases := []struct {
		name    string
		entries []string
		want    []string
	}{
		{"empty", nil, []string{}},
		{"trims trailing slash", []string{"docs/"}, []string{"docs"}},
		{"dedupes exact duplicates", []string{"docs", "docs"}, []string{"docs"}},
		{"trailing-slash duplicate collapses to one entry", []string{"docs/", "docs"}, []string{"docs"}},
		{"collapses nested entry", []string{"docs/sub", "docs"}, []string{"docs"}},
		{"collapses deeply nested entry", []string{"docs", "docs/sub", "docs/sub/deep"}, []string{"docs"}},
		{"collapses case-folded duplicate", []string{"Docs", "docs"}, []string{"Docs"}},
		{"sorts unrelated entries", []string{"notes", "docs", "faq.md"}, []string{"docs", "faq.md", "notes"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := NormalizeReadOnly(c.entries)
			if err != nil {
				t.Fatalf("NormalizeReadOnly(%v) = %v, want nil", c.entries, err)
			}
			if !reflect.DeepEqual(got.Entries(), c.want) {
				t.Fatalf("NormalizeReadOnly(%v).Entries() = %v, want %v", c.entries, got.Entries(), c.want)
			}
		})
	}

	t.Run("invalid entries", func(t *testing.T) {
		invalid := []string{"..", "/abs", ".git/x", "", "a/../b"}
		for _, entry := range invalid {
			if _, err := NormalizeReadOnly([]string{entry}); err == nil {
				t.Errorf("NormalizeReadOnly([%q]) = nil, want error", entry)
			}
		}
	})
}

// TestNormalizeReadOnlyEmptyEntriesNonNil checks Entries never returns nil.
func TestNormalizeReadOnlyEmptyEntriesNonNil(t *testing.T) {
	set, err := NormalizeReadOnly(nil)
	if err != nil {
		t.Fatalf("NormalizeReadOnly(nil) = %v", err)
	}
	if got := set.Entries(); got == nil {
		t.Fatal("Entries() = nil, want an empty non-nil slice")
	}
	var zero ReadOnlySet
	if got := zero.Entries(); got == nil {
		t.Fatal("zero value Entries() = nil, want an empty non-nil slice")
	}
}

// TestReadOnlyCovers checks segment-boundary matching under case folding.
func TestReadOnlyCovers(t *testing.T) {
	set, err := NormalizeReadOnly([]string{"docs", "faq.md"})
	if err != nil {
		t.Fatalf("NormalizeReadOnly() = %v", err)
	}
	cases := map[string]bool{
		"docs":            true,
		"docs/x.md":       true,
		"docs/sub/y.md":   true,
		"Docs/x.md":       true,
		"DOCS":            true,
		"faq.md":          true,
		"FAQ.MD":          true,
		"docsAdjacent.md": false,
		"notes/a.md":      false,
		"faq.md.bak":      false,
	}
	for path, want := range cases {
		if got := set.Covers(path); got != want {
			t.Errorf("Covers(%q) = %v, want %v", path, got, want)
		}
	}
}

// TestReadOnlyCoveringEntry checks the kept entry covering a path is returned.
func TestReadOnlyCoveringEntry(t *testing.T) {
	set, err := NormalizeReadOnly([]string{"docs", "faq.md"})
	if err != nil {
		t.Fatalf("NormalizeReadOnly() = %v", err)
	}
	cases := []struct {
		path      string
		wantEntry string
		wantOK    bool
	}{
		{"docs", "docs", true},
		{"docs/x.md", "docs", true},
		{"docs/sub/y.md", "docs", true},
		{"Docs/x.md", "docs", true},
		{"faq.md", "faq.md", true},
		{"FAQ.MD", "faq.md", true},
		{"docsAdjacent.md", "", false},
		{"notes/a.md", "", false},
	}
	for _, c := range cases {
		gotEntry, gotOK := set.CoveringEntry(c.path)
		if gotEntry != c.wantEntry || gotOK != c.wantOK {
			t.Errorf("CoveringEntry(%q) = (%q, %v), want (%q, %v)", c.path, gotEntry, gotOK, c.wantEntry, c.wantOK)
		}
	}
}

// TestReadOnlyChangedUnder checks added, changed, and deleted covered files are
// reported sorted.
func TestReadOnlyChangedUnder(t *testing.T) {
	set, err := NormalizeReadOnly([]string{"docs"})
	if err != nil {
		t.Fatalf("NormalizeReadOnly() = %v", err)
	}
	base := fakeSnapshot(map[string]string{
		"docs/a.md":  "a",
		"docs/b.md":  "b",
		"notes/x.md": "unrelated-base",
	})
	local := fakeSnapshot(map[string]string{
		"docs/a.md":  "a",       // unchanged
		"docs/b.md":  "changed", // changed
		"docs/c.md":  "added",   // added
		"notes/x.md": "unrelated-local",
	})
	got := set.ChangedUnder(local, base)
	want := []string{"docs/b.md", "docs/c.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChangedUnder() = %v, want %v (docs/a.md deleted from base but present unchanged, notes/x.md ignored)", got, want)
	}

	baseWithDeleted := fakeSnapshot(map[string]string{
		"docs/a.md": "a",
		"docs/d.md": "will be deleted",
	})
	localWithoutD := fakeSnapshot(map[string]string{
		"docs/a.md": "a",
	})
	got = set.ChangedUnder(localWithoutD, baseWithDeleted)
	want = []string{"docs/d.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChangedUnder(deletion) = %v, want %v", got, want)
	}
}

// TestReadOnlyChangedUnderEmptySet checks an empty set reports nothing.
func TestReadOnlyChangedUnderEmptySet(t *testing.T) {
	var set ReadOnlySet
	local := fakeSnapshot(map[string]string{"docs/a.md": "changed"})
	base := fakeSnapshot(map[string]string{"docs/a.md": "base"})
	if got := set.ChangedUnder(local, base); got != nil {
		t.Fatalf("ChangedUnder(empty set) = %v, want nil", got)
	}
}

// TestReadOnlyPin checks covered files are replaced by the baseline's and the
// rest kept.
func TestReadOnlyPin(t *testing.T) {
	set, err := NormalizeReadOnly([]string{"docs"})
	if err != nil {
		t.Fatalf("NormalizeReadOnly() = %v", err)
	}
	base := fakeSnapshot(map[string]string{
		"docs/a.md":  "base-a",
		"notes/x.md": "base-notes",
	})
	local := fakeSnapshot(map[string]string{
		"docs/a.md":  "local-a", // changed under the set: restored to base
		"docs/b.md":  "added",   // added under the set: dropped
		"notes/x.md": "local-notes",
	})
	got := set.Pin(local, base)
	want := fakeSnapshot(map[string]string{
		"docs/a.md":  "base-a",
		"notes/x.md": "local-notes",
	})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Pin() = %+v, want %+v", got, want)
	}

	var empty ReadOnlySet
	if got := empty.Pin(local, base); !reflect.DeepEqual(got, local) {
		t.Fatalf("Pin(empty set) = %+v, want local unchanged %+v", got, local)
	}
}

// blobCountingRepo counts ReadBlob calls.
type blobCountingRepo struct {
	Repository
	blobReads int
}

func (r *blobCountingRepo) ReadBlob(id OID) ([]byte, error) {
	r.blobReads++
	return r.Repository.ReadBlob(id)
}

// TestReadOnlyReadCovered checks only covered files are returned and no
// uncovered blob is read.
func TestReadOnlyReadCovered(t *testing.T) {
	repo := newFakeRepository()
	full := fakeSnapshot(map[string]string{
		"docs/faq.md":       "f",
		"docs/sub/x.md":     "x",
		"DOCS2/y.md":        "y",
		"notes/a.md":        "a",
		"faq.md":            "root",
		"deep/er/docs/z.md": "z",
		"deep/er/other.md":  "o",
		"Deep/er/docs/w.md": "w",
	})
	tree, err := BuildTree(repo, full)
	if err != nil {
		t.Fatalf("BuildTree() = %v", err)
	}

	t.Run("covered files only, no uncovered blob read", func(t *testing.T) {
		set, err := NormalizeReadOnly([]string{"docs", "deep/er/docs"})
		if err != nil {
			t.Fatalf("NormalizeReadOnly() = %v", err)
		}
		counting := &blobCountingRepo{Repository: repo}
		got, err := set.ReadCovered(counting, tree)
		if err != nil {
			t.Fatalf("ReadCovered() = %v", err)
		}
		want := fakeSnapshot(map[string]string{
			"docs/faq.md":       "f",
			"docs/sub/x.md":     "x",
			"deep/er/docs/z.md": "z",
			"Deep/er/docs/w.md": "w",
		})
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ReadCovered() = %+v, want %+v", got.Files, want.Files)
		}
		if counting.blobReads != len(want.Files) {
			t.Fatalf("ReadBlob calls = %d, want exactly the %d covered files", counting.blobReads, len(want.Files))
		}
	})

	t.Run("empty set reads nothing", func(t *testing.T) {
		counting := &blobCountingRepo{Repository: repo}
		got, err := ReadOnlySet{}.ReadCovered(counting, tree)
		if err != nil {
			t.Fatalf("ReadCovered() = %v", err)
		}
		if len(got.Files) != 0 || counting.blobReads != 0 {
			t.Fatalf("empty set: files = %d, blob reads = %d, want 0 and 0", len(got.Files), counting.blobReads)
		}
	})

	t.Run("unreadable tree is an error", func(t *testing.T) {
		set, _ := NormalizeReadOnly([]string{"docs"})
		if _, err := set.ReadCovered(repo, OID{}); err == nil {
			t.Fatal("ReadCovered(missing tree) = nil, want error")
		}
	})
}
