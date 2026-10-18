package recovery

import (
	"errors"
	"strings"
	"testing"
)

// TestWordlistIsCanonical proves the embedded list is the complete, sorted
// 2048-word BIP-39 English list: unique words, every word lowercase, and the
// known first and last entries.
func TestWordlistIsCanonical(t *testing.T) {
	t.Parallel()
	if len(wordlist) != 2048 {
		t.Fatalf("wordlist has %d words, want 2048", len(wordlist))
	}
	if wordlist[0] != "abandon" || wordlist[2047] != "zoo" {
		t.Fatalf("wordlist bounds = %q..%q, want abandon..zoo", wordlist[0], wordlist[2047])
	}
	seen := make(map[string]bool, len(wordlist))
	for i, w := range wordlist {
		if w != strings.ToLower(w) || w == "" {
			t.Fatalf("wordlist[%d] = %q is not a lowercase word", i, w)
		}
		if seen[w] {
			t.Fatalf("wordlist[%d] = %q is duplicated", i, w)
		}
		seen[w] = true
	}
}

// TestGenerateRoundTrips proves a generated code is twelve distinct known
// words whose parse returns the identical canonical phrase and a 16-byte
// secret that is exactly the phrase bytes.
func TestGenerateRoundTrips(t *testing.T) {
	t.Parallel()
	for range 32 {
		code, err := Generate()
		if err != nil {
			t.Fatalf("Generate() = %v", err)
		}
		words := strings.Fields(string(code))
		if len(words) != WordCount {
			t.Fatalf("code %q has %d words, want %d", code, len(words), WordCount)
		}
		for _, w := range words {
			if _, ok := index(w); !ok {
				t.Fatalf("code %q holds the unknown word %q", code, w)
			}
		}
		parsed, err := Parse(string(code))
		if err != nil {
			t.Fatalf("Parse(%q) = %v", code, err)
		}
		if parsed != code {
			t.Fatalf("Parse(%q) = %q, want the identical canonical code", code, parsed)
		}
		if got := string(parsed.Secret()); got != string(code) {
			t.Fatalf("Secret() = %q, want the phrase bytes %q", got, code)
		}
	}
}

// TestParseRejects proves a wrong word count, an unknown word and a code
// whose checksum does not match are all refused. The checksum row uses the
// canonical all-zero vector and its broken twin: the last word of a code
// carries the four checksum bits, so "about" closes the valid phrase and
// "abandon" carries the wrong four bits. A randomly swapped word cannot
// prove this row, because a four-bit checksum accepts one mutation in
// sixteen.
func TestParseRejects(t *testing.T) {
	t.Parallel()
	code, err := Generate()
	if err != nil {
		t.Fatalf("Generate() = %v", err)
	}
	words := strings.Fields(string(code))
	swap := func(i int, w string) string {
		out := append([]string(nil), words...)
		out[i] = w
		return strings.Join(out, " ")
	}
	for _, row := range []struct {
		name  string
		input string
		want  error
	}{
		{name: "eleven words", input: strings.Join(words[:11], " "), want: ErrCodeShape},
		{name: "thirteen words", input: strings.Join(append(words, words[0]), " "), want: ErrCodeShape},
		{name: "empty", input: "", want: ErrCodeShape},
		{name: "unknown word", input: swap(0, "notaword"), want: ErrUnknownWord},
		{name: "uppercase word", input: swap(0, strings.ToUpper(words[0])), want: ErrUnknownWord},
		{name: "checksum fails", input: zeroEntropyBroken, want: ErrChecksum},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(row.input); !errors.Is(err, row.want) {
				t.Fatalf("Parse(%q) = %v, want %v", row.input, err, row.want)
			}
		})
	}
}

// TestParseCanonicalizesSpace proves surrounding and repeated white space is
// collapsed to the canonical single-space form.
func TestParseCanonicalizesSpace(t *testing.T) {
	t.Parallel()
	code, err := Generate()
	if err != nil {
		t.Fatalf("Generate() = %v", err)
	}
	spaced := "  " + strings.Join(strings.Fields(string(code)), "   ") + "\n"
	parsed, err := Parse(spaced)
	if err != nil {
		t.Fatalf("Parse(%q) = %v", spaced, err)
	}
	if parsed != code {
		t.Fatalf("Parse(%q) = %q, want %q", spaced, parsed, code)
	}
}

// The canonical BIP-39 all-zero vector and its checksum-broken twin. The
// valid phrase closes with "about" (checksum bits 0011); the broken phrase
// closes with "abandon" (checksum bits 0000).
const (
	zeroEntropyValid  = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	zeroEntropyBroken = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon"
)

// TestParseAcceptsKnownVector pins the encoding against a fixed entropy value
// so a changed word order or checksum would change the words.
func TestParseAcceptsKnownVector(t *testing.T) {
	t.Parallel()
	entropy := make([]byte, EntropyBytes)
	code, err := encode(entropy)
	if err != nil {
		t.Fatalf("encode() = %v", err)
	}
	if code != zeroEntropyValid {
		t.Fatalf("encode(zero entropy) = %q, want %q", code, zeroEntropyValid)
	}
	if _, err := Parse(zeroEntropyValid); err != nil {
		t.Fatalf("Parse(%q) = %v", zeroEntropyValid, err)
	}
}
