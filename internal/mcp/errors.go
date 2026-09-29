package mcp

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// Stable error categories of the tool-error shape
// (architecture/product-contract.md and the worklog error taxonomy). The
// text of an error can change; the code and the structured conflict paths
// are stable. Notebook domain errors carry their own notebook.Code; only
// the codes this package generates itself are named here.
const (
	codeInvalidRequest = "INVALID_REQUEST"
	codeStorageFailure = "STORAGE_FAILURE"
)

// Reason and action tokens for errors raised before a request reaches the
// notebook (architecture/product-contract.md, Reason and action tokens).
const (
	reasonMalformedInput  = "MALFORMED_INPUT"
	reasonPathOutsideRoot = "PATH_OUTSIDE_ROOT"
	reasonInternal        = "INTERNAL"

	actionFixInput = "FIX_INPUT"
	actionRetry    = "RETRY"
)

// ToolError is the structured error object carried in the MCP tool result.
// Code, reason, action, diagnostic ID, retryable, message, and files are always
// present; detail and recovery are conditional (architecture/product-contract.md).
// Request paths are absolute; every files[].path is relative to the
// request path and uses the normalized internal slash form.
type ToolError struct {
	Code         string        `json:"code"`
	Reason       string        `json:"reason"`
	Action       string        `json:"action"`
	DiagnosticID string        `json:"diagnosticId"`
	Retryable    bool          `json:"retryable"`
	Message      string        `json:"message"`
	Detail       string        `json:"detail,omitempty"`
	Files        []ErrorFile   `json:"files"`
	Recovery     *RecoveryInfo `json:"recovery,omitempty"`
	ReadOnly     []string      `json:"readOnly"`
	Writable     []string      `json:"writable"`
}

type ErrorFile struct {
	Path   string       `json:"path"`
	Reason string       `json:"reason"`
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
			Reason:    reasonPathOutsideRoot,
			Action:    actionFixInput,
			Retryable: false,
			Message:   Redact(invalidPathMessage(err)),
			Files:     []ErrorFile{},
			ReadOnly:  []string{},
			Writable:  []string{},
		}, true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, false
	}
	return &ToolError{
		Code:      codeStorageFailure,
		Reason:    reasonInternal,
		Action:    actionRetry,
		Retryable: true,
		Message:   "the notebook service failed unexpectedly; retry the operation",
		Files:     []ErrorFile{},
		ReadOnly:  []string{},
		Writable:  []string{},
	}, true
}

