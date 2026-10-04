package settings

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/credentials"
)

// saveHelperEnv names the helper process the concurrency test spawns, so
// the lock is proved between processes. os.StartProcess spawns this test
// binary, so no external executable is ever invoked.
const saveHelperEnv = "SLIVINGDOC_SETTINGS_SAVE_HELPER"

func lookup(env map[string]string) func(string) string {
	return func(name string) string { return env[name] }
}

func entry(path, space, prefix string) Entry {
	return Entry{Path: path, Target: Target{Space: space, Prefix: prefix}}
}

func testFile(t *testing.T) File {
	t.Helper()
	f, err := Locate(lookup(map[string]string{credentials.DirEnv: filepath.Join(t.TempDir(), "cfg")}), runtime.GOOS)
	if err != nil {
		t.Fatalf("Locate() = %v", err)
	}
	return f
}

// TestMain intercepts the save helper: it runs the real read-modify-write
// in a second process, so the lock is proved between processes.
func TestMain(m *testing.M) {
	if path := os.Getenv(saveHelperEnv); path != "" {
		os.Exit(saveHelperMain(path))
	}
	os.Exit(m.Run())
}

// saveHelperMain stores one entry in the settings file at path under its
// own lock, and reports a failure as a nonzero exit.
func saveHelperMain(path string) int {
	f, err := Locate(lookup(map[string]string{credentials.DirEnv: filepath.Dir(path)}), runtime.GOOS)
	if err != nil {
		return 1
	}
	lock, err := f.Lock(context.Background())
	if err != nil {
		return 2
	}
	defer lock.Unlock()
	set, err := f.Load()
	if err != nil {
		return 3
	}
	put := entry(os.Getenv(saveHelperEnv+"_PATH"), os.Getenv(saveHelperEnv+"_SPACE"), "")
	if err := f.Save(set.Put(put)); err != nil {
		return 4
	}
	return 0
}

func TestLoadSaveRoundTrip(t *testing.T) {
	f := testFile(t)
	var set Set
	if _, ok := set.Lookup("/notes/one"); ok {
		t.Fatal("an empty Set holds an entry")
	}
	want := entry("/notes/one", "notes", "personal")
	if err := f.Save(set.Put(want).Put(entry("/notes/two", "team", ""))); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	got, err := f.Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if target, ok := got.Lookup("/notes/one"); !ok || target != want.Target {
		t.Fatalf("Lookup(one) = %+v, %v; want %+v", target, ok, want.Target)
	}
	if target, ok := got.Lookup("/notes/two"); !ok || target != (Target{Space: "team"}) {
		t.Fatalf("Lookup(two) = %+v, %v", target, ok)
	}
	if _, ok := got.Lookup("/notes/One"); ok {
		t.Fatal("Lookup() matched a path that differs by one byte")
	}
	if entries := got.Entries(); len(entries) != 2 || entries[0].Path != "/notes/one" || entries[1].Path != "/notes/two" {
		t.Fatalf("Entries() = %v, want the file order", entries)
	}
	raw, err := os.ReadFile(f.Path())
	if err != nil {
		t.Fatal(err)
	}
	file := `{"version":1,"entries":[{"path":"/notes/one","target":{"space":"notes","prefix":"personal"}},` +
		`{"path":"/notes/two","target":{"space":"team","prefix":""}}]}`
	if string(raw) != file {
		t.Fatalf("file = %s, want %s", raw, file)
	}
}

