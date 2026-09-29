package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/notebook"
)

func TestReportStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		st   notebook.Status
		want []string
	}{
		{"not pulled", notebook.Status{}, []string{"/n  space s\n", "not pulled yet"}},
		{"clean", notebook.Status{Generation: 3, Pulled: true}, []string{"generation 3\n", "no local changes\n"}},
		{"recovery", notebook.Status{Generation: 3, Pulled: true, RecoveryRequired: true}, []string{"recovery required"}},
		{"changes", notebook.Status{Generation: 1, Pulled: true, Changes: []notebook.Change{
			{Path: "a.md", Kind: notebook.ChangeModified, Insertions: 2, Deletions: 1},
			{Path: "b.md", Kind: notebook.ChangeAdded, Insertions: 1},
		}}, []string{"  modified  a.md  +2 -1\n", "  added     b.md  +1\n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			if err := ReportStatus(&out, tt.st, nil, "/n", "space s", nil, []string{"docs"}, nil); err != nil {
				t.Fatalf("ReportStatus() = %v", err)
			}
			for _, want := range append(tt.want, "read-only: docs") {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("output = %q, want %q", out.String(), want)
				}
			}
		})
	}
}

func TestReportStatusAndLogErrors(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := ReportStatus(&out, notebook.Status{}, context.Canceled, "/n", "", nil, nil, nil); err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("ReportStatus(non-domain) = %v, want the error unchanged", err)
	}
	if err := ReportLog(&out, notebook.History{}, context.Canceled, "/n", nil); err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("ReportLog(non-domain) = %v, want the error unchanged", err)
	}
}

func TestReportLog(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := ReportLog(&out, notebook.History{}, nil, "/n", nil); err != nil || !strings.Contains(out.String(), "no publications yet") {
		t.Fatalf("empty log = %q, %v", out.String(), err)
	}
	out.Reset()
	h := notebook.History{Entries: []notebook.LogEntry{{Message: "new\n\nbody"}, {Message: "old"}}, More: true}
	if err := ReportLog(&out, h, nil, "/n", nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"  new …\n", "  old\n", "raise --limit"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("log = %q, want %q", out.String(), want)
		}
	}
}
