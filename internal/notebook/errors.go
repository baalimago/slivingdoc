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

// ConflictFile names one conflicted path and the one-based inclusive marker
// ranges inside it (architecture section 12). A path with no marker range
// (a file/directory conflict) has an empty Ranges slice.
type ConflictFile struct {
	Path   string
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

// Error is a notebook domain error. Files is present only for
// CodeContentConflict; Recovery only for CodeRecoveryFailure. Cause keeps
// the underlying failure for diagnostics and errors.Is.
type Error struct {
	Code     Code
	Message  string
	Files    []ConflictFile
	Recovery *RecoveryReport
	Cause    error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("notebook: %s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("notebook: %s: %s", e.Code, e.Message)
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

// invalidRequest builds an INVALID_REQUEST error.
func invalidRequest(format string, args ...any) error {
	return &Error{Code: CodeInvalidRequest, Message: fmt.Sprintf(format, args...)}
}

// contentConflict builds a CONTENT_CONFLICT error naming every conflicted
// path and marker range.
func contentConflict(message string, files []ConflictFile) error {
	return &Error{Code: CodeContentConflict, Message: message, Files: files}
}

// storageIntegrity builds a STORAGE_INTEGRITY error.
func storageIntegrity(format string, args ...any) error {
	return &Error{Code: CodeStorageIntegrity, Message: fmt.Sprintf(format, args...)}
}

// storageFailure builds a STORAGE_FAILURE error wrapping cause.
func storageFailure(cause error, format string, args ...any) error {
	return &Error{Code: CodeStorageFailure, Message: fmt.Sprintf(format, args...), Cause: cause}
}

// remoteBusy builds a REMOTE_BUSY error.
func remoteBusy(format string, args ...any) error {
	return &Error{Code: CodeRemoteBusy, Message: fmt.Sprintf(format, args...)}
}

// recoveryFailure builds a RECOVERY_FAILURE error carrying the report and
// the underlying cause.
func recoveryFailure(report RecoveryReport, cause error) error {
	return &Error{
		Code: CodeRecoveryFailure, Message: "unexpected failure after local mutation started; recovery ran",
		Recovery: &report, Cause: cause,
	}
}

// contentConflictFiles converts git conflicts into the stable error shape.
func contentConflictFiles(conflicts []git.Conflict) []ConflictFile {
	files := make([]ConflictFile, 0, len(conflicts))
	for _, c := range conflicts {
		files = append(files, ConflictFile{Path: c.Path, Ranges: c.Ranges})
	}
	return files
}
