package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/baalimago/slivingdoc/internal/mcp"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/pathutil"
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

// Report writes the candid result of one CLI operation to out as the
// unified status/detail/trailer skeleton shared by success and domain
// errors (architecture section 2 CLI report): a status token and summary,
// one indented line per file the result is about, and a trailer. Colour
// is presentation-only: it appears only when out is a real terminal and
// NO_COLOR is unset or empty, and the success output stays prefixed with
// the OK token for script compatibility. The returned error is nil on
// success, the terse category for a domain error — the router echoes it
// and exits nonzero — or the unchanged error when it is not a domain
// error (cancellation).
func Report(out io.Writer, result notebook.Result, err error, env []string) error {
	p := painter{on: colourEnabled(out, env)}
	if err == nil {
		writeSuccess(out, result, p)
		return nil
	}
	te, domain := mcp.MapError(err)
	if !domain {
		return err
	}
	writeError(out, te, p)
	return errors.New(te.Code)
}

// writeSuccess renders the success report: the OK status token and the
// accepted generation, one line per changed file with its insertion and
// deletion counts (a zero-count side is omitted), and the totals trailer.
func writeSuccess(out io.Writer, result notebook.Result, p painter) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n", p.green("OK"), p.cyan(fmt.Sprintf("generation %d", result.Generation)))
	for _, f := range result.Stat.Files {
		counts := make([]string, 0, 2)
		if f.Insertions > 0 {
			counts = append(counts, p.green(fmt.Sprintf("+%d", f.Insertions)))
		}
		if f.Deletions > 0 {
			counts = append(counts, p.red(fmt.Sprintf("-%d", f.Deletions)))
		}
		fmt.Fprintf(&b, "  %s  %s\n", f.Path, strings.Join(counts, " "))
	}
	fmt.Fprintf(&b, "%d files changed, %d insertions(+), %d deletions(-)\n",
		len(result.Stat.Files), result.Stat.Insertions, result.Stat.Deletions)
	io.WriteString(out, b.String())
}

// writeError renders the domain-error report: the category status token
// and the candid message, one line per conflicted file with its one-based
// inclusive marker ranges (or a bare path), and the retryable and recovery
// trailer.
func writeError(out io.Writer, te *mcp.ToolError, p painter) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n", p.red(te.Code), te.Message)
	for _, f := range te.Files {
		if len(f.Ranges) == 0 {
			fmt.Fprintf(&b, "  %s\n", p.yellow(f.Path))
			continue
		}
		parts := make([]string, 0, len(f.Ranges))
		for _, r := range f.Ranges {
			parts = append(parts, fmt.Sprintf("%d-%d", r.Start, r.End))
		}
		fmt.Fprintf(&b, "  %s: lines %s\n", p.yellow(f.Path), strings.Join(parts, ", "))
	}
	fmt.Fprintf(&b, "retryable: %t\n", te.Retryable)
	if rec := te.Recovery; rec != nil {
		fmt.Fprintf(&b, "recovery: stage=%s remoteAccepted=%s resynchronized=%t\n",
			rec.Stage, rec.RemoteAccepted, rec.Resynchronized)
	}
	io.WriteString(out, b.String())
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
