package app

// Encryption command support: the process-wide KDF gate, the notebook
// identity's key material, the interactive secret prompt, and the five
// `slivingdoc encrypt` subcommands
// (worklogs/26-10-05-encrypted-notebooks/phase-4-command-surface.md). The
// command composes the resolvable candidate sources, the shared keyring and
// the notebook operations; it never duplicates a cryptographic operation and
// never echoes a secret.

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/baalimago/slivingdoc/internal/crypt"
	"github.com/baalimago/slivingdoc/internal/keyrecord"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/recovery"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/tui"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// EncryptSubcommand names one `slivingdoc encrypt` subcommand.
type EncryptSubcommand string

const (
	// EncryptInit establishes encryption and publishes a checkpoint.
	EncryptInit EncryptSubcommand = "init"
	// EncryptStatus reports the stored mode without writing.
	EncryptStatus EncryptSubcommand = "status"
	// EncryptPasswd adds, changes or removes a passphrase door.
	EncryptPasswd EncryptSubcommand = "passwd"
	// EncryptRecovery replaces the recovery door.
	EncryptRecovery EncryptSubcommand = "recovery"
	// EncryptRekey replaces the notebook key and retained history.
	EncryptRekey EncryptSubcommand = "rekey"
)

// EncryptSubcommands lists every subcommand in help order.
var EncryptSubcommands = []EncryptSubcommand{EncryptInit, EncryptStatus, EncryptPasswd, EncryptRecovery, EncryptRekey}

// maxDoors is the supported door count of one notebook
// (phase-4-command-surface.md, Options and command parameters).
const maxDoors = 4

// The commit messages of the two root checkpoints this package publishes.
// They are fixed because the encrypt commands take no message.
const (
	initMessage  = "encrypt init"
	rekeyMessage = "encrypt rekey"
)

// Errors a prompt or a record change reports.
var (
	errNoSecret   = errors.New("no secret was given")
	errPrompt     = errors.New("the prompt could not be answered")
	errNoAnswer   = errors.New("the prompt could not be answered: no answer")
	errDoorRemove = errors.New("removing this door would leave the notebook with no door; add another secret first")
)

// KeyRecordOptions are the resolved key-record flags of one encrypt command
// (phase-4-command-surface.md, Options and command parameters).
type KeyRecordOptions struct {
	StorePassphrase        bool
	ConfirmStorePassphrase bool
	StorePassphraseFile    string
	ForgetKeyMaterial      bool
}

// Validate refuses an option combination this phase disallows, before the
// store is touched. The store options belong to init, passwd and rekey; the
// forget option belongs to passwd and rekey alone
// (phase-4-command-surface.md, Options and command parameters).
func (o KeyRecordOptions) Validate(command EncryptSubcommand) error {
	offersStore := command == EncryptInit || command == EncryptPasswd || command == EncryptRekey
	offersForget := command == EncryptPasswd || command == EncryptRekey
	switch {
	case o.StorePassphrase && o.StorePassphraseFile != "":
		return errors.New("--store-passphrase and --store-passphrase-file are mutually exclusive")
	case o.ForgetKeyMaterial && (o.StorePassphrase || o.StorePassphraseFile != ""):
		return errors.New("--forget-key-material cannot be combined with a store option")
	case o.StorePassphrase && !o.ConfirmStorePassphrase:
		return errors.New("--store-passphrase requires --confirm-store-passphrase")
	case o.ConfirmStorePassphrase && !o.StorePassphrase:
		return errors.New("--confirm-store-passphrase is only meaningful with --store-passphrase")
	case !offersStore && (o.StorePassphrase || o.StorePassphraseFile != ""):
		return fmt.Errorf("encrypt %s accepts no key-record store option", command)
	case !offersForget && o.ForgetKeyMaterial:
		return fmt.Errorf("encrypt %s accepts no --forget-key-material", command)
	}
	return nil
}

