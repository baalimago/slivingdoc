package hosted

// The backend-aware binding of a directory: one target per directory on
// either backend, the one global default, and the effective values a built
// S3 store reports. The scenarios drive the released command surface and
// observe the settings file, the store, and the visible notebook
// (architecture/config.md, architecture/cli.md).

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/httpstore/gatewaytest"
	"github.com/baalimago/slivingdoc/internal/settings"
	"github.com/baalimago/slivingdoc/internal/tests3"
)

// TestScenarioRecordFirstTarget proves the gate of the recorder over both
// backends: a directory that remembers nothing records the complete target
// a flag or a variable named, while a target the global default supplied and
// a token's own space are answers for one run and are never recorded (I3,
// I4, I6, I7).
func TestScenarioRecordFirstTarget(t *testing.T) {
	t.Parallel()
	suite := tests3.Ensure(t)
	bucket := tests3.Bucket
	prefix := suite.FreshPrefix("record-first")
	configDir, privateRoot, root := BindingDir(t), t.TempDir(), t.TempDir()
	env := BindingEnv(t, configDir, privateRoot, root)
	notes := filepath.Join(root, "notes")

	target := S3Target(suite, bucket, prefix)
	RunCLIOK(t, "real", env, nil, "pull",
		"--bucket", bucket, "--endpoint", suite.Endpoint, "--region", tests3.Region,
		"--prefix", prefix, "--path-style", notes)
	want := settings.Entry{Path: notes, Target: target}
	AssertRecords(t, env, want)

	// The global default names the next directory's target, and that run
	// records nothing.
	before := PlantTarget(t, env, nil, &target)
	fromDefault := filepath.Join(root, "from-default")
	RunCLIOK(t, "real", env, nil, "pull", fromDefault)
	if after := ReadSettings(t, env); after != before {
		t.Fatalf("the settings file after a default-named pull = %s, want it unchanged (%s)", after, before)
	}

	// A token names its own space for one run, so no S3 default and no
	// record may pin it to the directory.
	g := gatewaytest.Start(t)
	g.AddSpace(HostedSpace, 1<<20)
	g.Grant(HostedToken, HostedSpace, false)
	tokenEnv := append(append([]string(nil), env...),
		"SLIVINGDOC_TOKEN="+HostedToken, "SLIVINGDOC_ENDPOINT="+g.URL())
	fromToken := filepath.Join(root, "from-token")
	RunCLIOK(t, "real", tokenEnv, nil, "pull", fromToken)
	if after := ReadSettings(t, env); after != before {
		t.Fatalf("the settings file after a token pull = %s, want it unchanged (%s)", after, before)
	}
	AssertRecords(t, env, want)
}

// TestScenarioBareCommandUsesTheS3Default proves the one global default is
// a complete target: a directory that remembers nothing reaches the stored
// bucket with no S3 identity variable, and the endpoint, prefix and path
// style all come from the default (I5).
func TestScenarioBareCommandUsesTheS3Default(t *testing.T) {
	t.Parallel()
	suite := tests3.Ensure(t)
	bucket := tests3.Bucket
	prefix := suite.FreshPrefix("default-complete")
	configDir, root := BindingDir(t), t.TempDir()
	env := BindingEnv(t, configDir, t.TempDir(), root)
	target := S3Target(suite, bucket, prefix)
	before := PlantTarget(t, env, nil, &target)

	notes := filepath.Join(root, "notes")
	RunCLIOK(t, "real", env, nil, "pull", notes)
	WriteCLIFile(t, filepath.Join(notes, "a.md"), "the default's note\n")
	RunCLIOK(t, "real", env, nil, "commit", notes, "-m", "default target")
	if after := ReadSettings(t, env); after != before {
		t.Fatalf("the settings file after a default-sourced pull = %s, want it unchanged (%s)", after, before)
	}
	if keys := S3Keys(t, suite, bucket, prefix); !slices.Contains(keys, prefix+"/current") {
		t.Fatalf("objects below %q = %v, want the accepted state at the default's prefix", prefix, keys)
	}
	if entries := LoadSettings(t, env).Entries(); len(entries) != 0 {
		t.Fatalf("records = %v, want a default-named run to record nothing", entries)
	}
}

