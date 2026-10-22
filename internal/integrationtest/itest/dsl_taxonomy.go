package itest

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// gitObjectID matches a bare 40-hex Git object ID. Git history is internal
// (architecture/git-engine.md), so no object ID may reach a caller.
var GitObjectID = regexp.MustCompile(`\b[0-9a-f]{40}\b`)

// gitVocabulary is Git terminology that may never reach a caller. Recovery
// instructions are expressed in ordinary file terms: a caller resolves
// conflicts with a text editor and never runs Git. Each term is
// unambiguous — none of them occurs in ordinary slivingdoc prose.
var GitVocabulary = []string{"git ", "git:", "rebase", "merge-base", "refs/", "packfile", "checkout", "blob", "tree ", "shallow"}

// assertTaxonomyRedaction proves that neither the caller-facing envelope nor
// the harness's own log records leak an S3 key, a private path, or a Git
// object ID, and that no caller-facing text speaks Git
// (architecture/product-contract.md).
//
// Credential redaction is proven where a credential actually exists: the
// in-process harness constructs its own store, so no secret ever reaches
// this configuration. TestScenarioConfigInvalidAndEarlyExit drives a real
// credential through the process startup path instead.
func AssertTaxonomyRedaction(t *testing.T, h *Harness, res *sdk.CallToolResult) {
	t.Helper()
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal tool result: %v", err)
	}
	logs := h.Logs().String()
	for _, forbidden := range []string{h.PrivateRoot(), "packs/"} {
		if forbidden == "" {
			t.Fatal("redaction fixture is empty; the scan would be vacuous")
		}
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("tool result leaks %q: %s", forbidden, data)
		}
		if strings.Contains(logs, forbidden) {
			t.Fatalf("log capture leaks %q: %s", forbidden, logs)
		}
	}
	if id := GitObjectID.FindString(string(data)); id != "" {
		t.Fatalf("tool result leaks the Git object ID %q", id)
	}
	if id := GitObjectID.FindString(logs); id != "" {
		t.Fatalf("log capture leaks the Git object ID %q", id)
	}
	lower := strings.ToLower(string(data))
	for _, word := range GitVocabulary {
		if strings.Contains(lower, word) {
			t.Fatalf("caller-facing text uses the Git term %q: %s", word, data)
		}
	}
}