// EncryptCommand is the resolved command line of one encrypt invocation.
type EncryptCommand struct {
	Command EncryptSubcommand
	Path    string
	Record  KeyRecordOptions
	Prompt  *SecretPrompt
}

// EncryptReport is the result of one encrypt command: the operation result,
// the optional status report, the recovery code a door change minted, and the
// warnings the caller must read (a record change that did not land, or a
// setting its owner must update).
type EncryptReport struct {
	Result   notebook.Result
	Status   *EncryptionReport
	Recovery recovery.Code
	Warnings []string
}

// EncryptionReport is the `encrypt status` view of a notebook.
type EncryptionReport struct {
	Enabled bool
	Locked  bool
	Doors   []crypt.Door
	Source  crypt.CandidateSource
}

// SecretPrompt reads the interactive answers of an encrypt command over the
// command's streams. One reader is shared by every answer, so a line that
// follows a newline is not lost in a discarded buffer. A secret read from a
// terminal is masked, so it never appears on the screen (architecture/tui.md,
// The masked read).
type SecretPrompt struct {
	source io.Reader
	reader *bufio.Reader
	out    io.Writer
}

// NewSecretPrompt returns a prompt over in and out; a nil stream is the
// process stream.
func NewSecretPrompt(in io.Reader, out io.Writer) *SecretPrompt {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stderr
	}
	return &SecretPrompt{source: in, reader: bufio.NewReader(in), out: out}
}

// Read asks a question and returns the answer; an empty answer is the empty
// slice, not an error. The answer's trailing newline is dropped and its other
// bytes are kept exactly, including leading or trailing spaces.
func (p *SecretPrompt) Read(label string) ([]byte, error) {
	if p == nil {
		return nil, errNoAnswer
	}
	fmt.Fprint(p.out, label)
	line, err := p.reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: %v", errPrompt, err)
	}
	if err != nil && line == "" {
		return nil, errNoAnswer
	}
	return []byte(strings.TrimRight(line, "\r\n")), nil
}

// Ask reads a non-empty secret, masked when the source is a terminal.
func (p *SecretPrompt) Ask(label string) ([]byte, error) {
	secret, err := p.ReadSecret(label)
	if err != nil {
		return nil, err
	}
	if len(secret) == 0 {
		return nil, errNoSecret
	}
	return secret, nil
}

// ReadSecret asks a question and returns the answer, drawing one mask byte per
// typed character instead of echoing it when the source is a terminal. A
// source that is not a terminal, and a reader that already buffered a line,
// keep the plain read a pipe and an injected reader need. An answer the person
// interrupts or ends with Ctrl-D is errNoAnswer, so the caller reports the
// documented KEY_REQUIRED rather than a new failure.
func (p *SecretPrompt) ReadSecret(label string) ([]byte, error) {
	if p == nil {
		return nil, errNoAnswer
	}
	if file, ok := p.source.(*os.File); ok && tui.IsTerminal(file) && p.reader.Buffered() == 0 {
		return p.readMasked(label, file)
	}
	return p.Read(label)
}

// readMasked reads one line of a terminal stream with its echo replaced by
// mask bytes. The label is drawn by ReadMasked, after the echo is off.
func (p *SecretPrompt) readMasked(label string, file *os.File) ([]byte, error) {
	secret, err := tui.ReadMasked(file, p.out, label, '*')
	switch {
	case errors.Is(err, tui.ErrInterrupt):
		return nil, errNoAnswer
	case errors.Is(err, io.EOF):
		if len(secret) == 0 {
			return nil, errNoAnswer
		}
		return secret, nil
	case err != nil:
		return nil, fmt.Errorf("%w: %v", errPrompt, err)
	}
	return secret, nil
}

// Confirm asks a yes/no question; an empty answer is yes.
func (p *SecretPrompt) Confirm(label string) (bool, error) {
	answer, err := p.Read(label)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(string(answer)) {
	case "", "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return false, fmt.Errorf("%w: answer y or n", errPrompt)
	}
}

