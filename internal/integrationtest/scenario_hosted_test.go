package integrationtest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/httpstore"
	"github.com/baalimago/slivingdoc/internal/httpstore/gatewaytest"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

const (
	hostedSpace  = "team-notes"
	hostedToken  = "sld_0123456789abcdef_aG9zdGVkLXRva2VuLWZvci1pbnRlZ3JhdGlvbi10ZXN0cw"
	hostedReader = "sld_fedcba9876543210_cmVhZC1vbmx5LXRva2VuLWZvci1pbnRlZ3JhdGlvbi10ZQ"
	// hostedPrefix is the notebook prefix spawnHelper gives every helper.
	hostedPrefix = "integration-prefix"
)

// hostedEnv starts a reference storage API with one space and returns the
// environment of one-shot CLI processes that use it through a token, plus
// the workspace root shared by those processes.
func hostedEnv(t *testing.T, quota int64) (*gatewaytest.Gateway, []string, string) {
	t.Helper()
	g := gatewaytest.Start(t)
	g.AddSpace(hostedSpace, quota)
	g.Grant(hostedToken, hostedSpace, false)
	g.Grant(hostedReader, hostedSpace, true)
	env, root := cliRoots(t)
	env = append(env,
		"SLIVINGDOC_TOKEN="+hostedToken,
		"SLIVINGDOC_ENDPOINT="+g.URL(),
		"SLIVINGDOC_BUCKET="+hostedSpace,
	)
	return g, env, root
}

// TestScenarioHostedRoundTrip proves the hosted workflow end to end: a
// token and a space name are the whole configuration, a one-shot pull and
// commit publish through the storage API, and a second workspace pulls the
// published file back from the space.
func TestScenarioHostedRoundTrip(t *testing.T) {
	t.Parallel()
	g, env, root := hostedEnv(t, 1<<20)
	notes := filepath.Join(root, "notes")

	runCLIExact(t, "real", env,
		"OK  generation 0  "+notes+"\n0 files changed, 0 insertions(+), 0 deletions(-)\n",
		"pull", notes)
	writeCLIFile(t, filepath.Join(notes, "a.md"), "hosted notes\n")
	runCLIExact(t, "real", env,
		"OK  generation 1  "+notes+"\n  a.md  +1\n1 files changed, 1 insertions(+), 0 deletions(-)\n",
		"commit", notes, "-m", "hosted commit")
	if g.Stored(hostedSpace) == 0 {
		t.Fatal("the space holds no pack bytes after a published commit")
	}

	other := filepath.Join(root, "other")
	runCLIOK(t, "real", env, nil, "pull", other)
	got, err := os.ReadFile(filepath.Join(other, "a.md"))
	if err != nil || string(got) != "hosted notes\n" {
		t.Fatalf("second workspace a.md = %q, %v; want the published file", got, err)
	}
}

