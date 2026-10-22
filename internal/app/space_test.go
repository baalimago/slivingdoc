package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/s3store"
	"github.com/baalimago/slivingdoc/internal/settings"
	"github.com/baalimago/slivingdoc/internal/sitelogin/sitetest"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/tui"
)

// hostedHost is the header every hosted group of the fixtures prints.
const hostedHost = "HOSTED  " + DefaultHostedEndpoint + "  ada@example.test\n"

// countingDoer records the site requests one space command sent, so a test
// proves a refusal never reached the site.
type countingDoer struct {
	mu    sync.Mutex
	calls map[string]int
}

func newCountingDoer() *countingDoer { return &countingDoer{calls: make(map[string]int)} }

func (d *countingDoer) Do(r *http.Request) (*http.Response, error) {
	d.mu.Lock()
	d.calls[r.Method+" "+r.URL.Path]++
	d.mu.Unlock()
	return http.DefaultClient.Do(r)
}

func (d *countingDoer) count(call string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls[call]
}

// plantSettings writes the scoped settings of the rig's configuration
// directory, beside its credentials file.
func (r *loginRig) plantSettings(t *testing.T, entries ...settings.Entry) {
	t.Helper()
	storeEntries(t, r.opts().Env, entries...)
}

// plantLogins replaces the stored credentials with these logins.
func (r *loginRig) plantLogins(t *testing.T, defs []storedDefault, logins ...storedLogin) {
	t.Helper()
	r.dir = strings.TrimPrefix(writeLogins(t, defs, logins...), credentials.DirEnv+"=")
}

// hostedRecord is a directory that remembers a hosted space at endpoint.
func hostedRecord(path, space, endpoint string) settings.Entry {
	return settings.Entry{Path: path, Target: settings.Target{Kind: settings.KindHosted, Endpoint: endpoint, Name: space}}
}

// replaceSettings writes the settings file exactly as given, which this
// build may refuse to read.
func (r *loginRig) replaceSettings(t *testing.T, body string) {
	t.Helper()
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.dir, settings.FileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// secondLogin stores a login for the development endpoint whose key reaches
// spaces.
func (r *loginRig) secondLogin(t *testing.T, spaces ...sitetest.Space) {
	t.Helper()
	r.site.SetSpaces(otherToken, spaces...)
	r.site.Next(approved(otherToken, "write", devEndpoint))
	if err := r.login(t); err != nil {
		t.Fatalf("second login = %v", err)
	}
	r.out.Reset()
	r.errOut.Reset()
}

func TestSpacePlainRows(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, teamSpace, space("shared", "", "write")})
	if err := r.space(t, "team"); err != nil {
		t.Fatalf("space team = %v", err)
	}
	r.out.Reset()
	r.errOut.Reset()
	if err := r.space(t); err != nil {
		t.Fatalf("space = %v", err)
	}
	want := hostedHost +
		"  notes  write  ada@example.test\n" +
		"  shared  write  a team\n" +
		"* team  read  bob@example.test\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	if r.errOut.String() != "" {
		t.Fatalf("stderr = %q, want no hint with a default", r.errOut.String())
	}
}

func TestSpaceListsEveryLogin(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	r.secondLogin(t, space("dev", "eve@example.test", "read"))
	doer := newCountingDoer()
	r.doer = doer
	if err := r.space(t); err != nil {
		t.Fatalf("space = %v", err)
	}
	want := hostedHost + "* notes  write  ada@example.test\n\n" +
		"HOSTED  " + devEndpoint + "  ada@example.test\n* dev  read  eve@example.test\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	if got := doer.count("GET /cli/v1/spaces"); got != 2 {
		t.Fatalf("spaces requests = %d, want one per login", got)
	}
}

func TestSpaceDirectories(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, teamSpace})
	r.plantSettings(t,
		hostedRecord("/a", "notes", ""),
		hostedRecord("/b", "notes", DefaultHostedEndpoint),
		hostedRecord("/c", "team", ""),
	)
	if err := r.space(t); err != nil {
		t.Fatalf("space = %v", err)
	}
	want := hostedHost +
		"  notes (/a, /b)  write  ada@example.test\n" +
		"  team (/c)  read  bob@example.test\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
}

func TestSpaceRowsShowThreeDirectories(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	r.plantSettings(t,
		hostedRecord("/d1", "notes", ""),
		hostedRecord("/d2", "notes", ""),
		hostedRecord("/d3", "notes", ""),
		hostedRecord("/d4", "notes", ""),
	)
	if err := r.space(t); err != nil {
		t.Fatalf("space = %v", err)
	}
	want := hostedHost + "* notes (/d1, /d2, /d3, +1)  write  ada@example.test\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
}

func TestSpaceNeverTargetsARecord(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	r.plantSettings(t, hostedRecord("/decoy", "decoy", devEndpoint))
	if err := r.space(t); err != nil {
		t.Fatalf("space = %v", err)
	}
	if !strings.Contains(r.out.String(), hostedHost) {
		t.Fatalf("stdout = %q, want the stored login's group", r.out.String())
	}
	if strings.Contains(r.out.String(), "HOSTED  "+devEndpoint+"  ") {
		t.Fatalf("stdout = %q; the record's endpoint became a listing target", r.out.String())
	}
	r.out.Reset()
	r.errOut.Reset()
	if err := r.space(t, "new", "research"); err != nil {
		t.Fatalf("space new = %v", err)
	}
	if !strings.Contains(r.out.String(), "at "+DefaultHostedEndpoint+".") {
		t.Fatalf("stdout = %q; the record became the create target", r.out.String())
	}
}

