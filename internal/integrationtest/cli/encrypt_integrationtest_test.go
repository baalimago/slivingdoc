package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/keyrecord"
)

// encryptConversionNote is the note content the conversion scenarios publish;
// its at-rest encryption is proven by the notebook-level conversion tests.
const encryptConversionNote = "SECRET-CONVERSION-CANARY-3f57"

// encryptConversionSecret is the passphrase every conversion scenario
// protects the notebook with. No scenario prints its value; the recovery
// scenario only proves that the printed recovery code follows acceptance.
const encryptConversionSecret = "conversion passphrase 8d41"

// encryptConversionEnv is the environment of a spawned encrypt command on the
// hosted reference gateway: the hosted configuration, the cheap Argon2id cost
// the strict package budget can afford, and the passphrase. The canonical
// production cost stays pinned by the internal/crypt unit tests and the
// performance harness (phase-4-command-surface.md, D17).
func encryptConversionEnv(hosted []string, secret string) []string {
	return append(append([]string(nil), hosted...),
		HelperKDFEnv+"=1",
		keyrecord.PassphraseEnv+"="+secret,
	)
}

// TestScenarioEncryptInitRecoveryCodeTiming proves init prints the recovery
// code only after the store accepted the conversion: a refused publication
// prints no code and leaves the notebook plaintext, and an accepted one
// prints the code and an encrypted current a later command opens.
func TestScenarioEncryptInitRecoveryCodeTiming(t *testing.T) {
	t.Parallel()
	g, hosted, root := HostedEnv(t, 1<<20)
	notes := filepath.Join(root, "notes")
	env := encryptConversionEnv(hosted, encryptConversionSecret)
	WriteCLIFile(t, filepath.Join(notes, "a.md"), encryptConversionNote+"\n")

	// A space with no pack room refuses the checkpoint upload, so nothing is
	// published and the caller must not be shown a secret it cannot use.
	g.SetQuota(HostedSpace, 1)
	code, stdout, stderr := RunCLI(t, "real", env, "encrypt", "init", notes)
	if code == 0 {
		t.Fatalf("init on a full space = exit 0 stdout %q, want the refusal", stdout)
	}
	if !strings.Contains(stdout, "STORAGE_FAILURE · STORAGE_FULL") {
		t.Fatalf("refused init stdout = %q, want STORAGE_FAILURE · STORAGE_FULL; stderr: %s", stdout, stderr)
	}
	if strings.Contains(stdout, "recovery code: ") {
		t.Fatalf("the refused init printed a recovery code: %q", stdout)
	}
	if _, out, _ := RunCLI(t, "real", env, "encrypt", "status", notes); !strings.Contains(out, "encryption: off") {
		t.Fatalf("status after the refused init = %q, want the notebook still plaintext", out)
	}

	// Room restored: the conversion publishes, and only then shows the code.
	g.SetQuota(HostedSpace, 1<<20)
	code, stdout, stderr = RunCLI(t, "real", env, "encrypt", "init", notes)
	if code != 0 {
		t.Fatalf("init = exit %d stdout %q stderr %s, want the accepted conversion", code, stdout, stderr)
	}
	if !strings.HasPrefix(stdout, "OK  generation 1") {
		t.Fatalf("init stdout = %q, want the accepted generation-1 report", stdout)
	}
	if !strings.Contains(stdout, "recovery code: ") {
		t.Fatalf("accepted init stdout = %q, want the one-time recovery code", stdout)
	}
	if _, out, _ := RunCLI(t, "real", env, "encrypt", "status", notes); !strings.Contains(out, "encryption: enabled") {
		t.Fatalf("status after the accepted init = %q, want the notebook encrypted", out)
	}
}

