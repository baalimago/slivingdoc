package mcp

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodePull(t *testing.T) {
	abs := "/tmp/notes"
	tests := []struct {
		name    string
		args    any
		want    string
		wantErr string
	}{
		{name: "valid", args: map[string]any{"path": abs}, want: abs},
		{name: "omitted path defers to the notebook root", args: map[string]any{}, want: ""},
		{name: "unknown field", args: map[string]any{"path": abs, "extra": 1}, wantErr: "unknown field"},
		{name: "null path", args: map[string]any{"path": nil}, wantErr: "explicit null"},
		{name: "duplicate field", args: `{"path":"/a","path":"/b"}`, wantErr: "duplicate field"},
		{name: "path not a string", args: map[string]any{"path": 5}, wantErr: "path must be a string"},
		{name: "relative path", args: map[string]any{"path": "notes"}, wantErr: "path must be absolute"},
		{name: "empty path", args: map[string]any{"path": ""}, wantErr: "path must not be empty"},
		{name: "path with NUL", args: map[string]any{"path": "/tmp/a\x00b"}, wantErr: "U+0000"},
		{name: "non-object arguments", args: []any{abs}, wantErr: "must be a JSON object"},
		{name: "string arguments", args: `"notes"`, wantErr: "must be a JSON object"},
		{name: "null arguments", args: nil, wantErr: "explicit null"},
		{name: "malformed JSON", args: `{"path":`, wantErr: "not a strict JSON object"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := marshalArgs(t, tt.args)
			got, err := decodePull(raw)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("decodePull() = %q, %v; want error containing %q", got, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodePull() = %v", err)
			}
			if got != tt.want {
				t.Fatalf("decodePull() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDecodePullExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := decodePull(marshalArgs(t, map[string]any{"path": "~/notes"}))
	if err != nil {
		t.Fatalf("decodePull() = %v", err)
	}
	if want := filepath.Join(home, "notes"); got != want {
		t.Fatalf("decodePull() = %q, want %q", got, want)
	}
}

func TestDecodePullOversizedPath(t *testing.T) {
	path := "/tmp/" + strings.Repeat("a", maxPathBytes) // > 4096 bytes
	_, err := decodePull(marshalArgs(t, map[string]any{"path": path}))
	if err == nil || !strings.Contains(err.Error(), "4096") {
		t.Fatalf("decodePull() error = %v, want the 4096-byte bound", err)
	}
	// The documented maximum is accepted.
	path = "/" + strings.Repeat("a", maxPathBytes-1)
	if _, err := decodePull(marshalArgs(t, map[string]any{"path": path})); err != nil {
		t.Fatalf("decodePull() at the bound = %v", err)
	}
}

func TestDecodeCommit(t *testing.T) {
	abs := "/tmp/notes"
	tests := []struct {
		name        string
		args        any
		wantPath    string
		wantMessage string
		wantErr     string
	}{
		{name: "valid", args: map[string]any{"path": abs, "message": "update"}, wantPath: abs, wantMessage: "update"},
		{name: "omitted path defers to the notebook root", args: map[string]any{"message": "m"}, wantPath: "", wantMessage: "m"},
		{name: "missing message", args: map[string]any{"path": abs}, wantErr: "message is required"},
		{name: "unknown field", args: map[string]any{"path": abs, "message": "m", "extra": 1}, wantErr: "unknown field"},
		{name: "null message", args: map[string]any{"path": abs, "message": nil}, wantErr: "explicit null"},
		{name: "message not a string", args: map[string]any{"path": abs, "message": true}, wantErr: "message must be a string"},
		{name: "blank message", args: map[string]any{"path": abs, "message": "   "}, wantErr: "must not be blank"},
		{name: "unicode whitespace message", args: map[string]any{"path": abs, "message": "\u00a0\u2003"}, wantErr: "must not be blank"},
		{name: "message with NUL", args: map[string]any{"path": abs, "message": "a\x00b"}, wantErr: "U+0000"},
		{name: "message with escapes preserved", args: map[string]any{"path": abs, "message": "line1\nline2"}, wantPath: abs, wantMessage: "line1\nline2"},
		{name: "relative path", args: map[string]any{"path": "notes", "message": "m"}, wantErr: "path must be absolute"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, message, err := decodeCommit(marshalArgs(t, tt.args))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("decodeCommit() = %q, %q, %v; want error containing %q", path, message, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeCommit() = %v", err)
			}
			if path != tt.wantPath || message != tt.wantMessage {
				t.Fatalf("decodeCommit() = %q, %q; want %q, %q", path, message, tt.wantPath, tt.wantMessage)
			}
		})
	}
}

func TestDecodeCommitOversizedMessage(t *testing.T) {
	message := strings.Repeat("x", maxMessageBytes+1)
	_, _, err := decodeCommit(marshalArgs(t, map[string]any{"path": "/tmp/n", "message": message}))
	if err == nil || !strings.Contains(err.Error(), "16384") {
		t.Fatalf("decodeCommit() error = %v, want the 16384-byte bound", err)
	}
	// The documented maximum is accepted.
	message = strings.Repeat("x", maxMessageBytes)
	if _, _, err := decodeCommit(marshalArgs(t, map[string]any{"path": "/tmp/n", "message": message})); err != nil {
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
