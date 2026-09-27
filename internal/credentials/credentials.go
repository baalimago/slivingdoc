// Package credentials owns the stored logins of `slivingdoc login`
// (architecture/login.md): one strict, versioned JSON file under the user
// configuration directory, holding one hosted API token per (endpoint,
// space) and the default login. The package reads and writes that file
// only; it never sends a token anywhere.
package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gofrs/flock"

	"github.com/baalimago/slivingdoc/internal/httpstore"
	"github.com/baalimago/slivingdoc/internal/strictjson"
)

// FileName is the credentials file inside the configuration directory.
const FileName = "credentials.json"

// DirEnv overrides the configuration directory that holds FileName.
const DirEnv = "SLIVINGDOC_CONFIG_DIR"

// FormatVersion is the only credentials file version this build reads.
const FormatVersion = 1

// LockName is the lock file beside FileName that serializes the
// read-modify-write of a login or logout.
const LockName = "credentials.lock"

// maxFileSize bounds how much of the credentials file Load reads.
const maxFileSize = 1 << 20

// lockRetry is how often Lock tries again while another process holds it.
const lockRetry = 20 * time.Millisecond

// The file and directory modes: the token is readable by its owner only.
const (
	fileMode os.FileMode = 0o600
	dirMode  os.FileMode = 0o700
)

var (
	// ErrNoConfigDir reports that neither DirEnv nor the platform's user
	// configuration directory resolves from the environment.
	ErrNoConfigDir = errors.New("credentials: no configuration directory")
	// ErrMalformed reports a credentials file this build cannot read.
	ErrMalformed = errors.New("credentials: malformed credentials file")
	// ErrNoLogin reports that no stored login matches the request.
	ErrNoLogin = errors.New("credentials: no stored login")
	// ErrNoDefault reports that no default login is stored.
	ErrNoDefault = errors.New("credentials: no default login")
	// ErrAmbiguous reports that one space has stored logins for several
	// endpoints and nothing chooses between them.
	ErrAmbiguous = errors.New("credentials: several stored logins match")
	// ErrExpired reports a stored login whose token has expired.
	ErrExpired = errors.New("credentials: stored login expired")
	// ErrExposed reports a credentials file, or its directory, that other
	// users could read or change.
	ErrExposed = errors.New("credentials: the credentials file is not private")
)

// Access is what a stored token may do in its space.
type Access string

// The two access levels the site issues.
const (
	AccessWrite Access = "write"
	AccessRead  Access = "read"
)

// Describe is the human wording of the access level.
func (a Access) Describe() string {
	if a == AccessRead {
		return "read only"
	}
	return "read and write"
}

// ParseAccess accepts exactly the two access tokens of the wire contract.
func ParseAccess(s string) (Access, error) {
	switch Access(s) {
	case AccessWrite, AccessRead:
		return Access(s), nil
	default:
		return "", fmt.Errorf("access %q is neither %q nor %q", s, AccessWrite, AccessRead)
	}
}

// Key names one stored login: the normalized hosted API endpoint the token
// was issued for and the space it is granted.
type Key struct {
	Endpoint string
	Space    string
}

// Expiry is the end of a token's life; the zero value never expires.
type Expiry struct {
	at time.Time
}

// ExpiresAt returns the expiry at t.
func ExpiresAt(t time.Time) Expiry { return Expiry{at: t.UTC()} }

// Never reports whether the token has no expiry.
func (e Expiry) Never() bool { return e.at.IsZero() }

// Time is the expiry instant; the zero time when Never.
func (e Expiry) Time() time.Time { return e.at }

// Passed reports whether the expiry is at or before now.
func (e Expiry) Passed(now time.Time) bool { return !e.Never() && !now.Before(e.at) }

// Describe is the human wording of the expiry: "until <date>" or "with no
// expiry".
func (e Expiry) Describe() string {
	if e.Never() {
		return "with no expiry"
	}
	return "until " + e.at.Format("2006-01-02 15:04 UTC")
}

