package app

import (
	"fmt"
	"io"
	"strings"

	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/tui"
)

// ReportStatus prints the status of the notebook at path and returns nil, or
// renders a domain error like Report does.
func ReportStatus(out io.Writer, st notebook.Status, err error, path, target string, env []string, readOnly, writable []string) error {
	p := tui.Detect(out, EnvLookup(env))
	if err != nil {
		return reportError(out, err, p, readOnly, writable)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s  %s\n", p.Mark(tui.Done), p.Brand(path), p.Dim(target))
	switch {
	case st.Generation == 0 && !st.Pulled:
		b.WriteString("not pulled yet: run 'slivingdoc pull' first\n")
	default:
		fmt.Fprintf(&b, "generation %d\n", st.Generation)
	}
	if st.RecoveryRequired {
		b.WriteString(p.Warn("recovery required: the next pull or commit rewrites the directory to the accepted state") + "\n")
		writePathSets(&b, writable, readOnly, p)
		io.WriteString(out, b.String())
		return nil
	}
	if len(st.Changes) == 0 {
		b.WriteString(p.Dim("no local changes") + "\n")
	}
	width := 0
	for _, c := range st.Changes {
		width = max(width, len(c.Kind))
	}
	for _, c := range st.Changes {
		counts := make([]string, 0, 2)
		if c.Insertions > 0 {
			counts = append(counts, p.Good(fmt.Sprintf("+%d", c.Insertions)))
		}
		if c.Deletions > 0 {
			counts = append(counts, p.Bad(fmt.Sprintf("-%d", c.Deletions)))
		}
		fmt.Fprintf(&b, "  %-*s  %s  %s\n", width, c.Kind, c.Path, strings.Join(counts, " "))
	}
	writePathSets(&b, writable, readOnly, p)
	io.WriteString(out, b.String())
	return nil
}

// ReportLog prints the recent publications, newest first, one line each.
func ReportLog(out io.Writer, h notebook.History, err error, path string, env []string) error {
	p := tui.Detect(out, EnvLookup(env))
	if err != nil {
		return reportError(out, err, p, nil, nil)
	}
	var b strings.Builder
	if len(h.Entries) == 0 {
		fmt.Fprintf(&b, "%s%s\n", p.Mark(tui.Done), p.Dim("no publications yet at "+path+": run 'slivingdoc pull' first"))
		io.WriteString(out, b.String())
		return nil
	}
	fmt.Fprintf(&b, "%s%s\n", p.Mark(tui.Done), p.Brand(path))
	for _, e := range h.Entries {
		first, _, multi := strings.Cut(strings.TrimSpace(e.Message), "\n")
		if multi {
			first += " …"
		}
		fmt.Fprintf(&b, "  %s\n", first)
	}
	if h.More {
		b.WriteString(p.Dim("older publications exist: raise --limit to see more") + "\n")
	}
	io.WriteString(out, b.String())
	return nil
}
