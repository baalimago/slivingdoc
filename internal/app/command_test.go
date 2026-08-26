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

// TestOperationPath proves the pull/commit argument contract: exactly one
// path, resolved against the working directory, with flags accepted on
// either side of it.
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
		{name: "no path", args: nil, wantErr: "exactly one notebook path"},
		{name: "two paths", args: []string{"a", "b"}, wantErr: "exactly one notebook path"},
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
	cfg, err := flags.resolve(nil, cwd, t.TempDir())
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
		if err := Report(&out, successResult(), nil, nil); err != nil {
			t.Fatalf("Report(nil) = %v", err)
		}
		want := "OK  generation 18\n" +
			"  archive/old.md  -3\n" +
			"  notes/a.md  +1 -1\n" +
			"  notes/c.md  +2\n" +
			"3 files changed, 3 insertions(+), 4 deletions(-)\n"
		if out.String() != want {
			t.Fatalf("output = %q, want %q", out.String(), want)
		}
	})

	t.Run("domain error writes the category report and returns the terse category", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		err := Report(&out, notebook.Result{}, &notebook.Error{
			Code:    notebook.CodeContentConflict,
			Message: "resolve the conflict blocks",
			Files: []notebook.ConflictFile{
				{Path: "a.md", Ranges: []git.MarkerRange{{Start: 1, End: 5}}},
				{Path: "dir/b.md", Ranges: nil},
			},
		}, nil)
		if err == nil || err.Error() != "CONTENT_CONFLICT" {
			t.Fatalf("Report() = %v, want the terse category", err)
		}
		want := "CONTENT_CONFLICT  resolve the conflict blocks\n" +
			"  a.md: lines 1-5\n" +
			"  dir/b.md\n" +
			"retryable: false\n"
		if out.String() != want {
			t.Fatalf("output = %q, want %q", out.String(), want)
		}
	})

	t.Run("non-domain error passes through unprinted", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		if err := Report(&out, notebook.Result{}, context.Canceled, nil); err != context.Canceled {
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
		if err := Report(w, successResult(), nil, nil); err != nil {
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
		if err := Report(&out, successResult(), nil, []string{"NO_COLOR=1"}); err != nil {
			t.Fatalf("Report() = %v", err)
		}
		if strings.Contains(out.String(), "\x1b[") {
			t.Fatalf("NO_COLOR output %q contains ANSI escapes", out.String())
		}
	})
}

// TestWriteSuccessColoured proves the success report's colour placement:
// the OK token green, the generation summary cyan, insertions green,
// deletions red, and a zero-count side omitted.
func TestWriteSuccessColoured(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	writeSuccess(&out, successResult(), painter{on: true})
	want := "\x1b[32mOK\x1b[0m  \x1b[36mgeneration 18\x1b[0m\n" +
		"  archive/old.md  \x1b[31m-3\x1b[0m\n" +
		"  notes/a.md  \x1b[32m+1\x1b[0m \x1b[31m-1\x1b[0m\n" +
		"  notes/c.md  \x1b[32m+2\x1b[0m\n" +
		"3 files changed, 3 insertions(+), 4 deletions(-)\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

// TestWriteSuccessEmptyStat proves the no-op synchronization report: an
// empty diffstat renders only the status line and the zero totals trailer.
func TestWriteSuccessEmptyStat(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	writeSuccess(&out, notebook.Result{Generation: 7}, painter{})
	want := "OK  generation 7\n" +
		"0 files changed, 0 insertions(+), 0 deletions(-)\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

// TestWriteErrorColoured proves the error report's colour placement: the
// category token red, conflict paths yellow, and the recovery trailer
// when present.
func TestWriteErrorColoured(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	te := &mcp.ToolError{
		Code: "RECOVERY_FAILURE", Retryable: true,
		Message: "unexpected failure after local mutation started; recovery ran",
		Files: []mcp.ErrorFile{
			{Path: "a.md", Ranges: []mcp.ErrorRange{{Start: 1, End: 5}}},
			{Path: "dir/b.md", Ranges: []mcp.ErrorRange{}},
		},
		Recovery: &mcp.RecoveryInfo{Stage: "publish", RemoteAccepted: "unknown", Resynchronized: true},
	}
	writeError(&out, te, painter{on: true})
	want := "\x1b[31mRECOVERY_FAILURE\x1b[0m  unexpected failure after local mutation started; recovery ran\n" +
		"  \x1b[33ma.md\x1b[0m: lines 1-5\n" +
		"  \x1b[33mdir/b.md\x1b[0m\n" +
		"retryable: true\n" +
		"recovery: stage=publish remoteAccepted=unknown resynchronized=true\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}
