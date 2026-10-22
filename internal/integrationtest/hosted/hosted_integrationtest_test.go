package hosted

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/httpstore"
	"github.com/baalimago/slivingdoc/internal/httpstore/gatewaytest"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// TestScenarioHostedRoundTrip proves the hosted workflow end to end: a
// token and a space name are the whole configuration, a one-shot pull and
// commit publish through the storage API, and a second workspace pulls the
// published file back from the space.
func TestScenarioHostedRoundTrip(t *testing.T) {
	t.Parallel()
	g, env, root := HostedEnv(t, 1<<20)
	notes := filepath.Join(root, "notes")

	RunCLIExact(t, "real", env,
		"OK  generation 0  "+notes+"\n0 files changed, 0 insertions(+), 0 deletions(-)\n",
		"pull", notes)
	WriteCLIFile(t, filepath.Join(notes, "a.md"), "hosted notes\n")
	RunCLIExact(t, "real", env,
		"OK  generation 1  "+notes+"\n  a.md  +1\n1 files changed, 1 insertions(+), 0 deletions(-)\n",
		"commit", notes, "-m", "hosted commit")
	if g.Stored(HostedSpace) == 0 {
		t.Fatal("the space holds no pack bytes after a published commit")
	}

	other := filepath.Join(root, "other")
	RunCLIOK(t, "real", env, nil, "pull", other)
	got, err := os.ReadFile(filepath.Join(other, "a.md"))
	if err != nil || string(got) != "hosted notes\n" {
		t.Fatalf("second workspace a.md = %q, %v; want the published file", got, err)
	}
}

// TestScenarioHostedSpaceFromToken proves the token alone is the hosted
// configuration: with no --bucket the process uses the one space the token
// reaches, and naming that same space explicitly addresses the same
// notebook and the same private state, so adding or dropping --bucket
// never forks a workspace.
func TestScenarioHostedSpaceFromToken(t *testing.T) {
	t.Parallel()
	g, named, root := HostedEnv(t, 1<<20)
	tokenOnly := WithoutBucket(named)
	notes := filepath.Join(root, "notes")

	RunCLIOK(t, "real", tokenOnly, nil, "pull", notes)
	WriteCLIFile(t, filepath.Join(notes, "a.md"), "from the token alone\n")
	RunCLIOK(t, "real", tokenOnly, nil, "commit", notes, "-m", "token only")
	if g.Stored(HostedSpace) == 0 {
		t.Fatal("the token's space holds no pack bytes after a commit without --bucket")
	}

	WriteCLIFile(t, filepath.Join(notes, "a.md"), "then with --bucket\n")
	RunCLIOK(t, "real", tokenOnly, nil, "commit", notes, "-m", "named", "--bucket", HostedSpace)

	other := filepath.Join(root, "other")
	RunCLIOK(t, "real", tokenOnly, nil, "pull", other)
	got, err := os.ReadFile(filepath.Join(other, "a.md"))
	if err != nil || string(got) != "then with --bucket\n" {
		t.Fatalf("second workspace a.md = %q, %v; want the file published under the explicit name", got, err)
	}
}

// TestScenarioHostedServerWithoutTokenLookup proves a server that predates
// GET /v1/token still works with --bucket, and that without one the
// process refuses to start rather than guess a space.
func TestScenarioHostedServerWithoutTokenLookup(t *testing.T) {
	t.Parallel()
	g, named, root := HostedEnv(t, 1<<20)
	g.DisableTokenLookup()
	RunCLIOK(t, "real", named, nil, "pull", filepath.Join(root, "notes"))

	tokenOnly := WithoutBucket(named)
	code, stdout, stderr := RunCLI(t, "real", tokenOnly, "pull", filepath.Join(root, "other"))
	if code == 0 || strings.TrimSpace(stdout) != "" {
		t.Fatalf("pull without --bucket against an older server = exit %d, stdout %q; want a startup refusal", code, stdout)
	}
	if !strings.Contains(stderr, "pass the space name as --space") {
		t.Fatalf("startup refusal stderr = %q, want it to ask for --bucket", stderr)
	}
}

