// Package status is the slivingdoc status command: what a notebook
// directory holds compared with the last accepted state, without changing
// anything.
package status

import (
	"context"
	"errors"
	"flag"
	"io"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/git"
)

type command struct {
	engine  git.Engine
	opts    app.ProcessOptions
	flags   *app.Flags
	flagset *flag.FlagSet
	path    string
	runtime *app.Runtime
}

// Command returns the status command over the given native engine and
// process environment.
func Command(engine git.Engine, opts app.ProcessOptions) *command {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	flags := app.NewFlags()
	flags.Bind(fs)
	return &command{engine: engine, opts: opts, flags: flags, flagset: fs}
}

func (c *command) Flagset() *flag.FlagSet { return c.flagset }

func (c *command) Describe() string { return "show what a notebook directory changed locally" }

func (c *command) Help() string { return helpText }

// Setup resolves the path argument, then the same startup as pull.
func (c *command) Setup(context.Context) error {
	path, runtime, err := app.OperationSetup("status", c.flagset, c.engine, c.flags, c.opts)
	if err != nil {
		return err
	}
	c.path = path
	c.runtime = runtime
	return nil
}

// Run reports the local state once; a domain error returns its terse category.
func (c *command) Run(ctx context.Context) error {
	if c.runtime == nil {
		return errors.New("status: Setup must run before Run")
	}
	defer c.runtime.Close()
	st, err := c.runtime.Status(ctx, c.path)
	return app.ReportStatus(c.opts.Out(), st, err, c.path, c.runtime.Target(), c.runtime.SpaceSource(), c.opts.Env, c.runtime.ReadOnlyPaths(), c.runtime.WritablePaths())
}

const helpText = `slivingdoc status - show what a notebook directory changed locally

Usage:
  slivingdoc status [flags] [path]

[path] is the notebook directory and defaults to the workspace root. Prints
the accepted generation and one line per file that differs from the accepted state
(added, modified or deleted, with its line counts). Paths that
--read-only-paths or --writable-paths protect are listed too, though a commit
refuses them. It changes no file, creates the directory when it is missing
like pull does, and reads no remote state; it opens the store only to check it, like pull. A
directory that needs recovery says so instead of listing changes, and the
next pull or commit repairs it.

A notebook directory that already pulled a hosted space remembers it, so
status there needs no --space and no --storage hosted, and it reports the
source of a space the directory remembered.

Takes the same flags as pull.

` + app.FlagReference
