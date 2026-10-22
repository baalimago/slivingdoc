//go:build linux

package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"
)

// TestScenarioCLIColourOnTerminal proves the process-level colour
// contract: the one-shot CLI renders ANSI when its stdout is a real
// terminal (a pseudo-terminal). The piped scenarios prove the plain side
// of the gate, and the unit-level colour gate tests prove that NO_COLOR
// disables a terminal; this scenario closes the loop with a genuine
// character device and the exact byte output.
func TestScenarioCLIColourOnTerminal(t *testing.T) {
	t.Parallel()

	t.Run("success report", func(t *testing.T) {
		t.Parallel()
		env, root := CliRoots(t)
		notes := filepath.Join(root, "notes")
		code, stdout := RunCLITTY(t, "fake", env, "pull", notes)
		if code != 0 {
			t.Fatalf("pull on a terminal = exit %d, want 0; stdout: %q", code, stdout)
		}
		want := "\x1b[32m✓\x1b[0m \x1b[32mOK\x1b[0m  \x1b[34mgeneration 0\x1b[0m  " + notes + "\n" +
			"\x1b[2m0 files changed, 0 insertions(+), 0 deletions(-)\x1b[0m\n"
		if stdout != want {
			t.Fatalf("pull on a terminal stdout = %q, want the ANSI report %q", stdout, want)
		}
	})

	// The read-only refusal on a pseudo-terminal carries the plain bytes
	// proven by TestScenarioCLIReadOnlyCommit, with the terminal's marks.
	t.Run("read-only refusal report", func(t *testing.T) {
		t.Parallel()
		env, root, prefix := RealCLIEnv(t, "integrationtest-colour-readonly")
		env = append(env, "SLIVINGDOC_READ_ONLY_PATHS=docs")

		writer := NewHarness(t, HarnessConfig{Prefix: prefix})
		seed := writer.WorkspaceRoot()
		writer.WriteFile(filepath.Join(seed, "docs", "faq.md"), "Q: a\nA: 1\n")
		writer.AssertOK(t, writer.Pull(""))
		writer.AssertOK(t, writer.Commit("", "seed"))

		notes := filepath.Join(root, "notes")
		RunCLIExact(t, "real", env,
			"OK  generation 1  "+notes+"\n"+
				"  1 file · 2 lines\n"+
				"  entry            files    % largest inside, without a row of its own\n"+
				"  docs/                1 100% faq.md\n"+
				"read-only: docs\n",
			"pull", notes)
		WriteCLIFile(t, filepath.Join(notes, "docs", "faq.md"), "A: 2\n")

		code, stdout := RunCLITTY(t, "real", env, "commit", notes, "-m", "m")
		if code != 1 {
			t.Fatalf("read-only commit on a terminal = exit %d, want 1; stdout: %q", code, stdout)
		}
		want := "\x1b[31m✗\x1b[0m \x1b[31mINVALID_REQUEST\x1b[0m · \x1b[2mREAD_ONLY_PATH\x1b[0m\n" +
			"docs is read-only in this server. Your changes there were discarded and the files reset. " +
			"Write outside the read-only paths, then commit again.\n" +
			"  \x1b[33mdocs/faq.md\x1b[0m  \x1b[2mread-only\x1b[0m\n" +
			"\x1b[34m→\x1b[0m edit the files, then commit\n" +
			"\x1b[2mretryable: false\x1b[0m\n" +
			"\x1b[2mread-only:\x1b[0m docs\n"
		if stdout != want {
			t.Fatalf("read-only refusal on a terminal stdout = %q, want the ANSI report %q", stdout, want)
		}
		plain := StripANSI(stdout)
		// Stripped of colour, the terminal report is the plain one with
		// its marks: a cross before the code, an arrow for "next:".
		wantPlain := "✗ INVALID_REQUEST · READ_ONLY_PATH\n" +
			"docs is read-only in this server. Your changes there were discarded and the files reset. " +
			"Write outside the read-only paths, then commit again.\n" +
			"  docs/faq.md  read-only\n" +
			"→ edit the files, then commit\n" +
			"retryable: false\n" +
			"read-only: docs\n"
		if plain != wantPlain {
			t.Fatalf("stripped read-only refusal = %q, want the plain report %q", plain, wantPlain)
		}
	})
}

// TestScenarioCLIOutlineFitsTheTerminalWidth proves a materialization's
// outline is cut for the terminal that prints it: a narrow terminal gets a
// strip and rows that stop at its edge instead of a fixed 150 columns
// (architecture/running.md, The report). The first line is left out of the
// bound because it reports the notebook path whole.
func TestScenarioCLIOutlineFitsTheTerminalWidth(t *testing.T) {
	t.Parallel()
	const cols = 40
	env, root, prefix := RealCLIEnv(t, "integrationtest-outline-width")
	writer := NewHarness(t, HarnessConfig{Prefix: prefix})
	seed := writer.WorkspaceRoot()
	for i := range 6 {
		writer.WriteFile(filepath.Join(seed, "docs", fmt.Sprintf("a-fairly-long-note-name-%02d.md", i)), "line\n")
	}
	writer.WriteFile(filepath.Join(seed, "a-top-level-name-that-outgrows-the-column", "a.md"), "line\n")
	writer.AssertOK(t, writer.Pull(""))
	writer.AssertOK(t, writer.Commit("", "seed"))

	notes := filepath.Join(root, "notes")
	code, out := RunCLIOnTTYSize(t, "real", env, StdoutOnly, TtySize{Cols: cols, Rows: 24}, "pull", notes)
	if code != 0 {
		t.Fatalf("pull on a %d-column terminal = exit %d; output %q", cols, code, out)
	}
	lines := strings.Split(StripANSI(out), "\n")
	strip := false
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		width := utf8.RuneCountInString(line)
		if width > cols {
			t.Fatalf("report line is %d columns on a %d-column terminal: %q", width, cols, line)
		}
		if strings.Contains(line, "█") {
			strip = true
			if width != cols {
				t.Fatalf("strip line is %d columns, want it to fill the %d-column terminal: %q", width, cols, line)
			}
		}
	}
	if !strip {
		t.Fatalf("report %q carries no strip line", out)
	}
}

