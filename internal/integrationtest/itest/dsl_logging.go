package itest

import (
	"strings"
	"testing"
)

// logLines returns the helper's stderr split into records, dropping the
// container noise a shared process may emit.
func LogLines(t *testing.T, h *HelperProc) []string {
	t.Helper()
	var out []string
	for line := range strings.SplitSeq(h.StderrText(t), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}