// Login is one stored hosted API token.
type Login struct {
	Key
	// Site is the origin that issued the token; logout revokes it there.
	Site    string
	Token   string
	Access  Access
	Expires Expiry
	// Account is the email of the person who approved the login and Owner
	// the email of the space's owner, as the site reported them; either
	// may be empty in a file written before the site reported them.
	Account string
	Owner   string
}

// Usable returns ErrExpired when the login's token has expired at now.
func (l Login) Usable(now time.Time) error {
	if l.Expires.Passed(now) {
		return fmt.Errorf("%w: space %q at %s expired %s", ErrExpired, l.Space, l.Endpoint,
			strings.TrimPrefix(l.Expires.Describe(), "until "))
	}
	return nil
}

// Set is the content of the credentials file: every stored login and the
// default one. The zero value is an empty file.
type Set struct {
	logins   []Login
	def      Key
	hasDef   bool
	location string
}

// Logins returns a copy of the stored logins in file order.
func (s Set) Logins() []Login { return slices.Clone(s.logins) }

// Default returns the default login: the first one stored, or the one a
// login stored with --default.
func (s Set) Default() (Login, error) {
	if !s.hasDef {
		return Login{}, fmt.Errorf("%w in %s", ErrNoDefault, s.where())
	}
	return s.Lookup(s.def)
}

// Lookup returns the login stored for exactly k.
func (s Set) Lookup(k Key) (Login, error) {
	for _, l := range s.logins {
		if l.Key == k {
			return l, nil
		}
	}
	return Login{}, fmt.Errorf("%w for space %q at %s in %s", ErrNoLogin, k.Space, k.Endpoint, s.where())
}

// ForSpace returns the one login a process uses for space when no endpoint
// is configured: the only login stored for space. Several logins for space
// at different endpoints are ErrAmbiguous, even when the default names one
// of them: only an endpoint chooses between them.
func (s Set) ForSpace(space string) (Login, error) {
	matches := s.Space(space)
	switch len(matches) {
	case 0:
		return Login{}, fmt.Errorf("%w for space %q in %s", ErrNoLogin, space, s.where())
	case 1:
		return matches[0], nil
	default:
		endpoints := make([]string, 0, len(matches))
		for _, m := range matches {
			endpoints = append(endpoints, m.Endpoint)
		}
		return Login{}, fmt.Errorf("%w: space %q has logins for %s", ErrAmbiguous, space, strings.Join(endpoints, ", "))
	}
}

// Space returns every login stored for space, whatever its endpoint.
func (s Set) Space(space string) []Login {
	var out []Login
	for _, l := range s.logins {
		if l.Space == space {
			out = append(out, l)
		}
	}
	return out
}

// Put stores l, replacing a login for the same key; the default is left
// alone (SetDefault). It returns the login it replaced, or ErrNoLogin when
// the key was new.
func (s *Set) Put(l Login) (Login, error) {
	for i, old := range s.logins {
		if old.Key == l.Key {
			s.logins[i] = l
			return old, nil
		}
	}
	s.logins = append(s.logins, l)
	return Login{}, fmt.Errorf("%w for space %q at %s", ErrNoLogin, l.Space, l.Endpoint)
}

// SetDefault makes the login stored for k the default; a missing key is
// ErrNoLogin.
func (s *Set) SetDefault(k Key) error {
	if _, err := s.Lookup(k); err != nil {
		return err
	}
	s.def, s.hasDef = k, true
	return nil
}

// Remove deletes the login stored for k and clears the default when it
// was k. A missing key is ErrNoLogin.
func (s *Set) Remove(k Key) error {
	i := slices.IndexFunc(s.logins, func(l Login) bool { return l.Key == k })
	if i < 0 {
		return fmt.Errorf("%w for space %q at %s in %s", ErrNoLogin, k.Space, k.Endpoint, s.where())
	}
	s.logins = slices.Delete(s.logins, i, i+1)
	if s.hasDef && s.def == k {
		s.def, s.hasDef = Key{}, false
	}
	return nil
}

func (s Set) where() string {
	if s.location == "" {
		return "the credentials file"
	}
	return s.location
}

