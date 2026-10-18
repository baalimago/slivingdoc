package cli_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/cli"
	"github.com/baalimago/slivingdoc/internal/crypt"
	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/git2"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
)

// encryptCommandPrefix is the storage prefix every encrypt-command scenario
// uses; each scenario owns its own fake store, so the value only has to
// satisfy the key grammar.
const encryptCommandPrefix = "encrypt-command"

// The passphrases the scenarios protect a notebook with. Tests assert they
// never reach an output stream.
const (
	encryptCommandSecret = "SECRET-PASSPHRASE-44c1"
	encryptCommandNew    = "SECRET-NEW-PASSPHRASE-7be2"
	encryptCommandWrong  = "SECRET-WRONG-PASSPHRASE-0d9a"
)

// encryptCommandSep is the plain CLI report separator between a code and its
// reason token (app.writeError).
const encryptCommandSep = " · "

// encryptCommandKDF is a deliberately cheap Argon2id cost: the many parallel
// scenarios would otherwise spend the whole test budget deriving canonical
// doors, and the race detector multiplies an Argon2id derivation's cost many
// times over. The scenarios inject it through ProcessOptions, whose gate
// accepts exactly this lowered floor; the canonical cost stays covered by the
// internal/crypt unit tests and the performance harness.
func encryptCommandKDF() *crypt.KDFParams {
	return &crypt.KDFParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1}
}

// encryptCLI drives the `encrypt` command family as a black-box CLI: argv,
// environment and stdin in; exit code, stdout, stderr and stored objects out.
// One deterministic fake store and one configuration directory are shared by
// every invocation, so consecutive commands observe one notebook and one key
// record, exactly as repeated one-shot processes would.
type encryptCLI struct {
	t         *testing.T
	eng       git.Engine
	store     *fake.Store
	root      string
	configDir string
	env       []string
}

// privateDir returns a temporary directory whose mode is exactly 0700, so
// the hardened configuration-file checks see the private directory they
// require regardless of the host's umask.
func privateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("Chmod(%s) = %v", dir, err)
	}
	return dir
}

// newEncryptCLI wires one scenario: private roots, an empty configuration
// directory, and a fake store the caller can inspect.
func newEncryptCLI(t *testing.T) *encryptCLI {
	t.Helper()
	root := privateDir(t)
	configDir := privateDir(t)
	env := []string{
		"SLIVINGDOC_CONFIG_DIR=" + configDir,
		"SLIVINGDOC_WORKSPACE_ROOT=" + root,
		"SLIVINGDOC_PRIVATE_ROOT=" + privateDir(t),
		"SLIVINGDOC_BUCKET=encrypt-bucket",
		"SLIVINGDOC_PREFIX=" + encryptCommandPrefix,
		"NO_COLOR=1",
	}
	return &encryptCLI{t: t, eng: git2.New(), store: fake.New(encryptCommandPrefix), root: root, configDir: configDir, env: env}
}

// run executes one CLI invocation: extra environment entries are appended,
// so a duplicated name wins, and stdin feeds the command's prompts.
func (c *encryptCLI) run(extraEnv []string, stdin string, args ...string) (int, string, string) {
	c.t.Helper()
	var stdout, stderr bytes.Buffer
	opts := app.ProcessOptions{
		Env:       append(slices.Clone(c.env), extraEnv...),
		Cwd:       c.root,
		CacheDir:  c.t.TempDir(),
		Stdout:    &stdout,
		Stderr:    &stderr,
		Stdin:     strings.NewReader(stdin),
		Signals:   make(chan os.Signal, 1),
		KDFParams: encryptCommandKDF(),
		StoreFactory: func(context.Context, app.ServiceConfig) (storage.ObjectStore, error) {
			return c.store, nil
		},
	}
	code := cli.Run(context.Background(), append([]string{"slivingdoc"}, args...), c.eng, opts)
	return code, stdout.String(), stderr.String()
}

// initNotebook runs a successful `encrypt init` and returns its stdout.
func (c *encryptCLI) initNotebook(secret string, args ...string) string {
	c.t.Helper()
	code, out, errOut := c.run(passphraseEnv(secret), "", append([]string{"encrypt", "init"}, args...)...)
	if code != 0 {
		c.t.Fatalf("encrypt init = exit %d stdout %q stderr %q", code, out, errOut)
	}
	return out
}

