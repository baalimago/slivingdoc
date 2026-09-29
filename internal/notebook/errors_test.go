package notebook

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// allReasons lists every Reason token under its owning code
// (architecture/product-contract.md).
var allReasons = []struct {
	code   Code
	reason Reason
	action Action
}{
	{CodeInvalidRequest, ReasonMalformedInput, ActionFixInput},
	{CodeInvalidRequest, ReasonPathOutsideRoot, ActionFixInput},
	{CodeInvalidRequest, ReasonMessageBlank, ActionFixInput},
	{CodeInvalidRequest, ReasonMessageTooLong, ActionFixInput},
	{CodeInvalidRequest, ReasonMessageInvalid, ActionFixInput},
	{CodeInvalidRequest, ReasonPullRequired, ActionPull},
	{CodeInvalidRequest, ReasonDirectoryNotEmpty, ActionFixInput},
	{CodeInvalidRequest, ReasonInvalidContent, ActionEditFiles},
	{CodeInvalidRequest, ReasonReadOnlyPath, ActionEditFiles},
	{CodeInvalidRequest, ReasonIgnoredConflict, ActionEditFiles},
	{CodeContentConflict, ReasonMergeConflict, ActionEditFiles},
	{CodeContentConflict, ReasonUnresolvedMarkers, ActionEditFiles},
	{CodeRemoteBusy, ReasonRetriesExhausted, ActionRetry},
	{CodeStorageFailure, ReasonManifestRead, ActionRetry},
	{CodeStorageFailure, ReasonPackDownload, ActionRetry},
	{CodeStorageFailure, ReasonPackUpload, ActionRetry},
	{CodeStorageFailure, ReasonPublicationUnproven, ActionPull},
	{CodeStorageFailure, ReasonManifestWrite, ActionRetry},
	{CodeStorageFailure, ReasonLocalState, ActionRetry},
	{CodeStorageFailure, ReasonInternal, ActionRetry},
	{CodeStorageFailure, ReasonStorageFull, ActionOperator},
	{CodeStorageFailure, ReasonRequestLimit, ActionOperator},
	{CodeStorageFailure, ReasonRateLimited, ActionRetry},
	{CodeStorageFailure, ReasonAccessDenied, ActionOperator},
	{CodeStorageFailure, ReasonObjectTooLarge, ActionOperator},
	{CodeStorageIntegrity, ReasonManifestInvalid, ActionOperator},
	{CodeStorageIntegrity, ReasonPackInvalid, ActionOperator},
	{CodeStorageIntegrity, ReasonHistoryInvalid, ActionOperator},
	{CodeStorageIntegrity, ReasonEngineFailed, ActionOperator},
}

// TestActionForEveryReason checks actionFor for every non-recovery pairing and
// the ActionRetry fallback for an unknown one.
func TestActionForEveryReason(t *testing.T) {
	for _, tt := range allReasons {
		t.Run(string(tt.reason), func(t *testing.T) {
			got, err := actionFor(tt.code, tt.reason, nil)
			if err != nil {
				t.Fatalf("actionFor(%s, %s) unexpected error: %v", tt.code, tt.reason, err)
			}
			if got != tt.action {
				t.Fatalf("actionFor(%s, %s) = %s, want %s", tt.code, tt.reason, got, tt.action)
			}
		})
	}

	t.Run("unknown reason", func(t *testing.T) {
		got, err := actionFor(CodeInvalidRequest, Reason("NOT_A_REAL_REASON"), nil)
		if got != ActionRetry {
			t.Fatalf("actionFor(unknown) = %s, want the conservative ActionRetry", got)
		}
		if err == nil || !errors.Is(err, errUnknownActionPairing) {
			t.Fatalf("actionFor(unknown) error = %v, want a wrapped errUnknownActionPairing", err)
		}
	})

	t.Run("reason under the wrong code", func(t *testing.T) {
		// A real reason under the wrong code is an unknown pairing.
		got, err := actionFor(CodeInvalidRequest, ReasonManifestRead, nil)
		if got != ActionRetry {
			t.Fatalf("actionFor(mismatched pairing) = %s, want the conservative ActionRetry", got)
		}
		if err == nil || !errors.Is(err, errUnknownActionPairing) {
			t.Fatalf("actionFor(mismatched pairing) error = %v, want a wrapped errUnknownActionPairing", err)
		}
	})
}

// TestActionForRecoveryReport checks the recovery-report branch of actionFor.
func TestActionForRecoveryReport(t *testing.T) {
	tests := []struct {
		name   string
		report *RecoveryReport
		want   Action
	}{
		{"nil report", nil, ActionRetry},
		{"not resynchronized", &RecoveryReport{Resynchronized: false}, ActionRetry},
		{"resynchronized", &RecoveryReport{Resynchronized: true}, ActionPull},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := actionFor(CodeRecoveryFailure, ReasonLocalMutationFailed, tt.report)
			if err != nil {
				t.Fatalf("actionFor(RECOVERY_FAILURE) unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("actionFor(RECOVERY_FAILURE, %+v) = %s, want %s", tt.report, got, tt.want)
			}
		})
	}
}

