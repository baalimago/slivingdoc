package integrationtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/httpstore/gatewaytest"
)

const (
	hostedSpace  = "team-notes"
	hostedToken  = "sld_0123456789abcdef_aG9zdGVkLXRva2VuLWZvci1pbnRlZ3JhdGlvbi10ZXN0cw"
	hostedReader = "sld_fedcba9876543210_cmVhZC1vbmx5LXRva2VuLWZvci1pbnRlZ3JhdGlvbi10ZQ"
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
// nothing; pulls keep working; and the same commit succeeds once the space
// has room again.
func TestScenarioHostedStorageFull(t *testing.T) {
	t.Parallel()
	g, env, root := hostedEnv(t, 1<<20)
	notes := filepath.Join(root, "notes")
	runCLIOK(t, "real", env, nil, "pull", notes)
	writeCLIFile(t, filepath.Join(notes, "a.md"), "first\n")
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
		"storage space is full",
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

	g.SetQuota(hostedSpace, 1<<20)
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