// File is the credentials file of one configuration directory.
type File struct {
	dir string
	// private says whether Load checks POSIX owners and permission bits:
	// on a platform built with them (hasOwners), unless goos is Windows.
	private bool
	// uid is the effective user that must own the file and its directory.
	uid int
}

// checksOwners is whether a File for goos checks owners and modes: the
// build has them, and goos is not Windows, whose ACLs they do not describe.
func checksOwners(goos string) bool { return hasOwners && goos != "windows" }

// Locate resolves the credentials file from the environment: DirEnv when
// set, else <user configuration directory>/slivingdoc. getenv and goos are
// the process environment and operating system, injected so a test never
// reads the developer's own file.
func Locate(getenv func(string) string, goos string) (File, error) {
	if dir := getenv(DirEnv); dir != "" {
		if !filepath.IsAbs(dir) {
			return File{}, fmt.Errorf("%w: %s must be an absolute path", ErrNoConfigDir, DirEnv)
		}
		return File{dir: filepath.Clean(dir), private: checksOwners(goos), uid: os.Geteuid()}, nil
	}
	base, err := userConfigDir(getenv, goos)
	if err != nil {
		return File{}, err
	}
	return File{dir: filepath.Join(base, "slivingdoc"), private: checksOwners(goos), uid: os.Geteuid()}, nil
}

// Path is the credentials file path.
func (f File) Path() string { return filepath.Join(f.dir, FileName) }

// userConfigDir mirrors os.UserConfigDir over the injected environment.
func userConfigDir(getenv func(string) string, goos string) (string, error) {
	var dir string
	switch goos {
	case "windows":
		dir = getenv("AppData")
	case "darwin", "ios":
		if home := getenv("HOME"); home != "" {
			dir = filepath.Join(home, "Library", "Application Support")
		}
	case "plan9":
		if home := getenv("home"); home != "" {
			dir = filepath.Join(home, "lib")
		}
	default:
		dir = getenv("XDG_CONFIG_HOME")
		if dir != "" && !filepath.IsAbs(dir) {
			dir = ""
		}
		if dir == "" {
			if home := getenv("HOME"); home != "" {
				dir = filepath.Join(home, ".config")
			}
		}
	}
	if dir == "" || !filepath.IsAbs(dir) {
		return "", fmt.Errorf("%w: set %s or HOME", ErrNoConfigDir, DirEnv)
	}
	return dir, nil
}