// TestActionForRecoveryRefusal checks that a RECOVERY_FAILURE carrying a
// store refusal's reason takes the refusal's action, whatever the report.
func TestActionForRecoveryRefusal(t *testing.T) {
	for _, tt := range recoveryRefusals {
		t.Run(string(tt.reason), func(t *testing.T) {
			got, err := actionFor(CodeRecoveryFailure, tt.reason, &RecoveryReport{Resynchronized: true})
			if err != nil {
				t.Fatalf("actionFor(RECOVERY_FAILURE, %s) unexpected error: %v", tt.reason, err)
			}
			if got != tt.action {
				t.Fatalf("actionFor(RECOVERY_FAILURE, %s) = %s, want %s", tt.reason, got, tt.action)
			}
		})
	}
}

// TestRecoveryRefusalMessagesCoverEveryRefusal checks every refusal reason
// has a recovery message, and that none claims a publication outcome.
func TestRecoveryRefusalMessagesCoverEveryRefusal(t *testing.T) {
	for _, tt := range recoveryRefusals {
		t.Run(string(tt.reason), func(t *testing.T) {
			ne := assertErrorCode(t, recoveryFailure(RecoveryReport{}, nil, tt.sentinel), CodeRecoveryFailure)
			if ne.Reason != tt.reason {
				t.Fatalf("reason = %s, want %s", ne.Reason, tt.reason)
			}
			msg, ok := recoveryRefusalMessages[tt.reason]
			if !ok || msg == "" || !strings.HasSuffix(ne.Message, msg) {
				t.Fatalf("message = %q, want it to end with the recovery message %q", ne.Message, msg)
			}
			for _, claim := range []string{"published", "commit again"} {
				if strings.Contains(ne.Message, claim) {
					t.Fatalf("message = %q claims %q", ne.Message, claim)
				}
			}
		})
	}
	if len(recoveryRefusalMessages) != len(recoveryRefusals) {
		t.Fatalf("recovery messages = %d, want one per refusal reason (%d)", len(recoveryRefusalMessages), len(recoveryRefusals))
	}
}

// TestRecoveryFailureKeepsBothCauses checks the resynchronization failure
// joins the local cause, and that a failure other than a store refusal
// keeps LOCAL_MUTATION_FAILED.
func TestRecoveryFailureKeepsBothCauses(t *testing.T) {
	cause := errors.New("local")
	resync := errors.New("resync")
	ne := assertErrorCode(t, recoveryFailure(RecoveryReport{}, cause, resync), CodeRecoveryFailure)
	if ne.Reason != ReasonLocalMutationFailed || ne.Action != ActionRetry {
		t.Fatalf("reason/action = %s/%s, want LOCAL_MUTATION_FAILED/RETRY", ne.Reason, ne.Action)
	}
	if !errors.Is(ne, cause) || !errors.Is(ne, resync) {
		t.Fatalf("cause = %v, want both failures", ne.Cause)
	}
	if entry := assertErrorCode(t, recoveryFailure(RecoveryReport{}, nil, resync), CodeRecoveryFailure); entry.Cause != resync {
		t.Fatalf("entry cause = %v, want the resynchronization failure itself", entry.Cause)
	}
}

