// Package notebook composes workspaces, Git state, and storage into the
// safe pull and optimistic commit operations of architecture sections 10-15.
// It is the only consumer of the storage protocol besides cleanup: Pull
// reads and validates the authoritative manifest and imports packs; Commit
// builds proposals, uploads immutable packs before their manifest CAS, and
// resolves contention, ambiguity, and recovery.
//
// The package consumes narrow consumer-owned interfaces (Workspace and the
// storage.ObjectStore boundary). All CGo and libgit2 types stay inside
// internal/git2; the notebook speaks only the git seam.
package notebook

import (
	"errors"
	"fmt"

	"github.com/baalimago/slivingdoc/internal/git"
)

// Code is the stable error taxonomy of notebook operations. The MCP layer
// maps each code to the structured tool error; the text of an error can
// change, its code cannot.
type Code string

const (
	// CodeInvalidRequest reports invalid tool input or a state the
	// operation refuses before any Git or S3 work: a blank commit
	// message, a commit without a managed pull, or invalid visible
	// content.
	CodeInvalidRequest Code = "INVALID_REQUEST"
	// CodeContentConflict reports a three-tree merge conflict. L is
	// rewritten with the full materialized result and the exact conflicted
	// paths and marker ranges are part of the error.
	CodeContentConflict Code = "CONTENT_CONFLICT"
	// CodeStorageIntegrity reports stored state that failed validation:
	// a corrupt pack, a pack that contradicts its descriptor, a missing
	// object in the accepted history, or a cache that cannot be trusted.
	CodeStorageIntegrity Code = "STORAGE_INTEGRITY"
	// CodeStorageFailure reports an object-store operation that failed
	// without a known accepted result: a pack download or upload, a
	// manifest read, or a CAS whose acceptance cannot be proved.
	CodeStorageFailure Code = "STORAGE_FAILURE"
	// CodeRemoteBusy reports that the CAS lost the configured retry
	// bound. Visible files are preserved for another attempt.
	CodeRemoteBusy Code = "REMOTE_BUSY"
	// CodeRecoveryFailure reports an unexpected failure after local
	// mutation started. The generic recovery path ran; the error carries
	// the recovery report.
	CodeRecoveryFailure Code = "RECOVERY_FAILURE"
)

// Reason classifies a domain error one level below Code (architecture
// section 2, Reason tokens by code).
type Reason string

const (
	ReasonMalformedInput      Reason = "MALFORMED_INPUT"
	ReasonPathOutsideRoot     Reason = "PATH_OUTSIDE_ROOT"
	ReasonMessageBlank        Reason = "MESSAGE_BLANK"
	ReasonMessageTooLong      Reason = "MESSAGE_TOO_LONG"
	ReasonMessageInvalid      Reason = "MESSAGE_INVALID"
	ReasonPullRequired        Reason = "PULL_REQUIRED"
	ReasonInvalidContent      Reason = "INVALID_CONTENT"
	ReasonReadOnlyPath        Reason = "READ_ONLY_PATH"
	ReasonMergeConflict       Reason = "MERGE_CONFLICT"
	ReasonUnresolvedMarkers   Reason = "UNRESOLVED_MARKERS"
	ReasonRetriesExhausted    Reason = "RETRIES_EXHAUSTED"
	ReasonManifestRead        Reason = "MANIFEST_READ"
	ReasonPackDownload        Reason = "PACK_DOWNLOAD"
	ReasonPackUpload          Reason = "PACK_UPLOAD"
	ReasonPublicationUnproven Reason = "PUBLICATION_UNPROVEN"
	ReasonManifestWrite       Reason = "MANIFEST_WRITE"
	ReasonLocalState          Reason = "LOCAL_STATE"
	ReasonInternal            Reason = "INTERNAL"
	ReasonManifestInvalid     Reason = "MANIFEST_INVALID"
	ReasonPackInvalid         Reason = "PACK_INVALID"
	ReasonHistoryInvalid      Reason = "HISTORY_INVALID"
	ReasonEngineFailed        Reason = "ENGINE_FAILED"
	ReasonLocalMutationFailed Reason = "LOCAL_MUTATION_FAILED"
)

// FileReason classifies one file entry of a domain error.
type FileReason string