// writeNote writes one visible note file under the workspace root.
func (c *encryptCLI) writeNote(name, data string) {
	c.t.Helper()
	path := filepath.Join(c.root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		c.t.Fatalf("MkdirAll(%s) = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		c.t.Fatalf("WriteFile(%s) = %v", path, err)
	}
}

// readNote reads one visible note file.
func (c *encryptCLI) readNote(name string) string {
	c.t.Helper()
	data, err := os.ReadFile(filepath.Join(c.root, name))
	if err != nil {
		c.t.Fatalf("ReadFile(%s) = %v", name, err)
	}
	return string(data)
}

// writeEnvfile writes a private envfile that sets the passphrase.
func (c *encryptCLI) writeEnvfile(secret string) string {
	c.t.Helper()
	path := filepath.Join(privateDir(c.t), "passphrase.env")
	if err := os.WriteFile(path, []byte("SLIVINGDOC_PASSPHRASE="+secret+"\n"), 0o600); err != nil {
		c.t.Fatalf("WriteFile(%s) = %v", path, err)
	}
	return path
}

// recordPath is the key record file of this scenario.
func (c *encryptCLI) recordPath() string { return filepath.Join(c.configDir, "notebooks.json") }

// readRecord reads the key record file, an empty slice when none exists.
func (c *encryptCLI) readRecord() []byte {
	c.t.Helper()
	data, err := os.ReadFile(c.recordPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		c.t.Fatalf("ReadFile(%s) = %v", c.recordPath(), err)
	}
	return data
}

// readObject reads one protocol object through the raw store.
func (c *encryptCLI) readObject(key string) ([]byte, error) {
	rc, _, err := c.store.ReadObject(context.Background(), key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(rc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// readCurrent reads the stored current bytes, reporting absence without
// failing the test.
func (c *encryptCLI) readCurrent() ([]byte, bool) {
	c.t.Helper()
	data, err := c.readObject(storage.CurrentKey)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, false
	}
	if err != nil {
		c.t.Fatalf("read current: %v", err)
	}
	return data, true
}

// rawCurrent reads the stored current bytes and fails when the notebook has
// none.
func (c *encryptCLI) rawCurrent() []byte {
	c.t.Helper()
	data, ok := c.readCurrent()
	if !ok {
		c.t.Fatal("the notebook has no current object")
	}
	return data
}

// putCurrent stores current with the caller's exact bytes, creating or
// replacing it as needed.
func (c *encryptCLI) putCurrent(data []byte) {
	c.t.Helper()
	ctx := context.Background()
	rc, info, err := c.store.ReadObject(ctx, storage.CurrentKey)
	if errors.Is(err, storage.ErrNotFound) {
		if _, err := c.store.CreateObject(ctx, storage.CurrentKey, data); err != nil {
			c.t.Fatalf("CreateObject(current) = %v", err)
		}
		return
	}
	if err != nil {
		c.t.Fatalf("read current: %v", err)
	}
	rc.Close()
	if _, err := c.store.ReplaceObject(ctx, storage.CurrentKey, info.ETag, data); err != nil {
		c.t.Fatalf("ReplaceObject(current) = %v", err)
	}
}

// keyring builds a keyring over one passphrase candidate, with a gate that
// accepts the cheap scenario cost.
func (c *encryptCLI) keyring(secret string) *crypt.Keyring {
	c.t.Helper()
	gate, err := crypt.NewKDFGateForCost(1, *encryptCommandKDF())
	if err != nil {
		c.t.Fatalf("NewKDFGateForCost: %v", err)
	}
	ring, err := crypt.NewKeyring([]crypt.Candidate{{Source: crypt.SourcePrompt, Secret: []byte(secret)}}, gate)
	if err != nil {
		c.t.Fatalf("NewKeyring: %v", err)
	}
	return ring
}

// manifest decrypts and decodes the stored current manifest.
func (c *encryptCLI) manifest(secret string) storage.Manifest {
	c.t.Helper()
	encoding, envelope, err := storage.ClassifyCurrent(c.rawCurrent())
	if err != nil {
		c.t.Fatalf("ClassifyCurrent: %v", err)
	}
	if encoding != storage.CurrentEncrypted {
		c.t.Fatalf("stored current is not encrypted: %v", encoding)
	}
	plaintext, _, err := c.keyring(secret).Open(context.Background(), storage.CurrentKey, envelope)
	if err != nil {
		c.t.Fatalf("open current with the passphrase: %v", err)
	}
	m, err := storage.DecodeManifest(plaintext)
	if err != nil {
		c.t.Fatalf("DecodeManifest: %v", err)
	}
	return m
}

// packKeys lists the manifest's referenced pack keys.
func packKeys(m storage.Manifest) []string {
	keys := []string{m.Checkpoint.Key.String()}
	for _, inc := range m.Increments {
		keys = append(keys, inc.Key.String())
	}
	return keys
}

// encryptObjectCount counts stored objects without the probe, whose
// create/delete pair leaves the count unchanged but touches the store.
func (c *encryptCLI) objectCount() int { return c.store.ObjectCount() }

// recoveryCode extracts the recovery phrase one command printed, failing the
// test when the command printed none.
func recoveryCode(t *testing.T, out string) string {
	t.Helper()
	const label = "recovery code: "
	for line := range strings.SplitSeq(out, "\n") {
		if phrase, ok := strings.CutPrefix(line, label); ok {
			if phrase == "" {
				t.Fatal("the recovery code line is empty")
			}
			return phrase
		}
	}
	t.Fatalf("output %q carries no recovery code", out)
	return ""
}

// assertEncryptOK asserts the accepted-publication report at generation.
func assertEncryptOK(t *testing.T, out string, generation uint64) {
	t.Helper()
	want := "OK  generation " + strconv.FormatUint(generation, 10) + "  "
	if !strings.HasPrefix(out, want) {
		t.Fatalf("output = %q, want it to start with %q", out, want)
	}
}

// assertEncryptCode asserts the structured refusal's code and reason token.
func assertEncryptCode(t *testing.T, out, code, reason string) {
	t.Helper()
	if !strings.HasPrefix(out, code+encryptCommandSep+reason) {
		t.Fatalf("refusal = %q, want it to start with %q", out, code+encryptCommandSep+reason)
	}
}

// passphraseEnv is the environment of a command that holds secret.
func passphraseEnv(secret string) []string {
	return []string{"SLIVINGDOC_PASSPHRASE=" + secret}
}

// TestEncryptCommandSurface routes every documented subcommand and asserts
// its documented result: the plaintext and encrypted status reports, the
// refusal of a second init, a passphrase change, a recovery-door replacement,
// and a rekey to a new root generation.
func TestEncryptCommandSurface(t *testing.T) {
	t.Parallel()
	c := newEncryptCLI(t)
	c.writeNote("a.md", "alpha\n")

	if code, out, errOut := c.run(nil, "", "encrypt", "status"); code != 0 || out != "encryption: off\nlocal: plaintext\n" {
		t.Fatalf("status on a plaintext notebook = exit %d stdout %q stderr %q", code, out, errOut)
	}
	if code, _, _ := c.run(nil, "", "encrypt"); code == 0 {
		t.Fatal("a bare encrypt was accepted, want the missing-subcommand refusal")
	}
	if code, _, _ := c.run(nil, "", "encrypt", "bogus"); code == 0 {
		t.Fatal("an unknown encrypt subcommand was accepted")
	}

	initOut := c.initNotebook(encryptCommandSecret)
	assertEncryptOK(t, initOut, 1)
	initRecovery := recoveryCode(t, initOut)

	code, out, _ := c.run(passphraseEnv(encryptCommandSecret), "", "encrypt", "init")
	if code == 0 {
		t.Fatalf("a second init = exit %d stdout %q, want refusal", code, out)
	}
	assertEncryptCode(t, out, "INVALID_REQUEST", "ENCRYPTION_ALREADY_ENABLED")

	code, out, errOut := c.run(passphraseEnv(encryptCommandSecret), "", "encrypt", "status")
	if code != 0 {
		t.Fatalf("status on an encrypted notebook = exit %d stdout %q stderr %q", code, out, errOut)
	}
	for _, want := range []string{
		"encryption: enabled\n",
		"doors: passphrase, recovery\n",
		"kdf: argon2id memory=64KiB iterations=1 parallelism=1\n",
		"source: environment\n",
		"local: plaintext\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("status stdout = %q, want it to contain %q", out, want)
		}
	}

	code, out, errOut = c.run(passphraseEnv(encryptCommandSecret), encryptCommandNew+"\n", "encrypt", "passwd")
	if code != 0 {
		t.Fatalf("passwd = exit %d stdout %q stderr %q", code, out, errOut)
	}
	assertEncryptOK(t, out, 1)

	code, out, _ = c.run(passphraseEnv(encryptCommandSecret), "", "encrypt", "status")
	if code != 0 || !strings.Contains(out, "locked: true") {
		t.Fatalf("status under the replaced passphrase = exit %d stdout %q, want locked", code, out)
	}
	code, out, errOut = c.run(passphraseEnv(encryptCommandNew), "", "encrypt", "status")
	if code != 0 || !strings.Contains(out, "encryption: enabled") {
		t.Fatalf("status under the new passphrase = exit %d stdout %q stderr %q", code, out, errOut)
	}
	if !strings.Contains(out, "doors: ") || !strings.Contains(out, "passphrase") || !strings.Contains(out, "recovery") {
		t.Fatalf("status doors = %q, want the passphrase and recovery doors", out)
	}

	code, out, errOut = c.run(passphraseEnv(encryptCommandNew), "", "encrypt", "recovery")
	if code != 0 {
		t.Fatalf("recovery = exit %d stdout %q stderr %q", code, out, errOut)
	}
	secondRecovery := recoveryCode(t, out)
	if secondRecovery == initRecovery {
		t.Fatal("the recovery command reused the initial recovery code")
	}

	code, out, errOut = c.run(passphraseEnv(encryptCommandNew), "y\n", "encrypt", "rekey")
	if code != 0 {
		t.Fatalf("rekey = exit %d stdout %q stderr %q", code, out, errOut)
	}
	assertEncryptOK(t, out, 2)
	thirdRecovery := recoveryCode(t, out)
	if thirdRecovery == secondRecovery {
		t.Fatal("rekey reused the previous recovery code")
	}

	code, out, _ = c.run(nil, "", "encrypt", "-h")
	if code != 0 {
		t.Fatalf("encrypt -h = exit %d", code)
	}
	for _, want := range []string{"init", "status", "passwd", "recovery", "rekey"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help = %q, want it to name %q", out, want)
		}
	}
}

// TestEncryptCandidateSources proves the resolved candidate order the status
// report labels: the environment value, its envfile, and the key-record value
// each open the notebook alone, the environment value wins over a wrong
// envfile, and a command with no resolved candidate asks the terminal.
func TestEncryptCandidateSources(t *testing.T) {
	t.Parallel()
	c := newEncryptCLI(t)
	c.writeNote("a.md", "alpha\n")
	c.initNotebook(encryptCommandSecret, "--store-passphrase", "--confirm-store-passphrase")

	if _, out, _ := c.run(passphraseEnv(encryptCommandSecret), "", "encrypt", "status"); !strings.Contains(out, "source: environment\n") {
		t.Fatalf("status with an environment value = %q, want the environment source", out)
	}
	if _, out, _ := c.run(nil, "", "encrypt", "status"); !strings.Contains(out, "source: key record\n") {
		t.Fatalf("status with only the key record = %q, want the key record source", out)
	}
	envfile := c.writeEnvfile(encryptCommandSecret)
	if _, out, _ := c.run([]string{"SLIVINGDOC_PASSPHRASE_FILE=" + envfile}, "", "encrypt", "status"); !strings.Contains(out, "source: envfile\n") {
		t.Fatalf("status with only its envfile = %q, want the envfile source", out)
	}
	wrongFile := c.writeEnvfile(encryptCommandWrong)
	_, out, _ := c.run([]string{"SLIVINGDOC_PASSPHRASE=" + encryptCommandSecret, "SLIVINGDOC_PASSPHRASE_FILE=" + wrongFile}, "", "encrypt", "status")
	if !strings.Contains(out, "source: environment\n") {
		t.Fatalf("status with an environment value over a wrong envfile = %q, want the environment source", out)
	}

	prompt := newEncryptCLI(t)
	prompt.writeNote("a.md", "alpha\n")
	prompt.initNotebook(encryptCommandSecret)
	code, out, errOut := prompt.run(nil, encryptCommandSecret+"\n"+encryptCommandNew+"\n", "encrypt", "passwd")
	if code != 0 {
		t.Fatalf("passwd with only a prompt = exit %d stdout %q stderr %q", code, out, errOut)
	}
	if code, out, _ := prompt.run(passphraseEnv(encryptCommandNew), "", "encrypt", "status"); code != 0 || !strings.Contains(out, "encryption: enabled") {
		t.Fatalf("status after a prompted passwd = exit %d stdout %q", code, out)
	}
}

// TestEncryptRekeyCredentialChoice proves rekey offers both credential
// choices: keeping the unlocking secret leaves it working, and entering a new
// passphrase replaces it. Either choice mints a new recovery code and
// replaces the previous door set.
func TestEncryptRekeyCredentialChoice(t *testing.T) {
	t.Parallel()
	c := newEncryptCLI(t)
	c.writeNote("a.md", "alpha\n")
	initRecovery := recoveryCode(t, c.initNotebook(encryptCommandSecret))

	code, out, errOut := c.run(passphraseEnv(encryptCommandSecret), "y\n", "encrypt", "rekey")
	if code != 0 {
		t.Fatalf("rekey keeping the secret = exit %d stdout %q stderr %q", code, out, errOut)
	}
	assertEncryptOK(t, out, 2)
	keepRecovery := recoveryCode(t, out)
	if keepRecovery == initRecovery {
		t.Fatal("rekey keeping the secret reused the initial recovery code")
	}
	if _, out, _ := c.run(passphraseEnv(encryptCommandSecret), "", "encrypt", "status"); !strings.Contains(out, "encryption: enabled") {
		t.Fatalf("the kept secret no longer opens the notebook: %q", out)
	}

	code, out, errOut = c.run(passphraseEnv(encryptCommandSecret), "n\n"+encryptCommandNew+"\n", "encrypt", "rekey")
	if code != 0 {
		t.Fatalf("rekey entering a new passphrase = exit %d stdout %q stderr %q", code, out, errOut)
	}
	assertEncryptOK(t, out, 3)
	newRecovery := recoveryCode(t, out)
	if newRecovery == keepRecovery {
		t.Fatal("rekey entering a new passphrase reused the previous recovery code")
	}

	if _, out, _ := c.run(passphraseEnv(encryptCommandSecret), "", "encrypt", "status"); !strings.Contains(out, "locked: true") {
		t.Fatalf("the replaced secret still opens the notebook: %q", out)
	}
	if _, out, _ := c.run(passphraseEnv(encryptCommandNew), "", "encrypt", "status"); !strings.Contains(out, "encryption: enabled") {
		t.Fatalf("the new secret does not open the notebook: %q", out)
	}
	if _, out, _ := c.run(passphraseEnv(initRecovery), "", "encrypt", "status"); !strings.Contains(out, "locked: true") {
		t.Fatalf("a rekey replaced recovery code still opens the notebook: %q", out)
	}
}

// TestEncryptPasswdChangesHeaderOnly proves a passphrase change rewrites only
// the encrypted current header: the referenced packs, their count and the
// manifest stay byte-identical, and the recovery door survives.
func TestEncryptPasswdChangesHeaderOnly(t *testing.T) {
	t.Parallel()
	c := newEncryptCLI(t)
	c.writeNote("a.md", "alpha\n")
	initOut := c.initNotebook(encryptCommandSecret)
	initRecovery := recoveryCode(t, initOut)

	before := c.manifest(encryptCommandSecret)
	beforeCurrent := c.rawCurrent()
	beforeCount := c.objectCount()

	code, out, errOut := c.run(passphraseEnv(encryptCommandSecret), encryptCommandNew+"\n", "encrypt", "passwd")
	if code != 0 {
		t.Fatalf("passwd = exit %d stdout %q stderr %q", code, out, errOut)
	}
	assertEncryptOK(t, out, 1)

	after := c.manifest(encryptCommandNew)
	if after.Generation != before.Generation {
		t.Fatalf("generation = %d, want %d unchanged by a door change", after.Generation, before.Generation)
	}
	if after.Checkpoint != before.Checkpoint {
		t.Fatal("the checkpoint descriptor changed under a door change")
	}
	if !slices.Equal(packKeys(after), packKeys(before)) {
		t.Fatalf("pack keys = %v, want %v unchanged", packKeys(after), packKeys(before))
	}
	if got := c.objectCount(); got != beforeCount {
		t.Fatalf("stored objects = %d, want %d (no pack may move for a door change)", got, beforeCount)
	}
	if bytes.Equal(beforeCurrent, c.rawCurrent()) {
		t.Fatal("the stored current header did not change")
	}
	if _, out, _ := c.run(passphraseEnv(initRecovery), "", "encrypt", "status"); !strings.Contains(out, "encryption: enabled") {
		t.Fatalf("the untouched recovery door no longer opens the notebook: %q", out)
	}
	if _, out, _ := c.run(passphraseEnv(encryptCommandSecret), "", "encrypt", "status"); !strings.Contains(out, "locked: true") {
		t.Fatalf("the replaced passphrase still opens the notebook: %q", out)
	}
}

// TestEncryptStaleSourceWarning proves a passphrase change warns the owner
// of an environment value or an envfile that its previous value no longer
// opens the notebook.
func TestEncryptStaleSourceWarning(t *testing.T) {
	t.Parallel()

	env := newEncryptCLI(t)
	env.writeNote("a.md", "alpha\n")
	env.initNotebook(encryptCommandSecret)
	code, out, errOut := env.run(passphraseEnv(encryptCommandSecret), encryptCommandNew+"\n", "encrypt", "passwd")
	if code != 0 {
		t.Fatalf("passwd with an environment value = exit %d stdout %q stderr %q", code, out, errOut)
	}
	if !strings.Contains(errOut, "warning:") || !strings.Contains(errOut, "SLIVINGDOC_PASSPHRASE:") {
		t.Fatalf("stderr = %q, want the stale environment-value warning", errOut)
	}

	file := newEncryptCLI(t)
	file.writeNote("a.md", "alpha\n")
	file.initNotebook(encryptCommandSecret)
	envfile := file.writeEnvfile(encryptCommandSecret)
	code, out, errOut = file.run([]string{"SLIVINGDOC_PASSPHRASE_FILE=" + envfile}, encryptCommandNew+"\n", "encrypt", "passwd")
	if code != 0 {
		t.Fatalf("passwd with an envfile = exit %d stdout %q stderr %q", code, out, errOut)
	}
	if !strings.Contains(errOut, "warning:") || !strings.Contains(errOut, "SLIVINGDOC_PASSPHRASE_FILE") {
		t.Fatalf("stderr = %q, want the stale envfile warning", errOut)
	}
}

// TestEncryptStatusIsReadOnly proves status leaves accepted state and local
// files unchanged, for a plaintext and for an encrypted notebook.
func TestEncryptStatusIsReadOnly(t *testing.T) {
	t.Parallel()
	plain := newEncryptCLI(t)
	plain.writeNote("a.md", "alpha\n")
	plainCount := plain.objectCount()
	if code, out, _ := plain.run(nil, "", "encrypt", "status"); code != 0 || out != "encryption: off\nlocal: plaintext\n" {
		t.Fatalf("status on a plaintext notebook = exit %d stdout %q", code, out)
	}
	if _, ok := plain.readCurrent(); ok {
		t.Fatal("status created a current object")
	}
	if got := plain.objectCount(); got != plainCount {
		t.Fatalf("status changed the stored object count: %d, want %d", got, plainCount)
	}

	c := newEncryptCLI(t)
	c.writeNote("a.md", "alpha\n")
	c.initNotebook(encryptCommandSecret)
	before := c.rawCurrent()
	beforeCount := c.objectCount()
	if code, out, errOut := c.run(passphraseEnv(encryptCommandSecret), "", "encrypt", "status"); code != 0 || !strings.Contains(out, "encryption: enabled") {
		t.Fatalf("status on an encrypted notebook = exit %d stdout %q stderr %q", code, out, errOut)
	}
	if got := c.objectCount(); got != beforeCount {
		t.Fatalf("status changed the stored object count: %d, want %d", got, beforeCount)
	}
	if !bytes.Equal(before, c.rawCurrent()) {
		t.Fatal("status changed the accepted current")
	}
	if c.readNote("a.md") != "alpha\n" {
		t.Fatal("status changed a visible note")
	}
}

// TestEncryptKeyRecordOptions proves the store, envfile, forget and
// confirmation rules: plaintext at rest needs the explicit confirmation, the
// envfile variant stores only the path, forget removes the entry, and an
// invalid combination is refused before any mutation.
func TestEncryptKeyRecordOptions(t *testing.T) {
	t.Parallel()

	t.Run("store requires confirmation", func(t *testing.T) {
		c := newEncryptCLI(t)
		c.writeNote("a.md", "alpha\n")
		code, _, _ := c.run(passphraseEnv(encryptCommandSecret), "", "encrypt", "init", "--store-passphrase")
		if code == 0 {
			t.Fatal("--store-passphrase without --confirm-store-passphrase was accepted")
		}
		if _, ok := c.readCurrent(); ok {
			t.Fatal("the refused init published a current object")
		}
		if len(c.readRecord()) != 0 {
			t.Fatal("the refused init wrote the key record")
		}
	})

	t.Run("store and forget", func(t *testing.T) {
		c := newEncryptCLI(t)
		c.writeNote("a.md", "alpha\n")
		code, out, errOut := c.run(passphraseEnv(encryptCommandSecret), "", "encrypt", "init", "--store-passphrase", "--confirm-store-passphrase")
		if code != 0 {
			t.Fatalf("encrypt init = exit %d stdout %q stderr %q", code, out, errOut)
		}
		if record := c.readRecord(); !bytes.Contains(record, []byte(encryptCommandSecret)) {
			t.Fatalf("key record = %q (stderr %q), want it to store the passphrase", record, errOut)
		}
		if _, out, _ := c.run(nil, "", "encrypt", "status"); !strings.Contains(out, "source: key record\n") {
			t.Fatalf("status = %q, want the key record source", out)
		}
		code, out, errOut = c.run(nil, "\n", "encrypt", "passwd", "--forget-key-material")
		if code != 0 {
			t.Fatalf("passwd --forget-key-material = exit %d stdout %q stderr %q", code, out, errOut)
		}
		if record := c.readRecord(); bytes.Contains(record, []byte(encryptCommandSecret)) {
			t.Fatalf("key record = %q, want the entry removed", record)
		}
		if _, out, _ := c.run(nil, "", "encrypt", "status"); !strings.Contains(out, "locked: true") {
			t.Fatalf("status after forgetting = %q, want locked", out)
		}
	})

	t.Run("store passphrase file", func(t *testing.T) {
		c := newEncryptCLI(t)
		c.writeNote("a.md", "alpha\n")
		envfile := c.writeEnvfile(encryptCommandSecret)
		c.initNotebook(encryptCommandSecret, "--store-passphrase-file", envfile)
		record := c.readRecord()
		if !bytes.Contains(record, []byte(envfile)) {
			t.Fatalf("key record = %q, want it to store the envfile path", record)
		}
		if bytes.Contains(record, []byte(encryptCommandSecret)) {
			t.Fatalf("key record = %q, want the envfile variant to store no secret", record)
		}
		if _, out, _ := c.run(nil, "", "encrypt", "status"); !strings.Contains(out, "source: key record\n") {
			t.Fatalf("status = %q, want the key record source", out)
		}
	})

	t.Run("invalid combinations", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			args []string
		}{
			{"exclusive store options", []string{"encrypt", "init", "--store-passphrase", "--confirm-store-passphrase", "--store-passphrase-file", "x"}},
			{"forget with store", []string{"encrypt", "init", "--store-passphrase", "--confirm-store-passphrase", "--forget-key-material"}},
			{"store on status", []string{"encrypt", "status", "--store-passphrase", "--confirm-store-passphrase"}},
			{"forget on init", []string{"encrypt", "init", "--forget-key-material"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				c := newEncryptCLI(t)
				c.writeNote("a.md", "alpha\n")
				code, out, _ := c.run(passphraseEnv(encryptCommandSecret), "", tc.args...)
				if code == 0 {
					t.Fatalf("%v = exit 0 stdout %q, want refusal", tc.args, out)
				}
				if _, ok := c.readCurrent(); ok {
					t.Fatalf("%v published a current object", tc.args)
				}
				if len(c.readRecord()) != 0 {
					t.Fatalf("%v wrote the key record", tc.args)
				}
			})
		}
	})
}

