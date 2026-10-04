// Package settings owns the scoped settings of slivingdoc: one strict,
// versioned JSON file under the user configuration directory that maps a
// directory to the settings of the notebook it holds
// (architecture/config.md). Today a remembered target is the hosted space
// and the prefix that identify it inside that space; the record names no
// credential, and the package reads and writes that one file only.
//
// The file is a registry of one record per directory, so further
// per-directory settings can be stored beside the space without a
// redesign.
package settings

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/baalimago/slivingdoc/internal/credentials"
)

// FileName is the settings file inside the configuration directory.
const FileName = "workspaces.json"

// LockName is the lock file beside FileName that serializes the
// read-modify-write of one directory's record.
const LockName = "workspaces.lock"

// FormatVersion is the only settings file version this build reads.
const FormatVersion = 1

// maxFileSize bounds how much of the file one read accepts and one write
// stores. It is the bound of the credentials file, so remembering a
// directory costs one read of the size slivingdoc already accepts.
const maxFileSize = credentials.MaxFileSize

var (
	// ErrNoConfigDir reports that neither credentials.DirEnv nor the
	// platform's user configuration directory resolves from the injected
	// environment.
	ErrNoConfigDir = errors.New("settings: no configuration directory")
	// ErrMalformed reports a settings file this build cannot read.
	ErrMalformed = errors.New("settings: malformed settings file")
	// ErrUnsupportedVersion reports a settings file of a version this
	// build does not read. It is never rewritten: an older file is
	// removed by the person, and a newer build's is kept and slivingdoc
	// updated instead.
	ErrUnsupportedVersion = errors.New("settings: unsupported settings file version")
	// ErrExposed reports a settings file, or its directory, that other
	// users could read or change.
	ErrExposed = errors.New("settings: the settings file is not private")
	// ErrTooLarge reports a settings file at or over the bound.
	ErrTooLarge = errors.New("settings: the settings file is too large")
)

// wording is this file's error vocabulary inside the hardened handling
// every slivingdoc configuration file shares.
var wording = credentials.Wording{
	Prefix:     "settings",
	NotPrivate: ErrExposed,
	Oversized:  ErrTooLarge,
	Holder:     "pull, commit, status or log command",
}

// Target is the remembered notebook of one directory: the hosted space
// and the prefix that identify it inside that space. It names no
// credential, and it is deliberately not called Storage, which belongs to
// internal/storage.
type Target struct {
	Space  string
	Prefix string
}

// Entry is one directory's remembered notebook.
type Entry struct {
	Path   string
	Target Target
}

// Set is the content of the settings file: one entry per directory, in the
// order the file lists them. The zero value is an empty file.
type Set struct {
	entries []Entry
}

// Entries returns a copy of the stored entries in file order.
func (s Set) Entries() []Entry { return slices.Clone(s.entries) }

// Lookup returns the target remembered for exactly path.
func (s Set) Lookup(path string) (Target, bool) {
	for _, e := range s.entries {
		if e.Path == path {
			return e.Target, true
		}
	}
	return Target{}, false
}

// Put returns the set with entry stored: an entry whose path is
// byte-identical is replaced where it stands, so a rewrite does not move
// it and another writer's entry survives, while a new path is appended.
func (s Set) Put(entry Entry) Set {
	out := Set{entries: slices.Clone(s.entries)}
	if i := slices.IndexFunc(out.entries, func(e Entry) bool { return e.Path == entry.Path }); i >= 0 {
		out.entries[i] = entry
		return out
	}
	out.entries = append(out.entries, entry)
	return out
}

// Config is what a caller may change about the store: the bound one read
// and one write accept, and the reader that answers instead of the file.
type Config struct {
	// MaxFileSize bounds one read. Zero is the package bound, the
	// credentials file's own.
	MaxFileSize int
	// Load reads the settings of every directory. nil reads the file.
	Load func() (Set, error)
}

// Read returns the settings of every directory, through the injected
// reader when there is one, and through the located file under the bound
// otherwise. getenv and goos locate the file, as credentials.Locate does.
func (c Config) Read(getenv func(string) string, goos string) (Set, error) {
	if c.Load != nil {
		return c.Load()
	}
	file, err := Locate(getenv, goos)
	if err != nil {
		return Set{}, err
	}
	return file.withBound(int64(c.MaxFileSize)).Load()
}

// File is the settings file of one configuration directory.
type File struct {
	dir string
	// private says whether Load checks POSIX owners and permission bits:
	// on a platform built with them, unless goos is Windows
	// (credentials.ChecksOwners).
	private bool
	// uid is the effective user that must own the file and its directory.
	uid int
	// max is the bound of one read and one write.
	max int64
}

// Path is the settings file path.
func (f File) Path() string { return filepath.Join(f.dir, FileName) }

// bounded is the bound one read and one write of f accept.
func (f File) bounded() int64 {
	if f.max <= 0 {
		return maxFileSize
	}
	return f.max
}

// withBound is f under a caller's own bound, where a bound of zero or
// less is the package bound.
func (f File) withBound(max int64) File {
	f.max = max
	return f
}

// hardened is this file inside the hardened configuration-file handling
// every slivingdoc configuration file shares (credentials.PrivateFile).
func (f File) hardened() credentials.PrivateFile {
	return credentials.PrivateFile{
		Path:     f.Path(),
		Dir:      f.dir,
		UID:      f.uid,
		Private:  f.private,
		MaxBytes: f.bounded(),
		Wording:  wording,
	}
}

// Load reads the settings file. A file that does not exist is an empty
// Set: no directory is remembered yet. Anything else that cannot be read
// or parsed strictly is an error naming the file, and a file of another
// version is never rewritten.
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

// Save writes s atomically: a temporary file in the same directory, mode
// 0600, synced, then renamed over the file, with the directory created
// 0700. A write over the bound is refused before anything is written.
func (f File) Save(s Set) error {
	data, err := encode(s)
	if err != nil {
		return fmt.Errorf("settings: encode: %w", err)
	}
	return f.hardened().Write(data)
}

// CheckDir returns ErrExposed when the directory exists and is not a
// directory, is owned by another user, or that group or other can write
// it; a missing directory is private. Load checks it only when the file
// exists, so a command that will write the file checks it first.
func (f File) CheckDir() error { return f.hardened().CheckDir() }

// Lock is a held settings lock. It is the credentials lock of its own, so
// the two never wait for each other: nothing takes both at once.
type Lock struct {
	lock *credentials.Lock
}

// Lock takes the lock beside the file, creating the directory 0700 when
// needed, so the load, the change and the save of one file cannot
// interleave with another's. It waits while another process holds it,
// until ctx ends.
func (f File) Lock(ctx context.Context) (*Lock, error) {
	if err := os.MkdirAll(f.dir, credentials.DirMode); err != nil {
		return nil, fmt.Errorf("settings: create %s: %w", f.dir, err)
	}
	if err := f.CheckDir(); err != nil {
		return nil, err
	}
	lock, err := f.hardened().Lock(ctx, filepath.Join(f.dir, LockName))
	if err != nil {
		return nil, err
	}
	return &Lock{lock: lock}, nil
}

// Unlock releases the lock.
func (l *Lock) Unlock() error { return l.lock.Unlock() }
