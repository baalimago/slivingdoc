package hosted

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/mcp"
	"github.com/baalimago/slivingdoc/internal/sitelogin/sitetest"
)

// assertSignInRequired asserts err is the answer of a serve that waits for
// the person to approve its sign-in: retryable, with the approval page and
// the code in the text item the agent passes on.
func assertSignInRequired(t *testing.T, te mcp.ToolError, text, page string) {
	t.Helper()
	if te.Code != "STORAGE_FAILURE" || te.Reason != "SIGN_IN_REQUIRED" || te.Action != "RETRY" || !te.Retryable {
		t.Fatalf("tool error = %+v, want STORAGE_FAILURE · SIGN_IN_REQUIRED, retryable", te)
	}
	for _, want := range []string{page, "BCDF-GHJK", "call this tool again"} {
		if !strings.Contains(te.Message, want) || !strings.Contains(text, want) {
			t.Fatalf("tool error message %q / text %q, want both to name %q", te.Message, text, want)
		}
	}
}

// TestScenarioServeSignsInOnFirstUse proves a serve started with no
// credential at all comes up with both tools, starts the device login when
// a tool is first called, answers with the approval page while the person
// has not approved it, and serves the login's only space once they have,
// with the login stored for every later process and its key only ever at
// the site.
func TestScenarioServeSignsInOnFirstUse(t *testing.T) {
	t.Parallel()
	g, site, env, root := LoginEnv(t)
	Approve(site, g, LoginKey, "write", LoginExpiry)
	site.Undecided()
	served := filepath.Join(root, "served")
	h := SpawnHelper(t, "real", append(append([]string(nil), env...), "SLIVINGDOC_WORKSPACE_ROOT="+served), "serve")
	cs := h.ConnectClient(t)

	te, text := CallToolError(t, cs, ToolPull)
	assertSignInRequired(t, te, text, site.URL()+"/cli/login#BCDF-GHJK")
	if starts := site.Starts(); len(starts) != 1 || starts[0].Access != "write" {
		t.Fatalf("start requests = %+v, want one write login", starts)
	}

	site.Decide()
	AwaitSignIn(t, cs, served)
	WriteCLIFile(t, filepath.Join(served, "a.md"), "signed in on first use\n")
	AssertProcessCallOK(t, cs, ToolCommit, served, "after the first sign-in")
	h.CloseClient(t, cs)
	if g.Stored(HostedSpace) == 0 {
		t.Fatal("the space holds no pack bytes after a commit through the first sign-in")
	}
	if len(site.Starts()) != 1 {
		t.Fatalf("start requests = %d, want the one sign-in", len(site.Starts()))
	}
	if stderr := h.StderrText(t); !strings.Contains(stderr, "Approved by "+LoginAccount) || !strings.Contains(stderr, "Logged in as "+LoginAccount) {
		t.Fatalf("serve stderr = %q, want the approval and the result line of the login", stderr)
	}

	// The login is stored: a one-shot pull uses it with no flag at all.
	notes := filepath.Join(root, "notes")
	RunCLIOK(t, "real", env, nil, "pull", notes)
	if data, err := os.ReadFile(filepath.Join(notes, "a.md")); err != nil || string(data) != "signed in on first use\n" {
		t.Fatalf("pulled a.md = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(CredentialsDir(env), credentials.SignInName)); !os.IsNotExist(err) {
		t.Fatalf("the sign-in record outlived the sign-in: %v", err)
	}
	AssertKeyStayedAtTheSite(t, g, site, env)
}

// TestScenarioServesShareOneSignIn proves two servers started together
// without a credential run one device login between them: both answer with
// the same page, the site sees one start, and both serve once it is
// approved.
func TestScenarioServesShareOneSignIn(t *testing.T) {
	t.Parallel()
	g, site, env, root := LoginEnv(t)
	Approve(site, g, LoginKey, "write", LoginExpiry)
	site.Undecided()
	page := site.URL() + "/cli/login#BCDF-GHJK"
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	h1 := SpawnHelper(t, "real", append(append([]string(nil), env...), "SLIVINGDOC_WORKSPACE_ROOT="+first), "serve")
	h2 := SpawnHelper(t, "real", append(append([]string(nil), env...), "SLIVINGDOC_WORKSPACE_ROOT="+second), "serve")
	cs1, cs2 := h1.ConnectClient(t), h2.ConnectClient(t)

	te, text := CallToolError(t, cs1, ToolPull)
	assertSignInRequired(t, te, text, page)
	te, text = CallToolError(t, cs2, ToolPull)
	assertSignInRequired(t, te, text, page)
	if starts := site.Starts(); len(starts) != 1 {
		t.Fatalf("start requests = %d, want one sign-in shared by both servers", len(starts))
	}

	site.Decide()
	AwaitSignIn(t, cs1, first)
	AwaitSignIn(t, cs2, second)
	h1.CloseClient(t, cs1)
	h2.CloseClient(t, cs2)
	if starts := site.Starts(); len(starts) != 1 {
		t.Fatalf("start requests = %d, want one sign-in shared by both servers", len(starts))
	}
}

// TestScenarioServeSignInCreatesFirstSpace proves the first sign-in of an
// account that has no space yet creates one called notes and serves it, so
// a new account works without a visit to the site.
func TestScenarioServeSignInCreatesFirstSpace(t *testing.T) {
	t.Parallel()
	g, site, env, root := LoginEnv(t)
	g.AddSpace("notes", 1<<20)
	Approve(site, g, LoginKey, "write", LoginExpiry)
	site.SetSpaces(LoginKey)
	served := filepath.Join(root, "served")
	h := SpawnHelper(t, "real", append(append([]string(nil), env...), "SLIVINGDOC_WORKSPACE_ROOT="+served), "serve")
	cs := h.ConnectClient(t)
	AwaitSignIn(t, cs, served)
	h.CloseClient(t, cs)
	if mints := site.Mints(); len(mints) == 0 || mints[0].Space != "notes" {
		t.Fatalf("mints = %+v, want tokens for the created space notes", mints)
	}
	if stderr := h.StderrText(t); !strings.Contains(stderr, `Created the space "notes".`) {
		t.Fatalf("serve stderr = %q, want the created space", stderr)
	}
}

// TestScenarioServeLoginNever proves --login never keeps the startup
// refusal of a serve without a credential, and that the one-shot commands
// never sign in.
func TestScenarioServeLoginNever(t *testing.T) {
	t.Parallel()
	_, site, env, root := LoginEnv(t)
	code, _, stderr := RunCLI(t, "real", env, "serve", "--login", "never")
	if code == 0 || !strings.Contains(stderr, "bucket is required") {
		t.Fatalf("serve --login never = exit %d, stderr %q; want the refusal", code, stderr)
	}
	code, _, stderr = RunCLI(t, "real", append(append([]string(nil), env...), "SLIVINGDOC_LOGIN=bogus"), "serve")
	if code == 0 || !strings.Contains(stderr, "--login must be auto or never") {
		t.Fatalf("serve with SLIVINGDOC_LOGIN=bogus = exit %d, stderr %q; want the refusal", code, stderr)
	}
	code, _, stderr = RunCLI(t, "real", env, "pull", filepath.Join(root, "notes"))
	if code == 0 || !strings.Contains(stderr, "bucket is required") {
		t.Fatalf("pull without a credential = exit %d, stderr %q; want the refusal", code, stderr)
	}
	if starts := site.Starts(); len(starts) != 0 {
		t.Fatalf("start requests = %d, want none", len(starts))
	}
}

// TestScenarioServeSignInDeniedThenRetried proves a sign-in the person
// denies ends that call with why, and the next call starts a new one that
// signs in.
func TestScenarioServeSignInDeniedThenRetried(t *testing.T) {
	t.Parallel()
	g, site, env, root := LoginEnv(t)
	Approve(site, g, LoginKey, "write", LoginExpiry)
	site.Queue(sitetest.Script{Final: "access_denied"})
	served := filepath.Join(root, "served")
	h := SpawnHelper(t, "real", append(append([]string(nil), env...), "SLIVINGDOC_WORKSPACE_ROOT="+served), "serve")
	cs := h.ConnectClient(t)

	te, text := CallToolError(t, cs, ToolPull)
	if te.Reason != "SIGN_IN_REQUIRED" || !strings.Contains(text, "The last sign-in ended without a login") || !strings.Contains(text, "start a new sign-in") {
		t.Fatalf("pull after a denied sign-in = %+v, text %q; want why it ended and the next step", te, text)
	}
	AwaitSignIn(t, cs, served)
	h.CloseClient(t, cs)
	if starts := site.Starts(); len(starts) != 2 {
		t.Fatalf("start requests = %d, want the denied one and the next", len(starts))
	}
}