// TestEncryptErrorEnvelope proves the encryption commands use the existing
// error codes with the documented stable reason, action and retryable value.
func TestEncryptErrorEnvelope(t *testing.T) {
	t.Parallel()

	encrypted := func(t *testing.T) *encryptCLI {
		c := newEncryptCLI(t)
		c.writeNote("a.md", "alpha\n")
		c.initNotebook(encryptCommandSecret)
		return c
	}

	t.Run("key rejected", func(t *testing.T) {
		c := encrypted(t)
		code, out, _ := c.run(passphraseEnv(encryptCommandWrong), encryptCommandNew+"\n", "encrypt", "passwd")
		if code == 0 {
			t.Fatal("a wrong passphrase was accepted")
		}
		assertEncryptCode(t, out, "STORAGE_FAILURE", "KEY_REJECTED")
		if !strings.Contains(out, "retryable: false") || !strings.Contains(out, "operator attention needed") {
			t.Fatalf("refusal = %q, want the non-retryable operator next step", out)
		}
	})

	t.Run("key required", func(t *testing.T) {
		c := encrypted(t)
		code, out, _ := c.run(nil, "", "encrypt", "passwd")
		if code == 0 {
			t.Fatal("a command with no secret was accepted")
		}
		assertEncryptCode(t, out, "STORAGE_FAILURE", "KEY_REQUIRED")
		if !strings.Contains(out, "retryable: false") {
			t.Fatalf("refusal = %q, want the non-retryable trailer", out)
		}
	})

	t.Run("not enabled", func(t *testing.T) {
		c := newEncryptCLI(t)
		c.writeNote("a.md", "alpha\n")
		code, out, _ := c.run(passphraseEnv(encryptCommandSecret), encryptCommandNew+"\n", "encrypt", "passwd")
		if code == 0 {
			t.Fatal("passwd on a plaintext notebook was accepted")
		}
		assertEncryptCode(t, out, "INVALID_REQUEST", "ENCRYPTION_NOT_ENABLED")
	})

	t.Run("upgrade required", func(t *testing.T) {
		c := encrypted(t)
		c.putCurrent([]byte(`{"version":3,"envelope":""}`))
		code, out, _ := c.run(passphraseEnv(encryptCommandSecret), "", "encrypt", "status")
		if code == 0 {
			t.Fatal("a newer wrapper was accepted")
		}
		assertEncryptCode(t, out, "STORAGE_FAILURE", "UPGRADE_REQUIRED")
	})

	t.Run("envelope invalid", func(t *testing.T) {
		c := encrypted(t)
		_, envelope, err := storage.ClassifyCurrent(c.rawCurrent())
		if err != nil {
			t.Fatalf("ClassifyCurrent: %v", err)
		}
		envelope[len(envelope)-1] ^= 0xff
		wrapped, err := storage.WrapCurrentEnvelope(envelope)
		if err != nil {
			t.Fatalf("WrapCurrentEnvelope: %v", err)
		}
		c.putCurrent(wrapped)
		code, out, _ := c.run(passphraseEnv(encryptCommandSecret), "", "encrypt", "status")
		if code == 0 {
			t.Fatal("a tampered envelope was accepted")
		}
		assertEncryptCode(t, out, "STORAGE_INTEGRITY", "ENVELOPE_INVALID")
		if !strings.Contains(out, "retryable: false") {
			t.Fatalf("refusal = %q, want the non-retryable trailer", out)
		}
	})
}

