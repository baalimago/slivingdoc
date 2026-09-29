package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/httpstore"
	"github.com/baalimago/slivingdoc/internal/mcp"
	"github.com/baalimago/slivingdoc/internal/sitelogin"
	"github.com/baalimago/slivingdoc/internal/tui"
)

// SpaceFlags are the space command's flags.
type SpaceFlags struct {
	endpoint stringFlag
}

// NewSpaceFlags returns an unbound space flag holder.
func NewSpaceFlags() *SpaceFlags { return &SpaceFlags{} }

// Bind registers the space flags on fs.
func (f *SpaceFlags) Bind(fs *flag.FlagSet) {
	fs.Var(&f.endpoint, "endpoint", "hosted storage API whose login to use (default: the only stored login)")
}

// Space is a prepared space command: the stored login is chosen and
// usable, and the name, when given, is a valid space name. Nothing has
// been sent yet.
type Space struct {
	opts   ProcessOptions
	file   credentials.File
	login  credentials.Login
	prior  string
	name   string
	client *sitelogin.Client
}

// PrepareSpace chooses the stored login the command lists or sets the
// default space of: the one for --endpoint or SLIVINGDOC_ENDPOINT when
// set, else the only one stored. name is the space to make the default,
// or empty to list.
func PrepareSpace(f *SpaceFlags, name string, opts ProcessOptions) (*Space, error) {
	env := environ(opts.Env)
	file, err := credentialsFile(env)
	if err != nil {
		return nil, err
	}
	set, err := file.Load()
	if err != nil {
		return nil, fmt.Errorf("space: %w", err)
	}
	explicit, err := normalizeEndpoint(resolveString(&f.endpoint, env["SLIVINGDOC_ENDPOINT"], ""))
	if err != nil {
		return nil, fmt.Errorf("space: %w", err)
	}
	login, err := pickLogin(set, explicit)
	switch {
	case errors.Is(err, credentials.ErrNoLogin):
		return nil, fmt.Errorf("space: not logged in: %w; run 'slivingdoc login'", err)
	case errors.Is(err, credentials.ErrAmbiguous):
		return nil, fmt.Errorf("space: %w; pass --endpoint to choose one", err)
	case err != nil:
		return nil, fmt.Errorf("space: %w", err)
	}
	if err := login.Usable(time.Now()); err != nil {
		return nil, fmt.Errorf("space: %w; run 'slivingdoc login' again", err)
	}
	if name != "" {
		if err := httpstore.ValidateSpace(name); err != nil {
			return nil, fmt.Errorf("space: %w", err)
		}
	}
	client, err := siteClient(login.Site, opts)
	if err != nil {
		return nil, fmt.Errorf("space: %w", err)
	}
	prior, _ := set.DefaultSpace(login.Endpoint)
	return &Space{opts: opts, file: file, login: login, prior: prior, name: name, client: client}, nil
}

// Run lists the spaces the login reaches (GET /cli/v1/spaces) and marks
// the default with *, or, with a name, checks the name is among them and
// stores it as the default space of the login's endpoint.
func (s *Space) Run(ctx context.Context) error {
	spaces, err := s.client.Spaces(ctx, s.login.Key)
	var refusal *sitelogin.Refusal
	if errors.As(err, &refusal) && refusal.Status == http.StatusUnauthorized {
		return fmt.Errorf("space: the site no longer accepts the stored login (revoked or expired); run 'slivingdoc login' again")
	}
	if err != nil {
		return fmt.Errorf("space: list the spaces the login reaches: %s", mcp.Redact(err.Error()))
	}
	if s.name == "" && len(spaces) > 0 && s.opts.pickable() == OnTerminal {
		name, err := s.opts.pickSpace(spaces, s.prior)
		if skipped(err) {
			io.WriteString(s.opts.ErrOut(), line(s.opts.errStyle(), tui.Next, "Nothing was changed", ""))
			return nil
		}
		if err != nil {
			return fmt.Errorf("space: %w", err)
		}
		s.name = name
	}
	if s.name == "" {
		s.list(spaces)
		return nil
	}
	i := slices.IndexFunc(spaces, func(sp sitelogin.Space) bool { return sp.Name == s.name })
	if i < 0 {
		return fmt.Errorf("space: the login of %s does not reach space %q (it reaches %s); nothing was changed", s.login.Account, s.name, spaceNames(spaces))
	}
	if err := s.store(ctx); err != nil {
		return err
	}
	if so := s.opts.outStyle(); so.Mode() == tui.Styled {
		sp := spaces[i]
		io.WriteString(s.opts.Out(), line(so, tui.Done, "Default space "+so.Bold(sp.Name), strings.TrimPrefix(describeSpace(sp), sp.Name+" ")))
		return nil
	}
	fmt.Fprintf(s.opts.Out(), "The default space is now %s\n", describeSpace(spaces[i]))
	return nil
}

// list prints one line per space on stdout, the default marked with *,
// and on stderr what to do when there is no usable default.
func (s *Space) list(spaces []sitelogin.Space) {
	errOut, es := s.opts.ErrOut(), s.opts.errStyle()
	if so := s.opts.outStyle(); so.Mode() == tui.Styled && len(spaces) > 0 {
		io.WriteString(s.opts.Out(), spacesTable(so, "", spaces, s.prior))
	} else {
		for _, sp := range spaces {
			mark := " "
			if sp.Name == s.prior {
				mark = "*"
			}
			fmt.Fprintf(s.opts.Out(), "%s %s\n", mark, describeSpace(sp))
		}
	}
	listed := slices.ContainsFunc(spaces, func(sp sitelogin.Space) bool { return sp.Name == s.prior })
	switch {
	case len(spaces) == 0:
		fmt.Fprintf(errOut, "%sThe login of %s reaches no space yet; create one on the site.\n", es.Mark(tui.Caution), s.login.Account)
	case s.prior == "":
		fmt.Fprintln(errOut, es.Mark(tui.Next)+"No default space; run 'slivingdoc space <name>' to choose one.")
	case !listed:
		fmt.Fprintf(errOut, "%sThe default space %q is not among them; run 'slivingdoc space <name>' to choose another.\n", es.Mark(tui.Caution), s.prior)
	}
}

// store writes the default space under the credentials lock, provided the
// login it was checked against is still the one stored.
func (s *Space) store(ctx context.Context) (errOut error) {
	lock, err := s.file.Lock(ctx)
	if err != nil {
		return fmt.Errorf("space: %w", err)
	}
	defer func() {
		if err := lock.Unlock(); err != nil && errOut == nil {
			errOut = fmt.Errorf("space: %w", err)
		}
	}()
	set, err := s.file.Load()
	if err != nil {
		return fmt.Errorf("space: %w", err)
	}
	current, err := set.Lookup(s.login.ID)
	if err != nil || current.Key != s.login.Key {
		return errors.New("space: the stored login changed while the spaces were listed; run the command again")
	}
	if err := set.SetDefaultSpace(s.login.Endpoint, s.name); err != nil {
		return fmt.Errorf("space: %w", err)
	}
	if err := s.file.Save(set); err != nil {
		return fmt.Errorf("space: %w", err)
	}
	return nil
}
