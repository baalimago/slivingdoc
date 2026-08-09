// Package version prints the slivingdoc release version.
//
// It does not use the boilerplate version command: the npm launcher and the
// release smoke test read exactly one line, "slivingdoc <version>", from
// stdout, and the shared command decorates its output and reports the Go
// build info as well.
package version

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/baalimago/slivingdoc/internal/app"
)

type command struct {
	out io.Writer
}

// Command returns the version command. A nil writer is os.Stdout.
func Command(out io.Writer) *command {
	if out == nil {
		out = os.Stdout
	}
	return &command{out: out}
}

func (c *command) Flagset() *flag.FlagSet {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func (c *command) Describe() string { return "print the slivingdoc version" }

func (c *command) Help() string { return "print the slivingdoc version and exit" }

// Setup touches no startup dependency: the version must be readable without
// a native engine, a bucket, or a reachable store.
func (c *command) Setup(context.Context) error { return nil }

func (c *command) Run(context.Context) error {
	_, err := fmt.Fprintf(c.out, "slivingdoc %s\n", app.Version)
	return err
}
