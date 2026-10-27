package credentials

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestSignInLock proves one process at a time holds the sign-in lock, that
// the holder's published approval is what the others read until it
// expires, and that releasing removes the record.
func TestSignInLock(t *testing.T) {
	t.Parallel()
	file, err := Locate(lookup(map[string]string{DirEnv: filepath.Join(t.TempDir(), "cfg")}), runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	if _, found, err := file.PendingSignIn(now); err != nil || found {
		t.Fatalf("PendingSignIn() before any sign-in = %v, %v; want none", found, err)
	}
	lock, err := file.TrySignIn()
	if err != nil {
		t.Fatalf("TrySignIn() = %v", err)
	}
	if _, err := file.TrySignIn(); !errors.Is(err, ErrSignInHeld) {
		t.Fatalf("second TrySignIn() = %v, want ErrSignInHeld", err)
	}
	want := SignIn{Page: "https://www.slivingdoc.dev/cli/login#BCDF-GHJK", Code: "BCDF-GHJK", Expires: now.Add(10 * time.Minute)}
	if err := lock.Publish(want); err != nil {
		t.Fatalf("Publish() = %v", err)
	}
	if got, found, err := file.PendingSignIn(now); err != nil || !found || got != want {
		t.Fatalf("PendingSignIn() = %+v, %v, %v; want %+v", got, found, err, want)
	}
	if _, found, err := file.PendingSignIn(want.Expires); err != nil || found {
		t.Fatalf("PendingSignIn() at the expiry = %v, %v; want none", found, err)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(filepath.Join(file.dir, SignInName)); err != nil || info.Mode().Perm() != FileMode {
			t.Fatalf("sign-in record mode = %v, %v; want %v", info.Mode().Perm(), err, FileMode)
		}
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release() = %v", err)
	}
	if _, found, err := file.PendingSignIn(now); err != nil || found {
		t.Fatalf("PendingSignIn() after the release = %v, %v; want none", found, err)
	}
	again, err := file.TrySignIn()
	if err != nil {
		t.Fatalf("TrySignIn() after the release = %v", err)
	}
	if err := again.Release(); err != nil {
		t.Fatal(err)
	}
}

// TestPendingSignInRefusesMalformed proves a record this slivingdoc did not
// write is refused rather than shown to an agent.
func TestPendingSignInRefusesMalformed(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"version":1,"page":"https://x/cli/login","code":"A B","expiresAt":"2026-10-27T12:10:00Z"}`,
		`{"version":1,"page":"javascript:alert(1)","code":"BCDF-GHJK","expiresAt":"2026-10-27T12:10:00Z"}`,
		`{"version":2,"page":"https://x/cli/login","code":"BCDF-GHJK","expiresAt":"2026-10-27T12:10:00Z"}`,
		`{"version":1,"page":"https://x/cli/login","code":"BCDF-GHJK","expiresAt":"soon"}`,
		`{"version":1,"page":"https://x/cli/login","code":"BCDF-GHJK","expiresAt":"2026-10-27T12:10:00Z","extra":1}`,
	} {
		dir := filepath.Join(t.TempDir(), "cfg")
		if err := os.MkdirAll(dir, DirMode); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, SignInName), []byte(body), FileMode); err != nil {
			t.Fatal(err)
		}
		file, err := Locate(lookup(map[string]string{DirEnv: dir}), runtime.GOOS)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := file.PendingSignIn(time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)); !errors.Is(err, ErrMalformed) {
			t.Fatalf("PendingSignIn(%s) = %v, want ErrMalformed", body, err)
		}
	}
}