// EncryptArgs extracts the encrypt subcommand and its optional notebook path
// from an already-parsed flag set, parsing any flag runs interleaved after
// them. Both are empty when the command line gave none.
func EncryptArgs(fs *flag.FlagSet, cwd string) (EncryptSubcommand, string, error) {
	positionals, err := positionals(fs)
	if err != nil {
		return "", "", err
	}
	if len(positionals) == 0 {
		return "", "", nil
	}
	command := EncryptSubcommand(positionals[0])
	if len(positionals) == 1 {
		return command, "", nil
	}
	if len(positionals) > 2 {
		return "", "", fmt.Errorf("at most one notebook path argument is accepted, got %d", len(positionals)-1)
	}
	path, err := resolvePath(cwd, positionals[1])
	if err != nil {
		return "", "", err
	}
	return command, path, nil
}

// NotebookPath resolves the optional notebook path of one encrypt command
// to the absolute directory its report names; an omitted path is the
// workspace root (phase-4-command-surface.md).
func (r *Runtime) NotebookPath(path string) string { return r.resolve(path) }

// AskForSecret asks the terminal for the passphrase or recovery code of an
// encrypted notebook when this process resolved no secret itself, and adopts
// the answer as this run's prompted candidate (architecture/encryption.md,
// The process keyring). The one-shot pull and commit commands call it before
// they start, because the operation reports KEY_REQUIRED without a secret. A
// process whose stdin and stderr are not terminals asks nothing, so a script
// and an MCP server keep that refusal.
func (r *Runtime) AskForSecret(ctx context.Context) error {
	if r.p.terminal != OnTerminal {
		return nil
	}
	return r.svc.AskForSecret(ctx, func() ([]byte, error) {
		return NewSecretPrompt(r.p.stdin, r.p.stderr).Ask(passphrasePrompt)
	})
}

// passphrasePrompt is the label of the terminal ask pull and commit make.
const passphrasePrompt = "Passphrase or recovery code: "

// Encrypt runs one encrypt command against the notebook at req.Path (the
// workspace root when empty).
func (r *Runtime) Encrypt(ctx context.Context, req EncryptCommand) (EncryptReport, error) {
	if err := req.Record.Validate(req.Command); err != nil {
		return EncryptReport{}, invalidInput(err)
	}
	ctx = notebook.WithLogger(ctx, Module(r.base, ModuleNotebook))
	// The encrypt commands classify, open and rewrite the stored current
	// wrapper themselves, so they take the raw notebook view: the decorator
	// would decrypt current under them.
	nb, err := r.svc.rawNotebookFor(ctx, r.resolve(req.Path))
	if err != nil {
		return EncryptReport{}, err
	}
	candidates, err := r.encryptionCandidates()
	if err != nil {
		return EncryptReport{}, err
	}
	raw := r.svc.store
	switch req.Command {
	case EncryptStatus:
		return r.encryptStatus(ctx, nb, raw, candidates)
	case EncryptInit:
		return r.encryptInit(ctx, nb, raw, candidates, req)
	case EncryptPasswd:
		return r.encryptPasswd(ctx, nb, raw, candidates, req)
	case EncryptRecovery:
		return r.encryptRecovery(ctx, nb, raw, candidates, req)
	case EncryptRekey:
		return r.encryptRekey(ctx, nb, raw, candidates, req)
	default:
		return EncryptReport{}, invalidInput(fmt.Errorf("%q is not an encrypt subcommand", req.Command))
	}
}

// encryptStatus reports the stored mode, and unlocks the notebook when a
// candidate opens it. A candidate set that opens no door reports the notebook
// as locked rather than failing, because status is a report and never writes.
func (r *Runtime) encryptStatus(ctx context.Context, nb *notebook.Notebook, raw storage.ObjectStore, candidates []crypt.Candidate) (EncryptReport, error) {
	status, err := r.classify(ctx, nb, raw, candidates)
	if err != nil {
		return EncryptReport{}, err
	}
	return EncryptReport{Status: &status}, nil
}

