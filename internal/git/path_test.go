package git

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestValidatePathAccepts(t *testing.T) {
	valid := []string{
		"a.txt",
		"notes/topic-a.md",
		"agents/observations.md",
		"deep/nested/dir/file",
		"unicode-ümlaut.txt",
		"emoji-📝.md",
		"a b/c d.txt",
		strings.Repeat("s", 255),         // max segment length
		strings.Repeat("s/", 2047) + "s", // 4095 bytes, below the total limit
	}
	for _, p := range valid {
		if err := ValidatePath(p); err != nil {
			t.Errorf("ValidatePath(%q) = %v, want nil", p, err)
		}
	}
}

func TestValidatePathRejects(t *testing.T) {
	invalid := []string{
		"",
		"/leading",
		"trailing/",
		"a//b",
		"./a",
		"a/../b",
		"a/./b",
		"a/.git/b",
		"a/.GIT/b",
		"a\\b",
		"a:b",
		"a*b",
		"a?b",
		`a"b`,
		"a<b",
		"a>b",
		"a|b",
		"a\x00b",
		"a\tb",
		"trailing-space ",
		"trailing-dot.",
		strings.Repeat("s", 256),
		strings.Repeat("s/", 2048), // exceeds 4096 bytes
		"CON",
		"con.txt",
		"PRN",
		"AUX",
		"NUL",
		"COM1",
		"COM9.txt",
		"LPT3",
	}
	for _, p := range invalid {
		if err := ValidatePath(p); err == nil {
			t.Errorf("ValidatePath(%q) = nil, want error", p)
		}
	}
	// COM10 and LPT10 are not reserved and must stay valid.
	if err := ValidatePath("COM10"); err != nil {
		t.Errorf("ValidatePath(COM10) = %v, want nil", err)
	}
}

func TestValidatePathRejectsNonNFC(t *testing.T) {
	// "é" in NFD (e + combining acute). The engine requires NFC paths.
	nfd := "e\u0301.txt"
	if err := ValidatePath(nfd); err == nil {
		t.Fatal("ValidatePath(NFD path) = nil, want error")
	}
	if err := ValidatePath("é.txt"); err != nil {
		t.Fatalf("ValidatePath(NFC é.txt) = %v, want nil", err)
	}
}

func TestValidatePathRejectsInvalidUTF8(t *testing.T) {
	if err := ValidatePath("a\xffb.txt"); err == nil {
		t.Fatal("ValidatePath(invalid UTF-8) = nil, want error")
	}
}

func TestValidateContent(t *testing.T) {
	if err := ValidateContent(nil); err != nil {
		t.Fatalf("ValidateContent(empty) = %v, want nil", err)
	}
	if err := ValidateContent([]byte("hello\nworld")); err != nil {
		t.Fatalf("ValidateContent(text) = %v, want nil", err)
	}
	if err := ValidateContent([]byte("caf\xc3\xa9")); err != nil {
		t.Fatalf("ValidateContent(UTF-8) = %v, want nil", err)
	}
	if err := ValidateContent([]byte{0x00}); err == nil {
		t.Fatal("ValidateContent(U+0000) = nil, want error")
	}
	if err := ValidateContent([]byte("ok\x00nul")); err == nil {
		t.Fatal("ValidateContent(embedded U+0000) = nil, want error")
	}
	if err := ValidateContent([]byte{0xff, 0xfe}); err == nil {
		t.Fatal("ValidateContent(invalid UTF-8) = nil, want error")
	}
}

func TestValidateSnapshotRejectsCaseFoldingCollisions(t *testing.T) {
	snap := fakeSnapshot(map[string]string{
		"a.txt": "one",
		"A.txt": "two",
	})
	if err := ValidateSnapshot(snap); err == nil {
		t.Fatal("ValidateSnapshot(case collision) = nil, want error")
	}

	ok := fakeSnapshot(map[string]string{
		"a.txt":     "one",
		"b.txt":     "two",
		"dir/c.txt": "three",
	})
	if err := ValidateSnapshot(ok); err != nil {
		t.Fatalf("ValidateSnapshot(valid) = %v, want nil", err)
	}
}

func TestValidateSnapshotRejectsDuplicateAndInvalidFiles(t *testing.T) {
	dup := Snapshot{Files: []File{
		{Path: "a.txt", Data: []byte("one")},
		{Path: "a.txt", Data: []byte("two")},
	}}
	if err := ValidateSnapshot(dup); err == nil {
		t.Fatal("ValidateSnapshot(duplicate path) = nil, want error")
	}

	bad := fakeSnapshot(map[string]string{"bad/../path": "x"})
	if err := ValidateSnapshot(bad); err == nil {
		t.Fatal("ValidateSnapshot(unsafe path) = nil, want error")
	}

	bin := fakeSnapshot(map[string]string{"bin.dat": string([]byte{0x00})})
	if err := ValidateSnapshot(bin); err == nil {
		t.Fatal("ValidateSnapshot(U+0000 content) = nil, want error")
	}
}

