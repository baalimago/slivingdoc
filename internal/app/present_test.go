package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/sitelogin/sitetest"
	"github.com/baalimago/slivingdoc/internal/tui"
)

// styled renders every stream of a rig as a 16-colour terminal would.
func styled(io.Writer) tui.Style { return tui.New(tui.Styled, tui.Basic) }

// typed is what a person types, one byte per read, so the confirmation
// prompt consumes only its own line and the picker reads the next one, as
// on a terminal.
func typed(lines string) io.Reader { return iotest.OneByteReader(strings.NewReader(lines)) }

var teamSpace = space("team", "bob@example.test", "read")

func TestStyledLoginPicksTheDefaultSpace(t *testing.T) {
	r := newLoginRig(t)
	r.terminal, r.answer, r.style = OnTerminal, typed("y\n1\n"), styled
	r.site.SetSpaces(loginToken, notesSpace, teamSpace)
	r.site.Next(approved(loginToken, "write", DefaultHostedEndpoint))
	if err := r.login(t); err != nil {
		t.Fatalf("login = %v", err)
	}
	stderr := r.errOut.String()
	for _, want := range []string{
		"\x1b[34m◆\x1b[0m \x1b[34mslivingdoc\x1b[0m \x1b[1mlogin\x1b[0m \x1b[2m· " + strings.TrimPrefix(r.site.URL(), "https://") + "\x1b[0m\n",
		"\x1b[33m▲\x1b[0m Logging in through " + r.site.URL(),
		"  \x1b[2mOpen\x1b[0m  \x1b[34m" + r.site.URL() + "/cli/login#BCDF-GHJK\x1b[0m\n",
		"  \x1b[2mCode\x1b[0m  \x1b[1mBCDF-GHJK\x1b[0m\n",
		"\x1b[33m▲\x1b[0m \x1b[33mOnly approve it if you started this login in your own terminal.\x1b[0m",
		"  Approved by \x1b[1mada@example.test\x1b[0m \x1b[2m· read and write · until 2026-12-26 08:00 UTC\x1b[0m\n",
		"  \x1b[2mStorage endpoint\x1b[0m  https://api.slivingdoc.dev\n",
		"  \x1b[1mnotes\x1b[0m  read and write  \x1b[2mada@example.test\x1b[0m\n",
		"  \x1b[1mteam\x1b[0m   read only       \x1b[2mbob@example.test\x1b[0m\n",
		"  \x1b[1mStore this login? [y/N] \x1b[0m",
		"Pick the default space for serve, pull and commit",
		"\x1b[32m✓\x1b[0m The default space is \"team\"",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr = %q, want it to contain %q", stderr, want)
		}
	}
	if strings.Contains(stderr, "Waiting for approval (") {
		t.Fatalf("stderr = %q: a terminal gets the countdown, not the plain waiting line", stderr)
	}
	want := "\x1b[32m✓\x1b[0m Logged in as \x1b[1mada@example.test\x1b[0m \x1b[2m· read and write · until 2026-12-26 08:00 UTC · default space team\x1b[0m\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q, want %q", r.out.String(), want)
	}
	if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "team" {
		t.Fatalf("default space = %q, want team", got)
	}
}

func TestStyledLoginWithoutSpaces(t *testing.T) {
	r := newLoginRig(t)
	r.style = styled
	r.site.SetSpaces(loginToken)
	r.site.Next(approved(loginToken, "write", DefaultHostedEndpoint))
	if err := r.login(t); err != nil {
		t.Fatalf("login = %v", err)
	}
	if !strings.Contains(r.errOut.String(), "\x1b[2mSpaces\x1b[0m            none yet\n") ||
		!strings.Contains(r.errOut.String(), "\x1b[33m▲\x1b[0m The login reaches no space yet") {
		t.Fatalf("stderr = %q", r.errOut.String())
	}
}

