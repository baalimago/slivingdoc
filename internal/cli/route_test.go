package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/tui"
)

// route runs one command line and returns the exit code and both streams.
func route(t *testing.T, env []string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut strings.Builder
	code := Run(context.Background(), append([]string{"slivingdoc"}, args...), &stubEngine{}, app.ProcessOptions{
		Args: args, Env: env, Cwd: t.TempDir(), Stdout: &out, Stderr: &errOut, Signals: make(chan os.Signal, 1),
	})
	return code, out.String(), errOut.String()
}

// TestRouterPlainSurface pins the router's own lines when neither stream
// is a terminal: the usage on stdout, and every refusal as exactly one
// "error: <message>" line on stderr (architecture/tui.md).
func TestRouterPlainSurface(t *testing.T) {
	t.Parallel()
	usage := func(out string) bool {
		return strings.HasPrefix(out, "slivingdoc - shared UTF-8 text notebook") &&
			strings.Contains(out, "Commands:\n  commit|c   publish") && strings.Contains(out, "  version|v  ")
	}
	for _, row := range []struct {
		name     string
		args     []string
		code     int
		usage    bool
		stderr   string
		stdoutIs string
	}{
		{name: "no command", code: 1, usage: true},
		{name: "-h", args: []string{"-h"}, code: 0, usage: true},
		{name: "--help", args: []string{"--help"}, code: 0, usage: true},
		{name: "help", args: []string{"help"}, code: 0, usage: true},
		{name: "help with a command", args: []string{"help", "logout"}, code: 0, stdoutIs: Commands(&stubEngine{}, app.ProcessOptions{})["logout"].Help()},
		{name: "help for an unknown command", args: []string{"help", "frob"}, code: 1, usage: true, stderr: "error: \"frob\" is not a slivingdoc command\n"},
		{name: "a flag without a command", args: []string{"--frob"}, code: 1, usage: true, stderr: "error: give a command before --frob\n"},
		{name: "unknown command", args: []string{"frob"}, code: 1, usage: true, stderr: "error: \"frob\" is not a slivingdoc command\n"},
		{
			name: "unknown flag", args: []string{"serve", "--frob"}, code: 1,
			stderr: "error: serve: flag provided but not defined: -frob; run 'slivingdoc serve -h' for its flags\n",
		},
		{name: "a shortcut", args: []string{"v"}, code: 0, stdoutIs: "slivingdoc " + app.Version + "\n"},
		{name: "command help is the help text", args: []string{"logout", "-h"}, code: 0, stdoutIs: Commands(&stubEngine{}, app.ProcessOptions{})["logout"].Help()},
		{name: "a refused setup is one line", args: []string{"logout", "extra"}, code: 1, stderr: "error: logout: unexpected argument \"extra\"\n"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			code, out, errOut := route(t, []string{"SLIVINGDOC_CONFIG_DIR=" + t.TempDir()}, row.args...)
			if code != row.code {
				t.Fatalf("exit = %d, want %d; stderr %q", code, row.code, errOut)
			}
			if row.usage && !usage(out) {
				t.Fatalf("stdout = %q, want the usage", out)
			}
			if row.stdoutIs != "" && out != row.stdoutIs {
				t.Fatalf("stdout = %q, want %q", out, row.stdoutIs)
			}
			if errOut != row.stderr {
				t.Fatalf("stderr = %q, want %q", errOut, row.stderr)
			}
			if strings.Contains(out+errOut, "\x1b[") {
				t.Fatalf("plain streams carry an escape: %q %q", out, errOut)
			}
		})
	}
}

// styledRouter is a router whose streams render styled, as on a terminal.
func styledRouter(t *testing.T, env []string) (router, *strings.Builder, *strings.Builder) {
	t.Helper()
	var out, errOut strings.Builder
	opts := app.ProcessOptions{Env: env, Stdout: &out, Stderr: &errOut}
	r := newRouter(Commands(&stubEngine{}, opts), opts)
	r.out, r.err = tui.New(tui.Styled, tui.Basic), tui.New(tui.Styled, tui.Basic)
	return r, &out, &errOut
}

