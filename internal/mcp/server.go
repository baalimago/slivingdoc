// Package mcp exposes the notebook service as the two-tool MCP server over
// stdio (architecture sections 2, 17, and 18). It owns the strict tool
// schemas, the strict input decoding, the transport, and the mapping of the
// stable error taxonomy to the structured tool-error shape. The package
// consumes the narrow Service interface, so in-memory tests need no S3,
// Docker, or native Git engine.
package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/baalimago/slivingdoc/internal/notebook"
)

// Tool names (architecture section 2). Exactly these two tools are
// registered; no other tool, prompt, or resource exists.
const (
	toolPull   = "notes_pull"
	toolCommit = "notes_commit"
)

// Service is the notebook view consumed by the tools: one requested
// visible path resolves to one notebook inside the implementation. Each
// method returns the operation result, which the handler maps into the
// structured success envelope.
type Service interface {
	// Root is the notebook directory an omitted request path resolves to.
	Root() string
	// ReadOnlyPaths is the normalized, sorted read-only set; never nil.
	ReadOnlyPaths() []string
	// Pull writes the current notebook into the resolved path.
	Pull(ctx context.Context, path string) (notebook.Result, error)
	// Commit publishes the caller's changes at the resolved path with the
	// given message.
	Commit(ctx context.Context, path, message string) (notebook.Result, error)
}

// Server is the slivingdoc MCP server. It registers exactly two tools and
// runs over one persistent transport at a time.
type Server struct {
	sdk *sdk.Server
}

// NewServer builds the two-tool server over svc. version is the
// slivingdoc release version advertised in the MCP implementation; logger
// receives server activity and must never write to stdout (logs use
// stderr). A nil logger discards activity.
func NewServer(svc Service, version string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	entries := svc.ReadOnlyPaths()
	impl := &sdk.Implementation{Name: "slivingdoc", Version: version}
	s := sdk.NewServer(impl, &sdk.ServerOptions{
		Instructions: instructions(svc.Root(), entries),
		Logger:       sdkLogger(logger),
	})
	h := &handler{svc: svc, logger: logger}
	s.AddTool(&sdk.Tool{
		Name:        toolPull,
		Description: pullDescription + readOnlyDescriptionSuffix(entries),
		InputSchema: pullSchema,
	}, h.pull)
	s.AddTool(&sdk.Tool{
		Name:        toolCommit,
		Description: commitDescription + readOnlyDescriptionSuffix(entries),
		InputSchema: commitSchema,
	}, h.commit)
	return &Server{sdk: s}
}

// Serve runs one session over an explicit transport until the client
// terminates the connection or ctx is canceled. The process body serves
// over stdio and tests inject in-memory transports. Stdout carries only
// protocol messages; the server never writes logs to it.
func (s *Server) Serve(ctx context.Context, transport sdk.Transport) error {
	return s.sdk.Run(ctx, transport)
}

// Connect serves one session over an explicit transport and returns the
// session, which the caller must close and wait on. Tests use in-memory
// transports; the process body uses Run and Serve.
func (s *Server) Connect(ctx context.Context, transport sdk.Transport) (*sdk.ServerSession, error) {
	return s.sdk.Connect(ctx, transport, nil)
}

// handler binds the two tool handlers to one service. The logger receives
// one correlated pair of records per tool call: the start record and the
// completion record, both carrying the mcpReqID correlation ID, the tool
// name, and (for completion) the duration and outcome. The same logger is
// attached to the request context for the notebook's background-effort
// warnings, so every record of one call shares the mcpReqID.
type handler struct {
	svc    Service
	logger *slog.Logger
}

// path resolves an omitted request path to the server's notebook root, so
// the service always receives one absolute directory.
func (h *handler) path(requested string) string {
	if requested == "" {
		return h.svc.Root()
	}
	return requested
}

