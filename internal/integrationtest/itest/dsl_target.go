package itest

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/httpstore/gatewaytest"
	"github.com/baalimago/slivingdoc/internal/settings"
	"github.com/baalimago/slivingdoc/internal/sitelogin/sitetest"
)

// recorded is one directory's remembered target, keyed by the notebook path
// the command resolved.
func Recorded(path, space string) settings.Entry {
	return settings.Entry{Path: path, Target: settings.Target{Kind: settings.KindHosted, Name: space, Prefix: HostedPrefix}}
}

// assertRecords proves the settings file of env's processes holds exactly
// these entries, in this order.
func AssertRecords(t *testing.T, env []string, want ...settings.Entry) {
	t.Helper()
	set, err := SettingsFile(t, env).Load()
	if err != nil {
		t.Fatalf("load the settings file: %v", err)
	}
	if got := set.Entries(); !slices.Equal(got, want) {
		t.Fatalf("the settings file holds %v, want %v", got, want)
	}
}

// assertRecordedFileNamesNoCredential proves the recorded file names no
// value that opens the notebook: no account key, no token, no account and no
// site endpoint. The version-2 field names are not values, and the hosted
// target's endpoint is empty in this build.
func AssertRecordedFileNamesNoCredential(t *testing.T, env []string) {
	t.Helper()
	body := strings.ToLower(ReadSettings(t, env))
	for _, forbidden := range []string{"sld_", strings.ToLower(LoginKey), strings.ToLower(SecondKey), "@", "http"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("the settings file = %s, want no %q in it", ReadSettings(t, env), forbidden)
		}
	}
	if !strings.Contains(body, `"kind":"hosted","endpoint":""`) {
		t.Fatalf("the settings file = %s, want a hosted target with an empty endpoint", body)
	}
}

// secondSpace is another space of the login's account, so a resolved space
// tells the remembered source apart from the login's default space.
const SecondSpace = "second-space"

// rememberedEnv starts the reference storage API with the login's default
// space and a second one, a reference site whose minted tokens the gateway
// grants, and a stored login for that account whose default space is the
// first one. The configuration directory holds the credentials file and the
// scoped settings file beside each other, so one environment selects both
// (architecture/config.md, The scoped settings store).
func RememberedEnv(t *testing.T) (*gatewaytest.Gateway, *sitetest.Site, []string, string) {
	t.Helper()
	g, site, env, root := LoginEnv(t)
	g.AddSpace(SecondSpace, 1<<20)
	Approve(site, g, LoginKey, "write", LoginExpiry)
	RunLogin(t, env, site, LoggedIn(g, "read and write", WithDefault), "--no-browser")
	site.SetSpaces(LoginKey, TeamNotes, SecondTeamNotes)
	return g, site, env, root
}

// secondTeamNotes is the second space of the login's account, whose id the
// gateway and the site share.
var SecondTeamNotes = sitetest.Space{Name: SecondSpace, Owner: LoginAccount, Access: "write"}

// remember plants one entry in the settings file of env's processes: the
// hosted notebook a directory holds. It answers the bytes it wrote, so a
// scenario can prove a later command left the file untouched.
func Remember(t *testing.T, env []string, path, space, prefix string) string {
	t.Helper()
	file := SettingsFile(t, env)
	set, err := file.Load()
	if err != nil {
		t.Fatalf("load the settings file: %v", err)
	}
	entry := settings.Entry{Path: path, Target: settings.Target{Kind: settings.KindHosted, Name: space, Prefix: prefix}}
	if err := file.Save(set.Put(entry)); err != nil {
		t.Fatalf("save the settings file: %v", err)
	}
	return ReadSettings(t, env)
}

// settingsFile is the scoped settings file of env's processes, beside the
// credentials file of the same configuration directory.
func SettingsFile(t *testing.T, env []string) settings.File {
	t.Helper()
	file, err := settings.Locate(func(string) string { return CredentialsDir(env) }, runtime.GOOS)
	if err != nil {
		t.Fatalf("locate the settings file: %v", err)
	}
	return file
}