// classify resolves the notebook's mode and, when the candidates open it, its
// doors and source.
func (r *Runtime) classify(ctx context.Context, nb *notebook.Notebook, raw storage.ObjectStore, candidates []crypt.Candidate) (EncryptionReport, error) {
	if len(candidates) == 0 {
		return classifyWith(ctx, nb, raw, nil)
	}
	ring, err := crypt.NewKeyring(candidates, r.gate)
	if err != nil {
		return EncryptionReport{}, keyMaterialInvalid(err)
	}
	status, err := nb.EncryptStatus(ctx, raw, ring)
	if err != nil {
		if !keyRejected(err) {
			return EncryptionReport{}, err
		}
		return classifyWith(ctx, nb, raw, nil)
	}
	return reportOf(status), nil
}

// classifyWith classifies current with an optional keyring, reporting a
// notebook the keyring cannot open as locked.
func classifyWith(ctx context.Context, nb *notebook.Notebook, raw storage.ObjectStore, keyring *crypt.Keyring) (EncryptionReport, error) {
	status, err := nb.EncryptStatus(ctx, raw, keyring)
	if err != nil {
		return EncryptionReport{}, err
	}
	return reportOf(status), nil
}

// encryptInit establishes encryption over a plaintext notebook: it mints a
// new notebook key and recovery code, wraps a passphrase and a recovery door,
// and publishes the accepted and visible state as one encrypted root
// checkpoint. The key record changes only after the publication is accepted.
func (r *Runtime) encryptInit(ctx context.Context, nb *notebook.Notebook, raw storage.ObjectStore, candidates []crypt.Candidate, req EncryptCommand) (EncryptReport, error) {
	status, err := nb.EncryptStatus(ctx, raw, nil)
	if err != nil {
		return EncryptReport{}, err
	}
	if status.Enabled {
		return EncryptReport{}, alreadyEnabled()
	}
	secret, err := r.initSecret(candidates, req.Prompt)
	if err != nil {
		return EncryptReport{}, err
	}
	if err := checkRecordConflict(candidates, secret, req.Record); err != nil {
		return EncryptReport{}, err
	}
	key, code, doors, err := r.newRootDoors(ctx, secret)
	if err != nil {
		return EncryptReport{}, err
	}
	seal, err := crypt.NewUnlockedKeyring(key, nil, r.gate)
	if err != nil {
		return EncryptReport{}, keyMaterialInvalid(err)
	}
	result, err := nb.EncryptInit(ctx, notebook.Cipher{Store: raw, Seal: seal, Doors: doors}, initMessage)
	if err != nil {
		return EncryptReport{}, err
	}
	return EncryptReport{Result: result, Recovery: code, Warnings: r.recordWarnings(ctx, req.Record, secret)}, nil
}

// encryptRekey publishes a new root checkpoint under a new notebook key with
// one passphrase door and one fresh recovery door. It requires an already
// encrypted notebook and an accepted baseline in this workspace.
func (r *Runtime) encryptRekey(ctx context.Context, nb *notebook.Notebook, raw storage.ObjectStore, candidates []crypt.Candidate, req EncryptCommand) (EncryptReport, error) {
	ring, err := r.openKeyring(candidates, req.Prompt, "Current passphrase or recovery code: ")
	if err != nil {
		return EncryptReport{}, err
	}
	status, err := nb.EncryptStatus(ctx, raw, ring)
	if err != nil {
		return EncryptReport{}, err
	}
	if !status.Enabled {
		return EncryptReport{}, notEnabled()
	}
	keep, err := req.Prompt.Confirm("Keep the unlocking secret? [Y/n] ")
	if err != nil {
		return EncryptReport{}, promptRefusal(err)
	}
	secret := ring.ResolvedSecret()
	if !keep || len(secret) == 0 {
		if secret, err = req.Prompt.Ask("New passphrase: "); err != nil {
			return EncryptReport{}, promptRefusal(err)
		}
	}
	if err := checkRecordConflict(candidates, secret, req.Record); err != nil {
		return EncryptReport{}, err
	}
	key, code, doors, err := r.newRootDoors(ctx, secret)
	if err != nil {
		return EncryptReport{}, err
	}
	seal, err := crypt.NewUnlockedKeyring(key, nil, r.gate)
	if err != nil {
		return EncryptReport{}, keyMaterialInvalid(err)
	}
	result, err := nb.EncryptRekey(ctx, notebook.Cipher{Store: raw, Open: ring, Seal: seal, Doors: doors}, rekeyMessage)
	if err != nil {
		return EncryptReport{}, err
	}
	warnings := r.recordWarnings(ctx, req.Record, secret)
	if !keep {
		warnings = append(warnings, staleSourceWarning(status.Source)...)
	}
	return EncryptReport{Result: result, Recovery: code, Warnings: warnings}, nil
}