// TestErrorConstructorsCarryReasonAndAction checks every constructor sets Reason
// and Action.
func TestErrorConstructorsCarryReasonAndAction(t *testing.T) {
	cause := errors.New("boom")
	tests := []struct {
		name   string
		err    error
		code   Code
		reason Reason
		action Action
	}{
		{"invalidRequest no files", invalidRequest(ReasonMessageBlank, nil, nil, "blank"), CodeInvalidRequest, ReasonMessageBlank, ActionFixInput},
		{
			"invalidRequest with files", invalidRequest(ReasonInvalidContent, cause, []ErrorFile{{Path: "a.md", Reason: FileReasonInvalidContent}}, "bad content"),
			CodeInvalidRequest, ReasonInvalidContent, ActionEditFiles,
		},
		{"BuildTree failure has no known path", invalidRequest(ReasonInvalidContent, cause, nil, "visible files cannot be represented as notebook state"), CodeInvalidRequest, ReasonInvalidContent, ActionEditFiles},
		{"contentConflict", contentConflict(ReasonMergeConflict, "resolve", nil), CodeContentConflict, ReasonMergeConflict, ActionEditFiles},
		{"storageIntegrity", storageIntegrity(ReasonEngineFailed, cause, "merge failed"), CodeStorageIntegrity, ReasonEngineFailed, ActionOperator},
		{"storageFailure", storageFailure(ReasonManifestRead, cause, "read current manifest"), CodeStorageFailure, ReasonManifestRead, ActionRetry},
		{"remoteBusy", remoteBusy("exhausted"), CodeRemoteBusy, ReasonRetriesExhausted, ActionRetry},
		{"recoveryFailure resynchronized", recoveryFailure(RecoveryReport{Resynchronized: true}, cause, nil), CodeRecoveryFailure, ReasonLocalMutationFailed, ActionPull},
		{"recoveryFailure not resynchronized", recoveryFailure(RecoveryReport{Resynchronized: false}, cause, nil), CodeRecoveryFailure, ReasonLocalMutationFailed, ActionRetry},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ne *Error
			if !errors.As(tt.err, &ne) {
				t.Fatalf("constructor did not return *Error: %v", tt.err)
			}
			if ne.Code != tt.code {
				t.Fatalf("code = %s, want %s", ne.Code, tt.code)
			}
			if ne.Reason == "" {
				t.Fatal("reason must not be empty")
			}
			if ne.Reason != tt.reason {
				t.Fatalf("reason = %s, want %s", ne.Reason, tt.reason)
			}
			if ne.Action == "" {
				t.Fatal("action must not be empty")
			}
			if ne.Action != tt.action {
				t.Fatalf("action = %s, want %s", ne.Action, tt.action)
			}
		})
	}

	t.Run("BuildTree validation fails after a clean scan has empty files", func(t *testing.T) {
		var ne *Error
		err := invalidRequest(ReasonInvalidContent, cause, nil, "visible files cannot be represented as notebook state")
		if !errors.As(err, &ne) {
			t.Fatalf("constructor did not return *Error: %v", err)
		}
		if len(ne.Files) != 0 {
			t.Fatalf("files = %+v, want empty", ne.Files)
		}
	})
}

// TestContentConflictFilesClassifiesByContent checks TEXT_CONFLICT versus
// PATH_CONFLICT classification.
func TestContentConflictFilesClassifiesByContent(t *testing.T) {
	conflicts := []git.Conflict{
		{Path: "notes/a.md", Content: []byte("<<<<<<< local\na\n=======\nb\n>>>>>>> remote\n"), Ranges: []git.MarkerRange{{Start: 1, End: 5}}},
		{Path: "notes/a.md/x", Content: nil, Ranges: nil},
	}
	got := contentConflictFiles(conflicts)
	if len(got) != 2 {
		t.Fatalf("files = %+v, want 2", got)
	}
	if got[0].Reason != FileReasonTextConflict {
		t.Fatalf("file 0 reason = %s, want %s", got[0].Reason, FileReasonTextConflict)
	}
	if got[1].Reason != FileReasonPathConflict {
		t.Fatalf("file 1 reason = %s, want %s", got[1].Reason, FileReasonPathConflict)
	}
	if len(got[1].Ranges) != 0 {
		t.Fatalf("file 1 ranges = %+v, want empty", got[1].Ranges)
	}
}

// TestMapLocalErrorNamesScanFile checks the offending file is named only when
// the scan error carries a path.
func TestMapLocalErrorNamesScanFile(t *testing.T) {
	nb := &Notebook{}

	t.Run("scan error with a known path", func(t *testing.T) {
		src := &workspace.ScanError{Path: "docs/bad.md", Err: workspace.ErrInvalidContent}
		var ne *Error
		if !errors.As(nb.mapLocalError(src), &ne) {
			t.Fatal("mapLocalError did not return *Error")
		}
		if ne.Code != CodeInvalidRequest || ne.Reason != ReasonInvalidContent || ne.Action != ActionEditFiles {
			t.Fatalf("error = %+v, want INVALID_REQUEST/INVALID_CONTENT/EDIT_FILES", ne)
		}
		if len(ne.Files) != 1 || ne.Files[0].Path != "docs/bad.md" || ne.Files[0].Reason != FileReasonInvalidContent {
			t.Fatalf("files = %+v, want exactly [{docs/bad.md INVALID_CONTENT}]", ne.Files)
		}
	})

	t.Run("path collision names both paths", func(t *testing.T) {
		src := &workspace.ScanError{Path: "notes.md", Other: "Notes.md", Err: workspace.ErrPathCollision}
		var ne *Error
		if !errors.As(nb.mapLocalError(src), &ne) {
			t.Fatal("mapLocalError did not return *Error")
		}
		if ne.Code != CodeInvalidRequest || ne.Reason != ReasonInvalidContent || ne.Action != ActionEditFiles {
			t.Fatalf("error = %+v, want INVALID_REQUEST/INVALID_CONTENT/EDIT_FILES", ne)
		}
		want := []ErrorFile{
			{Path: "Notes.md", Reason: FileReasonInvalidContent},
			{Path: "notes.md", Reason: FileReasonInvalidContent},
		}
		if !reflect.DeepEqual(ne.Files, want) {
			t.Fatalf("files = %+v, want %+v", ne.Files, want)
		}
	})

	t.Run("scan rejection without a known path", func(t *testing.T) {
		var ne *Error
		if !errors.As(nb.mapLocalError(workspace.ErrInvalidContent), &ne) {
			t.Fatal("mapLocalError did not return *Error")
		}
		if ne.Reason == "" || ne.Action == "" {
			t.Fatal("reason and action must be non-empty even with no known path")
		}
		if len(ne.Files) != 0 {
			t.Fatalf("files = %+v, want empty", ne.Files)
		}
	})

	t.Run("local-state failure", func(t *testing.T) {
		var ne *Error
		if !errors.As(nb.mapLocalError(errors.New("disk full")), &ne) {
			t.Fatal("mapLocalError did not return *Error")
		}
		if ne.Code != CodeStorageFailure || ne.Reason != ReasonLocalState || ne.Action != ActionRetry {
			t.Fatalf("error = %+v, want STORAGE_FAILURE/LOCAL_STATE/RETRY", ne)
		}
	})
}

