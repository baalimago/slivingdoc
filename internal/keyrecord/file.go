package keyrecord

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/baalimago/slivingdoc/internal/credentials"
)

// LockName is the lock file beside FileName that serializes the
// read-modify-write of one notebook's key material.
const LockName = "notebooks.lock"

// maxFileSize bounds how much of the file one read accepts and one write
// stores. It is the bound of every slivingdoc configuration file
// (internal/settings).
const maxFileSize = credentials.MaxFileSize

// wording is this file's error vocabulary inside the hardened handling every
// slivingdoc configuration file shares (credentials.PrivateFile).
var wording = credentials.Wording{
	Prefix:     "keyrecord",
	NotPrivate: ErrExposed,
	Oversized:  ErrTooLarge,
	Holder:     "encrypt command",
}

// File is the key record file of one configuration directory.
type File struct {
	dir string
	// private says whether Load checks POSIX owners and permission bits: on a
	// platform built with them, unless goos is Windows (ChecksOwners).
	private bool
	// uid is the effective user that must own the file and its directory.
	uid int
}

// Locate resolves the key record file by the credentials file's own rule:
// credentials.DirEnv when set, else the platform's user configuration
// directory plus "slivingdoc". getenv and goos are the process environment and
// operating system, injected so a test never reads the developer's own file.
func Locate(getenv func(string) string, goos string) (File, error) {
	dir, uid, err := credentials.ConfigDir(getenv, goos)
	if err != nil {
		return File{}, credentials.NoDirectory(ErrNoConfigDir, err)
	}
	return File{dir: dir, private: credentials.ChecksOwners(goos), uid: uid}, nil
}

// Path is the key record file path.
func (f File) Path() string { return filepath.Join(f.dir, FileName) }

// hardened is this file inside the hardened configuration-file handling every
// slivingdoc configuration file shares (credentials.PrivateFile).
func (f File) hardened() credentials.PrivateFile {
	return credentials.PrivateFile{
		Path:     f.Path(),
		Dir:      f.dir,
		UID:      f.uid,
		Private:  f.private,
		MaxBytes: maxFileSize,
		Wording:  wording,
	}
}

// Load reads the key record file. A file that does not exist is an empty Set:
// no notebook stores key material yet. Anything else that cannot be read or
// parsed strictly is an error naming the file, and a file of another version
// is never rewritten.
func (f File) Load() (Set, error) {
	data, found, err := f.hardened().Read()
	if err != nil {
		return Set{}, err
	}
	if !found {
		return Set{}, nil
	}
	set, err := decode(data)
	var version *versionError
	if errors.As(err, &version) {
		return Set{}, version.withFix(f.Path())
	}
	if err != nil {
		return Set{}, fmt.Errorf("%w %s: %w", ErrMalformed, f.Path(), err)
	}
	return set, nil
}

// Save writes s atomically: a temporary file in the same directory, mode 0600,
// synced, then renamed over the file, with the directory created 0700. A write
// over the bound is refused before anything is written.
func (f File) Save(s Set) error {
	data, err := encode(s)
	if err != nil {
		return fmt.Errorf("keyrecord: encode: %w", err)
	}
	return f.hardened().Write(data)
}

// CheckDir returns ErrExposed when the directory exists and is not a
// directory, is owned by another user, or that group or other can write it; a
// missing directory is private. Load checks it only when the file exists, so a
// command that will write the file checks it first.
func (f File) CheckDir() error { return f.hardened().CheckDir() }

// Lock takes the lock beside the file, creating the directory 0700 when
// needed, so the load, the change and the save of one file cannot interleave
// with another's. It waits while another process holds it, until ctx ends.
func (f File) Lock(ctx context.Context) (*credentials.Lock, error) {
	if err := os.MkdirAll(f.dir, credentials.DirMode); err != nil {
		return nil, fmt.Errorf("keyrecord: create %s: %w", f.dir, err)
	}
	if err := f.CheckDir(); err != nil {
		return nil, err
	}
	return f.hardened().Lock(ctx, filepath.Join(f.dir, LockName))
}

// Update stores e under the file lock, so a caller's explicit request replaces
// only its own notebook identity's record and another writer's entry survives.
func (f File) Update(ctx context.Context, e Entry) error {
	lock, err := f.Lock(ctx)
	if err != nil {
		return err
	}
	defer lock.Unlock()
	set, err := f.Load()
	if err != nil {
		return err
	}
	return f.Save(set.Put(e))
}

// Forget removes the entry for digest under the file lock. A missing entry is
// not an error: the notebook simply stores no key material.
func (f File) Forget(ctx context.Context, digest string) error {
	lock, err := f.Lock(ctx)
	if err != nil {
		return err
	}
	defer lock.Unlock()
	set, err := f.Load()
	if err != nil {
		return err
	}
	return f.Save(set.Remove(digest))
}
