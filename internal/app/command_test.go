package app

import (
	"bytes"
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/mcp"
	"github.com/baalimago/slivingdoc/internal/notebook"
)

// parsedFlagSet mimics the command router: it binds the shared flags and
// parses the command line up to the first positional argument.
func parsedFlagSet(t *testing.T, args []string) (*Flags, *flag.FlagSet) {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	f := NewFlags()
	f.Bind(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("Parse(%v) = %v", args, err)
	}
	return f, fs
}

// TestOperationPath proves the pull/commit argument contract: at most one
// path, resolved against the working directory, with flags accepted on
// either side of it. No path is the empty string, which the runtime
// resolves to the workspace root.
func TestOperationPath(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	abs := filepath.Join(cwd, "elsewhere")
	for _, row := range []struct {
		name    string
		args    []string
		want    string
		wantErr string
	}{
		{name: "relative path resolves against cwd", args: []string{"notes"}, want: filepath.Join(cwd, "notes")},
		{name: "absolute path is cleaned", args: []string{abs + "/./sub"}, want: filepath.Join(abs, "sub")},
		{name: "flags after the path are parsed", args: []string{"notes", "--bucket", "b"}, want: filepath.Join(cwd, "notes")},
		{name: "no path defers to the workspace root", args: nil, want: ""},
		{name: "two paths", args: []string{"a", "b"}, wantErr: "at most one notebook path"},
		{name: "unknown flag after the path", args: []string{"notes", "--frobnicate"}, wantErr: "frobnicate"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			_, fs := parsedFlagSet(t, row.args)
			got, err := OperationPath(fs, cwd)
			if row.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), row.wantErr) {
					t.Fatalf("OperationPath(%v) = %q, %v; want an error containing %q", row.args, got, err, row.wantErr)
				}
				return
			}
			if err != nil || got != row.want {
				t.Fatalf("OperationPath(%v) = %q, %v; want %q", row.args, got, err, row.want)
			}
		})
	}
}

func TestOperationPathExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	_, fs := parsedFlagSet(t, []string{"~/notes"})
	got, err := OperationPath(fs, t.TempDir())
	if err != nil {
		t.Fatalf("OperationPath() = %v", err)
	}
	if want := filepath.Join(home, "notes"); got != want {
		t.Fatalf("OperationPath() = %q, want %q", got, want)
	}
}

// TestOperationPathParsesTrailingFlags proves a flag placed after the
// positional path really lands in the shared holder, which is what lets a
// human write "slivingdoc commit notes -m msg" in the documented order.
func TestOperationPathParsesTrailingFlags(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	flags, fs := parsedFlagSet(t, []string{"notes", "--bucket", "after-path"})
	if _, err := OperationPath(fs, cwd); err != nil {
		t.Fatalf("OperationPath() = %v", err)
	}
	cfg, err := flags.resolve(nil, cwd, t.TempDir(), false, nil)
	if err != nil {
		t.Fatalf("resolve() = %v", err)
	}
	if cfg.bucket != "after-path" {
		t.Fatalf("bucket = %q, want the trailing flag value", cfg.bucket)
	}
}

// successResult is the documented success summary of the worklog envelope
// example: generation 18 and the three-file diffstat, in the sorted-by-path
// order DiffSnapshots produces.
func successResult() notebook.Result {
	return notebook.Result{
		Generation: 18,
		Stat: git.DiffStat{
			Files: []git.FileStat{
				{Path: "archive/old.md", Insertions: 0, Deletions: 3},
				{Path: "notes/a.md", Insertions: 1, Deletions: 1},
				{Path: "notes/c.md", Insertions: 2, Deletions: 0},
			},
			Insertions: 3,
			Deletions:  4,
		},
	}
}

