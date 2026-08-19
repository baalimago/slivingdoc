//go:build windows || plan9

package integrationtest

import (
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// exerciseOptionalPathSecurity is unavailable where the native fixture
// primitives used by the Unix scenario do not exist. The stdio transport
// contract still runs there through TestScenarioTransportStdioProcess.
func exerciseOptionalPathSecurity(t *testing.T, h *helperProc, cs *sdk.ClientSession) {
	t.Helper()
}
