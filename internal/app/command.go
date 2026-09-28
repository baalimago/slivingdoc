package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/baalimago/slivingdoc/internal/mcp"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/pathutil"
	"github.com/baalimago/slivingdoc/internal/tui"
)

// OperationPath extracts the optional positional notebook path of a pull or
// commit command line and resolves it against the working directory. The
// command router parses flags up to the first positional argument only, so
// flags that follow the path are parsed here before the count is judged. No
// positional argument returns the empty string, which the runtime resolves
// to the workspace root.
func OperationPath(fs *flag.FlagSet, cwd string) (string, error) {
	positionals, err := positionals(fs)
	if err != nil {
		return "", err
	}
	switch len(positionals) {
	case 0:
		return "", nil
	case 1:
		return resolvePath(cwd, positionals[0])
	default:
		return "", fmt.Errorf("at most one notebook path argument is accepted, got %d", len(positionals))
	}
}

// positionals returns the positional arguments remaining on an
// already-parsed flag set, parsing any flag runs interleaved after them.
// The "--" terminator ends flag parsing, matching the flag package.
func positionals(fs *flag.FlagSet) ([]string, error) {
	var out []string
	rest := fs.Args()
	for len(rest) > 0 {
		arg := rest[0]
		if arg == "--" {
			return append(out, rest[1:]...), nil
		}
		if len(arg) > 1 && arg[0] == '-' {
			if err := fs.Parse(rest); err != nil {
				return nil, err
			}
			rest = fs.Args()
			continue
		}
		out = append(out, arg)
		rest = rest[1:]
	}
	return out, nil
}

// resolvePath makes the requested notebook path absolute: a leading home
// abbreviation expands first, an absolute path is cleaned, and a relative
// one joins the working directory.
func resolvePath(cwd, path string) (string, error) {
	if path == "" {
		return "", errors.New("the notebook path must not be empty")
	}
	var err error
	if path, err = pathutil.ExpandHome(path); err != nil {
		return "", err
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve the working directory for a relative path: %w", err)
		}
	}
	return filepath.Join(cwd, path), nil
}

// statusSeparator sits between the code and the reason token on the CLI
// status line (architecture/product-contract.md, CLI report).
const statusSeparator = " · "

// fileReasonWords is the CLI wording of each notebook.FileReason token.
var fileReasonWords = map[string]string{
	"TEXT_CONFLICT":      "conflict",
	"PATH_CONFLICT":      "path conflict",
	"UNRESOLVED_MARKERS": "unresolved markers",
	"READ_ONLY":          "read-only",
	"INVALID_CONTENT":    "invalid content",
	// The first-pull refusal (DIRECTORY_NOT_EMPTY).
	"NOT_IN_NOTEBOOK":       "not in the notebook",
	"DIFFERS_FROM_NOTEBOOK": "differs from the notebook",
}

// actionWordings is the CLI wording of each notebook.Action token.
var actionWordings = map[string]string{
	"FIX_INPUT":  "correct the request, then call again",
	"EDIT_FILES": "edit the files, then commit",
	"PULL":       "pull, then continue",
	"RETRY":      "retry the same call",
	"OPERATOR":   "operator attention needed",
}

// reasonNextSteps overrides the action's next-step wording for a reason
// whose action wording would mislead: a refused first pull needs the
// directory changed, not the request.
var reasonNextSteps = map[string]string{
	"DIRECTORY_NOT_EMPTY": "pull into an empty directory, or move those files away, then pull again",
}

// nextStep renders the next-step line of a domain error: the reason's own
// wording when it has one, else the action's.
func nextStep(te *mcp.ToolError) string {
	if step, ok := reasonNextSteps[te.Reason]; ok {
		return step
	}
	return actionWording(te.Action)
}

// fileReasonWord renders a file reason token, verbatim when unknown.
func fileReasonWord(reason string) string {
	if word, ok := fileReasonWords[reason]; ok {
		return word
	}
	return reason
}

// actionWording renders an action token, verbatim when unknown.
func actionWording(action string) string {
	if word, ok := actionWordings[action]; ok {
		return word
	}
	return action
}

// Report writes the candid result of one CLI operation to out as the
// unified status/detail/trailer skeleton shared by success and domain
// errors (architecture/product-contract.md, CLI report): a status token and
// summary, one indented line per file the result is about, and a trailer.
// path is the resolved notebook directory the success line reports.
// readOnly and writable are attached to the envelope exactly as the MCP
// handler does. Colour is presentation-only: it appears only when out is a
// real terminal and NO_COLOR is unset or empty, and the success output
// stays prefixed with the OK token for script compatibility. The returned
// error is nil on success, the terse category for a domain error — the
// router echoes it and exits nonzero — or the unchanged error when it is
// not a domain error (cancellation).
func Report(out io.Writer, result notebook.Result, err error, path string, env []string, readOnly, writable []string) error {
	p := tui.Detect(out, EnvLookup(env))
	if err == nil {
		info := mcp.MapSuccess(result, path)
		info.ReadOnly = readOnly
		info.Writable = writable
		writeSuccess(out, info, p)
		return nil
	}
	te, domain := mcp.MapError(err)
	if !domain {
		return err
	}
	te.ReadOnly = readOnly
	te.Writable = writable
	writeError(out, te, p)
	return errors.New(te.Code)
}

