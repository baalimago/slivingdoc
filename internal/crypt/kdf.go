package crypt

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Argon2id acceptance window (phase-1-crypto-core.md, Parameters). A
// written cost is canonical; an accepted cost may be narrower so a
// notebook written by another build of this format still opens.
const (
	minMemoryKiB   uint32 = 64 << 10
	maxMemoryKiB   uint32 = 256 << 10
	minIterations  uint32 = 1
	maxIterations  uint32 = 3
	minParallelism uint8  = 1
	maxParallelism uint8  = 4

	// maxSecretBytes bounds every passphrase and recovery secret.
	maxSecretBytes = 1 << 10
)

// DefaultKDFParams is the Argon2id cost written for a new door: 256 MiB,
// 3 iterations, 4 lanes.
func DefaultKDFParams() KDFParams {
	return KDFParams{MemoryKiB: 256 << 10, Iterations: 3, Parallelism: 4}
}

// KDFParams is one door's Argon2id cost.
type KDFParams struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
}

// Errors the KDF layer reports.
var (
	// ErrKDFParams reports an Argon2id cost outside the accepted window.
	ErrKDFParams = errors.New("crypt: Argon2id parameters outside the accepted window")
	// ErrSecretBounds reports an empty, oversized, or non-UTF-8 secret.
	ErrSecretBounds = errors.New("crypt: secret outside the accepted bounds")
	// ErrKDFGate reports a missing or invalid KDF gate.
	ErrKDFGate = errors.New("crypt: invalid KDF gate")
)

// kdfWindow is the accepted Argon2id cost range of the derivations one gate
// admits. The canonical window is the production policy
// (phase-1-crypto-core.md, Parameters).
type kdfWindow struct {
	minMemoryKiB   uint32
	maxMemoryKiB   uint32
	minIterations  uint32
	maxIterations  uint32
	minParallelism uint8
	maxParallelism uint8
}

// canonicalWindow is the accepted cost range of every production process.
var canonicalWindow = kdfWindow{
	minMemoryKiB:   minMemoryKiB,
	maxMemoryKiB:   maxMemoryKiB,
	minIterations:  minIterations,
	maxIterations:  maxIterations,
	minParallelism: minParallelism,
	maxParallelism: maxParallelism,
}

// validate reports whether the cost is inside the window.
func (w kdfWindow) validate(p KDFParams) error {
	if p.MemoryKiB < w.minMemoryKiB || p.MemoryKiB > w.maxMemoryKiB {
		return fmt.Errorf("%w: memory %d KiB", ErrKDFParams, p.MemoryKiB)
	}
	if p.Iterations < w.minIterations || p.Iterations > w.maxIterations {
		return fmt.Errorf("%w: %d iterations", ErrKDFParams, p.Iterations)
	}
	if p.Parallelism < w.minParallelism || p.Parallelism > w.maxParallelism {
		return fmt.Errorf("%w: %d lanes", ErrKDFParams, p.Parallelism)
	}
	return nil
}

// Validate reports whether the cost is inside the canonical window.
func (p KDFParams) Validate() error { return canonicalWindow.validate(p) }

// KDFGate bounds how many Argon2id derivations run at once, so a process
// cannot multiply a door's memory cost by its concurrency, and carries the
// accepted cost window of those derivations. The application creates one
// gate per process and injects it into every keyring.
type KDFGate struct {
	sem    chan struct{}
	window kdfWindow
}

// NewKDFGate returns a gate that admits at most maxJobs simultaneous
// derivations of the canonical accepted cost window.
func NewKDFGate(maxJobs int) (*KDFGate, error) {
	return newKDFGate(maxJobs, canonicalWindow)
}

// NewKDFGateForCost returns a gate that admits at most maxJobs simultaneous
// derivations whose accepted memory floor also admits cost: the canonical
// floor is lowered only when the caller's cost is lower. The application
// passes the cost injected through ProcessOptions.KDFParams, so a test can
// derive doors with a cheap accepted cost and keep an end-to-end scenario
// inside the test budget; a production process passes the canonical default
// and the accepted window is unchanged.
func NewKDFGateForCost(maxJobs int, cost KDFParams) (*KDFGate, error) {
	// Argon2id itself refuses memory below eight KiB per lane; refuse it here
	// before the gate can admit it.
	if cost.MemoryKiB < 8*uint32(cost.Parallelism) || cost.MemoryKiB > maxMemoryKiB {
		return nil, fmt.Errorf("%w: memory %d KiB", ErrKDFParams, cost.MemoryKiB)
	}
	window := canonicalWindow
	if cost.MemoryKiB < window.minMemoryKiB {
		window.minMemoryKiB = cost.MemoryKiB
	}
	if err := window.validate(cost); err != nil {
		return nil, err
	}
	return newKDFGate(maxJobs, window)
}

// newKDFGate builds a gate of the given concurrency and accepted window.
func newKDFGate(maxJobs int, window kdfWindow) (*KDFGate, error) {
	if maxJobs < 1 {
		return nil, fmt.Errorf("%w: %d jobs", ErrKDFGate, maxJobs)
	}
	return &KDFGate{sem: make(chan struct{}, maxJobs), window: window}, nil
}

// DeriveKEK validates the secret and cost, waits for a gate slot, and
// derives the 32-byte key-encryption key with Argon2id version 19. An
// invalid secret or cost is refused before any allocation or derivation.
func DeriveKEK(ctx context.Context, gate *KDFGate, secret []byte, salt [16]byte, params KDFParams) ([32]byte, error) {
	var kek [32]byte
	if gate == nil {
		return kek, fmt.Errorf("%w: nil gate", ErrKDFGate)
	}
	if err := validateSecret(secret); err != nil {
		return kek, err
	}
	if err := gate.window.validate(params); err != nil {
		return kek, err
	}
	select {
	case gate.sem <- struct{}{}:
	case <-ctx.Done():
		return kek, fmt.Errorf("crypt: derive key: %w", ctx.Err())
	}
	defer func() { <-gate.sem }()
	copy(kek[:], argon2.IDKey(secret, salt[:], params.Iterations, params.MemoryKiB, params.Parallelism, keySize))
	return kek, nil
}

// validateSecret enforces the passphrase byte contract: non-empty, exact
// valid UTF-8 without normalization, at most 1 KiB.
func validateSecret(secret []byte) error {
	switch {
	case len(secret) == 0:
		return fmt.Errorf("%w: empty secret", ErrSecretBounds)
	case len(secret) > maxSecretBytes:
		return fmt.Errorf("%w: secret longer than %d bytes", ErrSecretBounds, maxSecretBytes)
	case !utf8.Valid(secret):
		return fmt.Errorf("%w: secret is not valid UTF-8", ErrSecretBounds)
	}
	return nil
}
