package notebook

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// allReasons lists every Reason token under its owning code (architecture section 2).
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
	{CodeInvalidRequest, ReasonInvalidContent, ActionEditFiles},
	{CodeInvalidRequest, ReasonReadOnlyPath, ActionEditFiles},
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
		{"recoveryFailure resynchronized", recoveryFailure(RecoveryReport{Resynchronized: true}, cause), CodeRecoveryFailure, ReasonLocalMutationFailed, ActionPull},
		{"recoveryFailure not resynchronized", recoveryFailure(RecoveryReport{Resynchronized: false}, cause), CodeRecoveryFailure, ReasonLocalMutationFailed, ActionRetry},
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