// TestEncryptCommandDoesNotLeakSecrets proves no encrypt command prints a
// passphrase or an envfile path, on success or on failure.
func TestEncryptCommandDoesNotLeakSecrets(t *testing.T) {
	t.Parallel()
	c := newEncryptCLI(t)
	c.writeNote("a.md", "alpha\n")
	envfile := c.writeEnvfile(encryptCommandSecret)

	var captured strings.Builder
	invoke := func(env []string, stdin string, args ...string) {
		_, out, errOut := c.run(env, stdin, args...)
		captured.WriteString(out)
		captured.WriteString(errOut)
	}
	invoke(passphraseEnv(encryptCommandSecret), "", "encrypt", "init", "--store-passphrase-file", envfile)
	invoke(passphraseEnv(encryptCommandSecret), "", "encrypt", "status")
	invoke(passphraseEnv(encryptCommandSecret), encryptCommandNew+"\n", "encrypt", "passwd")
	invoke(passphraseEnv(encryptCommandNew), "", "encrypt", "recovery")
	invoke(passphraseEnv(encryptCommandNew), "y\n", "encrypt", "rekey")
	invoke(passphraseEnv(encryptCommandSecret), "", "encrypt", "init")
	invoke(passphraseEnv(encryptCommandSecret), "", "encrypt", "status")

	all := captured.String()
	for _, secret := range []string{encryptCommandSecret, encryptCommandNew} {
		if strings.Contains(all, secret) {
			t.Fatalf("command output leaked a secret: %q", all)
		}
	}
	if strings.Contains(all, envfile) {
		t.Fatalf("command output leaked the envfile path: %q", all)
	}
}