func TestLoginPickerLeftOrBroken(t *testing.T) {
	for _, tt := range []struct {
		name   string
		answer io.Reader
		told   string
	}{
		{"quit", typed("y\nq\n"), "Run 'slivingdoc space <name>' to choose the default space"},
		{"a broken terminal", io.MultiReader(typed("y\n"), iotest.ErrReader(errors.New("input/output error"))), "The space picker failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newLoginRig(t)
			r.terminal, r.answer = OnTerminal, tt.answer
			r.site.SetSpaces(loginToken, notesSpace, teamSpace)
			r.site.Next(approved(loginToken, "write", DefaultHostedEndpoint))
			if err := r.login(t); err != nil {
				t.Fatalf("login = %v; a left picker keeps the login", err)
			}
			if !strings.Contains(r.errOut.String(), tt.told) {
				t.Fatalf("stderr = %q, want %q", r.errOut.String(), tt.told)
			}
			if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "" {
				t.Fatalf("default space = %q, want none", got)
			}
			if len(r.logins(t).Logins()) != 1 {
				t.Fatal("the login was not kept")
			}
		})
	}
}

// revokedWhenRead answers the picker with q and records which keys the
// site had revoked by the time the picker first read.
type revokedWhenRead struct {
	site   *sitetest.Site
	seen   []string
	answer io.Reader
}

func (r *revokedWhenRead) Read(p []byte) (int, error) {
	if r.answer == nil {
		r.seen = r.site.Revoked()
		r.answer = typed("q\n")
	}
	return r.answer.Read(p)
}

func TestLoginRevokesTheReplacedKeyBeforeThePicker(t *testing.T) {
	r := newLoginRig(t)
	r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, teamSpace})
	picker := &revokedWhenRead{site: r.site}
	r.terminal, r.answer = OnTerminal, io.MultiReader(typed("y\n"), picker)
	r.site.SetSpaces(otherToken, notesSpace, teamSpace)
	r.site.Next(approved(otherToken, "write", DefaultHostedEndpoint))
	if err := r.login(t); err != nil {
		t.Fatalf("second login = %v", err)
	}
	// Once the picker runs, a Ctrl-C ends the process; the replaced key
	// must be revoked by then.
	if len(picker.seen) != 1 || picker.seen[0] != loginToken {
		t.Fatalf("revoked when the picker read = %v, want the replaced key", picker.seen)
	}
}

func TestSpacePicker(t *testing.T) {
	t.Run("picks the default", func(t *testing.T) {
		r := newLoginRig(t)
		r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, teamSpace})
		r.terminal, r.answer, r.style = OnTerminal, typed("1\n"), styled
		if err := r.space(t); err != nil {
			t.Fatalf("space = %v", err)
		}
		if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "team" {
			t.Fatalf("default space = %q, want team", got)
		}
		if want := "\x1b[32m✓\x1b[0m Default space \x1b[1mteam\x1b[0m \x1b[2m· (read only), owned by bob@example.test\x1b[0m\n"; r.out.String() != want {
			t.Fatalf("stdout = %q, want %q", r.out.String(), want)
		}
		r.out.Reset()
		r.errOut.Reset()
		r.answer = typed("q\n")
		if err := r.space(t); err != nil {
			t.Fatalf("space = %v", err)
		}
		if !strings.Contains(r.errOut.String(), "bob@example.test  default\n") || !strings.Contains(r.errOut.String(), "Nothing was changed") {
			t.Fatalf("stderr = %q, want the default marked and nothing changed", r.errOut.String())
		}
		if got := r.defaultSpace(t, DefaultHostedEndpoint); got != "team" || r.out.String() != "" {
			t.Fatalf("default space = %q, stdout %q; a left picker changes nothing", got, r.out.String())
		}
	})
	t.Run("a broken terminal is an error", func(t *testing.T) {
		r := newLoginRig(t)
		r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, teamSpace})
		r.terminal, r.answer = OnTerminal, iotest.ErrReader(errors.New("input/output error"))
		if err := r.space(t); err == nil || !strings.Contains(err.Error(), "space: tui: pick") {
			t.Fatalf("space = %v, want the picker's failure", err)
		}
	})
	t.Run("styled listing without a person", func(t *testing.T) {
		r := newLoginRig(t)
		r.loginOnce(t, loginToken, []sitetest.Space{notesSpace, teamSpace}, "--space", "team")
		r.style = styled
		if err := r.space(t); err != nil {
			t.Fatalf("space = %v", err)
		}
		want := "\x1b[2mSPACE\x1b[0m  \x1b[2mACCESS\x1b[0m          \x1b[2mOWNER\x1b[0m\n" +
			"\x1b[1mnotes\x1b[0m  read and write  \x1b[2mada@example.test\x1b[0m\n" +
			"\x1b[1mteam\x1b[0m   read only       \x1b[2mbob@example.test\x1b[0m  \x1b[32mdefault\x1b[0m\n"
		if r.out.String() != want {
			t.Fatalf("stdout = %q, want %q", r.out.String(), want)
		}
	})
}

