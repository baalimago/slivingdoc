package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/httpstore"
	"github.com/baalimago/slivingdoc/internal/mcp"
	"github.com/baalimago/slivingdoc/internal/sitelogin"
	"github.com/baalimago/slivingdoc/internal/storage"
)

// renewalShare is the part of a minted token's lifetime after which the
// next request mints another (architecture/login.md, Minted tokens).
const (
	renewalNumerator   = 8
	renewalDenominator = 10
)

// mintedTokens is the httpstore.TokenSource of a stored login: it trades
// the login's key at its site for a short-lived token of one space, keeps
// that token in memory only, and mints another once 80 % of its lifetime
// has passed or the storage endpoint refused it. The key goes only to the
// site; the token goes only to the store, which sends it to the login's
// endpoint.
type mintedTokens struct {
	client   *sitelogin.Client
	key      string
	space    string
	endpoint string
	// access is what each mint asks for: empty for the most the key and
	// the grant allow, or read.
	access credentials.Access
	now    func() time.Time

	// minting admits one mint at a time; a caller waiting for it gives up
	// when its own context ends. mu guards token, renewAt, spaceID and
	// replaced, and is never held across a request.
	minting chan struct{}
	mu      sync.Mutex
	token   string
	renewAt time.Time
	// spaceID is the space id the mints have named; empty until one names
	// an id. A later mint naming another id means the name now reaches
	// another space, and replaced then holds the refusal every later call
	// returns without asking the site again.
	spaceID  httpstore.SpaceID
	replaced error
}

var _ httpstore.RenewingSource = (*mintedTokens)(nil)

// heldToken is the token current returns: empty when there is none to use.
type heldToken string

// none reports that no token is held or it is due for renewal.
func (h heldToken) none() bool { return h == "" }

// newMintedTokens binds a token source to login and space. A nil doer is
// the default site client; a nil now is time.Now.
func newMintedTokens(login credentials.Login, space string, doer sitelogin.Doer, now func() time.Time) (*mintedTokens, error) {
	client, err := sitelogin.New(sitelogin.Config{Site: login.Site, UserAgent: "slivingdoc/" + Version, Client: doer})
	if err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	// A write login asks for the most the grant allows, so a read-only
	// space is still readable; a read login caps every token at read.
	var access credentials.Access
	if login.Access == credentials.AccessRead {
		access = credentials.AccessRead
	}
	return &mintedTokens{
		client: client, key: login.Key, space: space, endpoint: login.Endpoint, access: access, now: now,
		minting: make(chan struct{}, 1),
	}, nil
}

// wallNow is the clock without its monotonic reading, so renewal compares
// wall times: a machine that slept past a token's renewal time renews it
// on waking, which a monotonic comparison would not.
func (m *mintedTokens) wallNow() time.Time { return m.now().Round(0) }

// current returns the held token while its renewal time is still ahead,
// and none once it is due.
func (m *mintedTokens) current() heldToken {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.token != "" && m.wallNow().Before(m.renewAt) {
		return heldToken(m.token)
	}
	return ""
}

// Token returns the current minted token, minting one when there is none
// or its renewal time has come. Concurrent callers wait for one mint, each
// only as long as its own context allows.
func (m *mintedTokens) Token(ctx context.Context) (string, error) {
	if err := m.refusal(); err != nil {
		return "", err
	}
	if held := m.current(); !held.none() {
		return string(held), nil
	}
	select {
	case m.minting <- struct{}{}:
	case <-ctx.Done():
		return "", fmt.Errorf("app: wait for a token: %w: %w", ctx.Err(), storage.ErrTransport)
	}
	defer func() { <-m.minting }()
	// Another caller may have minted while this one waited.
	if held := m.current(); !held.none() {
		return string(held), nil
	}
	return m.mint(ctx)
}

// RenewsTokens marks the tokens as short-lived and renewable, so the
// store reports a streamed upload they failed as one to retry
// (httpstore.RenewingSource).
func (m *mintedTokens) RenewsTokens() {}

// Rejected forgets token when it is still the current one, so the next
// Token call mints another; a refusal of an older token changes nothing.
func (m *mintedTokens) Rejected(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if token == m.token {
		m.token = ""
	}
}

