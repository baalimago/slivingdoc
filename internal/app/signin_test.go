package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/sitelogin/sitetest"
)

// TestLoadConfigSignIn proves which startups a serve that may sign in keeps
// for its first tool call: no bucket and no login, and --storage hosted
// with no login, while --login never, the one-shot commands and a broken
// value keep refusing (architecture/login.md, Sign in on first use).
func TestLoadConfigSignIn(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name    string
		signIn  bool
		env     []string
		args    []string
		pending string
		refusal string
	}{
		{name: "no bucket", signIn: true, pending: "bucket is required"},
		{name: "hosted without a login", signIn: true, args: []string{"--storage", "hosted"}, pending: "--storage hosted needs SLIVINGDOC_TOKEN or a stored login"},
		{name: "a named hosted space", signIn: true, args: []string{"--storage", "hosted", "--space", "team"}, pending: "--storage hosted needs SLIVINGDOC_TOKEN or a stored login"},
		{name: "login never", signIn: true, args: []string{"--login", "never"}, refusal: "bucket is required"},
		{name: "login never from the variable", signIn: true, env: []string{LoginEnv + "=never"}, refusal: "bucket is required"},
		{name: "a broken value", signIn: true, env: []string{LoginEnv + "=sometimes"}, refusal: "--login must be auto or never"},
		{name: "one-shot command", refusal: "bucket is required"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			p := testProcess(row.env, row.args...)
			p.signIn = row.signIn
			cfg, err := loadConfig(p)
			if row.refusal != "" {
				if err == nil || !strings.Contains(err.Error(), row.refusal) {
					t.Fatalf("loadConfig() = %v, want the refusal %q", err, row.refusal)
				}
				return
			}
			if err != nil {
				t.Fatalf("loadConfig() = %v, want a pending sign-in", err)
			}
			if cfg.signIn == nil || !strings.Contains(cfg.signIn.Error(), row.pending) || cfg.reresolve == nil {
				t.Fatalf("signIn = %v, want the pending refusal %q and a way to resolve again", cfg.signIn, row.pending)
			}
			// Resolving again without a login is the same refusal, now
			// returned, and marked as one a sign-in resolves.
			if _, err := cfg.reresolve(); !errors.As(err, new(needsSignIn)) {
				t.Fatalf("reresolve() = %v, want a refusal a sign-in resolves", err)
			}
		})
	}
	p := testProcess(nil, "--storage", "hosted", "--space", "team")
	p.signIn = true
	cfg, err := loadConfig(p)
	if err != nil || cfg.bucket != "team" {
		t.Fatalf("loadConfig(--space team) = bucket %q, %v; want the space the sign-in makes the default", cfg.bucket, err)
	}
}

// TestSignInAnswers pins the two tool errors of a pending sign-in: the page
// to open, retryable, with why the last sign-in ended told once; and the
// refusal the person must fix, not retryable.
func TestSignInAnswers(t *testing.T) {
	t.Parallel()
	s := &signIn{lastErr: errors.New("the code expired")}
	page := credentials.SignIn{Page: "https://www.slivingdoc.dev/cli/login#BCDF-GHJK", Code: "BCDF-GHJK"}
	var nb *notebook.Error
	if err := s.signInRequired(page); !errors.As(err, &nb) || nb.Reason != notebook.ReasonSignInRequired || nb.Action != notebook.ActionRetry ||
		!strings.Contains(nb.Message, page.Page) || !strings.Contains(nb.Message, "the code expired") {
		t.Fatalf("signInRequired() = %v, want the page and the last sign-in's end", err)
	}
	if err := s.signInRequired(credentials.SignIn{}); !errors.As(err, &nb) || strings.Contains(nb.Message, "expired") || !strings.Contains(nb.Message, "call this tool again in a moment") {
		t.Fatalf("signInRequired(no page) = %v, want the wait without the told end", err)
	}
	s.lastErr = errors.New("access_denied")
	if err := s.signInRequired(credentials.SignIn{}); !errors.As(err, &nb) || !strings.Contains(nb.Message, "access_denied") || !strings.Contains(nb.Message, "start a new sign-in") {
		t.Fatalf("signInRequired(ended) = %v, want why it ended and the next step", err)
	}
	if err := setupRequired(errors.New("no default space")); !errors.As(err, &nb) || nb.Reason != notebook.ReasonSetupRequired || nb.Action != notebook.ActionOperator ||
		!strings.Contains(nb.Message, "no default space") {
		t.Fatalf("setupRequired() = %v, want the refusal for the person", err)
	}
}

// TestSignInAdoptsADefaultSpace pins the default a serve stores for a
// login that has none: the only space, notes among several, notes created
// when the login reaches none; and what it refuses: several spaces without
// notes, a listing or a creation the site refuses.
func TestSignInAdoptsADefaultSpace(t *testing.T) {
	t.Parallel()
	other := space("other", "bob@example.test", "read")
	team := space("team", "ada@example.test", "write")
	for _, row := range []struct {
		name    string
		spaces  []sitetest.Space
		refuse  func(*sitetest.Site)
		want    string
		refusal string
	}{
		{name: "the only space", spaces: []sitetest.Space{team}, want: "team"},
		{name: "notes among several", spaces: []sitetest.Space{team, notesSpace, other}, want: "notes"},
		{name: "notes created", want: "notes"},
		{name: "several without notes", spaces: []sitetest.Space{team, other}, refusal: "reaches several (team, other)"},
		{name: "listing refused", spaces: []sitetest.Space{team}, refuse: func(s *sitetest.Site) { s.Revoke(loginToken) }, refusal: "list the spaces"},
		{name: "creation refused", refuse: func(s *sitetest.Site) { s.RefuseCreate(403, "space_limit") }, refusal: `create the space "notes"`},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			r := newLoginRig(t)
			r.loginOnce(t, loginToken, []sitetest.Space{team, other})
			r.site.SetSpaces(loginToken, row.spaces...)
			if row.refuse != nil {
				row.refuse(r.site)
			}
			opts := r.opts()
			s := &signIn{rt: &Runtime{p: process{env: opts.Env, login: opts}, logger: discardLogger()}}
			err := s.adoptDefault(context.Background(), r.stored(t))
			got := r.defaultSpace(t, DefaultHostedEndpoint)
			if row.refusal != "" {
				if err == nil || !strings.Contains(err.Error(), row.refusal) || got != "" {
					t.Fatalf("adoptDefault() = %v, default %q; want the refusal %q and no default", err, got, row.refusal)
				}
				return
			}
			if err != nil || got != row.want {
				t.Fatalf("adoptDefault() = %v, default %q; want %q", err, got, row.want)
			}
		})
	}
}
