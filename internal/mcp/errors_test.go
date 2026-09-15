package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// TestMapErrorEveryCategory proves the stable mapping of the shared error
// taxonomy (the worklog error taxonomy): each notebook category maps to
// its code, retryable flag, and the always-present files array; conflict
// files and recovery reports survive exactly.
func TestMapErrorEveryCategory(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   string
		wantReason string
		wantAction string
		wantRetry  bool
		wantFiles  bool
		wantReco   bool
	}{
		{
			name: "invalid request", wantCode: codeInvalidRequest, wantReason: "MESSAGE_BLANK", wantAction: "FIX_INPUT", wantRetry: false,
			err: &notebook.Error{Code: notebook.CodeInvalidRequest, Reason: notebook.ReasonMessageBlank, Action: notebook.ActionFixInput, Message: "commit message must not be blank"},
		},
		{name: "content conflict", err: conflictError(), wantCode: "CONTENT_CONFLICT", wantReason: "MERGE_CONFLICT", wantAction: "EDIT_FILES", wantRetry: false, wantFiles: true},
		{
			name: "remote busy", wantCode: "REMOTE_BUSY", wantReason: "RETRIES_EXHAUSTED", wantAction: "RETRY", wantRetry: true,
			err: &notebook.Error{Code: notebook.CodeRemoteBusy, Reason: notebook.ReasonRetriesExhausted, Action: notebook.ActionRetry, Message: "another writer kept winning"},
		},
		{
			name: "storage failure", wantCode: codeStorageFailure, wantReason: "PACK_UPLOAD", wantAction: "RETRY", wantRetry: true,
			err: &notebook.Error{Code: notebook.CodeStorageFailure, Reason: notebook.ReasonPackUpload, Action: notebook.ActionRetry, Message: "pack upload failed"},
		},
		{
			name: "storage integrity", wantCode: "STORAGE_INTEGRITY", wantReason: "PACK_INVALID", wantAction: "OPERATOR", wantRetry: false,
			err: &notebook.Error{Code: notebook.CodeStorageIntegrity, Reason: notebook.ReasonPackInvalid, Action: notebook.ActionOperator, Message: "corrupt pack"},
		},
		{name: "recovery failure", err: recoveryError(), wantCode: "RECOVERY_FAILURE", wantReason: "LOCAL_MUTATION_FAILED", wantAction: "PULL", wantRetry: true, wantReco: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			te, domain := MapError(tt.err)
			if !domain {
				t.Fatal("MapError() reported a non-domain error")
			}
			if te.Code != tt.wantCode {
				t.Fatalf("code = %q, want %q", te.Code, tt.wantCode)
			}
			if te.Reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", te.Reason, tt.wantReason)
			}
			if te.Action != tt.wantAction {
				t.Fatalf("action = %q, want %q", te.Action, tt.wantAction)
			}
			if te.Retryable != tt.wantRetry {
				t.Fatalf("retryable = %v, want %v", te.Retryable, tt.wantRetry)
			}
			if te.Message == "" {
				t.Fatal("message must not be empty")
			}
			if te.Files == nil {
				t.Fatal("files must always be present")
			}
			if tt.wantFiles && len(te.Files) == 0 {
				t.Fatal("conflict error must carry files")
			}
			if tt.wantReco && te.Recovery == nil {
				t.Fatal("recovery failure must carry the recovery report")
			}
			if !tt.wantReco && te.Recovery != nil {
				t.Fatal("only RECOVERY_FAILURE carries the recovery report")
			}
		})
	}
}

// TestMapErrorReasonAndActionAlwaysPresent checks every MapError branch sets
// reason and action.
func TestMapErrorReasonAndActionAlwaysPresent(t *testing.T) {
	errs := []error{
		&notebook.Error{Code: notebook.CodeInvalidRequest, Reason: notebook.ReasonPullRequired, Action: notebook.ActionPull, Message: "commit requires a pull"},
		workspace.ErrInvalidPath,
		workspace.ErrSymlink,
		errors.New("unrecognized failure"),
	}
	for _, err := range errs {
		te, domain := MapError(err)
		if !domain {
			t.Fatalf("MapError(%v) is not a domain error", err)
		}
		if te.Reason == "" {
			t.Fatalf("MapError(%v) reason is empty", err)
		}
		if te.Action == "" {
			t.Fatalf("MapError(%v) action is empty", err)
		}
	}
}