func TestSpaceDegradedListShowsDefaultAndRecords(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, teamSpace})
	if err := r.space(t, "team"); err != nil {
		t.Fatalf("space team = %v", err)
	}
	r.plantSettings(t, hostedRecord("/d", "notes", ""))
	r.site.Revoke(loginToken)
	r.out.Reset()
	r.errOut.Reset()
	if err := r.space(t); err != nil {
		t.Fatalf("space with a revoked key = %v; a degraded listing exits zero", err)
	}
	want := hostedHost + " ! notes (/d)\n*! team\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	hint := "! the site no longer accepts the stored login for " + DefaultHostedEndpoint + " (revoked or expired); run 'slivingdoc login' again\n"
	if r.errOut.String() != hint {
		t.Fatalf("stderr = %q, want %q", r.errOut.String(), hint)
	}

	r = newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, teamSpace})
	r.site.Revoke(loginToken)
	r.out.Reset()
	r.errOut.Reset()
	if err := r.space(t); err != nil {
		t.Fatalf("space with a revoked key = %v", err)
	}
	if r.out.String() != hostedHost {
		t.Fatalf("stdout = %q; a failed empty group prints no 'No spaces' line", r.out.String())
	}
}

func TestSpaceShowsOrphanRecord(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	r.plantSettings(t, hostedRecord("/x", "orphan", devEndpoint))
	if err := r.space(t); err != nil {
		t.Fatalf("space = %v", err)
	}
	want := hostedHost + "* notes  write  ada@example.test\n\n" +
		"HOSTED  (no stored login)  —\n ! orphan (/x)\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	hint := "! this directory remembers hosted space \"orphan\", which no stored login reaches; run 'slivingdoc login'\n"
	if r.errOut.String() != hint {
		t.Fatalf("stderr = %q, want %q", r.errOut.String(), hint)
	}
}

func TestSpaceShowsMissingDefault(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, teamSpace})
	if err := r.space(t, "team"); err != nil {
		t.Fatalf("space team = %v", err)
	}
	r.site.SetSpaces(loginToken, notesSpace)
	r.out.Reset()
	r.errOut.Reset()
	if err := r.space(t); err != nil {
		t.Fatalf("space = %v", err)
	}
	if want := hostedHost + "  notes  write  ada@example.test\n*! team\n"; r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	if !strings.HasPrefix(r.errOut.String(), "! the default space \"team\" is not among the spaces") {
		t.Fatalf("stderr = %q", r.errOut.String())
	}
}

func TestSpaceIgnoresToken(t *testing.T) {
	r := newLoginRig(t)
	r.extraEnv = []string{"SLIVINGDOC_TOKEN=" + hostedTestToken}
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	if err := r.space(t); err != nil {
		t.Fatalf("space with SLIVINGDOC_TOKEN = %v", err)
	}
	if want := hostedHost + "* notes  write  ada@example.test\n"; r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}

	r = newLoginRig(t)
	r.extraEnv = []string{"SLIVINGDOC_TOKEN=" + hostedTestToken}
	if err := r.space(t); err == nil || !strings.Contains(err.Error(), "no backend is reachable") {
		t.Fatalf("space with a token and no login = %v; a token is not an account credential", err)
	}
}

func TestSpaceNewHosted(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	if err := r.space(t, "new", "research"); err != nil {
		t.Fatalf("space new = %v", err)
	}
	want := "Created hosted space research (write, ada@example.test) at " + DefaultHostedEndpoint + ".\n" +
		"It is not the default; run 'slivingdoc space research' to make it the default.\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	r.out.Reset()
	if err := r.space(t); err != nil {
		t.Fatalf("space = %v", err)
	}
	if want := hostedHost + "* notes  write  ada@example.test\n  research  write  ada@example.test\n"; r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
}

func TestSpaceNewCallsCreateOnce(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	doer := newCountingDoer()
	r.doer = doer
	if err := r.space(t, "new", "research"); err != nil {
		t.Fatalf("space new = %v", err)
	}
	if got := doer.count("POST /cli/v1/spaces"); got != 1 {
		t.Fatalf("create requests = %d, want exactly one", got)
	}
}

func TestSpaceNewLeavesTheDefault(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	if err := r.space(t, "new", "research"); err != nil {
		t.Fatalf("space new = %v", err)
	}
	if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "notes" {
		t.Fatalf("default space = %q, want notes; a create changes no default", got)
	}
	if strings.Contains(r.out.String(), "The default space is now") {
		t.Fatalf("stdout = %q, want no default line", r.out.String())
	}
}

func TestSpaceNewBareKeepsDefault(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{space("new", "", "write"), notesSpace}, "--space", "notes")
	if err := r.space(t, "new"); err == nil || !strings.Contains(err.Error(), "name the space to create") {
		t.Fatalf("space new = %v, want the create-form refusal", err)
	}
	if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "notes" {
		t.Fatalf("default space = %q; a bare 'new' changes no default", got)
	}
	if got := r.settingsSet(t).Defaults(); len(got) != 0 {
		t.Fatalf("defaults = %+v; a bare 'new' writes nothing", got)
	}
}

func TestSpaceNewHintFlags(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	if err := r.space(t, "--storage", "hosted", "--endpoint", "https://API.slivingdoc.dev/", "new", "research"); err != nil {
		t.Fatalf("space new = %v", err)
	}
	want := "It is not the default; run 'slivingdoc space --storage hosted --endpoint " + DefaultHostedEndpoint + " research' to make it the default.\n"
	if !strings.HasSuffix(r.out.String(), want) {
		t.Fatalf("stdout = %q, want it to end with %q", r.out.String(), want)
	}
}

