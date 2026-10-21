package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/s3store"
	"github.com/baalimago/slivingdoc/internal/settings"
	"github.com/baalimago/slivingdoc/internal/sitelogin/sitetest"
	"github.com/baalimago/slivingdoc/internal/storage"
)

const s3AccessKeyID = "AKIAIOSFODNN7EXAMPLE"

// fakeS3Admin records what the space command asked of one identity, so a
// test can prove the only bucket calls are the listing and, when asked, the
// create.
type fakeS3Admin struct {
	buckets   []string
	listErr   error
	createErr error
	account   s3store.Identity
	// identityErr models a credential the identity cannot retrieve.
	identityErr error
	// failAfterList makes the Nth listing answer listErr, so a test can
	// fail the refresh that follows a create.
	failAfterList int
	listCalls     int
	createCalls   int
	identityCalls int
	created       []string
}

func (f *fakeS3Admin) ListBuckets(context.Context) ([]s3store.Bucket, error) {
	f.listCalls++
	if f.listErr != nil && (f.failAfterList == 0 || f.listCalls > f.failAfterList) {
		return nil, f.listErr
	}
	out := make([]s3store.Bucket, 0, len(f.buckets))
	for _, name := range f.buckets {
		out = append(out, s3store.Bucket{Name: name})
	}
	return out, nil
}

func (f *fakeS3Admin) CreateBucket(_ context.Context, name string) error {
	f.createCalls++
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, name)
	f.buckets = append(f.buckets, name)
	return nil
}

func (f *fakeS3Admin) Identity(context.Context) (s3store.Identity, error) {
	f.identityCalls++
	if f.identityErr != nil {
		return s3store.Identity{}, f.identityErr
	}
	return f.account, nil
}

// s3IdentityFixture is one profile's injected administration and the values
// the real resolver would report.
type s3IdentityFixture struct {
	admin    *fakeS3Admin
	resolved s3store.Resolved
	err      error
}

// newS3Admin is the rig's s3Admin factory: it never reads AWS
// configuration. An unplanted profile resolves the ambient chain to an
// empty account, so an accidental S3 selection still lists no bucket.
func (r *loginRig) newS3Admin(_ context.Context, cfg s3store.Config, opts s3store.Options) (s3Admin, s3store.Resolved, error) {
	r.s3Configs = append(r.s3Configs, cfg)
	fixture, planted := r.s3[cfg.Profile]
	if !planted {
		fixture = s3IdentityFixture{admin: &fakeS3Admin{account: s3store.Identity{Profile: cfg.Profile, Region: cfg.Region, Endpoint: cfg.Endpoint, AccessKeyID: s3AccessKeyID}}}
	}
	if fixture.err != nil {
		return nil, s3store.Resolved{}, fixture.err
	}
	resolved := fixture.resolved
	if cfg.Region != "" {
		resolved.Region = cfg.Region
	}
	if resolved.Endpoint == "" {
		resolved.Endpoint = cfg.Endpoint
	}
	if resolved.Region == "" {
		resolved.Region = cfg.Region
	}
	if resolved.Profile == "" {
		resolved.Profile = cfg.Profile
	}
	if resolved.Endpoint != "" || opts.ForcePathStyle {
		resolved.PathStyle = true
	}
	return fixture.admin, resolved, nil
}

// addS3 plants one profile's fake administration. An empty profile is the
// ambient chain.
func (r *loginRig) addS3(profile, region string, buckets ...string) *fakeS3Admin {
	if r.s3 == nil {
		r.s3 = make(map[string]s3IdentityFixture)
	}
	admin := &fakeS3Admin{
		buckets: append([]string(nil), buckets...),
		account: s3store.Identity{Profile: profile, Region: region, AccessKeyID: s3AccessKeyID},
	}
	r.s3[profile] = s3IdentityFixture{admin: admin, resolved: s3store.Resolved{Profile: profile, Region: region}}
	return admin
}

// plantFail plants a profile whose administration cannot be built.
func (r *loginRig) plantFail(profile string, err error) {
	if r.s3 == nil {
		r.s3 = make(map[string]s3IdentityFixture)
	}
	r.s3[profile] = s3IdentityFixture{err: err}
}