// TestScenarioHostedStorageFull proves the over-quota contract: a commit
// that would take the space past its quota fails with STORAGE_FULL, says
// how to fix it, is not retryable, keeps the visible edit, and publishes
// nothing; pulls keep working; deleting notes makes room; a used-up request
// allowance is REQUEST_LIMIT; and a commit succeeds once there is room.
func TestScenarioHostedStorageFull(t *testing.T) {
	t.Parallel()
	g, env, root := HostedEnv(t, 1<<20)
	notes := filepath.Join(root, "notes")
	RunCLIOK(t, "real", env, nil, "pull", notes)
	WriteCLIFile(t, filepath.Join(notes, "a.md"), Incompressible(4096))
	RunCLIOK(t, "real", env, nil, "commit", notes, "-m", "fits")

	stored := g.Stored(HostedSpace)
	g.SetQuota(HostedSpace, stored)
	WriteCLIFile(t, filepath.Join(notes, "b.md"), "does not fit\n")
	code, stdout, stderr := RunCLI(t, "real", env, "commit", notes, "-m", "too much")
	if code != 1 {
		t.Fatalf("commit over quota = exit %d, want 1; stdout: %q stderr: %s", code, stdout, stderr)
	}
	for _, want := range []string{
		"STORAGE_FAILURE · STORAGE_FULL",
		"account that owns this space is full",
		"The storage says: refused: quota_exceeded (storage_full)",
		"https://slivingdoc.dev",
		"Pulls keep working",
		"retryable: false",
		"next: operator attention needed",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("over-quota report %q does not contain %q", stdout, want)
		}
	}
	if strings.Contains(stdout+stderr, HostedToken) {
		t.Fatal("the over-quota report leaks the token")
	}
	if got := g.Stored(HostedSpace); got != stored {
		t.Fatalf("stored bytes after the refused commit = %d, want %d", got, stored)
	}
	if got, err := os.ReadFile(filepath.Join(notes, "b.md")); err != nil || string(got) != "does not fit\n" {
		t.Fatalf("visible edit after the refused commit = %q, %v; want it kept", got, err)
	}

	RunCLIOK(t, "real", env, nil, "pull", filepath.Join(root, "reader"))

	// Deleting notes makes room: the refused increment is published as a
	// checkpoint of the smaller state, and the old packs are removed.
	if err := os.Remove(filepath.Join(notes, "b.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(notes, "a.md")); err != nil {
		t.Fatal(err)
	}
	WriteCLIFile(t, filepath.Join(notes, "small.md"), "s\n")
	RunCLIOK(t, "real", env, nil, "commit", notes, "-m", "delete to make room")
	if got := g.Stored(HostedSpace); got >= stored {
		t.Fatalf("stored bytes after deleting notes = %d, want less than %d", got, stored)
	}
	RunCLIOK(t, "real", env, nil, "pull", filepath.Join(root, "reader"))

	g.SetQuota(HostedSpace, 1<<20)
	WriteCLIFile(t, filepath.Join(notes, "c.md"), "room again\n")
	g.RefuseNextWithReason(http.MethodPut, http.StatusInsufficientStorage, "quota_exceeded", "request_limit")
	code, stdout, stderr = RunCLI(t, "real", env, "commit", notes, "-m", "allowance used")
	if code != 1 || !strings.Contains(stdout, "STORAGE_FAILURE · REQUEST_LIMIT") || !strings.Contains(stdout, "first of the month") {
		t.Fatalf("commit past the request allowance = exit %d, stdout %q, stderr %s; want REQUEST_LIMIT", code, stdout, stderr)
	}

	RunCLIOK(t, "real", env, nil, "commit", notes, "-m", "room again")
}