func TestSpaceNewReadOnly(t *testing.T) {
	r := newLoginRig(t)
	r.plantLogins(t, nil, storedLogin{
		Site: r.site.URL(), Endpoint: DefaultHostedEndpoint, Key: loginToken,
		Access: "read", ExpiresAt: loginExpiry.UTC().Format(time.RFC3339), Account: "ada@example.test",
	})
	doer := newCountingDoer()
	r.doer = doer
	if err := r.space(t, "new", "research"); err == nil || err.Error() != readOnlyCreate {
		t.Fatalf("space new with a read-only login = %v, want %q", err, readOnlyCreate)
	}
	if got := doer.count("POST /cli/v1/spaces"); got != 0 {
		t.Fatalf("create requests = %d; a read-only login never reaches the site", got)
	}
}

func TestSpaceNewNeedsOneLogin(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	r.secondLogin(t, space("dev", "", "write"))
	doer := newCountingDoer()
	r.doer = doer
	if err := r.space(t, "new", "research"); err == nil || !strings.Contains(err.Error(), "several stored logins match; pass --endpoint") {
		t.Fatalf("space new with two logins = %v", err)
	}
	if got := doer.count("POST /cli/v1/spaces"); got != 0 {
		t.Fatalf("create requests = %d, want none", got)
	}
}

func TestSpaceNewInvalidName(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	doer := newCountingDoer()
	r.doer = doer
	err := r.space(t, "new", "x")
	if !errors.Is(err, storage.ErrInvalidNewSpaceName) || !strings.Contains(err.Error(), "3 to 63") {
		t.Fatalf("space new with an invalid name = %v", err)
	}
	if got := doer.count("POST /cli/v1/spaces"); got != 0 {
		t.Fatalf("create requests = %d; an invalid name is refused locally", got)
	}
}

func TestSpaceSetDefault(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, teamSpace})
	doer := newCountingDoer()
	r.doer = doer
	if err := r.space(t, "team"); err != nil {
		t.Fatalf("space team = %v", err)
	}
	if want := "The default space is now team (read, bob@example.test).\n"; r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "team" {
		t.Fatalf("default space = %q, want team", got)
	}
	if got := doer.count("GET /cli/v1/spaces"); got != 1 {
		t.Fatalf("spaces requests = %d, want one listing", got)
	}
	if got := doer.count("POST /cli/v1/spaces"); got != 0 {
		t.Fatalf("create requests = %d, want none", got)
	}
}

func TestSpaceSetDefaultAmbiguous(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, teamSpace})
	r.secondLogin(t, notesSpace, teamSpace)
	if err := r.space(t, "team"); err == nil ||
		!strings.Contains(err.Error(), "team is listed by 2 logins ("+DefaultHostedEndpoint+", "+devEndpoint+"); pass --endpoint to choose one") {
		t.Fatalf("space team with two identities = %v", err)
	}
	for _, endpoint := range []string{DefaultHostedEndpoint, devEndpoint} {
		if got := r.defaultSpace(t, endpoint); got != "" {
			t.Fatalf("default space of %s = %q; a refusal changes nothing", endpoint, got)
		}
	}
	r.out.Reset()
	if err := r.space(t, "--endpoint", devEndpoint, "team"); err != nil {
		t.Fatalf("space --endpoint team = %v", err)
	}
	if got := r.defaultSpace(t, devEndpoint); got != "team" {
		t.Fatalf("default space of the chosen login = %q, want team", got)
	}
	if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "" {
		t.Fatalf("default space of the other login = %q; only the chosen identity changed", got)
	}
}

func TestSpaceSetUnknown(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, teamSpace})
	if err := r.space(t, "nope"); err == nil || !strings.Contains(err.Error(), `no backend lists "nope" (the listing has notes, team)`) {
		t.Fatalf("space nope = %v", err)
	}
}

func TestSpaceSetIncomplete(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	r.site.Revoke(loginToken)
	if err := r.space(t, "notes"); err == nil || !strings.Contains(err.Error(), `no reachable backend listed "notes", and a listing failed, so the listing was incomplete; run the command again`) {
		t.Fatalf("space notes over a failed listing = %v", err)
	}
	if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "notes" {
		t.Fatalf("default space = %q; an incomplete listing changes nothing", got)
	}
}

// TestSpaceSetUnlistedDefault proves a successful listing that no longer
// returns the stored default is not reported as an incomplete listing: the
// default is visible, but it cannot become the default again.
func TestSpaceSetUnlistedDefault(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, teamSpace})
	if err := r.space(t, "team"); err != nil {
		t.Fatalf("space team = %v", err)
	}
	r.site.SetSpaces(loginToken, notesSpace)
	r.out.Reset()
	r.errOut.Reset()
	if err := r.space(t, "team"); err == nil ||
		!strings.Contains(err.Error(), `no backend lists "team" (the listing has notes)`) {
		t.Fatalf("space team after the site dropped it = %v", err)
	}
}

func TestSpaceHostedRefusesS3Flags(t *testing.T) {
	for _, row := range []struct {
		name string
		args []string
	}{
		{name: "--profile", args: []string{"--storage", "hosted", "--profile", "dev"}},
		{name: "--region", args: []string{"--storage", "hosted", "--region", "eu-north-1"}},
		{name: "--path-style", args: []string{"--storage", "hosted", "--path-style"}},
		{name: "--prefix", args: []string{"--storage", "hosted", "--prefix", "team"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			r := newLoginRig(t)
			doer := newCountingDoer()
			r.doer = doer
			err := r.space(t, row.args...)
			if err == nil || !strings.Contains(err.Error(), row.name+" names an S3 store") || !strings.Contains(err.Error(), "the backend is hosted") {
				t.Fatalf("space %v = %v, want the S3-flag refusal", row.args, err)
			}
			if len(doer.calls) != 0 {
				t.Fatalf("requests = %v, want none", doer.calls)
			}
		})
	}
}