func (r *loginRig) settingsFile(t *testing.T) settings.File {
	t.Helper()
	file, err := settings.Locate(func(name string) string {
		if name == credentials.DirEnv {
			return r.dir
		}
		return ""
	}, runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func (r *loginRig) settingsSet(t *testing.T) settings.Set {
	t.Helper()
	set, err := r.settingsFile(t).Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	return set
}

// plantDefaults stores S3 defaults beside the entries the rig already
// planted.
func (r *loginRig) plantDefaults(t *testing.T, targets ...settings.Target) {
	t.Helper()
	file := r.settingsFile(t)
	set, err := file.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		set = set.PutDefault(target)
	}
	if err := file.Save(set); err != nil {
		t.Fatal(err)
	}
}

// s3Record is a directory that remembers an S3 bucket with the given
// recorded identity fields.
func s3Record(path, name, endpoint, region, profile string) settings.Entry {
	return settings.Entry{Path: path, Target: settings.Target{
		Kind: settings.KindS3, Endpoint: endpoint, Region: region, Profile: profile, Name: name,
	}}
}

func s3HeaderLine(endpoint, region, profile string) string {
	return fmt.Sprintf("S3  endpoint=%s  region=%s  profile=%s  key=%s\n", endpoint, region, profile, s3AccessKeyID)
}

func TestSpaceS3Header(t *testing.T) {
	r := newLoginRig(t)
	r.addS3("dev", "eu-north-1")
	if err := r.space(t, "--profile", "dev"); err != nil {
		t.Fatalf("space --profile dev = %v", err)
	}
	want := s3HeaderLine("(AWS)", "eu-north-1", "dev") +
		"No buckets; run 'slivingdoc space new <name>' to create one.\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
}

// TestSpaceS3EndpointVariableWinsOverDefault proves a variable that names an
// S3 endpoint selects it instead of the active default's identity.
func TestSpaceS3EndpointVariableWinsOverDefault(t *testing.T) {
	r := newLoginRig(t)
	r.addS3("", "us-east-1", "ambient-bucket")
	r.plantDefaults(t, settings.Target{Kind: settings.KindS3, Region: "eu-north-1", Profile: "dev", Name: "analytics"})
	r.extraEnv = []string{"SLIVINGDOC_ENDPOINT=http://127.0.0.1:9000"}
	if err := r.space(t); err != nil {
		t.Fatalf("space = %v", err)
	}
	if len(r.s3Configs) == 0 || r.s3Configs[0].Endpoint != "http://127.0.0.1:9000" {
		t.Fatalf("resolved identities = %+v, want the endpoint variable first", r.s3Configs)
	}
}

func TestSpaceS3UsesListBucketsOnly(t *testing.T) {
	r := newLoginRig(t)
	admin := r.addS3("dev", "eu-north-1", "analytics", "research")
	if err := r.space(t, "--profile", "dev"); err != nil {
		t.Fatalf("space --profile dev = %v", err)
	}
	if admin.listCalls != 1 || admin.identityCalls != 1 || admin.createCalls != 0 {
		t.Fatalf("list = %d, identity = %d, create = %d; want one list and no create", admin.listCalls, admin.identityCalls, admin.createCalls)
	}
	want := s3HeaderLine("(AWS)", "eu-north-1", "dev") + "  analytics\n  research\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
}

func TestSpaceS3Directories(t *testing.T) {
	for _, tt := range []struct {
		name     string
		settings []settings.Entry
		out      string
		errOut   string
	}{
		{
			name: "an empty record field is a value, not a wildcard",
			settings: []settings.Entry{
				s3Record("/a", "analytics", "", "eu-north-1", "dev"),
				s3Record("/b", "analytics", "", "", ""),
				s3Record("/c", "analytics", "https://other.example.test", "", ""),
				s3Record("/d", "team", "https://other.example.test", "", ""),
				s3Record("/f", "team", "https://other.example.test", "us-east-1", ""),
				s3Record("/e", "team", "", "us-east-1", "prod"),
			},
			out: s3HeaderLine("(AWS)", "eu-north-1", "dev") +
				"  analytics (/a)\n  team\n\n" +
				s3HeaderLine("(AWS)", "us-east-1", "(ambient)") +
				" ! analytics (/b)\n\n" +
				s3HeaderLine("https://other.example.test", "us-east-1", "(ambient)") +
				" ! analytics (/c)\n ! team (/d, /f)\n\n" +
				s3HeaderLine("(AWS)", "us-east-1", "prod") +
				" ! team (/e)\n",
			errOut: "No default bucket; run 'slivingdoc space <name>' to choose one.\n" +
				"! this directory remembers bucket \"analytics\", which endpoint=(AWS), region=us-east-1, profile=(ambient) did not list\n" +
				"! this directory remembers bucket \"analytics\", which endpoint=https://other.example.test, region=us-east-1, profile=(ambient) did not list\n" +
				"! this directory remembers bucket \"team\", which endpoint=https://other.example.test, region=us-east-1, profile=(ambient) did not list\n" +
				"! this directory remembers bucket \"team\", which endpoint=(AWS), region=us-east-1, profile=prod did not list\n",
		},
		{
			name: "a partial record owns its own identity",
			settings: []settings.Entry{
				s3Record("/a", "analytics", "", "eu-north-1", "dev"),
				s3Record("/b", "analytics", "", "", ""),
			},
			out: s3HeaderLine("(AWS)", "eu-north-1", "dev") +
				"  analytics (/a)\n  team\n\n" +
				s3HeaderLine("(AWS)", "us-east-1", "(ambient)") +
				" ! analytics (/b)\n",
			errOut: "No default bucket; run 'slivingdoc space <name>' to choose one.\n" +
				"! this directory remembers bucket \"analytics\", which endpoint=(AWS), region=us-east-1, profile=(ambient) did not list\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newLoginRig(t)
			r.addS3("dev", "eu-north-1", "analytics", "team")
			r.plantSettings(t, tt.settings...)
			if err := r.space(t, "--profile", "dev"); err != nil {
				t.Fatalf("space --profile dev = %v", err)
			}
			if r.out.String() != tt.out {
				t.Fatalf("stdout = %q, want %q", r.out.String(), tt.out)
			}
			if r.errOut.String() != tt.errOut {
				t.Fatalf("stderr = %q, want %q", r.errOut.String(), tt.errOut)
			}
		})
	}
}