// TestEncryptRekeyPolicy proves a rekey refuses unresolved conflict markers
// and a change under the configured read-only policy before it publishes a
// new key, restoring the protected path from the accepted baseline.
func TestEncryptRekeyPolicy(t *testing.T) {
	t.Parallel()
	c := newEncryptCLI(t)
	c.writeNote("protected.md", "original\n")
	c.writeNote("busy.md", "busy\n")
	c.initNotebook(encryptCommandSecret)
	before := c.rawCurrent()

	c.writeNote("protected.md", "changed\n")
	code, out, _ := c.run(passphraseEnv(encryptCommandSecret), "y\n", "encrypt", "rekey", "--read-only-paths", "protected.md")
	if code == 0 {
		t.Fatalf("rekey with a protected-path change = exit 0 stdout %q, want refusal", out)
	}
	assertEncryptCode(t, out, "INVALID_REQUEST", "READ_ONLY_PATH")
	if got := c.readNote("protected.md"); got != "original\n" {
		t.Fatalf("protected.md = %q, want it restored to the baseline", got)
	}
	if !bytes.Equal(before, c.rawCurrent()) {
		t.Fatal("the refused rekey changed the accepted current")
	}

	c.writeNote("busy.md", "<<<<<<< local\nours\n=======\ntheirs\n>>>>>>> remote\n")
	code, out, _ = c.run(passphraseEnv(encryptCommandSecret), "y\n", "encrypt", "rekey")
	if code == 0 {
		t.Fatalf("rekey with unresolved markers = exit 0 stdout %q, want refusal", out)
	}
	assertEncryptCode(t, out, "CONTENT_CONFLICT", "UNRESOLVED_MARKERS")
	if !bytes.Equal(before, c.rawCurrent()) {
		t.Fatal("the refused rekey changed the accepted current")
	}
}