// TestSpaceHostedIgnoresS3Environment proves an explicit hosted backend is
// not refused by the ambient AWS and S3 environment: only a flag names an
// S3 store beside it.
func TestSpaceHostedIgnoresS3Environment(t *testing.T) {
	r := newLoginRig(t)
	r.extraEnv = []string{"AWS_PROFILE=dev", "AWS_REGION=eu-north-1", "SLIVINGDOC_PREFIX=team", "SLIVINGDOC_PATH_STYLE=true"}
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	if err := r.space(t, "--storage", "hosted"); err != nil {
		t.Fatalf("space --storage hosted with an ambient S3 environment = %v", err)
	}
	if want := hostedHost + "* notes  write  ada@example.test\n"; r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
}

func TestSpaceWithoutLogin(t *testing.T) {
	r := newLoginRig(t)
	if err := r.space(t, "--storage", "hosted"); err == nil ||
		!strings.Contains(err.Error(), "needs a stored login; run 'slivingdoc login'") {
		t.Fatalf("space --storage hosted without a login = %v", err)
	}
	if err := r.space(t, "new", "research"); err == nil ||
		!strings.Contains(err.Error(), "space new: no backend is reachable; run 'slivingdoc login' for hosted storage, or set an AWS profile or endpoint for S3") {
		t.Fatalf("space new without a login = %v", err)
	}
	if err := r.space(t); err == nil || !strings.Contains(err.Error(), "no backend is reachable; run 'slivingdoc login'") {
		t.Fatalf("space without a login = %v", err)
	}
}

func TestSpaceExpiredLoginIsDegraded(t *testing.T) {
	r := newLoginRig(t)
	expired := entry(DefaultHostedEndpoint, loginToken)
	expired.ExpiresAt = longAgo
	r.plantLogins(t, defaults(DefaultHostedEndpoint, "team"), expired)
	if err := r.space(t); err != nil {
		t.Fatalf("space with an expired login = %v; a listing degrades", err)
	}
	if want := hostedHost + "*! team\n"; r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	if !strings.HasPrefix(r.errOut.String(), "! the stored login for "+DefaultHostedEndpoint+" expired") {
		t.Fatalf("stderr = %q", r.errOut.String())
	}
}

// TestSpaceExpiredLoginBesideAUsableOne proves one login's failure is a
// hint, not the command's failure, when another login lists.
func TestSpaceExpiredLoginBesideAUsableOne(t *testing.T) {
	r := newLoginRig(t)
	expired := entry(DefaultHostedEndpoint, loginToken)
	expired.Site, expired.ExpiresAt = r.site.URL(), longAgo
	live := entry(devEndpoint, otherToken)
	live.Site = r.site.URL()
	r.site.SetSpaces(otherToken, space("dev", "eve@example.test", "read"))
	r.site.Issued(otherToken, devEndpoint)
	r.plantLogins(t, nil, expired, live)
	if err := r.space(t); err != nil {
		t.Fatalf("space beside an expired login = %v", err)
	}
	want := hostedHost + "\nHOSTED  " + devEndpoint + "  ada@example.test\n  dev  read  eve@example.test\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	wantErr := "! the stored login for " + DefaultHostedEndpoint + " expired; run 'slivingdoc login' again\n" +
		"No default space; run 'slivingdoc space <name>' to choose one.\n"
	if r.errOut.String() != wantErr {
		t.Fatalf("stderr = %q, want %q", r.errOut.String(), wantErr)
	}
}

func TestSpaceHostedExpiredLoginRefuses(t *testing.T) {
	r := newLoginRig(t)
	expired := entry(DefaultHostedEndpoint, loginToken)
	expired.ExpiresAt = longAgo
	r.plantLogins(t, nil, expired)
	if err := r.space(t, "--storage", "hosted"); err == nil ||
		!strings.Contains(err.Error(), "expired") || !strings.Contains(err.Error(), "'slivingdoc login' again") {
		t.Fatalf("hosted space with an expired login = %v", err)
	}
}

func TestSpaceExpiredLogin(t *testing.T) {
	r := newLoginRig(t)
	expired := entry(DefaultHostedEndpoint, loginToken)
	expired.ExpiresAt = longAgo
	r.plantLogins(t, nil, expired)
	doer := newCountingDoer()
	r.doer = doer
	if err := r.space(t, "new", "research"); err == nil ||
		!strings.Contains(err.Error(), "expired") || !strings.Contains(err.Error(), "'slivingdoc login' again") {
		t.Fatalf("space new with an expired login = %v", err)
	}
	if len(doer.calls) != 0 {
		t.Fatalf("requests = %v; an expired login never reaches the site", doer.calls)
	}
}

func TestSpaceListRevoked(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	r.site.Revoke(loginToken)
	if err := r.space(t); err != nil {
		t.Fatalf("space with a revoked key = %v; a degraded listing exits zero", err)
	}
	if want := hostedHost + "*! notes\n"; r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	if !strings.HasPrefix(r.errOut.String(), "! the site no longer accepts the stored login") ||
		!strings.Contains(r.errOut.String(), "'slivingdoc login' again") {
		t.Fatalf("stderr = %q", r.errOut.String())
	}
}

func TestSpaceUnusableSettings(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	r.replaceSettings(t, `{"version":2}`)
	doer := newCountingDoer()
	r.doer = doer
	if err := r.space(t); err == nil || !strings.Contains(err.Error(), "malformed settings file") || !strings.Contains(err.Error(), "remove it") {
		t.Fatalf("space with an unreadable settings file = %v", err)
	}
	if len(doer.calls) != 0 {
		t.Fatalf("requests = %v; the directory column cannot be trusted", doer.calls)
	}
}

