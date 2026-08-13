package mcp

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// Stable error categories of the tool-error shape (architecture section 2
// and the worklog error taxonomy). The text of an error can change; the
// code and the structured conflict paths are stable. Notebook domain
// errors carry their own notebook.Code; only the codes this package
// generates itself are named here.
const (
	codeInvalidRequest = "INVALID_REQUEST"
	codeStorageFailure = "STORAGE_FAILURE"
)

// ToolError is the structured error object carried in the MCP tool result.
// Code, retryable, message, and files are always present; recovery appears
// only for RECOVERY_FAILURE (architecture section 2). Request paths are
// absolute; every files[].path is relative to the request path and uses
// the normalized internal slash form.
type ToolError struct {
	Code      string        `json:"code"`
	Retryable bool          `json:"retryable"`
	Message   string        `json:"message"`
	Files     []ErrorFile   `json:"files"`
	Recovery  *RecoveryInfo `json:"recovery,omitempty"`
}

type ErrorFile struct {
	Path   string       `json:"path"`
	Ranges []ErrorRange `json:"ranges"`
}

// ErrorRange is one conflict-marker block: one-based and inclusive.
type ErrorRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// RecoveryInfo is the RECOVERY_FAILURE report: the failed stage, whether
// remote acceptance is known, and whether resynchronization succeeded.
type RecoveryInfo struct {
	Stage          string `json:"stage"`
	RemoteAccepted string `json:"remoteAccepted"`
	Resynchronized bool   `json:"resynchronized"`
}

// MapError converts a service error into the structured tool error. The
// second result reports whether the error is a domain error: false keeps
// the error a protocol error (request cancellation), so the SDK reports it
// outside the tool-error envelope.
func MapError(err error) (*ToolError, bool) {
	var nb *notebook.Error
	if errors.As(err, &nb) {
		return mapNotebookError(nb), true
	}
	if errors.Is(err, workspace.ErrInvalidPath) || errors.Is(err, workspace.ErrSymlink) {
		return &ToolError{
			Code:      codeInvalidRequest,
			Retryable: false,
			Message:   Redact(invalidPathMessage(err)),
			Files:     []ErrorFile{},
		}, true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, false
	}
	return &ToolError{
		Code:      codeStorageFailure,
		Retryable: true,
		Message:   "the notebook service failed unexpectedly; retry the operation",
		Files:     []ErrorFile{},
	}, true
}

// mapNotebookError converts one notebook domain error into the stable
// tool-error shape. Only the notebook's public message reaches the
// envelope; the wrapped cause stays internal and is never surfaced.
func mapNotebookError(e *notebook.Error) *ToolError {
	files := make([]ErrorFile, 0, len(e.Files))
	for _, f := range e.Files {
		ranges := make([]ErrorRange, 0, len(f.Ranges))
		for _, r := range f.Ranges {
			ranges = append(ranges, ErrorRange{Start: r.Start, End: r.End})
		}
		files = append(files, ErrorFile{Path: f.Path, Ranges: ranges})
	}
	te := &ToolError{
		Code:      string(e.Code),
		Retryable: retryable(e.Code),
		Message:   Redact(e.Message),
		Files:     files,
	}
	if e.Code == notebook.CodeRecoveryFailure && e.Recovery != nil {
		te.Recovery = &RecoveryInfo{
			Stage:          e.Recovery.Stage,
			RemoteAccepted: string(e.Recovery.RemoteAccepted),
			Resynchronized: e.Recovery.Resynchronized,
		}
	}
	return te
}

// retryable reports whether a notebook error category permits a retry.
func retryable(code notebook.Code) bool {
	switch code {
	case notebook.CodeRemoteBusy, notebook.CodeStorageFailure, notebook.CodeRecoveryFailure:
		return true
	default:
		return false
	}
}

// invalidPathMessage explains why the request path was rejected. An
// out-of-root path names the workspace root so the caller can correct the
// request; every other case keeps the generic text. No message echoes the
// rejected path, because it may be a guess at private state.
func invalidPathMessage(err error) string {
	var esc *workspace.PathEscapeError
	if errors.As(err, &esc) {
		return fmt.Sprintf("the requested path must stay at or below the workspace root %q", esc.Root)
	}
	return "the requested path is not a valid notebook path"
}

// invalidRequest builds an INVALID_REQUEST tool error from a strict-decode
// failure.
func invalidRequest(cause error) *ToolError {
	return &ToolError{
		Code:      codeInvalidRequest,
		Retryable: false,
		Message:   Redact(cause.Error()),
		Files:     []ErrorFile{},
	}
}

// Report renders the candid CLI text form of the envelope: the category
// and message, the retryable verdict, one indented line per conflicted
// file with its one-based inclusive line ranges, and the recovery report
// when present. The pull and commit subcommands print it verbatim.
func (te *ToolError) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s\n", te.Code, te.Message)
	fmt.Fprintf(&b, "retryable: %t\n", te.Retryable)
	for _, f := range te.Files {
		if len(f.Ranges) == 0 {
			fmt.Fprintf(&b, "  %s\n", f.Path)
			continue
		}
		parts := make([]string, 0, len(f.Ranges))
		for _, r := range f.Ranges {
			parts = append(parts, fmt.Sprintf("%d-%d", r.Start, r.End))
		}
		fmt.Fprintf(&b, "  %s: lines %s\n", f.Path, strings.Join(parts, ", "))
	}
	if rec := te.Recovery; rec != nil {
		fmt.Fprintf(&b, "recovery: stage=%s remoteAccepted=%s resynchronized=%t\n",
			rec.Stage, rec.RemoteAccepted, rec.Resynchronized)
	}
	return b.String()
}

// Redaction patterns. The architecture (section 2) forbids credentials,
// S3 keys, private paths, and Git IDs in any error text or data. The
// notebook messages never contain credentials, but pack keys (for example
// "packs/checkpoints/1-<uuid>.pack"), the probe key ("probe/<uuid>"), Git
// object IDs (40 hex), the derived private-directory key (64 hex), and AWS
// access key IDs (AKIA + 16) are scrubbed as defense in depth.
var (
	packKeyRE    = regexp.MustCompile(`packs/(?:checkpoints|increments)/\d+-[0-9a-fA-F-]{36}\.pack`)
	probeKeyRE   = regexp.MustCompile(`probe/[0-9a-fA-F-]{36}`)
	gitIDRE      = regexp.MustCompile(`\b[0-9a-fA-F]{40}\b`)
	derivedKeyRE = regexp.MustCompile(`\b[0-9a-fA-F]{64}\b`)
	accessKeyRE  = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
	userInfoRE   = regexp.MustCompile(`://[^@/\s]+@`)
)

const redacted = "[redacted]"

// Redact removes credentials, S3 keys, private paths, and Git IDs from
// diagnostic text. The output keeps its structure but never leaks a
// protected value.
func Redact(s string) string {
	s = packKeyRE.ReplaceAllString(s, redacted)
	s = probeKeyRE.ReplaceAllString(s, redacted)
	s = gitIDRE.ReplaceAllString(s, redacted)
	s = derivedKeyRE.ReplaceAllString(s, redacted)
	s = accessKeyRE.ReplaceAllString(s, redacted)
	s = userInfoRE.ReplaceAllString(s, "://"+redacted+"@")
	return strings.TrimSpace(s)
}
