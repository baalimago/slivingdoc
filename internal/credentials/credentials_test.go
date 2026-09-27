package credentials

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	testToken  = "sld_0123456789abcdef_Y3JlZGVudGlhbHMtdGVzdC10b2tlbi1mb3ItdGhlLWZpbGU"
	testToken2 = "sld_fedcba9876543210_c2Vjb25kLWNyZWRlbnRpYWxzLXRlc3QtdG9rZW4tZm9yLWE"
	apiA       = "https://api.slivingdoc.dev"
	apiB       = "https://api.dev.slivingdoc.dev"
	site       = "https://www.slivingdoc.dev"
)

func lookup(env map[string]string) func(string) string {
	return func(name string) string { return env[name] }
}

func TestLocate(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		goos string
		want string
	}{
		{"override", map[string]string{DirEnv: "/cfg/x/", "HOME": "/home/u"}, "linux", "/cfg/x"},
		{"xdg", map[string]string{"XDG_CONFIG_HOME": "/xdg", "HOME": "/home/u"}, "linux", "/xdg/slivingdoc"},
		{"relative xdg falls back to home", map[string]string{"XDG_CONFIG_HOME": "xdg", "HOME": "/home/u"}, "linux", "/home/u/.config/slivingdoc"},
		{"home", map[string]string{"HOME": "/home/u"}, "freebsd", "/home/u/.config/slivingdoc"},
		{"darwin", map[string]string{"HOME": "/Users/u"}, "darwin", "/Users/u/Library/Application Support/slivingdoc"},
		{"plan9", map[string]string{"home": "/usr/u"}, "plan9", "/usr/u/lib/slivingdoc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := Locate(lookup(tt.env), tt.goos)
			if err != nil {
				t.Fatalf("Locate() = %v", err)
			}
			if want := filepath.Join(filepath.FromSlash(tt.want), FileName); f.Path() != want {
				t.Fatalf("Path() = %q, want %q", f.Path(), want)
			}
		})
	}
	for _, tt := range []struct {
		name string
		env  map[string]string
		goos string
	}{
		{"nothing", nil, "linux"},
		{"relative override", map[string]string{DirEnv: "cfg"}, "linux"},
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

func testFile(t *testing.T) File {
	t.Helper()
	f, err := Locate(lookup(map[string]string{DirEnv: filepath.Join(t.TempDir(), "cfg")}), runtime.GOOS)
	if err != nil {
		t.Fatalf("Locate() = %v", err)
	}
	return f
}

func login(endpoint, space, token string, access Access) Login {
	return Login{Key: Key{Endpoint: endpoint, Space: space}, Site: site, Token: token, Access: access}
}

func TestMissingFileIsEmpty(t *testing.T) {
	set, err := testFile(t).Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if len(set.Logins()) != 0 {
		t.Fatalf("Logins() = %v, want none", set.Logins())
	}
	if _, err := set.Default(); !errors.Is(err, ErrNoDefault) {
		t.Fatalf("Default() = %v, want ErrNoDefault", err)
	}
	if _, err := set.Lookup(Key{Endpoint: apiA, Space: "notes"}); !errors.Is(err, ErrNoLogin) {
		t.Fatalf("Lookup() = %v, want ErrNoLogin", err)
	}
	if _, err := set.ForSpace("notes"); !errors.Is(err, ErrNoLogin) {
		t.Fatalf("ForSpace() = %v, want ErrNoLogin", err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	f := testFile(t)
	var set Set
	expiring := login(apiA, "notes", testToken, AccessWrite)
	expiring.Expires = ExpiresAt(time.Date(2026, 12, 26, 10, 30, 0, 0, time.FixedZone("x", 3600)))
	if _, err := set.Put(expiring); !errors.Is(err, ErrNoLogin) {
		t.Fatalf("Put(new) = %v, want ErrNoLogin (nothing replaced)", err)
	}
	team := login(apiB, "team", testToken2, AccessRead)
	team.Account, team.Owner = "ada@example.test", "bob@example.test"
	if _, err := set.Put(team); !errors.Is(err, ErrNoLogin) {
		t.Fatalf("Put(new) = %v, want ErrNoLogin", err)
	}
	if err := set.SetDefault(team.Key); err != nil {
		t.Fatalf("SetDefault(team) = %v", err)
	}
	if err := f.Save(set); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(f.Path())
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("file mode = %v, %v; want 0600", info.Mode().Perm(), err)
		}
		dir, err := os.Stat(filepath.Dir(f.Path()))
		if err != nil || dir.Mode().Perm() != 0o700 {
			t.Fatalf("dir mode = %v, %v; want 0700", dir.Mode().Perm(), err)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(f.Path()))
	if err != nil || len(entries) != 1 {
		t.Fatalf("config dir holds %v, %v; want only the credentials file", entries, err)
	}

	got, err := f.Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	def, err := got.Default()
	if err != nil || def.Space != "team" || def.Endpoint != apiB || def.Access != AccessRead || !def.Expires.Never() ||
		def.Account != "ada@example.test" || def.Owner != "bob@example.test" {
		t.Fatalf("Default() = %+v, %v; want the read-only team login without expiry", def, err)
	}
	notes, err := got.Lookup(Key{Endpoint: apiA, Space: "notes"})
	if err != nil || notes.Token != testToken || notes.Site != site || !notes.Expires.Time().Equal(expiring.Expires.Time()) ||
		notes.Account != "" || notes.Owner != "" {
		t.Fatalf("Lookup(notes) = %+v, %v; want the stored login", notes, err)
	}
	if notes.Expires.Describe() != "until 2026-12-26 09:30 UTC" {
		t.Fatalf("Describe() = %q", notes.Expires.Describe())
	}
}

func TestPutReplacesAndRemoveClearsDefault(t *testing.T) {
	var set Set
	set.Put(login(apiA, "notes", testToken, AccessWrite))
	if _, err := set.Default(); !errors.Is(err, ErrNoDefault) {
		t.Fatalf("Default() after Put = %v, want ErrNoDefault: Put never chooses the default", err)
	}
	if err := set.SetDefault(Key{Endpoint: apiA, Space: "notes"}); err != nil {
		t.Fatalf("SetDefault(notes) = %v", err)
	}
	if err := set.SetDefault(Key{Endpoint: apiB, Space: "notes"}); !errors.Is(err, ErrNoLogin) {
		t.Fatalf("SetDefault(missing) = %v, want ErrNoLogin", err)
	}
	set.Put(login(apiA, "team", testToken, AccessWrite))
	old, err := set.Put(login(apiA, "notes", testToken2, AccessRead))
	if err != nil || old.Token != testToken {
		t.Fatalf("Put(replacement) = %+v, %v; want the replaced login", old, err)
	}
	if n := len(set.Logins()); n != 2 {
		t.Fatalf("Logins() = %d entries, want 2", n)
	}
	if def, _ := set.Default(); def.Space != "notes" || def.Token != testToken2 {
		t.Fatalf("Default() = %+v, want the replacement", def)
	}
	if err := set.Remove(Key{Endpoint: apiA, Space: "team"}); err != nil {
		t.Fatalf("Remove(team) = %v", err)
	}
	if _, err := set.Default(); err != nil {
		t.Fatal("removing another login cleared the default")
	}
	if err := set.Remove(Key{Endpoint: apiA, Space: "notes"}); err != nil {
		t.Fatalf("Remove(notes) = %v", err)
	}
	if _, err := set.Default(); !errors.Is(err, ErrNoDefault) {
		t.Fatalf("Default() after removing it = %v, want ErrNoDefault", err)
	}
	if err := set.Remove(Key{Endpoint: apiA, Space: "notes"}); !errors.Is(err, ErrNoLogin) {
		t.Fatalf("Remove(missing) = %v, want ErrNoLogin", err)
	}
}

func TestForSpace(t *testing.T) {
	var set Set
	set.Put(login(apiA, "notes", testToken, AccessWrite))
	set.Put(login(apiB, "notes", testToken2, AccessWrite))
	set.Put(login(apiA, "team", testToken, AccessWrite))
	if l, err := set.ForSpace("team"); err != nil || l.Endpoint != apiA {
		t.Fatalf("ForSpace(team) = %+v, %v; want the only team login", l, err)
	}
	if _, err := set.ForSpace("notes"); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("ForSpace(notes) = %v, want ErrAmbiguous", err)
	}
	if err := set.SetDefault(Key{Endpoint: apiB, Space: "notes"}); err != nil {
		t.Fatal(err)
	}
	if _, err := set.ForSpace("notes"); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("ForSpace(notes) with a default among them = %v, want ErrAmbiguous: only an endpoint chooses", err)
	}
	if n := len(set.Space("notes")); n != 2 {
		t.Fatalf("Space(notes) = %d logins, want 2", n)
	}
}