// TestEncryptRekeyKeyRecordOptions proves the record rules for rekey: a new
// passphrase that conflicts with the stored record is refused before
// publication, an explicit store option resolves it, and a refused rekey
// never changes the record.
func TestEncryptRekeyKeyRecordOptions(t *testing.T) {
	t.Parallel()
	c := newEncryptCLI(t)
	c.writeNote("a.md", "alpha\n")
	c.initNotebook(encryptCommandSecret, "--store-passphrase", "--confirm-store-passphrase")
	before := c.rawCurrent()

	code, out, _ := c.run(passphraseEnv(encryptCommandSecret), "n\n"+encryptCommandNew+"\n", "encrypt", "rekey")
	if code == 0 {
		t.Fatalf("rekey conflicting with the record = exit 0 stdout %q, want refusal", out)
	}
	assertEncryptCode(t, out, "STORAGE_FAILURE", "KEY_MATERIAL_INVALID")
	if !bytes.Equal(before, c.rawCurrent()) {
		t.Fatal("the refused rekey changed the accepted current")
	}
	if record := c.readRecord(); !bytes.Contains(record, []byte(encryptCommandSecret)) {
		t.Fatalf("key record = %q, want the refused rekey to leave it unchanged", record)
	}

	code, out, errOut := c.run(passphraseEnv(encryptCommandSecret), "n\n"+encryptCommandNew+"\n", "encrypt", "rekey", "--store-passphrase", "--confirm-store-passphrase")
	if code != 0 {
		t.Fatalf("rekey storing the new passphrase = exit %d stdout %q stderr %q", code, out, errOut)
	}
	assertEncryptOK(t, out, 2)
	record := c.readRecord()
	if !bytes.Contains(record, []byte(encryptCommandNew)) || bytes.Contains(record, []byte(encryptCommandSecret)) {
		t.Fatalf("key record = %q, want it to hold only the new passphrase", record)
	}
}