// TestValidateSnapshotRejectsFileThatIsADirectory proves a name that is a
// file and also a directory of another path is refused, exactly and under
// case folding, in either order, and names the file and the path below it.
func TestValidateSnapshotRejectsFileThatIsADirectory(t *testing.T) {
	tests := []struct {
		name  string
		files []File
		want  PathCollisionError
	}{
		{
			name:  "exact",
			files: []File{{Path: "p", Data: []byte("file")}, {Path: "p/q.md", Data: []byte("below")}},
			want:  PathCollisionError{First: "p", Path: "p/q.md", Dir: true},
		},
		{
			name:  "deeper and unsorted",
			files: []File{{Path: "a/b/c.md", Data: []byte("below")}, {Path: "a/b", Data: []byte("file")}},
			want:  PathCollisionError{First: "a/b", Path: "a/b/c.md", Dir: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSnapshot(Snapshot{Files: tt.files})
			var got *PathCollisionError
			if !errors.As(err, &got) || *got != tt.want {
				t.Fatalf("ValidateSnapshot() = %v, want %+v", err, tt.want)
			}
			if !strings.Contains(err.Error(), "is a file and a directory") {
				t.Fatalf("error text = %q, want the file/directory wording", err)
			}
		})
	}
	sibling := Snapshot{Files: []File{{Path: "p.md", Data: []byte("x")}, {Path: "p/q.md", Data: []byte("y")}}}
	if err := ValidateSnapshot(sibling); err != nil {
		t.Fatalf("ValidateSnapshot(sibling names) = %v, want nil", err)
	}
	// Accepted state an older writer published with a folded pair stays
	// readable: the folded rule is ValidateFoldedDirectories' alone.
	folded := Snapshot{Files: []File{{Path: "README", Data: []byte("file")}, {Path: "readme/x.md", Data: []byte("below")}}}
	if err := ValidateSnapshot(folded); err != nil {
		t.Fatalf("ValidateSnapshot(folded file/directory pair) = %v, want nil", err)
	}
}

// TestValidateFoldedDirectories proves the local-content rule: a file whose
// name folds to a directory's is refused naming both, exact pairs too, and
// sibling names pass.
func TestValidateFoldedDirectories(t *testing.T) {
	for _, tt := range []struct {
		name  string
		files []File
		want  *PathCollisionError
	}{
		{
			name:  "case folding",
			files: []File{{Path: "README", Data: []byte("file")}, {Path: "readme/x.md", Data: []byte("below")}},
			want:  &PathCollisionError{First: "README", Path: "readme/x.md", Fold: true, Dir: true},
		},
		{
			name:  "exact",
			files: []File{{Path: "p", Data: []byte("file")}, {Path: "p/q.md", Data: []byte("below")}},
			want:  &PathCollisionError{First: "p", Path: "p/q.md", Dir: true},
		},
		{
			name:  "siblings",
			files: []File{{Path: "P.md", Data: []byte("x")}, {Path: "p/q.md", Data: []byte("y")}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFoldedDirectories(Snapshot{Files: tt.files})
			if tt.want == nil {
				if err != nil {
					t.Fatalf("ValidateFoldedDirectories() = %v, want nil", err)
				}
				return
			}
			var got *PathCollisionError
			if !errors.As(err, &got) || *got != *tt.want {
				t.Fatalf("ValidateFoldedDirectories() = %v, want %+v", err, *tt.want)
			}
		})
	}
}

func TestFoldedDirectoryPairs(t *testing.T) {
	snap := Snapshot{Files: []File{
		{Path: "README", Data: []byte("file")},
		{Path: "a.md", Data: []byte("x")},
		{Path: "readme/x.md", Data: []byte("below")},
		{Path: "readme/y/z.md", Data: []byte("deeper")},
	}}
	want := []PathCollisionError{
		{First: "README", Path: "readme/x.md", Fold: true, Dir: true},
		{First: "README", Path: "readme/y/z.md", Fold: true, Dir: true},
	}
	if got := FoldedDirectoryPairs(snap); !reflect.DeepEqual(got, want) {
		t.Fatalf("FoldedDirectoryPairs() = %+v, want %+v", got, want)
	}
	if got := FoldedDirectoryPairs(Snapshot{Files: []File{{Path: "a.md"}, {Path: "b/c.md"}}}); got != nil {
		t.Fatalf("FoldedDirectoryPairs(no pair) = %+v, want nil", got)
	}
}