// TestScenarioHostedReadOnlyToken proves a read-only token pulls the space
// but its commit is refused as ACCESS_DENIED, not retryable, with nothing
// published.
func TestScenarioHostedReadOnlyToken(t *testing.T) {
	t.Parallel()
	g, env, root := HostedEnv(t, 1<<20)
	writer := filepath.Join(root, "writer")
	RunCLIOK(t, "real", env, nil, "pull", writer)
	WriteCLIFile(t, filepath.Join(writer, "a.md"), "from the writer\n")
	RunCLIOK(t, "real", env, nil, "commit", writer, "-m", "writer")
	stored := g.Stored(HostedSpace)

	readerEnv := append(append([]string(nil), env...), "SLIVINGDOC_TOKEN="+HostedReader)
	reader := filepath.Join(root, "reader")
	RunCLIOK(t, "real", readerEnv, nil, "pull", reader)
	WriteCLIFile(t, filepath.Join(reader, "b.md"), "from the reader\n")
	code, stdout, stderr := RunCLI(t, "real", readerEnv, "commit", reader, "-m", "reader")
	if code != 1 {
		t.Fatalf("read-only commit = exit %d, want 1; stdout: %q stderr: %s", code, stdout, stderr)
	}
	for _, want := range []string{"STORAGE_FAILURE · ACCESS_DENIED", "SLIVINGDOC_TOKEN", "retryable: false"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("read-only report %q does not contain %q", stdout, want)
		}
	}
	if g.Stored(HostedSpace) != stored {
		t.Fatal("a read-only commit changed the space")
	}
}

// TestScenarioHostedStartupRefusals proves every hosted misconfiguration
// refuses before any operation, with a diagnostic that names the fix and
// never echoes the token: an unknown token, a space the token was not
// granted, an invalid space name, and a token bound for a plain-HTTP
// remote endpoint.
func TestScenarioHostedStartupRefusals(t *testing.T) {
	t.Parallel()
	g := gatewaytest.Start(t)
	g.AddSpace(HostedSpace, 1<<20)
	g.AddSpace("elsewhere", 1<<20)
	g.Grant(HostedToken, HostedSpace, false)
	for _, row := range []struct {
		Name string
		env  []string
		want string
	}{
		{
			Name: "unknown token",
			env:  []string{"SLIVINGDOC_TOKEN=sld_unknown_token", "SLIVINGDOC_BUCKET=" + HostedSpace},
			want: "refused the token",
		},
		{
			Name: "bucket names another space than the token's",
			env:  []string{"SLIVINGDOC_TOKEN=" + HostedToken, "SLIVINGDOC_BUCKET=elsewhere"},
			want: `the token reaches hosted space "team-notes", not "elsewhere"`,
		},
		{
			Name: "unknown token without a bucket",
			env:  []string{"SLIVINGDOC_TOKEN=sld_unknown_token", "SLIVINGDOC_BUCKET="},
			want: "refused the token",
		},
		{
			Name: "invalid space name",
			env:  []string{"SLIVINGDOC_TOKEN=" + HostedToken, "SLIVINGDOC_BUCKET=Team_Notes"},
			want: "invalid space name",
		},
		{
			Name: "plain http remote endpoint",
			env: []string{
				"SLIVINGDOC_TOKEN=" + HostedToken, "SLIVINGDOC_BUCKET=" + HostedSpace,
				"SLIVINGDOC_ENDPOINT=http://api.example.test",
			},
			want: "must use https",
		},
	} {
		t.Run(row.Name, func(t *testing.T) {
			t.Parallel()
			env, root := CliRoots(t)
			env = append(env, "SLIVINGDOC_ENDPOINT="+g.URL())
			env = append(env, row.env...)
			code, stdout, stderr := RunCLI(t, "real", env, "pull", filepath.Join(root, "notes"))
			if code == 0 {
				t.Fatalf("pull = exit 0, want a startup refusal; stdout: %q", stdout)
			}
			if strings.TrimSpace(stdout) != "" {
				t.Fatalf("startup refusal wrote stdout: %q", stdout)
			}
			if !strings.Contains(stderr, row.want) {
				t.Fatalf("startup refusal stderr = %q, want it to contain %q", stderr, row.want)
			}
			if strings.Contains(stderr, HostedToken) || strings.Contains(stderr, "sld_unknown_token") {
				t.Fatalf("startup refusal stderr = %q leaks the token", stderr)
			}
		})
	}
}