// TestReport proves the CLI result contract: success writes the unified
// OK-prefixed report and returns nil, a domain error writes the unified
// category report and returns the terse category, and a non-domain error
// passes through unprinted. The captured writers are not terminals, so
// every output here is plain text.
func TestReport(t *testing.T) {
	t.Parallel()

	t.Run("success writes the OK-prefixed report", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		if err := Report(&out, successResult(), nil, "/tmp/nb", nil, nil); err != nil {
			t.Fatalf("Report(nil) = %v", err)
		}
		want := "OK  generation 18  /tmp/nb\n" +
			"  archive/old.md  -3\n" +
			"  notes/a.md  +1 -1\n" +
			"  notes/c.md  +2\n" +
			"3 files changed, 3 insertions(+), 4 deletions(-)\n"
		if out.String() != want {
			t.Fatalf("output = %q, want %q", out.String(), want)
		}
	})

	t.Run("success trailer names the read-only set", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		if err := Report(&out, successResult(), nil, "/tmp/nb", nil, []string{"docs", "faq.md"}); err != nil {
			t.Fatalf("Report(nil) = %v", err)
		}
		want := "OK  generation 18  /tmp/nb\n" +
			"  archive/old.md  -3\n" +
			"  notes/a.md  +1 -1\n" +
			"  notes/c.md  +2\n" +
			"3 files changed, 3 insertions(+), 4 deletions(-)\n" +
			"read-only: docs, faq.md\n"
		if out.String() != want {
			t.Fatalf("output = %q, want %q", out.String(), want)
		}
	})

	t.Run("domain error writes the category report and returns the terse category", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		err := Report(&out, notebook.Result{}, &notebook.Error{
			Code:    notebook.CodeContentConflict,
			Reason:  notebook.ReasonMergeConflict,
			Action:  notebook.ActionEditFiles,
			Message: "resolve the conflict blocks",
			Files: []notebook.ErrorFile{
				{Path: "a.md", Reason: notebook.FileReasonTextConflict, Ranges: []git.MarkerRange{{Start: 1, End: 5}}},
				{Path: "dir/b.md", Reason: notebook.FileReasonPathConflict, Ranges: nil},
			},
		}, "/tmp/nb", nil, nil)
		if err == nil || err.Error() != "CONTENT_CONFLICT" {
			t.Fatalf("Report() = %v, want the terse category", err)
		}
		want := "CONTENT_CONFLICT · MERGE_CONFLICT\n" +
			"resolve the conflict blocks\n" +
			"  a.md      conflict  lines 1-5\n" +
			"  dir/b.md  path conflict\n" +
			"next: edit the files, then commit\n" +
			"retryable: false\n"
		if out.String() != want {
			t.Fatalf("output = %q, want %q", out.String(), want)
		}
	})

	t.Run("error with no files skips straight to the trailer", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		err := Report(&out, notebook.Result{}, &notebook.Error{
			Code:    notebook.CodeInvalidRequest,
			Reason:  notebook.ReasonPullRequired,
			Action:  notebook.ActionPull,
			Message: "a managed pull must run before commit",
		}, "/tmp/nb", nil, nil)
		if err == nil || err.Error() != "INVALID_REQUEST" {
			t.Fatalf("Report() = %v, want the terse category", err)
		}
		want := "INVALID_REQUEST · PULL_REQUIRED\n" +
			"a managed pull must run before commit\n" +
			"next: pull, then continue\n" +
			"retryable: false\n"
		if out.String() != want {
			t.Fatalf("output = %q, want %q", out.String(), want)
		}
	})

	t.Run("RECOVERY_FAILURE trailer order is recovery then read-only", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		err := Report(&out, notebook.Result{}, &notebook.Error{
			Code:    notebook.CodeRecoveryFailure,
			Reason:  notebook.ReasonLocalMutationFailed,
			Action:  notebook.ActionRetry,
			Message: "unexpected failure after local mutation started; recovery ran",
			Recovery: &notebook.RecoveryReport{
				Stage: "publish", RemoteAccepted: notebook.RemoteAcceptedUnknown, Resynchronized: false,
			},
		}, "/tmp/nb", nil, []string{"docs"})
		if err == nil || err.Error() != "RECOVERY_FAILURE" {
			t.Fatalf("Report() = %v, want the terse category", err)
		}
		want := "RECOVERY_FAILURE · LOCAL_MUTATION_FAILED\n" +
			"unexpected failure after local mutation started; recovery ran\n" +
			"next: retry the same call\n" +
			"retryable: true\n" +
			"recovery: stage=publish remoteAccepted=unknown resynchronized=false\n" +
			"read-only: docs\n"
		if out.String() != want {
			t.Fatalf("output = %q, want %q", out.String(), want)
		}
	})

	t.Run("non-domain error passes through unprinted", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		if err := Report(&out, notebook.Result{}, context.Canceled, "/tmp/nb", nil, nil); err != context.Canceled {
			t.Fatalf("Report(context.Canceled) = %v, want the unchanged error", err)
		}
		if out.Len() != 0 {
			t.Fatalf("output = %q, want none for a non-domain error", out.String())
		}
	})

	t.Run("piped output stays plain", func(t *testing.T) {
		t.Parallel()
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("Pipe() = %v", err)
		}
		defer r.Close()
		defer w.Close()
		got := make(chan string, 1)
		go func() {
			var b bytes.Buffer
			_, _ = io.Copy(&b, r)
			got <- b.String()
		}()
		if err := Report(w, successResult(), nil, "/tmp/nb", nil, nil); err != nil {
			t.Fatalf("Report() = %v", err)
		}
		w.Close()
		if out := <-got; strings.Contains(out, "\x1b[") || !strings.HasPrefix(out, "OK  generation ") {
			t.Fatalf("piped output = %q, want a plain OK-prefixed report", out)
		}
	})

	t.Run("NO_COLOR keeps the report plain", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		if err := Report(&out, successResult(), nil, "/tmp/nb", []string{"NO_COLOR=1"}, nil); err != nil {
			t.Fatalf("Report() = %v", err)
		}
		if strings.Contains(out.String(), "\x1b[") {
			t.Fatalf("NO_COLOR output %q contains ANSI escapes", out.String())
		}
	})
}