// logoutAll runs logout the way a person does: no --site and no
// SLIVINGDOC_SITE, so every stored login is a candidate.
func (r *loginRig) logoutAll(t *testing.T) error {
	t.Helper()
	opts := r.opts()
	opts.Env = []string{credentials.DirEnv + "=" + r.dir}
	op, err := PrepareLogout(NewLogoutFlags(), opts)
	if err != nil {
		return err
	}
	return op.Run(context.Background())
}

func TestLogoutPicker(t *testing.T) {
	two := func(t *testing.T) *loginRig {
		r := newLoginRig(t)
		r.loginOnce(t, loginToken, []sitetest.Space{notesSpace})
		r.site.SetSpaces("second-key", notesSpace)
		r.site.Next(approved("second-key", "write", devEndpoint))
		if err := r.login(t); err != nil {
			t.Fatalf("second login = %v", err)
		}
		r.out.Reset()
		r.errOut.Reset()
		return r
	}
	t.Run("picks one", func(t *testing.T) {
		r := two(t)
		r.terminal, r.answer, r.style = OnTerminal, typed("1\n"), styled
		if err := r.logoutAll(t); err != nil {
			t.Fatalf("logout = %v", err)
		}
		logins := r.logins(t).Logins()
		if len(logins) != 1 || logins[0].Endpoint != DefaultHostedEndpoint {
			t.Fatalf("stored = %+v, want only the unpicked login", logins)
		}
		if !strings.Contains(r.out.String(), "\x1b[32m✓\x1b[0m Logged out of \x1b[1mada@example.test\x1b[0m at "+devEndpoint) {
			t.Fatalf("stdout = %q", r.out.String())
		}
	})
	t.Run("left", func(t *testing.T) {
		r := two(t)
		r.terminal, r.answer = OnTerminal, typed("q\n")
		if err := r.logoutAll(t); err != nil {
			t.Fatalf("logout = %v", err)
		}
		if len(r.logins(t).Logins()) != 2 || len(r.site.Revoked()) != 0 {
			t.Fatal("a left picker logged out")
		}
		if !strings.Contains(r.errOut.String(), "Nothing was changed; every login stays stored") {
			t.Fatalf("stderr = %q", r.errOut.String())
		}
	})
	t.Run("a broken terminal is an error", func(t *testing.T) {
		r := two(t)
		r.terminal, r.answer = OnTerminal, iotest.ErrReader(errors.New("input/output error"))
		if err := r.logoutAll(t); err == nil || !strings.Contains(err.Error(), "logout: tui: pick") {
			t.Fatalf("logout = %v", err)
		}
	})
	t.Run("--site chooses without asking", func(t *testing.T) {
		r := two(t)
		r.terminal, r.answer = OnTerminal, typed("q\n")
		if err := r.logout(t, "--site", r.site.URL()); err != nil {
			t.Fatalf("logout = %v", err)
		}
		if len(r.logins(t).Logins()) != 0 {
			t.Fatal("--site did not log out of both")
		}
	})
}

func TestCountdown(t *testing.T) {
	for d, want := range map[string]string{"0s": "0:00", "-5s": "0:00", "65s": "1:05", "600s": "10:00"} {
		dur, err := time.ParseDuration(d)
		if err != nil {
			t.Fatal(err)
		}
		if got := countdown(dur); got != want {
			t.Fatalf("countdown(%s) = %q, want %q", d, got, want)
		}
	}
}