// TestEncryptRekeyRecordFailureAfterAcceptance proves a key-record update
// failure does not hide the accepted rekey or its recovery code.
func TestEncryptRekeyRecordFailureAfterAcceptance(t *testing.T) {
	t.Parallel()
	c := newEncryptCLI(t)
	c.writeNote("a.md", "alpha\n")
	c.initNotebook(encryptCommandSecret)
	// A directory where the record lock file belongs makes the post-acceptance
	// record write fail deterministically, without touching the read path.
	if err := os.Mkdir(filepath.Join(c.configDir, "notebooks.lock"), 0o700); err != nil {
		t.Fatalf("Mkdir(notebooks.lock) = %v", err)
	}

	code, out, errOut := c.run(passphraseEnv(encryptCommandSecret), "y\n", "encrypt", "rekey", "--store-passphrase", "--confirm-store-passphrase")
	if code != 0 {
		t.Fatalf("rekey = exit %d stdout %q stderr %q, want the accepted rekey", code, out, errOut)
	}
	assertEncryptOK(t, out, 2)
	recoveryCode(t, out)
	if !strings.Contains(errOut, "warning:") || !strings.Contains(errOut, "key material record was not changed") {
		t.Fatalf("stderr = %q, want the key-record warning", errOut)
	}
	if _, out, _ := c.run(passphraseEnv(encryptCommandSecret), "", "encrypt", "status"); !strings.Contains(out, "encryption: enabled") {
		t.Fatalf("the accepted rekey does not open the notebook: %q", out)
	}
}