func TestSpaceNewDoesNotReadOrWriteSettings(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	const malformed = `{"version":2}`
	r.replaceSettings(t, malformed)
	settingsPath := filepath.Join(r.dir, settings.FileName)
	before, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := r.space(t, "new", "research"); err != nil {
		t.Fatalf("space new with unusable settings = %v", err)
	}
	want := "Created hosted space research (write, ada@example.test) at " + DefaultHostedEndpoint + ".\n" +
		"It is not the default; run 'slivingdoc space research' to make it the default.\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(after, before) {
		t.Fatalf("settings bytes changed from %q to %q", before, after)
	}
}

func TestSpaceNewNameTaken(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, space("research", "", "write")})
	if err := r.space(t, "new", "research"); err == nil ||
		!strings.Contains(err.Error(), "research is already used by this account; run 'slivingdoc space research' to use it") {
		t.Fatalf("space new for a taken name = %v", err)
	}
}

func TestSpaceNewLimit(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	r.site.RefuseCreate(http.StatusForbidden, "space_limit")
	if err := r.space(t, "new", "research"); err == nil ||
		!strings.Contains(err.Error(), "this account already owns its allowed number of spaces") {
		t.Fatalf("space new at the limit = %v", err)
	}
}

func TestSpaceNewAccessDenied(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	r.site.RefuseCreate(http.StatusForbidden, "access_denied")
	if err := r.space(t, "new", "research"); err == nil || err.Error() != readOnlyCreate {
		t.Fatalf("space new refused with access_denied = %v, want %q", err, readOnlyCreate)
	}
}

func TestSpaceNewBusy(t *testing.T) {
	for _, row := range []struct {
		status int
		code   string
		want   string
	}{
		{status: http.StatusTooManyRequests, code: "busy", want: "the site is busy creating spaces; try again in a minute"},
		{status: http.StatusServiceUnavailable, code: "pool_empty", want: "the site has no space capacity right now; try again later"},
	} {
		t.Run(row.code, func(t *testing.T) {
			r := newLoginRig(t)
			r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
			r.site.RefuseCreate(row.status, row.code)
			if err := r.space(t, "new", "research"); err == nil || !strings.Contains(err.Error(), row.want) {
				t.Fatalf("space new refused with %s = %v", row.code, err)
			}
		})
	}
}

func TestSpaceNewRevoked(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	r.site.Revoke(loginToken)
	if err := r.space(t, "new", "research"); err == nil ||
		!strings.Contains(err.Error(), "the site no longer accepts the stored login (revoked or expired); run 'slivingdoc login' again") {
		t.Fatalf("space new with a revoked key = %v", err)
	}
}

func TestSpaceSetLoginChanged(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	f := NewSpaceFlags()
	op, err := PrepareSpace(f, []string{"notes"}, r.opts())
	if err != nil {
		t.Fatal(err)
	}
	r.site.SetSpaces(otherToken, notesSpace)
	r.site.Next(eve(otherToken))
	if err := r.login(t, "--force"); err != nil {
		t.Fatal(err)
	}
	if err := op.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "the stored login changed") {
		t.Fatalf("space after another login = %v, want the refusal", err)
	}
}

// preparedSpace is a space command prepared from args, so a test can
// inspect its picker choices before it runs.
func (r *loginRig) preparedSpace(t *testing.T, args ...string) *Space {
	t.Helper()
	f := NewSpaceFlags()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f.Bind(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	op, err := PrepareSpace(f, fs.Args(), r.opts())
	if err != nil {
		t.Fatalf("PrepareSpace(%v) = %v", args, err)
	}
	return op
}

// spaceChoices is the prepared command's picker rows.
func (r *loginRig) spaceChoices(t *testing.T, op *Space) []spaceChoice {
	t.Helper()
	if err := op.prepare(context.Background()); err != nil {
		t.Fatalf("prepare = %v", err)
	}
	groups, err := op.listing(context.Background())
	if err != nil {
		t.Fatalf("listing = %v", err)
	}
	return op.choices(groups)
}

func pickerCellTexts(row []tui.Cell) []string {
	out := make([]string, len(row))
	for i, cell := range row {
		out[i] = cell.Text
	}
	return out
}

func assertPickerRows(t *testing.T, choices []spaceChoice, want [][]string) {
	t.Helper()
	rows := spacePickerRows(choices)
	if len(rows) != len(want) {
		t.Fatalf("picker rows = %d, want %d", len(rows), len(want))
	}
	for i, row := range rows {
		if got := pickerCellTexts(row); !slices.Equal(got, want[i]) {
			t.Fatalf("picker row %d = %q, want %q", i, got, want[i])
		}
	}
}

func TestSpacePickerSkip(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, teamSpace}, "--space", "notes")
	r.terminal, r.answer = OnTerminal, typed("q\n")
	if err := r.space(t); err != nil {
		t.Fatalf("space = %v", err)
	}
	if !strings.Contains(r.errOut.String(), "ada@example.test  default\n") || !strings.Contains(r.errOut.String(), "Nothing was changed") {
		t.Fatalf("stderr = %q, want the default marked and nothing changed", r.errOut.String())
	}
	if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "notes" || r.out.String() != "" {
		t.Fatalf("default space = %q, stdout %q; a left picker changes nothing", got, r.out.String())
	}
}

func TestSpacePickerRows(t *testing.T) {
	r := newLoginRig(t)
	entries := make([]settings.Entry, 0, 4)
	for _, path := range []string{"/a", "/b", "/c", "/d"} {
		entries = append(entries, hostedRecord(path, "team", DefaultHostedEndpoint))
	}
	r.plantSettings(t, entries...)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, teamSpace}, "--space", "team")
	target := "hosted " + DefaultHostedEndpoint + " ada@example.test"
	assertPickerRows(t, r.spaceChoices(t, r.preparedSpace(t)), [][]string{
		{target, "notes", "", "write", "ada@example.test", ""},
		{target, "team", "/a, /b, /c, +1", "read", "bob@example.test", "default"},
		{target, "+ Create a new hosted space", "", "", "", ""},
	})
}

