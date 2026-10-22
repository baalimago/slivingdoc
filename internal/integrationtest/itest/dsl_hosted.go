package itest

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/httpstore/gatewaytest"
)

const (
	HostedSpace  = "team-notes"
	HostedToken  = "sld_0123456789abcdef_aG9zdGVkLXRva2VuLWZvci1pbnRlZ3JhdGlvbi10ZXN0cw"
	HostedReader = "sld_fedcba9876543210_cmVhZC1vbmx5LXRva2VuLWZvci1pbnRlZ3JhdGlvbi10ZQ"
	// hostedPrefix is the notebook prefix SpawnHelper gives every helper.
	HostedPrefix = "integration-prefix"
)

// hostedEnv starts a reference storage API with one space and returns the
// environment of one-shot CLI processes that use it through a token, plus
// the workspace root shared by those processes.
func HostedEnv(t *testing.T, quota int64) (*gatewaytest.Gateway, []string, string) {
	t.Helper()
	g := gatewaytest.Start(t)
	g.AddSpace(HostedSpace, quota)
	g.Grant(HostedToken, HostedSpace, false)
	g.Grant(HostedReader, HostedSpace, true)
	env, root := CliRoots(t)
	env = append(
		env,
		"SLIVINGDOC_TOKEN="+HostedToken,
		"SLIVINGDOC_ENDPOINT="+g.URL(),
		"SLIVINGDOC_BUCKET="+HostedSpace,
	)
	return g, env, root
}

// withoutBucket empties SLIVINGDOC_BUCKET, which the helper otherwise
// inherits, leaving the token as the whole hosted configuration.
func WithoutBucket(env []string) []string {
	var out []string
	for _, kv := range env {
		if !strings.HasPrefix(kv, "SLIVINGDOC_BUCKET=") {
			out = append(out, kv)
		}
	}
	return append(out, "SLIVINGDOC_BUCKET=")
}

// hostedCuts make the space unreachable for hostedToken while a notebook
// is in use, the two ways the gateway then answers 404 no_space: the space
// is deleted, or the token's grant moves to another space.
var HostedCuts = []struct {
	Name string
	Cut  func(g *gatewaytest.Gateway)
}{
	{"space deleted", func(g *gatewaytest.Gateway) { g.DeleteSpace(HostedSpace) }},
	{"grant moved", MoveHostedGrant},
}

func MoveHostedGrant(g *gatewaytest.Gateway) {
	g.AddSpace("elsewhere", 1<<20)
	g.Grant(HostedToken, "elsewhere", false)
}

// hostedLocalEdit edits a published note and adds an unpublished one, so a
// refused pull has local work to lose as well as published notes.
func HostedLocalEdit(t *testing.T, notes string) {
	t.Helper()
	WriteCLIFile(t, filepath.Join(notes, "a.md"), "first note, edited locally\n")
	WriteCLIFile(t, filepath.Join(notes, "draft.md"), "not yet committed\n")
}

// assertTreeKept fails when the notebook directory differs from before in
// any file or byte.
func AssertTreeKept(t *testing.T, notes string, before map[string]string) {
	t.Helper()
	if after := FSSnapshotDir(t, notes); !maps.Equal(before, after) {
		t.Fatalf("notebook directory after the refusal = %v, want it byte-identical to %v", after, before)
	}
}

// incompressible returns about n bytes of hex text that compression cannot
// shrink, so pack sizes follow the notes rather than commit metadata.
func Incompressible(n int) string {
	var b strings.Builder
	sum := sha256.Sum256([]byte("slivingdoc"))
	for b.Len() < n {
		sum = sha256.Sum256(sum[:])
		b.WriteString(hex.EncodeToString(sum[:]))
		b.WriteByte('\n')
	}
	return b.String()
}
