// Package serve is the slivingdoc server command: it resolves the
// configuration, opens the pinned native engine, proves the S3
// compatibility probe, and serves the two MCP tools over stdio
// (architecture sections 2, 17, and 18).
package serve

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
	runtime *app.Runtime
}

// Command returns the serve command over the given native engine and
// process environment. Nil option fields take the production defaults.
func Command(engine git.Engine, opts app.ProcessOptions) *command {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	// Flag diagnostics are formatted by this process, not by the flag
	// package: a startup refusal is one redacted line on stderr.
	fs.SetOutput(io.Discard)
	flags := app.NewFlags()
	flags.Bind(fs)
	// With no configured root the server owns a temporary notebook
	// directory: every agent gets a private one and the tools need no path.
	opts.Ephemeral = true
	return &command{engine: engine, opts: opts, flags: flags, flagset: fs}
}

func (c *command) Flagset() *flag.FlagSet { return c.flagset }

func (c *command) Describe() string {
	return "serve the notebook over MCP stdio"
}

func (c *command) Help() string { return app.HelpText }

// Setup performs the whole startup refusal surface: configuration
// resolution, the pinned-version engine check, and the S3 compatibility
// probe. The returned error is already redacted, since the command router
// prints it verbatim.
func (c *command) Setup(context.Context) error {
	runtime, err := app.Setup(c.engine, c.flags, c.opts)
	if err != nil {
		return err
	}
	c.runtime = runtime
	return nil
}

// Run serves MCP until the client disconnects, the context is cancelled, or
// a termination signal starts the bounded shutdown.
func (c *command) Run(ctx context.Context) error {
	if c.runtime == nil {
		return errors.New("serve: Setup must run before Run")
	}
	defer c.runtime.Close()
	return c.runtime.Serve(ctx)
}