// TestScenarioRecordWriteFailures proves the recorder never changes an
// operation: a pull that fails at the store and a pull whose target came
// from the record write nothing, and a successful pull whose record write
// fails still prints its OK line while the failure goes to stderr (I3).
func TestScenarioRecordWriteFailures(t *testing.T) {
	t.Parallel()
	t.Run("a pull refused at the store", func(t *testing.T) {
		t.Parallel()
		configDir := BindingDir(t)
		env, _ := CliRoots(t)
		env = append(env, credentials.DirEnv+"="+configDir)
		PlantSettings(t, env, `{"version":2,"entries":[],"defaults":[]}`)
		before := ReadSettings(t, env)
		args := []string{"pull", filepath.Join(t.TempDir(), "notes")}
		code, stdout, stderr := RunCLI(t, "bad-store", env, args...)
		if code != 1 || strings.Contains(stdout, "OK") {
			t.Fatalf("%v = exit %d, stdout %q, stderr %q; want the store refusal", args, code, stdout, stderr)
		}
		if after := ReadSettings(t, env); after != before {
			t.Fatalf("the settings file after a store refusal = %s, want it unchanged (%s)", after, before)
		}
	})

	t.Run("a target from the record", func(t *testing.T) {
		t.Parallel()
		_, _, env, root := RememberedEnv(t)
		notes := filepath.Join(root, "notes")
		before := Remember(t, env, notes, SecondSpace, HostedPrefix)
		RunCLIOK(t, "real", env, nil, "pull", notes)
		if after := ReadSettings(t, env); after != before {
			t.Fatalf("the settings file after a record-sourced pull = %s, want it unchanged (%s)", after, before)
		}
	})

	t.Run("a successful pull whose record write fails", func(t *testing.T) {
		t.Parallel()
		_, _, env, root := RememberedEnv(t)
		configDir := CredentialsDir(env)
		MustDo(t, os.Chmod(configDir, 0o500))
		t.Cleanup(func() { _ = os.Chmod(configDir, 0o700) })
		notes := filepath.Join(root, "notes")
		code, stdout, stderr := RunCLI(t, "real", env, "pull", "--space", SecondSpace, notes)
		if code != 0 || !strings.HasPrefix(stdout, "OK  generation ") {
			t.Fatalf("pull with an unwritable record = exit %d, stdout %q, stderr %q; want the OK line", code, stdout, stderr)
		}
		if !strings.Contains(stderr, "remembering the notebook failed") {
			t.Fatalf("pull stderr = %q, want the failed record write named", stderr)
		}
		if _, err := os.Stat(SettingsPath(env)); !os.IsNotExist(err) {
			t.Fatalf("Stat(settings) = %v, want no record after a failed write", err)
		}
	})
}

// TestScenarioResolvedEffectiveValues proves the effective values of a
// built store feed the notebook identity: a record's profile supplies a
// region no flag named, so the directory's identity changes once, the fresh
// private state imports the notebook's packs again, every note survives,
// and the profile appears in the record only (I9).
func TestScenarioResolvedEffectiveValues(t *testing.T) {
	t.Parallel()
	suite := tests3.Ensure(t)
	bucket := tests3.Bucket
	prefix := suite.FreshPrefix("resolved-values")
	configDir, privateRoot, root := BindingDir(t), t.TempDir(), t.TempDir()
	env := BindingEnv(t, configDir, privateRoot, root)
	notes := filepath.Join(root, "notes")

	// The first run names a region the profile will not supply, so the
	// directory starts under one identity and the record remembers its own
	// target.
	first := []string{
		"--bucket", bucket, "--endpoint", suite.Endpoint, "--region", "eu-north-1",
		"--prefix", prefix, "--path-style",
	}
	RunCLIOK(t, "real", env, nil, append([]string{"pull"}, append(first, notes)...)...)
	WriteCLIFile(t, filepath.Join(notes, "a.md"), "the surviving note\n")
	RunCLIOK(t, "real", env, nil, "commit", notes, "-m", "first identity")
	if got := IdentityDirs(t, privateRoot); len(got) != 1 {
		t.Fatalf("private identities after the first run = %v, want one", got)
	}

	// The record supplies the profile and no region; the profile's region
	// then reaches the store, the identity, and neither the record nor the
	// proof.
	profile := "region-profile"
	record := settings.Entry{Path: notes, Target: settings.Target{
		Kind: settings.KindS3, Endpoint: suite.Endpoint, Profile: profile,
		Name: bucket, Prefix: prefix, PathStyle: true,
	}}
	before := PlantTarget(t, env, &record, nil)
	shared := t.TempDir()
	config := filepath.Join(shared, "config")
	WriteCLIFile(t, config, "[profile "+profile+"]\nregion = us-east-1\n")
	credentialsFile := filepath.Join(shared, "credentials")
	WriteCLIFile(t, credentialsFile, "["+profile+"]\naws_access_key_id = "+tests3.User+"\naws_secret_access_key = "+tests3.Pass+"\n")
	profiled := append(append([]string(nil), env...),
		"AWS_CONFIG_FILE="+config, "AWS_SHARED_CREDENTIALS_FILE="+credentialsFile)

	RunCLIOK(t, "real", profiled, nil, "pull", notes)
	if after := ReadSettings(t, env); after != before {
		t.Fatalf("the settings file after a record-sourced pull = %s, want it unchanged (%s)", after, before)
	}
	if got, err := os.ReadFile(filepath.Join(notes, "a.md")); err != nil || string(got) != "the surviving note\n" {
		t.Fatalf("the note after the identity change = %q, %v; want it imported from the accepted state", got, err)
	}
	if got := IdentityDirs(t, privateRoot); len(got) != 2 {
		t.Fatalf("private identities after the profile's region = %v, want a second identity", got)
	}
	// A third run under the same record keeps the identity: the change is
	// once, not per process.
	RunCLIOK(t, "real", profiled, nil, "pull", notes)
	if got := IdentityDirs(t, privateRoot); len(got) != 2 {
		t.Fatalf("private identities after the third run = %v, want the second reused", got)
	}
	// The profile is configuration: the record carries it, and no private
	// state names it.
	if !strings.Contains(ReadSettings(t, env), `"profile":"`+profile+`"`) {
		t.Fatalf("the settings file = %s, want the record's profile", ReadSettings(t, env))
	}
	if NamesProfile(t, privateRoot, profile) {
		t.Fatalf("a private state under %s names the profile %q, want configuration only", privateRoot, profile)
	}
}