func TestSpaceS3ShowsUnreachableRecord(t *testing.T) {
	r := newLoginRig(t)
	r.addS3("dev", "eu-north-1", "analytics")
	r.plantSettings(t, s3Record("/d", "notes", "", "eu-north-1", "dev"))
	r.plantDefaults(t, settings.Target{Kind: settings.KindS3, Region: "eu-north-1", Profile: "dev", Name: "archive"})
	if err := r.space(t, "--profile", "dev"); err != nil {
		t.Fatalf("space --profile dev = %v", err)
	}
	want := s3HeaderLine("(AWS)", "eu-north-1", "dev") +
		"  analytics\n*! archive\n ! notes (/d)\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	wantErr := "! the configured default bucket \"archive\" is not among the buckets this identity lists; run 'slivingdoc space <name>' to choose another\n" +
		"! this directory remembers bucket \"notes\", which endpoint=(AWS), region=eu-north-1, profile=dev did not list\n"
	if r.errOut.String() != wantErr {
		t.Fatalf("stderr = %q, want %q", r.errOut.String(), wantErr)
	}
}

func TestSpaceS3RecordWithoutRegion(t *testing.T) {
	r := newLoginRig(t)
	r.addS3("dev", "eu-north-1", "analytics")
	r.plantSettings(t, s3Record("/a", "analytics", "", "", "dev"))
	if err := r.space(t, "--profile", "dev"); err != nil {
		t.Fatalf("space --profile dev = %v", err)
	}
	want := s3HeaderLine("(AWS)", "eu-north-1", "dev") +
		"  analytics\n\n" +
		s3HeaderLine("(AWS)", "us-east-1", "dev") +
		"  analytics (/a)\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	if strings.Contains(r.out.String(), "! ") || r.errOut.String() != "No default bucket; run 'slivingdoc space <name>' to choose one.\n" {
		t.Fatalf("stdout = %q, stderr = %q; the region-less record must bind without a ! row", r.out.String(), r.errOut.String())
	}
}

func TestSpaceS3DegradedList(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	admin := r.addS3("dev", "eu-north-1")
	admin.listErr = fmt.Errorf("s3store: list buckets: %w", storage.ErrTransport)
	r.plantSettings(t, s3Record("/d", "notes", "", "eu-north-1", "dev"))
	r.plantDefaults(t, settings.Target{Kind: settings.KindS3, Region: "eu-north-1", Profile: "dev", Name: "archive"})
	if err := r.space(t, "--profile", "dev"); err != nil {
		t.Fatalf("space over a failed list = %v; a degraded list exits zero", err)
	}
	want := hostedHost + "  notes  write  ada@example.test\n\n" +
		s3HeaderLine("(AWS)", "eu-north-1", "dev") + "*! archive\n ! notes (/d)\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	if !strings.HasPrefix(r.errOut.String(), "! could not list the buckets of endpoint=(AWS), region=eu-north-1, profile=dev: ") ||
		!strings.HasSuffix(r.errOut.String(), "; the configured default is shown\n") {
		t.Fatalf("stderr = %q", r.errOut.String())
	}
	if strings.Contains(r.errOut.String(), "No default bucket") {
		t.Fatalf("stderr = %q; a degraded group claims no empty default", r.errOut.String())
	}
}