func TestSpacePickerUnreachable(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, teamSpace}, "--space", "team")
	r.site.SetSpaces(loginToken, notesSpace)
	target := "hosted " + DefaultHostedEndpoint + " ada@example.test"
	assertPickerRows(t, r.spaceChoices(t, r.preparedSpace(t)), [][]string{
		{target, "notes", "", "write", "ada@example.test", ""},
		{target, "! team", "", "", "", "default"},
		{target, "+ Create a new hosted space", "", "", "", ""},
	})
}

func TestSpacePickerRefusesUnreachable(t *testing.T) {
	r := newLoginRig(t)
	r.plantSettings(t, hostedRecord("/x", "team", DefaultHostedEndpoint))
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace}, "--space", "notes")
	r.site.SetSpaces(loginToken, notesSpace)
	r.terminal, r.answer = OnTerminal, typed("1\n")
	err := r.space(t)
	if err == nil || !strings.Contains(err.Error(), `no backend lists "team" (the listing has notes)`) {
		t.Fatalf("space over an unreachable row = %v, want the listing refusal", err)
	}
	if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "notes" {
		t.Fatalf("default space = %q; an unreachable row changes nothing", got)
	}
}

func TestSpacePickerCreateRows(t *testing.T) {
	t.Run("hosted", func(t *testing.T) {
		r := newLoginRig(t)
		r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
		choices := r.spaceChoices(t, r.preparedSpace(t))
		assertPickerRows(t, choices, [][]string{
			{"hosted " + DefaultHostedEndpoint + " ada@example.test", "notes", "", "write", "ada@example.test", "default"},
			{"hosted " + DefaultHostedEndpoint + " ada@example.test", "+ Create a new hosted space", "", "", "", ""},
		})
	})
	t.Run("s3", func(t *testing.T) {
		r := newLoginRig(t)
		r.addS3("dev", "eu-north-1", "analytics")
		choices := r.spaceChoices(t, r.preparedSpace(t, "--profile", "dev"))
		target := "s3 (AWS) eu-north-1 profile=dev key=" + s3AccessKeyID
		assertPickerRows(t, choices, [][]string{
			{target, "analytics", "", "", "", ""},
			{target, "+ Create a new S3 bucket", "", "", "", ""},
		})
	})
	t.Run("degraded", func(t *testing.T) {
		r := newLoginRig(t)
		admin := r.addS3("dev", "eu-north-1")
		admin.listErr = errors.New("list failed")
		r.plantSettings(t, s3Record("/p", "prod-bucket", "", "eu-north-1", "dev"))
		choices := r.spaceChoices(t, r.preparedSpace(t, "--profile", "dev"))
		target := "s3 (AWS) eu-north-1 profile=dev key=" + s3AccessKeyID
		assertPickerRows(t, choices, [][]string{
			{target, "! prod-bucket", "/p", "", "", ""},
			{target, "+ Create a new S3 bucket", "", "", "", ""},
		})
	})
}

func TestSpacePickerEmptyGroupStillOffersCreate(t *testing.T) {
	t.Run("hosted", func(t *testing.T) {
		r := newLoginRig(t)
		r.loginOnce(t, loginToken, nil)
		choices := r.spaceChoices(t, r.preparedSpace(t))
		assertPickerRows(t, choices, [][]string{
			{"hosted " + DefaultHostedEndpoint + " ada@example.test", "+ Create a new hosted space", "", "", "", ""},
		})
	})
	t.Run("s3", func(t *testing.T) {
		r := newLoginRig(t)
		r.addS3("", "us-east-1")
		choices := r.spaceChoices(t, r.preparedSpace(t, "--storage", "s3"))
		assertPickerRows(t, choices, [][]string{
			{"s3 (AWS) us-east-1 profile=(ambient) key=" + s3AccessKeyID, "+ Create a new S3 bucket", "", "", "", ""},
		})
	})
}

func TestSpacePickerChoosesTheRightBackend(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	r.secondLogin(t, space("dev", "eve@example.test", "read"), space("ops", "eve@example.test", "write"))
	r.terminal, r.answer = OnTerminal, typed("2\n")
	if err := r.space(t); err != nil {
		t.Fatalf("space = %v", err)
	}
	if got := r.defaultSpace(t, devEndpoint); got != "dev" {
		t.Fatalf("default of %s = %q, want dev", devEndpoint, got)
	}
	if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "notes" {
		t.Fatalf("default of the first login = %q; only the chosen backend changed", got)
	}
	if want := "The default space is now dev (read, eve@example.test).\n"; r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
}

func TestSpacePickerCreatesAndSelects(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	r.terminal, r.answer = OnTerminal, typed("1\nresearch\n")
	if err := r.space(t); err != nil {
		t.Fatalf("space = %v", err)
	}
	want := "Created hosted space research (write, ada@example.test) at " + DefaultHostedEndpoint + ".\n" +
		"The default space is now research (write, ada@example.test).\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "research" {
		t.Fatalf("default space = %q, want research", got)
	}
	for _, want := range []string{"Pick the default target", "Name for the new hosted space: "} {
		if !strings.Contains(r.errOut.String(), want) {
			t.Fatalf("stderr = %q, want it to contain %q", r.errOut.String(), want)
		}
	}
}