// encryptPasswd adds, changes or removes the passphrase door. It rewrites only
// the encrypted current header, so no pack byte moves, and applies a requested
// key-record change only after the accepted replacement.
func (r *Runtime) encryptPasswd(ctx context.Context, nb *notebook.Notebook, raw storage.ObjectStore, candidates []crypt.Candidate, req EncryptCommand) (EncryptReport, error) {
	ring, err := r.openKeyring(candidates, req.Prompt, "Current passphrase or recovery code: ")
	if err != nil {
		return EncryptReport{}, err
	}
	status, err := nb.EncryptStatus(ctx, raw, ring)
	if err != nil {
		return EncryptReport{}, err
	}
	if !status.Enabled {
		return EncryptReport{}, notEnabled()
	}
	secret, err := req.Prompt.ReadSecret("New passphrase (empty removes the passphrase door): ")
	if err != nil {
		return EncryptReport{}, promptRefusal(err)
	}
	var add *crypt.Door
	if len(secret) > 0 {
		door, err := ring.WrapDoor(ctx, storage.CurrentKey, secret, crypt.DoorPassphrase, r.kdfParams)
		if err != nil {
			return EncryptReport{}, keyMaterialInvalid(err)
		}
		add = &door
	}
	doors, err := replaceDoor(status.Doors, crypt.DoorPassphrase, add)
	if err != nil {
		return EncryptReport{}, err
	}
	generation, err := nb.EncryptDoors(ctx, raw, ring, doors)
	if err != nil {
		return EncryptReport{}, err
	}
	warnings := r.recordWarnings(ctx, req.Record, secret)
	if len(secret) > 0 && !bytes.Equal(secret, ring.ResolvedSecret()) {
		warnings = append(warnings, staleSourceWarning(status.Source)...)
	}
	return EncryptReport{
		Result:   notebook.Result{Generation: generation},
		Warnings: warnings,
	}, nil
}

// encryptRecovery replaces the recovery door and returns the new code. It
// rewrites only the encrypted current header.
func (r *Runtime) encryptRecovery(ctx context.Context, nb *notebook.Notebook, raw storage.ObjectStore, candidates []crypt.Candidate, req EncryptCommand) (EncryptReport, error) {
	ring, err := r.openKeyring(candidates, req.Prompt, "Current passphrase or recovery code: ")
	if err != nil {
		return EncryptReport{}, err
	}
	status, err := nb.EncryptStatus(ctx, raw, ring)
	if err != nil {
		return EncryptReport{}, err
	}
	if !status.Enabled {
		return EncryptReport{}, notEnabled()
	}
	code, err := recovery.Generate()
	if err != nil {
		return EncryptReport{}, keyMaterialInvalid(err)
	}
	door, err := ring.WrapDoor(ctx, storage.CurrentKey, code.Secret(), crypt.DoorRecovery, r.kdfParams)
	if err != nil {
		return EncryptReport{}, keyMaterialInvalid(err)
	}
	doors, err := replaceDoor(status.Doors, crypt.DoorRecovery, &door)
	if err != nil {
		return EncryptReport{}, err
	}
	generation, err := nb.EncryptDoors(ctx, raw, ring, doors)
	if err != nil {
		return EncryptReport{}, err
	}
	return EncryptReport{Result: notebook.Result{Generation: generation}, Recovery: code}, nil
}

