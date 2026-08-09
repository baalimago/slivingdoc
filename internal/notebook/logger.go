package notebook

import (
	"context"
	"io"
	"log/slog"
)

// loggerKey is the context key for the request-scoped notebook logger.
type loggerKey struct{}

// WithLogger attaches logger to ctx for notebook background-effort
// records (checkpoint and cleanup warnings). The MCP layer attaches the
// request-scoped logger carrying the mcpReqID attribute, so a warning
// from a best-effort checkpoint or cleanup stays correlated with the tool
// call that scheduled it.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// LoggerFrom returns the logger attached by WithLogger, or a discard
// logger when none is attached. It never returns nil, so callers can log
// unconditionally.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok && logger != nil {
		return logger
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
