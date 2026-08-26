// Package commit is the slivingdoc commit command: one direct human-facing
// invocation of the notes_commit operation. It resolves the same
// configuration as serve, performs the same startup refusals (pinned
// engine, S3 compatibility probe), publishes the caller's changes at the
// requested directory, and prints the candid result.
package commit

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/git"
)

type command struct {
	engine     git.Engine
	opts       app.ProcessOptions
	flags      *app.Flags
	flagset    *flag.FlagSet
	path       string
	message    string
	messageSet bool
	runtime    *app.Runtime
}

// Command returns the commit command over the given native engine and
// process environment. Nil option fields take the production defaults.
func Command(engine git.Engine, opts app.ProcessOptions) *command {
	fs := flag.NewFlagSet("commit", flag.ContinueOnError)
	// Diagnostics are formatted by this process, not by the flag package.
	fs.SetOutput(io.Discard)
	flags := app.NewFlags()
	flags.Bind(fs)
	c := &command{engine: engine, opts: opts, flags: flags, flagset: fs}
	setMessage := func(s string) error {
		c.message = s
		c.messageSet = true
		return nil
	}
	fs.Func("m", "commit message (required)", setMessage)
	fs.Func("message", "commit message (required)", setMessage)
	return c
}

func (c *command) Flagset() *flag.FlagSet { return c.flagset }

func (c *command) Describe() string {
	return "publish the changes at a notebook directory"
}

func (c *command) Help() string { return helpText }

// Setup resolves the notebook path and message arguments, then performs
// the same startup refusal surface as serve: configuration resolution, the
// pinned-version engine check, and the S3 compatibility probe. The
// argument refusals come first, so a bad command line touches no
// dependency.
func (c *command) Setup(context.Context) error {
	path, err := app.OperationPath(c.flagset, c.opts.Cwd)
	if err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	c.path = path
	if !c.messageSet {
		return errors.New("commit: a message is required: -m <message>")
	}
	runtime, err := app.Setup(c.engine, c.flags, c.opts)
	if err != nil {
		return err
	}
	c.runtime = runtime
	return nil
}

// Run commits the notebook once and prints the unified result report: the
// OK-prefixed success report with the generation and the published-increment
// diffstat on success, the coloured category report otherwise. A domain
// error returns its terse category, so the router exits nonzero.
func (c *command) Run(ctx context.Context) error {
	if c.runtime == nil {
		return errors.New("commit: Setup must run before Run")
	}
	defer c.runtime.Close()
	result, err := c.runtime.Commit(ctx, c.path, c.message)
	return app.Report(c.opts.Out(), result, err, c.opts.Env)
}

const helpText = `slivingdoc commit - publish the changes at a notebook directory

Usage:
  slivingdoc commit [flags] -m <message> [path]

[path] is the notebook directory of an earlier 'slivingdoc pull' and
defaults to the workspace root, which is the working directory unless
--workspace-root says otherwise. A relative path resolves against the
working directory; the resolved path must stay at or below the workspace
root. Concurrent non-conflicting
changes are incorporated; a conflict rewrites the files with visible
markers and reports every conflicted range.

  -m, --message string          commit message (required): non-blank UTF-8
                                without U+0000, at most 16,384 bytes

Prints the unified result report on stdout. Success shows the OK status
token, the accepted remote generation, one line per changed file with its
insertion and deletion counts, and the totals trailer, and exits zero. A
domain error shows the category and message, the conflicted files with
their one-based inclusive line ranges, and the retryable verdict, and
exits nonzero. Colour is presentation-only: it appears only on a real
terminal, and any non-empty NO_COLOR disables it.

Flags take precedence over environment variables, which override defaults.
An explicitly empty flag value does not fall back to an environment value.

` + app.FlagReference
