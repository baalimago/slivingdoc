package credentials

// The hardened handling of one plain private file of the configuration
// directory. Every slivingdoc configuration file goes through it:
// credentials.json (internal/credentials) and workspaces.json
// (internal/settings, architecture/config.md) share one convention
// rather than keeping two.
//
// Nothing here knows a schema or a package name. Load and Save own
// those, and hand their refusals the vocabulary of the package the
// file belongs to (Wording).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

// The file and directory modes: a configuration file is readable by
// its owner only, and nobody but its owner may change the directory
// that holds it.
const (
	FileMode os.FileMode = 0o600
	DirMode  os.FileMode = 0o700
)

// Wording is the error vocabulary of the package that owns a
// configuration file: Prefix leads every message of this file's
// handling, NotPrivate is the error a file another user could read or
// change wraps, Oversized is the error a file over its bound wraps, and
// Holder names the commands that take the file's lock.
type Wording struct {
	Prefix     string
	NotPrivate error
	Oversized  error
	Holder     string
}

// PrivateFile is one plain private file of the configuration
// directory. Its schema and its wording belong to the package that
// owns it; the filesystem handling belongs here.
type PrivateFile struct {
	// Path is the file, and Dir the directory holding it, which a write
	// creates at DirMode when it does not exist.
	Path string
	Dir  string
	// UID is the effective user that must own both.
	UID int
	// Private says whether POSIX owners and permission bits are checked:
	// the build has them, and goos is not Windows (ChecksOwners).
	Private bool
	// MaxBytes bounds one read and one write.
	MaxBytes int64
	// Wording is the error vocabulary of the package owning the file.
	Wording Wording
}

