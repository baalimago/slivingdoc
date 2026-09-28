// Package sitelogin is the client of the site's CLI login routes
// (architecture/login.md): POST /cli/v1/start opens a device approval,
// POST /cli/v1/token polls it until a person approves or denies it in the
// browser and then returns the account CLI key, GET /cli/v1/spaces lists
// the spaces that key reaches, POST /cli/v1/space-token trades the key
// for a short-lived token of one space, and POST /cli/v1/revoke withdraws
// a key with every token minted from it. The client validates everything
// the site answers before a caller prints it, opens it in a browser,
// stores it, or sends it on, and never follows a redirect.
package sitelogin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/httpstore"
)

// DefaultSite is the site a login uses when none is configured.
const DefaultSite = "https://www.slivingdoc.dev"

// The contract's route paths.
const (
	startPath  = "/cli/v1/start"
	tokenPath  = "/cli/v1/token"
	spacesPath = "/cli/v1/spaces"
	mintPath   = "/cli/v1/space-token"
	revokePath = "/cli/v1/revoke"
)

// slowDownStep is what a slow_down answer adds to the poll interval.
const slowDownStep = 5 * time.Second

// minInterval bounds how fast the client polls whatever the site says,
// and defaultInterval is the pause when the site names none.
const (
	minInterval     = time.Second
	defaultInterval = 5 * time.Second
)

// maxExpiresIn bounds how long the client waits for an approval, whatever
// lifetime the site answers.
const maxExpiresIn = 30 * time.Minute

// loginPagePath is the approval page; the complete page carries the user
// code in its fragment, so the code never reaches a server log or a
// Referer header.
const loginPagePath = "/cli/login"

// userCodePattern is the only user code shape the contract allows: two
// groups of four consonants.
var userCodePattern = regexp.MustCompile(`^[BCDFGHJKLMNPQRSTVWXZ]{4}-[BCDFGHJKLMNPQRSTVWXZ]{4}$`)

// TokenHint is added to every error after which a login key may exist
// that the client never received.
const TokenHint = "a login key may have been issued; revoke it on the Tokens page"

// bodyLimit bounds how much of an answer is read, spacesLimit the larger
// space list, and requestLimit is the size every request body stays
// below, as the site requires.
const (
	bodyLimit    = 16 << 10
	spacesLimit  = 256 << 10
	requestLimit = 4 << 10
)

// clientLimit is the longest client label the start route accepts.
const clientLimit = 64

var (
	// ErrDenied reports that the person denied the login in the browser.
	ErrDenied = errors.New("sitelogin: the login was denied in the browser")
	// ErrCodeExpired reports a device code the site no longer knows: it
	// expired, or it was already claimed.
	ErrCodeExpired = errors.New("sitelogin: the login code expired before it was approved")
	// ErrProtocol reports an answer outside the wire contract.
	ErrProtocol = errors.New("sitelogin: unexpected answer from the site")
	// ErrRefused reports an error answer of the site.
	ErrRefused = errors.New("sitelogin: the site refused the request")
	// ErrUnreachable reports a request that got no answer, or a 5xx
	// answer that broke off while it was read.
	ErrUnreachable = errors.New("sitelogin: the site is unreachable")
	// ErrBrokenAnswer reports a success answer that broke off while it was
	// read: the site did what was asked, but the client cannot tell the
	// result.
	ErrBrokenAnswer = errors.New("sitelogin: the site's answer broke off")
)

// RejectedError reports a key or token the site issued in an answer
// outside the wire contract. The credential itself is sendable, so the
// caller can revoke it rather than leave it valid and unseen; Error never
// contains it.
type RejectedError struct {
	Credential string
	Err        error
}

func (e *RejectedError) Error() string { return e.Err.Error() }

func (e *RejectedError) Unwrap() error { return e.Err }

// The backoff of a poll that got no usable answer: the pause doubles from
// the interval per failure in a row, up to maxRetryWait.
const maxRetryWait = time.Minute