// TestMapErrorInvalidContentFile checks a named INVALID_CONTENT file maps
// through unchanged.
func TestMapErrorInvalidContentFile(t *testing.T) {
	err := &notebook.Error{
		Code: notebook.CodeInvalidRequest, Reason: notebook.ReasonInvalidContent, Action: notebook.ActionEditFiles,
		Message: "visible files violate the notebook contract",
		Files:   []notebook.ErrorFile{{Path: "docs/bad.md", Reason: notebook.FileReasonInvalidContent}},
	}
	te, domain := MapError(err)
	if !domain {
		t.Fatal("MapError() reported a non-domain error")
	}
	if len(te.Files) != 1 || te.Files[0].Path != "docs/bad.md" || te.Files[0].Reason != "INVALID_CONTENT" {
		t.Fatalf("files = %+v, want exactly [{docs/bad.md INVALID_CONTENT}]", te.Files)
	}
}

// TestMapErrorConflictShape proves the exact structured conflict data: the
// normalized relative path and the one-based inclusive ranges survive the
// mapping unchanged.
func TestMapErrorConflictShape(t *testing.T) {
	te, _ := MapError(conflictError())
	if len(te.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(te.Files))
	}
	first := te.Files[0]
	if first.Path != "notes/today.md" || first.Reason != "TEXT_CONFLICT" {
		t.Fatalf("path/reason = %q/%q, want notes/today.md/TEXT_CONFLICT", first.Path, first.Reason)
	}
	if len(first.Ranges) != 2 || first.Ranges[0] != (ErrorRange{12, 18}) || first.Ranges[1] != (ErrorRange{25, 25}) {
		t.Fatalf("ranges = %+v, want [{12 18} {25 25}]", first.Ranges)
	}
	second := te.Files[1]
	if second.Path != "notes/plan.txt" || second.Reason != "PATH_CONFLICT" || len(second.Ranges) != 0 {
		t.Fatalf("second file = %+v, want PATH_CONFLICT with an empty ranges array", second)
	}
}

// TestMapErrorRecoveryShape proves the recovery report: stage, the
// remoteAccepted enum, and the resynchronized flag.
func TestMapErrorRecoveryShape(t *testing.T) {
	te, _ := MapError(recoveryError())
	if te.Recovery.Stage != "commit.cas" {
		t.Fatalf("stage = %q", te.Recovery.Stage)
	}
	if te.Recovery.RemoteAccepted != "yes" {
		t.Fatalf("remoteAccepted = %q, want yes", te.Recovery.RemoteAccepted)
	}
	if !te.Recovery.Resynchronized {
		t.Fatal("resynchronized = false, want true")
	}
}

// TestMapErrorServicePath maps the workspace path sentinels the service
// returns before any notebook work to PATH_OUTSIDE_ROOT/FIX_INPUT.
func TestMapErrorServicePath(t *testing.T) {
	for _, err := range []error{workspace.ErrInvalidPath, workspace.ErrSymlink} {
		te, domain := MapError(err)
		if !domain {
			t.Fatalf("MapError(%v) is not a domain error", err)
		}
		if te.Code != codeInvalidRequest || te.Retryable {
			t.Fatalf("MapError(%v) = %+v, want INVALID_REQUEST not retryable", err, te)
		}
		if te.Reason != reasonPathOutsideRoot || te.Action != actionFixInput {
			t.Fatalf("MapError(%v) reason/action = %q/%q, want %q/%q", err, te.Reason, te.Action, reasonPathOutsideRoot, actionFixInput)
		}
	}
}

// TestMapErrorEscapeNamesRoot proves that an out-of-root request error
// names the workspace root so the caller can correct the request, and
// never echoes the rejected path (the requested path may be a guess at
// private state, which caller-facing text must not confirm).
func TestMapErrorEscapeNamesRoot(t *testing.T) {
	root := "/srv/notes"
	rejected := "/srv/elsewhere"
	err := &workspace.PathEscapeError{Path: rejected, Root: root}
	te, domain := MapError(err)
	if !domain || te.Code != codeInvalidRequest || te.Retryable {
		t.Fatalf("MapError(%v) = %+v, %v; want non-retryable INVALID_REQUEST", err, te, domain)
	}
	if !strings.Contains(te.Message, root) {
		t.Fatalf("message = %q, want it to name the workspace root %q", te.Message, root)
	}
	if strings.Contains(te.Message, rejected) {
		t.Fatalf("message = %q, must not echo the rejected path %q", te.Message, rejected)
	}
}

