package mcp

import (
	"context"
	"log/slog"
	"sync"
	"testing"
)

// recordedLog is one record the recording handler received: the emitted
// level, the message, and every attribute (bound and per-record) by key.
type recordedLog struct {
	level slog.Level
	msg   string
	attrs map[string]slog.Value
}

// recordingHandler captures records above min. It is the observable inner
// handler the sdkLogger tests wrap: min models the configured module level
// and logs collects what would reach stderr.
type recordingHandler struct {
	mu    *sync.Mutex
	min   slog.Level
	bound []slog.Attr
	logs  *[]recordedLog
}

func newRecordingHandler(min slog.Level) *recordingHandler {
	return &recordingHandler{mu: &sync.Mutex{}, min: min, logs: &[]recordedLog{}}
}

func (h *recordingHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.min
}

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := map[string]slog.Value{}
	for _, a := range h.bound {
		attrs[a.Key] = a.Value
	}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.logs = append(*h.logs, recordedLog{level: r.Level, msg: r.Message, attrs: attrs})
	return nil
}

func (h *recordingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	bound := append(append([]slog.Attr{}, h.bound...), attrs...)
	return &recordingHandler{mu: h.mu, min: h.min, bound: bound, logs: h.logs}
}

func (h *recordingHandler) WithGroup(string) slog.Handler { return h }

func (h *recordingHandler) records() []recordedLog {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]recordedLog{}, *h.logs...)
}

// TestSDKLoggerDemotesAndDropsEmptySessionID proves the SDK-facing wrapper:
// the SDK's INFO lifecycle record reaches the inner handler at DEBUG, an
// empty session_id disappears, and a real session ID passes through
// untouched.
func TestSDKLoggerDemotesAndDropsEmptySessionID(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		wantAttr  bool
	}{
		{name: "empty stdio session ID is dropped", sessionID: "", wantAttr: false},
		{name: "real session ID passes through", sessionID: "abc", wantAttr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner := newRecordingHandler(slog.LevelDebug)
			sdkLogger(slog.New(inner)).Info("server session connected", "session_id", tt.sessionID)
			logs := inner.records()
			if len(logs) != 1 {
				t.Fatalf("records = %d, want exactly 1", len(logs))
			}
			got := logs[0]
			if got.level != slog.LevelDebug {
				t.Fatalf("level = %v, want DEBUG", got.level)
			}
			if got.msg != "server session connected" {
				t.Fatalf("msg = %q, want the SDK message", got.msg)
			}
			value, ok := got.attrs["session_id"]
			if ok != tt.wantAttr {
				t.Fatalf("session_id present = %v, want %v (attrs %v)", ok, tt.wantAttr, got.attrs)
			}
			if tt.wantAttr && value.String() != tt.sessionID {
				t.Fatalf("session_id = %q, want %q", value.String(), tt.sessionID)
			}
		})
	}
}

// TestSDKLoggerDropsEmptyNamedStringType proves the empty-string rule
// covers named string types, which slog stores as KindAny: the SDK logs
// its LoggingLevel that way (level="" in "client log level set").
func TestSDKLoggerDropsEmptyNamedStringType(t *testing.T) {
	type loggingLevel string
	inner := newRecordingHandler(slog.LevelDebug)
	sdkLogger(slog.New(inner)).Info("client log level set", "level", loggingLevel(""))
	logs := inner.records()
	if len(logs) != 1 {
		t.Fatalf("records = %d, want exactly 1", len(logs))
	}
	if _, ok := logs[0].attrs["level"]; ok {
		t.Fatalf("attrs = %v, want no empty named-string level", logs[0].attrs)
	}
}

// TestSDKLoggerSilentAboveDebug proves the demotion is effective at the
// default INFO level: no SDK record reaches the inner handler, whatever
// level the SDK chose.
func TestSDKLoggerSilentAboveDebug(t *testing.T) {
	inner := newRecordingHandler(slog.LevelInfo)
	logger := sdkLogger(slog.New(inner))
	logger.Info("server session connected", "session_id", "")
	logger.Error("connection closed", "error", "eof")
	if logs := inner.records(); len(logs) != 0 {
		t.Fatalf("records = %v, want none above the DEBUG threshold", logs)
	}
}

// TestSDKLoggerFiltersBoundAttrs proves a logger the SDK derives with With
// follows the same empty-string rule as per-record attributes.
func TestSDKLoggerFiltersBoundAttrs(t *testing.T) {
	inner := newRecordingHandler(slog.LevelDebug)
	sdkLogger(slog.New(inner)).With("session_id", "", "transport", "stdio").Debug("session initialized")
	logs := inner.records()
	if len(logs) != 1 {
		t.Fatalf("records = %d, want exactly 1", len(logs))
	}
	if _, ok := logs[0].attrs["session_id"]; ok {
		t.Fatalf("attrs = %v, want no empty session_id", logs[0].attrs)
	}
	if got := logs[0].attrs["transport"]; got.String() != "stdio" {
		t.Fatalf("transport = %q, want the bound value kept", got.String())
	}
}

// TestUnwrappedLoggerKeepsLevels proves the wrapper touches only the
// SDK-facing logger: slivingdoc's own logger over the same handler still
// emits its tool-call records at INFO with every attribute intact.
func TestUnwrappedLoggerKeepsLevels(t *testing.T) {
	inner := newRecordingHandler(slog.LevelInfo)
	slog.New(inner).Info("tool call started", "mcpReqID", "0011223344556677", "tool", toolPull)
	logs := inner.records()
	if len(logs) != 1 {
		t.Fatalf("records = %d, want exactly 1", len(logs))
	}
	got := logs[0]
	if got.level != slog.LevelInfo {
		t.Fatalf("level = %v, want INFO", got.level)
	}
	if got.attrs["mcpReqID"].String() != "0011223344556677" || got.attrs["tool"].String() != toolPull {
		t.Fatalf("attrs = %v, want the tool-call correlation attributes", got.attrs)
	}
}
