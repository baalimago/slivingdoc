package hosted

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/httpstore/gatewaytest"
	"github.com/baalimago/slivingdoc/internal/sitelogin/sitetest"
)

// TestScenarioSameNameSpacesKeepTheirOwnState proves a workspace belongs
// to the hosted space it pulled, not to that space's name: on one machine,
// a directory that pulled account one's "notes" is a first pull for
// account two's "notes", so it is refused because it holds files that
// space lacks; it keeps account one's files and unpublished edit, account
// two's space receives nothing from it, and account one's workspace keeps
// working afterwards.
func TestScenarioSameNameSpacesKeepTheirOwnState(t *testing.T) {
	t.Parallel()
	g := gatewaytest.Start(t)
	g.AddSpace("space-one", 1<<20)
	g.AddSpace("space-two", 1<<20)
	g.GrantAs(AccountOneToken, "space-one", SameNameSpace, false)
	g.GrantAs(AccountTwoToken, "space-two", SameNameSpace, false)
	env, root := CliRoots(t)
	env = append(env, "SLIVINGDOC_ENDPOINT="+g.URL(), "SLIVINGDOC_BUCKET=")
	one := append(env[:len(env):len(env)], "SLIVINGDOC_TOKEN="+AccountOneToken)
	two := append(env[:len(env):len(env)], "SLIVINGDOC_TOKEN="+AccountTwoToken)
	notes := filepath.Join(root, "notes")
	elsewhere := filepath.Join(root, "elsewhere")
	RunCLIOK(t, "real", two, nil, "pull", elsewhere)
	WriteCLIFile(t, filepath.Join(elsewhere, "b.md"), "account two's notes\n")
	RunCLIOK(t, "real", two, nil, "commit", elsewhere, "-m", "account two")
	storedTwo := g.Stored("space-two")

	RunCLIOK(t, "real", one, nil, "pull", notes)
	WriteCLIFile(t, filepath.Join(notes, "a.md"), "account one's notes\n")
	RunCLIOK(t, "real", one, nil, "commit", notes, "-m", "account one")
	WriteCLIFile(t, filepath.Join(notes, "draft.md"), "account one's unsent draft\n")

	code, stdout, stderr := RunCLI(t, "real", two, "pull", notes)
	if code == 0 || !strings.Contains(stdout+stderr, "DIRECTORY_NOT_EMPTY") {
		t.Fatalf("account two's pull into account one's workspace = exit %d, stdout %q, stderr %q; want the first-pull refusal", code, stdout, stderr)
	}
	code, stdout, stderr = RunCLI(t, "real", two, "commit", notes, "-m", "account two")
	if code == 0 {
		t.Fatalf("account two's commit from account one's workspace = exit 0, stdout %q, stderr %q; want a refusal", stdout, stderr)
	}
	for name, want := range map[string]string{"a.md": "account one's notes\n", "draft.md": "account one's unsent draft\n"} {
		if got, err := os.ReadFile(filepath.Join(notes, name)); err != nil || string(got) != want {
			t.Fatalf("%s after account two's calls = %q, %v; want %q", name, got, err, want)
		}
	}
	if stored := g.Stored("space-two"); stored != storedTwo {
		t.Fatalf("account two's space holds %d pack bytes, was %d; want nothing from account one's workspace", stored, storedTwo)
	}

	RunCLIOK(t, "real", one, nil, "commit", notes, "-m", "the draft")
	fresh := filepath.Join(root, "fresh")
	RunCLIOK(t, "real", one, nil, "pull", fresh)
	if got, err := os.ReadFile(filepath.Join(fresh, "draft.md")); err != nil || string(got) != "account one's unsent draft\n" {
		t.Fatalf("account one's draft in a fresh pull = %q, %v; want it published to account one's space", got, err)
	}
}

// TestScenarioSameNameSpacesAcrossLogins proves the same through stored
// logins: after logging out of one account and into another whose space
// of the same name is another space, the new login refuses the old login's
// workspace as a first pull and receives nothing from it.
func TestScenarioSameNameSpacesAcrossLogins(t *testing.T) {
	t.Parallel()
	g, site, env, root := LoginEnv(t)
	Approve(site, g, LoginKey, "write", LoginExpiry)
	RunLogin(t, env, site, LoggedIn(g, "read and write", WithDefault), "--no-browser")
	notes := filepath.Join(root, "notes")
	RunCLIOK(t, "real", env, nil, "pull", notes)
	WriteCLIFile(t, filepath.Join(notes, "a.md"), "ada's notes\n")
	RunCLIOK(t, "real", env, nil, "commit", notes, "-m", "ada")
	WriteCLIFile(t, filepath.Join(notes, "draft.md"), "ada's unsent draft\n")
	if code, stdout, stderr := RunCLI(t, "real", env, "logout"); code != 0 || !strings.HasPrefix(stdout, "Logged out of ada@example.test") {
		t.Fatalf("ada's logout = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}

	g.AddSpace("eve-notes", 1<<20)
	site.SetSpaces(SecondKey, sitetest.Space{Name: HostedSpace, Owner: "eve@example.test", Access: "write", ID: "eve-notes"})
	site.Next(sitetest.Script{Issue: sitetest.Issue{Key: SecondKey, Access: "write", Endpoint: g.URL(), ExpiresAt: LoginExpiry, Account: "eve@example.test"}})
	if code, stdout, stderr := RunCLI(t, "real", env, "login", "--no-browser"); code != 0 || !strings.HasPrefix(stdout, "Logged in as eve@example.test") {
		t.Fatalf("eve's login = exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	elsewhere := filepath.Join(root, "elsewhere")
	RunCLIOK(t, "real", env, nil, "pull", elsewhere)
	WriteCLIFile(t, filepath.Join(elsewhere, "b.md"), "eve's notes\n")
	RunCLIOK(t, "real", env, nil, "commit", elsewhere, "-m", "eve")
	storedEve := g.Stored("eve-notes")

	code, stdout, stderr := RunCLI(t, "real", env, "pull", notes)
	if code == 0 || !strings.Contains(stdout+stderr, "DIRECTORY_NOT_EMPTY") {
		t.Fatalf("eve's pull into ada's workspace = exit %d, stdout %q, stderr %q; want the first-pull refusal", code, stdout, stderr)
	}
	if code, stdout, stderr := RunCLI(t, "real", env, "commit", notes, "-m", "eve"); code == 0 {
		t.Fatalf("eve's commit from ada's workspace = exit 0, stdout %q, stderr %q; want a refusal", stdout, stderr)
	}
	if stored := g.Stored("eve-notes"); stored != storedEve {
		t.Fatalf("eve's space holds %d pack bytes, was %d; want nothing from ada's workspace", stored, storedEve)
	}
	if got, err := os.ReadFile(filepath.Join(notes, "draft.md")); err != nil || string(got) != "ada's unsent draft\n" {
		t.Fatalf("ada's draft after eve's calls = %q, %v; want it kept", got, err)
	}
}