// TestScenarioHostedSpaceGoneMidSession proves a long-running serve process
// never reads a space that became unreachable as an empty notebook. After
// the space is deleted or the grant moves, notes_pull and notes_commit are
// STORAGE_FAILURE/ACCESS_DENIED, action OPERATOR, not retryable, and the
// notebook directory is byte-identical, with and without a local edit. The
// startup access check passed long before, so only the read of current can
// tell an unreachable space from an empty one (architecture/hosted-mode.md).
func TestScenarioHostedSpaceGoneMidSession(t *testing.T) {
	t.Parallel()
	for _, row := range HostedCuts {
		for _, edit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/local edit %v", row.Name, edit), func(t *testing.T) {
				t.Parallel()
				g, env, root := HostedEnv(t, 1<<20)
				h := SpawnHelper(t, "real", env, "serve")
				cs := h.ConnectClient(t)
				notes := root

				AssertProcessCallOK(t, cs, ToolPull, notes, "")
				WriteCLIFile(t, filepath.Join(notes, "a.md"), "first note\n")
				WriteCLIFile(t, filepath.Join(notes, "sub", "b.md"), "second note\n")
				AssertProcessCallOK(t, cs, ToolCommit, notes, "two notes")
				if got := AssertProcessCallOK(t, cs, ToolPull, notes, ""); got.Generation != 1 {
					t.Fatalf("pull after the commit = generation %d, want 1", got.Generation)
				}
				if edit {
					HostedLocalEdit(t, notes)
				}
				before := FSSnapshotDir(t, notes)

				row.Cut(g)
				for _, tool := range []string{ToolPull, ToolCommit} {
					got := ProcessError(t, cs, tool, "after the space is gone")
					if got.Code != "STORAGE_FAILURE" || got.Reason != "ACCESS_DENIED" || got.Action != "OPERATOR" || got.Retryable {
						t.Fatalf("%s on an unreachable space = %s/%s/%s retryable %v, want STORAGE_FAILURE/ACCESS_DENIED/OPERATOR not retryable; message: %s",
							tool, got.Code, got.Reason, got.Action, got.Retryable, got.Message)
					}
					if strings.Contains(got.Message, HostedToken) {
						t.Fatalf("%s refusal leaks the token: %s", tool, got.Message)
					}
					AssertTreeKept(t, notes, before)
				}

				h.CloseClient(t, cs)
			})
		}
	}
}

// TestScenarioHostedSpaceGoneDuringOneShotPull proves the one-shot pull has
// the same guarantee when the space becomes unreachable after its startup
// access check passed and before it reads current: exit 1 with the
// ACCESS_DENIED report, and the notebook directory byte-identical. A
// space already unreachable when the process starts is refused by the
// access check (TestScenarioHostedStartupRefusals).
func TestScenarioHostedSpaceGoneDuringOneShotPull(t *testing.T) {
	t.Parallel()
	for _, row := range HostedCuts {
		for _, edit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/local edit %v", row.Name, edit), func(t *testing.T) {
				t.Parallel()
				g, env, root := HostedEnv(t, 1<<20)
				env = append(env, "SLIVINGDOC_PREFIX="+HostedPrefix)
				notes := filepath.Join(root, "notes")
				RunCLIOK(t, "real", env, nil, "pull", notes)
				WriteCLIFile(t, filepath.Join(notes, "a.md"), "first note\n")
				WriteCLIFile(t, filepath.Join(notes, "sub", "b.md"), "second note\n")
				RunCLIOK(t, "real", env, nil, "commit", notes, "-m", "two notes")
				if edit {
					HostedLocalEdit(t, notes)
				}
				before := FSSnapshotDir(t, notes)

				var cut atomic.Bool
				g.BeforeNextObject(http.MethodGet, HostedPrefix+"/current", func() {
					row.Cut(g)
					cut.Store(true)
				})
				code, stdout, stderr := RunCLI(t, "real", env, "pull", notes)
				if !cut.Load() {
					t.Fatalf("the pull never read current; exit %d, stdout %q, stderr %s", code, stdout, stderr)
				}
				if code != 1 {
					t.Fatalf("pull of an unreachable space = exit %d, want 1; stdout: %q stderr: %s", code, stdout, stderr)
				}
				for _, want := range []string{
					"STORAGE_FAILURE · ACCESS_DENIED",
					"SLIVINGDOC_TOKEN",
					"retryable: false",
					"next: operator attention needed",
				} {
					if !strings.Contains(stdout, want) {
						t.Fatalf("unreachable-space report %q does not contain %q", stdout, want)
					}
				}
				if strings.Contains(stdout+stderr, HostedToken) {
					t.Fatal("the unreachable-space report leaks the token")
				}
				AssertTreeKept(t, notes, before)
			})
		}
	}
}