// writeSuccess renders the success report: the OK status token, the
// accepted generation, and the resolved notebook directory, one line per
// changed file with its insertion and deletion counts (a zero-count side
// is omitted), the totals trailer, and the writable and read-only trailers
// when those sets are non-empty.
func writeSuccess(out io.Writer, info *mcp.SuccessInfo, p tui.Style) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s  %s  %s\n", p.Mark(tui.Done), p.Good("OK"), p.Brand(fmt.Sprintf("generation %d", info.Generation)), info.Path)
	for _, f := range info.Files {
		counts := make([]string, 0, 2)
		if f.Insertions > 0 {
			counts = append(counts, p.Good(fmt.Sprintf("+%d", f.Insertions)))
		}
		if f.Deletions > 0 {
			counts = append(counts, p.Bad(fmt.Sprintf("-%d", f.Deletions)))
		}
		fmt.Fprintf(&b, "  %s  %s\n", f.Path, strings.Join(counts, " "))
	}
	b.WriteString(p.Dim(fmt.Sprintf("%d files changed, %d insertions(+), %d deletions(-)",
		info.FilesChanged, info.Insertions, info.Deletions)) + "\n")
	writePathSets(&b, info.Writable, info.ReadOnly, p)
	io.WriteString(out, b.String())
}

// writeError renders the domain-error report
// (architecture/product-contract.md, CLI report): status line, message, one
// aligned line per file, then the next, retryable, recovery, writable, and
// read-only trailers.
func writeError(out io.Writer, te *mcp.ToolError, p tui.Style) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s%s%s\n", p.Mark(tui.Failed), p.Bad(te.Code), statusSeparator, p.Dim(te.Reason))
	fmt.Fprintf(&b, "%s\n", te.Message)
	width := longestErrorFilePath(te.Files) + 2
	for _, f := range te.Files {
		fmt.Fprintf(&b, "  %s%s%s", p.Warn(f.Path), strings.Repeat(" ", width-utf8.RuneCountInString(f.Path)), p.Dim(fileReasonWord(f.Reason)))
		if len(f.Ranges) > 0 {
			parts := make([]string, 0, len(f.Ranges))
			for _, r := range f.Ranges {
				parts = append(parts, fmt.Sprintf("%d-%d", r.Start, r.End))
			}
			fmt.Fprintf(&b, "  lines %s", strings.Join(parts, ", "))
		}
		b.WriteByte('\n')
	}
	// On a terminal the next step leads with the arrow instead of its
	// label; the plain report keeps "next:" for scripts.
	next := "next: "
	if p.Mode() == tui.Styled {
		next = p.Mark(tui.Next)
	}
	fmt.Fprintf(&b, "%s%s\n", next, nextStep(te))
	b.WriteString(p.Dim(fmt.Sprintf("retryable: %t", te.Retryable)) + "\n")
	if rec := te.Recovery; rec != nil {
		b.WriteString(p.Dim(fmt.Sprintf("recovery: stage=%s remoteAccepted=%s resynchronized=%t",
			rec.Stage, rec.RemoteAccepted, rec.Resynchronized)) + "\n")
	}
	writePathSets(&b, te.Writable, te.ReadOnly, p)
	io.WriteString(out, b.String())
}

// writePathSets renders the trailer of each configured path set, writable
// first, so an operator reads where the process may write before the
// exceptions inside that region. An empty set has no trailer. Two
// non-empty sets may name the same region at different depths, so they are
// followed by the rule that reconciles them
// (architecture/product-contract.md, Read-only and writable paths).
func writePathSets(b *strings.Builder, writable, readOnly []string, p tui.Style) {
	if len(writable) > 0 {
		fmt.Fprintf(b, "%s %s\n", p.Dim("writable:"), strings.Join(writable, notebook.ReadOnlyListSeparator))
	}
	if len(readOnly) > 0 {
		fmt.Fprintf(b, "%s %s\n", p.Dim("read-only:"), strings.Join(readOnly, notebook.ReadOnlyListSeparator))
	}
	if len(writable) > 0 && len(readOnly) > 0 {
		fmt.Fprintf(b, "%s %s\n", p.Dim("path-rule:"), notebook.PathSetsNestRule)
	}
}

// longestErrorFilePath is the widest file path in runes, for column
// alignment.
func longestErrorFilePath(files []mcp.ErrorFile) int {
	longest := 0
	for _, f := range files {
		if n := utf8.RuneCountInString(f.Path); n > longest {
			longest = n
		}
	}
	return longest
}

// Out is the command-output writer of the options: Stdout, or the process
// stdout when unset. Setup defaults an unset Stdout to io.Discard because
// the MCP transport owns the real stream; command output must not inherit
// that default.
func (o ProcessOptions) Out() io.Writer {
	if o.Stdout != nil {
		return o.Stdout
	}
	return os.Stdout
}