func (h *handler) pull(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
	logger, ctx, reqID := h.requestLogger(ctx, toolPull)
	start := time.Now()
	logger.Info("tool call started")
	requested, err := decodePull(req.Params.Arguments)
	if err != nil {
		logger.Warn("tool call completed", "outcome", "invalid_request", "duration", time.Since(start))
		return h.errorResult(decodeFailureError(err), reqID), nil
	}
	path := h.path(requested)
	result, err := h.svc.Pull(ctx, path)
	if err != nil {
		logger.Warn("tool call completed", "outcome", "error", "duration", time.Since(start), "cause", redactValues(err.Error()))
		return h.resultFor(err, reqID)
	}
	logger.Info("tool call completed", "outcome", "ok", "duration", time.Since(start))
	return h.successResult(result, path), nil
}

func (h *handler) commit(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
	logger, ctx, reqID := h.requestLogger(ctx, toolCommit)
	start := time.Now()
	logger.Info("tool call started")
	requested, message, err := decodeCommit(req.Params.Arguments)
	if err != nil {
		logger.Warn("tool call completed", "outcome", "invalid_request", "duration", time.Since(start))
		return h.errorResult(decodeFailureError(err), reqID), nil
	}
	path := h.path(requested)
	result, err := h.svc.Commit(ctx, path, message)
	if err != nil {
		logger.Warn("tool call completed", "outcome", "error", "duration", time.Since(start), "cause", redactValues(err.Error()))
		return h.resultFor(err, reqID)
	}
	logger.Info("tool call completed", "outcome", "ok", "duration", time.Since(start))
	return h.successResult(result, path), nil
}

// requestLogger derives the request-scoped logger from the server logger:
// the mcpReqID correlation ID, the tool name, and the notebook logger
// attached to the derived context. The SDK does not expose the wire
// JSON-RPC request ID to handlers, so the server generates its own
// 16-hex-char correlation ID per call.
func (h *handler) requestLogger(ctx context.Context, tool string) (*slog.Logger, context.Context, string) {
	reqID := newRequestID()
	logger := h.logger.With("mcpReqID", reqID, "tool", tool)
	return logger, notebook.WithLogger(ctx, logger), reqID
}

// newRequestID returns a fresh 16-hex-char correlation ID. A randomness
// failure is practically unreachable and degrades to a clock-based fallback
// so logging never fails.
func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().String()))[:16]
	}
	return hex.EncodeToString(b[:])
}

// resultFor converts a service error into the MCP result: a domain error
// becomes an isError tool result with one candid text item and the
// structured object; any other error (request cancellation) stays a
// protocol error.
func (h *handler) resultFor(err error, diagnosticID string) (*sdk.CallToolResult, error) {
	te, domain := MapError(err)
	if !domain {
		return nil, err
	}
	return h.errorResult(te, diagnosticID), nil
}

// successResult is the success envelope: one text item and the structured
// SuccessInfo object (architecture section 2). The text item is often the
// only part a client forwards to its model, so it names the directory and
// the read-only set.
func (h *handler) successResult(result notebook.Result, path string) *sdk.CallToolResult {
	info := MapSuccess(result, path)
	info.ReadOnly = h.svc.ReadOnlyPaths()
	return &sdk.CallToolResult{
		Content:           []sdk.Content{&sdk.TextContent{Text: successText(path, info.ReadOnly)}},
		StructuredContent: info,
	}
}

// successText is the success text item (architecture section 2, Read-only
// paths).
func successText(path string, entries []string) string {
	if len(entries) == 0 {
		return path
	}
	return path + " (read-only: " + strings.Join(entries, notebook.ReadOnlyListSeparator) + ")"
}

// instructions is the server instruction text: the notebook directory and,
// when configured, the read-only set.
func instructions(root string, entries []string) string {
	base := "The notebook directory is " + root + ". Call notes_pull, edit " +
		"UTF-8 text files (without U+0000) there, then call notes_commit to " +
		"publish. Both tools default to that directory; pass path only to " +
		"address a subdirectory of it."
	if len(entries) == 0 {
		return base
	}
	return base + " Read-only paths: " + strings.Join(entries, notebook.ReadOnlyListSeparator) +
		". notes_commit refuses any change under them, resets those files, and reports READ_ONLY_PATH; write elsewhere."
}