// mint trades the key for a token; the caller holds m.minting. A token the site
// minted in an answer outside the contract, or for another endpoint, is
// revoked, best effort, rather than left valid unseen.
func (m *mintedTokens) mint(ctx context.Context) (string, error) {
	asked := m.wallNow()
	minted, err := m.client.Mint(ctx, m.key, m.space, m.access)
	var rejected *sitelogin.RejectedError
	if errors.As(err, &rejected) {
		m.discard(ctx, rejected.Credential)
	}
	if err != nil {
		return "", mintError(err, m.space)
	}
	endpoint, err := normalizeEndpoint(minted.Endpoint)
	if err != nil || endpoint != m.endpoint {
		m.discard(ctx, minted.Token)
		return "", fmt.Errorf("app: the site minted a token for another storage endpoint than the login's %s: %w", m.endpoint, storage.ErrIncompatible)
	}
	lifetime := minted.Expires.Time().Sub(asked)
	if lifetime <= 0 {
		m.discard(ctx, minted.Token)
		return "", fmt.Errorf("app: the minted token expires %s, which is not after this machine's clock (%s); check the system clock: %w",
			minted.Expires.Describe(), asked.UTC().Format(time.RFC3339), storage.ErrIncompatible)
	}
	if err := m.bind(minted.SpaceID); err != nil {
		m.discard(ctx, minted.Token)
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.token = minted.Token
	m.renewAt = asked.Add(lifetime * renewalNumerator / renewalDenominator)
	return m.token, nil
}

// bind ties the source to the first space id a mint names and refuses a
// later mint for another id: the login's space of that name was replaced
// by another space, and this process's workspaces belong to the first
// (architecture/login.md, Minted tokens). A mint without an id, or the
// first id after mints without one (a site that started naming ids while
// the process ran), is accepted: the process keeps the key it started
// with, as before ids existed. The refusal is kept, so later calls fail
// without minting and revoking again.
func (m *mintedTokens) bind(id httpstore.SpaceID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case id == "":
		return nil
	case m.spaceID == "":
		m.spaceID = id
		return nil
	case id != m.spaceID:
		m.replaced = fmt.Errorf("app: the login's space %q is no longer the space this process started with; start it again: %w", m.space, storage.ErrAccessDenied)
		return m.replaced
	default:
		return nil
	}
}

// refusal is the kept refusal of a replaced space, or nil.
func (m *mintedTokens) refusal() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.replaced
}

// boundSpace is the space id the mints have named; empty before the first
// mint, or from a site that names none. buildService reads it right after
// the first mint, so it is the id the process's storage identity holds.
func (m *mintedTokens) boundSpace() httpstore.SpaceID {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.spaceID
}

func (m *mintedTokens) discard(ctx context.Context, token string) {
	rctx, cancel := revocationContext(ctx)
	defer cancel()
	// Best effort: the token lives an hour at most, and the refusal the
	// caller returns says what went wrong.
	_ = m.client.Revoke(rctx, token)
}

// mintError maps a failed mint to the storage error it stands for, so the
// notebook reports it in its taxonomy: a refused key or space is
// ErrAccessDenied, busy is ErrRateLimited, an unreachable or failing site
// is ErrTransport, and an answer outside the contract is ErrIncompatible.
// The text is the site's sanitized refusal; it never holds the key.
func mintError(err error, space string) error {
	var refusal *sitelogin.Refusal
	text := mcp.Redact(err.Error())
	switch {
	case errors.As(err, &refusal) && refusal.Status == http.StatusUnauthorized:
		return fmt.Errorf("app: the site no longer accepts the stored login (revoked or expired): %s: %w", text, storage.ErrAccessDenied)
	case errors.As(err, &refusal) && refusal.Code == "no_space":
		return fmt.Errorf("app: the login reaches no space %q: %s: %w", space, text, storage.ErrAccessDenied)
	case errors.As(err, &refusal) && refusal.Code == "space_suspended":
		return fmt.Errorf("app: space %q is suspended: %s: %w", space, text, storage.ErrAccessDenied)
	case errors.As(err, &refusal) && refusal.Status == http.StatusForbidden:
		return fmt.Errorf("app: the login may not use space %q: %s: %w", space, text, storage.ErrAccessDenied)
	case errors.As(err, &refusal) && refusal.Status == http.StatusTooManyRequests:
		return fmt.Errorf("app: the site is busy minting tokens: %s: %w", text, storage.ErrRateLimited)
	case errors.As(err, &refusal) && refusal.Status < http.StatusInternalServerError:
		return fmt.Errorf("app: the site refused to mint a token: %s: %w", text, storage.ErrIncompatible)
	case errors.Is(err, sitelogin.ErrProtocol):
		return fmt.Errorf("app: %s: %w", text, storage.ErrIncompatible)
	default:
		return fmt.Errorf("app: mint a token: %s: %w", text, storage.ErrTransport)
	}
}

// mintRefusal is the startup refusal of a process whose first mint
// failed, naming the command that fixes it.
func mintRefusal(err error, cfg config) error {
	var fix string
	switch {
	case errors.Is(err, storage.ErrAccessDenied) && cfg.bucketFrom == bucketFromLogin:
		fix = "run 'slivingdoc space' to list the login's spaces and 'slivingdoc space <name>' to choose the default, or 'slivingdoc login' again"
	case errors.Is(err, storage.ErrAccessDenied):
		fix = fmt.Sprintf("check %s, run 'slivingdoc space' to list the login's spaces, or run 'slivingdoc login' again", cfg.bucketFrom)
	case errors.Is(err, storage.ErrRateLimited), errors.Is(err, storage.ErrTransport):
		fix = "try again later"
	default:
		fix = "the site answered outside the login contract; update slivingdoc, or try again later"
	}
	return fmt.Errorf("app: the stored login could not mint a token for space %q at %s: %s; %s",
		cfg.bucket, cfg.login.Site, mcp.Redact(err.Error()), fix)
}
