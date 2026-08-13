package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/baalimago/slivingdoc/internal/mcp"
)

// OperationPath extracts the one positional notebook path of a pull or
// commit command line and resolves it against the working directory. The
// command router parses flags up to the first positional argument only, so
// flags that follow the path are parsed here before the count is judged.
func OperationPath(fs *flag.FlagSet, cwd string) (string, error) {
	positionals, err := positionals(fs)
	if err != nil {
		return "", err
	}
	if len(positionals) != 1 {
		return "", fmt.Errorf("exactly one notebook path argument is required, got %d", len(positionals))
	}
	return resolvePath(cwd, positionals[0])
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

// resolvePath makes the requested notebook path absolute: an absolute path
// is cleaned, a relative one joins the working directory.
func resolvePath(cwd, path string) (string, error) {
	if path == "" {
		return "", errors.New("the notebook path must not be empty")
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

// Report writes the candid result of one CLI operation to out: the exact
// OK line on success, or the structured domain report (category, retryable
// verdict, conflicted files with line ranges, recovery when present). The
// returned error is nil on success, the terse category for a domain error
// — the router echoes it and exits nonzero — or the unchanged error when
// it is not a domain error (cancellation).
func Report(out io.Writer, err error) error {
	if err == nil {
		fmt.Fprintln(out, "OK")
		return nil
	}
	te, domain := mcp.MapError(err)
	if !domain {
		return err
	}
	fmt.Fprint(out, te.Report())
	return errors.New(te.Code)
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
