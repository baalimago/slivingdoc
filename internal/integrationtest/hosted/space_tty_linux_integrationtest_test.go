//go:build linux

package hosted

import (
	"bytes"
	"os"
	"strings"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"
)

// TestTerminalHarnessRunCLIOnTypedTTY proves the harness itself: both
// streams are the child's terminal and the exit status survives the run.
// The picker scenarios prove the stdin side.
func TestTerminalHarnessRunCLIOnTypedTTY(t *testing.T) {
	t.Parallel()
	t.Run("stdout and exit code", func(t *testing.T) {
		t.Parallel()
		code, out := RunCLIOnTypedTTY(t, "fake", nil, nil, "version")
		if code != 0 || !strings.HasPrefix(StripANSI(out), "slivingdoc ") {
			t.Fatalf("version on a terminal = exit %d, output %q", code, out)
		}
	})
	t.Run("stderr and exit code", func(t *testing.T) {
		t.Parallel()
		env := []string{"SLIVINGDOC_CONFIG_DIR=" + t.TempDir()}
		code, out := RunCLIOnTypedTTY(t, "fake", env, nil, "space")
		if code != 1 || !strings.Contains(StripANSI(out), "no backend is reachable") {
			t.Fatalf("space without a backend = exit %d, output %q", code, out)
		}
	})
}

func TestScenarioSpacePickerSkipOnTerminal(t *testing.T) {
	t.Parallel()
	g, site, env, _ := LoginEnv(t)
	Approve(site, g, LoginKey, "write", LoginExpiry)
	RunLogin(t, env, site, LoggedIn(g, "read and write", WithDefault), "--no-browser")

	code, out := RunCLIOnTypedTTY(t, "real", env, []TtyStep{
		{After: "+ Create a new hosted space", Typed: "q\n"},
	}, "space")
	if code != 0 {
		t.Fatalf("space on a terminal = exit %d, want 0; output %q", code, out)
	}
	plain := StripANSI(out)
	if !strings.Contains(plain, "Nothing was changed") {
		t.Fatalf("a left picker = %q, want the skip line", plain)
	}
	RunCLIExact(t, "real", env, "HOSTED  "+g.URL()+"  "+LoginAccount+"\n"+
		"* team-notes  write  "+LoginAccount+"\n", "space")
}

func TestScenarioSpacePickerCreateOnTerminal(t *testing.T) {
	t.Parallel()
	g, site, env, _ := LoginEnv(t)
	Approve(site, g, LoginKey, "write", LoginExpiry)
	RunLogin(t, env, site, LoggedIn(g, "read and write", WithDefault), "--no-browser")

	code, out := RunCLIOnTypedTTY(t, "real", env, []TtyStep{
		{After: "+ Create a new hosted space", Typed: "1\n"},
		{After: "Name for the new hosted space: ", Typed: "research\n"},
	}, "space")
	if code != 0 {
		t.Fatalf("space on a terminal = exit %d, want 0; output %q", code, out)
	}
	plain := StripANSI(out)
	// The terminal renders the same create and default lines as the
	// non-interactive path's terminal form, with their marks.
	create := "✓ Created hosted space research · (write, " + LoginAccount + ") at " + g.URL() + "\n"
	selectDefault := "✓ Default space research · (write, " + LoginAccount + ")\n"
	if !strings.Contains(plain, create) || !strings.Contains(plain, selectDefault) {
		t.Fatalf("create on a terminal = %q, want %q then %q", plain, create, selectDefault)
	}
	RunCLIExact(t, "real", env, "HOSTED  "+g.URL()+"  "+LoginAccount+"\n"+
		"* research  write  "+LoginAccount+"\n"+
		"  team-notes  write  "+LoginAccount+"\n", "space")
}

func TestScenarioSpacePickerInterruptAtTable(t *testing.T) {
	t.Parallel()
	g, site, env, _ := LoginEnv(t)
	Approve(site, g, LoginKey, "write", LoginExpiry)
	RunLogin(t, env, site, LoggedIn(g, "read and write", WithDefault), "--no-browser")
	before, err := os.ReadFile(CredentialsPath(env))
	if err != nil {
		t.Fatal(err)
	}

	code, out := RunCLIOnTypedTTY(t, "real", env, []TtyStep{
		{After: "+ Create a new hosted space", Signal: os.Interrupt},
	}, "space")
	if code != 130 {
		t.Fatalf("Ctrl-C at the table = exit %d, want 130; output %q", code, out)
	}
	plain := StripANSI(out)
	if strings.Contains(plain, "Nothing was changed") || strings.Contains(plain, "The default space is now") {
		t.Fatalf("Ctrl-C at the table = %q, want no result line", plain)
	}
	after, err := os.ReadFile(CredentialsPath(env))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("Ctrl-C at the table changed the stored defaults")
	}
}