// TestScenarioHostedSpaceGoneEntryRecovery proves entry recovery never
// resynchronizes the notebook directory to an empty notebook when the space
// became unreachable. A pull whose replacement fails while the grant moves
// away is RECOVERY_FAILURE/ACCESS_DENIED, not resynchronized; the next pull
// runs entry recovery and is refused the same way at stage entry; L stays
// byte-identical throughout. Once the grant is back, entry recovery
// resynchronizes to the accepted state. The in-process harness runs the
// real hosted adapter here because the workspace failpoints are reachable
// only through it.
func TestScenarioHostedSpaceGoneEntryRecovery(t *testing.T) {
	t.Parallel()
	g := gatewaytest.Start(t)
	g.AddSpace(HostedSpace, 1<<20)
	g.Grant(HostedToken, HostedSpace, false)
	store, err := httpstore.New(httpstore.Config{
		Endpoint: g.URL(), Space: HostedSpace, Prefix: HostedPrefix, Token: HostedToken, Retries: -1,
	})
	if err != nil {
		t.Fatalf("httpstore.New() = %v", err)
	}
	h := NewHarness(t, HarnessConfig{
		Store: store, Prefix: HostedPrefix, Bucket: HostedSpace, Endpoint: g.URL(),
		Hooks: &app.ServiceHooks{Workspace: &workspace.Failpoints{}, Notebook: &notebook.Failpoints{}},
	})
	other := NewSharedHarness(t, store, HostedPrefix, HarnessConfig{Bucket: HostedSpace, Endpoint: g.URL()})
	path := h.WorkspaceRoot()
	CommitFirst(t, h, "a.md", "kept\n", "first")
	otherPath := other.WorkspaceRoot()
	other.AssertOK(t, other.Pull(""))
	other.WriteFile(otherPath+"/b.md", "remote\n")
	other.AssertOK(t, other.Commit("", "second"))
	before := FSSnapshotDir(t, path)

	var fired atomic.Bool
	h.WorkspaceFailpoints().Replace = func() error {
		if !fired.CompareAndSwap(false, true) {
			return nil
		}
		MoveHostedGrant(g)
		return errors.New("injected replacement failure")
	}
	denied := func(stage, accepted string) CallExpectation {
		return CallExpectation{
			ErrorCode: CodeRecoveryFailure, Reason: "ACCESS_DENIED", Action: "OPERATOR", Retryable: new(false),
			Recovery: &RecoveryExpectation{Stage: stage, RemoteAccepted: accepted, Resynchronized: new(false)},
			NoText:   []string{HostedToken},
		}
	}
	h.AssertEnvelope(t, ToolCall{Tool: ToolPull, Path: path, Expect: denied("pull.accept", "no")}, h.Pull(""))
	AssertTreeKept(t, path, before)
	if !h.StateRecord(t).RecoveryRequired {
		t.Fatal("the failed replacement must leave P requiring recovery")
	}

	h.AssertEnvelope(t, ToolCall{Tool: ToolPull, Path: path, Expect: denied("entry", "unknown")}, h.Pull(""))
	AssertTreeKept(t, path, before)
	if !h.StateRecord(t).RecoveryRequired {
		t.Fatal("a refused entry recovery must keep P requiring recovery")
	}

	g.Grant(HostedToken, HostedSpace, false)
	h.AssertEnvelope(t, ToolCall{Tool: ToolPull, Path: path, Expect: CallExpectation{
		ErrorCode: CodeRecoveryFailure, Action: "PULL",
		Recovery: &RecoveryExpectation{Stage: "entry", RemoteAccepted: "unknown", Resynchronized: new(true)},
	}}, h.Pull(""))
	AssertVisibleFiles(t, h, map[string]string{"a.md": "kept\n", "b.md": "remote\n"})
	h.AssertOK(t, h.Pull(""))
}