func TestDecodeRejectsStrictViolations(t *testing.T) {
	const valid = `{"version":1,"entries":[{"path":"/n","target":{"space":"notes","prefix":"p"}}]}`
	with := func(old, new string) string {
		return strings.Replace(valid, old, new, 1)
	}
	tests := []struct {
		name string
		data string
	}{
		{"not json", `{`},
		{"array", `[]`},
		{"unknown top field", with(`"entries"`, `"extra":1,"entries"`)},
		{"missing version", `{"entries":[]}`},
		{"missing entries", `{"version":1}`},
		{"entries not an array", `{"version":1,"entries":{}}`},
		{"duplicate version", `{"version":1,"version":1,"entries":[]}`},
		{"null field", with(`"space"`, `"space":null`)},
		{"entry not an object", `{"version":1,"entries":[1]}`},
		{"unknown entry field", with(`"path"`, `"token":"sld_secret","path"`)},
		{"missing path", with(`"path":"/n",`, "")},
		{"empty path", with(`"/n"`, `""`)},
		{"numeric path", with(`"/n"`, `7`)},
		{"missing target", with(`,"target":{"space":"notes","prefix":"p"}`, "")},
		{"target not an object", with(`{"space":"notes","prefix":"p"}`, `"notes"`)},
		{"unknown target field", with(`"prefix"`, `"key":"sld_secret","prefix"`)},
		{"missing space", with(`"space":"notes",`, "")},
		{"empty space", with(`"notes"`, `""`)},
		{"missing prefix", with(`,"prefix":"p"`, "")},
		{"numeric prefix", with(`"p"`, `3`)},
		{"duplicate entry", strings.Replace(valid, `]}`, `,{"path":"/n","target":{"space":"o","prefix":""}}]}`, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := testFile(t)
			writeRaw(t, f, tt.data)
			_, err := f.Load()
			if !errors.Is(err, ErrMalformed) {
				t.Fatalf("Load(%s) = %v, want ErrMalformed", tt.data, err)
			}
			if strings.Contains(err.Error(), "sld_secret") {
				t.Fatalf("Load(%s) = %q echoes the value", tt.data, err)
			}
		})
	}
}

// TestDecodeRejectsCredentialField proves the record holds no credential:
// the schema names a space and a prefix, so a token or a key beside them
// is an unknown field and the whole file is refused.
func TestDecodeRejectsCredentialField(t *testing.T) {
	for _, data := range []string{
		`{"version":1,"entries":[{"path":"/n","target":{"space":"notes","prefix":"","token":"sld_secret"}}]}`,
		`{"version":1,"entries":[{"path":"/n","target":{"space":"notes","prefix":"","key":"sld_secret"}}]}`,
		`{"version":1,"entries":[{"path":"/n","space":"notes","key":"sld_secret"}]}`,
	} {
		f := testFile(t)
		writeRaw(t, f, data)
		if _, err := f.Load(); !errors.Is(err, ErrMalformed) || strings.Contains(err.Error(), "sld_secret") {
			t.Fatalf("Load(%s) = %v, want ErrMalformed naming the field only", data, err)
		}
	}
}

func TestDecodeRejectsMalformedJSON(t *testing.T) {
	f := testFile(t)
	writeRaw(t, f, `{"version":1,"entries":[`)
	if _, err := f.Load(); !errors.Is(err, ErrMalformed) {
		t.Fatalf("Load() = %v, want ErrMalformed", err)
	}
}

