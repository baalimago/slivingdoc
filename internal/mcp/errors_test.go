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
		name      string
		err       error
		wantCode  string
		wantRetry bool
		wantFiles bool
		wantReco  bool
	}{
		{name: "invalid request", err: &notebook.Error{Code: notebook.CodeInvalidRequest, Message: "commit message must not be blank"}, wantCode: codeInvalidRequest, wantRetry: false},
		{name: "content conflict", err: conflictError(), wantCode: "CONTENT_CONFLICT", wantRetry: false, wantFiles: true},
		{name: "remote busy", err: &notebook.Error{Code: notebook.CodeRemoteBusy, Message: "another writer kept winning"}, wantCode: "REMOTE_BUSY", wantRetry: true},
		{name: "storage failure", err: &notebook.Error{Code: notebook.CodeStorageFailure, Message: "pack upload failed"}, wantCode: codeStorageFailure, wantRetry: true},
		{name: "storage integrity", err: &notebook.Error{Code: notebook.CodeStorageIntegrity, Message: "corrupt pack"}, wantCode: "STORAGE_INTEGRITY", wantRetry: false},
		{name: "recovery failure", err: recoveryError(), wantCode: "RECOVERY_FAILURE", wantRetry: true, wantReco: true},
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

// TestMapErrorConflictShape proves the exact structured conflict data: the
// normalized relative path and the one-based inclusive ranges survive the
// mapping unchanged.
func TestMapErrorConflictShape(t *testing.T) {
	te, _ := MapError(conflictError())
	if len(te.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(te.Files))
	}
	first := te.Files[0]
	if first.Path != "notes/today.md" {
		t.Fatalf("path = %q, want the normalized relative path", first.Path)
	}
	if len(first.Ranges) != 2 || first.Ranges[0] != (ErrorRange{12, 18}) || first.Ranges[1] != (ErrorRange{25, 25}) {
		t.Fatalf("ranges = %+v, want [{12 18} {25 25}]", first.Ranges)
	}
	second := te.Files[1]
	if second.Path != "notes/plan.txt" || len(second.Ranges) != 0 {
		t.Fatalf("second file = %+v, want an empty ranges array", second)
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
// returns before any notebook work.
func TestMapErrorServicePath(t *testing.T) {
	for _, err := range []error{workspace.ErrInvalidPath, workspace.ErrSymlink} {
		te, domain := MapError(err)
		if !domain {
			t.Fatalf("MapError(%v) is not a domain error", err)
		}
		if te.Code != codeInvalidRequest || te.Retryable {
			t.Fatalf("MapError(%v) = %+v, want INVALID_REQUEST not retryable", err, te)
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
		Code:    notebook.CodeContentConflict,
		Message: "Resolve the conflict blocks before notes_commit.",
		Files: []notebook.ConflictFile{
			{Path: "notes/today.md", Ranges: []git.MarkerRange{{Start: 12, End: 18}, {Start: 25, End: 25}}},
			{Path: "notes/plan.txt", Ranges: nil},
		},
	}
}

func recoveryError() error {
	return &notebook.Error{
		Code:     notebook.CodeRecoveryFailure,
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
		"key AKIAIOSFODNN7EXAMPLE and endpoint http://user:secret@minio.example.com"
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

// TestToolErrorReport pins the candid CLI text form of the envelope: the
// category and message, the retryable verdict, one indented line per
// conflicted file, and the recovery report only when present.
func TestToolErrorReport(t *testing.T) {
	for _, row := range []struct {
		name string
		te   *ToolError
		want string
	}{
		{
			name: "conflict with ranges and a rangeless file",
			te: &ToolError{
				Code: "CONTENT_CONFLICT", Retryable: false,
				Message: "resolve the conflict blocks",
				Files: []ErrorFile{
					{Path: "a.md", Ranges: []ErrorRange{{Start: 1, End: 5}, {Start: 7, End: 11}}},
					{Path: "dir/b.md", Ranges: []ErrorRange{}},
				},
			},
			want: "CONTENT_CONFLICT: resolve the conflict blocks\n" +
				"retryable: false\n" +
				"  a.md: lines 1-5, 7-11\n" +
				"  dir/b.md\n",
		},
		{
			name: "recovery failure carries the recovery line",
			te: &ToolError{
				Code: "RECOVERY_FAILURE", Retryable: true,
				Message: "recovery ran",
				Files:   []ErrorFile{},
				Recovery: &RecoveryInfo{
					Stage: "publish", RemoteAccepted: "unknown", Resynchronized: true,
				},
			},
			want: "RECOVERY_FAILURE: recovery ran\n" +
				"retryable: true\n" +
				"recovery: stage=publish remoteAccepted=unknown resynchronized=true\n",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := row.te.Report(); got != row.want {
				t.Fatalf("Report() = %q, want %q", got, row.want)
			}
		})
	}
}
