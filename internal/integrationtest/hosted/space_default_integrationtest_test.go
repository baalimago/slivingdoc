package hosted

// The one global default of `space` seen by a process: a hosted selection
// clears every stored S3 default, an S3 selection replaces it with one
// complete target, only the active default carries the mark, a create
// changes nothing, a legacy version-2 file keeps working, a failed write
// keeps the previous default, and no output or stored target carries a
// secret (architecture/login.md, architecture/config.md).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/baalimago/slivingdoc/internal/settings"
	"github.com/baalimago/slivingdoc/internal/tests3"
)

// TestScenarioSpaceActiveMark proves exactly one default is active: the S3
// default beside a stored hosted default carries the mark, and the S3 group
// is listed with the default's own endpoint, region, and profile rather than
// the ambient AWS identity.
func TestScenarioSpaceActiveMark(t *testing.T) {
	t.Parallel()
	suite := tests3.Ensure(t)
	g, site, env, _ := LoginEnv(t)
	Approve(site, g, LoginKey, "write", LoginExpiry)
	RunLogin(t, env, site, LoggedIn(g, "read and write", WithDefault), "--no-browser")
	env = append(S3Credentials(env), "SLIVINGDOC_PREFIX=")

	name := NewS3Bucket(t, suite, "space-active-mark")
	MustDo(t, os.MkdirAll(CredentialsDir(env), 0o700))
	PlantSettings(t, env, S3SettingsFile(S3DefaultTarget(suite, name)))

	code, stdout, stderr := RunCLI(t, "real", env, "space")
	if code != 0 {
		t.Fatalf("space = exit %d, want 0; stderr: %s", code, stderr)
	}
	header := "HOSTED  " + g.URL() + "  " + LoginAccount + "\n  team-notes  write  " + LoginAccount + "\n" +
		"\nS3  endpoint=" + suite.Endpoint + "  region=" + tests3.Region + "  profile=(ambient)  key=" + tests3.User + "\n"
	if !strings.HasPrefix(stdout, header) {
		t.Fatalf("stdout = %q, want it to begin with %q", stdout, header)
	}
	if !strings.Contains(stdout, "* "+name+"\n") {
		t.Fatalf("stdout = %q, want the S3 default %q marked", stdout, name)
	}
	if got := strings.Count(stdout, "* "); got != 1 {
		t.Fatalf("stdout = %q, want exactly one marked row, got %d", stdout, got)
	}
}