// Read returns the file's bytes, at most MaxBytes of them. found is
// false when the file does not exist, which is not an error: nobody
// has stored anything yet.
//
// Like ssh, Read refuses a symbolic link, anything but a regular file,
// and on every platform but Windows a file another user owns or that
// group or other can read or write, and a directory another user owns
// or that group or other can write: another user could read the keys
// or plant their own. The file is opened without following a final
// symbolic link where the platform can, and checked through the open
// descriptor.
func (p PrivateFile) Read() (data []byte, found bool, err error) {
	link, err := os.Lstat(p.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, p.fail("read", p.Path, err)
	}
	if link.Mode()&fs.ModeSymlink != 0 {
		return nil, false, p.notPrivate("%s is a symbolic link; replace it with the file itself", p.Path)
	}
	file, err := os.OpenFile(p.Path, openFlags, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, p.fail("open", p.Path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, false, p.fail("stat", p.Path, err)
	}
	if err := p.CheckDir(); err != nil {
		return nil, false, err
	}
	if err := p.checkFile(info, func() (int, error) { return fileOwner(file) }); err != nil {
		return nil, false, err
	}
	data, err = io.ReadAll(io.LimitReader(file, p.MaxBytes+1))
	if err != nil {
		return nil, false, p.fail("read", p.Path, err)
	}
	if int64(len(data)) > p.MaxBytes {
		return nil, false, p.oversized()
	}
	return data, true, nil
}

// CheckDir returns the not-private refusal when the directory exists
// and is not a directory, is owned by another user, or that group or
// other can write it; a missing directory is private. A read checks
// the directory only when the file exists, so a command that will
// write the file checks it first.
func (p PrivateFile) CheckDir() error {
	info, err := os.Stat(p.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return p.fail("stat", p.Dir, err)
	}
	if !info.IsDir() {
		return p.notPrivate("%s is not a directory", p.Dir)
	}
	if !p.Private {
		return nil
	}
	if err := p.checkOwner(func() (int, error) { return pathOwner(p.Dir) }, p.Dir); err != nil {
		return err
	}
	if info.Mode().Perm()&0o022 != 0 {
		return p.notPrivate("other users can write to %s; run 'chmod go-w %s'", p.Dir, p.Dir)
	}
	return nil
}

// Write stores data as the file: a temporary file in the same
// directory, mode FileMode, synced, then renamed over the file, with
// the directory created at DirMode when it does not exist. Data over
// MaxBytes is refused before anything is written, and no temporary
// file outlives a failure.
func (p PrivateFile) Write(data []byte) error {
	if int64(len(data)) > p.MaxBytes {
		return p.oversized()
	}
	if err := os.MkdirAll(p.Dir, DirMode); err != nil {
		return p.fail("create", p.Dir, err)
	}
	if err := p.CheckDir(); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(p.Dir, ".private-*.tmp")
	if err != nil {
		return p.fail("create temporary file", p.Dir, err)
	}
	name := tmp.Name()
	if err := writeSynced(tmp, data); err != nil {
		os.Remove(name)
		return p.fail("write temporary file", name, err)
	}
	if err := os.Rename(name, p.Path); err != nil {
		os.Remove(name)
		return p.fail("replace", p.Path, err)
	}
	return nil
}

// Lock is a held configuration-file lock.
type Lock struct {
	fl      *flock.Flock
	wording Wording
}

// Lock takes the advisory lock at path, mode FileMode, so that a read,
// a change and a write of one file cannot interleave with another's.
// It waits while another process holds it, until ctx ends. Creating
// and checking the directory holding the lock file belongs to the
// caller.
func (p PrivateFile) Lock(ctx context.Context, path string) (*Lock, error) {
	fl := flock.New(path, flock.SetPermissions(FileMode))
	locked, err := fl.TryLockContext(ctx, lockRetry)
	if err != nil {
		return nil, p.fail("lock", path, err)
	}
	if !locked {
		return nil, fmt.Errorf("%s: lock %s: another %s holds it", p.Wording.Prefix, path, p.Wording.Holder)
	}
	return &Lock{fl: fl, wording: p.Wording}, nil
}

// Unlock releases the lock.
func (l *Lock) Unlock() error {
	if err := l.fl.Unlock(); err != nil {
		return fmt.Errorf("%s: unlock %s: %w", l.wording.Prefix, l.fl.Path(), err)
	}
	return nil
}

// checkFile refuses an opened file that is not a regular file,
// another user's, or accessible to group or other.
func (p PrivateFile) checkFile(info fs.FileInfo, ownerOf func() (int, error)) error {
	if !info.Mode().IsRegular() {
		return p.notPrivate("%s is not a regular file", p.Path)
	}
	if !p.Private {
		return nil
	}
	if err := p.checkOwner(ownerOf, p.Path); err != nil {
		return err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return p.notPrivate("other users can access %s; run 'chmod %o %s'", p.Path, FileMode, p.Path)
	}
	return nil
}

func (p PrivateFile) checkOwner(ownerOf func() (int, error), path string) error {
	owner, err := ownerOf()
	if err != nil {
		return p.fail("owner of", path, err)
	}
	if owner != p.UID {
		return p.notPrivate("%s is owned by user %d, not by you (user %d)", path, owner, p.UID)
	}
	return nil
}

func (p PrivateFile) notPrivate(format string, args ...any) error {
	return fmt.Errorf("%w: %s", p.Wording.NotPrivate, fmt.Sprintf(format, args...))
}

func (p PrivateFile) oversized() error {
	return fmt.Errorf("%w %s: larger than %d bytes", p.Wording.Oversized, p.Path, p.MaxBytes)
}

func (p PrivateFile) fail(op, path string, err error) error {
	return fmt.Errorf("%s: %s %s: %w", p.Wording.Prefix, op, path, err)
}

// writeSynced gives the temporary file its mode, writes it, flushes
// it to the device, and closes it.
func writeSynced(tmp *os.File, data []byte) error {
	if err := tmp.Chmod(FileMode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	return tmp.Close()
}

// ConfigDir resolves the directory the slivingdoc configuration files
// live in: DirEnv when it is set and absolute, else the platform's user
// configuration directory plus "slivingdoc". It also answers the
// effective user whose files that directory holds, so a package with a
// file of its own locates it by this rule (internal/settings,
// architecture/config.md). A directory that does not resolve is a
// NoConfigDir naming the cause, which NoDirectory words in the
// caller's own vocabulary.
func ConfigDir(getenv func(string) string, goos string) (dir string, uid int, err error) {
	if set := getenv(DirEnv); set != "" {
		if !filepath.IsAbs(set) {
			return "", 0, NoConfigDir{Reason: DirEnv + " must be an absolute path"}
		}
		return filepath.Clean(set), os.Geteuid(), nil
	}
	base, err := userConfigDir(getenv, goos)
	if err != nil {
		return "", 0, err
	}
	return filepath.Join(base, "slivingdoc"), os.Geteuid(), nil
}

// NoConfigDir reports why the configuration directory does not
// resolve: DirEnv is set but not absolute, or neither DirEnv nor the
// platform's user configuration directory resolves from the injected
// environment. It names no package, so each file words it in its own
// vocabulary (NoDirectory).
type NoConfigDir struct {
	Reason string
}

func (e NoConfigDir) Error() string { return "no configuration directory: " + e.Reason }

// NoDirectory words err from ConfigDir in the caller's own
// no-configuration-directory error.
func NoDirectory(missing error, err error) error {
	var cause NoConfigDir
	if errors.As(err, &cause) {
		return fmt.Errorf("%w: %s", missing, cause.Reason)
	}
	return fmt.Errorf("%w: %w", missing, err)
}

// ChecksOwners reports whether a file for goos checks POSIX owners
// and permission bits: the build has them, and goos is not Windows,
// whose ACLs they do not describe.
func ChecksOwners(goos string) bool { return hasOwners && goos != "windows" }
