package app

import (
	"bytes"
	"context"
	"flag"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/git"
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

// TestReport proves the candid CLI result contract: the exact OK line on
// success, the structured report plus the terse category for a domain
// error, and the unchanged error for a non-domain failure.
func TestReport(t *testing.T) {
	t.Parallel()
	t.Run("success is exactly OK", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		if err := Report(&out, nil); err != nil {
			t.Fatalf("Report(nil) = %v", err)
		}
		if out.String() != "OK\n" {
			t.Fatalf("output = %q, want exactly the OK line", out.String())
		}
	})

	t.Run("domain error prints the report and returns the category", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		err := Report(&out, &notebook.Error{
			Code:    notebook.CodeContentConflict,
			Message: "resolve the conflict blocks",
			Files: []notebook.ConflictFile{
				{Path: "a.md", Ranges: []git.MarkerRange{{Start: 1, End: 5}}},
			},
		})
		if err == nil || err.Error() != "CONTENT_CONFLICT" {
			t.Fatalf("Report() = %v, want the terse category", err)
		}
		for _, want := range []string{"CONTENT_CONFLICT: resolve the conflict blocks", "retryable: false", "a.md: lines 1-5"} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("report %q does not contain %q", out.String(), want)
			}
		}
	})

	t.Run("non-domain error passes through unprinted", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		if err := Report(&out, context.Canceled); err != context.Canceled {
			t.Fatalf("Report(context.Canceled) = %v, want the unchanged error", err)
		}
		if out.Len() != 0 {
			t.Fatalf("output = %q, want none for a non-domain error", out.String())
		}
	})
}
