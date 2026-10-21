package app

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/httpstore/gatewaytest"
	"github.com/baalimago/slivingdoc/internal/settings"
	"github.com/baalimago/slivingdoc/internal/sitelogin/sitetest"
)

// The remembered target of the fixtures: another space of the same account,
// and a prefix of its own, so every row can tell a remembered value from the
// stored default space and the default prefix.
const (
	rememberedSpace = "second-space"
	rememberedPath  = "/work/notes"
	rememberedPrfx  = "team-notes-prefix"
)

// remembers is the one entry of the fixtures.
func remembers(space, prefix string) settings.Entry {
	return settings.Entry{Path: rememberedPath, Target: settings.Target{Kind: settings.KindHosted, Name: space, Prefix: prefix}}
}

// settingsFileOf is the scoped settings file of the configuration directory
// env names, whose directory it creates.
func settingsFileOf(t *testing.T, env []string) settings.File {
	t.Helper()
	dir := configDirOf(t, env)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := settings.Locate(func(string) string { return dir }, runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

// storeEntries writes the entries into the settings file of the configuration
// directory env names, which is the directory its credentials file lives in, so
// one environment selects both files.
func storeEntries(t *testing.T, env []string, entries ...settings.Entry) {
	t.Helper()
	set := settings.Set{}
	for _, e := range entries {
		if e.Target.Name == "" {
			continue
		}
		set = set.Put(e)
	}
	if err := settingsFileOf(t, env).Save(set); err != nil {
		t.Fatalf("save the settings file: %v", err)
	}
}

// configDirOf is the configuration directory env names, else a fresh one.
func configDirOf(t *testing.T, env []string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "cfg")
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, credentials.DirEnv+"="); ok {
			dir = v
		}
	}
	return dir
}

// associatedProcess is testProcess over the notebook path of a command, so the
// resolved configuration reads the record of that directory.
func associatedProcess(env []string, path string, args ...string) process {
	p := testProcess(env, args...)
	p.association = ProcessOptions{Env: env}.WithAssociation(path).association()
	return p
}