func TestRouterStyledLines(t *testing.T) {
	t.Parallel()
	r, out, errOut := styledRouter(t, nil)
	r.fail(os.ErrNotExist)
	if got := errOut.String(); got != "\x1b[31m✗\x1b[0m file does not exist\n" {
		t.Fatalf("styled error = %q", got)
	}
	r.help("logout", "slivingdoc logout - revoke\n\nUsage:\n  slivingdoc logout\nFlags:\n  --site string  x: y\n")
	want := "\x1b[34m◆\x1b[0m \x1b[34mslivingdoc\x1b[0m \x1b[1mlogout\x1b[0m \x1b[2m· revoke\x1b[0m\n\n\x1b[34mUsage:\x1b[0m\n  slivingdoc logout\n\x1b[34mFlags:\x1b[0m\n  --site string  x: y\n"
	if got := out.String(); got != want {
		t.Fatalf("styled help = %q, want %q", got, want)
	}
	out.Reset()
	r.usage()
	if got := out.String(); !strings.HasPrefix(got, "\x1b[34m◆\x1b[0m \x1b[34mslivingdoc\x1b[0m \x1b[2m· shared UTF-8") || !strings.Contains(got, "\x1b[34mCommands:\x1b[0m") {
		t.Fatalf("styled usage = %q", got)
	}
	if got := styleHelp(tui.New(tui.Styled, tui.Basic), "pull", "no summary line\n"); got != "no summary line\n" {
		t.Fatalf("styleHelp() changed a text without a summary line: %q", got)
	}
}

func TestHomeScreen(t *testing.T) {
	t.Parallel()
	t.Run("not logged in", func(t *testing.T) {
		t.Parallel()
		r, out, _ := styledRouter(t, []string{"SLIVINGDOC_CONFIG_DIR=" + t.TempDir()})
		r.home()
		got := out.String()
		for _, want := range []string{
			"\x1b[34mslivingdoc\x1b[0m \x1b[2m· v" + app.Version + " · shared notes for agents\x1b[0m\n",
			"Not logged in to hosted storage",
			"\x1b[1mSync\x1b[0m     \x1b[34mpull\x1b[0m     write the current notebook",
			"\x1b[1mAccount\x1b[0m  \x1b[34mlogin\x1b[0m",
			"slivingdoc <command> -h for its flags",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("home = %q, want it to contain %q", got, want)
			}
		}
	})
	t.Run("stored logins", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		env := []string{"SLIVINGDOC_CONFIG_DIR=" + dir}
		file, err := credentials.Locate(app.EnvLookup(env), runtime.GOOS)
		if err != nil {
			t.Fatal(err)
		}
		var set credentials.Set
		for _, l := range []credentials.Login{
			{ID: credentials.ID{Site: "https://www.slivingdoc.dev", Endpoint: app.DefaultHostedEndpoint}, Key: "k1", Access: credentials.AccessWrite, Account: "a@x"},
			{ID: credentials.ID{Site: "https://www.dev.slivingdoc.dev", Endpoint: "https://api.dev.slivingdoc.dev"}, Key: "k2", Access: credentials.AccessRead, Account: "b@x"},
			{
				ID: credentials.ID{Site: "https://site.test", Endpoint: "https://api.test"}, Key: "k3", Access: credentials.AccessRead, Account: "c@x",
				Expires: credentials.ExpiresAt(time.Now().Add(-time.Hour)),
			},
		} {
			// Put reports a new ID as ErrNoLogin: nothing was replaced.
			if _, err := set.Put(l); !errors.Is(err, credentials.ErrNoLogin) {
				t.Fatal(err)
			}
		}
		if err := set.SetDefaultSpace(app.DefaultHostedEndpoint, "notes"); err != nil {
			t.Fatal(err)
		}
		if err := file.Save(set); err != nil {
			t.Fatal(err)
		}
		r, out, _ := styledRouter(t, env)
		r.home()
		got := out.String()
		for _, want := range []string{
			"Logged in as \x1b[1ma@x\x1b[0m \x1b[2m· read and write · with no expiry\x1b[0m\n",
			"Default space \x1b[1mnotes\x1b[0m\n",
			"Logged in as \x1b[1mb@x\x1b[0m at https://api.dev.slivingdoc.dev",
			"No default space · \x1b[34m→\x1b[0m slivingdoc space\n",
			"\x1b[1mc@x\x1b[0m at https://api.test \x1b[2m· the login expired · \x1b[0m",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("home = %q, want it to contain %q", got, want)
			}
		}
	})
	t.Run("an unreadable file is told", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, credentials.FileName), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		r, out, _ := styledRouter(t, []string{"SLIVINGDOC_CONFIG_DIR=" + dir})
		r.home()
		if !strings.Contains(out.String(), "The stored logins cannot be read") {
			t.Fatalf("home = %q, want the unreadable file told", out.String())
		}
	})
}

// TestCommandGroupsCoverTheMap fails when a command is missing from the
// home screen's groups or a group names a command that does not exist.
func TestCommandGroupsCoverTheMap(t *testing.T) {
	t.Parallel()
	commands := Commands(&stubEngine{}, app.ProcessOptions{})
	r := newRouter(commands, app.ProcessOptions{})
	grouped := 0
	for _, g := range commandGroups {
		for _, name := range g.commands {
			if command, _ := r.lookup(name); command == nil {
				t.Fatalf("group %s names %q, which is not a command", g.title, name)
			}
			grouped++
		}
	}
	if grouped != len(commands) {
		t.Fatalf("the groups hold %d commands, the map %d", grouped, len(commands))
	}
}
