// Package log is the slivingdoc log command: the recent publications a
// notebook directory has accepted.
package log

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/git"
)

// DefaultLimit is how many publications log shows unless --limit says otherwise.
const DefaultLimit = 20

type command struct {
	engine  git.Engine
	opts    app.ProcessOptions
	flags   *app.Flags
	flagset *flag.FlagSet
	limit   int
	path    string
	runtime *app.Runtime
}

// Command returns the log command over the given native engine and
// process environment.
func Command(engine git.Engine, opts app.ProcessOptions) *command {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	flags := app.NewFlags()
	flags.Bind(fs)
	c := &command{engine: engine, opts: opts, flags: flags, flagset: fs}
	fs.IntVar(&c.limit, "limit", DefaultLimit, "how many publications to show")
	return c
}

func (c *command) Flagset() *flag.FlagSet { return c.flagset }

func (c *command) Describe() string { return "list the recent publications of a notebook" }

func (c *command) Help() string { return helpText }

// Setup resolves the path argument, then the same startup as pull.
func (c *command) Setup(context.Context) error {
	if c.limit < 1 {
		return fmt.Errorf("log: --limit must be at least 1, got %d", c.limit)
	}
	path, runtime, err := app.OperationSetup("log", c.flagset, c.engine, c.flags, c.opts)
	if err != nil {
		return err
	}
	c.path = path
	c.runtime = runtime
	return nil
}

// Run prints the publications, newest first.
func (c *command) Run(ctx context.Context) error {
	if c.runtime == nil {
		return errors.New("log: Setup must run before Run")
	}
	defer c.runtime.Close()
	h, err := c.runtime.Log(ctx, c.path, c.limit)
	return app.ReportLog(c.opts.Out(), h, err, c.path, c.opts.Env)
}

const helpText = `slivingdoc log - list the recent publications of a notebook

Usage:
  slivingdoc log [flags] [path]

[path] is the notebook directory and defaults to the workspace root. Prints
the message of each accepted publication this machine holds, newest first.
History older than a checkpoint may not be held here; log says when older
publications exist.

  --limit int    how many publications to show (default 20)

A notebook directory that already pulled a hosted space remembers it, so
log there needs no --space and no --storage hosted.

Takes the same flags as pull.

` + app.FlagReference