const (
	FileReasonTextConflict      FileReason = "TEXT_CONFLICT"
	FileReasonPathConflict      FileReason = "PATH_CONFLICT"
	FileReasonUnresolvedMarkers FileReason = "UNRESOLVED_MARKERS"
	FileReasonReadOnly          FileReason = "READ_ONLY"
	FileReasonInvalidContent    FileReason = "INVALID_CONTENT"
)

// Action is the caller's next step after a domain error.
type Action string

const (
	ActionFixInput  Action = "FIX_INPUT"
	ActionEditFiles Action = "EDIT_FILES"
	ActionPull      Action = "PULL"
	ActionRetry     Action = "RETRY"
	ActionOperator  Action = "OPERATOR"
)

type codeReason struct {
	code   Code
	reason Reason
}

// actionForPairing is the code/reason to action table of architecture
// section 2; CodeRecoveryFailure branches on the recovery report instead.
var actionForPairing = map[codeReason]Action{
	{CodeInvalidRequest, ReasonMalformedInput}:      ActionFixInput,
	{CodeInvalidRequest, ReasonPathOutsideRoot}:     ActionFixInput,
	{CodeInvalidRequest, ReasonMessageBlank}:        ActionFixInput,
	{CodeInvalidRequest, ReasonMessageTooLong}:      ActionFixInput,
	{CodeInvalidRequest, ReasonMessageInvalid}:      ActionFixInput,
	{CodeInvalidRequest, ReasonPullRequired}:        ActionPull,
	{CodeInvalidRequest, ReasonInvalidContent}:      ActionEditFiles,
	{CodeInvalidRequest, ReasonReadOnlyPath}:        ActionEditFiles,
	{CodeContentConflict, ReasonMergeConflict}:      ActionEditFiles,
	{CodeContentConflict, ReasonUnresolvedMarkers}:  ActionEditFiles,
	{CodeRemoteBusy, ReasonRetriesExhausted}:        ActionRetry,
	{CodeStorageFailure, ReasonManifestRead}:        ActionRetry,
	{CodeStorageFailure, ReasonPackDownload}:        ActionRetry,
	{CodeStorageFailure, ReasonPackUpload}:          ActionRetry,
	{CodeStorageFailure, ReasonPublicationUnproven}: ActionPull,
	{CodeStorageFailure, ReasonManifestWrite}:       ActionRetry,
	{CodeStorageFailure, ReasonLocalState}:          ActionRetry,
	{CodeStorageFailure, ReasonInternal}:            ActionRetry,
	{CodeStorageIntegrity, ReasonManifestInvalid}:   ActionOperator,
	{CodeStorageIntegrity, ReasonPackInvalid}:       ActionOperator,
	{CodeStorageIntegrity, ReasonHistoryInvalid}:    ActionOperator,
	{CodeStorageIntegrity, ReasonEngineFailed}:      ActionOperator,
}

// errUnknownActionPairing marks a constructor-site programming error.
var errUnknownActionPairing = errors.New("notebook: programming error: no action mapped for this code/reason pairing")

// actionFor returns the Action for a code/reason pairing. An unknown
// pairing returns ActionRetry and errUnknownActionPairing.
func actionFor(code Code, reason Reason, report *RecoveryReport) (Action, error) {
	if code == CodeRecoveryFailure {
		if report != nil && report.Resynchronized {
			return ActionPull, nil
		}
		return ActionRetry, nil
	}
	if action, ok := actionForPairing[codeReason{code, reason}]; ok {
		return action, nil
	}
	return ActionRetry, fmt.Errorf("%w: code=%q reason=%q", errUnknownActionPairing, code, reason)
}

// ErrorFile names one conflicted or rejected path, its reason, and the
// one-based inclusive marker ranges inside it (architecture section 12).
type ErrorFile struct {
	Path   string
	Reason FileReason
	Ranges []git.MarkerRange
}

// RemoteAccepted is the recovery report's statement about remote
// acceptance: the proposal landed, never landed, or cannot be proved.
type RemoteAccepted string

const (
	RemoteAcceptedYes     RemoteAccepted = "yes"
	RemoteAcceptedNo      RemoteAccepted = "no"
	RemoteAcceptedUnknown RemoteAccepted = "unknown"
)