func TestUsable(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	l := login(apiA, "notes", testToken, AccessWrite)
	if err := l.Usable(now); err != nil {
		t.Fatalf("Usable(no expiry) = %v", err)
	}
	l.Expires = ExpiresAt(now.Add(time.Hour))
	if err := l.Usable(now); err != nil {
		t.Fatalf("Usable(future) = %v", err)
	}
	l.Expires = ExpiresAt(now)
	err := l.Usable(now)
	if !errors.Is(err, ErrExpired) || strings.Contains(err.Error(), testToken) || !strings.Contains(err.Error(), "2026-09-27 12:00 UTC") {
		t.Fatalf("Usable(expired) = %v, want ErrExpired naming the date and not the token", err)
	}
}

func TestAccess(t *testing.T) {
	if AccessWrite.Describe() != "read and write" || AccessRead.Describe() != "read only" {
		t.Fatal("unexpected access wording")
	}
	if _, err := ParseAccess("admin"); err == nil {
		t.Fatal("ParseAccess(admin) = nil, want an error")
	}
	if a, err := ParseAccess("read"); err != nil || a != AccessRead {
		t.Fatalf("ParseAccess(read) = %v, %v", a, err)
	}
	if ExpiresAt(time.Time{}).Describe() != "with no expiry" {
		t.Fatal("unexpected no-expiry wording")
	}
}

