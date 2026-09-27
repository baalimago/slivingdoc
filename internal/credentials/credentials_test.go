package credentials

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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

func login(endpoint, key string, access Access) Login {
	return Login{ID: ID{Site: site, Endpoint: endpoint}, Key: key, Access: access, Account: "ada@example.test"}
}

func TestMissingFileIsEmpty(t *testing.T) {
	set, err := testFile(t).Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if len(set.Logins()) != 0 {
		t.Fatalf("Logins() = %v, want none", set.Logins())
	}
	if _, err := set.DefaultSpace(apiA); !errors.Is(err, ErrNoDefault) {
		t.Fatalf("DefaultSpace() = %v, want ErrNoDefault", err)
	}
	if _, err := set.Lookup(ID{Site: site, Endpoint: apiA}); !errors.Is(err, ErrNoLogin) {
		t.Fatalf("Lookup() = %v, want ErrNoLogin", err)
	}
	if _, err := set.ForEndpoint(apiA); !errors.Is(err, ErrNoLogin) {
		t.Fatalf("ForEndpoint() = %v, want ErrNoLogin", err)
	}
	if _, err := set.Only(); !errors.Is(err, ErrNoLogin) {
		t.Fatalf("Only() = %v, want ErrNoLogin", err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	f := testFile(t)
	var set Set
	expiring := login(apiA, testToken, AccessWrite)
	expiring.Expires = ExpiresAt(time.Date(2026, 12, 26, 10, 30, 0, 0, time.FixedZone("x", 3600)))
	if _, err := set.Put(expiring); !errors.Is(err, ErrNoLogin) {
		t.Fatalf("Put(new) = %v, want ErrNoLogin (nothing replaced)", err)
	}
	team := login(apiB, testToken2, AccessRead)
	team.Account = "bob@example.test"
	if _, err := set.Put(team); !errors.Is(err, ErrNoLogin) {
		t.Fatalf("Put(new) = %v, want ErrNoLogin", err)
	}
	if err := set.SetDefaultSpace(apiB, "team"); err != nil {
		t.Fatalf("SetDefaultSpace(team) = %v", err)
	}
	if err := set.SetDefaultSpace(apiA, "notes"); err != nil {
		t.Fatalf("SetDefaultSpace(notes) = %v", err)
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
	if space, err := got.DefaultSpace(apiB); err != nil || space != "team" {
		t.Fatalf("DefaultSpace(apiB) = %q, %v; want team", space, err)
	}
	if space, err := got.DefaultSpace(apiA); err != nil || space != "notes" {
		t.Fatalf("DefaultSpace(apiA) = %q, %v; want notes", space, err)
	}
	b, err := got.ForEndpoint(apiB)
	if err != nil || b.Key != testToken2 || b.Access != AccessRead || !b.Expires.Never() || b.Account != "bob@example.test" {
		t.Fatalf("ForEndpoint(apiB) = %+v, %v; want the read-only login without expiry", b, err)
	}
	a, err := got.Lookup(ID{Site: site, Endpoint: apiA})
	if err != nil || a.Key != testToken || !a.Expires.Time().Equal(expiring.Expires.Time()) || a.Account != "ada@example.test" {
		t.Fatalf("Lookup(apiA) = %+v, %v; want the stored login", a, err)
	}
	if a.Expires.Describe() != "until 2026-12-26 09:30 UTC" {
		t.Fatalf("Describe() = %q", a.Expires.Describe())
	}
}

func TestPutReplacesAndRemoveClearsDefault(t *testing.T) {
	var set Set
	if err := set.SetDefaultSpace(apiA, "notes"); !errors.Is(err, ErrNoLogin) {
		t.Fatalf("SetDefaultSpace() without a login = %v, want ErrNoLogin", err)
	}
	set.Put(login(apiA, testToken, AccessWrite))
	if _, err := set.DefaultSpace(apiA); !errors.Is(err, ErrNoDefault) {
		t.Fatalf("DefaultSpace() after Put = %v, want ErrNoDefault: Put never chooses the default", err)
	}
	if err := set.SetDefaultSpace(apiA, "No_Space"); err == nil {
		t.Fatal("SetDefaultSpace(invalid name) = nil, want an error")
	}
	if err := set.SetDefaultSpace(apiA, "notes"); err != nil {
		t.Fatalf("SetDefaultSpace(notes) = %v", err)
	}
	other := login(apiA, testToken, AccessWrite)
	other.Site = "https://other.example.test"
	set.Put(other)
	old, err := set.Put(login(apiA, testToken2, AccessRead))
	if err != nil || old.Key != testToken {
		t.Fatalf("Put(replacement) = %+v, %v; want the replaced login", old, err)
	}
	if n := len(set.Logins()); n != 2 {
		t.Fatalf("Logins() = %d entries, want 2", n)
	}
	if space, _ := set.DefaultSpace(apiA); space != "notes" {
		t.Fatalf("DefaultSpace() = %q, want notes kept across a replacement", space)
	}
	if err := set.Remove(other.ID); err != nil {
		t.Fatalf("Remove(other) = %v", err)
	}
	if _, err := set.DefaultSpace(apiA); err != nil {
		t.Fatal("removing one of two logins for the endpoint cleared its default")
	}
	if err := set.Remove(ID{Site: site, Endpoint: apiA}); err != nil {
		t.Fatalf("Remove() = %v", err)
	}
	if _, err := set.DefaultSpace(apiA); !errors.Is(err, ErrNoDefault) {
		t.Fatalf("DefaultSpace() after removing the last login = %v, want ErrNoDefault", err)
	}
	if err := set.Remove(ID{Site: site, Endpoint: apiA}); !errors.Is(err, ErrNoLogin) {
		t.Fatalf("Remove(missing) = %v, want ErrNoLogin", err)
	}
	set.Put(login(apiA, testToken, AccessWrite))
	if err := set.SetDefaultSpace(apiA, "notes"); err != nil {
		t.Fatal(err)
	}
	set.ClearDefaultSpace(apiA)
	if _, err := set.DefaultSpace(apiA); !errors.Is(err, ErrNoDefault) {
		t.Fatalf("DefaultSpace() after ClearDefaultSpace = %v, want ErrNoDefault", err)
	}
}

func TestChoosingALogin(t *testing.T) {
	var set Set
	set.Put(login(apiA, testToken, AccessWrite))
	if l, err := set.Only(); err != nil || l.Endpoint != apiA {
		t.Fatalf("Only() = %+v, %v; want the only login", l, err)
	}
	set.Put(login(apiB, testToken2, AccessWrite))
	if _, err := set.Only(); !errors.Is(err, ErrAmbiguous) || !strings.Contains(err.Error(), apiB) {
		t.Fatalf("Only() with two logins = %v, want ErrAmbiguous naming the endpoints", err)
	}
	if l, err := set.ForEndpoint(apiB); err != nil || l.Key != testToken2 {
		t.Fatalf("ForEndpoint(apiB) = %+v, %v", l, err)
	}
	other := login(apiA, testToken2, AccessWrite)
	other.Site = "https://other.example.test"
	set.Put(other)
	_, err := set.ForEndpoint(apiA)
	if !errors.Is(err, ErrAmbiguous) || !strings.Contains(err.Error(), other.Site) || strings.Contains(err.Error(), testToken) {
		t.Fatalf("ForEndpoint(apiA) with two sites = %v, want ErrAmbiguous naming the sites and not a key", err)
	}
}

func TestUsable(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	l := login(apiA, testToken, AccessWrite)
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
		t.Fatalf("Usable(expired) = %v, want ErrExpired naming the date and not the key", err)
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

func writeRaw(t *testing.T, f File, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(f.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.Path(), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRefusesMalformedFiles(t *testing.T) {
	entry := `{"site":"https://www.slivingdoc.dev","endpoint":"https://api.slivingdoc.dev","key":"` + testToken + `","access":"write","account":"ada@example.test"}`
	with := func(old, new string) string {
		return `{"version":2,"logins":[` + strings.Replace(entry, old, new, 1) + `]}`
	}
	def := func(d string) string { return `{"version":2,"logins":[` + entry + `],"defaultSpaces":[` + d + `]}` }
	tests := []struct {
		name string
		data string
	}{
		{"not json", `{`},
		{"array", `[]`},
		{"unknown top field", `{"version":2,"logins":[],"extra":1}`},
		{"missing version", `{"logins":[]}`},
		{"newer version", `{"version":3,"logins":[]}`},
		{"missing logins", `{"version":2}`},
		{"duplicate key", `{"version":2,"version":2,"logins":[]}`},
		{"login not object", `{"version":2,"logins":[1]}`},
		{"unknown login field", with(`"access"`, `"space":"notes","access"`)},
		{"missing key", with(`"key":"`+testToken+`",`, "")},
		{"key with space", with(testToken, "sld bad")},
		{"numeric key", with(`"`+testToken+`"`, "5")},
		{"bad access", with(`"write"`, `"admin"`)},
		{"missing account", with(`,"account":"ada@example.test"`, "")},
		{"empty account", with(`"ada@example.test"`, `""`)},
		{"bad expiry", with(`"write"`, `"write","expiresAt":"tomorrow"`)},
		{"http remote endpoint", with("https://api", "http://api")},
		{"http remote site", with("https://www", "http://www")},
		{"endpoint with user information", with("https://api", "https://user:secret@api")},
		{"endpoint with a query", with("api.slivingdoc.dev", "api.slivingdoc.dev/?k=secret")},
		{"endpoint with a fragment", with("api.slivingdoc.dev", "api.slivingdoc.dev/#secret")},
		{"endpoint with an empty query", with("api.slivingdoc.dev", "api.slivingdoc.dev?")},
		{"endpoint with an empty fragment", with("api.slivingdoc.dev", "api.slivingdoc.dev#")},
		{"site with user information", with("https://www", "https://user:secret@www")},
		{"site with a query", with("www.slivingdoc.dev", "www.slivingdoc.dev?secret")},
		{"site with a fragment", with("www.slivingdoc.dev", "www.slivingdoc.dev#secret")},
		{"duplicate login", `{"version":2,"logins":[` + entry + `,` + entry + `]}`},
		{"defaults not array", `{"version":2,"logins":[` + entry + `],"defaultSpaces":{}}`},
		{"default not object", def(`"notes"`)},
		{"default with extra field", def(`{"endpoint":"https://api.slivingdoc.dev","space":"notes","x":1}`)},
		{"default without space", def(`{"endpoint":"https://api.slivingdoc.dev"}`)},
		{"default with a bad space", def(`{"endpoint":"https://api.slivingdoc.dev","space":"No_Space"}`)},
		{"default without login", def(`{"endpoint":"https://api.dev.slivingdoc.dev","space":"notes"}`)},
		{"duplicate default", def(`{"endpoint":"https://api.slivingdoc.dev","space":"notes"},{"endpoint":"https://api.slivingdoc.dev","space":"team"}`)},
		{"default endpoint with user information", def(`{"endpoint":"https://user:secret@api.slivingdoc.dev","space":"notes"}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := testFile(t)
			writeRaw(t, f, tt.data)
			_, err := f.Load()
			if !errors.Is(err, ErrMalformed) {
				t.Fatalf("Load() = %v, want ErrMalformed", err)
			}
			if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "sld bad") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("Load() = %q echoes the key", err)
			}
		})
	}
}

func TestLoadRefusesAnEarlierFile(t *testing.T) {
	f := testFile(t)
	writeRaw(t, f, `{"version":1,"logins":[{"endpoint":"https://api.slivingdoc.dev","space":"notes",`+
		`"site":"https://www.slivingdoc.dev","token":"`+testToken+`","access":"write"},`+
		`{"space":"team","site":"https://www.slivingdoc.dev","token":"`+testToken+`"},`+
		`{"site":"https://user:pw@evil.example","token":"`+testToken+`"},{"site":"https://www.slivingdoc.dev","token":"sld bad"},`+
		`{"token":"`+testToken+`"},"not an object"],"default":{"x":1}}`)
	_, err := f.Load()
	if !errors.Is(err, ErrOutdated) || errors.Is(err, ErrMalformed) || !strings.Contains(err.Error(), "slivingdoc login") ||
		!strings.Contains(err.Error(), f.Path()) || strings.Contains(err.Error(), testToken) {
		t.Fatalf("Load() of a version 1 file = %v, want ErrOutdated naming login and the file, not the token", err)
	}
	// The tokens come back for revocation: each sendable one with a plain
	// site once; an entry without either is skipped.
	var outdated *OutdatedFileError
	if !errors.As(err, &outdated) || outdated.Path != f.Path() ||
		!slices.Equal(outdated.Tokens, []OutdatedToken{{Site: "https://www.slivingdoc.dev", Token: testToken}}) {
		t.Fatalf("Load() of a version 1 file = %#v, want its one sendable token", err)
	}
	writeRaw(t, f, `{"version":1}`)
	if _, err := f.Load(); !errors.As(err, &outdated) || len(outdated.Tokens) != 0 {
		t.Fatalf("Load() of a version 1 file without logins = %v, want ErrOutdated and no tokens", err)
	}
}

func TestLoadAcceptsLoopbackHTTP(t *testing.T) {
	f := testFile(t)
	writeRaw(t, f, `{"version":2,"defaultSpaces":[{"endpoint":"http://127.0.0.1:8787","space":"notes"}],"logins":[`+
		`{"endpoint":"http://127.0.0.1:8787","site":"http://localhost:8788","key":"`+testToken+
		`","access":"read","account":"ada@example.test","expiresAt":"2030-01-02T03:04:05Z"}]}`)
	set, err := f.Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	l, err := set.Only()
	if err != nil || l.Site != "http://localhost:8788" || l.Access != AccessRead || l.Expires.Never() {
		t.Fatalf("Only() = %+v, %v", l, err)
	}
	if space, err := set.DefaultSpace("http://127.0.0.1:8787"); err != nil || space != "notes" {
		t.Fatalf("DefaultSpace() = %q, %v", space, err)
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
	set.Put(login(apiA, testToken, AccessWrite))
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

func TestChecksOwnersNeverOnWindows(t *testing.T) {
	if checksOwners("windows") {
		t.Fatal("a Windows file checks POSIX owners")
	}
}
