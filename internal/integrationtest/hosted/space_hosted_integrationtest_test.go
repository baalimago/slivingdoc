package hosted

// The hosted space admin surface seen by a process: list every stored
// login's spaces with their directories, create one, set the default, and
// degrade a failed login instead of failing the command
// (architecture/login.md).

import (
	"os"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/settings"
	"github.com/baalimago/slivingdoc/internal/sitelogin/sitetest"
)

func TestScenarioSpaceHosted(t *testing.T) {
	t.Parallel()
	g, site, env, _ := LoginEnv(t)
	const second = "second-space"
	g.AddSpace(second, 1<<20)
	Approve(site, g, LoginKey, "write", LoginExpiry)
	site.SetSpaces(LoginKey, TeamNotes, sitetest.Space{Name: second, Owner: "bob@example.test", Access: "read"})
	RunLogin(t, env, site, LoggedIn(g, "read and write", "until 2026-12-26 09:00 UTC"), "--no-browser")

	header := "HOSTED  " + g.URL() + "  " + LoginAccount + "\n"
	RunCLIExact(t, "real", env, header+
		"  second-space  read  bob@example.test\n"+
		"  team-notes  write  "+LoginAccount+"\n", "space")
	RunCLIExact(t, "real", env, "The default space is now team-notes (write, ada@example.test).\n", "space", HostedSpace)

	// A hosted selection also clears every stored S3 default, so the hosted
	// default is the one active default. The closed endpoint proves the
	// command never tries to list an S3 backend it is about to clear.
	MustDo(t, os.MkdirAll(CredentialsDir(env), 0o700))
	PlantSettings(t, env, S3SettingsFile(settings.Target{
		Kind: settings.KindS3, Endpoint: ClosedS3, Region: "us-east-1", Name: "bucket", PathStyle: true,
	}))
	RunCLIExact(t, "real", env, "The default space is now team-notes (write, ada@example.test).\n", "space", "--storage", "hosted", HostedSpace)
	if got := LoadSettings(t, env).Defaults(); len(got) != 0 {
		t.Fatalf("defaults = %+v; a hosted selection clears the S3 default", got)
	}

	RunCLIExact(t, "real", env, header+
		"  second-space  read  bob@example.test\n"+
		"* team-notes  write  "+LoginAccount+"\n", "space")

	// Create prints the target and never changes the default.
	RunCLIExact(t, "real", env,
		"Created hosted space research (write, ada@example.test) at "+g.URL()+".\n"+
			"It is not the default; run 'slivingdoc space research' to make it the default.\n",
		"space", "new", "research")
	RunCLIExact(t, "real", env, header+
		"  research  write  "+LoginAccount+"\n"+
		"  second-space  read  bob@example.test\n"+
		"* team-notes  write  "+LoginAccount+"\n", "space")

	// A revoked key degrades the group: the default stays visible with the
	// literal "! " hint, and the command exits zero.
	site.Revoke(LoginKey)
	code, stdout, stderr := RunCLI(t, "real", env, "space")
	if code != 0 {
		t.Fatalf("space with a revoked key = exit %d, want 0; stderr: %s", code, stderr)
	}
	if want := header + "*! team-notes\n"; stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
	hint := "! the site no longer accepts the stored login for " + g.URL() + " (revoked or expired); run 'slivingdoc login' again\n"
	if stderr != hint {
		t.Fatalf("stderr = %q, want %q", stderr, hint)
	}
}

func TestScenarioSpaceNewInvalidNameRefusal(t *testing.T) {
	t.Parallel()
	env, _ := CliRoots(t)
	code, stdout, stderr := RunCLI(t, "real", env, "space", "new", "x")
	if code == 0 {
		t.Fatalf("space new x = exit %d, want nonzero", code)
	}
	if stdout != "" {
		t.Fatalf("space new x stdout = %q, want empty", stdout)
	}
	const want = "error: space new: storage: invalid new space name: \"x\" must be 3 to 63 lowercase letters, digits, or inner hyphens\n"
	if stderr != want {
		t.Fatalf("space new x stderr = %q, want exactly %q", stderr, want)
	}
}
