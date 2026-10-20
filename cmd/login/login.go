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
	return newCommand("space", "list, create, or set the default of a hosted space or S3 bucket", spaceHelp, flags.Bind, 2,
		func(args []string) (operation, error) { return app.PrepareSpace(flags, args, opts) })
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

const spaceHelp = `slivingdoc space - list, create, or set the default of a hosted space or
S3 bucket

Usage:
  slivingdoc space [--storage auto|hosted|s3] [--endpoint <url>]
                   [--profile <name>] [--region <region>] [--path-style]
                   [--prefix <prefix>]
  slivingdoc space [flags] <name>
  slivingdoc space [flags] new <name>

Without a positional word, lists the targets of every backend the mode
selects, one group per identity, and marks the default of each group with
*. A hosted group is one stored login and its spaces; an S3 group is one
resolved identity and its buckets. Rows carry the directories that
remember them in parentheses; a hosted row also carries its access (write
or read) and owner. A target the listing did not return, but a directory
remembers or a layout stores as the default, stays visible marked ! and
cannot be chosen as a default; its hint goes to stderr. When stdin, stdout
and stderr are all a terminal, the targets appear as a numbered table with
one create row after each backend instead: the number you type becomes that
target's default, and a create row asks for a name, creates the target, and
makes it the default (q changes nothing).

With a name, checks that one identity lists that target and stores it as
that identity's default. A hosted default is what serve, pull and commit
use when neither --space nor SLIVINGDOC_SPACE is given. With 'new <name>',
creates a hosted space through the site or an S3 bucket through the S3
API, on the identity the flags select, and leaves the default unchanged.
A bare 'new' makes a target named new the default when one is listed, and
otherwise says how to create one. A create refuses --prefix, because it
makes a new store rather than a notebook inside one.

In auto mode a hosted group needs a stored login, and an S3 group needs an
S3 flag, an AWS variable, an existing ~/.aws/credentials or ~/.aws/config
file, or a recorded S3 target; shared AWS files alone do not select S3 for
'space new'. Without an S3 signal, a machine with only a hosted login reads no
AWS configuration. A group that cannot list keeps
its default and recorded targets visible, marked !, and reports the reason
on stderr. An S3 identity that the environment selects and auto cannot
resolve degrades the same way; --storage s3, or a flag that names the
identity, refuses instead. An S3 target stores the resolved endpoint,
region, profile, and the --prefix value under that identity's store key.
A profile is a [profile <name>] section of ~/.aws/config; a bare [<name>]
section there supplies no region, so the identity falls back to us-east-1.
Listing an S3 account needs s3:ListAllMyBuckets and creating a bucket
needs s3:CreateBucket. The login key goes only to the site that issued it.

Flags:
  --storage string   storage backend: auto, hosted, or s3          SLIVINGDOC_STORAGE
  --endpoint string  hosted storage API whose login to use; the    SLIVINGDOC_ENDPOINT
                     S3 endpoint selects S3
  --profile string   AWS shared-config profile (S3 only)           AWS_PROFILE
  --region string    S3 region (S3 only)                           AWS_REGION
  --path-style       force S3 path-style addressing (S3 only)      SLIVINGDOC_PATH_STYLE
  --prefix string    notebook prefix inside an S3 bucket           SLIVINGDOC_PREFIX
`