// TestScenarioHostedSpaceSpellings proves --space and SLIVINGDOC_SPACE are
// the hosted names of --bucket and SLIVINGDOC_BUCKET: serve --space reaches
// the space, the variable works for pull, two spellings with different
// values are refused naming both, and a mismatch names the spelling used.
func TestScenarioHostedSpaceSpellings(t *testing.T) {
	t.Parallel()
	g, named, root := HostedEnv(t, 1<<20)
	g.AddSpace("other-notes", 1<<20)
	tokenOnly := WithoutBucket(named)

	h := SpawnHelper(t, "real", tokenOnly, "serve", "--space", HostedSpace)
	cs := h.ConnectClient(t)
	served := root
	AssertProcessCallOK(t, cs, ToolPull, served, "")
	WriteCLIFile(t, filepath.Join(served, "a.md"), "through --space\n")
	AssertProcessCallOK(t, cs, ToolCommit, served, "serve --space")
	h.CloseClient(t, cs)
	if stderr := h.StderrText(t); !strings.Contains(stderr, "from=--space") {
		t.Fatalf("serve stderr = %s, want the space logged as from --space", stderr)
	}

	viaEnv := filepath.Join(root, "env")
	RunCLIOK(t, "real", append(append([]string(nil), tokenOnly...), "SLIVINGDOC_SPACE="+HostedSpace), nil, "pull", viaEnv)
	if got, err := os.ReadFile(filepath.Join(viaEnv, "a.md")); err != nil || string(got) != "through --space\n" {
		t.Fatalf("SLIVINGDOC_SPACE workspace a.md = %q, %v; want the file served through --space", got, err)
	}

	for _, row := range []struct {
		name  string
		extra []string
		args  []string
		want  string
	}{
		{"two flags", nil, []string{"--bucket", HostedSpace, "--space", "other-notes"}, `--bucket "team-notes" and --space "other-notes" name different spaces`},
		{"two variables", []string{"SLIVINGDOC_BUCKET=" + HostedSpace, "SLIVINGDOC_SPACE=other-notes"}, nil, `SLIVINGDOC_BUCKET "team-notes" and SLIVINGDOC_SPACE "other-notes" name different spaces`},
		{"a --space the token does not reach", nil, []string{"--space", "other-notes"}, `not "other-notes" from --space; drop --space to use the token's space`},
		{"a SLIVINGDOC_SPACE the token does not reach", []string{"SLIVINGDOC_SPACE=other-notes"}, nil, `not "other-notes" from SLIVINGDOC_SPACE; unset SLIVINGDOC_SPACE`},
	} {
		env := append(append([]string(nil), tokenOnly...), row.extra...)
		args := append([]string{"pull"}, row.args...)
		args = append(args, filepath.Join(root, "refused"))
		code, stdout, stderr := RunCLI(t, "real", env, args...)
		if code != 1 || strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, row.want) {
			t.Fatalf("%s = exit %d, stdout %q, stderr %s; want a refusal containing %q", row.name, code, stdout, stderr, row.want)
		}
	}
}