func TestSpaceS3ListDenied(t *testing.T) {
	r := newLoginRig(t)
	admin := r.addS3("dev", "eu-north-1")
	admin.listErr = fmt.Errorf("s3store: list buckets: %w", storage.ErrAccessDenied)
	if err := r.space(t, "--profile", "dev"); err != nil {
		t.Fatalf("space over a denied list = %v; a degraded list exits zero", err)
	}
	want := "! this login may not list buckets (s3:ListAllMyBuckets is denied); the configured default is shown\n"
	if r.errOut.String() != want {
		t.Fatalf("stderr = %q, want %q", r.errOut.String(), want)
	}
}

func TestSpaceS3ListTransport(t *testing.T) {
	r := newLoginRig(t)
	admin := r.addS3("dev", "eu-north-1")
	admin.listErr = fmt.Errorf("s3store: list buckets: connection reset: %w", storage.ErrTransport)
	if err := r.space(t, "--profile", "dev"); err != nil {
		t.Fatalf("space over a transport failure = %v; a degraded list exits zero", err)
	}
	if !strings.Contains(r.errOut.String(), "connection reset") || !strings.Contains(r.errOut.String(), "the configured default is shown") {
		t.Fatalf("stderr = %q, want the sanitized transport hint", r.errOut.String())
	}
}

func TestSpaceS3IdentityDenied(t *testing.T) {
	r := newLoginRig(t)
	r.addS3("", "us-east-1")
	admin := r.addS3("dev", "eu-north-1")
	admin.identityErr = fmt.Errorf("s3store: identity: %w", storage.ErrAccessDenied)
	r.plantSettings(t, s3Record("/d", "notes", "", "eu-north-1", "dev"))
	r.plantDefaults(t, settings.Target{Kind: settings.KindS3, Region: "eu-north-1", Profile: "dev", Name: "archive"})
	if err := r.space(t, "--region", "us-east-1"); err != nil {
		t.Fatalf("space beside a denied identity = %v; a recorded identity degrades", err)
	}
	want := s3HeaderLine("(AWS)", "us-east-1", "(ambient)") +
		"No buckets; run 'slivingdoc space new <name>' to create one.\n\n" +
		"S3  endpoint=(AWS)  region=eu-north-1  profile=dev\n*! archive\n ! notes (/d)\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	wantErr := "! this login may not use the S3 identity endpoint=(AWS), region=eu-north-1, profile=dev (its credentials are denied); the configured default is shown\n"
	if r.errOut.String() != wantErr {
		t.Fatalf("stderr = %q, want %q", r.errOut.String(), wantErr)
	}
}

func TestSpaceS3ExplicitFailureRefuses(t *testing.T) {
	r := newLoginRig(t)
	admin := r.addS3("", "us-east-1")
	admin.identityErr = fmt.Errorf("s3store: identity: %w", storage.ErrAccessDenied)
	err := r.space(t, "--storage", "s3")
	if err == nil || !strings.Contains(err.Error(), "cannot use the S3 identity endpoint=(AWS), region=us-east-1, profile=(ambient)") ||
		!strings.Contains(err.Error(), "check the profile, region, and endpoint") {
		t.Fatalf("space --storage s3 with a denied identity = %v", err)
	}
	if r.out.String() != "" {
		t.Fatalf("stdout = %q; an explicit failure refuses before a listing", r.out.String())
	}
}

func TestSpaceS3ExplicitBackend(t *testing.T) {
	r := newLoginRig(t)
	r.addS3("", "us-east-1")
	if err := r.space(t, "--storage", "s3"); err != nil {
		t.Fatalf("space --storage s3 = %v", err)
	}
	want := s3HeaderLine("(AWS)", "us-east-1", "(ambient)") +
		"No buckets; run 'slivingdoc space new <name>' to create one.\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
}

func TestSpaceAutoS3SignalLists(t *testing.T) {
	for _, row := range []struct {
		name    string
		args    []string
		env     []string
		profile string
		region  string
		want    string
	}{
		{name: "--region", args: []string{"--region", "eu-north-1"}, want: s3HeaderLine("(AWS)", "eu-north-1", "(ambient)")},
		{name: "--profile", args: []string{"--profile", "dev"}, profile: "dev", region: "eu-north-1", want: s3HeaderLine("(AWS)", "eu-north-1", "dev")},
		{name: "--path-style", args: []string{"--path-style"}, want: s3HeaderLine("(AWS)", "us-east-1", "(ambient)")},
		{name: "--endpoint", args: []string{"--endpoint", "https://s3.example.test"}, want: s3HeaderLine("https://s3.example.test", "us-east-1", "(ambient)")},
		{name: "AWS_REGION", env: []string{"AWS_REGION=eu-north-1"}, want: s3HeaderLine("(AWS)", "eu-north-1", "(ambient)")},
	} {
		t.Run(row.name, func(t *testing.T) {
			r := newLoginRig(t)
			r.addS3(row.profile, row.region)
			r.extraEnv = row.env
			if err := r.space(t, row.args...); err != nil {
				t.Fatalf("space %v = %v", row.args, err)
			}
			if !strings.HasPrefix(r.out.String(), row.want) {
				t.Fatalf("stdout = %q, want the header %q", r.out.String(), row.want)
			}
		})
	}
}