// TestResolveSpacePrecedenceTable proves the documented order of the space:
// flags, then the environment, then the space this directory remembers, then
// the login's stored default space. The prefix follows the same order, the
// shared AWS files are no refusal beside a remembered space, and a token
// ignores the record entirely.
func TestResolveSpacePrecedenceTable(t *testing.T) {
	login := writeLogins(t, defaults(DefaultHostedEndpoint, "notes"), entry(DefaultHostedEndpoint, loginToken))
	awsHome := awsHomeDir(t)
	for _, row := range []struct {
		name       string
		env        []string
		args       []string
		remembered settings.Entry
		wantBucket string
		wantFrom   bucketSource
		wantPrefix string
	}{
		{
			name:       "the remembered space beats the stored default space",
			env:        []string{login},
			remembered: remembers(rememberedSpace, rememberedPrfx),
			wantBucket: rememberedSpace, wantFrom: bucketFromRemembered, wantPrefix: rememberedPrfx,
		},
		{
			name:       "the remembered prefix applies where nothing named one",
			env:        []string{login},
			remembered: remembers(rememberedSpace, ""),
			wantBucket: rememberedSpace, wantFrom: bucketFromRemembered, wantPrefix: "slivingdoc",
		},
		{
			name:       "a flag naming the remembered space keeps its prefix",
			env:        []string{login},
			args:       []string{"--space", rememberedSpace},
			remembered: remembers(rememberedSpace, rememberedPrfx),
			wantBucket: rememberedSpace, wantFrom: bucketFromSpaceFlag, wantPrefix: rememberedPrfx,
		},
		{
			name:       "a flag naming another space leaves the record alone",
			env:        []string{login},
			args:       []string{"--space", "flag-space"},
			remembered: remembers(rememberedSpace, rememberedPrfx),
			wantBucket: "flag-space", wantFrom: bucketFromSpaceFlag, wantPrefix: "slivingdoc",
		},
		{
			name:       "an explicitly cleared flag falls to the remembered space",
			env:        []string{login},
			args:       []string{"--space="},
			remembered: remembers(rememberedSpace, rememberedPrfx),
			wantBucket: rememberedSpace, wantFrom: bucketFromRemembered, wantPrefix: rememberedPrfx,
		},
		{
			name:       "--space beats the remembered space",
			env:        []string{login},
			args:       []string{"--space", "flag-space"},
			remembered: remembers(rememberedSpace, rememberedPrfx),
			wantBucket: "flag-space", wantFrom: bucketFromSpaceFlag, wantPrefix: "slivingdoc",
		},
		{
			name:       "--bucket beats the remembered space",
			env:        []string{login},
			args:       []string{"--bucket", "flag-space"},
			remembered: remembers(rememberedSpace, rememberedPrfx),
			wantBucket: "flag-space", wantFrom: bucketFromFlag, wantPrefix: "slivingdoc",
		},
		{
			name:       "SLIVINGDOC_SPACE beats the remembered space",
			env:        []string{login, "SLIVINGDOC_SPACE=env-space"},
			remembered: remembers(rememberedSpace, rememberedPrfx),
			wantBucket: "env-space", wantFrom: bucketFromSpaceEnv, wantPrefix: "slivingdoc",
		},
		{
			name:       "SLIVINGDOC_BUCKET beats the remembered space",
			env:        []string{login, "SLIVINGDOC_BUCKET=env-space"},
			remembered: remembers(rememberedSpace, rememberedPrfx),
			wantBucket: "env-space", wantFrom: bucketFromEnv, wantPrefix: "slivingdoc",
		},
		{
			name:       "the stored default space wins where no entry exists",
			env:        []string{login},
			remembered: settings.Entry{},
			wantBucket: "notes", wantFrom: bucketFromLogin, wantPrefix: "slivingdoc",
		},
		{
			name:       "the shared AWS files are no refusal beside a remembered space",
			env:        []string{login, "HOME=" + awsHome},
			remembered: remembers(rememberedSpace, rememberedPrfx),
			wantBucket: rememberedSpace, wantFrom: bucketFromRemembered, wantPrefix: rememberedPrfx,
		},
		{
			name:       "--storage hosted takes the remembered space",
			env:        []string{login, "AWS_ACCESS_KEY_ID=key"},
			args:       []string{"--storage", "hosted"},
			remembered: remembers(rememberedSpace, rememberedPrfx),
			wantBucket: rememberedSpace, wantFrom: bucketFromRemembered, wantPrefix: rememberedPrfx,
		},
		{
			name:       "the token ignores the remembered space and the prefix",
			env:        []string{login, "SLIVINGDOC_TOKEN=" + hostedTestToken},
			remembered: remembers(rememberedSpace, rememberedPrfx),
			wantBucket: "", wantFrom: bucketNone, wantPrefix: "slivingdoc",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			env := row.env
			storeEntries(t, env, row.remembered)
			cfg, err := loadConfig(associatedProcess(env, rememberedPath, row.args...))
			if err != nil {
				t.Fatalf("loadConfig() = %v", err)
			}
			if cfg.bucket != row.wantBucket || cfg.bucketFrom != row.wantFrom || cfg.prefix != row.wantPrefix {
				t.Fatalf("config = space %q from %v with prefix %q; want %q from %v with prefix %q",
					cfg.bucket, cfg.bucketFrom, cfg.prefix, row.wantBucket, row.wantFrom, row.wantPrefix)
			}
		})
	}
}