func TestSpacePickerRefusesAnInvalidS3Name(t *testing.T) {
	r := newLoginRig(t)
	admin := r.addS3("", "us-east-1")
	r.terminal, r.answer = OnTerminal, typed("0\nbad_name\n")
	err := r.space(t, "--storage", "s3")
	want := `space new: storage: invalid new space name: "bad_name" must be 3 to 63 lowercase letters, digits, or inner hyphens`
	if err == nil || err.Error() != want {
		t.Fatalf("space picker with invalid S3 name = %v, want %q", err, want)
	}
	if admin.createCalls != 0 || len(admin.created) != 0 {
		t.Fatalf("S3 create calls = %d, created = %q; invalid name reached the admin", admin.createCalls, admin.created)
	}
}

// afterPrefix runs fn once prefix is exhausted and then blocks, as a
// terminal that never answers does.
type afterPrefix struct {
	prefix *strings.Reader
	effect func()
	done   bool
	ended  chan struct{}
}

func (a *afterPrefix) Read(p []byte) (int, error) {
	if a.prefix.Len() > 0 {
		return a.prefix.Read(p)
	}
	if !a.done {
		a.done = true
		a.effect()
	}
	<-a.ended
	return 0, io.EOF
}

func TestSpacePickerPromptSkips(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input func(*testing.T, context.CancelFunc) (io.Reader, chan os.Signal)
	}{
		{name: "an empty line", input: func(*testing.T, context.CancelFunc) (io.Reader, chan os.Signal) { return typed("0\n\n"), nil }},
		{name: "end of input", input: func(*testing.T, context.CancelFunc) (io.Reader, chan os.Signal) { return typed("0\n"), nil }},
		{name: "a cancelled context", input: func(t *testing.T, cancel context.CancelFunc) (io.Reader, chan os.Signal) {
			reader := &afterPrefix{prefix: strings.NewReader("0\n"), effect: cancel, ended: make(chan struct{})}
			t.Cleanup(func() { close(reader.ended) })
			return reader, nil
		}},
		{name: "Ctrl-C at the prompt", input: func(t *testing.T, _ context.CancelFunc) (io.Reader, chan os.Signal) {
			sig := make(chan os.Signal, 1)
			reader := &afterPrefix{prefix: strings.NewReader("0\n"), effect: func() { sig <- os.Interrupt }, ended: make(chan struct{})}
			t.Cleanup(func() { close(reader.ended) })
			return reader, sig
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newLoginRig(t)
			r.loginOnce(t, loginToken, nil)
			r.terminal = OnTerminal
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var signals chan os.Signal
			r.answer, signals = tt.input(t, cancel)
			op := r.preparedSpace(t)
			op.opts.Signals = signals
			if err := op.Run(ctx); err != nil {
				t.Fatalf("space = %v", err)
			}
			if !strings.Contains(r.errOut.String(), "Nothing was changed") || r.out.String() != "" {
				t.Fatalf("stdout %q stderr %q, want only the skip line", r.out.String(), r.errOut.String())
			}
			if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "" {
				t.Fatalf("default space = %q; a skip changes nothing", got)
			}
		})
	}
}

func TestSpacePickerNotOnAPipe(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, teamSpace})
	if err := r.space(t); err != nil {
		t.Fatalf("space = %v", err)
	}
	if strings.Contains(r.out.String()+r.errOut.String(), "Pick the default target") {
		t.Fatal("a pipe opened the picker")
	}
	if !strings.Contains(r.out.String(), "* notes") && !strings.Contains(r.out.String(), "  notes") {
		t.Fatalf("stdout = %q, want the plain listing", r.out.String())
	}
}

func TestSpacePlainUnchanged(t *testing.T) {
	t.Run("hosted", func(t *testing.T) {
		r := newLoginRig(t)
		r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, teamSpace}, "--space", "team")
		if err := r.space(t); err != nil {
			t.Fatalf("space = %v", err)
		}
		want := hostedHost +
			"  notes  write  ada@example.test\n" +
			"* team  read  bob@example.test\n"
		if r.out.String() != want {
			t.Fatalf("stdout = %q, want %q", r.out.String(), want)
		}
	})
	t.Run("s3", func(t *testing.T) {
		r := newLoginRig(t)
		r.addS3("dev", "eu-north-1", "analytics")
		if err := r.space(t, "--profile", "dev"); err != nil {
			t.Fatalf("space = %v", err)
		}
		want := s3HeaderLine("(AWS)", "eu-north-1", "dev") + "  analytics\n"
		if r.out.String() != want {
			t.Fatalf("stdout = %q, want %q", r.out.String(), want)
		}
	})
}

func TestSpacePickerS3CreateUsesCurrentIdentity(t *testing.T) {
	r := newLoginRig(t)
	dev := r.addS3("dev", "eu-north-1")
	prod := r.addS3("prod", "us-east-1", "prod-bucket")
	r.plantSettings(t, s3Record("/p", "prod-bucket", "", "us-east-1", "prod"))
	r.terminal, r.answer = OnTerminal, typed("2\nanalytics\n")
	if err := r.space(t, "--profile", "dev"); err != nil {
		t.Fatalf("space = %v", err)
	}
	if dev.createCalls != 1 || prod.createCalls != 0 {
		t.Fatalf("dev creates = %d, prod creates = %d; want only the current identity", dev.createCalls, prod.createCalls)
	}
	if got := r.settingsSet(t).Defaults(); len(got) != 1 || got[0].Profile != "dev" || got[0].Name != "analytics" {
		t.Fatalf("defaults = %+v, want the dev identity's analytics", got)
	}
}

// failingWriter refuses every write, so a picker's draw fails.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestSpacePickerDrawFailure(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	opts := r.opts()
	opts.Stderr = failingWriter{}
	opts.Terminal = func() TerminalState { return OnTerminal }
	f := NewSpaceFlags()
	op, err := PrepareSpace(f, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := op.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "space: tui: draw the picker") {
		t.Fatalf("space = %v, want the picker draw failure", err)
	}
}

