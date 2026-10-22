//go:build windows || plan9

package itest

import (
	"errors"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// exerciseOptionalPathSecurity is unavailable where the native fixture
// primitives used by the Unix scenario do not exist. The stdio transport
// contract still runs there through TestScenarioTransportStdioProcess.
func ExerciseOptionalPathSecurity(t *testing.T, h *HelperProc, cs *sdk.ClientSession) {
	t.Helper()
}

// mkfifo is never reached: the credentials file scenarios skip where there
// are no FIFOs.
func Mkfifo(string) error { return errors.New("this platform has no FIFOs") }