// TestNoErrorLiteralsOutsideErrorsFile checks every *Error literal lives in
// errors.go, so no call site can omit a reason.
func TestNoErrorLiteralsOutsideErrorsFile(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir(.) = %v", err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".go" || e.Name() == "errors.go" {
			continue
		}
		file, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatalf("ParseFile(%s) = %v", e.Name(), err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			u, ok := n.(*ast.UnaryExpr)
			if !ok || u.Op != token.AND {
				return true
			}
			cl, ok := u.X.(*ast.CompositeLit)
			if !ok {
				return true
			}
			id, ok := cl.Type.(*ast.Ident)
			if !ok || id.Name != "Error" {
				return true
			}
			t.Errorf("%s:%s: &Error{...} literal outside errors.go", e.Name(), fset.Position(n.Pos()))
			return true
		})
	}
}

// TestStorageFailureNamesStoreRefusals proves an account-level refusal of
// the store replaces the operation's reason and message with one that names
// the fix, keeps the cause for errors.Is, and leaves other causes alone.
func TestStorageFailureNamesStoreRefusals(t *testing.T) {
	tests := []struct {
		cause   error
		reason  Reason
		action  Action
		message string
	}{
		{storage.ErrQuotaExceeded, ReasonStorageFull, ActionOperator, "https://slivingdoc.dev"},
		{storage.ErrRequestLimit, ReasonRequestLimit, ActionOperator, "first of the month"},
		{storage.ErrRateLimited, ReasonRateLimited, ActionRetry, "wait, then retry"},
		{storage.ErrAccessDenied, ReasonAccessDenied, ActionOperator, "SLIVINGDOC_TOKEN"},
		{storage.ErrTooLarge, ReasonObjectTooLarge, ActionOperator, "larger than the storage accepts"},
		{storage.ErrTransport, ReasonPackUpload, ActionRetry, "pack upload failed"},
	}
	for _, tt := range tests {
		t.Run(string(tt.reason), func(t *testing.T) {
			cause := fmt.Errorf("httpstore: put: %w", tt.cause)
			var e *Error
			if !errors.As(storageFailure(ReasonPackUpload, cause, "pack upload failed"), &e) {
				t.Fatal("storageFailure did not build a notebook error")
			}
			if e.Code != CodeStorageFailure || e.Reason != tt.reason || e.Action != tt.action {
				t.Fatalf("error = %s/%s/%s, want %s/%s/%s", e.Code, e.Reason, e.Action, CodeStorageFailure, tt.reason, tt.action)
			}
			if !strings.Contains(e.Message, tt.message) {
				t.Fatalf("message = %q, want it to contain %q", e.Message, tt.message)
			}
			if !errors.Is(e, tt.cause) {
				t.Fatal("the error does not wrap its cause")
			}
		})
	}
}

// The store's own message is written for the person running the client, so
// it follows the notebook's message as is.
func TestStorageFailureShowsStoreMessage(t *testing.T) {
	cause := &storage.Refusal{Err: storage.ErrRequestLimit, Detail: "HTTP 507", Message: "Upgrade at https://slivingdoc.dev/billing."}
	var e *Error
	if !errors.As(storageFailure(ReasonPackUpload, fmt.Errorf("put: %w", cause), "pack upload failed"), &e) {
		t.Fatal("storageFailure did not build a notebook error")
	}
	if !strings.HasSuffix(e.Message, ". The storage says: Upgrade at https://slivingdoc.dev/billing.") {
		t.Fatalf("message = %q, want the store's message at the end", e.Message)
	}
}