// TestUnsupportedVersionIsRefusedUnwritten proves a file of any version
// but FormatVersion is refused with the file named and the fix for its
// direction, that the refusal precedes any decoding, and that nothing
// rewrites it.
func TestUnsupportedVersionIsRefusedUnwritten(t *testing.T) {
	const older, newer = "remove %s and pull again", "a newer slivingdoc wrote it (%s); update slivingdoc, and keep the file"
	for _, row := range []struct {
		data, fix, not string
	}{
		{`{"version":0,"entries":[]}`, older, "update slivingdoc"},
		{`{"version":2,"entries":[],"extra":"sld_secret"}`, newer, "remove "},
	} {
		f := testFile(t)
		writeRaw(t, f, row.data)
		_, err := f.Load()
		if !errors.Is(err, ErrUnsupportedVersion) || errors.Is(err, ErrMalformed) ||
			!strings.Contains(err.Error(), strings.Replace(row.fix, "%s", f.Path(), 1)) ||
			strings.Contains(err.Error(), row.not) || !strings.Contains(err.Error(), "reads version 1 only") ||
			strings.Contains(err.Error(), "sld_secret") {
			t.Fatalf("Load(%s) = %v, want ErrUnsupportedVersion with %q", row.data, err, row.fix)
		}
		raw, readErr := os.ReadFile(f.Path())
		if readErr != nil || string(raw) != row.data {
			t.Fatalf("the file after the refusal is %q, %v; want it byte-identical", raw, readErr)
		}
		if err := f.Save(Set{}); err != nil {
			t.Fatalf("Save() = %v", err)
		}
		if raw, _ := os.ReadFile(f.Path()); string(raw) == row.data {
			t.Fatalf("Save() of an empty Set left the other version in place: %s", raw)
		}
		if _, err := f.Load(); errors.Is(err, ErrUnsupportedVersion) {
			t.Fatalf("Load() after the overwrite = %v, want the written file read back", err)
		}
	}
}

func TestPutReplacesAndAppends(t *testing.T) {
	var set Set
	set = set.Put(entry("/a", "notes", "")).Put(entry("/b", "team", "shared"))
	set = set.Put(entry("/a", "notes", "personal"))
	entries := set.Entries()
	if len(entries) != 2 || entries[0].Path != "/a" || entries[0].Target.Prefix != "personal" || entries[1].Path != "/b" {
		t.Fatalf("Entries() = %v, want the replacement in place and the append last", entries)
	}
	if _, ok := set.Lookup("/a"); !ok {
		t.Fatal("Put() dropped the replaced entry")
	}
	f := testFile(t)
	if err := f.Save(set); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	got, err := f.Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if target, _ := got.Lookup("/b"); target != (Target{Space: "team", Prefix: "shared"}) {
		t.Fatalf("Lookup(b) = %+v, want the untouched sibling entry", target)
	}
}

// TestLocateMatchesCredentialsLocate proves the settings file is located
// by the credentials file's own rule, so one configuration directory
// holds both.
func TestLocateMatchesCredentialsLocate(t *testing.T) {
	for _, tt := range []struct {
		name string
		env  map[string]string
		goos string
	}{
		{"override", map[string]string{credentials.DirEnv: "/cfg/x/", "HOME": "/home/u"}, "linux"},
		{"xdg", map[string]string{"XDG_CONFIG_HOME": "/xdg", "HOME": "/home/u"}, "linux"},
		{"relative xdg falls back to home", map[string]string{"XDG_CONFIG_HOME": "xdg", "HOME": "/home/u"}, "linux"},
		{"home", map[string]string{"HOME": "/home/u"}, "freebsd"},
		{"darwin", map[string]string{"HOME": "/Users/u"}, "darwin"},
		{"plan9", map[string]string{"home": "/usr/u"}, "plan9"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mine, err := Locate(lookup(tt.env), tt.goos)
			if err != nil {
				t.Fatalf("Locate() = %v", err)
			}
			their, err := credentials.Locate(lookup(tt.env), tt.goos)
			if err != nil {
				t.Fatalf("credentials.Locate() = %v", err)
			}
			if got, want := filepath.Dir(mine.Path()), filepath.Dir(their.Path()); got != want {
				t.Fatalf("settings directory %s, credentials directory %s", got, want)
			}
		})
	}
	for _, tt := range []struct {
		name string
		env  map[string]string
		goos string
	}{
		{"nothing", nil, "linux"},
		{"relative override", map[string]string{credentials.DirEnv: "cfg"}, "linux"},
		{"windows without AppData", map[string]string{"HOME": "/home/u"}, "windows"},
		{"darwin without home", nil, "darwin"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Locate(lookup(tt.env), tt.goos); !errors.Is(err, ErrNoConfigDir) {
				t.Fatalf("Locate() = %v, want ErrNoConfigDir", err)
			}
		})
	}
}