// TestScenarioCLIHomeOnTerminal proves the router's terminal surface: a
// bare slivingdoc on a terminal is the home screen with the login state
// and the grouped commands, and a command's help gets the header, while
// the same lines stay plain on a pipe (TestScenarioConfigRefusals).
func TestScenarioCLIHomeOnTerminal(t *testing.T) {
	t.Parallel()
	t.Run("home screen", func(t *testing.T) {
		t.Parallel()
		code, stdout := RunCLITTY(t, "fake", []string{"SLIVINGDOC_CONFIG_DIR=" + t.TempDir()})
		if code != 1 {
			t.Fatalf("bare slivingdoc on a terminal = exit %d, want 1", code)
		}
		plain := StripANSI(stdout)
		for _, want := range []string{
			"◆ slivingdoc · v", "Not logged in to hosted storage · → slivingdoc login\n",
			"  Sync     pull     write the current notebook into a directory\n",
			"  Account  login    log in to hosted storage through the browser\n",
		} {
			if !strings.Contains(plain, want) {
				t.Fatalf("home on a terminal = %q, want it to contain %q", plain, want)
			}
		}
	})
	t.Run("command help", func(t *testing.T) {
		t.Parallel()
		code, stdout := RunCLITTY(t, "fake", nil, "space", "-h")
		if code != 0 || !strings.HasPrefix(stdout, "\x1b[34m◆\x1b[0m \x1b[34mslivingdoc\x1b[0m \x1b[1mspace\x1b[0m \x1b[2m· list, create, or set the default of a hosted space") ||
			!strings.Contains(stdout, "\x1b[34mUsage:\x1b[0m\n") {
			t.Fatalf("space -h on a terminal = exit %d stdout %q", code, stdout)
		}
	})
}

// TestScenarioCLIStderrOnTerminal proves the stderr side of the terminal
// presentation: pull draws its progress line and clears it before the
// report, and serve announces that it is ready, only because stderr is a
// terminal (architecture/tui.md).
func TestScenarioCLIStderrOnTerminal(t *testing.T) {
	t.Parallel()
	t.Run("pull progress", func(t *testing.T) {
		t.Parallel()
		env, root := CliRoots(t)
		notes := filepath.Join(root, "notes")
		code, out := RunCLIOnTTY(t, "fake", env, StdoutAndStderr, "pull", notes)
		if code != 0 {
			t.Fatalf("pull = exit %d; output %q", code, out)
		}
		progress := "\r\x1b[K  \x1b[34m⠋\x1b[0m Pulling " + notes + " \x1b[2m· bucket integration-bucket · "
		if !strings.Contains(out, progress) || !strings.Contains(out, "\r\x1b[K\x1b[32m✓\x1b[0m \x1b[32mOK\x1b[0m") {
			t.Fatalf("pull on a terminal = %q, want the progress line %q cleared before the report", out, progress)
		}
	})
	t.Run("serve ready line", func(t *testing.T) {
		t.Parallel()
		code, out := RunCLIOnTTY(t, "fake", []string{"LOG_LEVEL=warn"}, StdoutAndStderr, "serve")
		if code != 0 {
			t.Fatalf("serve = exit %d; output %q", code, out)
		}
		plain := StripANSI(out)
		if !strings.HasPrefix(plain, "◆ slivingdoc serve · bucket integration-bucket · ") || !strings.Contains(plain, " · waiting for an MCP client on stdio\n") {
			t.Fatalf("serve on a terminal = %q, want the ready line", plain)
		}
	})
}

// TestScenarioStatusTrailerOnTerminal proves the colour side of the status
// source trailer: on a real terminal it is dimmed like every other detail
// line, and stripped of colour it reads as the words a pipe sees.
func TestScenarioStatusTrailerOnTerminal(t *testing.T) {
	t.Parallel()
	_, _, env, root := RememberedEnv(t)
	notes := filepath.Join(root, "remembered")
	Remember(t, env, notes, SecondSpace, HostedPrefix)
	RunCLIOK(t, "real", env, nil, "pull", notes)

	code, stdout := RunCLITTY(t, "real", env, "status", notes)
	if code != 0 {
		t.Fatalf("status on a terminal = exit %d, stdout %q", code, stdout)
	}
	if !strings.Contains(stdout, "\x1b[2mspace: remembered space\x1b[0m\n") {
		t.Fatalf("status on a terminal = %q, want the dimmed source trailer", stdout)
	}
	if plain := StripANSI(stdout); !strings.Contains(plain, notes+"  space "+SecondSpace+"\nspace: remembered space\n") {
		t.Fatalf("stripped status = %q, want the plain trailer", plain)
	}
}
