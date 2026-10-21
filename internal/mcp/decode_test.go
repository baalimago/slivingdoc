package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodePull(t *testing.T) {
	tests := []struct {
		name    string
		args    any
		wantErr string
	}{
		{name: "empty object", args: map[string]any{}},
		{name: "unknown field", args: map[string]any{"extra": 1}, wantErr: "unknown field"},
		{name: "path of any type is refused", args: map[string]any{"path": "/a"}, wantErr: "--workspace-root"},
		{name: "null path is refused", args: map[string]any{"path": nil}, wantErr: "--workspace-root"},
		{name: "number path is refused", args: map[string]any{"path": 5}, wantErr: "--workspace-root"},
		{name: "duplicate path is refused", args: `{"path":"/a","path":"/b"}`, wantErr: "--workspace-root"},
		{name: "path before an unknown field", args: map[string]any{"path": "/a", "extra": 1}, wantErr: "--workspace-root"},
		{name: "duplicate unknown field", args: `{"extra":1,"extra":2}`, wantErr: "duplicate field"},
		{name: "non-object arguments", args: []any{"/a"}, wantErr: "must be a JSON object"},
		{name: "string arguments", args: `"notes"`, wantErr: "must be a JSON object"},
		{name: "null arguments are the empty object", args: nil},
		{name: "absent arguments are the empty object", args: ""},
		{name: "malformed JSON", args: `{"path":`, wantErr: "not a strict JSON object"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := decodePull(marshalArgs(t, tt.args))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("decodePull() = %v; want error containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodePull() = %v", err)
			}
		})
	}
}

func TestDecodeCommit(t *testing.T) {
	tests := []struct {
		name        string
		args        any
		wantMessage string
		wantErr     string
	}{
		{name: "valid", args: map[string]any{"message": "update"}, wantMessage: "update"},
		{name: "missing message", args: map[string]any{}, wantErr: "message is required"},
		{name: "unknown field", args: map[string]any{"message": "m", "extra": 1}, wantErr: "unknown field"},
		{name: "null message", args: map[string]any{"message": nil}, wantErr: "explicit null"},
		{name: "message not a string", args: map[string]any{"message": true}, wantErr: "message must be a string"},
		{name: "blank message", args: map[string]any{"message": "   "}, wantErr: "must not be blank"},
		{name: "unicode whitespace message", args: map[string]any{"message": "\u00a0\u2003"}, wantErr: "must not be blank"},
		{name: "message with NUL", args: map[string]any{"message": "a\x00b"}, wantErr: "U+0000"},
		{name: "message with escapes preserved", args: map[string]any{"message": "line1\nline2"}, wantMessage: "line1\nline2"},
		{name: "path is refused before the message", args: map[string]any{"path": "/a", "message": "m"}, wantErr: "--workspace-root"},
		{name: "null arguments lack the message", args: nil, wantErr: "message is required"},
		{name: "absent arguments lack the message", args: "", wantErr: "message is required"},
		{name: "non-object arguments", args: `[]`, wantErr: "must be a JSON object"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message, err := decodeCommit(marshalArgs(t, tt.args))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("decodeCommit() = %q, %v; want error containing %q", message, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeCommit() = %v", err)
			}
			if message != tt.wantMessage {
				t.Fatalf("decodeCommit() = %q, want %q", message, tt.wantMessage)
			}
		})
	}
}

func TestDecodeCommitOversizedMessage(t *testing.T) {
	message := strings.Repeat("x", maxMessageBytes+1)
	if _, err := decodeCommit(marshalArgs(t, map[string]any{"message": message})); err == nil || !strings.Contains(err.Error(), "16384") {
		t.Fatalf("decodeCommit() error = %v, want the 16384-byte bound", err)
	}
	message = strings.Repeat("x", maxMessageBytes)
	if _, err := decodeCommit(marshalArgs(t, map[string]any{"message": message})); err != nil {
		t.Fatalf("decodeCommit() at the bound = %v", err)
	}
}

// marshalArgs renders the test argument value as the raw JSON the SDK
// delivers in CallToolParamsRaw.Arguments. A string renders as raw JSON,
// so tests can express malformed and duplicate-field documents.
func marshalArgs(t *testing.T, args any) json.RawMessage {
	t.Helper()
	switch v := args.(type) {
	case string:
		return json.RawMessage(v)
	case nil:
		return json.RawMessage("null")
	default:
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal test args: %v", err)
		}
		return data
	}
}