func TestSpaceAutoS3IdentityFailureDegrades(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	r.plantFail("dev", errors.New("no shared config"))
	r.extraEnv = []string{"AWS_PROFILE=dev"}
	if err := r.space(t); err != nil {
		t.Fatalf("space beside an environment-selected identity = %v; auto degrades", err)
	}
	want := hostedHost + "* notes  write  ada@example.test\n\n" +
		"S3  endpoint=(AWS)  region=us-east-1  profile=dev\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	wantErr := "! could not use the S3 identity endpoint=(AWS), region=us-east-1, profile=dev: no shared config; the configured default is shown\n"
	if r.errOut.String() != wantErr {
		t.Fatalf("stderr = %q, want %q", r.errOut.String(), wantErr)
	}
}

// TestSpaceEnvPrefixDoesNotSelectS3 proves SLIVINGDOC_PREFIX, which every
// one-shot command shares, names a notebook inside a store rather than the
// store itself: a hosted-only machine still lists and creates hosted.
func TestSpaceEnvPrefixDoesNotSelectS3(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	r.extraEnv = []string{"SLIVINGDOC_PREFIX=team"}
	if err := r.space(t); err != nil {
		t.Fatalf("space with SLIVINGDOC_PREFIX = %v", err)
	}
	if len(r.s3Configs) != 0 {
		t.Fatalf("AWS configuration resolved %d times; a prefix is not an S3 selector", len(r.s3Configs))
	}
	r.out.Reset()
	r.errOut.Reset()
	if err := r.space(t, "new", "research"); err != nil {
		t.Fatalf("space new with SLIVINGDOC_PREFIX = %v", err)
	}
	if want := "Created hosted space research (write, ada@example.test) at " + DefaultHostedEndpoint + ".\n" +
		"It is not the default; run 'slivingdoc space research' to make it the default.\n"; r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
}

func TestSpaceAutoHostedOnlyLoadsNoAWS(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	if err := r.space(t); err != nil {
		t.Fatalf("space = %v", err)
	}
	if len(r.s3Configs) != 0 {
		t.Fatalf("AWS configuration resolved %d times; a hosted-only machine loads none", len(r.s3Configs))
	}
	if want := hostedHost + "* notes  write  ada@example.test\n"; r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
}

func TestSpaceS3Create(t *testing.T) {
	r := newLoginRig(t)
	admin := r.addS3("dev", "eu-north-1")
	if err := r.space(t, "--profile", "dev", "new", "analytics"); err != nil {
		t.Fatalf("space --profile dev new analytics = %v", err)
	}
	want := "Created bucket analytics (endpoint=(AWS), region=eu-north-1, profile=dev).\n" +
		"It is not the default; run 'slivingdoc space --profile dev analytics' to make it the default.\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	if admin.createCalls != 1 || admin.created[0] != "analytics" {
		t.Fatalf("creates = %v; want one create of analytics", admin.created)
	}
	if set := r.settingsSet(t); len(set.Defaults()) != 0 {
		t.Fatalf("defaults = %+v; a create changes no default", set.Defaults())
	}
}

func TestSpaceS3CreateUsesCurrentIdentity(t *testing.T) {
	r := newLoginRig(t)
	dev := r.addS3("dev", "eu-north-1")
	prod := r.addS3("prod", "us-east-1")
	r.plantSettings(t, s3Record("/p", "prod-bucket", "", "us-east-1", "prod"))
	if err := r.space(t, "--profile", "dev", "new", "analytics"); err != nil {
		t.Fatalf("space --profile dev new analytics = %v", err)
	}
	if len(r.s3Configs) != 1 || r.s3Configs[0].Profile != "dev" {
		t.Fatalf("resolved identities = %+v; want only the flag-selected dev profile", r.s3Configs)
	}
	if dev.createCalls != 1 || prod.createCalls != 0 {
		t.Fatalf("dev creates = %d, prod creates = %d; want only the current identity", dev.createCalls, prod.createCalls)
	}
}