// TestScenarioEncryptInitCapacityBoundary proves the conversion observes the
// hosted headroom budget: a checkpoint pack may take the space up to twice
// its quota, so a full plaintext notebook still converts, while a space with
// no such room refuses the conversion and leaves the accepted state
// unchanged.
func TestScenarioEncryptInitCapacityBoundary(t *testing.T) {
	t.Parallel()
	g, hosted, root := HostedEnv(t, 1<<20)
	notes := filepath.Join(root, "notes")
	env := encryptConversionEnv(hosted, encryptConversionSecret)

	// Two generations leave the accepted state smaller than the packs the
	// space already holds: the head holds only the rewritten note, while the
	// first checkpoint still holds the larger one it replaced.
	RunCLIOK(t, "real", env, nil, "pull", notes)
	WriteCLIFile(t, filepath.Join(notes, "a.md"), Incompressible(8192))
	RunCLIOK(t, "real", env, nil, "commit", notes, "-m", "large note")
	WriteCLIFile(t, filepath.Join(notes, "a.md"), Incompressible(4096))
	RunCLIOK(t, "real", env, nil, "commit", notes, "-m", "smaller note")
	stored := g.Stored(HostedSpace)
	if stored == 0 {
		t.Fatal("the seeded notebook stored no pack bytes")
	}

	// No pack room at all: even the doubled checkpoint budget cannot hold the
	// checkpoint, so the conversion is refused and nothing is published.
	g.SetQuota(HostedSpace, 1)
	code, stdout, stderr := RunCLI(t, "real", env, "encrypt", "init", notes)
	if code == 0 {
		t.Fatalf("init with insufficient capacity = exit 0 stdout %q, want the refusal", stdout)
	}
	if !strings.Contains(stdout, "STORAGE_FAILURE · STORAGE_FULL") {
		t.Fatalf("insufficient-capacity init stdout = %q, want STORAGE_FAILURE · STORAGE_FULL; stderr: %s", stdout, stderr)
	}
	if strings.Contains(stdout, "recovery code: ") {
		t.Fatalf("the refused init printed a recovery code: %q", stdout)
	}
	if got := g.Stored(HostedSpace); got != stored {
		t.Fatalf("stored bytes after the refused init = %d, want %d unchanged", got, stored)
	}
	if _, out, _ := RunCLI(t, "real", env, "encrypt", "status", notes); !strings.Contains(out, "encryption: off") {
		t.Fatalf("status after the refused init = %q, want the notebook still plaintext", out)
	}

	// A space exactly at its quota still converts: the checkpoint may take the
	// space up to twice the quota, and the whole-state checkpoint is smaller
	// than the packs the space already holds.
	g.SetQuota(HostedSpace, stored)
	code, stdout, stderr = RunCLI(t, "real", env, "encrypt", "init", notes)
	if code != 0 {
		t.Fatalf("init on a full space = exit %d stdout %q stderr %s, want the accepted conversion", code, stdout, stderr)
	}
	if !strings.HasPrefix(stdout, "OK  generation 3") {
		t.Fatalf("init stdout = %q, want the accepted generation-3 report", stdout)
	}
	if !strings.Contains(stdout, "recovery code: ") {
		t.Fatalf("accepted init stdout = %q, want the one-time recovery code", stdout)
	}
	if _, out, _ := RunCLI(t, "real", env, "encrypt", "status", notes); !strings.Contains(out, "encryption: enabled") {
		t.Fatalf("status after the accepted init = %q, want the notebook encrypted", out)
	}
}

// withoutSecret returns env without the passphrase entry, so the process it
// configures holds no key material at all.
func withoutSecret(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, keyrecord.PassphraseEnv+"=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// TestScenarioEncryptedNotebookProcessKeyring proves a released process
// reads and publishes an encrypted notebook through the key material it
// resolves itself: the passphrase-carrying process commits, a process with
// no secret refuses the same notebook as KEY_REQUIRED and changes nothing,
// and a passphrase-carrying process then reads every published note.
func TestScenarioEncryptedNotebookProcessKeyring(t *testing.T) {
	t.Parallel()
	g, hosted, root := HostedEnv(t, 1<<20)
	notes := filepath.Join(root, "notes")
	env := encryptConversionEnv(hosted, encryptConversionSecret)
	WriteCLIFile(t, filepath.Join(notes, "a.md"), encryptConversionNote+"\n")
	// init prints the one-time recovery code, so the exact success report of
	// RunCLIOK does not apply; its value is never echoed here.
	if code, stdout, stderr := RunCLI(t, "real", env, "encrypt", "init", notes); code != 0 || !strings.HasPrefix(stdout, "OK  generation 1") {
		t.Fatalf("encrypt init = exit %d, want the accepted generation-1 conversion; stderr: %s", code, stderr)
	}

	WriteCLIFile(t, filepath.Join(notes, "a.md"), encryptConversionNote+" edited\n")
	WriteCLIFile(t, filepath.Join(notes, "b.md"), encryptConversionNote+" second\n")
	RunCLIOK(t, "real", env, nil, "commit", notes, "-m", "second")
	stored := g.Stored(HostedSpace)

	// A process that holds no secret cannot read the notebook, and the
	// refusal leaves the stored state byte-identical.
	cold := filepath.Join(root, "cold")
	code, stdout, stderr := RunCLI(t, "real", withoutSecret(env), "pull", cold)
	if code == 0 {
		t.Fatalf("pull without a secret = exit 0 stdout %q, want the KEY_REQUIRED refusal", stdout)
	}
	if !strings.Contains(stdout, "STORAGE_FAILURE · KEY_REQUIRED") {
		t.Fatalf("pull without a secret stdout = %q, want STORAGE_FAILURE · KEY_REQUIRED; stderr: %s", stdout, stderr)
	}
	if got := g.Stored(HostedSpace); got != stored {
		t.Fatalf("stored bytes after the refused pull = %d, want %d unchanged", got, stored)
	}

	// The same directory pulls once the process holds the passphrase.
	RunCLIOK(t, "real", env, nil, "pull", cold)
	for name, want := range map[string]string{
		"a.md": encryptConversionNote + " edited\n",
		"b.md": encryptConversionNote + " second\n",
	} {
		got, err := os.ReadFile(filepath.Join(cold, name))
		if err != nil || string(got) != want {
			t.Fatalf("pulled %s = %q, %v; want %q", name, got, err, want)
		}
	}
}
