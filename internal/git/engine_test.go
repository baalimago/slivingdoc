package git

import (
	"errors"
	"strings"
	"testing"
)

func TestOIDString(t *testing.T) {
	var zero OID
	if got, want := zero.String(), strings.Repeat("0", 40); got != want {
		t.Fatalf("zero OID String() = %q, want %q", got, want)
	}

	var oid OID
	for i := range oid {
		oid[i] = byte(i)
	}
	// Bytes 0x00..0x13 become hex 000102...1213.
	want := "000102030405060708090a0b0c0d0e0f10111213"
	if got := oid.String(); got != want {
		t.Fatalf("OID String() = %q, want %q", got, want)
	}
}

func TestFeaturesFromMask(t *testing.T) {
	// The pinned build reports threads, nsec, http-parser, regex, i18n,
	// compression, and sha1; transports stay disabled.
	f := FeaturesFromMask(1<<0 | 1<<3 | 1<<4 | 1<<5 | 1<<6 | 1<<9 | 1<<10)
	if !f.Threads || !f.NSEC || !f.HTTPParser || !f.Regex || !f.I18N || !f.Compression || !f.SHA1 {
		t.Fatalf("expected base features enabled, got %+v", f)
	}
	if f.HTTPS || f.SSH || f.AuthNTLM || f.AuthNegotiate || f.SHA256 {
		t.Fatalf("expected transport and SHA-256 features disabled, got %+v", f)
	}

	if f := FeaturesFromMask(0); f != (Features{}) {
		t.Fatalf("empty mask = %+v, want zero Features", f)
	}
}

func TestFeaturesString(t *testing.T) {
	f := Features{Threads: true, NSEC: true}
	if got, want := f.String(), "threads, nsec"; got != want {
		t.Fatalf("Features.String() = %q, want %q", got, want)
	}
	if got, want := (Features{}).String(), ""; got != want {
		t.Fatalf("empty Features.String() = %q, want %q", got, want)
	}
}

func TestErrorMessages(t *testing.T) {
	me := &VersionMismatchError{Pinned: "1.9.6", Runtime: "2.0.0"}
	if got, want := me.Error(), "git: runtime libgit2 2.0.0 does not match pinned 1.9.6"; got != want {
		t.Fatalf("VersionMismatchError.Error() = %q, want %q", got, want)
	}

	ne := &NativeError{Op: "read blob", Class: 3, Message: "object not found"}
	if got, want := ne.Error(), "git: read blob: object not found"; got != want {
		t.Fatalf("NativeError.Error() = %q, want %q", got, want)
	}
	if ne.Op != "read blob" || ne.Class != 3 {
		t.Fatalf("NativeError lost operation or native detail: %+v", ne)
	}

	bare := &NativeError{Op: "open libgit2"}
	if got, want := bare.Error(), "git: open libgit2 failed"; got != want {
		t.Fatalf("bare NativeError.Error() = %q, want %q", got, want)
	}
}

func TestErrUnavailable(t *testing.T) {
	if !errors.Is(ErrUnavailable, ErrUnavailable) {
		t.Fatal("ErrUnavailable must identify itself")
	}
	if ErrUnavailable.Error() == "" {
		t.Fatal("ErrUnavailable must carry a message")
	}
}

// fakeEngine proves that the seam compiles and records calls without any
// native code. Higher packages mirror this mock per the duplication policy.
type fakeEngine struct {
	opened   bool
	openErr  error
	closed   bool
	version  string
	features Features
}

func (f *fakeEngine) Open() error {
	if f.openErr != nil {
		return f.openErr
	}
	f.opened = true
	return nil
}

func (f *fakeEngine) Close() error {
	f.closed = true
	f.opened = false
	return nil
}

func (f *fakeEngine) Version() (string, error) { return f.version, nil }

func (f *fakeEngine) Features() (Features, error) { return f.features, nil }

func (f *fakeEngine) CreateRepo(string) (Repository, error) { return nil, ErrUnavailable }

func (f *fakeEngine) OpenRepo(string) (Repository, error) { return nil, ErrUnavailable }

func TestFakeEngineSatisfiesSeam(t *testing.T) {
	var _ Engine = (*fakeEngine)(nil)
	f := &fakeEngine{version: "1.9.6", features: Features{Threads: true}}
	if err := f.Open(); err != nil {
		t.Fatalf("Open() = %v", err)
	}
	if !f.opened {
		t.Fatal("fake did not record Open")
	}
	if v, err := f.Version(); err != nil || v != "1.9.6" {
		t.Fatalf("Version() = %q, %v", v, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	if !f.closed {
		t.Fatal("fake did not record Close")
	}
}
