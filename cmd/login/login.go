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
                   [--default] [--force]

Prints an approval page and a code, opens the page in a browser, and waits
while you sign in to the site, pick the space, and approve the code. The
site then issues a token for that one space. Whoever enters a code first
decides it, so before anything is stored the login shows who approved it,
the space and its owner, the access, the storage endpoint and the site; on
a terminal it asks "Store this login? [y/N]" and revokes the token unless
you answer y. The token is stored in
<user-config-dir>/slivingdoc/credentials.json (SLIVINGDOC_CONFIG_DIR
overrides the directory). A login made while no default is stored
becomes the default login, and --default makes a later one the default; serve, pull and commit then use
hosted storage for the default space without SLIVINGDOC_TOKEN or --bucket.
Logging in again for the same space replaces the stored token and revokes
the old one when the same account approved both. Without a terminal
nobody is asked, so check the "Approved by" line; a login that would
replace a login or a default another account approved is refused unless
--force is given.

Flags:
  --bucket string   space to preselect; a token for another space is refused
  --read-only       ask for a read-only token
  --site string     site that approves the login              SLIVINGDOC_SITE
                    (default "https://www.slivingdoc.dev")
  --no-browser      print the page without opening a browser
  --default         make this login the default
  --force           without a terminal, replace another account's login or
                    default
`

const logoutHelp = `slivingdoc logout - revoke and remove a stored login

Usage:
  slivingdoc logout [--bucket <space>] [--site <url>]

Revokes the stored token of the space at the site that issued it and
removes it from the credentials file. A token the site no longer knows
(401 invalid_token) counts as revoked; a token whose revocation fails
stays stored, so the logout can be repeated. Logging out of the default
login's space clears the default.

Flags:
  --bucket string   space to log out of (default: the default login's space)
  --site string     only log out of logins this site issued   SLIVINGDOC_SITE
`
