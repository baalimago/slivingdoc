package mcp

import (
	"context"
	"log/slog"
	"reflect"
)

// sdkLogger wraps the server logger for the SDK's ServerOptions.Logger.
// Everything the SDK logs through that logger is session and transport
// plumbing (server run start, session connected, session initialized,
// session disconnected), which drowns the operational records of embedders
// that start one serve per agent request, so the wrapper demotes every
// record to DEBUG. It also drops attributes whose value is the empty
// string: over stdio the SDK has no session ID and would otherwise log a
// confusing session_id="". slivingdoc's own records (serving, tool calls,
// errors) use the unwrapped logger and keep their levels.
func sdkLogger(logger *slog.Logger) *slog.Logger {
	return slog.New(&sdkLogHandler{inner: logger.Handler()})
}

// sdkLogHandler is the slog.Handler decorator behind sdkLogger.
type sdkLogHandler struct {
	inner slog.Handler
}

// Enabled gates on the inner handler's DEBUG threshold regardless of the
// record's original level, because Handle demotes every record to DEBUG.
func (h *sdkLogHandler) Enabled(ctx context.Context, _ slog.Level) bool {
	return h.inner.Enabled(ctx, slog.LevelDebug)
}

// Handle re-emits the record at DEBUG without its empty-string attributes.
func (h *sdkLogHandler) Handle(ctx context.Context, r slog.Record) error {
	demoted := slog.NewRecord(r.Time, slog.LevelDebug, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		if !emptyStringAttr(a) {
			demoted.AddAttrs(a)
		}
		return true
	})
	return h.inner.Handle(ctx, demoted)
}

// WithAttrs binds the non-empty attributes on the inner handler, so a
// logger derived with With follows the same empty-string rule as Handle.
func (h *sdkLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	kept := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		if !emptyStringAttr(a) {
			kept = append(kept, a)
		}
	}
	if len(kept) == 0 {
		return h
	}
	return &sdkLogHandler{inner: h.inner.WithAttrs(kept)}
}

func (h *sdkLogHandler) WithGroup(name string) slog.Handler {
	return &sdkLogHandler{inner: h.inner.WithGroup(name)}
}

// emptyStringAttr reports whether a resolves to an empty string, the shape
// of the SDK's absent stdio session ID. The SDK also logs named string
// types (its LoggingLevel), which slog stores as KindAny, so the check
// covers any string-kinded value.
func emptyStringAttr(a slog.Attr) bool {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return v.String() == ""
	case slog.KindAny:
		rv := reflect.ValueOf(v.Any())
		return rv.IsValid() && rv.Kind() == reflect.String && rv.String() == ""
	default:
		return false
	}
}