// TestSpaceSourceWording proves every source of the space names itself the way
// an operator changes it, so a refusal and the status report name the setting
// that has to change (architecture/login.md).
func TestSpaceSourceWording(t *testing.T) {
	login := writeLogins(t, defaults(DefaultHostedEndpoint, "notes"), entry(DefaultHostedEndpoint, loginToken))
	for _, row := range []struct {
		name       string
		env        []string
		args       []string
		want       string
		remembered settings.Entry
	}{
		{name: "the remembered space", env: []string{login}, want: "remembered space", remembered: remembers(rememberedSpace, rememberedPrfx)},
		{name: "--space", env: []string{login}, args: []string{"--space", "notes"}, want: "--space", remembered: remembers(rememberedSpace, rememberedPrfx)},
		{name: "--bucket", env: []string{login}, args: []string{"--bucket", "notes"}, want: "--bucket", remembered: remembers(rememberedSpace, rememberedPrfx)},
		{name: "SLIVINGDOC_SPACE", env: []string{login, "SLIVINGDOC_SPACE=notes"}, want: "SLIVINGDOC_SPACE", remembered: remembers(rememberedSpace, rememberedPrfx)},
		{name: "SLIVINGDOC_BUCKET", env: []string{login, "SLIVINGDOC_BUCKET=notes"}, want: "SLIVINGDOC_BUCKET", remembered: remembers(rememberedSpace, rememberedPrfx)},
		{name: "the stored default space", env: []string{login}, want: "default space"},
		{name: "an S3 bucket", env: []string{"SLIVINGDOC_BUCKET=s3-bucket"}, want: "SLIVINGDOC_BUCKET", remembered: remembers(rememberedSpace, rememberedPrfx)},
	} {
		t.Run(row.name, func(t *testing.T) {
			storeEntries(t, row.env, row.remembered)
			cfg, err := loadConfig(associatedProcess(row.env, rememberedPath, row.args...))
			if err != nil {
				t.Fatalf("loadConfig() = %v", err)
			}
			if got := cfg.bucketFrom.String(); got != row.want {
				t.Fatalf("space source = %q, want %q", got, row.want)
			}
		})
	}
}

// TestRememberedSpaceRefusals proves every way a remembered space can fail is a
// refusal naming the space, and that none of them falls back to S3, to the
// account's default space or to another notebook of the space.
func TestRememberedSpaceRefusals(t *testing.T) {
	other := writeLogins(t, nil, entry(devEndpoint, loginToken))
	expiredEntry := entry(DefaultHostedEndpoint, loginToken)
	expiredEntry.ExpiresAt = longAgo
	expired := writeLogins(t, nil, expiredEntry)
	two := writeLogins(t, defaults(DefaultHostedEndpoint, "notes"), entry(DefaultHostedEndpoint, loginToken), entry(devEndpoint, otherToken))
	one := writeLogins(t, defaults(DefaultHostedEndpoint, "notes"), entry(DefaultHostedEndpoint, loginToken))
	for _, row := range []struct {
		name       string
		env        []string
		args       []string
		remembered settings.Entry
		want       []string
	}{
		{
			name:       "no credential at all",
			env:        []string{credentials.DirEnv + "=" + filepath.Join(t.TempDir(), "empty")},
			remembered: remembers(rememberedSpace, rememberedPrfx),
			want:       []string{`remembers hosted space "second-space"`, "run 'slivingdoc login'", "--space"},
		},
		{
			name:       "the only login is for another endpoint",
			env:        []string{other, "SLIVINGDOC_ENDPOINT=https://api.example.test"},
			remembered: remembers(rememberedSpace, rememberedPrfx),
			want:       []string{`remembers hosted space "second-space"`, "at https://api.example.test", "the stored login is for " + devEndpoint},
		},
		{
			name:       "several stored logins",
			env:        []string{two},
			remembered: remembers(rememberedSpace, rememberedPrfx),
			want:       []string{"several stored logins", "pass --endpoint"},
		},
		{
			name:       "an expired login",
			env:        []string{expired},
			remembered: remembers(rememberedSpace, rememberedPrfx),
			want:       []string{"stored login expired", "run 'slivingdoc login' again"},
		},
		{
			name:       "a space outside the API grammar",
			env:        []string{one},
			remembered: remembers("Second_Space", rememberedPrfx),
			want:       []string{"invalid space name", `"Second_Space"`},
		},
		{
			name:       "the variable names another notebook of the space",
			env:        []string{one, "SLIVINGDOC_PREFIX=other"},
			remembered: remembers(rememberedSpace, rememberedPrfx),
			want:       []string{`remembers hosted space "second-space" with prefix "team-notes-prefix"`, `"other" names another notebook`, "--prefix team-notes-prefix"},
		},
		{
			name:       "a flag names another notebook of the space",
			env:        []string{one},
			args:       []string{"--prefix=flag-prefix"},
			remembered: remembers(rememberedSpace, rememberedPrfx),
			want:       []string{`"flag-prefix" names another notebook`, "pass --prefix team-notes-prefix"},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			storeEntries(t, row.env, row.remembered)
			_, err := loadConfig(associatedProcess(row.env, rememberedPath, row.args...))
			if err == nil {
				t.Fatal("loadConfig() = nil, want a refusal")
			}
			for _, want := range row.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("loadConfig() = %q, want it to contain %q", err, want)
				}
			}
			if strings.Contains(err.Error(), loginToken) || strings.Contains(err.Error(), otherToken) {
				t.Fatalf("loadConfig() = %q echoes a key", err)
			}
		})
	}
}