// TestWriteSuccessColoured proves the success report's colour placement:
// the OK token green, the generation summary cyan, insertions green,
// deletions red, a zero-count side omitted, and the read-only trailer
// label dim.
func TestWriteSuccessColoured(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	info := mcp.MapSuccess(successResult(), "/tmp/nb")
	info.ReadOnly = []string{"docs"}
	writeSuccess(&out, info, painter{on: true})
	want := "\x1b[32mOK\x1b[0m  \x1b[36mgeneration 18\x1b[0m  /tmp/nb\n" +
		"  archive/old.md  \x1b[31m-3\x1b[0m\n" +
		"  notes/a.md  \x1b[32m+1\x1b[0m \x1b[31m-1\x1b[0m\n" +
		"  notes/c.md  \x1b[32m+2\x1b[0m\n" +
		"3 files changed, 3 insertions(+), 4 deletions(-)\n" +
		"\x1b[2mread-only:\x1b[0m docs\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

// TestWriteSuccessEmptyStat proves the no-op synchronization report: an
// empty diffstat renders only the status line and the zero totals trailer.
func TestWriteSuccessEmptyStat(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	writeSuccess(&out, mcp.MapSuccess(notebook.Result{Generation: 7}, "/tmp/nb"), painter{})
	want := "OK  generation 7  /tmp/nb\n" +
		"0 files changed, 0 insertions(+), 0 deletions(-)\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

// TestWriteSuccessReadOnlyTrailer checks the trailer appears only for a
// non-empty set.
func TestWriteSuccessReadOnlyTrailer(t *testing.T) {
	t.Parallel()
	t.Run("empty set adds no trailer", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		writeSuccess(&out, mcp.MapSuccess(notebook.Result{Generation: 1}, "/tmp/nb"), painter{})
		want := "OK  generation 1  /tmp/nb\n0 files changed, 0 insertions(+), 0 deletions(-)\n"
		if out.String() != want {
			t.Fatalf("output = %q, want %q", out.String(), want)
		}
	})
	t.Run("non-empty set trails the totals line", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		info := mcp.MapSuccess(notebook.Result{Generation: 1}, "/tmp/nb")
		info.ReadOnly = []string{"docs", "faq.md"}
		writeSuccess(&out, info, painter{})
		want := "OK  generation 1  /tmp/nb\n0 files changed, 0 insertions(+), 0 deletions(-)\nread-only: docs, faq.md\n"
		if out.String() != want {
			t.Fatalf("output = %q, want %q", out.String(), want)
		}
	})
}

