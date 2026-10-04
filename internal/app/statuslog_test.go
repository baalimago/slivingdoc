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
			if err := ReportStatus(&out, tt.st, nil, "/n", "space s", "--space", nil, []string{"docs"}, nil); err != nil {
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
	if err := ReportStatus(&out, notebook.Status{}, context.Canceled, "/n", "", "", nil, nil, nil); err == nil || !errors.Is(err, context.Canceled) {
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
	h := notebook.History{Entries: []notebook.LogEntry{{Message: "new\n\nbody"}, {Message: "old\x1b[2J\rspoof"}}, More: true}
	if err := ReportLog(&out, h, nil, "/n", nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"  new …\n", "  old [2J spoof\n", "raise --limit"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("log = %q, want %q", out.String(), want)
		}
	}
}

// TestStatusNamesRememberedSpaceSource proves the one source the status
// report explains: a space the directory itself remembered gets a trailer
// naming where it came from, so an operator who moved a directory reads why
// it talks to that space.
func TestStatusNamesRememberedSpaceSource(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := ReportStatus(&out, notebook.Status{Generation: 2, Pulled: true}, nil, "/n", "space notes", rememberedSource, nil, nil, nil); err != nil {
		t.Fatalf("ReportStatus() = %v", err)
	}
	want := "/n  space notes\nspace: " + rememberedSource + "\ngeneration 2\n"
	if !strings.HasPrefix(out.String(), want) {
		t.Fatalf("status = %q, want it to start with %q", out.String(), want)
	}
}

// TestStatusOmitsSourceForExplicitChoices proves every other source keeps
// today's report: a source the operator set by hand needs no explanation,
// and an S3 process that named no bucket has none to name.
func TestStatusOmitsSourceForExplicitChoices(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"--space", "--bucket", bucketEnv, spaceEnv, "default space", "token", bucketNone.String()} {
		var out bytes.Buffer
		if err := ReportStatus(&out, notebook.Status{Generation: 1, Pulled: true}, nil, "/n", "bucket b", source, nil, nil, nil); err != nil {
			t.Fatalf("ReportStatus(%s) = %v", source, err)
		}
		if strings.Contains(out.String(), "space: ") {
			t.Fatalf("status with the %s source = %q, want no source trailer", source, out.String())
		}
	}
}

// TestStatusTrailerIsPlainOffATTY proves the trailer obeys the colour rule
// of every other line of the report: a pipe sees the words alone, so no
// escape sequence reaches a script. The style is chosen once from the
// stream, so a buffer and a NO_COLOR environment render plain alike.
func TestStatusTrailerIsPlainOffATTY(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := ReportStatus(&out, notebook.Status{}, nil, "/n", "space notes", rememberedSource, []string{"NO_COLOR=1"}, nil, nil); err != nil {
		t.Fatalf("ReportStatus() = %v", err)
	}
	got := out.String()
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("status on a plain stream = %q, want no ANSI escape", got)
	}
	if !strings.Contains(got, "space: "+rememberedSource+"\n") {
		t.Fatalf("status on a plain stream = %q, want the trailer in plain words", got)
	}
}

// TestLogNeverNamesTheAssociation proves log names no source: its report
// has no line that could carry one, so neither the space nor the store
// reaches it.
func TestLogNeverNamesTheAssociation(t *testing.T) {
	t.Parallel()
	for _, h := range []notebook.History{
		{},
		{Entries: []notebook.LogEntry{{Message: "the first note"}}, More: true},
	} {
		var out bytes.Buffer
		if err := ReportLog(&out, h, nil, "/n", nil); err != nil {
			t.Fatalf("ReportLog() = %v", err)
		}
		if strings.Contains(out.String(), "space") {
			t.Fatalf("log = %q, want no space named", out.String())
		}
	}
}