// TestScenarioSpaceNewKeepsDefault proves a create -- hosted, S3, and the
// bare create form -- changes no default and never writes the settings file.
func TestScenarioSpaceNewKeepsDefault(t *testing.T) {
	t.Parallel()
	suite := tests3.Ensure(t)
	g, site, env, _ := LoginEnv(t)
	Approve(site, g, LoginKey, "write", LoginExpiry)
	RunLogin(t, env, site, LoggedIn(g, "read and write", WithDefault), "--no-browser")
	env = append(S3Credentials(env), "SLIVINGDOC_PREFIX=")

	RunCLIExact(t, "real", env,
		"Created hosted space research (write, ada@example.test) at "+g.URL()+".\n"+
			"It is not the default; run 'slivingdoc space research' to make it the default.\n",
		"space", "new", "research")

	name := FreshS3Bucket(suite, "space-new-s3")
	t.Cleanup(func() {
		_, _ = suite.Raw.DeleteBucket(context.Background(), &awss3.DeleteBucketInput{Bucket: aws.String(name)})
	})
	flags := []string{"space", "--storage", "s3", "--endpoint", suite.Endpoint, "--region", tests3.Region}
	RunCLIExact(t, "real", env,
		"Created bucket "+name+" (endpoint="+suite.Endpoint+", region="+tests3.Region+", profile=(ambient)).\n"+
			"It is not the default; run 'slivingdoc space --storage s3 --endpoint "+suite.Endpoint+
			" --region "+tests3.Region+" "+name+"' to make it the default.\n",
		append(append([]string(nil), flags...), "new", name)...)

	code, stdout, stderr := RunCLI(t, "real", env, "space", "new")
	if code != 1 || stdout != "" {
		t.Fatalf("space new = exit %d, stdout %q; want a refusal", code, stdout)
	}
	if want := "error: space new: name the space to create"; !strings.Contains(stderr, want) {
		t.Fatalf("space new stderr = %q, want it to contain %q", stderr, want)
	}

	if _, err := os.Stat(SettingsPath(env)); !os.IsNotExist(err) {
		t.Fatalf("settings file after creates = %v, want none", err)
	}
	code, stdout, stderr = RunCLI(t, "real", env, "space")
	if code != 0 {
		t.Fatalf("space = exit %d, want 0; stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "HOSTED  "+g.URL()+"  "+LoginAccount+"\n") {
		t.Fatalf("stdout = %q, want the hosted group header", stdout)
	}
	for _, row := range []string{"* team-notes", "  research"} {
		if !strings.Contains(stdout, row+"  write  "+LoginAccount+"\n") {
			t.Fatalf("stdout = %q, want the row %q", stdout, row)
		}
	}
	if got := strings.Count(stdout, "* "); got != 1 {
		t.Fatalf("stdout = %q, want exactly one marked row, got %d", stdout, got)
	}
}

// TestScenarioSpaceSettingsFailures proves a settings file this build cannot
// read is refused and left untouched, and a failed default write exits
// nonzero with the previous default still active.
func TestScenarioSpaceSettingsFailures(t *testing.T) {
	t.Parallel()
	suite := tests3.Ensure(t)

	t.Run("unreadable file", func(t *testing.T) {
		env, _ := CliRoots(t)
		env = append(env, "SLIVINGDOC_CONFIG_DIR="+filepath.Join(t.TempDir(), "cfg"))
		PlantSettings(t, env, `{"version":2}`)
		before := ReadSettings(t, env)

		code, stdout, stderr := RunCLI(t, "real", env, "space")
		if code != 1 || stdout != "" {
			t.Fatalf("space = exit %d, stdout %q; want a refusal", code, stdout)
		}
		if want := "settings: malformed settings file"; !strings.Contains(stderr, want) {
			t.Fatalf("space stderr = %q, want it to contain %q", stderr, want)
		}
		if after := ReadSettings(t, env); after != before {
			t.Fatalf("settings file = %q, want the untouched %q", after, before)
		}
	})

	t.Run("failed write", func(t *testing.T) {
		env := S3DefaultEnv(t)
		old := NewS3Bucket(t, suite, "space-old-default")
		next := NewS3Bucket(t, suite, "space-next-default")
		MustDo(t, os.MkdirAll(CredentialsDir(env), 0o700))
		PlantSettings(t, env, S3SettingsFile(S3DefaultTarget(suite, old)))
		MustDo(t, os.MkdirAll(filepath.Join(CredentialsDir(env), settings.LockName), 0o700))

		code, stdout, stderr := RunCLI(t, "real", env, "space", "--storage", "s3", next)
		if code != 1 || stdout != "" {
			t.Fatalf("space <name> = exit %d, stdout %q; want the write refusal", code, stdout)
		}
		if want := "settings: lock "; !strings.Contains(stderr, want) {
			t.Fatalf("space stderr = %q, want it to contain %q", stderr, want)
		}
		if got := LoadSettings(t, env).Defaults(); len(got) != 1 || got[0].Name != old {
			t.Fatalf("defaults = %+v; a failed write must leave the previous default", got)
		}
		_, stdout, stderr = RunCLI(t, "real", env, "space", "--storage", "s3")
		if !strings.Contains(stdout, "* "+old+"\n") || strings.Contains(stdout, "* "+next+"\n") {
			t.Fatalf("stdout = %q, stderr = %q; want only %q marked", stdout, stderr, old)
		}
		if got := strings.Count(stdout, "* "); got != 1 {
			t.Fatalf("stdout = %q, want exactly one marked row, got %d", stdout, got)
		}
	})
}

// TestScenarioSpaceLegacyDefaults proves a version-2 file written by the
// earlier per-store-key rule stays readable: its last default is active, and
// the next successful selection collapses them to one.
func TestScenarioSpaceLegacyDefaults(t *testing.T) {
	t.Parallel()
	suite := tests3.Ensure(t)
	env := S3DefaultEnv(t)
	first := NewS3Bucket(t, suite, "space-legacy-first")
	last := NewS3Bucket(t, suite, "space-legacy-last")
	MustDo(t, os.MkdirAll(CredentialsDir(env), 0o700))
	PlantSettings(t, env, S3SettingsFile(S3DefaultTarget(suite, first), S3DefaultTarget(suite, last)))

	header := "S3  endpoint=" + suite.Endpoint + "  region=" + tests3.Region + "  profile=(ambient)  key=" + tests3.User + "\n"
	code, stdout, stderr := RunCLI(t, "real", env, "space")
	if code != 0 {
		t.Fatalf("space = exit %d, want 0; stderr: %s", code, stderr)
	}
	if !strings.HasPrefix(stdout, header) || !strings.Contains(stdout, "* "+last+"\n") ||
		!strings.Contains(stdout, "  "+first+"\n") || strings.Contains(stdout, "* "+first+"\n") {
		t.Fatalf("stdout = %q, want only the last default %q marked", stdout, last)
	}
	if got := strings.Count(stdout, "* "); got != 1 {
		t.Fatalf("stdout = %q, want exactly one marked row, got %d", stdout, got)
	}
	RunCLIExact(t, "real", env,
		"The default bucket for s3 endpoint="+suite.Endpoint+", region="+tests3.Region+", profile=(ambient) is now "+first+".\n",
		"space", first)

	if got := LoadSettings(t, env).Defaults(); len(got) != 1 || got[0].Name != first {
		t.Fatalf("defaults = %+v; the selection must collapse every stored default to one", got)
	}
	code, stdout, stderr = RunCLI(t, "real", env, "space")
	if code != 0 || !strings.Contains(stdout, "* "+first+"\n") || strings.Contains(stdout, "* "+last+"\n") ||
		!strings.Contains(stdout, "  "+last+"\n") {
		t.Fatalf("space = exit %d, stdout %q, stderr %q; want only %q marked", code, stdout, stderr, first)
	}
}

// TestScenarioSpaceRedaction proves a stored S3 default and a stored login
// never put a credential, a login key, a minted token, or a private path into
// the settings file, stdout, or a log record. The S3 access key id is the
// one deliberate display (architecture/security.md); the secret is not.
func TestScenarioSpaceRedaction(t *testing.T) {
	t.Parallel()
	suite := tests3.Ensure(t)
	g, site, env, _ := LoginEnv(t)
	Approve(site, g, LoginKey, "write", LoginExpiry)
	RunLogin(t, env, site, LoggedIn(g, "read and write", WithDefault), "--no-browser")
	env = append(S3Credentials(env), "LOG_LEVEL=debug")

	name := NewS3Bucket(t, suite, "space-redaction")
	MustDo(t, os.MkdirAll(CredentialsDir(env), 0o700))
	PlantSettings(t, env, S3SettingsFile(S3DefaultTarget(suite, name)))

	code, stdout, stderr := RunCLI(t, "real", env, "space")
	if code != 0 {
		t.Fatalf("space = exit %d, want 0; stderr: %s", code, stderr)
	}
	stored := ReadSettings(t, env)
	forbidden := map[string]string{
		"the S3 secret": tests3.Pass,
		"the login key": LoginKey,
	}
	for _, m := range site.Mints() {
		forbidden["a minted token"] = m.Token
	}
	for name, value := range forbidden {
		if strings.Contains(stdout, value) || strings.Contains(stderr, value) || strings.Contains(stored, value) {
			t.Fatalf("%s reached stdout, a log record, or the settings file", name)
		}
	}
	for _, secret := range []string{CredentialsDir(env), PrivateRootOf(env)} {
		if secret == "" {
			continue
		}
		if strings.Contains(stdout, secret) || strings.Contains(stderr, secret) || strings.Contains(stored, secret) {
			t.Fatalf("the private path %q reached stdout, a log record, or the settings file", secret)
		}
	}
	if strings.Contains(stdout, "sld_") || strings.Contains(stderr, "sld_") || strings.Contains(stored, "sld_") {
		t.Fatal("a login key or minted token reached stdout, a log record, or the settings file")
	}
}