func TestSpaceS3CreateOwned(t *testing.T) {
	r := newLoginRig(t)
	admin := r.addS3("dev", "eu-north-1")
	admin.createErr = fmt.Errorf("s3store: create bucket: %w", s3store.ErrBucketOwned)
	err := r.space(t, "--profile", "dev", "new", "analytics")
	want := "space new: bucket analytics already exists and this account owns it; run 'slivingdoc space --profile dev analytics' to use it"
	if err == nil || err.Error() != want {
		t.Fatalf("space new over an owned bucket = %v, want %q", err, want)
	}
	if admin.createCalls != 1 {
		t.Fatalf("create calls = %d, want one without a retry", admin.createCalls)
	}
}

func TestSpaceS3CreateExists(t *testing.T) {
	r := newLoginRig(t)
	admin := r.addS3("dev", "eu-north-1")
	admin.createErr = fmt.Errorf("s3store: create bucket: %w", s3store.ErrBucketExists)
	err := r.space(t, "--profile", "dev", "new", "analytics")
	want := "space new: bucket analytics already exists in another account; choose another name"
	if err == nil || err.Error() != want {
		t.Fatalf("space new over an existing bucket = %v, want %q", err, want)
	}
}

func TestSpaceS3CreateDenied(t *testing.T) {
	r := newLoginRig(t)
	admin := r.addS3("dev", "eu-north-1")
	admin.createErr = fmt.Errorf("s3store: create bucket: %w", storage.ErrAccessDenied)
	err := r.space(t, "--profile", "dev", "new", "analytics")
	want := "space new: this login may not create a bucket (s3:CreateBucket is denied); check the profile or endpoint"
	if err == nil || err.Error() != want {
		t.Fatalf("space new over a denied create = %v, want %q", err, want)
	}
}

func TestSpaceS3CreateTransport(t *testing.T) {
	r := newLoginRig(t)
	admin := r.addS3("dev", "eu-north-1")
	admin.createErr = fmt.Errorf("s3store: create bucket: %w", storage.ErrTransport)
	err := r.space(t, "--profile", "dev", "new", "analytics")
	want := "space new: could not create bucket analytics; try again later"
	if err == nil || err.Error() != want {
		t.Fatalf("space new over a transport failure = %v, want %q", err, want)
	}
}

func TestSpaceS3CreateInvalidName(t *testing.T) {
	r := newLoginRig(t)
	r.addS3("dev", "eu-north-1")
	err := r.space(t, "--profile", "dev", "new", "x")
	if !errors.Is(err, storage.ErrInvalidNewSpaceName) || !strings.Contains(err.Error(), "3 to 63") {
		t.Fatalf("space new with an invalid name = %v", err)
	}
	if len(r.s3Configs) != 0 {
		t.Fatalf("AWS configuration loaded for an invalid name: %+v", r.s3Configs)
	}
}

func TestSpaceS3CreateRefusesPrefix(t *testing.T) {
	r := newLoginRig(t)
	admin := r.addS3("dev", "eu-north-1")
	err := r.space(t, "--profile", "dev", "--prefix", "team", "new", "analytics")
	if err == nil || !strings.Contains(err.Error(), "space new: --prefix names the notebook inside an existing store") {
		t.Fatalf("space new with --prefix = %v", err)
	}
	if len(r.s3Configs) != 0 || admin.createCalls != 0 {
		t.Fatalf("configuration loads = %d, creates = %d; a refused create sends nothing", len(r.s3Configs), admin.createCalls)
	}
}

func TestSpaceS3InvalidPrefix(t *testing.T) {
	r := newLoginRig(t)
	r.addS3("dev", "eu-north-1")
	err := r.space(t, "--profile", "dev", "--prefix", "/team")
	if !errors.Is(err, storage.ErrInvalidPrefix) {
		t.Fatalf("space with an invalid prefix = %v, want ErrInvalidPrefix", err)
	}
	if len(r.s3Configs) != 0 {
		t.Fatalf("AWS configuration loaded before the prefix check: %+v", r.s3Configs)
	}
}

func TestSpaceS3SetDefault(t *testing.T) {
	r := newLoginRig(t)
	r.addS3("dev", "eu-north-1", "analytics")
	if err := r.space(t, "--profile", "dev", "analytics"); err != nil {
		t.Fatalf("space --profile dev analytics = %v", err)
	}
	want := "The default bucket for s3 endpoint=(AWS), region=eu-north-1, profile=dev is now analytics.\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	target, ok := r.settingsSet(t).S3Default()
	if !ok {
		t.Fatal("no default stored")
	}
	wantTarget := settings.Target{Kind: settings.KindS3, Region: "eu-north-1", Profile: "dev", Name: "analytics"}
	if target != wantTarget {
		t.Fatalf("stored default = %+v, want %+v", target, wantTarget)
	}
}