// newRootDoors mints a notebook key and a recovery code and wraps them as one
// passphrase door and one recovery door under the given secret.
func (r *Runtime) newRootDoors(ctx context.Context, secret []byte) (crypt.NotebookKey, recovery.Code, []crypt.Door, error) {
	key, err := crypt.GenerateNotebookKey()
	if err != nil {
		return crypt.NotebookKey{}, "", nil, keyMaterialInvalid(err)
	}
	code, err := recovery.Generate()
	if err != nil {
		return crypt.NotebookKey{}, "", nil, keyMaterialInvalid(err)
	}
	passDoor, err := crypt.WrapKey(ctx, r.gate, key, secret, storage.CurrentKey, crypt.DoorPassphrase, r.kdfParams)
	if err != nil {
		return crypt.NotebookKey{}, "", nil, keyMaterialInvalid(err)
	}
	recoveryDoor, err := crypt.WrapKey(ctx, r.gate, key, code.Secret(), storage.CurrentKey, crypt.DoorRecovery, r.kdfParams)
	if err != nil {
		return crypt.NotebookKey{}, "", nil, keyMaterialInvalid(err)
	}
	return key, code, []crypt.Door{passDoor, recoveryDoor}, nil
}

// openKeyring returns a locked keyring over the resolvable candidates. With no
// candidate it asks the prompt for one, so an interactive command can be the
// only secret source.
func (r *Runtime) openKeyring(candidates []crypt.Candidate, prompt *SecretPrompt, label string) (*crypt.Keyring, error) {
	if len(candidates) == 0 {
		secret, err := prompt.Ask(label)
		if err != nil {
			return nil, promptRefusal(err)
		}
		candidates = []crypt.Candidate{{Source: crypt.SourcePrompt, Secret: secret}}
	}
	ring, err := crypt.NewKeyring(candidates, r.gate)
	if err != nil {
		return nil, keyMaterialInvalid(err)
	}
	return ring, nil
}

// initSecret is the passphrase a new notebook is protected with: the first
// resolvable candidate, else the prompted one.
func (r *Runtime) initSecret(candidates []crypt.Candidate, prompt *SecretPrompt) ([]byte, error) {
	if len(candidates) > 0 {
		return candidates[0].Secret, nil
	}
	secret, err := prompt.Ask("New passphrase: ")
	if err != nil {
		return nil, promptRefusal(err)
	}
	return secret, nil
}

// encryptionCandidates resolves the candidate secrets of this notebook
// identity: the environment value, its envfile value, then the key record.
func (r *Runtime) encryptionCandidates() ([]crypt.Candidate, error) {
	candidates, err := candidatesOf(r.cfg, EnvLookup(r.p.env))
	if err != nil {
		return nil, keyMaterialInvalid(err)
	}
	return candidates, nil
}

// candidatesOf resolves the candidate secrets of the notebook identity cfg
// names (architecture/encryption.md, Candidate sources).
func candidatesOf(cfg config, getenv func(string) string) ([]crypt.Candidate, error) {
	return keyrecord.Resolve(cfg.serviceConfig().identity(), getenv)
}

// keyring returns the process keyring: the candidate secrets the
// environment, its envfile and the key record supply, over the process
// gate. A notebook with no candidate yields a valid locked keyring, so a
// plaintext notebook keeps working and an encrypted one reports
// KEY_REQUIRED at its first operation.
func keyring(cfg config, getenv func(string) string, gate *crypt.KDFGate) (*crypt.Keyring, error) {
	candidates, err := candidatesOf(cfg, getenv)
	if err != nil {
		return nil, err
	}
	ring, err := crypt.NewKeyring(candidates, gate)
	if err != nil {
		return nil, err
	}
	return ring, nil
}

