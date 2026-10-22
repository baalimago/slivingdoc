//go:build linux

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/credentials"
)

// TestScenarioSecretPromptOnTerminal proves the terminal ask of the one-shot
// commands: a process that resolved no passphrase itself asks for one when the
// stored notebook is encrypted, and pulls and commits the notebook with the
// typed answer, which stays in memory for that run. An empty answer keeps the
// KEY_REQUIRED refusal and writes nothing.
func TestScenarioSecretPromptOnTerminal(t *testing.T) {
	t.Parallel()
	_, hosted, root := HostedEnv(t, 1<<20)
	notes := filepath.Join(root, "notes")
	env := encryptConversionEnv(hosted, encryptConversionSecret)
	WriteCLIFile(t, filepath.Join(notes, "a.md"), encryptConversionNote+"\n")
	if code, stdout, stderr := RunCLI(t, "real", env, "encrypt", "init", notes); code != 0 {
		t.Fatalf("encrypt init = exit %d, want the accepted conversion; stdout: %q stderr: %s", code, stdout, stderr)
	}
	// No passphrase in the environment and an empty key record, so the process
	// has the terminal as its only secret source.
	secretless := withoutSecret(append(env, credentials.DirEnv+"="+PrivateTempDir(t)))

	// redacted keeps the typed answer, which the terminal echoes, out of a
	// failure message.
	redacted := func(plain string) string {
		return strings.ReplaceAll(plain, encryptConversionSecret, "<secret>")
	}

	// maskedPrompt is the prompt followed by one star per typed character. A
	// terminal that shows it, and never the answer itself, has masked the
	// passphrase as it was typed.
	maskedPrompt := "Passphrase or recovery code: " + strings.Repeat("*", len(encryptConversionSecret))
	assertMasked := func(t *testing.T, out string) {
		t.Helper()
		plain := StripANSI(out)
		if !strings.Contains(plain, maskedPrompt) {
			t.Fatalf("terminal output = %q, want %q", redacted(plain), maskedPrompt)
		}
		if strings.Contains(plain, encryptConversionSecret) {
			t.Fatal("the terminal output echoed the typed passphrase")
		}
	}

	t.Run("pull and commit are asked", func(t *testing.T) {
		cold := filepath.Join(root, "cold-typed")
		code, out := RunCLIOnTypedTTY(t, "real", secretless, []TtyStep{
			{After: "Passphrase or recovery code: ", Typed: encryptConversionSecret + "\n"},
		}, "pull", cold)
		if code != 0 {
			t.Fatalf("pull on a terminal = exit %d, want 0; output %q", code, redacted(StripANSI(out)))
		}
		assertMasked(t, out)
		if !strings.Contains(StripANSI(out), "OK  generation 1") {
			t.Fatalf("pull on a terminal = %q, want the accepted report", redacted(StripANSI(out)))
		}
		got, err := os.ReadFile(filepath.Join(cold, "a.md"))
		if err != nil || string(got) != encryptConversionNote+"\n" {
			t.Fatalf("pulled a.md = %q, %v; want the published note", got, err)
		}

		WriteCLIFile(t, filepath.Join(cold, "a.md"), encryptConversionNote+" edited\n")
		code, out = RunCLIOnTypedTTY(t, "real", secretless, []TtyStep{
			{After: "Passphrase or recovery code: ", Typed: encryptConversionSecret + "\n"},
		}, "commit", cold, "-m", "typed passphrase")
		if code != 0 {
			t.Fatalf("commit on a terminal = exit %d, want 0; output %q", code, redacted(StripANSI(out)))
		}
		assertMasked(t, out)
		if !strings.Contains(StripANSI(out), "OK  generation 2") {
			t.Fatalf("commit on a terminal = %q, want the accepted generation-2 report", redacted(StripANSI(out)))
		}
	})

	t.Run("a wrong answer is refused", func(t *testing.T) {
		cold := filepath.Join(root, "cold-wrong")
		wrong := "not the passphrase 3f2a"
		code, out := RunCLIOnTypedTTY(t, "real", secretless, []TtyStep{
			{After: "Passphrase or recovery code: ", Typed: wrong + "\n"},
		}, "pull", cold)
		if code == 0 {
			t.Fatalf("pull answered with another passphrase = exit 0, want the refusal; output %q", redacted(StripANSI(out)))
		}
		plain := StripANSI(out)
		if !strings.Contains(plain, strings.Repeat("*", len(wrong))) {
			t.Fatalf("pull answered with another passphrase = %q, want one mask per typed character", strings.ReplaceAll(plain, wrong, "<secret>"))
		}
		if strings.Contains(plain, wrong) {
			t.Fatal("the terminal output echoed the typed passphrase")
		}
		if !strings.Contains(plain, "STORAGE_FAILURE · KEY_REJECTED") {
			t.Fatalf("pull answered with another passphrase = %q, want STORAGE_FAILURE · KEY_REJECTED", redacted(plain))
		}
		if _, err := os.Stat(filepath.Join(cold, "a.md")); err == nil {
			t.Fatal("the refused pull wrote the note")
		}
	})

	t.Run("an empty answer keeps the refusal", func(t *testing.T) {
		cold := filepath.Join(root, "cold-empty")
		code, out := RunCLIOnTypedTTY(t, "real", secretless, []TtyStep{
			{After: "Passphrase or recovery code: ", Typed: "\n"},
		}, "pull", cold)
		if code == 0 {
			t.Fatalf("pull answered with nothing = exit 0, want the refusal; output %q", redacted(StripANSI(out)))
		}
		plain := StripANSI(out)
		if !strings.Contains(plain, "Passphrase or recovery code: \r\n") {
			t.Fatalf("pull answered with nothing = %q, want the prompt with no mask and the line end", redacted(plain))
		}
		if !strings.Contains(plain, "STORAGE_FAILURE · KEY_REQUIRED") {
			t.Fatalf("pull answered with nothing = %q, want STORAGE_FAILURE · KEY_REQUIRED", redacted(plain))
		}
		if _, err := os.Stat(filepath.Join(cold, "a.md")); err == nil {
			t.Fatal("the refused pull wrote the note")
		}
	})
}