func TestSpaceS3SetDefaultPrefix(t *testing.T) {
	r := newLoginRig(t)
	r.addS3("dev", "eu-north-1", "analytics")
	if err := r.space(t, "--profile", "dev", "--prefix", "team", "analytics"); err != nil {
		t.Fatalf("space --prefix team analytics = %v", err)
	}
	target, ok := r.settingsSet(t).S3Default()
	if !ok || target.Prefix != "team" {
		t.Fatalf("stored default = %+v, %v; want the validated prefix", target, ok)
	}
}

func TestSpaceS3DottedBucketPathStyle(t *testing.T) {
	r := newLoginRig(t)
	r.addS3("dev", "eu-north-1", "legacy.example.com")
	if err := r.space(t, "--profile", "dev", "legacy.example.com"); err != nil {
		t.Fatalf("space --profile dev legacy.example.com = %v", err)
	}
	target, ok := r.settingsSet(t).S3Default()
	if !ok || !target.PathStyle {
		t.Fatalf("stored default = %+v, %v; a dotted legacy bucket records path style", target, ok)
	}
}

func TestSpaceS3FailedIdentityKeepsCustomEndpointPathStyle(t *testing.T) {
	r := newLoginRig(t)
	r.addS3("dev", "eu-north-1")
	r.plantSettings(t, s3Record("/d", "analytics", "https://other.example.test", "us-east-1", "remote"))
	r.plantFail("remote", errors.New("no shared config"))
	op := r.preparedSpace(t, "--profile", "dev")
	ctx := context.Background()
	if err := op.prepare(ctx); err != nil {
		t.Fatalf("prepare = %v", err)
	}
	var degraded *s3Space
	for _, backend := range op.backends {
		if s, ok := backend.(*s3Space); ok && s.header.Endpoint == "https://other.example.test" {
			degraded = s
		}
	}
	if degraded == nil || degraded.admin != nil {
		t.Fatalf("backend = %+v; want the failed recorded identity", degraded)
	}
	if err := degraded.SetDefault(ctx, "analytics"); err != nil {
		t.Fatalf("SetDefault = %v", err)
	}
	target, ok := r.settingsSet(t).S3Default()
	if !ok || !target.PathStyle || target.Endpoint != "https://other.example.test" ||
		target.Region != "us-east-1" || target.Profile != "remote" {
		t.Fatalf("stored default = %+v, %v; a custom endpoint requires path style and its identity", target, ok)
	}
}

func TestSpaceS3SetDefaultAmbiguous(t *testing.T) {
	r := newLoginRig(t)
	r.addS3("dev", "eu-north-1", "analytics")
	r.plantSettings(t, s3Record("/p", "analytics", "", "us-east-1", "dev"))
	err := r.space(t, "--profile", "dev", "--region", "eu-north-1", "analytics")
	want := "space: analytics is listed by 2 identities (endpoint=(AWS) region=eu-north-1 profile=dev, endpoint=(AWS) region=us-east-1 profile=dev); pass --region to choose one"
	if err == nil || err.Error() != want {
		t.Fatalf("space analytics with two identities = %v, want %q", err, want)
	}
	if set := r.settingsSet(t); len(set.Defaults()) != 0 {
		t.Fatalf("defaults = %+v; an ambiguity changes nothing", set.Defaults())
	}
}

func TestSpaceS3SetDefaultLocks(t *testing.T) {
	r := newLoginRig(t)
	r.addS3("dev", "eu-north-1", "analytics")
	f := NewSpaceFlags()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	f.Bind(fs)
	if err := fs.Parse([]string{"--profile", "dev", "analytics"}); err != nil {
		t.Fatal(err)
	}
	op, err := PrepareSpace(f, []string{"analytics"}, r.opts())
	if err != nil {
		t.Fatal(err)
	}
	lock, err := r.settingsFile(t).Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Unlock() }()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = op.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "lock") {
		t.Fatalf("space while the settings lock is held = %v, want the lock refusal", err)
	}
	if set, loadErr := r.settingsFile(t).Load(); loadErr != nil || len(set.Defaults()) != 0 {
		t.Fatalf("defaults = %+v, %v; a refused write changes nothing", set.Defaults(), loadErr)
	}
}

func TestSpaceS3SetDefaultWriteFailure(t *testing.T) {
	r := newLoginRig(t)
	r.addS3("dev", "eu-north-1", "analytics")
	f := NewSpaceFlags()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	f.Bind(fs)
	if err := fs.Parse([]string{"--profile", "dev", "analytics"}); err != nil {
		t.Fatal(err)
	}
	op, err := PrepareSpace(f, []string{"analytics"}, r.opts())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.dir, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := op.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "space:") {
		t.Fatalf("space over an unusable settings directory = %v, want the write refusal", err)
	}
}