// readOnlyDescriptionSuffix advertises a non-empty read-only set in a tool
// description.
func readOnlyDescriptionSuffix(entries []string) string {
	if len(entries) == 0 {
		return ""
	}
	return " Read-only paths in this server: " + strings.Join(entries, notebook.ReadOnlyListSeparator) +
		"; changes under them are refused and reset."
}

// errorResult attaches the server's read-only set and builds the envelope.
func (h *handler) errorResult(te *ToolError, diagnosticID string) *sdk.CallToolResult {
	te.ReadOnly = h.svc.ReadOnlyPaths()
	te.DiagnosticID = diagnosticID
	return errorResult(te)
}

// errorResult is the domain-error envelope: isError, one candid text item,
// and the exact structured error object.
func errorResult(te *ToolError) *sdk.CallToolResult {
	return &sdk.CallToolResult{
		IsError:           true,
		Content:           []sdk.Content{&sdk.TextContent{Text: errorText(te)}},
		StructuredContent: te,
	}
}

// errorText renders the complete safe error summary for clients that discard
// StructuredContent and forward only the MCP text item.
func errorText(te *ToolError) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s\n%s\n", te.Code, te.Reason, te.Message)
	if te.Detail != "" {
		fmt.Fprintf(&b, "detail: %s\n", te.Detail)
	}
	for _, file := range te.Files {
		fmt.Fprintf(&b, "file: %s · %s", file.Path, file.Reason)
		if len(file.Ranges) > 0 {
			b.WriteString(" · lines ")
			for i, r := range file.Ranges {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(&b, "%d-%d", r.Start, r.End)
			}
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "action: %s\nretryable: %t\ndiagnosticId: %s", te.Action, te.Retryable, te.DiagnosticID)
	if rec := te.Recovery; rec != nil {
		fmt.Fprintf(&b, "\nrecovery: stage=%s remoteAccepted=%s resynchronized=%t", rec.Stage, rec.RemoteAccepted, rec.Resynchronized)
	}
	if len(te.ReadOnly) > 0 {
		fmt.Fprintf(&b, "\nread-only: %s", strings.Join(te.ReadOnly, notebook.ReadOnlyListSeparator))
	}
	return b.String()
}

// Tool descriptions tell the caller to edit UTF-8 text files between pull
// and commit (architecture section 2).
const (
	pullDescription = "Write the current notebook into the notebook directory " +
		"and record the accepted state. Omit path or pass an empty string to use the server's notebook " +
		"directory, which the result reports. Edit UTF-8 text files (without " +
		"U+0000) there between notes_pull and notes_commit; notes_commit " +
		"publishes the changes and incorporates concurrent non-conflicting changes."

	commitDescription = "Publish the caller's changes in the notebook directory " +
		"and incorporate concurrent non-conflicting changes. Omit path or pass an empty string to use " +
		"the server's notebook directory, which the result reports. message must " +
		"be non-blank UTF-8 without U+0000, at most 16,384 bytes; it is retained " +
		"in recent internal history only."
)

// The strict input schemas advertise the documented fields and reject
// unknown ones. The byte bounds are documented in the schema description;
// the exact 4,096/16,384-byte limits are enforced by the strict decode,
// because JSON Schema maxLength counts code points, not bytes.
var (
	pullSchema = map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"path": pathProperty},
		"required":             []string{},
		"additionalProperties": false,
	}

	commitSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    pathProperty,
			"message": messageProperty,
		},
		"required":             []string{"message"},
		"additionalProperties": false,
	}

	pathProperty = map[string]any{
		"type": "string",
		"description": "Optional absolute UTF-8 host path of the notebook " +
			"directory, 1 through 4,096 bytes. Omit it or pass an empty string to use the server's " +
			"notebook directory.",
		"minLength": 0,
		"maxLength": maxPathBytes,
	}

	messageProperty = map[string]any{
		"type":        "string",
		"description": "Commit message: non-blank UTF-8 without U+0000, at most 16,384 bytes.",
		"minLength":   1,
		"maxLength":   maxMessageBytes,
	}
)
