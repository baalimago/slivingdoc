package crypt

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"runtime"
	"testing"
	"time"
)

func TestKDFLimitsAndGate(t *testing.T) {
	var salt [16]byte
	gate := testGate(t, 1)

	outOfWindow := []KDFParams{
		{},
		{MemoryKiB: minMemoryKiB - 1, Iterations: 1, Parallelism: 1},
		{MemoryKiB: maxMemoryKiB + 1, Iterations: 1, Parallelism: 1},
		{MemoryKiB: minMemoryKiB, Iterations: 0, Parallelism: 1},
		{MemoryKiB: minMemoryKiB, Iterations: maxIterations + 1, Parallelism: 1},
		{MemoryKiB: minMemoryKiB, Iterations: 1, Parallelism: 0},
		{MemoryKiB: minMemoryKiB, Iterations: 1, Parallelism: maxParallelism + 1},
	}
	for _, params := range outOfWindow {
		if _, err := DeriveKEK(testContext, gate, testSecret, salt, params); !errors.Is(err, ErrKDFParams) {
			t.Fatalf("DeriveKEK(%+v) = %v, want %v", params, err, ErrKDFParams)
		}
	}

	if _, err := NewKDFGate(0); !errors.Is(err, ErrKDFGate) {
		t.Fatalf("NewKDFGate(0) = %v, want %v", err, ErrKDFGate)
	}
	if _, err := NewKDFGate(-1); !errors.Is(err, ErrKDFGate) {
		t.Fatalf("NewKDFGate(-1) = %v, want %v", err, ErrKDFGate)
	}
	if _, err := DeriveKEK(testContext, nil, testSecret, salt, cheapKDFParams()); !errors.Is(err, ErrKDFGate) {
		t.Fatalf("DeriveKEK(nil gate) = %v, want %v", err, ErrKDFGate)
	}

	// The same inputs derive the same KEK.
	first, err := DeriveKEK(testContext, gate, testSecret, salt, cheapKDFParams())
	if err != nil {
		t.Fatalf("DeriveKEK() = %v", err)
	}
	second, err := DeriveKEK(testContext, gate, testSecret, salt, cheapKDFParams())
	if err != nil {
		t.Fatalf("DeriveKEK() = %v", err)
	}
	if first != second {
		t.Fatalf("DeriveKEK() = %x then %x, want equal", first, second)
	}

	// A full gate blocks a new derivation, and the wait honors cancellation.
	full := testGate(t, 1)
	full.sem <- struct{}{}
	ctx, cancel := context.WithTimeout(testContext, 50*time.Millisecond)
	defer cancel()
	if _, err := DeriveKEK(ctx, full, testSecret, salt, cheapKDFParams()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("DeriveKEK(blocked) = %v, want %v", err, context.DeadlineExceeded)
	}
	<-full.sem

	two := testGate(t, 2)
	two.sem <- struct{}{}
	two.sem <- struct{}{}
	ctxTwo, cancelTwo := context.WithTimeout(testContext, 50*time.Millisecond)
	defer cancelTwo()
	if _, err := DeriveKEK(ctxTwo, two, testSecret, salt, cheapKDFParams()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("DeriveKEK(blocked on two slots) = %v, want %v", err, context.DeadlineExceeded)
	}
	<-two.sem
	<-two.sem

	// A one-job gate holds its only slot for the whole derivation.
	serial := testGate(t, 1)
	done := make(chan error, 1)
	go func() {
		_, err := DeriveKEK(testContext, serial, testSecret, salt, cheapKDFParams())
		done <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for len(serial.sem) == 0 && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if len(serial.sem) != 1 {
		t.Fatal("a one-job gate admitted no derivation")
	}
	if err := <-done; err != nil {
		t.Fatalf("DeriveKEK() = %v", err)
	}
}

func TestSecretInputBounds(t *testing.T) {
	var salt [16]byte
	gate := testGate(t, 1)

	invalid := [][]byte{
		nil,
		{},
		bytes.Repeat([]byte("a"), maxSecretBytes+1),
		{0xff, 0xfe},
	}
	for _, secret := range invalid {
		if _, err := DeriveKEK(testContext, gate, secret, salt, cheapKDFParams()); !errors.Is(err, ErrSecretBounds) {
			t.Fatalf("DeriveKEK(%d-byte secret) = %v, want %v", len(secret), err, ErrSecretBounds)
		}
	}
	if _, err := DeriveKEK(testContext, gate, bytes.Repeat([]byte("a"), maxSecretBytes), salt, cheapKDFParams()); err != nil {
		t.Fatalf("DeriveKEK(exact bound) = %v", err)
	}

	// Validation precedes the gate: an invalid secret is refused even
	// while every slot is taken.
	full := testGate(t, 1)
	full.sem <- struct{}{}
	ctx, cancel := context.WithTimeout(testContext, 50*time.Millisecond)
	defer cancel()
	if _, err := DeriveKEK(ctx, full, nil, salt, cheapKDFParams()); !errors.Is(err, ErrSecretBounds) {
		t.Fatalf("DeriveKEK(empty secret, full gate) = %v, want %v", err, ErrSecretBounds)
	}
	<-full.sem

	if _, err := WrapKey(testContext, gate, fixedKey(), nil, testObjectKey, DoorPassphrase, cheapKDFParams()); !errors.Is(err, ErrSecretBounds) {
		t.Fatalf("WrapKey(empty secret) = %v, want %v", err, ErrSecretBounds)
	}
}

func TestKDFParametersWritten(t *testing.T) {
	params := DefaultKDFParams()
	if params != (KDFParams{MemoryKiB: 256 << 10, Iterations: 3, Parallelism: 4}) {
		t.Fatalf("DefaultKDFParams() = %+v", params)
	}
	if err := params.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}

	gate := testGate(t, 1)
	key := fixedKey()
	door, err := WrapKey(testContext, gate, key, testSecret, testObjectKey, DoorPassphrase, params)
	if err != nil {
		t.Fatalf("WrapKey() = %v", err)
	}
	if door.KDFID != kdfIDArgon2id || door.Params != params {
		t.Fatalf("WrapKey() door = %+v, want KDF %d and %+v", door, kdfIDArgon2id, params)
	}
	record, err := encodeDoorRecord(door)
	if err != nil {
		t.Fatalf("encodeDoorRecord() = %v", err)
	}
	if got := binary.BigEndian.Uint16(record[2:4]); got != doorBodySize {
		t.Fatalf("door body length = %d, want %d", got, doorBodySize)
	}
	if roundTripped := decodeDoorBody(door.Type, door.KDFID, record[4:]); roundTripped != door {
		t.Fatalf("decodeDoorBody() = %+v, want %+v", roundTripped, door)
	}

	cheap := mustDoor(t, gate, key, testSecret, DoorPassphrase)
	unwrapped, err := UnwrapKey(testContext, gate, cheap, testSecret, testObjectKey)
	if err != nil {
		t.Fatalf("UnwrapKey() = %v", err)
	}
	if unwrapped != key {
		t.Fatalf("UnwrapKey() = %x, want %x", unwrapped, key)
	}
	if _, err := UnwrapKey(testContext, gate, cheap, []byte("wrong secret"), testObjectKey); !errors.Is(err, ErrKeyRejected) {
		t.Fatalf("UnwrapKey(wrong secret) = %v, want %v", err, ErrKeyRejected)
	}

	for _, out := range []KDFParams{
		{MemoryKiB: minMemoryKiB - 1, Iterations: 1, Parallelism: 1},
		{MemoryKiB: minMemoryKiB, Iterations: maxIterations + 1, Parallelism: 1},
		{MemoryKiB: minMemoryKiB, Iterations: 1, Parallelism: maxParallelism + 1},
	} {
		if _, err := WrapKey(testContext, gate, key, testSecret, testObjectKey, DoorPassphrase, out); !errors.Is(err, ErrKDFParams) {
			t.Fatalf("WrapKey(%+v) = %v, want %v", out, err, ErrKDFParams)
		}
	}
	if _, err := WrapKey(testContext, gate, key, testSecret, testObjectKey, DoorType(3), params); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("WrapKey(unknown door type) = %v, want %v", err, ErrUnsupportedFormat)
	}
	if _, err := UnwrapKey(testContext, gate, Door{Type: DoorType(3)}, testSecret, testObjectKey); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("UnwrapKey(unknown door type) = %v, want %v", err, ErrUnsupportedFormat)
	}
	if _, err := UnwrapKey(testContext, gate, Door{Type: DoorPassphrase, KDFID: 9}, testSecret, testObjectKey); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("UnwrapKey(unknown KDF) = %v, want %v", err, ErrUnsupportedFormat)
	}
}