func TestLoadRefusesMalformedFiles(t *testing.T) {
	entry := `{"endpoint":"https://api.slivingdoc.dev","space":"notes","site":"https://www.slivingdoc.dev","token":"` + testToken + `","access":"write"}`
	tests := []struct {
		name string
		data string
	}{
		{"not json", `{`},
		{"array", `[]`},
		{"unknown top field", `{"version":1,"logins":[],"extra":1}`},
		{"missing version", `{"logins":[]}`},
		{"newer version", `{"version":2,"logins":[]}`},
		{"missing logins", `{"version":1}`},
		{"null default", `{"version":1,"logins":[],"default":null}`},
		{"duplicate key", `{"version":1,"version":1,"logins":[]}`},
		{"login not object", `{"version":1,"logins":[1]}`},
		{"unknown login field", `{"version":1,"logins":[` + strings.Replace(entry, `"access"`, `"x":1,"access"`, 1) + `]}`},
		{"missing token", `{"version":1,"logins":[` + strings.Replace(entry, `"token":"`+testToken+`",`, "", 1) + `]}`},
		{"token with space", `{"version":1,"logins":[` + strings.Replace(entry, testToken, "sld bad", 1) + `]}`},
		{"bad access", `{"version":1,"logins":[` + strings.Replace(entry, `"write"`, `"admin"`, 1) + `]}`},
		{"bad space", `{"version":1,"logins":[` + strings.Replace(entry, `"notes"`, `"No_Space"`, 1) + `]}`},
		{"http remote endpoint", `{"version":1,"logins":[` + strings.Replace(entry, "https://api", "http://api", 1) + `]}`},
		{"http remote site", `{"version":1,"logins":[` + strings.Replace(entry, "https://www", "http://www", 1) + `]}`},
		{"numeric token", `{"version":1,"logins":[` + strings.Replace(entry, `"`+testToken+`"`, "5", 1) + `]}`},
		{"bad expiry", `{"version":1,"logins":[` + strings.Replace(entry, `"write"`, `"write","expiresAt":"tomorrow"`, 1) + `]}`},
		{"empty account", `{"version":1,"logins":[` + strings.Replace(entry, `"write"`, `"write","account":""`, 1) + `]}`},
		{"numeric owner", `{"version":1,"logins":[` + strings.Replace(entry, `"write"`, `"write","owner":1`, 1) + `]}`},
		{"duplicate login", `{"version":1,"logins":[` + entry + `,` + entry + `]}`},
		{"default without login", `{"version":1,"logins":[],"default":{"endpoint":"https://api.slivingdoc.dev","space":"notes"}}`},
		{"default with extra field", `{"version":1,"logins":[` + entry + `],"default":{"endpoint":"https://api.slivingdoc.dev","space":"notes","x":1}}`},
		{"default not object", `{"version":1,"logins":[],"default":"notes"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := testFile(t)
			if err := os.MkdirAll(filepath.Dir(f.Path()), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f.Path(), []byte(tt.data), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := f.Load()
			if !errors.Is(err, ErrMalformed) {
				t.Fatalf("Load() = %v, want ErrMalformed", err)
			}
			if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "sld bad") {
				t.Fatalf("Load() = %q echoes the token", err)
			}
		})
	}
}

func TestLoadAcceptsLoopbackHTTP(t *testing.T) {
	f := testFile(t)
	if err := os.MkdirAll(filepath.Dir(f.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"version":1,"default":{"endpoint":"http://127.0.0.1:8787","space":"notes"},"logins":[` +
		`{"endpoint":"http://127.0.0.1:8787","space":"notes","site":"http://localhost:8788","token":"` + testToken +
		`","access":"read","expiresAt":"2030-01-02T03:04:05Z"}]}`
	if err := os.WriteFile(f.Path(), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	set, err := f.Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	def, err := set.Default()
	if err != nil || def.Site != "http://localhost:8788" || def.Access != AccessRead || def.Expires.Never() {
		t.Fatalf("Default() = %+v, %v", def, err)
	}
}

func TestLoadRefusesExposedFiles(t *testing.T) {
	f := testFile(t)
	if err := f.Save(Set{}); err != nil {
		t.Fatalf("Save() = %v", err)
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
			_, err := f.Load()
			if !errors.Is(err, ErrExposed) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() = %v, want ErrExposed saying %q", err, tt.want)
			}
			windows := f
			windows.private = false
			if _, err := windows.Load(); err != nil {
				t.Fatalf("Load() without permission bits = %v", err)
			}
		})
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(f.Path(), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Load(); err != nil {
		t.Fatalf("Load() of a private file in a readable directory = %v", err)
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
	if err := f.CheckDir(); !errors.Is(err, ErrExposed) {
		t.Fatalf("CheckDir() of a world-writable directory = %v, want ErrExposed", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Load(); err != nil {
		t.Fatalf("Load() without a directory = %v, want an empty set", err)
	}
	if err := f.CheckDir(); err != nil {
		t.Fatalf("CheckDir() without a directory = %v", err)
	}
}

func TestLoadAndSaveReportFilesystemFailures(t *testing.T) {
	f := testFile(t)
	if err := os.MkdirAll(f.Path(), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Load(); err == nil || errors.Is(err, ErrMalformed) {
		t.Fatalf("Load() of a directory = %v, want a read error", err)
	}
	if err := f.Save(Set{}); err == nil {
		t.Fatal("Save() over a directory = nil, want an error")
	}
	blocked := File{dir: filepath.Join(f.Path(), "x", FileName)}
	if err := os.WriteFile(filepath.Join(f.Path(), "x"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := blocked.Save(Set{}); err == nil {
		t.Fatal("Save() below a file = nil, want an error")
	}
}

// savedFile is a testFile holding one saved login.
func savedFile(t *testing.T) File {
	t.Helper()
	f := testFile(t)
	var set Set
	set.Put(login(apiA, "notes", testToken, AccessWrite))
	if err := f.Save(set); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	return f
}

func TestLoadRefusesWhatIsNotTheUsersPlainFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no POSIX owners, and symbolic links need a privilege")
	}
	t.Run("a symbolic link to a private file", func(t *testing.T) {
		f := savedFile(t)
		target := filepath.Join(filepath.Dir(f.Path()), "real.json")
		if err := os.Rename(f.Path(), target); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, f.Path()); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Load(); !errors.Is(err, ErrExposed) || !strings.Contains(err.Error(), "symbolic link") {
			t.Fatalf("Load() = %v, want ErrExposed naming the link", err)
		}
	})
	t.Run("a dangling symbolic link", func(t *testing.T) {
		f := testFile(t)
		if err := os.MkdirAll(filepath.Dir(f.Path()), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(t.TempDir(), "planted.json"), f.Path()); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Load(); !errors.Is(err, ErrExposed) {
			t.Fatalf("Load() = %v, want ErrExposed: a link is never an empty file", err)
		}
	})
	t.Run("a FIFO", func(t *testing.T) {
		f := testFile(t)
		if err := os.MkdirAll(filepath.Dir(f.Path()), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := mkfifo(f.Path()); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Load(); !errors.Is(err, ErrExposed) || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("Load() = %v, want ErrExposed without blocking", err)
		}
	})
	t.Run("a file another user owns", func(t *testing.T) {
		f := savedFile(t)
		f.uid++
		if _, err := f.Load(); !errors.Is(err, ErrExposed) || !strings.Contains(err.Error(), "owned by user") {
			t.Fatalf("Load() = %v, want ErrExposed naming the owner", err)
		}
		if err := f.CheckDir(); !errors.Is(err, ErrExposed) {
			t.Fatalf("CheckDir() = %v, want ErrExposed: the directory is another user's too", err)
		}
		if err := f.Save(Set{}); !errors.Is(err, ErrExposed) {
			t.Fatalf("Save() = %v, want ErrExposed", err)
		}
	})
	t.Run("a file larger than 1 MiB", func(t *testing.T) {
		f := savedFile(t)
		if err := os.WriteFile(f.Path(), make([]byte, maxFileSize+1), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Load(); !errors.Is(err, ErrMalformed) || !strings.Contains(err.Error(), "larger than") {
			t.Fatalf("Load() = %v, want ErrMalformed for the size", err)
		}
	})
	t.Run("a directory path that is a file", func(t *testing.T) {
		f := testFile(t)
		if err := os.WriteFile(f.dir, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := f.CheckDir(); !errors.Is(err, ErrExposed) || !strings.Contains(err.Error(), "not a directory") {
			t.Fatalf("CheckDir() = %v, want ErrExposed", err)
		}
	})
}

func TestLockSerializesLogins(t *testing.T) {
	f := testFile(t)
	first, err := f.Lock(context.Background())
	if err != nil {
		t.Fatalf("Lock() = %v", err)
	}
	if info, err := os.Stat(filepath.Join(f.dir, LockName)); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("lock file = %v, %v; want it beside the file, 0600", info, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := f.Lock(ctx); err == nil {
		t.Fatal("a second Lock() while the first is held = nil, want it to wait until ctx ends")
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
		if err := os.Chmod(f.dir, 0o777); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Lock(context.Background()); !errors.Is(err, ErrExposed) {
			t.Fatalf("Lock() in a world-writable directory = %v, want ErrExposed", err)
		}
	}
	blocked := File{dir: filepath.Join(f.dir, LockName, "below")}
	if _, err := blocked.Lock(context.Background()); err == nil {
		t.Fatal("Lock() below a file = nil, want an error")
	}
}
