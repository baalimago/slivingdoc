// Package encrypt is the slivingdoc encrypt command: the key-material
// operations of an encrypted notebook. It routes init, status, passwd,
// recovery and rekey (worklogs/26-10-05-encrypted-notebooks/
// phase-4-command-surface.md), composes the same storage configuration as
// serve, asks the terminal for a secret only when no other source supplies
// one, and prints the accepted result and any one-time recovery code.
package encrypt

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/crypt"
	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/notebook"
)

type command struct {
	engine  git.Engine
	opts    app.ProcessOptions
	flags   *app.Flags
	flagset *flag.FlagSet
	record  app.KeyRecordOptions
	sub     app.EncryptSubcommand
	path    string
	runtime *app.Runtime
}

// Command returns the encrypt command over the given native engine and
// process environment. Nil option fields take the production defaults.
func Command(engine git.Engine, opts app.ProcessOptions) *command {
	fs := flag.NewFlagSet("encrypt", flag.ContinueOnError)
	// Diagnostics are formatted by this process, not by the flag package.
	fs.SetOutput(io.Discard)
	flags := app.NewFlags()
	flags.Bind(fs)
	c := &command{engine: engine, opts: opts, flags: flags, flagset: fs}
	fs.BoolVar(&c.record.StorePassphrase, "store-passphrase", false, "store the plaintext passphrase for this notebook (requires --confirm-store-passphrase)")
	fs.BoolVar(&c.record.ConfirmStorePassphrase, "confirm-store-passphrase", false, "confirm the plaintext-at-rest warning of --store-passphrase")
	fs.StringVar(&c.record.StorePassphraseFile, "store-passphrase-file", "", "store the path of the envfile that holds the passphrase")
	fs.BoolVar(&c.record.ForgetKeyMaterial, "forget-key-material", false, "remove this notebook's stored key material")
	return c
}

func (c *command) Flagset() *flag.FlagSet { return c.flagset }

func (c *command) Describe() string { return "manage the encryption of a notebook" }

func (c *command) Help() string { return helpText }

// Setup resolves the subcommand and its optional path, refuses an invalid key
// record option, then performs the same startup refusal surface as pull: the
// configuration, the pinned-version engine check, and the store probe. The
// argument refusals come first, so a bad command line touches no dependency.
func (c *command) Setup(context.Context) error {
	sub, path, err := app.EncryptArgs(c.flagset, c.opts.Cwd)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}
	if sub == "" {
		return fmt.Errorf("encrypt: a subcommand is required: %s", subcommandList())
	}
	if !slices.Contains(app.EncryptSubcommands, sub) {
		return fmt.Errorf("encrypt: %q is not an encrypt subcommand; use %s", sub, subcommandList())
	}
	if err := c.record.Validate(sub); err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}
	runtime, err := app.Setup(c.engine, c.flags, c.opts.WithAssociation(path))
	if err != nil {
		return err
	}
	c.sub, c.path, c.runtime = sub, path, runtime
	return nil
}

// Run executes the subcommand once and prints its result: the unified report
// for an accepted publication, the status report for status, and a one-time
// recovery code where a door was minted. A domain error prints the same
// structured report pull and commit print and returns its terse category.
func (c *command) Run(ctx context.Context) error {
	if c.runtime == nil {
		return errors.New("encrypt: Setup must run before Run")
	}
	defer c.runtime.Close()
	path := c.runtime.NotebookPath(c.path)
	report, err := c.runtime.Encrypt(ctx, app.EncryptCommand{
		Command: c.sub,
		Path:    c.path,
		Record:  c.record,
		Prompt:  app.NewSecretPrompt(c.opts.Stdin, c.opts.ErrOut()),
	})
	if err != nil {
		return app.Report(c.opts.Out(), notebook.Result{}, err, path, c.opts.Env, c.runtime.ReadOnlyPaths(), c.runtime.WritablePaths())
	}
	if report.Status != nil {
		writeStatus(c.opts.Out(), report.Status)
	} else if err := app.Report(c.opts.Out(), report.Result, nil, path, c.opts.Env, c.runtime.ReadOnlyPaths(), c.runtime.WritablePaths()); err != nil {
		return err
	}
	if report.Recovery != "" {
		fmt.Fprintf(c.opts.Out(), "recovery code: %s\n", report.Recovery)
	}
	for _, warning := range report.Warnings {
		fmt.Fprintf(c.opts.ErrOut(), "warning: %s\n", warning)
	}
	return nil
}

// writeStatus renders the `encrypt status` report: the stored mode, the doors
// and KDF settings when a key opened the notebook, the key-material source,
// and the reminder that local files stay plaintext.
func writeStatus(out io.Writer, status *app.EncryptionReport) {
	if !status.Enabled {
		io.WriteString(out, "encryption: off\nlocal: plaintext\n")
		return
	}
	io.WriteString(out, "encryption: enabled\n")
	if status.Locked {
		io.WriteString(out, "locked: true\nlocal: plaintext\n")
		return
	}
	fmt.Fprintf(out, "doors: %s\n", strings.Join(app.DoorNames(status.Doors), ", "))
	if params, ok := doorParams(status.Doors); ok {
		fmt.Fprintf(out, "kdf: argon2id memory=%dKiB iterations=%d parallelism=%d\n", params.MemoryKiB, params.Iterations, params.Parallelism)
	}
	fmt.Fprintf(out, "source: %s\n", app.SourceName(status.Source))
	io.WriteString(out, "local: plaintext\n")
}

// doorParams is the KDF cost of the first supported door, the settings every
// door of one notebook shares.
func doorParams(doors []crypt.Door) (crypt.KDFParams, bool) {
	for _, door := range doors {
		if door.Type == crypt.DoorPassphrase || door.Type == crypt.DoorRecovery {
			return door.Params, true
		}
	}
	return crypt.KDFParams{}, false
}

// subcommandList renders the subcommand names for a refusal.
func subcommandList() string {
	names := make([]string, 0, len(app.EncryptSubcommands))
	for _, sub := range app.EncryptSubcommands {
		names = append(names, string(sub))
	}
	return strings.Join(names, ", ")
}

const helpText = `slivingdoc encrypt - manage the encryption of a notebook

Usage:
  slivingdoc encrypt <subcommand> [flags] [path]

[path] is the notebook directory and defaults to the workspace root. Only the
stored objects are encrypted; the visible directory, the private state and the
pack cache stay plaintext always. There is no command that returns a notebook
to plaintext.

Subcommands:
  init       establish encryption and publish a checkpoint; print the
             recovery code once, after acceptance
  status     report the mode, doors, KDF settings and key-material source;
             reads only, never writes
  passwd     add, change or remove a passphrase door; no pack object moves
  recovery   replace the recovery door and show the new code once
  rekey      replace the notebook key and retained history under a new
             passphrase and a fresh recovery code

The passphrase or recovery code is read from SLIVINGDOC_PASSPHRASE, from the
file named by SLIVINGDOC_PASSPHRASE_FILE, from the stored key record, or, when
none of those supplies one, from the terminal, where the typed characters show
as '*' and never as themselves. No flag supplies a secret and no command echoes
one.

  --store-passphrase            store the plaintext passphrase for this
                                notebook (init, passwd, rekey)
  --confirm-store-passphrase    confirm the plaintext-at-rest warning
  --store-passphrase-file path  store only the envfile path (init, passwd,
                                rekey); exclusive with --store-passphrase
  --forget-key-material         remove this notebook's stored key material
                                (passwd, rekey); exclusive with a store option

A key-record change lands only after the store accepts the operation. If that
local change fails, the command still reports its accepted result and warns.

` + app.FlagReference
