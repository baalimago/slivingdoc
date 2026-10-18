// Package recovery generates and parses the human-visible recovery code of an
// encrypted notebook (worklogs/26-10-05-encrypted-notebooks/phase-4-command-surface.md).
//
// A code is 128 random entropy bits encoded as twelve canonical lowercase
// English BIP-39 words with a checksum. The code's own bytes — the exact
// space-separated phrase — are the secret an Argon2id door wraps the notebook
// key under; BIP-39 seed derivation is never applied. Parsing a code proves the
// checksum, so a mistyped word is refused before any key work.
package recovery

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
)

const (
	// EntropyBytes is the recovery code's random entropy: 128 bits.
	EntropyBytes = 16
	// WordCount is the number of words a code carries.
	WordCount = 12
	// bitsPerWord is the BIP-39 word width.
	bitsPerWord = 11
	// checksumBits is the number of entropy SHA-256 bits appended, one per 32
	// entropy bits.
	checksumBits = EntropyBytes * 8 / 32
)

// Errors recovery-code handling reports.
var (
	// ErrCodeShape reports a code that is not exactly twelve words.
	ErrCodeShape = errors.New("recovery: a code is twelve words")
	// ErrUnknownWord reports a word the BIP-39 English list does not hold.
	ErrUnknownWord = errors.New("recovery: a word is not in the wordlist")
	// ErrChecksum reports a code whose checksum does not match its words.
	ErrChecksum = errors.New("recovery: the code checksum does not match")
)

// Code is one canonical recovery phrase: twelve lowercase BIP-39 words
// separated by single spaces.
type Code string

// Secret returns the exact bytes the recovery door's Argon2id derivation
// consumes: the canonical phrase, never a BIP-39 seed.
func (c Code) Secret() []byte { return []byte(c) }

// Generate returns a new random recovery code.
func Generate() (Code, error) {
	entropy := make([]byte, EntropyBytes)
	if _, err := rand.Read(entropy); err != nil {
		return "", fmt.Errorf("recovery: random entropy: %w", err)
	}
	return encode(entropy)
}

// Parse canonicalizes and verifies a code: exactly twelve words, every word
// known, and the checksum intact. The surrounding and repeated white space a
// person may type is collapsed; the words themselves must be lowercase.
func Parse(s string) (Code, error) {
	words := strings.Fields(s)
	if len(words) != WordCount {
		return "", fmt.Errorf("%w, got %d", ErrCodeShape, len(words))
	}
	bits := make([]byte, 0, WordCount*bitsPerWord)
	for _, w := range words {
		i, ok := index(w)
		if !ok {
			return "", fmt.Errorf("%w: %q", ErrUnknownWord, w)
		}
		bits = appendBits(bits, uint32(i), bitsPerWord)
	}
	entropy := bitsToBytes(bits[:EntropyBytes*8])
	if err := verifyChecksum(entropy, bits[EntropyBytes*8:]); err != nil {
		return "", err
	}
	return Code(strings.Join(words, " ")), nil
}

// encode renders entropy as a code.
func encode(entropy []byte) (Code, error) {
	if len(entropy) != EntropyBytes {
		return "", fmt.Errorf("recovery: entropy is %d bytes, want %d", len(entropy), EntropyBytes)
	}
	bits := make([]byte, 0, WordCount*bitsPerWord)
	for _, b := range entropy {
		bits = appendBits(bits, uint32(b), 8)
	}
	sum := sha256.Sum256(entropy)
	bits = appendBits(bits, uint32(sum[0])>>(8-checksumBits), checksumBits)
	words := make([]string, 0, WordCount)
	for i := range WordCount {
		words = append(words, wordlist[bitsToUint(bits[i*bitsPerWord:(i+1)*bitsPerWord])])
	}
	return Code(strings.Join(words, " ")), nil
}

// verifyChecksum compares the trailing bits against the entropy's digest.
func verifyChecksum(entropy, checksum []byte) error {
	sum := sha256.Sum256(entropy)
	want := bitsOfByte(sum[0], checksumBits)
	for i := range checksum {
		if checksum[i] != want[i] {
			return ErrChecksum
		}
	}
	return nil
}

// index returns the wordlist position of a word.
func index(word string) (int, bool) {
	lo, hi := 0, len(wordlist)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		switch {
		case wordlist[mid] == word:
			return mid, true
		case wordlist[mid] < word:
			lo = mid + 1
		default:
			hi = mid - 1
		}
	}
	return 0, false
}

// appendBits appends the low n bits of v to a big-endian bit slice.
func appendBits(bits []byte, v uint32, n int) []byte {
	for i := n - 1; i >= 0; i-- {
		bits = append(bits, byte(v>>i)&1)
	}
	return bits
}

// bitsOfByte returns the high n bits of b, one byte per bit.
func bitsOfByte(b byte, n int) []byte {
	out := make([]byte, 0, n)
	for i := range n {
		out = append(out, (b>>(7-i))&1)
	}
	return out
}

// bitsToBytes packs a big-endian bit slice whose length is a multiple of 8.
func bitsToBytes(bits []byte) []byte {
	out := make([]byte, len(bits)/8)
	for i, bit := range bits {
		out[i/8] |= bit << (7 - i%8)
	}
	return out
}

// bitsToUint reads a big-endian bit slice.
func bitsToUint(bits []byte) int {
	v := 0
	for _, bit := range bits {
		v = v<<1 | int(bit)
	}
	return v
}