// Load reads the credentials file. A file that does not exist is an empty
// Set: nobody has logged in yet. Anything else that cannot be read or
// parsed strictly is an error naming the file.
//
// Like ssh, Load refuses a symbolic link, anything but a regular file, and
// on every platform but Windows a file another user owns or that group or
// other can read or write, or a directory another user owns or that group
// or other can write: another user could read the tokens or plant their
// own. The file is opened without following a final symbolic link where
// the platform can, checked through the open descriptor, and read up to
// 1 MiB.
func (f File) Load() (Set, error) {
	link, err := os.Lstat(f.Path())
	if errors.Is(err, fs.ErrNotExist) {
		return Set{location: f.Path()}, nil
	}
	if err != nil {
		return Set{}, fmt.Errorf("credentials: read %s: %w", f.Path(), err)
	}
	if link.Mode()&fs.ModeSymlink != 0 {
		return Set{}, fmt.Errorf("%w: %s is a symbolic link; replace it with the file itself", ErrExposed, f.Path())
	}
	file, err := os.OpenFile(f.Path(), openFlags, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return Set{location: f.Path()}, nil
	}
	if err != nil {
		return Set{}, fmt.Errorf("credentials: open %s: %w", f.Path(), err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Set{}, fmt.Errorf("credentials: stat %s: %w", f.Path(), err)
	}
	if err := f.CheckDir(); err != nil {
		return Set{}, err
	}
	if err := f.checkFile(info, func() (int, error) { return fileOwner(file) }); err != nil {
		return Set{}, err
	}
	data, err := io.ReadAll(io.LimitReader(file, maxFileSize+1))
	if err != nil {
		return Set{}, fmt.Errorf("credentials: read %s: %w", f.Path(), err)
	}
	if len(data) > maxFileSize {
		return Set{}, fmt.Errorf("%w %s: larger than %d bytes", ErrMalformed, f.Path(), maxFileSize)
	}
	set, err := decode(data)
	if err != nil {
		return Set{}, fmt.Errorf("%w %s: %w", ErrMalformed, f.Path(), err)
	}
	set.location = f.Path()
	return set, nil
}

// CheckDir returns ErrExposed when the directory exists and is not a
// directory, is owned by another user, or group or other can write it; a
// missing directory is private. Load checks it only when the file exists,
// so a command that will write the file calls it first.
func (f File) CheckDir() error {
	info, err := os.Stat(f.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("credentials: stat %s: %w", f.dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrExposed, f.dir)
	}
	if !f.private {
		return nil
	}
	if err := f.checkOwner(func() (int, error) { return pathOwner(f.dir) }, f.dir); err != nil {
		return err
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%w: other users can write to %s; run 'chmod go-w %s'", ErrExposed, f.dir, f.dir)
	}
	return nil
}

// checkFile refuses an opened credentials file that is not a regular file,
// another user's, or accessible to group or other.
func (f File) checkFile(info fs.FileInfo, owner func() (int, error)) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is not a regular file", ErrExposed, f.Path())
	}
	if !f.private {
		return nil
	}
	if err := f.checkOwner(owner, f.Path()); err != nil {
		return err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: other users can access %s; run 'chmod 600 %s'", ErrExposed, f.Path(), f.Path())
	}
	return nil
}

func (f File) checkOwner(ownerOf func() (int, error), path string) error {
	owner, err := ownerOf()
	if err != nil {
		return fmt.Errorf("credentials: %s: %w", path, err)
	}
	if owner != f.uid {
		return fmt.Errorf("%w: %s is owned by user %d, not by you (user %d)", ErrExposed, path, owner, f.uid)
	}
	return nil
}

// Lock is a held credentials lock.
type Lock struct {
	fl *flock.Flock
}

// Lock takes the lock beside the file, creating the directory 0700 when
// needed, so a login or logout reads, changes and writes the file without
// another one interleaving. It waits while another process holds it,
// until ctx ends.
func (f File) Lock(ctx context.Context) (*Lock, error) {
	if err := os.MkdirAll(f.dir, dirMode); err != nil {
		return nil, fmt.Errorf("credentials: create %s: %w", f.dir, err)
	}
	if err := f.CheckDir(); err != nil {
		return nil, err
	}
	fl := flock.New(filepath.Join(f.dir, LockName), flock.SetPermissions(fileMode))
	locked, err := fl.TryLockContext(ctx, lockRetry)
	if err != nil {
		return nil, fmt.Errorf("credentials: lock %s: %w", fl.Path(), err)
	}
	if !locked {
		return nil, fmt.Errorf("credentials: lock %s: another login or logout holds it", fl.Path())
	}
	return &Lock{fl: fl}, nil
}

// Unlock releases the lock.
func (l *Lock) Unlock() error {
	if err := l.fl.Unlock(); err != nil {
		return fmt.Errorf("credentials: unlock %s: %w", l.fl.Path(), err)
	}
	return nil
}

// Save writes s atomically: a temporary file in the same directory, mode
// 0600, synced, then renamed over the file. The directory is created 0700.
func (f File) Save(s Set) error {
	data, err := encode(s)
	if err != nil {
		return fmt.Errorf("credentials: encode: %w", err)
	}
	if err := os.MkdirAll(f.dir, dirMode); err != nil {
		return fmt.Errorf("credentials: create %s: %w", f.dir, err)
	}
	if err := f.CheckDir(); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(f.dir, ".credentials-*.json")
	if err != nil {
		return fmt.Errorf("credentials: create temporary file: %w", err)
	}
	tmpName := tmp.Name()
	if err := writeSynced(tmp, data); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("credentials: write temporary file: %w", err)
	}
	if err := os.Rename(tmpName, f.Path()); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("credentials: replace %s: %w", f.Path(), err)
	}
	return nil
}

