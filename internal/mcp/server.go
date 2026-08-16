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
	"io"
	"log/slog"
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
	impl := &sdk.Implementation{Name: "slivingdoc", Version: version}
	s := sdk.NewServer(impl, &sdk.ServerOptions{
		Instructions: "Edit UTF-8 text files at the request path between " +
			"notes_pull and notes_commit. The notebook accepts UTF-8 text " +
			"without U+0000 only.",
		Logger: logger,
	})
	h := &handler{svc: svc, logger: logger}
	s.AddTool(&sdk.Tool{
		Name:        toolPull,
		Description: pullDescription,
		InputSchema: pullSchema,
	}, h.pull)
	s.AddTool(&sdk.Tool{
		Name:        toolCommit,
		Description: commitDescription,
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

func (h *handler) pull(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
	logger, ctx := h.requestLogger(ctx, toolPull)
	start := time.Now()
	logger.Info("tool call started")
	path, err := decodePull(req.Params.Arguments)
	if err != nil {
		logger.Warn("tool call completed", "outcome", "invalid_request", "duration", time.Since(start))
		return errorResult(invalidRequest(err)), nil
	}
	result, err := h.svc.Pull(ctx, path)
	if err != nil {
		logger.Warn("tool call completed", "outcome", "error", "duration", time.Since(start))
		return resultFor(err)
	}
	logger.Info("tool call completed", "outcome", "ok", "duration", time.Since(start))
	return successResult(result), nil
}

func (h *handler) commit(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
	logger, ctx := h.requestLogger(ctx, toolCommit)
	start := time.Now()
	logger.Info("tool call started")
	path, message, err := decodeCommit(req.Params.Arguments)
	if err != nil {
		logger.Warn("tool call completed", "outcome", "invalid_request", "duration", time.Since(start))
		return errorResult(invalidRequest(err)), nil
	}
	result, err := h.svc.Commit(ctx, path, message)
	if err != nil {
		logger.Warn("tool call completed", "outcome", "error", "duration", time.Since(start))
		return resultFor(err)
	}
	logger.Info("tool call completed", "outcome", "ok", "duration", time.Since(start))
	return successResult(result), nil
}

// requestLogger derives the request-scoped logger from the server logger:
// the mcpReqID correlation ID, the tool name, and the notebook logger
// attached to the derived context. The SDK does not expose the wire
// JSON-RPC request ID to handlers, so the server generates its own
// 16-hex-char correlation ID per call.
func (h *handler) requestLogger(ctx context.Context, tool string) (*slog.Logger, context.Context) {
	reqID := newRequestID()
	logger := h.logger.With("mcpReqID", reqID, "tool", tool)
	return logger, notebook.WithLogger(ctx, logger)
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
func resultFor(err error) (*sdk.CallToolResult, error) {
	te, domain := MapError(err)
	if !domain {
		return nil, err
	}
	return errorResult(te), nil
}

// successResult is the success envelope: one text item with exactly "OK"
// and the structured SuccessInfo object (architecture section 2).
func successResult(result notebook.Result) *sdk.CallToolResult {
	return &sdk.CallToolResult{
		Content:           []sdk.Content{&sdk.TextContent{Text: "OK"}},
		StructuredContent: MapSuccess(result),
	}
}

// errorResult is the domain-error envelope: isError, one candid text item,
// and the exact structured error object.
func errorResult(te *ToolError) *sdk.CallToolResult {
	return &sdk.CallToolResult{
		IsError:           true,
		Content:           []sdk.Content{&sdk.TextContent{Text: te.Message}},
		StructuredContent: te,
	}
}

// Tool descriptions tell the caller to edit UTF-8 text files between pull
// and commit (architecture section 2).
const (
	pullDescription = "Write the current notebook into the absolute path and " +
		"record the accepted state. Edit UTF-8 text files (without U+0000) at " +
		"that path between notes_pull and notes_commit; notes_commit publishes " +
		"the changes and incorporates concurrent non-conflicting changes."

	commitDescription = "Publish the caller's changes at the absolute path and " +
		"incorporate concurrent non-conflicting changes. message must be " +
		"non-blank UTF-8 without U+0000, at most 16,384 bytes; it is retained " +
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
		"required":             []string{"path"},
		"additionalProperties": false,
	}

	commitSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    pathProperty,
			"message": messageProperty,
		},
		"required":             []string{"path", "message"},
		"additionalProperties": false,
	}

	pathProperty = map[string]any{
		"type":        "string",
		"description": "Absolute UTF-8 host path of the notebook directory, 1 through 4,096 bytes.",
		"minLength":   1,
		"maxLength":   maxPathBytes,
	}

	messageProperty = map[string]any{
		"type":        "string",
		"description": "Commit message: non-blank UTF-8 without U+0000, at most 16,384 bytes.",
		"minLength":   1,
		"maxLength":   maxMessageBytes,
	}
)