// TestScenarioHostedStorageFull proves the over-quota contract: a commit
// that would take the space past its quota fails with STORAGE_FULL, says
// how to fix it, is not retryable, keeps the visible edit, and publishes
// nothing; pulls keep working; deleting notes makes room; a used-up request
// allowance is REQUEST_LIMIT; and a commit succeeds once there is room.
func TestScenarioHostedStorageFull(t *testing.T) {
	t.Parallel()
	g, env, root := hostedEnv(t, 1<<20)
	notes := filepath.Join(root, "notes")
	runCLIOK(t, "real", env, nil, "pull", notes)
	writeCLIFile(t, filepath.Join(notes, "a.md"), incompressible(4096))
	runCLIOK(t, "real", env, nil, "commit", notes, "-m", "fits")

	stored := g.Stored(hostedSpace)
	g.SetQuota(hostedSpace, stored)
	writeCLIFile(t, filepath.Join(notes, "b.md"), "does not fit\n")
	code, stdout, stderr := runCLI(t, "real", env, "commit", notes, "-m", "too much")
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
	if strings.Contains(stdout+stderr, hostedToken) {
		t.Fatal("the over-quota report leaks the token")
	}
	if got := g.Stored(hostedSpace); got != stored {
		t.Fatalf("stored bytes after the refused commit = %d, want %d", got, stored)
	}
	if got, err := os.ReadFile(filepath.Join(notes, "b.md")); err != nil || string(got) != "does not fit\n" {
		t.Fatalf("visible edit after the refused commit = %q, %v; want it kept", got, err)
	}

	runCLIOK(t, "real", env, nil, "pull", filepath.Join(root, "reader"))

	// Deleting notes makes room: the refused increment is published as a
	// checkpoint of the smaller state, and the old packs are removed.
	if err := os.Remove(filepath.Join(notes, "b.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(notes, "a.md")); err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, filepath.Join(notes, "small.md"), "s\n")
	runCLIOK(t, "real", env, nil, "commit", notes, "-m", "delete to make room")
	if got := g.Stored(hostedSpace); got >= stored {
		t.Fatalf("stored bytes after deleting notes = %d, want less than %d", got, stored)
	}
	runCLIOK(t, "real", env, nil, "pull", filepath.Join(root, "reader"))

	g.SetQuota(hostedSpace, 1<<20)
	writeCLIFile(t, filepath.Join(notes, "c.md"), "room again\n")
	g.RefuseNextWithReason(http.MethodPut, http.StatusInsufficientStorage, "quota_exceeded", "request_limit")
	code, stdout, stderr = runCLI(t, "real", env, "commit", notes, "-m", "allowance used")
	if code != 1 || !strings.Contains(stdout, "STORAGE_FAILURE · REQUEST_LIMIT") || !strings.Contains(stdout, "first of the month") {
		t.Fatalf("commit past the request allowance = exit %d, stdout %q, stderr %s; want REQUEST_LIMIT", code, stdout, stderr)
	}

	runCLIOK(t, "real", env, nil, "commit", notes, "-m", "room again")
}

// TestScenarioHostedReadOnlyToken proves a read-only token pulls the space
// but its commit is refused as ACCESS_DENIED, not retryable, with nothing
// published.
func TestScenarioHostedReadOnlyToken(t *testing.T) {
	t.Parallel()
	g, env, root := hostedEnv(t, 1<<20)
	writer := filepath.Join(root, "writer")
	runCLIOK(t, "real", env, nil, "pull", writer)
	writeCLIFile(t, filepath.Join(writer, "a.md"), "from the writer\n")
	runCLIOK(t, "real", env, nil, "commit", writer, "-m", "writer")
	stored := g.Stored(hostedSpace)

	readerEnv := append(append([]string(nil), env...), "SLIVINGDOC_TOKEN="+hostedReader)
	reader := filepath.Join(root, "reader")
	runCLIOK(t, "real", readerEnv, nil, "pull", reader)
	writeCLIFile(t, filepath.Join(reader, "b.md"), "from the reader\n")
	code, stdout, stderr := runCLI(t, "real", readerEnv, "commit", reader, "-m", "reader")
	if code != 1 {
		t.Fatalf("read-only commit = exit %d, want 1; stdout: %q stderr: %s", code, stdout, stderr)
	}
	for _, want := range []string{"STORAGE_FAILURE · ACCESS_DENIED", "SLIVINGDOC_TOKEN", "retryable: false"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("read-only report %q does not contain %q", stdout, want)
		}
	}
	if g.Stored(hostedSpace) != stored {
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
	g.AddSpace(hostedSpace, 1<<20)
	g.AddSpace("elsewhere", 1<<20)
	g.Grant(hostedToken, hostedSpace, false)
	for _, row := range []struct {
		name string
		env  []string
		want string
	}{
		{
			name: "unknown token",
			env:  []string{"SLIVINGDOC_TOKEN=sld_unknown_token", "SLIVINGDOC_BUCKET=" + hostedSpace},
			want: "refused the token",
		},
		{
			name: "space not granted",
			env:  []string{"SLIVINGDOC_TOKEN=" + hostedToken, "SLIVINGDOC_BUCKET=elsewhere"},
			want: "refused the token",
		},
		{
			name: "invalid space name",
			env:  []string{"SLIVINGDOC_TOKEN=" + hostedToken, "SLIVINGDOC_BUCKET=Team_Notes"},
			want: "invalid space name",
		},
		{
			name: "plain http remote endpoint",
			env: []string{
				"SLIVINGDOC_TOKEN=" + hostedToken, "SLIVINGDOC_BUCKET=" + hostedSpace,
				"SLIVINGDOC_ENDPOINT=http://api.example.test",
			},
			want: "must use https",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			env, root := cliRoots(t)
			env = append(env, "SLIVINGDOC_ENDPOINT="+g.URL())
			env = append(env, row.env...)
			code, stdout, stderr := runCLI(t, "real", env, "pull", filepath.Join(root, "notes"))
			if code == 0 {
				t.Fatalf("pull = exit 0, want a startup refusal; stdout: %q", stdout)
			}
			if strings.TrimSpace(stdout) != "" {
				t.Fatalf("startup refusal wrote stdout: %q", stdout)
			}
			if !strings.Contains(stderr, row.want) {
				t.Fatalf("startup refusal stderr = %q, want it to contain %q", stderr, row.want)
			}
			if strings.Contains(stderr, hostedToken) || strings.Contains(stderr, "sld_unknown_token") {
				t.Fatalf("startup refusal stderr = %q leaks the token", stderr)
			}
		})
	}
}

// hostedCuts make the space unreachable for hostedToken while a notebook
// is in use, the two ways the gateway then answers 404 no_space: the space
// is deleted, or the token's grant moves to another space.
var hostedCuts = []struct {
	name string
	cut  func(g *gatewaytest.Gateway)
}{
	{"space deleted", func(g *gatewaytest.Gateway) { g.DeleteSpace(hostedSpace) }},
	{"grant moved", func(g *gatewaytest.Gateway) {
		g.AddSpace("elsewhere", 1<<20)
		g.Grant(hostedToken, "elsewhere", false)
	}},
}

// hostedLocalEdit edits a published note and adds an unpublished one, so a
// refused pull has local work to lose as well as published notes.
func hostedLocalEdit(t *testing.T, notes string) {
	t.Helper()
	writeCLIFile(t, filepath.Join(notes, "a.md"), "first note, edited locally\n")
	writeCLIFile(t, filepath.Join(notes, "draft.md"), "not yet committed\n")
}

// assertTreeKept fails when the notebook directory differs from before in
// any file or byte.
func assertTreeKept(t *testing.T, notes string, before map[string]string) {
	t.Helper()
	if after := fsSnapshot(t, notes); !maps.Equal(before, after) {
		t.Fatalf("notebook directory after the refusal = %v, want it byte-identical to %v", after, before)
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
	for _, row := range hostedCuts {
		for _, edit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/local edit %v", row.name, edit), func(t *testing.T) {
				t.Parallel()
				g, env, root := hostedEnv(t, 1<<20)
				h := spawnHelper(t, "real", env, "serve")
				cs := h.connectClient(t)
				notes := filepath.Join(root, "notes")

				assertProcessCallOK(t, cs, toolPull, notes, "")
				writeCLIFile(t, filepath.Join(notes, "a.md"), "first note\n")
				writeCLIFile(t, filepath.Join(notes, "sub", "b.md"), "second note\n")
				assertProcessCallOK(t, cs, toolCommit, notes, "two notes")
				if got := assertProcessCallOK(t, cs, toolPull, notes, ""); got.Generation != 1 {
					t.Fatalf("pull after the commit = generation %d, want 1", got.Generation)
				}
				if edit {
					hostedLocalEdit(t, notes)
				}
				before := fsSnapshot(t, notes)

				row.cut(g)
				for _, tool := range []string{toolPull, toolCommit} {
					got := processError(t, cs, tool, notes, "after the space is gone")
					if got.Code != "STORAGE_FAILURE" || got.Reason != "ACCESS_DENIED" || got.Action != "OPERATOR" || got.Retryable {
						t.Fatalf("%s on an unreachable space = %s/%s/%s retryable %v, want STORAGE_FAILURE/ACCESS_DENIED/OPERATOR not retryable; message: %s",
							tool, got.Code, got.Reason, got.Action, got.Retryable, got.Message)
					}
					if strings.Contains(got.Message, hostedToken) {
						t.Fatalf("%s refusal leaks the token: %s", tool, got.Message)
					}
					assertTreeKept(t, notes, before)
				}

				if err := cs.Close(); err != nil {
					t.Fatalf("close MCP client: %v", err)
				}
				if code := h.waitExit(t); code != 0 {
					t.Fatalf("serve exit = %d, want 0; stderr: %s", code, h.stderrText(t))
				}
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
	for _, row := range hostedCuts {
		for _, edit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/local edit %v", row.name, edit), func(t *testing.T) {
				t.Parallel()
				g, env, root := hostedEnv(t, 1<<20)
				env = append(env, "SLIVINGDOC_PREFIX="+hostedPrefix)
				notes := filepath.Join(root, "notes")
				runCLIOK(t, "real", env, nil, "pull", notes)
				writeCLIFile(t, filepath.Join(notes, "a.md"), "first note\n")
				writeCLIFile(t, filepath.Join(notes, "sub", "b.md"), "second note\n")
				runCLIOK(t, "real", env, nil, "commit", notes, "-m", "two notes")
				runCLIOK(t, "real", env, nil, "pull", notes)
				if edit {
					hostedLocalEdit(t, notes)
				}
				before := fsSnapshot(t, notes)

				var cut atomic.Bool
				g.BeforeNextObject(http.MethodGet, hostedPrefix+"/current", func() {
					row.cut(g)
					cut.Store(true)
				})
				code, stdout, stderr := runCLI(t, "real", env, "pull", notes)
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
				if strings.Contains(stdout+stderr, hostedToken) {
					t.Fatal("the unreachable-space report leaks the token")
				}
				assertTreeKept(t, notes, before)
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
	g.AddSpace(hostedSpace, 1<<20)
	g.Grant(hostedToken, hostedSpace, false)
	store, err := httpstore.New(httpstore.Config{
		Endpoint: g.URL(), Space: hostedSpace, Prefix: hostedPrefix, Token: hostedToken, Retries: -1,
	})
	if err != nil {
		t.Fatalf("httpstore.New() = %v", err)
	}
	h := NewHarness(t, HarnessConfig{
		Store: store, Prefix: hostedPrefix, Bucket: hostedSpace, Endpoint: g.URL(),
		Hooks: &app.ServiceHooks{Workspace: &workspace.Failpoints{}, Notebook: &notebook.Failpoints{}},
	})
	other := newSharedHarness(t, store, hostedPrefix, HarnessConfig{Bucket: hostedSpace, Endpoint: g.URL()})
	path := h.Path("notes")
	commitFirst(t, h, path, "a.md", "kept\n", "first")
	otherPath := other.Path("notes")
	other.assertOK(t, other.Pull("", otherPath))
	other.WriteFile(otherPath+"/b.md", "remote\n")
	other.assertOK(t, other.Commit("", otherPath, "second"))
	before := fsSnapshot(t, path)

	var fired atomic.Bool
	h.WorkspaceFailpoints().Replace = func() error {
		if !fired.CompareAndSwap(false, true) {
			return nil
		}
		hostedCuts[1].cut(g)
		return errors.New("injected replacement failure")
	}
	denied := func(stage, accepted string) CallExpectation {
		return CallExpectation{
			ErrorCode: codeRecoveryFailure, Reason: "ACCESS_DENIED", Action: "OPERATOR", Retryable: new(false),
			Recovery: &RecoveryExpectation{Stage: stage, RemoteAccepted: accepted, Resynchronized: new(false)},
			NoText:   []string{hostedToken},
		}
	}
	h.assertEnvelope(t, ToolCall{Tool: toolPull, Path: path, Expect: denied("pull.accept", "no")}, h.Pull("", path))
	assertTreeKept(t, path, before)
	if !h.StateRecord(t, path).RecoveryRequired {
		t.Fatal("the failed replacement must leave P requiring recovery")
	}

	h.assertEnvelope(t, ToolCall{Tool: toolPull, Path: path, Expect: denied("entry", "unknown")}, h.Pull("", path))
	assertTreeKept(t, path, before)
	if !h.StateRecord(t, path).RecoveryRequired {
		t.Fatal("a refused entry recovery must keep P requiring recovery")
	}

	g.Grant(hostedToken, hostedSpace, false)
	h.assertEnvelope(t, ToolCall{Tool: toolPull, Path: path, Expect: CallExpectation{
		ErrorCode: codeRecoveryFailure, Action: "PULL",
		Recovery: &RecoveryExpectation{Stage: "entry", RemoteAccepted: "unknown", Resynchronized: new(true)},
	}}, h.Pull("", path))
	assertVisibleFiles(t, h, path, map[string]string{"a.md": "kept\n", "b.md": "remote\n"})
	h.assertOK(t, h.Pull("", path))
}

// incompressible returns about n bytes of hex text that compression cannot
// shrink, so pack sizes follow the notes rather than commit metadata.
func incompressible(n int) string {
	var b strings.Builder
	sum := sha256.Sum256([]byte("slivingdoc"))
	for b.Len() < n {
		sum = sha256.Sum256(sum[:])
		b.WriteString(hex.EncodeToString(sum[:]))
		b.WriteByte('\n')
	}
	return b.String()
}