func TestLocateWithoutConfigDir(t *testing.T) {
	_, err := Locate(lookup(map[string]string{credentials.DirEnv: "relative"}), "linux")
	if !errors.Is(err, ErrNoConfigDir) || !strings.Contains(err.Error(), credentials.DirEnv) {
		t.Fatalf("Locate() = %v, want ErrNoConfigDir naming %s", err, credentials.DirEnv)
	}
	if _, err := Locate(lookup(nil), "linux"); !errors.Is(err, ErrNoConfigDir) || !strings.Contains(err.Error(), "HOME") {
		t.Fatalf("Locate() = %v, want ErrNoConfigDir naming HOME", err)
	}
}

// TestMissingFileIsEmpty proves an absent file is an empty Set, not a
// refusal: nobody has remembered a directory yet.
func TestMissingFileIsEmpty(t *testing.T) {
	f := testFile(t)
	set, err := f.Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if len(set.Entries()) != 0 {
		t.Fatalf("Entries() = %v, want none", set.Entries())
	}
	if err := f.CheckDir(); err != nil {
		t.Fatalf("CheckDir() without a directory = %v", err)
	}
}

func TestLoadRefusesWhatIsNotTheFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no POSIX owners, and symbolic links need a privilege")
	}
	for _, tt := range []struct {
		name, want string
		plant      func(t *testing.T, f File)
	}{
		{"a symbolic link to a stored file", "symbolic link", func(t *testing.T, f File) {
			if err := f.Save(Set{}.Put(entry("/n", "notes", ""))); err != nil {
				t.Fatal(err)
			}
			real := filepath.Join(filepath.Dir(f.Path()), "real.json")
			if err := os.Rename(f.Path(), real); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(real, f.Path()); err != nil {
				t.Fatal(err)
			}
		}},
		{"a dangling symbolic link", "symbolic link", func(t *testing.T, f File) {
			if err := os.MkdirAll(filepath.Dir(f.Path()), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(t.TempDir(), "planted.json"), f.Path()); err != nil {
				t.Fatal(err)
			}
		}},
		{"a directory", "not a regular file", func(t *testing.T, f File) {
			if err := os.MkdirAll(f.Path(), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"a FIFO", "not a regular file", func(t *testing.T, f File) {
			if err := os.MkdirAll(filepath.Dir(f.Path()), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := mkfifo(f.Path()); err != nil {
				t.Skipf("no FIFO on this platform: %v", err)
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := testFile(t)
			tt.plant(t, f)
			if _, err := f.Load(); !errors.Is(err, ErrExposed) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() = %v, want ErrExposed saying %q", err, tt.want)
			}
		})
	}
}

func TestLoadRefusesExposedFileAndDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no POSIX owners, or permission bits to change")
	}
	f := testFile(t)
	if err := f.Save(Set{}.Put(entry("/n", "notes", ""))); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(f.Path())
	for _, tt := range []struct {
		name          string
		dirMode, mode os.FileMode
		want          string
	}{
		{"group-readable file", 0o700, 0o640, "chmod 600"},
		{"world-writable file", 0o700, 0o602, "chmod 600"},
		{"group-writable directory", 0o770, 0o600, "chmod go-w"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.Chmod(dir, tt.dirMode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(f.Path(), tt.mode); err != nil {
				t.Fatal(err)
			}
			if _, err := f.Load(); !errors.Is(err, ErrExposed) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() = %v, want ErrExposed saying %q", err, tt.want)
			}
		})
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(Set{}); !errors.Is(err, ErrExposed) {
		t.Fatalf("Save() into a world-writable directory = %v, want ErrExposed", err)
	}
	if err := os.Remove(f.Path()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Load(); err != nil {
		t.Fatalf("Load() without a file = %v; an exposed directory holding no file is not read", err)
	}
}

func TestSaveModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no POSIX permission bits")
	}
	f := testFile(t)
	if err := f.Save(Set{}.Put(entry("/n", "notes", ""))); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(f.Path()); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, %v; want 0600", info, err)
	}
	if info, err := os.Stat(filepath.Dir(f.Path())); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode = %v, %v; want 0700", info, err)
	}
}