// recordWarnings applies a requested key-record change. It never hides an
// accepted operation: a failure becomes a warning the caller prints, and the
// command's result stands.
func (r *Runtime) recordWarnings(ctx context.Context, opts KeyRecordOptions, secret []byte) []string {
	err := r.applyRecordChange(ctx, opts, secret)
	if err == nil {
		return nil
	}
	return []string{"the key material record was not changed: " + err.Error()}
}

// applyRecordChange stores or forgets this notebook identity's key material as
// the caller explicitly asked. It runs only after remote acceptance.
func (r *Runtime) applyRecordChange(ctx context.Context, opts KeyRecordOptions, secret []byte) error {
	if !opts.StorePassphrase && opts.StorePassphraseFile == "" && !opts.ForgetKeyMaterial {
		return nil
	}
	file, err := keyrecord.Locate(EnvLookup(r.p.env), runtime.GOOS)
	if err != nil {
		return err
	}
	digest := workspace.IdentityDigest(r.cfg.serviceConfig().identity())
	switch {
	case opts.ForgetKeyMaterial:
		return file.Forget(ctx, digest)
	case opts.StorePassphrase:
		entry, err := keyrecord.Passphrase(digest, string(secret))
		if err != nil {
			return err
		}
		return file.Update(ctx, entry)
	default:
		entry, err := keyrecord.PassphraseFile(digest, opts.StorePassphraseFile)
		if err != nil {
			return err
		}
		return file.Update(ctx, entry)
	}
}

// reportOf converts the notebook's encryption status into the command report.
func reportOf(status notebook.EncryptionStatus) EncryptionReport {
	return EncryptionReport{Enabled: status.Enabled, Locked: status.Locked, Doors: status.Doors, Source: status.Source}
}

// replaceDoor returns doors with every door of kind replaced by add, or add
// appended when none is present, and refuses a set outside the supported
// bounds.
func replaceDoor(doors []crypt.Door, kind crypt.DoorType, add *crypt.Door) ([]crypt.Door, error) {
	out := make([]crypt.Door, 0, len(doors)+1)
	for _, door := range doors {
		if door.Type == kind {
			continue
		}
		out = append(out, door)
	}
	if add != nil {
		out = append(out, *add)
	}
	if len(out) == 0 {
		return nil, invalidInput(errDoorRemove)
	}
	if len(out) > maxDoors {
		return nil, doorLimit()
	}
	return out, nil
}

// checkRecordConflict refuses a rekey whose chosen secret would leave the
// stored key record pointing at a different secret, unless the caller stated
// what to do with that record.
func checkRecordConflict(candidates []crypt.Candidate, chosen []byte, opts KeyRecordOptions) error {
	if opts.StorePassphrase || opts.StorePassphraseFile != "" || opts.ForgetKeyMaterial {
		return nil
	}
	for _, candidate := range candidates {
		if candidate.Source != crypt.SourceKeyRecord {
			continue
		}
		if bytes.Equal(candidate.Secret, chosen) {
			return nil
		}
		return &notebook.Error{
			Code:    notebook.CodeStorageFailure,
			Reason:  notebook.ReasonKeyMaterialInvalid,
			Action:  notebook.ActionOperator,
			Message: "the stored key material holds another secret; run again with --store-passphrase --confirm-store-passphrase to replace it, or with --forget-key-material to remove it",
		}
	}
	return nil
}

// staleSourceWarning tells the owner of an environment value or envfile that a
// new passphrase no longer matches it. The record option path is already
// explicit, so only a setting the owner edits himself needs the warning.
func staleSourceWarning(source crypt.CandidateSource) []string {
	switch source {
	case crypt.SourceEnvironment:
		return []string{"update your own SLIVINGDOC_PASSPHRASE: the previous value no longer opens this notebook"}
	case crypt.SourceEnvironmentFile:
		return []string{"update the envfile named by SLIVINGDOC_PASSPHRASE_FILE: its previous value no longer opens this notebook"}
	default:
		return nil
	}
}