// RecoveryReport describes one generic recovery run (architecture section
// 15): the failed stage, whether remote acceptance is known, and whether
// resynchronization from authoritative current succeeded.
type RecoveryReport struct {
	Stage          string
	RemoteAccepted RemoteAccepted
	Resynchronized bool
}

// Error is a notebook domain error (architecture section 2). Recovery is
// set only for CodeRecoveryFailure. Cause keeps the underlying failure for
// diagnostics and errors.Is.
type Error struct {
	Code     Code
	Reason   Reason
	Action   Action
	Message  string
	Files    []ErrorFile
	Recovery *RecoveryReport
	Cause    error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("notebook: %s: %s: %s: %v", e.Code, e.Reason, e.Message, e.Cause)
	}
	return fmt.Sprintf("notebook: %s: %s: %s", e.Code, e.Reason, e.Message)
}

// Unwrap exposes the cause so errors.Is can classify wrapped failures.
func (e *Error) Unwrap() error { return e.Cause }

// errCASLost is the internal signal that a conditional manifest write lost
// the race; commit maps it to a retry or REMOTE_BUSY at the bound.
var errCASLost = errors.New("notebook: manifest CAS lost")

// errStaleManifest is the internal signal that a referenced pack
// disappeared during readRemote; the reader re-reads current and restarts
// unless the manifest is unchanged.
var errStaleManifest = errors.New("notebook: manifest references a missing pack")

// invalidRequest builds an INVALID_REQUEST error; cause and files are
// optional.
func invalidRequest(reason Reason, cause error, files []ErrorFile, format string, args ...any) error {
	action, _ := actionFor(CodeInvalidRequest, reason, nil)
	return &Error{
		Code: CodeInvalidRequest, Reason: reason, Action: action,
		Message: fmt.Sprintf(format, args...), Files: files, Cause: cause,
	}
}

// contentConflict builds a CONTENT_CONFLICT error naming every conflicted
// path and marker range.
func contentConflict(reason Reason, message string, files []ErrorFile) error {
	action, _ := actionFor(CodeContentConflict, reason, nil)
	return &Error{Code: CodeContentConflict, Reason: reason, Action: action, Message: message, Files: files}
}

// storageIntegrity builds a STORAGE_INTEGRITY error; cause may be nil.
func storageIntegrity(reason Reason, cause error, format string, args ...any) error {
	action, _ := actionFor(CodeStorageIntegrity, reason, nil)
	return &Error{
		Code: CodeStorageIntegrity, Reason: reason, Action: action,
		Message: fmt.Sprintf(format, args...), Cause: cause,
	}
}

// storageFailure builds a STORAGE_FAILURE error wrapping cause.
func storageFailure(reason Reason, cause error, format string, args ...any) error {
	action, _ := actionFor(CodeStorageFailure, reason, nil)
	return &Error{
		Code: CodeStorageFailure, Reason: reason, Action: action,
		Message: fmt.Sprintf(format, args...), Cause: cause,
	}
}

// remoteBusy builds a REMOTE_BUSY error.
func remoteBusy(format string, args ...any) error {
	const reason = ReasonRetriesExhausted
	action, _ := actionFor(CodeRemoteBusy, reason, nil)
	return &Error{Code: CodeRemoteBusy, Reason: reason, Action: action, Message: fmt.Sprintf(format, args...)}
}

// recoveryFailure builds a RECOVERY_FAILURE error carrying the report and
// the underlying cause.
func recoveryFailure(report RecoveryReport, cause error) error {
	const reason = ReasonLocalMutationFailed
	action, _ := actionFor(CodeRecoveryFailure, reason, &report)
	return &Error{
		Code: CodeRecoveryFailure, Reason: reason, Action: action,
		Message:  "unexpected failure after local mutation started; recovery ran",
		Recovery: &report, Cause: cause,
	}
}

// contentConflictFiles converts git conflicts into the stable error shape.
func contentConflictFiles(conflicts []git.Conflict) []ErrorFile {
	files := make([]ErrorFile, 0, len(conflicts))
	for _, c := range conflicts {
		reason := FileReasonPathConflict
		if c.Content != nil {
			reason = FileReasonTextConflict
		}
		files = append(files, ErrorFile{Path: c.Path, Reason: reason, Ranges: c.Ranges})
	}
	return files
}