func TestSpacePickerPromptReadError(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, nil)
	r.terminal = OnTerminal
	r.answer = io.MultiReader(typed("0\n"), iotest.ErrReader(errors.New("input/output error")))
	if err := r.space(t); err == nil || !strings.Contains(err.Error(), "read the name") || !strings.Contains(err.Error(), "nothing was created") {
		t.Fatalf("space = %v, want the prompt read failure", err)
	}
	if r.out.String() != "" {
		t.Fatalf("stdout = %q, want nothing created", r.out.String())
	}
}

func TestSpacePickerCreateRefused(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	r.site.RefuseCreate(http.StatusConflict, "name_taken")
	r.terminal, r.answer = OnTerminal, typed("1\nresearch\n")
	if err := r.space(t); err == nil || !strings.Contains(err.Error(), "research is already used by this account") {
		t.Fatalf("space = %v, want the create refusal", err)
	}
	if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "notes" {
		t.Fatalf("default space = %q; a refused create changes nothing", got)
	}
}

func TestSpacePickerS3CreateRefused(t *testing.T) {
	r := newLoginRig(t)
	admin := r.addS3("dev", "eu-north-1")
	admin.createErr = s3store.ErrBucketOwned
	r.terminal, r.answer = OnTerminal, typed("0\nanalytics\n")
	if err := r.space(t, "--profile", "dev"); err == nil || !strings.Contains(err.Error(), "already exists and this account owns it") {
		t.Fatalf("space = %v, want the S3 create refusal", err)
	}
	if got := r.settingsSet(t).Defaults(); len(got) != 0 {
		t.Fatalf("defaults = %+v; a refused create changes nothing", got)
	}
}

func TestSpacePickerDefaultWriteFailure(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	lock := filepath.Join(r.dir, credentials.LockName)
	if err := os.Remove(lock); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err := os.Mkdir(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	r.terminal, r.answer = OnTerminal, typed("1\nresearch\n")
	if err := r.space(t); err == nil || !strings.Contains(err.Error(), "lock") {
		t.Fatalf("space = %v, want the default write failure", err)
	}
	if want := "Created hosted space research (write, ada@example.test) at " + DefaultHostedEndpoint + ".\n"; r.out.String() != want {
		t.Fatalf("stdout = %q, want the create line before the failure", r.out.String())
	}
	if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "notes" {
		t.Fatalf("default space = %q; the failed write left it unchanged", got)
	}
}

// onFirstRead runs effect before the first byte is returned.
type onFirstRead struct {
	once   sync.Once
	effect func()
	in     io.Reader
}

func (o *onFirstRead) Read(p []byte) (int, error) {
	o.once.Do(o.effect)
	return o.in.Read(p)
}

// replaceLoginKey changes the stored login's key, as another login would.
func (r *loginRig) replaceLoginKey(t *testing.T, key string) {
	t.Helper()
	path := filepath.Join(r.dir, credentials.FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Version  int             `json:"version"`
		Logins   []storedLogin   `json:"logins"`
		Defaults []storedDefault `json:"defaultSpaces"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Logins) != 1 {
		t.Fatalf("stored logins = %d, want one", len(file.Logins))
	}
	file.Logins[0].Key = key
	out, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSpacePickerLoginChanged(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
	r.terminal = OnTerminal
	r.answer = &onFirstRead{effect: func() { r.replaceLoginKey(t, otherToken) }, in: typed("0\n")}
	if err := r.space(t); err == nil || !strings.Contains(err.Error(), "the stored login changed") {
		t.Fatalf("space = %v, want the changed-login refusal", err)
	}
}

func TestSpacePickerRefreshFailure(t *testing.T) {
	r := newLoginRig(t)
	admin := r.addS3("dev", "eu-north-1")
	admin.listErr = errors.New("list failed")
	admin.failAfterList = 1
	r.terminal, r.answer = OnTerminal, typed("0\nanalytics\n")
	if err := r.space(t, "--profile", "dev"); err != nil {
		t.Fatalf("space = %v; a failed refresh is display only", err)
	}
	want := "Created bucket analytics (endpoint=(AWS), region=eu-north-1, profile=dev).\n" +
		"The default bucket for s3 endpoint=(AWS), region=eu-north-1, profile=dev is now analytics.\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	if got := r.settingsSet(t).Defaults(); len(got) != 1 || got[0].Name != "analytics" {
		t.Fatalf("defaults = %+v, want analytics", got)
	}
}

func TestLoginOutputUnchanged(t *testing.T) {
	r := newLoginRig(t)
	r.site.SetSpaces(loginToken, notesSpace, teamSpace)
	r.site.Next(approved(loginToken, "write", DefaultHostedEndpoint))
	if err := r.login(t); err != nil {
		t.Fatalf("login = %v", err)
	}
	plain := "  Spaces:\n    notes (read and write), owned by ada@example.test\n    team (read only), owned by bob@example.test\n"
	if !strings.Contains(r.errOut.String(), plain) || strings.Contains(r.errOut.String(), "HOSTED") {
		t.Fatalf("plain login stderr = %q, want the unchanged space list", r.errOut.String())
	}

	r = newLoginRig(t)
	r.style = styled
	r.site.SetSpaces(loginToken, notesSpace, teamSpace)
	r.site.Next(approved(loginToken, "write", DefaultHostedEndpoint))
	if err := r.login(t); err != nil {
		t.Fatalf("styled login = %v", err)
	}
	if !strings.Contains(r.errOut.String(), "\x1b[2mSPACE\x1b[0m") || !strings.Contains(r.errOut.String(), "\x1b[1mnotes\x1b[0m") ||
		strings.Contains(r.errOut.String(), "HOSTED") {
		t.Fatalf("styled login stderr = %q, want the unchanged spaces table", r.errOut.String())
	}
}