// TestBoundRefusesOverSizeFile proves the bound holds on a read and on a
// write: a file over the bound is refused before it is decoded, and a
// write that would grow past it never creates the file.
func TestBoundRefusesOverSizeFile(t *testing.T) {
	f := testFile(t)
	if err := f.Save(Set{}.Put(entry("/n", "notes", strings.Repeat("p", 200)))); err != nil {
		t.Fatalf("Save() under the bound = %v", err)
	}
	raw, err := os.ReadFile(f.Path())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.withBound(int64(len(raw))).Load(); err != nil {
		t.Fatalf("Load() of a file of exactly the bound = %v", err)
	}
	if err := os.WriteFile(f.Path(), append(raw, ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	// The appended byte would break the JSON too, so the bound is checked
	// first: the refusal must be the size.
	atBound := f.withBound(int64(len(raw)))
	if _, err := atBound.Load(); !errors.Is(err, ErrTooLarge) || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("Load() of an over-size file = %v, want ErrTooLarge", err)
	}
	fresh := testFile(t).withBound(64)
	if err := fresh.Save(Set{}.Put(entry("/n", "notes", strings.Repeat("p", 200)))); !errors.Is(err, ErrTooLarge) ||
		!strings.Contains(err.Error(), fresh.Path()) {
		t.Fatalf("Save() over the bound = %v, want ErrTooLarge naming the file", err)
	}
	if _, err := os.Stat(fresh.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the refused write created %s", fresh.Path())
	}
}

// TestConfigInjectedLoadSkipsTheFile proves the injected reader is the
// only reader when it is set: a file this build could not read never
// reaches it, and the injection is used exactly once.
func TestConfigInjectedLoadSkipsTheFile(t *testing.T) {
	f := testFile(t)
	writeRaw(t, f, "{not json")
	want := Set{}.Put(entry("/injected", "notes", "injected"))
	var calls int
	env := lookup(map[string]string{credentials.DirEnv: filepath.Dir(f.Path())})
	got, err := Config{Load: func() (Set, error) {
		calls++
		return want, nil
	}}.Read(env, runtime.GOOS)
	if err != nil {
		t.Fatalf("Read() = %v", err)
	}
	if calls != 1 {
		t.Fatalf("the injected reader ran %d times, want once", calls)
	}
	if target, ok := got.Lookup("/injected"); !ok || target != (Target{Space: "notes", Prefix: "injected"}) {
		t.Fatalf("Lookup() = %+v, %v; want the injected set", target, ok)
	}
	if _, err := (Config{}).Read(env, runtime.GOOS); !errors.Is(err, ErrMalformed) {
		t.Fatalf("Read() without an injected reader = %v, want the file's own refusal", err)
	}
}

// TestConfigBoundAppliesToTheFile proves the injected bound reaches the
// file the config owns.
func TestConfigBoundAppliesToTheFile(t *testing.T) {
	f := testFile(t)
	if err := f.Save(Set{}.Put(entry("/n", "notes", strings.Repeat("p", 200)))); err != nil {
		t.Fatal(err)
	}
	env := lookup(map[string]string{credentials.DirEnv: filepath.Dir(f.Path())})
	if _, err := (Config{MaxFileSize: 64}).Read(env, runtime.GOOS); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Read() = %v, want ErrTooLarge under the injected bound", err)
	}
	if _, err := (Config{}).Read(env, runtime.GOOS); err != nil {
		t.Fatalf("Read() under the package bound = %v", err)
	}
	if _, err := (Config{}).Read(lookup(nil), runtime.GOOS); !errors.Is(err, ErrNoConfigDir) {
		t.Fatalf("Read() without a configuration directory = %v, want ErrNoConfigDir", err)
	}
}

