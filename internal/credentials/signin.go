package credentials

// The shared first-use sign-in of serve processes (architecture/login.md,
// Sign in on first use). Several servers started together without a login
// run one device login between them: the one holding SignInLockName runs
// it and writes the approval page to SignInName, and the others show that
// page until the credentials file holds the login.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

// SignInLockName is the lock file whose holder runs the shared sign-in.
const SignInLockName = "signin.lock"

// SignInName is the record of the approval the sign-in waits for.
const SignInName = "signin.json"

// signInVersion is the one version of the sign-in record.
const signInVersion = 1

// maxSignIn bounds the sign-in record.
const maxSignIn = 4 << 10

// ErrSignInHeld reports that another process holds the sign-in lock.
var ErrSignInHeld = errors.New("credentials: another slivingdoc process is signing in")

// SignIn is the approval a running sign-in waits for: the page to open,
// the code to compare on it, and when the code expires. It holds no secret:
// the device code stays in the process that polls.
type SignIn struct {
	Page    string
	Code    string
	Expires time.Time
}

type signInRecord struct {
	Version   int    `json:"version"`
	Page      string `json:"page"`
	Code      string `json:"code"`
	ExpiresAt string `json:"expiresAt"`
}

// SignInLock is the held sign-in lock.
type SignInLock struct {
	fl   *flock.Flock
	file File
}

// TrySignIn takes the sign-in lock without waiting, creating the directory
// 0700 when needed. It returns ErrSignInHeld while another process holds
// it; the kernel releases a holder's lock when that process ends, so a
// crashed sign-in never blocks the next one.
func (f File) TrySignIn() (*SignInLock, error) {
	if err := os.MkdirAll(f.dir, DirMode); err != nil {
		return nil, fmt.Errorf("credentials: create %s: %w", f.dir, err)
	}
	if err := f.CheckDir(); err != nil {
		return nil, err
	}
	path := filepath.Join(f.dir, SignInLockName)
	fl := flock.New(path, flock.SetPermissions(FileMode))
	locked, err := fl.TryLock()
	if err != nil {
		return nil, fmt.Errorf("credentials: lock %s: %w", path, err)
	}
	if !locked {
		return nil, ErrSignInHeld
	}
	return &SignInLock{fl: fl, file: f}, nil
}

// Publish records the approval the holder waits for, so the processes that
// wait on it can show the same page.
func (l *SignInLock) Publish(s SignIn) error {
	data, err := json.Marshal(signInRecord{
		Version: signInVersion, Page: s.Page, Code: s.Code, ExpiresAt: s.Expires.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("credentials: encode the sign-in record: %w", err)
	}
	return l.file.signIn().Write(append(data, '\n'))
}

// Release removes the record and releases the lock.
func (l *SignInLock) Release() error {
	rmErr := os.Remove(l.file.signIn().Path)
	if errors.Is(rmErr, os.ErrNotExist) {
		rmErr = nil
	}
	if err := l.fl.Unlock(); err != nil {
		return fmt.Errorf("credentials: unlock %s: %w", l.fl.Path(), err)
	}
	return rmErr
}

// PendingSignIn reads the approval another process's sign-in waits for.
// found is false when no sign-in is recorded, or the recorded code has
// expired at now. The file is read like the credentials file, so one
// another user could write is refused.
func (f File) PendingSignIn(now time.Time) (s SignIn, found bool, err error) {
	data, found, err := f.signIn().Read()
	if err != nil || !found {
		return SignIn{}, false, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var rec signInRecord
	if err := dec.Decode(&rec); err != nil {
		return SignIn{}, false, fmt.Errorf("%w %s: %w", ErrMalformed, f.signIn().Path, err)
	}
	expires, err := time.Parse(time.RFC3339, rec.ExpiresAt)
	if err != nil || rec.Version != signInVersion || !strings.HasPrefix(rec.Page, "https://") && !strings.HasPrefix(rec.Page, "http://") || !printableASCII(rec.Page) || !printableASCII(rec.Code) {
		return SignIn{}, false, fmt.Errorf("%w %s: not a sign-in record this slivingdoc reads", ErrMalformed, f.signIn().Path)
	}
	if !now.Before(expires) {
		return SignIn{}, false, nil
	}
	return SignIn{Page: rec.Page, Code: rec.Code, Expires: expires}, true, nil
}

// signIn is the sign-in record inside the hardened file handling.
func (f File) signIn() PrivateFile {
	p := f.hardened()
	p.Path = filepath.Join(f.dir, SignInName)
	p.MaxBytes = maxSignIn
	return p
}

func printableASCII(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
}