// TestMapErrorCancellationKeepsProtocolError proves that a canceled
// request is not wrapped into the tool-error envelope.
func TestMapErrorCancellationKeepsProtocolError(t *testing.T) {
	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		if te, domain := MapError(fmt.Errorf("wrap: %w", err)); domain || te != nil {
			t.Fatalf("MapError(%v) = %+v, %v; want a protocol error", err, te, domain)
		}
	}
}

// TestMapErrorFallback maps an unknown service failure to the stable
// retryable storage category.
func TestMapErrorFallback(t *testing.T) {
	te, domain := MapError(errors.New("unexpected internal failure"))
	if !domain || te.Code != codeStorageFailure || !te.Retryable {
		t.Fatalf("MapError() = %+v, %v; want a retryable storage failure", te, domain)
	}
}

func conflictError() error {
	return &notebook.Error{
		Code: notebook.CodeContentConflict, Reason: notebook.ReasonMergeConflict, Action: notebook.ActionEditFiles,
		Message: "Resolve the conflict blocks before notes_commit.",
		Files: []notebook.ErrorFile{
			{Path: "notes/today.md", Reason: notebook.FileReasonTextConflict, Ranges: []git.MarkerRange{{Start: 12, End: 18}, {Start: 25, End: 25}}},
			{Path: "notes/plan.txt", Reason: notebook.FileReasonPathConflict, Ranges: nil},
		},
	}
}

func recoveryError() error {
	return &notebook.Error{
		Code: notebook.CodeRecoveryFailure, Reason: notebook.ReasonLocalMutationFailed, Action: notebook.ActionPull,
		Message:  "unexpected failure after local mutation started; recovery ran",
		Recovery: &notebook.RecoveryReport{Stage: "commit.cas", RemoteAccepted: notebook.RemoteAcceptedYes, Resynchronized: true},
	}
}

// TestRedact scrubs credentials, S3 keys, private paths, and Git IDs from
// diagnostic text (architecture section 2).
func TestRedact(t *testing.T) {
	packUUID := "0196c2d0-7f2b-7e00-8000-000000000004"
	probeUUID := "0196c2d0-7f2b-7e00-8000-000000000005"
	gitID := strings.Repeat("a", 40)
	derivedKey := strings.Repeat("b", 64)
	input := "download pack packs/increments/3-" + packUUID + ".pack failed; " +
		"probe/" + probeUUID + " did not create; " +
		"head " + gitID + " unreadable; " +
		"private /home/user/.cache/slivingdoc/" + derivedKey + " + " +
		"key AKIAIOSFODNN7EXAMPLE and endpoint http://user:secret@s3.example.com"
	got := Redact(input)
	for _, leaked := range []string{
		"packs/increments", packUUID, "probe/" + probeUUID,
		gitID, derivedKey, "AKIAIOSFODNN7EXAMPLE", "user:secret",
	} {
		if strings.Contains(got, leaked) {
			t.Fatalf("Redact() leaked %q in %q", leaked, got)
		}
	}
	for _, kept := range []string{"download", "failed", "private", "endpoint"} {
		if !strings.Contains(got, kept) {
			t.Fatalf("Redact() dropped %q in %q", kept, got)
		}
	}
}

// TestRedactPreservesConflictPaths proves that the redaction never touches
// the normalized relative paths that must survive in the error files.
func TestRedactPreservesConflictPaths(t *testing.T) {
	got := Redact("Resolve notes/today.md before continuing")
	if !strings.Contains(got, "notes/today.md") {
		t.Fatalf("Redact() changed a conflict path: %q", got)
	}
}

// TestRedactPreservesReasonTokens checks reason and action tokens are copied
// verbatim and survive Redact.
func TestRedactPreservesReasonTokens(t *testing.T) {
	if got := Redact("MESSAGE_BLANK and READ_ONLY_PATH stay exactly as written"); got != "MESSAGE_BLANK and READ_ONLY_PATH stay exactly as written" {
		t.Fatalf("Redact() altered token-shaped text: %q", got)
	}
	err := &notebook.Error{
		Code: notebook.CodeInvalidRequest, Reason: notebook.ReasonInvalidContent, Action: notebook.ActionEditFiles,
		Message: "download pack packs/increments/3-" + "0196c2d0-7f2b-7e00-8000-000000000004" + ".pack failed",
	}
	te, _ := MapError(err)
	if te.Reason != "INVALID_CONTENT" || te.Action != "EDIT_FILES" {
		t.Fatalf("reason/action = %q/%q, want the tokens unaffected by message redaction", te.Reason, te.Action)
	}
	if strings.Contains(te.Message, "packs/increments") {
		t.Fatalf("message = %q, want the pack key redacted", te.Message)
	}
}