// Doer sends one HTTP request. *http.Client satisfies it.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Config binds a client to one site.
type Config struct {
	// Site is the normalized site origin, for example
	// https://www.slivingdoc.dev.
	Site string
	// UserAgent is sent with every request.
	UserAgent string
	// Client sends the requests; nil uses a client that never follows a
	// redirect.
	Client Doer
	// Sleep waits d or until ctx ends; nil waits on a timer.
	Sleep func(ctx context.Context, d time.Duration) error
	// Now is the clock of the code's expiry; nil is time.Now.
	Now func() time.Time
}

// Client talks to one site's CLI login routes.
type Client struct {
	site      *url.URL
	userAgent string
	client    Doer
	sleep     func(context.Context, time.Duration) error
	now       func() time.Time
}

// New validates the site and returns a client. The site must be an
// absolute https origin, or http to this machine only: the approval and
// the token travel over it.
func New(cfg Config) (*Client, error) {
	site, err := parseSite(cfg.Site)
	if err != nil {
		return nil, err
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	sleep := cfg.Sleep
	if sleep == nil {
		sleep = timerSleep
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Client{site: site, userAgent: cfg.UserAgent, client: client, sleep: sleep, now: now}, nil
}

// Site is the normalized site origin.
func (c *Client) Site() string { return c.site.String() }

func parseSite(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("sitelogin: the site must be an absolute http or https URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("sitelogin: the site must be an origin without a path, user information, query or fragment")
	}
	if u.Scheme != "https" && !httpstore.IsLoopback(u.Hostname()) {
		return nil, errors.New("sitelogin: the site must use https so the login is never sent in clear text")
	}
	return &url.URL{Scheme: strings.ToLower(u.Scheme), Host: canonicalHost(u)}, nil
}

// canonicalHost is u's host in the one spelling a site is compared by:
// lower case, without a trailing dot, and without the scheme's default
// port, so https://www.slivingdoc.dev.:443 is the default site.
func canonicalHost(u *url.URL) string {
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	port := u.Port()
	if (port == "443" && strings.EqualFold(u.Scheme, "https")) || (port == "80" && strings.EqualFold(u.Scheme, "http")) {
		port = ""
	}
	if port == "" {
		if strings.Contains(host, ":") {
			return "[" + host + "]"
		}
		return host
	}
	return net.JoinHostPort(host, port)
}

func timerSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// StartRequest asks the site for a device approval of an account login.
type StartRequest struct {
	Access credentials.Access
	// Client labels the key, usually the host name; it is cut to the
	// printable ASCII the route accepts.
	Client string
}

// Approval is an open device approval.
type Approval struct {
	deviceCode string
	// UserCode is what the person compares on the approval page.
	UserCode string
	// URI is the approval page and CompleteURI the same page with the code
	// in its fragment. The client builds both from the site origin; the
	// site's own complete URI only has to agree.
	URI         string
	CompleteURI string
	// Interval is the pause between two polls.
	Interval time.Duration
	// Deadline is when the site forgets the approval.
	Deadline time.Time
}

type startBody struct {
	Access string `json:"access"`
	Client string `json:"client,omitempty"`
}

type startAnswer struct {
	DeviceCode              string `json:"deviceCode"`
	UserCode                string `json:"userCode"`
	VerificationURI         string `json:"verificationUri"`
	VerificationURIComplete string `json:"verificationUriComplete"`
	Interval                *int   `json:"interval"`
	ExpiresIn               int    `json:"expiresIn"`
}

// Start opens a device approval.
func (c *Client) Start(ctx context.Context, req StartRequest) (Approval, error) {
	var ans startAnswer
	if err := c.call(ctx, http.MethodPost, startPath, "", startBody{
		Access: string(req.Access),
		Client: label(req.Client),
	}, &ans); err != nil {
		return Approval{}, err
	}
	if ans.DeviceCode == "" || !printable(ans.DeviceCode) {
		return Approval{}, fmt.Errorf("%w: no usable device code", ErrProtocol)
	}
	if !userCodePattern.MatchString(ans.UserCode) {
		return Approval{}, fmt.Errorf("%w: the user code is not two groups of four consonants", ErrProtocol)
	}
	uri := c.site.String() + loginPagePath
	complete := uri + "#" + ans.UserCode
	if ans.VerificationURIComplete != complete {
		return Approval{}, fmt.Errorf("%w: verificationUriComplete is not %s", ErrProtocol, complete)
	}
	interval := defaultInterval
	if ans.Interval != nil {
		if *ans.Interval < 0 {
			return Approval{}, fmt.Errorf("%w: interval %d", ErrProtocol, *ans.Interval)
		}
		interval = max(time.Duration(*ans.Interval)*time.Second, minInterval)
	}
	if ans.ExpiresIn <= 0 {
		return Approval{}, fmt.Errorf("%w: expiresIn %d", ErrProtocol, ans.ExpiresIn)
	}
	return Approval{
		deviceCode:  ans.DeviceCode,
		UserCode:    ans.UserCode,
		URI:         uri,
		CompleteURI: complete,
		Interval:    interval,
		Deadline:    c.now().Add(min(time.Duration(ans.ExpiresIn)*time.Second, maxExpiresIn)),
	}, nil
}

// Issued is the account CLI key an approved login returns.
type Issued struct {
	// Key is sent only to the site: it lists spaces and mints space
	// tokens, and never reaches a storage endpoint.
	Key     string
	Access  credentials.Access
	Expires credentials.Expiry
	// Endpoint is the hosted API the key mints tokens for.
	Endpoint string
	// Account is the email of the person who approved the code. Whoever
	// submits a code first decides it, so a caller shows it: that is how a
	// person notices that someone else approved their login.
	Account string
}

type tokenBody struct {
	DeviceCode string `json:"deviceCode"`
}

type tokenAnswer struct {
	Key       string  `json:"key"`
	Access    string  `json:"access"`
	ExpiresAt *string `json:"expiresAt"`
	Endpoint  string  `json:"endpoint"`
	Account   string  `json:"account"`
}

// Wait polls the approval until the site issues the key, the person
// denies it, the code expires, or ctx ends. It pauses Interval before
// every poll and adds five seconds after each slow_down answer. A poll
// that gets no answer or a 5xx is retried with a doubling pause, never
// past the code's expiry, until the code expires; any other error answer
// ends the wait. A key issued in an answer outside the contract is
// returned inside a *RejectedError. Every other failure after which
// the site may have issued a key (a success answer that breaks off, is
// too large or cannot be decoded, or a cancellation while a poll is in
// flight) carries TokenHint.
func (c *Client) Wait(ctx context.Context, a Approval) (Issued, error) {
	interval := a.Interval
	var failures int
	var lastFailure error
	// lost is set once a poll went unanswered: that poll may have been the
	// one the site issued the key to, so a later expiry or a stop during
	// a later sleep carries the hint.
	lost := noLostPoll
	afterLoss := func(err error) error {
		if lost == lostPoll {
			return fmt.Errorf("%w; %s", err, TokenHint)
		}
		return err
	}
	for {
		wait := min(retryWait(interval, failures), max(a.Deadline.Sub(c.now()), 0))
		if err := c.sleep(ctx, wait); err != nil {
			return Issued{}, afterLoss(err)
		}
		if !c.now().Before(a.Deadline) {
			if lastFailure != nil {
				return Issued{}, afterLoss(fmt.Errorf("%w; the last poll failed: %w", ErrCodeExpired, lastFailure))
			}
			return Issued{}, afterLoss(ErrCodeExpired)
		}
		var ans tokenAnswer
		err := c.call(ctx, http.MethodPost, tokenPath, "", tokenBody{DeviceCode: a.deviceCode}, &ans)
		if err != nil && ctx.Err() != nil {
			return Issued{}, fmt.Errorf("sitelogin: stopped while a poll was in flight: %w; %s", ctx.Err(), TokenHint)
		}
		if transient(err) {
			lost = lostPoll
			failures++
			lastFailure = err
			continue
		}
		failures, lastFailure = 0, nil
		if errors.Is(err, ErrBrokenAnswer) || errors.Is(err, ErrProtocol) {
			return Issued{}, fmt.Errorf("%w; %s", err, TokenHint)
		}
		var refusal *Refusal
		if errors.As(err, &refusal) {
			switch refusal.Code {
			case "authorization_pending":
				continue
			case "slow_down":
				interval += slowDownStep
				continue
			case "access_denied":
				return Issued{}, withMessage(ErrDenied, refusal)
			case "expired_token":
				return Issued{}, afterLoss(withMessage(ErrCodeExpired, refusal))
			}
		}
		if err != nil {
			return Issued{}, err
		}
		got, err := issued(ans)
		var rejected *RejectedError
		if err != nil && !errors.As(err, &rejected) {
			return Issued{}, fmt.Errorf("%w; %s", err, TokenHint)
		}
		return got, err
	}
}

// pollLoss is whether a poll of a wait went unanswered.
type pollLoss int

const (
	noLostPoll pollLoss = iota
	lostPoll
)

// transient reports a poll failure worth retrying: no answer, or a 5xx.
func transient(err error) bool {
	var refusal *Refusal
	return errors.Is(err, ErrUnreachable) || (errors.As(err, &refusal) && refusal.Status >= http.StatusInternalServerError)
}

// retryWait is the pause before a poll after failures transient failures
// in a row.
func retryWait(interval time.Duration, failures int) time.Duration {
	wait := interval
	for range failures {
		if wait >= maxRetryWait/2 {
			return maxRetryWait
		}
		wait *= 2
	}
	return wait
}

// withMessage adds the site's message for a person to a terminal poll
// outcome.
func withMessage(outcome error, r *Refusal) error {
	if r.Message == "" {
		return outcome
	}
	return fmt.Errorf("%w (the site says: %s)", outcome, r.Message)
}

// emailLimit bounds the account and owners the client prints.
const emailLimit = 254

func issued(ans tokenAnswer) (Issued, error) {
	if err := httpstore.ValidateToken(ans.Key); err != nil {
		return Issued{}, fmt.Errorf("%w: the key cannot be used", ErrProtocol)
	}
	reject := rejecter(ans.Key)
	access, err := credentials.ParseAccess(ans.Access)
	if err != nil {
		return Issued{}, reject("%w", err)
	}
	if err := httpstore.ValidateEndpoint(ans.Endpoint); err != nil {
		return Issued{}, reject("%w", err)
	}
	if !usableEmail(ans.Account) {
		return Issued{}, reject("no usable account")
	}
	expires, err := expiry(ans.ExpiresAt)
	if err != nil {
		return Issued{}, reject("%w", err)
	}
	return Issued{Key: ans.Key, Access: access, Endpoint: ans.Endpoint, Expires: expires, Account: ans.Account}, nil
}

// rejecter builds the *RejectedError of an answer that carried the
// sendable credential secret.
func rejecter(secret string) func(format string, args ...any) error {
	return func(format string, args ...any) error {
		return &RejectedError{Credential: secret, Err: fmt.Errorf("%w: "+format, append([]any{ErrProtocol}, args...)...)}
	}
}

// expiry reads the required expiresAt of a key or a minted token.
func expiry(raw *string) (credentials.Expiry, error) {
	if raw == nil {
		return credentials.Expiry{}, errors.New("no expiresAt")
	}
	at, err := time.Parse(time.RFC3339, *raw)
	if err != nil {
		return credentials.Expiry{}, errors.New("expiresAt is not RFC 3339")
	}
	return credentials.ExpiresAt(at), nil
}

// Space is one space an account CLI key reaches.
type Space struct {
	Name string
	// Owner is the email of the space's owner; empty when a team owns it.
	Owner string
	// Access is what the key may do in the space: the grant, capped by the
	// key.
	Access credentials.Access
}

type spacesAnswer struct {
	Spaces *[]spaceAnswer `json:"spaces"`
}

type spaceAnswer struct {
	Name   string `json:"name"`
	Owner  string `json:"owner"`
	Access string `json:"access"`
}

// Spaces lists the spaces key reaches, in the site's order. A key the
// site no longer knows is a *Refusal with Status 401.
func (c *Client) Spaces(ctx context.Context, key string) ([]Space, error) {
	if err := httpstore.ValidateToken(key); err != nil {
		return nil, errors.New("sitelogin: the stored key cannot be sent")
	}
	var ans spacesAnswer
	if err := c.call(ctx, http.MethodGet, spacesPath, key, nil, &ans); err != nil {
		return nil, err
	}
	if ans.Spaces == nil {
		return nil, fmt.Errorf("%w: no spaces list", ErrProtocol)
	}
	spaces := make([]Space, 0, len(*ans.Spaces))
	seen := map[string]bool{}
	for i, sp := range *ans.Spaces {
		if err := httpstore.ValidateSpace(sp.Name); err != nil {
			return nil, fmt.Errorf("%w: spaces[%d]: %w", ErrProtocol, i, err)
		}
		if seen[sp.Name] {
			return nil, fmt.Errorf("%w: spaces[%d]: %s is listed twice", ErrProtocol, i, sp.Name)
		}
		seen[sp.Name] = true
		if !usableOwner(sp.Owner) {
			return nil, fmt.Errorf("%w: spaces[%d]: no usable owner", ErrProtocol, i)
		}
		access, err := credentials.ParseAccess(sp.Access)
		if err != nil {
			return nil, fmt.Errorf("%w: spaces[%d]: %w", ErrProtocol, i, err)
		}
		spaces = append(spaces, Space{Name: sp.Name, Owner: sp.Owner, Access: access})
	}
	return spaces, nil
}

// Minted is a short-lived token of one space, traded for an account CLI
// key. It is sent only to Endpoint and never stored.
type Minted struct {
	Token    string
	Space    string
	Access   credentials.Access
	Expires  credentials.Expiry
	Endpoint string
	// Owner is the email of the space's owner; empty when a team owns it.
	Owner string
}

type mintBody struct {
	Space  string `json:"space"`
	Access string `json:"access,omitempty"`
}

type mintAnswer struct {
	Token     string  `json:"token"`
	Space     string  `json:"space"`
	Access    string  `json:"access"`
	ExpiresAt *string `json:"expiresAt"`
	Endpoint  string  `json:"endpoint"`
	Owner     string  `json:"owner"`
}

// Mint trades key for a token of space. An empty access asks for the most
// the key and the grant allow; AccessRead caps the token at read. A token
// in an answer outside the contract (another space, more access than
// asked, no expiry) is returned inside a *RejectedError. The site's
// refusals are a *Refusal: 401 invalid_token, 404 no_space, 403
// access_denied or space_suspended, and 429 busy.
func (c *Client) Mint(ctx context.Context, key, space string, access credentials.Access) (Minted, error) {
	if err := httpstore.ValidateToken(key); err != nil {
		return Minted{}, errors.New("sitelogin: the stored key cannot be sent")
	}
	if err := httpstore.ValidateSpace(space); err != nil {
		return Minted{}, fmt.Errorf("sitelogin: %w", err)
	}
	var ans mintAnswer
	if err := c.call(ctx, http.MethodPost, mintPath, key, mintBody{Space: space, Access: string(access)}, &ans); err != nil {
		return Minted{}, err
	}
	if err := httpstore.ValidateToken(ans.Token); err != nil {
		return Minted{}, fmt.Errorf("%w: the minted token cannot be used", ErrProtocol)
	}
	reject := rejecter(ans.Token)
	if ans.Space != space {
		return Minted{}, reject("the token is for another space")
	}
	got, err := credentials.ParseAccess(ans.Access)
	if err != nil {
		return Minted{}, reject("%w", err)
	}
	if access == credentials.AccessRead && got != credentials.AccessRead {
		return Minted{}, reject("a read token was asked for and a write token issued")
	}
	if err := httpstore.ValidateEndpoint(ans.Endpoint); err != nil {
		return Minted{}, reject("%w", err)
	}
	if !usableOwner(ans.Owner) {
		return Minted{}, reject("no usable owner")
	}
	expires, err := expiry(ans.ExpiresAt)
	if err != nil {
		return Minted{}, reject("%w", err)
	}
	return Minted{Token: ans.Token, Space: ans.Space, Access: got, Expires: expires, Endpoint: ans.Endpoint, Owner: ans.Owner}, nil
}

// Revoke withdraws a credential at the site: a key together with every
// token minted from it, or one minted token alone. A credential the site
// no longer knows (401 invalid_token) is already withdrawn, so it counts
// as done; any other 401 is a failure.
func (c *Client) Revoke(ctx context.Context, credential string) error {
	if err := httpstore.ValidateToken(credential); err != nil {
		return errors.New("sitelogin: the stored key cannot be sent")
	}
	err := c.call(ctx, http.MethodPost, revokePath, credential, nil, nil)
	var refusal *Refusal
	if errors.As(err, &refusal) && refusal.Status == http.StatusUnauthorized && refusal.Code == "invalid_token" {
		return nil
	}
	return err
}

// Refusal is an error answer of the site: its status, its error code and
// its message, sanitized to one printable line.
type Refusal struct {
	Status  int
	Code    string
	Message string
}

func (r *Refusal) Error() string {
	msg := fmt.Sprintf("sitelogin: the site answered HTTP %d", r.Status)
	if r.Code != "" {
		msg += " " + r.Code
	}
	if r.Message != "" {
		msg += ": " + r.Message
	}
	return msg
}

func (r *Refusal) Unwrap() error { return ErrRefused }

type errorAnswer struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// call sends one request with an optional JSON body. A non-empty bearer
// authenticates it; a nil out expects 204 and no body.
func (c *Client) call(ctx context.Context, method, path, bearer string, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("sitelogin: encode request: %w", err)
		}
		if len(data) >= requestLimit {
			return fmt.Errorf("sitelogin: the request body is %d bytes, the site accepts less than %d", len(data), requestLimit)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.site.String()+path, body)
	if err != nil {
		return fmt.Errorf("sitelogin: build request: %w", err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w (%s): %w", ErrUnreachable, c.site, err)
	}
	defer resp.Body.Close()
	limit := bodyLimit
	if path == spacesPath {
		limit = spacesLimit
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		switch {
		case resp.StatusCode >= http.StatusInternalServerError:
			return fmt.Errorf("%w (%s): an HTTP %d answer broke off: %w", ErrUnreachable, c.site, resp.StatusCode, err)
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return fmt.Errorf("%w (%s): HTTP %d: %w", ErrBrokenAnswer, c.site, resp.StatusCode, err)
		}
	}
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300 && out == nil:
		return nil
	case resp.StatusCode == http.StatusOK && len(data) > limit:
		return fmt.Errorf("%w: the answer is larger than %d bytes", ErrProtocol, limit)
	case resp.StatusCode == http.StatusOK:
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("%w: the body is not the expected JSON", ErrProtocol)
		}
		return nil
	default:
		var ans errorAnswer
		_ = json.Unmarshal(data[:min(len(data), bodyLimit)], &ans)
		return &Refusal{Status: resp.StatusCode, Code: httpstore.Sanitize(ans.Error, 64), Message: httpstore.Sanitize(ans.Message, 300)}
	}
}

// label keeps the printable ASCII of a client label, cut to the length
// the route accepts.
func label(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if r >= ' ' && r <= '~' && b.Len() < clientLimit {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// usableEmail accepts an account or owner the client can print on one
// terminal line.
func usableEmail(s string) bool { return s != "" && len(s) <= emailLimit && printable(s) }

// usableOwner accepts a space owner's email, or none: a team owns the
// space.
func usableOwner(s string) bool { return s == "" || usableEmail(s) }

func printable(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] <= ' ' || s[i] > '~' {
			return false
		}
	}
	return true
}