// TestUnusableSettingsFileRefusesStartup proves every way the settings file can
// be unreadable is a refusal naming the file and the fix, never an empty result
// that would choose another store (architecture/config.md).
func TestUnusableSettingsFileRefusesStartup(t *testing.T) {
	login := writeLogins(t, defaults(DefaultHostedEndpoint, "notes"), entry(DefaultHostedEndpoint, loginToken))
	for _, row := range []struct {
		name string
		body string
		want string
	}{
		{"malformed", `{"version":1,"entries":[{"path":"/work/notes"}]}`, "malformed settings file"},
		{"an older version", `{"version":0,"entries":[]}`, "unsupported settings file version"},
		{"a newer build's version", `{"version":99,"entries":[]}`, "a newer slivingdoc wrote it"},
		{"a credential beside the space", `{"version":1,"entries":[{"path":"/work/notes","target":{"space":"second-space","token":"sld_secret"}}]}`, "malformed settings file"},
	} {
		t.Run(row.name, func(t *testing.T) {
			env := []string{login}
			dir := configDirOf(t, env)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, settings.FileName), []byte(row.body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := loadConfig(associatedProcess(env, rememberedPath))
			if err == nil || !strings.Contains(err.Error(), row.want) {
				t.Fatalf("loadConfig() = %v, want it to contain %q", err, row.want)
			}
			if !strings.Contains(err.Error(), "remove it to choose the space") {
				t.Fatalf("loadConfig() = %q, want it to name the way to run without the file", err)
			}
		})
	}
}