// keyRejected reports whether err is the keyring's no-door refusal.
func keyRejected(err error) bool {
	var nb *notebook.Error
	return errors.As(err, &nb) && nb.Reason == notebook.ReasonKeyRejected
}

// promptRefusal classifies an unusable interactive answer. A missing secret is
// the documented KEY_REQUIRED; every other answer failure is invalid input.
func promptRefusal(err error) error {
	if errors.Is(err, errNoSecret) || errors.Is(err, errNoAnswer) {
		return keyRequired()
	}
	return invalidInput(err)
}

// invalidInput builds an INVALID_REQUEST for a refused command line.
func invalidInput(cause error) error {
	return &notebook.Error{
		Code: notebook.CodeInvalidRequest, Reason: notebook.ReasonMalformedInput,
		Action: notebook.ActionFixInput, Message: cause.Error(), Cause: cause,
	}
}

// keyRequired builds the STORAGE_FAILURE of a command with no usable secret.
func keyRequired() error {
	return &notebook.Error{
		Code: notebook.CodeStorageFailure, Reason: notebook.ReasonKeyRequired, Action: notebook.ActionOperator,
		Message: "no passphrase or recovery code was given; run the command at a terminal to be asked, or supply the key material through the environment or the key record",
	}
}

// keyMaterialInvalid builds the STORAGE_FAILURE of unusable key material.
func keyMaterialInvalid(cause error) error {
	return &notebook.Error{
		Code: notebook.CodeStorageFailure, Reason: notebook.ReasonKeyMaterialInvalid, Action: notebook.ActionOperator,
		Message: "the notebook's key material cannot be used; correct the key record or the passphrase file without sharing its value",
		Cause:   cause,
	}
}

// alreadyEnabled builds the refusal of init on an encrypted notebook.
func alreadyEnabled() error {
	return &notebook.Error{
		Code: notebook.CodeInvalidRequest, Reason: notebook.ReasonEncryptionAlreadyEnabled, Action: notebook.ActionFixInput,
		Message: "this notebook is already encrypted; use 'slivingdoc encrypt passwd' or 'rekey', or choose a plaintext notebook",
	}
}

// notEnabled builds the refusal of an operation that needs an encrypted
// notebook.
func notEnabled() error {
	return &notebook.Error{
		Code: notebook.CodeInvalidRequest, Reason: notebook.ReasonEncryptionNotEnabled, Action: notebook.ActionFixInput,
		Message: "this notebook is not encrypted; run 'slivingdoc encrypt init' first",
	}
}

// doorLimit builds the refusal of a door-add request at the cap.
func doorLimit() error {
	return &notebook.Error{
		Code: notebook.CodeInvalidRequest, Reason: notebook.ReasonDoorLimit, Action: notebook.ActionFixInput,
		Message: "this notebook already has the maximum four doors; remove a door before adding another",
	}
}

// DoorNames renders a door list as the status report spells it.
func DoorNames(doors []crypt.Door) []string {
	names := make([]string, 0, len(doors))
	seen := map[crypt.DoorType]bool{}
	for _, door := range doors {
		if seen[door.Type] {
			continue
		}
		seen[door.Type] = true
		switch door.Type {
		case crypt.DoorPassphrase:
			names = append(names, "passphrase")
		case crypt.DoorRecovery:
			names = append(names, "recovery")
		default:
			names = append(names, fmt.Sprintf("type-%d", door.Type))
		}
	}
	return names
}

// SourceName renders a candidate source as the status report spells it.
func SourceName(source crypt.CandidateSource) string {
	switch source {
	case crypt.SourcePrompt:
		return "prompt"
	case crypt.SourceEnvironment:
		return "environment"
	case crypt.SourceEnvironmentFile:
		return "envfile"
	case crypt.SourceKeyRecord:
		return "key record"
	default:
		return "none"
	}
}
