package integrationtest

// The hosted space admin surface seen by a process: list every stored
// login's spaces with their directories, create one, set the default, and
// degrade a failed login instead of failing the command
// (architecture/login.md).

import (
	"testing"

	"github.com/baalimago/slivingdoc/internal/sitelogin/sitetest"
)

func TestScenarioSpaceHosted(t *testing.T) {
	t.Parallel()
	g, site, env, _ := loginEnv(t)
	const second = "second-space"
	g.AddSpace(second, 1<<20)
	approve(site, g, loginKey, "write", loginExpiry)
	site.SetSpaces(loginKey, teamNotes, sitetest.Space{Name: second, Owner: "bob@example.test", Access: "read"})
	runLogin(t, env, site, loggedIn(g, "read and write", "until 2026-12-26 09:00 UTC"), "--no-browser")

	header := "HOSTED  " + g.URL() + "  " + loginAccount + "\n"
	runCLIExact(t, "real", env, header+
		"  second-space  read  bob@example.test\n"+
		"  team-notes  write  "+loginAccount+"\n", "space")
	runCLIExact(t, "real", env, "The default space is now team-notes (write, ada@example.test).\n", "space", hostedSpace)
	runCLIExact(t, "real", env, header+
		"  second-space  read  bob@example.test\n"+
		"* team-notes  write  "+loginAccount+"\n", "space")

	// Create prints the target and never changes the default.
	runCLIExact(t, "real", env,
		"Created hosted space research (write, ada@example.test) at "+g.URL()+".\n"+
			"It is not the default; run 'slivingdoc space research' to make it the default.\n",
		"space", "new", "research")
	runCLIExact(t, "real", env, header+
		"  research  write  "+loginAccount+"\n"+
		"  second-space  read  bob@example.test\n"+
		"* team-notes  write  "+loginAccount+"\n", "space")

	// A revoked key degrades the group: the default stays visible with the
	// literal "! " hint, and the command exits zero.
	site.Revoke(loginKey)
	code, stdout, stderr := runCLI(t, "real", env, "space")
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
	env, _ := cliRoots(t)
	code, stdout, stderr := runCLI(t, "real", env, "space", "new", "x")
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