// TestSpaceS3ListsTheDefaultIdentity proves a bare listing uses the active
// S3 default's own profile, endpoint, and region, not the ambient chain.
func TestSpaceS3ListsTheDefaultIdentity(t *testing.T) {
	r := newLoginRig(t)
	r.addS3("dev", "eu-north-1", "analytics")
	r.plantDefaults(t, settings.Target{Kind: settings.KindS3, Region: "eu-north-1", Profile: "dev", Name: "analytics"})
	if err := r.space(t); err != nil {
		t.Fatalf("space = %v", err)
	}
	want := s3HeaderLine("(AWS)", "eu-north-1", "dev") + "* analytics\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
}

// TestSpaceHostedSetDefaultClearsS3Default proves a hosted selection leaves
// no S3 default behind.
func TestSpaceHostedSetDefaultClearsS3Default(t *testing.T) {
	r := newLoginRig(t)
	r.addS3("", "us-east-1", "bucket")
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, space("team", "", "write")}, "--space", "notes")
	r.plantDefaults(t, settings.Target{Kind: settings.KindS3, Region: "us-east-1", Name: "bucket"})
	if err := r.space(t, "team"); err != nil {
		t.Fatalf("space team = %v", err)
	}
	if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "team" {
		t.Fatalf("default space = %q, want team", got)
	}
	if got := r.settingsSet(t).Defaults(); len(got) != 0 {
		t.Fatalf("defaults = %+v; a hosted selection clears the S3 default", got)
	}
}

// TestSpaceHostedSetDefaultClearFailure proves a failed clear leaves the
// previous S3 default active while the hosted default it already wrote stays
// stored.
func TestSpaceHostedSetDefaultClearFailure(t *testing.T) {
	r := newLoginRig(t)
	r.addS3("", "us-east-1", "bucket")
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, space("team", "", "write")}, "--space", "notes")
	r.plantDefaults(t, settings.Target{Kind: settings.KindS3, Region: "us-east-1", Name: "bucket"})
	if err := os.Mkdir(filepath.Join(r.dir, settings.LockName), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := r.space(t, "team"); err == nil || !strings.Contains(err.Error(), "lock") {
		t.Fatalf("space team with an unusable settings lock = %v", err)
	}
	if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "team" {
		t.Fatalf("default space = %q; the stored login's default was written before the clear", got)
	}
	if def, ok := r.settingsSet(t).S3Default(); !ok || def.Name != "bucket" {
		t.Fatalf("S3 default = %+v, %v; the failed clear must leave the previous default active", def, ok)
	}
}

func TestSpaceS3UnknownProfile(t *testing.T) {
	r := newLoginRig(t)
	r.plantFail("nope", fmt.Errorf("s3store: load AWS configuration: %w", s3store.ErrProfileNotFound))
	err := r.space(t, "--profile", "nope")
	want := "space: no AWS profile named nope; check ~/.aws/config"
	if err == nil || err.Error() != want {
		t.Fatalf("space --profile nope = %v, want %q", err, want)
	}
}

func TestSpaceS3KeyIDIsDisplayOnly(t *testing.T) {
	r := newLoginRig(t)
	r.addS3("dev", "eu-north-1", "analytics")
	if err := r.space(t, "--profile", "dev"); err != nil {
		t.Fatalf("space --profile dev = %v", err)
	}
	if !strings.Contains(r.out.String(), "key="+s3AccessKeyID) {
		t.Fatalf("stdout = %q, want the access key id in the header", r.out.String())
	}
	r.out.Reset()
	r.errOut.Reset()
	if err := r.space(t, "--profile", "dev", "analytics"); err != nil {
		t.Fatalf("space --profile dev analytics = %v", err)
	}
	body, err := os.ReadFile(r.settingsFile(t).Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), s3AccessKeyID) || strings.Contains(r.errOut.String(), s3AccessKeyID) {
		t.Fatalf("settings = %q, stderr = %q; the key id is display only", body, r.errOut.String())
	}
}

func TestSpaceS3SetUnknown(t *testing.T) {
	r := newLoginRig(t)
	r.addS3("dev", "eu-north-1", "analytics")
	err := r.space(t, "--profile", "dev", "nope")
	if err == nil || !strings.Contains(err.Error(), `no backend lists "nope" (the listing has analytics)`) {
		t.Fatalf("space nope = %v", err)
	}
}

func TestSpaceNoBackendReachable(t *testing.T) {
	r := newLoginRig(t)
	err := r.space(t)
	want := "space: no backend is reachable; run 'slivingdoc login' for hosted storage, or set an AWS profile or endpoint for S3"
	if err == nil || err.Error() != want {
		t.Fatalf("space with no backend = %v, want %q", err, want)
	}
}