// TestWriteErrorColoured proves the error report's colour placement: the
// code red, the reason token dim, conflict paths yellow, the file reason
// word dim, the next-step label cyan, and the read-only trailer label dim.
func TestWriteErrorColoured(t *testing.T) {
	t.Parallel()
	te := &mcp.ToolError{
		Code: "RECOVERY_FAILURE", Reason: "LOCAL_MUTATION_FAILED", Action: "PULL", Retryable: true,
		Message: "unexpected failure after local mutation started; recovery ran",
		Files: []mcp.ErrorFile{
			{Path: "a.md", Reason: "TEXT_CONFLICT", Ranges: []mcp.ErrorRange{{Start: 1, End: 5}}},
			{Path: "dir/b.md", Reason: "PATH_CONFLICT", Ranges: []mcp.ErrorRange{}},
		},
		Recovery: &mcp.RecoveryInfo{Stage: "publish", RemoteAccepted: "unknown", Resynchronized: true},
		ReadOnly: []string{"docs", "faq.md"},
	}
	var out bytes.Buffer
	writeError(&out, te, painter{on: true})
	want := "\x1b[31mRECOVERY_FAILURE\x1b[0m · \x1b[2mLOCAL_MUTATION_FAILED\x1b[0m\n" +
		"unexpected failure after local mutation started; recovery ran\n" +
		"  \x1b[33ma.md\x1b[0m      \x1b[2mconflict\x1b[0m  lines 1-5\n" +
		"  \x1b[33mdir/b.md\x1b[0m  \x1b[2mpath conflict\x1b[0m\n" +
		"\x1b[36mnext:\x1b[0m pull, then continue\n" +
		"retryable: true\n" +
		"recovery: stage=publish remoteAccepted=unknown resynchronized=true\n" +
		"\x1b[2mread-only:\x1b[0m docs, faq.md\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

// TestWriteErrorReadOnly checks the plain read-only refusal report byte for
// byte (architecture section 2).
func TestWriteErrorReadOnly(t *testing.T) {
	t.Parallel()
	te := &mcp.ToolError{
		Code: "INVALID_REQUEST", Reason: "READ_ONLY_PATH", Action: "EDIT_FILES", Retryable: false,
		Message: "docs is read-only in this server. Your changes there were discarded and the files reset. " +
			"Write outside the read-only paths, then commit again.",
		Files:    []mcp.ErrorFile{{Path: "docs/faq.md", Reason: "READ_ONLY"}},
		ReadOnly: []string{"docs"},
	}
	var out bytes.Buffer
	writeError(&out, te, painter{})
	want := "INVALID_REQUEST · READ_ONLY_PATH\n" +
		"docs is read-only in this server. Your changes there were discarded and the files reset. " +
		"Write outside the read-only paths, then commit again.\n" +
		"  docs/faq.md  read-only\n" +
		"next: edit the files, then commit\n" +
		"retryable: false\n" +
		"read-only: docs\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

// TestWriteErrorAlignsPathColumn checks the reason column is padded to the
// longest path measured in runes.
func TestWriteErrorAlignsPathColumn(t *testing.T) {
	t.Parallel()
	te := &mcp.ToolError{
		Code: "CONTENT_CONFLICT", Reason: "MERGE_CONFLICT", Action: "EDIT_FILES",
		Message: "Resolve the conflict blocks before notes_commit.",
		Files: []mcp.ErrorFile{
			{Path: "notes/today.md", Reason: "TEXT_CONFLICT", Ranges: []mcp.ErrorRange{{Start: 12, End: 18}, {Start: 40, End: 42}}},
			{Path: "notes/plan.md", Reason: "PATH_CONFLICT"},
			{Path: "docs/résumé.md", Reason: "TEXT_CONFLICT"},
		},
	}
	var out bytes.Buffer
	writeError(&out, te, painter{})
	want := "CONTENT_CONFLICT · MERGE_CONFLICT\n" +
		"Resolve the conflict blocks before notes_commit.\n" +
		"  notes/today.md  conflict  lines 12-18, 40-42\n" +
		"  notes/plan.md   path conflict\n" +
		"  docs/résumé.md  conflict\n" +
		"next: edit the files, then commit\n" +
		"retryable: false\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

// TestFileReasonWords checks every FileReason wording and the verbatim
// fallback.
func TestFileReasonWords(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		reason string
		want   string
	}{
		{"TEXT_CONFLICT", "conflict"},
		{"PATH_CONFLICT", "path conflict"},
		{"UNRESOLVED_MARKERS", "unresolved markers"},
		{"READ_ONLY", "read-only"},
		{"INVALID_CONTENT", "invalid content"},
		{"SOME_UNKNOWN_TOKEN", "SOME_UNKNOWN_TOKEN"},
	} {
		t.Run(row.reason, func(t *testing.T) {
			t.Parallel()
			if got := fileReasonWord(row.reason); got != row.want {
				t.Fatalf("fileReasonWord(%q) = %q, want %q", row.reason, got, row.want)
			}
		})
	}
}

// TestActionWording checks every Action wording and the verbatim fallback.
func TestActionWording(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		action string
		want   string
	}{
		{"FIX_INPUT", "correct the request, then call again"},
		{"EDIT_FILES", "edit the files, then commit"},
		{"PULL", "pull, then continue"},
		{"RETRY", "retry the same call"},
		{"OPERATOR", "operator attention needed"},
		{"SOME_UNKNOWN_TOKEN", "SOME_UNKNOWN_TOKEN"},
	} {
		t.Run(row.action, func(t *testing.T) {
			t.Parallel()
			if got := actionWording(row.action); got != row.want {
				t.Fatalf("actionWording(%q) = %q, want %q", row.action, got, row.want)
			}
		})
	}
}
