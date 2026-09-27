// Package login holds the slivingdoc login and logout commands. login asks
// the site for a device approval, waits while a person approves it in the
// browser, and stores the issued hosted API token so serve, pull and
// commit need no SLIVINGDOC_TOKEN; logout revokes a stored token and
// removes it (architecture/login.md).
package login

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/baalimago/slivingdoc/internal/app"
)

// operation is a prepared login or logout.
type operation interface {
	Run(ctx context.Context) error
}

type command struct {
	name     string
	describe string
	help     string
	flagset  *flag.FlagSet
	prepare  func() (operation, error)
	op       operation
}

// Command returns the login command over the process environment. Nil
// option fields take the production defaults.
func Command(opts app.ProcessOptions) *command {
	flags := app.NewLoginFlags()
	return newCommand("login", "log in to hosted storage through the browser", loginHelp, flags.Bind,
		func() (operation, error) { return app.PrepareLogin(flags, opts) })
}

// LogoutCommand returns the logout command over the process environment.
func LogoutCommand(opts app.ProcessOptions) *command {
	flags := app.NewLogoutFlags()
	return newCommand("logout", "revoke and remove a stored login", logoutHelp, flags.Bind,
		func() (operation, error) { return app.PrepareLogout(flags, opts) })
}

func newCommand(name, describe, help string, bind func(*flag.FlagSet), prepare func() (operation, error)) *command {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	// Diagnostics are formatted by this process, not by the flag package.
	fs.SetOutput(io.Discard)
	bind(fs)
	return &command{name: name, describe: describe, help: help, flagset: fs, prepare: prepare}
}

func (c *command) Flagset() *flag.FlagSet { return c.flagset }

func (c *command) Describe() string { return c.describe }

func (c *command) Help() string { return c.help }

// Setup refuses positional arguments, invalid flags and an unreadable
// credentials file before the site is contacted.
func (c *command) Setup(context.Context) error {
	if c.flagset.NArg() > 0 {
		return fmt.Errorf("%s: unexpected argument %q", c.name, c.flagset.Arg(0))
	}
	op, err := c.prepare()
	if err != nil {
		return err
	}
	c.op = op
	return nil
}

func (c *command) Run(ctx context.Context) error {
	if c.op == nil {
		return errors.New(c.name + ": Setup must run before Run")
	}
	return c.op.Run(ctx)
}

const loginHelp = `slivingdoc login - log in to hosted storage through the browser

Usage:
  slivingdoc login [--bucket <space>] [--read-only] [--site <url>] [--no-browser]

Prints an approval page and a code, opens the page in a browser, and waits
while you sign in to the site, pick the space, and approve the code. The
site then issues a token for that one space, which is stored in
<user-config-dir>/slivingdoc/credentials.json (SLIVINGDOC_CONFIG_DIR
overrides the directory) and becomes the default login. serve, pull and
commit then use hosted storage for that space without SLIVINGDOC_TOKEN, and
--bucket may be omitted. Logging in again for the same space replaces the
stored token and revokes the old one. The result line names the account
that approved the code, and the space's owner when that is someone else:
whoever enters a code first decides it, so check that it is you.

Flags:
  --bucket string   space to preselect on the approval page
  --read-only       ask for a read-only token
  --site string     site that approves the login              SLIVINGDOC_SITE
                    (default "https://www.slivingdoc.dev")
  --no-browser      print the page without opening a browser
`

const logoutHelp = `slivingdoc logout - revoke and remove a stored login

Usage:
  slivingdoc logout [--bucket <space>] [--site <url>]

Revokes the stored token of the space at the site that issued it and
removes it from the credentials file. A token the site no longer knows
counts as revoked; a token whose revocation fails stays stored, so the
logout can be repeated. Logging out of the default login's space clears
the default.

Flags:
  --bucket string   space to log out of (default: the default login's space)
  --site string     only log out of logins this site issued   SLIVINGDOC_SITE
`