// mapNotebookError converts one notebook domain error into the stable
// tool-error shape. Only the notebook's public message and, for a named
// engine failure, a fixed safe description of the cause reach the envelope;
// raw cause text stays internal.
func mapNotebookError(e *notebook.Error) *ToolError {
	files := make([]ErrorFile, 0, len(e.Files))
	for _, f := range e.Files {
		ranges := make([]ErrorRange, 0, len(f.Ranges))
		for _, r := range f.Ranges {
			ranges = append(ranges, ErrorRange{Start: r.Start, End: r.End})
		}
		files = append(files, ErrorFile{Path: f.Path, Reason: string(f.Reason), Ranges: ranges})
	}
	te := &ToolError{
		Code:      string(e.Code),
		Reason:    string(e.Reason),
		Action:    string(e.Action),
		Retryable: retryable(e.Code, e.Reason),
		Message:   Redact(e.Message),
		Files:     files,
		ReadOnly:  []string{},
		Writable:  []string{},
	}
	if e.Reason == notebook.ReasonEngineFailed && e.Cause != nil {
		te.Detail = safeEngineDetail(e.Cause)
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

// safeEngineDetail describes an engine failure the engine could name itself.
// Sanitizing free-form driver prose is not sound — a denylist of terms both
// mangles the surrounding words and misses every term nobody listed — so the
// mapping is an allowlist over typed causes and an unrecognized cause yields
// no detail at all. The operator reads the full cause from the server log,
// correlated by the result's diagnostic ID.
func safeEngineDetail(err error) string {
	var mode *git.UnsupportedModeError
	switch {
	case errors.As(err, &mode):
		return Redact(fmt.Sprintf("%q is not a regular text file; the notebook stores UTF-8 text files only", mode.Name))
	case errors.Is(err, git.ErrNoNewObjects):
		return "no changed files were found to publish"
	case errors.Is(err, git.ErrObjectMissing):
		return "part of the stored notebook state is missing locally; run notes_pull again to restore it"
	case errors.Is(err, git.ErrEmptyPack):
		return "the downloaded notebook state was empty"
	case errors.Is(err, git.ErrHeadRequired):
		return "the notebook state to record was empty"
	default:
		return ""
	}
}

// redactValues removes protected values from operator-facing diagnostic text:
// everything Redact covers, plus filesystem paths. It leaves vocabulary
// intact, so it is safe for the server log but never for a tool result.
func redactValues(s string) string {
	return strings.TrimSpace(absolutePathRE.ReplaceAllString(Redact(s), "${1}"+redacted))
}

// retryable reports whether a notebook error permits a retry. Storage and
// recovery failures do, except when a store refusal that repeating cannot
// change caused them: a full space, a used-up request allowance, denied
// credentials, and an oversized object.
func retryable(code notebook.Code, reason notebook.Reason) bool {
	switch code {
	case notebook.CodeStorageFailure, notebook.CodeRecoveryFailure:
		return !permanentRefusal(reason)
	case notebook.CodeRemoteBusy:
		return true
	default:
		return false
	}
}

// permanentRefusal reports whether reason names a store refusal a retry
// cannot change.
func permanentRefusal(reason notebook.Reason) bool {
	switch reason {
	case notebook.ReasonStorageFull, notebook.ReasonRequestLimit, notebook.ReasonAccessDenied, notebook.ReasonObjectTooLarge, notebook.ReasonUpgradeRequired:
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

// decodeFailureError maps a decode failure: a *notebook.Error (a rejected
// commit message) keeps its own tokens, anything else is MALFORMED_INPUT.
func decodeFailureError(err error) *ToolError {
	var nb *notebook.Error
	if errors.As(err, &nb) {
		return mapNotebookError(nb)
	}
	return invalidRequest(err)
}

// invalidRequest builds an INVALID_REQUEST tool error from a strict-decode
// failure.
func invalidRequest(cause error) *ToolError {
	return &ToolError{
		Code:      codeInvalidRequest,
		Reason:    reasonMalformedInput,
		Action:    actionFixInput,
		Retryable: false,
		Message:   Redact(cause.Error()),
		Files:     []ErrorFile{},
		ReadOnly:  []string{},
		Writable:  []string{},
	}
}

// Redaction patterns. architecture/product-contract.md forbids credentials,
// S3 keys, private paths, and Git IDs in any error text or data. The
// notebook messages never contain credentials, but pack keys (for example
// "packs/checkpoints/1-<uuid>.pack"), the probe key ("probe/<uuid>"), Git
// object IDs (40 hex), the derived private-directory key (64 hex), and AWS
// access key IDs (AKIA + 16), and hosted API tokens (sld_...) are scrubbed
// as defense in depth.
var (
	packKeyRE      = regexp.MustCompile(`packs/(?:checkpoints|increments)/\d+-[0-9a-fA-F-]{36}\.pack`)
	probeKeyRE     = regexp.MustCompile(`probe/[0-9a-fA-F-]{36}`)
	gitIDRE        = regexp.MustCompile(`\b[0-9a-fA-F]{40}\b`)
	derivedKeyRE   = regexp.MustCompile(`\b[0-9a-fA-F]{64}\b`)
	accessKeyRE    = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
	apiTokenRE     = regexp.MustCompile(`\bsld_[0-9A-Za-z_-]+`)
	userInfoRE     = regexp.MustCompile(`://[^@/\s]+@`)
	absolutePathRE = regexp.MustCompile(`(^|[\s"'(=])((?:[A-Za-z]:\\|/)[^\s:;,()"']*)`)
)

const redacted = "[redacted]"

// Redact removes credentials, S3 keys, private paths, and Git IDs from
// diagnostic text. The output keeps its structure but never leaks a
// protected value.
func Redact(s string) string {
	// Tokens first: an earlier pattern matching inside a token would cut
	// the token match short and leave its tail.
	s = apiTokenRE.ReplaceAllString(s, redacted)
	s = packKeyRE.ReplaceAllString(s, redacted)
	s = probeKeyRE.ReplaceAllString(s, redacted)
	s = gitIDRE.ReplaceAllString(s, redacted)
	s = derivedKeyRE.ReplaceAllString(s, redacted)
	s = accessKeyRE.ReplaceAllString(s, redacted)
	s = userInfoRE.ReplaceAllString(s, "://"+redacted+"@")
	return strings.TrimSpace(s)
}
