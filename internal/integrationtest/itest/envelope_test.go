package itest

import (
	"strings"
	"testing"
)

// TestDecodeEnvelopeRequiresTokens: the harness decoder refuses an envelope
// missing reason, action, or a per-file reason. EnvelopeTokenViolation is
// called directly because a t.Fatalf inside a subtest would fail the parent.
func TestDecodeEnvelopeRequiresTokens(t *testing.T) {
	call := ToolCall{Tool: ToolCommit, Path: "/tmp/notebook"}
	complete := map[string]any{
		"code": "INVALID_REQUEST", "reason": "MESSAGE_BLANK", "action": "FIX_INPUT",
		"diagnosticId": "0011223344556677",
		"retryable":    false, "message": "commit message must not be blank",
		"files": []any{}, "readOnly": []any{}, "writable": []any{},
	}
	res := TokenEnvelopeResult(complete)
	env := DecodeEnvelope(t, call, res)
	if env.Reason != "MESSAGE_BLANK" || env.Action != "FIX_INPUT" {
		t.Fatalf("env = %+v, want the reason and action tokens preserved", env)
	}
	if v := EnvelopeTokenViolation(env); v != "" {
		t.Fatalf("EnvelopeTokenViolation(complete) = %q, want no violation", v)
	}

	tests := []struct {
		name    string
		mutate  func(Envelope) Envelope
		wantSub string
	}{
		{"missing message", func(e Envelope) Envelope { e.Message = ""; return e }, "empty message"},
		{"missing reason", func(e Envelope) Envelope { e.Reason = ""; return e }, "empty reason"},
		{"missing action", func(e Envelope) Envelope { e.Action = ""; return e }, "empty action"},
		{"missing diagnostic id", func(e Envelope) Envelope { e.DiagnosticID = ""; return e }, "empty diagnosticId"},
		{"nil files", func(e Envelope) Envelope { e.Files = nil; return e }, "no files key"},
		{"nil readOnly", func(e Envelope) Envelope { e.ReadOnly = nil; return e }, "no readOnly key"},
		{"nil writable", func(e Envelope) Envelope { e.Writable = nil; return e }, "no writable key"},
		{"file with no reason", func(e Envelope) Envelope {
			e.Files = []EnvelopeFile{{Path: "notes/a.md"}}
			return e
		}, "empty reason"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bad := tt.mutate(env)
			v := EnvelopeTokenViolation(bad)
			if v == "" || !strings.Contains(v, tt.wantSub) {
				t.Fatalf("EnvelopeTokenViolation(%+v) = %q, want it to mention %q", bad, v, tt.wantSub)
			}
		})
	}
}