// TestScratchServeNeverLooksUpTheAssociation proves a server that owns a
// scratch directory consults no record: its process carries no reader, so a
// settings file this build cannot read never stops it.
func TestScratchServeNeverLooksUpTheAssociation(t *testing.T) {
	env := []string{"SLIVINGDOC_BUCKET=b"}
	dir := configDirOf(t, env)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"version":0,"entries":[]}`)
	if err := os.WriteFile(filepath.Join(dir, settings.FileName), body, 0o600); err != nil {
		t.Fatal(err)
	}
	env = append(env, credentials.DirEnv+"="+dir)
	session := filepath.Join(t.TempDir(), "session")
	cfg, err := loadConfig(ephemeralProcess(session, env))
	if err != nil {
		t.Fatalf("loadConfig() with an unreadable settings file = %v, want serve unaffected", err)
	}
	if !cfg.hosted() && cfg.bucket != "b" {
		t.Fatalf("config = space %q from %v, want the bucket", cfg.bucket, cfg.bucketFrom)
	}
	if got, err := os.ReadFile(filepath.Join(dir, settings.FileName)); err != nil || string(got) != string(body) {
		t.Fatalf("settings file after a configuration read = %q, %v; want it untouched", got, err)
	}
}

// TestRuntimeSpaceAccessors proves the resolved pair a report names: the space
// and the setting that named it (architecture/config.md).
func TestRuntimeSpaceAccessors(t *testing.T) {
	const account = "ada@example.test"
	site := sitetest.Start(t)
	g := gatewaytest.Start(t)
	g.AddSpace(rememberedSpace, 1<<20)
	site.SetSpaces(loginToken, sitetest.Space{Name: rememberedSpace, Owner: account, Access: "write"})
	site.Next(sitetest.Script{Issue: sitetest.Issue{
		Key: loginToken, Access: "write", Endpoint: g.URL(), ExpiresAt: loginExpiry, Account: account,
	}})
	login := writeLogins(t, nil, storedLogin{
		Site: site.URL(), Endpoint: g.URL(), Key: loginToken, Access: "write", Account: account,
	})
	site.OnMint(func(m sitetest.Minted) { g.GrantAs(m.Token, m.SpaceID, m.Space, false) })
	site.Issued(loginToken, g.URL())
	env := []string{login}
	storeEntries(t, env, remembers(rememberedSpace, rememberedPrfx))
	p := associatedProcess(env, rememberedPath)
	rt, err := setup(p)
	if err != nil {
		t.Fatalf("setup() = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	if got := rt.Space(); got != rememberedSpace {
		t.Fatalf("Space() = %q, want %q", got, rememberedSpace)
	}
	if got := rt.SpaceSource(); got != "remembered space" {
		t.Fatalf("SpaceSource() = %q, want the remembered-space wording", got)
	}
	if got := rt.Target(); got != "space "+rememberedSpace {
		t.Fatalf("Target() = %q, want the hosted space", got)
	}
}

// TestSpaceSourceNamesAnS3Record proves the record reports one source word per
// backend: an S3 record is a remembered bucket, because the source itself
// carries no kind (architecture/config.md, The operator surface of the record).
func TestSpaceSourceNamesAnS3Record(t *testing.T) {
	t.Parallel()
	rt := &Runtime{cfg: config{bucket: "a-bucket", bucketFrom: bucketFromRemembered}}
	if got := rt.SpaceSource(); got != rememberedBucketSource {
		t.Fatalf("SpaceSource() = %q, want %q", got, rememberedBucketSource)
	}
	rt.cfg.login = &credentials.Login{}
	if got := rt.SpaceSource(); got != rememberedSource {
		t.Fatalf("SpaceSource() of a hosted record = %q, want %q", got, rememberedSource)
	}
}

// TestAssociationReadsTheSettingsOnce proves the cost the read adds: a
// process reads the settings file once, so every later resolution of that
// process answers from the set of that first read even after the file
// changed on disk.
func TestAssociationReadsTheSettingsOnce(t *testing.T) {
	login := writeLogins(t, defaults(DefaultHostedEndpoint, "notes"), entry(DefaultHostedEndpoint, loginToken))
	env := []string{login}
	storeEntries(t, env, remembers(rememberedSpace, rememberedPrfx))
	reader := ProcessOptions{Env: env}.WithAssociation(rememberedPath).Load
	first, err := reader()
	if err != nil {
		t.Fatalf("read the settings file = %v", err)
	}
	if target, ok := first.Lookup(rememberedPath); !ok || target.Name != rememberedSpace {
		t.Fatalf("the first read holds %+v, %v; want the remembered target", target, ok)
	}
	storeEntries(t, env, remembers(rememberedSpace, "another-prefix"))
	second, err := reader()
	if err != nil {
		t.Fatalf("read the settings file again = %v", err)
	}
	if !slices.Equal(second.Entries(), first.Entries()) {
		t.Fatalf("the second read holds %v, want the set of the first read %v", second.Entries(), first.Entries())
	}
}

// s3Want is the target an S3 resolution row expects.
type s3Want struct {
	bucket    string
	from      bucketSource
	endpoint  string
	region    string
	profile   string
	prefix    string
	pathStyle bool
}

// TestResolveS3RecordFields proves the per-field order of an S3 target: the
// flag or slivingdoc variable, then a record of the resolved kind, then the
// one global S3 default, then the ambient AWS configuration. A record of
// another kind supplies no field, and a named prefix that contradicts the
// record is refused in the record's own words (I4, I5).
func TestResolveS3RecordFields(t *testing.T) {
	record := settings.Target{
		Kind: settings.KindS3, Endpoint: "https://record.example.test", Region: "eu-north-1",
		Profile: "record-profile", Name: "record-bucket", Prefix: "record-prefix", PathStyle: true,
	}
	def := settings.Target{
		Kind: settings.KindS3, Endpoint: "https://default.example.test", Region: "us-west-2",
		Profile: "default-profile", Name: "default-bucket", Prefix: "default-prefix",
	}
	recordTarget := s3Want{
		bucket: "record-bucket", from: bucketFromRemembered, endpoint: "https://record.example.test",
		region: "eu-north-1", profile: "record-profile", prefix: "record-prefix", pathStyle: true,
	}
	for _, row := range []struct {
		name    string
		record  *settings.Target
		def     *settings.Target
		args    []string
		want    s3Want
		wantErr string
	}{
		{
			name:   "the record supplies every field",
			record: &record,
			want:   recordTarget,
		},
		{
			name:   "a flag beats the record for its own run",
			record: &record,
			args:   []string{"--endpoint", "https://flag.example.test", "--region", "ap-south-1", "--profile", "flag-profile", "--path-style=false"},
			want: s3Want{
				bucket: "record-bucket", from: bucketFromRemembered, endpoint: "https://flag.example.test",
				region: "ap-south-1", profile: "flag-profile", prefix: "record-prefix",
			},
		},
		{
			name: "the S3 default fills a directory that remembers nothing",
			def:  &def,
			want: s3Want{
				bucket: "default-bucket", from: bucketFromDefault, endpoint: "https://default.example.test",
				region: "us-west-2", profile: "default-profile", prefix: "default-prefix",
			},
		},
		{
			name:   "the record beats the S3 default",
			record: &record,
			def:    &def,
			want:   recordTarget,
		},
		{
			name:    "a named prefix that contradicts the record is refused",
			record:  &record,
			args:    []string{"--prefix", "another-prefix"},
			wantErr: `remembers bucket "record-bucket"`,
		},
		{
			name:    "a hosted record supplies no field to an S3 run",
			record:  &settings.Target{Kind: settings.KindHosted, Name: "hosted-space", Prefix: "hosted-prefix"},
			args:    []string{"--storage", "s3"},
			wantErr: "bucket is required",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			env := []string{credentials.DirEnv + "=" + filepath.Join(t.TempDir(), "cfg"), "HOME=" + t.TempDir()}
			set := settings.Set{}
			if row.record != nil {
				set = set.Put(settings.Entry{Path: rememberedPath, Target: *row.record})
			}
			if row.def != nil {
				set = set.PutDefault(*row.def)
			}
			if err := settingsFileOf(t, env).Save(set); err != nil {
				t.Fatalf("save the settings file: %v", err)
			}
			cfg, err := loadConfig(associatedProcess(env, rememberedPath, row.args...))
			if row.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), row.wantErr) {
					t.Fatalf("loadConfig() = %v, want it to contain %q", err, row.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("loadConfig() = %v", err)
			}
			got := s3Want{
				bucket: cfg.bucket, from: cfg.bucketFrom, endpoint: cfg.endpoint,
				region: cfg.region, profile: cfg.profile, prefix: cfg.prefix, pathStyle: cfg.pathStyle,
			}
			if got != row.want {
				t.Fatalf("config = %+v, want %+v", got, row.want)
			}
		})
	}
}