func TestSaveRefusesWhileLocked(t *testing.T) {
	f := testFile(t)
	first, err := f.Lock(context.Background())
	if err != nil {
		t.Fatalf("Lock() = %v", err)
	}
	if info, err := os.Stat(filepath.Join(filepath.Dir(f.Path()), LockName)); err != nil ||
		(runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("lock file = %v, %v; want it beside the settings file, 0600", info, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := f.Lock(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a second Lock() while the first is held = %v, want the deadline", err)
	}
	if err := first.Unlock(); err != nil {
		t.Fatalf("Unlock() = %v", err)
	}
	second, err := f.Lock(context.Background())
	if err != nil {
		t.Fatalf("Lock() after Unlock() = %v", err)
	}
	if err := second.Unlock(); err != nil {
		t.Fatalf("Unlock() = %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(filepath.Dir(f.Path()), 0o777); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Lock(context.Background()); !errors.Is(err, ErrExposed) {
			t.Fatalf("Lock() in a world-writable directory = %v, want ErrExposed", err)
		}
	}
}

// TestConcurrentSavesKeepBothEntries proves the lock is what keeps two
// writers apart: two processes each store their own entry, and the file
// that survives holds both.
func TestConcurrentSavesKeepBothEntries(t *testing.T) {
	f := testFile(t)
	if err := f.Save(Set{}.Put(entry("/first", "alpha", ""))); err != nil {
		t.Fatal(err)
	}
	spawn := func(path, space string) *os.Process {
		t.Helper()
		proc, err := os.StartProcess(os.Args[0], []string{os.Args[0]}, &os.ProcAttr{
			Env: append(os.Environ(),
				saveHelperEnv+"="+f.Path(),
				saveHelperEnv+"_PATH="+path,
				saveHelperEnv+"_SPACE="+space,
			),
		})
		if err != nil {
			t.Fatalf("start helper: %v", err)
		}
		return proc
	}
	first := spawn("/second", "beta")
	second := spawn("/third", "gamma")
	wait := func(p *os.Process, name string) {
		t.Helper()
		state, err := p.Wait()
		if err != nil {
			t.Fatalf("wait for the %s helper: %v", name, err)
		}
		if state.ExitCode() != 0 {
			t.Fatalf("the %s helper exited %d", name, state.ExitCode())
		}
	}
	wait(first, "first")
	wait(second, "second")
	set, err := f.Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	for _, want := range []Entry{entry("/first", "alpha", ""), entry("/second", "beta", ""), entry("/third", "gamma", "")} {
		target, ok := set.Lookup(want.Path)
		if !ok || target.Space != want.Target.Space {
			t.Fatalf("Lookup(%s) = %+v, %v; the concurrent writer lost its entry", want.Path, target, ok)
		}
	}
}

func TestSaveRemovesTempFileOnFailure(t *testing.T) {
	f := testFile(t)
	if err := os.MkdirAll(filepath.Dir(f.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	// A directory in place of the file makes the rename fail after the
	// temporary file exists.
	if err := os.Mkdir(f.Path(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(Set{}.Put(entry("/n", "notes", ""))); err == nil {
		t.Fatal("Save() over a directory = nil, want an error")
	}
	entries, err := os.ReadDir(filepath.Dir(f.Path()))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("Save() left the temporary file %s behind", e.Name())
		}
	}
	// A configuration directory that cannot be created fails before any
	// file is written.
	blocked := testFile(t)
	if err := os.WriteFile(filepath.Dir(blocked.Path()), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := blocked.Save(Set{}.Put(entry("/n", "notes", ""))); err == nil {
		t.Fatal("Save() below a file = nil, want an error")
	}
}

func writeRaw(t *testing.T, f File, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(f.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.Path(), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