// settingsPath is the settings file of env's processes.
func SettingsPath(env []string) string { return filepath.Join(CredentialsDir(env), settings.FileName) }

// plantSettings writes the settings file of env's processes exactly as
// given, for the rows that need bytes this build cannot read.
func PlantSettings(t *testing.T, env []string, body string) {
	t.Helper()
	path := SettingsPath(env)
	WriteCLIFile(t, path, body)
	MustDo(t, os.Chmod(path, 0o600))
}

// readSettings is the settings file of env's processes as it stands.
func ReadSettings(t *testing.T, env []string) string {
	t.Helper()
	data, err := os.ReadFile(SettingsPath(env))
	if err != nil {
		t.Fatalf("read the settings file: %v", err)
	}
	return string(data)
}

// assertRefusal is the black-box refusal contract of a startup: a nonzero
// exit, nothing on stdout, and every cause and fix the row names in stderr.
func AssertRefusal(t *testing.T, args []string, code int, stdout, stderr string, want ...string) {
	t.Helper()
	if code != 1 || strings.TrimSpace(stdout) != "" {
		t.Fatalf("%v = exit %d, stdout %q; want a startup refusal", args, code, stdout)
	}
	for _, want := range want {
		if !strings.Contains(stderr, want) {
			t.Fatalf("%v stderr = %q, want it to contain %q", args, stderr, want)
		}
	}
}

// assertReachedSpace proves which space a process really used: the last
// token the site minted was for it.
func AssertReachedSpace(t *testing.T, site *sitetest.Site, space string) {
	t.Helper()
	mints := site.Mints()
	if len(mints) == 0 {
		t.Fatal("the process reached the site without minting a token")
	}
	if last := mints[len(mints)-1].Space; last != space {
		t.Fatalf("the last minted token is for space %q, want %q", last, space)
	}
}

// reach is how far every process of a scenario has got: the spaces the site
// minted for and the requests the gateway served. A refusal must not change
// it.
func Reach(g *gatewaytest.Gateway, site *sitetest.Site) string {
	spaces := make([]string, 0, len(site.Mints()))
	for _, m := range site.Mints() {
		spaces = append(spaces, m.Space)
	}
	return fmt.Sprintf("mints=%s requests=%d", strings.Join(spaces, ","), g.Requests())
}

// expireStoredLogin replaces the stored login of env with one whose key has
// expired, so the next process refuses it before any store is reached.
func ExpireStoredLogin(t *testing.T, g *gatewaytest.Gateway, site *sitetest.Site, env []string) {
	t.Helper()
	past := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	Approve(site, g, LoginKey, "write", past)
	RunLogin(t, env, site, LoggedIn(g, "read and write", `until 2001-01-01 00:00 UTC; default space "team-notes"`), "--no-browser")
	site.SetSpaces(LoginKey, TeamNotes, SecondTeamNotes)
}

// s3SettingsFile is a version-2 settings file with no entries and the given
// S3 defaults, in the order an earlier build stored them.
func S3SettingsFile(defaults ...settings.Target) string {
	var b strings.Builder
	b.WriteString(`{"version":2,"entries":[],"defaults":[`)
	for i, t := range defaults {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"kind":%q,"endpoint":%q,"region":%q,"profile":%q,"name":%q,"prefix":%q,"pathStyle":%t}`,
			t.Kind, t.Endpoint, t.Region, t.Profile, t.Name, t.Prefix, t.PathStyle)
	}
	b.WriteString(`]}`)
	return b.String()
}

// loadSettings reads the settings file of env's processes.
func LoadSettings(t *testing.T, env []string) settings.Set {
	t.Helper()
	set, err := SettingsFile(t, env).Load()
	if err != nil {
		t.Fatalf("load the settings file: %v", err)
	}
	return set
}