func writeSynced(tmp *os.File, data []byte) error {
	if err := tmp.Chmod(fileMode); err != nil {
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

// The JSON field names of the file (architecture/login.md).
const (
	fieldVersion   = "version"
	fieldDefault   = "default"
	fieldLogins    = "logins"
	fieldEndpoint  = "endpoint"
	fieldSpace     = "space"
	fieldSite      = "site"
	fieldToken     = "token"
	fieldAccess    = "access"
	fieldExpiresAt = "expiresAt"
	fieldAccount   = "account"
	fieldOwner     = "owner"
)

func decode(data []byte) (Set, error) {
	root, err := strictjson.Parse(data)
	if err != nil {
		return Set{}, err
	}
	if root.Kind != strictjson.Object {
		return Set{}, errors.New("the top level is not an object")
	}
	if err := root.RejectUnknown(fieldVersion, fieldDefault, fieldLogins); err != nil {
		return Set{}, err
	}
	ver, ok := root.Field(fieldVersion)
	if !ok || ver.Kind != strictjson.Number {
		return Set{}, errors.New("missing numeric version")
	}
	if ver.Num != FormatVersion {
		return Set{}, fmt.Errorf("version %d is not %d; a newer slivingdoc wrote it", ver.Num, FormatVersion)
	}
	list, ok := root.Field(fieldLogins)
	if !ok || list.Kind != strictjson.Array {
		return Set{}, errors.New("missing logins array")
	}
	var set Set
	for i, item := range list.Arr {
		l, err := decodeLogin(item)
		if err != nil {
			return Set{}, fmt.Errorf("logins[%d]: %w", i, err)
		}
		if _, err := set.Lookup(l.Key); err == nil {
			return Set{}, fmt.Errorf("logins[%d]: a second login for space %q at %s", i, l.Space, l.Endpoint)
		}
		set.logins = append(set.logins, l)
	}
	if def, ok := root.Field(fieldDefault); ok {
		k, err := decodeKey(def)
		if err != nil {
			return Set{}, fmt.Errorf("default: %w", err)
		}
		if _, err := set.Lookup(k); err != nil {
			return Set{}, fmt.Errorf("default names space %q at %s, which has no login", k.Space, k.Endpoint)
		}
		set.def, set.hasDef = k, true
	}
	return set, nil
}

// plainURL refuses a stored endpoint or site with user information, a
// query or a fragment. Login never stores one (the endpoints are
// normalized first), so such a file was edited by hand; the refusal never
// echoes the value, which may hold a secret.
func plainURL(raw string) error {
	u, err := url.Parse(raw)
	switch {
	case err != nil:
		return errors.New("is not a valid URL")
	case u.User != nil:
		return errors.New("has user information")
	case u.RawQuery != "" || u.ForceQuery:
		return errors.New("has a query")
	case u.Fragment != "" || strings.HasSuffix(raw, "#"):
		return errors.New("has a fragment")
	}
	return nil
}

func decodeKey(v strictjson.Value) (Key, error) {
	if v.Kind != strictjson.Object {
		return Key{}, errors.New("not an object")
	}
	if err := v.RejectUnknown(fieldEndpoint, fieldSpace); err != nil {
		return Key{}, err
	}
	return keyFields(v)
}

func keyFields(v strictjson.Value) (Key, error) {
	endpoint, err := stringField(v, fieldEndpoint)
	if err != nil {
		return Key{}, err
	}
	if err := httpstore.ValidateEndpoint(endpoint); err != nil {
		return Key{}, fmt.Errorf("endpoint: %w", err)
	}
	if err := plainURL(endpoint); err != nil {
		return Key{}, fmt.Errorf("the endpoint %w", err)
	}
	space, err := stringField(v, fieldSpace)
	if err != nil {
		return Key{}, err
	}
	if err := httpstore.ValidateSpace(space); err != nil {
		return Key{}, err
	}
	return Key{Endpoint: endpoint, Space: space}, nil
}

func decodeLogin(v strictjson.Value) (Login, error) {
	if v.Kind != strictjson.Object {
		return Login{}, errors.New("not an object")
	}
	if err := v.RejectUnknown(fieldEndpoint, fieldSpace, fieldSite, fieldToken, fieldAccess, fieldExpiresAt, fieldAccount, fieldOwner); err != nil {
		return Login{}, err
	}
	k, err := keyFields(v)
	if err != nil {
		return Login{}, err
	}
	site, err := stringField(v, fieldSite)
	if err != nil {
		return Login{}, err
	}
	if err := httpstore.ValidateEndpoint(site); err != nil {
		return Login{}, fmt.Errorf("site: %w", err)
	}
	if err := plainURL(site); err != nil {
		return Login{}, fmt.Errorf("the site %w", err)
	}
	token, err := stringField(v, fieldToken)
	if err != nil {
		return Login{}, err
	}
	if err := httpstore.ValidateToken(token); err != nil {
		// The error never echoes the token itself.
		return Login{}, errors.New("token: not printable characters without white space")
	}
	raw, err := stringField(v, fieldAccess)
	if err != nil {
		return Login{}, err
	}
	access, err := ParseAccess(raw)
	if err != nil {
		return Login{}, err
	}
	l := Login{Key: k, Site: site, Token: token, Access: access}
	if _, ok := v.Field(fieldExpiresAt); ok {
		stamp, err := stringField(v, fieldExpiresAt)
		if err != nil {
			return Login{}, err
		}
		at, err := time.Parse(time.RFC3339, stamp)
		if err != nil {
			return Login{}, fmt.Errorf("expiresAt: %w", err)
		}
		l.Expires = ExpiresAt(at)
	}
	if l.Account, err = optionalString(v, fieldAccount); err != nil {
		return Login{}, err
	}
	if l.Owner, err = optionalString(v, fieldOwner); err != nil {
		return Login{}, err
	}
	return l, nil
}

// optionalString reads a field that may be absent; present, it is a
// non-empty string.
func optionalString(v strictjson.Value, name string) (string, error) {
	if _, ok := v.Field(name); !ok {
		return "", nil
	}
	s, err := stringField(v, name)
	if err != nil {
		return "", err
	}
	if s == "" {
		return "", fmt.Errorf("field %q is empty", name)
	}
	return s, nil
}

func stringField(v strictjson.Value, name string) (string, error) {
	f, ok := v.Field(name)
	if !ok {
		return "", fmt.Errorf("missing field %q", name)
	}
	if f.Kind != strictjson.String {
		return "", fmt.Errorf("field %q is not a string", name)
	}
	return f.Str, nil
}

type fileJSON struct {
	Version int         `json:"version"`
	Default *keyJSON    `json:"default,omitempty"`
	Logins  []loginJSON `json:"logins"`
}

type keyJSON struct {
	Endpoint string `json:"endpoint"`
	Space    string `json:"space"`
}

type loginJSON struct {
	Endpoint  string `json:"endpoint"`
	Space     string `json:"space"`
	Site      string `json:"site"`
	Token     string `json:"token"`
	Access    Access `json:"access"`
	ExpiresAt string `json:"expiresAt,omitempty"`
	Account   string `json:"account,omitempty"`
	Owner     string `json:"owner,omitempty"`
}

func encode(s Set) ([]byte, error) {
	out := fileJSON{Version: FormatVersion, Logins: make([]loginJSON, 0, len(s.logins))}
	if s.hasDef {
		out.Default = &keyJSON{Endpoint: s.def.Endpoint, Space: s.def.Space}
	}
	for _, l := range s.logins {
		item := loginJSON{
			Endpoint: l.Endpoint, Space: l.Space, Site: l.Site,
			Token: l.Token, Access: l.Access, Account: l.Account, Owner: l.Owner,
		}
		if !l.Expires.Never() {
			item.ExpiresAt = l.Expires.Time().Format(time.RFC3339)
		}
		out.Logins = append(out.Logins, item)
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