// TestEncryptPasswdContention proves a lost conditional header replacement
// returns the existing busy result without rewriting a pack and without
// changing accepted state.
func TestEncryptPasswdContention(t *testing.T) {
	t.Parallel()
	c := newEncryptCLI(t)
	c.writeNote("a.md", "alpha\n")
	c.initNotebook(encryptCommandSecret)
	before := c.rawCurrent()
	beforeCount := c.objectCount()

	c.store.FailNextKey(fake.OpReplace, storage.CurrentKey, storage.ErrPreconditionFailed)
	code, out, _ := c.run(passphraseEnv(encryptCommandSecret), encryptCommandNew+"\n", "encrypt", "passwd", "--commit-retries", "0")
	if code == 0 {
		t.Fatalf("passwd under a lost CAS = exit 0 stdout %q, want refusal", out)
	}
	assertEncryptCode(t, out, "REMOTE_BUSY", "RETRIES_EXHAUSTED")
	if !strings.Contains(out, "retryable: true") {
		t.Fatalf("refusal = %q, want the retryable trailer", out)
	}
	if got := c.objectCount(); got != beforeCount {
		t.Fatalf("stored objects = %d, want %d (a door change uploads no pack)", got, beforeCount)
	}
	if !bytes.Equal(before, c.rawCurrent()) {
		t.Fatal("the lost CAS changed the accepted current")
	}
}

// TestEncryptUnsafeKeyRecord proves an unsafe key record is a typed startup
// refusal that changes no accepted state: the process resolves the key
// material before any notebook operation, and the refusal names no path.
func TestEncryptUnsafeKeyRecord(t *testing.T) {
	t.Parallel()
	c := newEncryptCLI(t)
	c.writeNote("a.md", "alpha\n")
	c.initNotebook(encryptCommandSecret)
	before := c.rawCurrent()

	target := filepath.Join(c.configDir, "elsewhere.json")
	if err := os.WriteFile(target, []byte(`{"version":1,"notebooks":{}}`), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) = %v", target, err)
	}
	if err := os.Symlink(target, c.recordPath()); err != nil {
		t.Fatalf("Symlink(%s) = %v", c.recordPath(), err)
	}

	code, out, errOut := c.run(nil, "", "encrypt", "status")
	if code == 0 {
		t.Fatalf("status with an unsafe record = exit 0 stdout %q, want refusal", out)
	}
	if out != "" {
		t.Fatalf("status with an unsafe record stdout = %q, want the refusal on stderr", out)
	}
	if !strings.Contains(errOut, "error: STORAGE_FAILURE · KEY_MATERIAL_INVALID") {
		t.Fatalf("status with an unsafe record stderr = %q, want the key-material refusal", errOut)
	}
	if strings.Contains(errOut, "notebooks.json") {
		t.Fatalf("the refusal names the record file: %q", errOut)
	}
	if !bytes.Equal(before, c.rawCurrent()) {
		t.Fatal("the unsafe-record refusal changed the accepted current")
	}
}

// TestEncryptUnlockRefusals proves the documented unlocks: the recovery code
// opens its door, an incorrect secret is refused as KEY_REJECTED, and a
// missing secret is refused as KEY_REQUIRED.
func TestEncryptUnlockRefusals(t *testing.T) {
	t.Parallel()
	c := newEncryptCLI(t)
	c.writeNote("a.md", "alpha\n")
	initRecovery := recoveryCode(t, c.initNotebook(encryptCommandSecret))

	if code, out, _ := c.run(passphraseEnv(initRecovery), "", "encrypt", "status"); code != 0 || !strings.Contains(out, "encryption: enabled") {
		t.Fatalf("status with the recovery code = exit %d stdout %q, want it unlocked", code, out)
	}
	if code, out, _ := c.run(passphraseEnv(encryptCommandWrong), encryptCommandNew+"\n", "encrypt", "passwd"); code == 0 {
		t.Fatalf("passwd with a wrong secret = exit 0 stdout %q, want refusal", out)
	} else {
		assertEncryptCode(t, out, "STORAGE_FAILURE", "KEY_REJECTED")
	}
	if code, out, _ := c.run(nil, "", "encrypt", "passwd"); code == 0 {
		t.Fatalf("passwd with no secret = exit 0 stdout %q, want refusal", out)
	} else {
		assertEncryptCode(t, out, "STORAGE_FAILURE", "KEY_REQUIRED")
	}
}
