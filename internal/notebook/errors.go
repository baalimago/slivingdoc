// Package notebook composes workspaces, Git state, and storage into the
// safe pull and optimistic commit operations in architecture/pull.md,
// commit.md, conflicts.md, checkpoints.md, and guarantees.md. It is the
// only consumer of the storage protocol besides cleanup: Pull reads and
// validates the authoritative manifest and imports packs; Commit builds
// proposals, uploads immutable packs before their manifest CAS, and
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
	"github.com/baalimago/slivingdoc/internal/storage"
)

// Code is the stable error taxonomy of notebook operations. The MCP layer
// maps each code to the structured tool error; the text of an error can
// change, its code cannot.
type Code string

const (
	// CodeInvalidRequest reports invalid tool input or a state the
	// operation refuses before any Git or S3 work: a blank commit
	// message, a commit without a managed pull, or invalid visible
	// content; or before any local mutation: a first pull into a
	// directory holding files the remote notebook does not.
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

// Reason classifies a domain error one level below Code
// (architecture/product-contract.md, Reason and action tokens).
type Reason string

const (
	ReasonMalformedInput      Reason = "MALFORMED_INPUT"
	ReasonPathOutsideRoot     Reason = "PATH_OUTSIDE_ROOT"
	ReasonMessageBlank        Reason = "MESSAGE_BLANK"
	ReasonMessageTooLong      Reason = "MESSAGE_TOO_LONG"
	ReasonMessageInvalid      Reason = "MESSAGE_INVALID"
	ReasonPullRequired        Reason = "PULL_REQUIRED"
	ReasonDirectoryNotEmpty   Reason = "DIRECTORY_NOT_EMPTY"
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
	ReasonStorageFull         Reason = "STORAGE_FULL"
	ReasonRequestLimit        Reason = "REQUEST_LIMIT"
	ReasonRateLimited         Reason = "RATE_LIMITED"
	ReasonAccessDenied        Reason = "ACCESS_DENIED"
	ReasonObjectTooLarge      Reason = "OBJECT_TOO_LARGE"
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
	// A first pull found the file in the directory but not in the notebook,
	// or with other bytes than the notebook's (ReasonDirectoryNotEmpty).
	FileReasonNotInNotebook       FileReason = "NOT_IN_NOTEBOOK"
	FileReasonDiffersFromNotebook FileReason = "DIFFERS_FROM_NOTEBOOK"
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

// actionForPairing is the code/reason to action table in
// architecture/product-contract.md. actionFor handles CodeRecoveryFailure
// itself: a store refusal reason (isRefusalReason) takes the
// CodeStorageFailure pairing's action, and every other reason branches on
// the recovery report.
var actionForPairing = map[codeReason]Action{
	{CodeInvalidRequest, ReasonMalformedInput}:      ActionFixInput,
	{CodeInvalidRequest, ReasonPathOutsideRoot}:     ActionFixInput,
	{CodeInvalidRequest, ReasonMessageBlank}:        ActionFixInput,
	{CodeInvalidRequest, ReasonMessageTooLong}:      ActionFixInput,
	{CodeInvalidRequest, ReasonMessageInvalid}:      ActionFixInput,
	{CodeInvalidRequest, ReasonPullRequired}:        ActionPull,
	{CodeInvalidRequest, ReasonDirectoryNotEmpty}:   ActionFixInput,
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
	{CodeStorageFailure, ReasonStorageFull}:         ActionOperator,
	{CodeStorageFailure, ReasonRequestLimit}:        ActionOperator,
	{CodeStorageFailure, ReasonRateLimited}:         ActionRetry,
	{CodeStorageFailure, ReasonAccessDenied}:        ActionOperator,
	{CodeStorageFailure, ReasonObjectTooLarge}:      ActionOperator,
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
		if isRefusalReason(reason) {
			// A store refusal stopped the resynchronization: the caller's
			// next step is the refusal's, as for a STORAGE_FAILURE.
			return actionFor(CodeStorageFailure, reason, nil)
		}
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
// one-based inclusive marker ranges inside it (architecture/conflicts.md).
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

// RecoveryReport describes one generic recovery run
// (architecture/guarantees.md): the failed stage, whether remote acceptance
// is known, and whether resynchronization from authoritative current
// succeeded.
type RecoveryReport struct {
	Stage          string
	RemoteAccepted RemoteAccepted
	Resynchronized bool
}

// Error is a notebook domain error (architecture/product-contract.md).
// Recovery is set only for CodeRecoveryFailure. Cause keeps the underlying
// failure for diagnostics and errors.Is.
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

// errManifestRefused marks a manifest write the store answered with a
// refusal: the manifest definitely did not accept the proposal, unlike a
// transport failure, whose acceptance is unknown.
var errManifestRefused = errors.New("notebook: manifest write refused")

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

// storageFailure builds a STORAGE_FAILURE error wrapping cause. A cause
// the store refused for an account reason (a full space, a request limit,
// denied credentials, an oversized object) replaces the operation's reason
// and message, because the caller's next step is about the account, not
// the operation.
func storageFailure(reason Reason, cause error, format string, args ...any) error {
	message := fmt.Sprintf(format, args...)
	if r, m, ok := refusalMessage(cause); ok {
		reason, message = r, m
	}
	action, _ := actionFor(CodeStorageFailure, reason, nil)
	return &Error{
		Code: CodeStorageFailure, Reason: reason, Action: action,
		Message: message, Cause: cause,
	}
}

// refusalMessage is storeRefusal with the store's own sanitized line
// appended to the message when the refusal carries one.
func refusalMessage(cause error) (Reason, string, bool) {
	reason, message, ok := storeRefusal(cause)
	if !ok {
		return "", "", false
	}
	return reason, message + storageSays(cause), true
}

// storageSays is the store's own sanitized line of a refusal, ready to
// append to a message, or "" when the refusal carries none.
func storageSays(cause error) string {
	var refusal *storage.Refusal
	if errors.As(cause, &refusal) && refusal.Message != "" {
		return ". The storage says: " + refusal.Message
	}
	return ""
}

// recoveryRefusalMessages tell the caller why the store refused the read
// that resynchronizes the notebook directory. Unlike storeRefusal's
// messages they make no claim about publication: after an accepted CAS the
// commit was published, and at entry acceptance is unknown.
var recoveryRefusalMessages = map[Reason]string{
	ReasonStorageFull: "the storage refused the read that repairs the notebook directory because the account that owns this space is full; " +
		"its owner must add storage (for slivingdoc.dev: upgrade at https://slivingdoc.dev) or delete notes, then pull",
	ReasonRequestLimit: "the storage refused the read that repairs the notebook directory because the account that owns this space used its " +
		"request allowance for the month; its owner can raise the allowance (for slivingdoc.dev: upgrade at https://slivingdoc.dev) " +
		"or wait until it resets on the first of the month (UTC), then pull",
	ReasonRateLimited: "the storage is slowing down requests from this account, so the notebook directory could not be repaired yet; wait, then pull",
	ReasonAccessDenied: "the storage refused the read that repairs the notebook directory: the token is missing, revoked, read-only, " +
		"or not granted this space, the space does not exist, or --endpoint does not point at the storage API. " +
		"Check SLIVINGDOC_TOKEN, --space and --endpoint, then pull",
	ReasonObjectTooLarge: "the storage refused the read that repairs the notebook directory as larger than it serves; an operator must check the storage",
}

// isRefusalReason reports whether reason is one storeRefusal produces.
func isRefusalReason(reason Reason) bool {
	switch reason {
	case ReasonStorageFull, ReasonRequestLimit, ReasonRateLimited, ReasonAccessDenied, ReasonObjectTooLarge:
		return true
	default:
		return false
	}
}

// storeRefusal names an account-level refusal of the store and the message
// that tells the caller how to fix it. The third result reports whether
// cause is such a refusal.
func storeRefusal(cause error) (Reason, string, bool) {
	switch {
	case errors.Is(cause, storage.ErrQuotaExceeded):
		return ReasonStorageFull, "the storage account that owns this space is full, so nothing was published; " +
			"its owner must add storage (for slivingdoc.dev: upgrade at https://slivingdoc.dev), " +
			"or delete notes to make room, then commit again. Pulls keep working", true
	case errors.Is(cause, storage.ErrRequestLimit):
		return ReasonRequestLimit, "the storage account that owns this space used its request allowance for the month, " +
			"so nothing was published; its owner can raise the allowance (for slivingdoc.dev: upgrade at https://slivingdoc.dev) or wait until it resets " +
			"on the first of the month (UTC), then commit again. Pulls keep working, more slowly", true
	case errors.Is(cause, storage.ErrRateLimited):
		return ReasonRateLimited, "the storage is slowing down requests from this account; wait, then retry", true
	case errors.Is(cause, storage.ErrAccessDenied):
		return ReasonAccessDenied, "the storage refused the request: the token is missing, revoked, read-only, " +
			"or not granted this space, the space does not exist, or --endpoint does not point at the storage API. " +
			"Check SLIVINGDOC_TOKEN, --space and --endpoint", true
	case errors.Is(cause, storage.ErrTooLarge):
		return ReasonObjectTooLarge, "the notebook data to upload is larger than the storage accepts in one object; " +
			"nothing was published", true
	default:
		return "", "", false
	}
}

// remoteBusy builds a REMOTE_BUSY error.
func remoteBusy(format string, args ...any) error {
	const reason = ReasonRetriesExhausted
	action, _ := actionFor(CodeRemoteBusy, reason, nil)
	return &Error{Code: CodeRemoteBusy, Reason: reason, Action: action, Message: fmt.Sprintf(format, args...)}
}

// recoveryFailure builds a RECOVERY_FAILURE error carrying the report, the
// underlying cause, and the failure of the resynchronization, if any. A
// nil cause means the resynchronization failure is the cause itself (entry
// recovery). When the store refused the resynchronizing read, the error
// keeps its code but takes the refusal's reason and action and a
// recovery-specific message (recoveryRefusalMessages), so the caller sees
// why recovery could not finish (architecture/guarantees.md).
func recoveryFailure(report RecoveryReport, cause, resyncErr error) error {
	reason := ReasonLocalMutationFailed
	message := "unexpected failure after local mutation started; recovery ran"
	if r, _, ok := storeRefusal(resyncErr); ok {
		reason = r
		message = "unexpected failure after local mutation started; recovery could not resynchronize the notebook directory: " +
			recoveryRefusalMessages[r] + storageSays(resyncErr)
	}
	switch {
	case cause == nil:
		cause = resyncErr
	case resyncErr != nil:
		cause = errors.Join(cause, resyncErr)
	}
	action, _ := actionFor(CodeRecoveryFailure, reason, &report)
	return &Error{
		Code: CodeRecoveryFailure, Reason: reason, Action: action,
		Message: message, Recovery: &report, Cause: cause,
	}
}

// entryRecovered builds the RECOVERY_FAILURE of a successful entry
// recovery: the repair rewrote L to the accepted state, so edits made there
// since the failed call are gone and the caller must know
// (architecture/guarantees.md).
func entryRecovered(report RecoveryReport, cause error) error {
	const reason = ReasonLocalMutationFailed
	action, _ := actionFor(CodeRecoveryFailure, reason, &report)
	return &Error{
		Code: CodeRecoveryFailure, Reason: reason, Action: action,
		Message: "an earlier call left the notebook directory partially updated; it was rewritten to the accepted " +
			"state and edits made there since that call were discarded; pull, then reapply them",
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
