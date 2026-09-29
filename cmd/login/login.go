// Package login holds the slivingdoc login, logout and space commands.
// login asks the site for a device approval, waits while a person approves
// it in the browser, and stores the issued account login key, from which
// serve, pull and commit mint short-lived space tokens without
// SLIVINGDOC_TOKEN; space lists the spaces the login reaches and sets the
// default one; logout revokes the key and removes it
// (architecture/login.md).
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
	// positional is how many arguments the command takes at most.
	positional int
	prepare    func(args []string) (operation, error)
	op         operation
}

// Command returns the login command over the process environment. Nil
// option fields take the production defaults.
func Command(opts app.ProcessOptions) *command {
	flags := app.NewLoginFlags()
	return newCommand("login", "log in to hosted storage through the browser", loginHelp, flags.Bind, 0,
		func([]string) (operation, error) { return app.PrepareLogin(flags, opts) })
}

// LogoutCommand returns the logout command over the process environment.
func LogoutCommand(opts app.ProcessOptions) *command {
	flags := app.NewLogoutFlags()
	return newCommand("logout", "revoke and remove the stored login", logoutHelp, flags.Bind, 0,
		func([]string) (operation, error) { return app.PrepareLogout(flags, opts) })
}

// SpaceCommand returns the space command over the process environment.
func SpaceCommand(opts app.ProcessOptions) *command {
	flags := app.NewSpaceFlags()
	return newCommand("space", "list the login's spaces, or set the default one", spaceHelp, flags.Bind, 1,
		func(args []string) (operation, error) {
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			return app.PrepareSpace(flags, name, opts)
		})
}

func newCommand(name, describe, help string, bind func(*flag.FlagSet), positional int, prepare func([]string) (operation, error)) *command {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	// Diagnostics are formatted by this process, not by the flag package.
	fs.SetOutput(io.Discard)
	bind(fs)
	return &command{name: name, describe: describe, help: help, flagset: fs, positional: positional, prepare: prepare}
}

func (c *command) Flagset() *flag.FlagSet { return c.flagset }

func (c *command) Describe() string { return c.describe }

func (c *command) Help() string { return c.help }

// Setup refuses extra positional arguments, invalid flags and an
// unreadable credentials file before the site is contacted.
func (c *command) Setup(context.Context) error {
	if c.flagset.NArg() > c.positional {
		return fmt.Errorf("%s: unexpected argument %q", c.name, c.flagset.Arg(c.positional))
	}
	op, err := c.prepare(c.flagset.Args())
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
  slivingdoc login [--space <space>] [--read-only] [--site <url>] [--no-browser]
                   [--force]

Prints an approval page and a code, opens the page in a browser, and waits
while you sign in to the site and approve the code. The site then issues a
login key for your account, which is stored in
<user-config-dir>/slivingdoc/credentials.json (SLIVINGDOC_CONFIG_DIR
overrides the directory) and is only ever sent to that site: serve, pull
and commit trade it there for a short-lived token of one space, which only
the storage endpoint sees and which is never stored.

Whoever enters a code first decides it, so before anything is stored the
login shows who approved it, the access, the storage endpoint, the site,
and the spaces the login reaches with their owners; on a terminal it asks
"Store this login? [y/N]" and revokes the key unless you answer y. Without
a terminal nobody is asked, so check the "Approved by" line; a login that
would replace one another account approved is refused unless --force is
given.

The default space becomes --space, else the only space the login reaches,
else the earlier default when the login still reaches it. Otherwise, when
the login reaches several spaces and stdin and stderr are a terminal, a
numbered list asks which space to make the default (q chooses later);
without one, run 'slivingdoc space <name>'. Logging in again replaces the stored login and
revokes the old key when the same account approved both. One login is
stored per storage endpoint: log out of another site's login for the same
endpoint first.

Flags:
  --space string    space to make the default; the login must reach it
  --bucket string   the same as --space (both with different values are
                    refused)
  --read-only       ask for a read-only login
  --site string     site that approves the login              SLIVINGDOC_SITE
                    (default "https://www.slivingdoc.dev")
  --no-browser      print the page without opening a browser
  --force           without a terminal, replace another account's login
`

const logoutHelp = `slivingdoc logout - revoke and remove the stored login

Usage:
  slivingdoc logout [--site <url>]

Revokes each stored login key at the site that issued it, which also
revokes every token minted from it, and removes the login and its default
space from the credentials file. On a terminal with several stored logins
and no --site, a numbered list asks which to log out of ("0,1" or the
range "0:1" picks several; q keeps every login). A key the site no longer knows (401
invalid_token) counts as revoked; a key whose revocation fails stays
stored, so the logout can be repeated.

Flags:
  --site string     only log out of logins this site issued   SLIVINGDOC_SITE
`

const spaceHelp = `slivingdoc space - list the login's spaces, or set the default one

Usage:
  slivingdoc space [--endpoint <url>]
  slivingdoc space [--endpoint <url>] <name>

Without a name, lists the spaces the stored login reaches, one per line
with its access and owner, and marks the default space with *; when
stdin, stdout and stderr are all a terminal, the list is numbered and the
number you type becomes the default space (q changes nothing). With a
name, checks that the login reaches that space and stores it as the
default, which serve, pull and commit use when neither --space nor
SLIVINGDOC_SPACE is given. The login key goes only to the site that
issued it.

Flags:
  --endpoint string  hosted storage API whose login to use     SLIVINGDOC_ENDPOINT
                     (default: the only stored login)
`
