package git

import (
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
