// Package httpstore implements the semantic object-store boundary over the
// hosted slivingdoc storage API, version 1: the six ObjectStore operations
// over HTTPS with a bearer token, addressed to one named space. The package
// maps HTTP statuses to the storage semantic errors, owns the notebook
// prefix join, and streams every object upload and download. Nothing in it
// is specific to one hosting provider.
package httpstore

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
	"strconv"
	"strings"
	"time"

	"github.com/baalimago/slivingdoc/internal/storage"
)

// API identifies the protocol this client speaks; GET /v1 must report it.
const (
	API     = "slivingdoc-storage"
	Version = 1
)

// deleteBatch is the most keys the API accepts in one delete request.
const deleteBatch = 1000

// errorBodyLimit bounds how much of an error response is read.
const errorBodyLimit = 4 << 10

var spaceRE = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// ErrInvalidSpace reports a space name outside the API grammar.
var ErrInvalidSpace = errors.New("httpstore: invalid space name")

// ErrInvalidToken reports a token that cannot travel in an HTTP header.
var ErrInvalidToken = errors.New("httpstore: invalid token")

// ValidateSpace reports whether name is a valid space name: lowercase
// letters, digits, and inner hyphens, 1 to 63 characters.
func ValidateSpace(name string) error {
	if !spaceRE.MatchString(name) {
		return fmt.Errorf("%w: %q must be 1 to 63 lowercase letters, digits, or inner hyphens", ErrInvalidSpace, name)
	}
	return nil
}

// ValidateToken reports whether token is non-empty printable ASCII without
// white space. The server owns the token grammar; the client only refuses
// what could not be sent.
func ValidateToken(token string) error {
	if token == "" {
		return fmt.Errorf("%w: empty", ErrInvalidToken)
	}
	for i := 0; i < len(token); i++ {
		if token[i] <= ' ' || token[i] > '~' {
			return fmt.Errorf("%w: contains white space or a non-printable character", ErrInvalidToken)
		}
	}
	return nil
}

// Doer sends one HTTP request. *http.Client satisfies it.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Config binds a store to one space of one server.
type Config struct {
	// Endpoint is the normalized server base URL without /v1, for example
	// https://api.slivingdoc.dev.
	Endpoint string
	// Space is the space name, the CLI's --space (or --bucket).
	Space string
	// Prefix is the notebook prefix inside the space; validated.
	Prefix string
	// Token is the API bearer token. It is sent only in the Authorization
	// header and never appears in an error. Tokens, when set, replaces it.
	Token string
	// Tokens supplies the bearer token of each request instead of Token:
	// a stored login's short-lived space tokens, re-minted before they
	// expire and once after the server refuses one.
	Tokens TokenSource
	// UserAgent is sent with every request; empty sends none of our own.
	UserAgent string
	// Client sends the requests; nil uses a default client.
	Client Doer
	// Retries bounds extra attempts of an idempotent request after a
	// transport failure or a 5xx answer. Zero means the default; a negative
	// value turns retries off.
	Retries int
	// Backoff returns the wait before retry attempt n (1-based); nil uses
	// the default.
	Backoff func(n int) time.Duration
}

// TokenSource supplies the bearer token of every request. Token returns
// the token to send now; Rejected reports one the server answered 401 to,
// so the next Token call returns another. The store sends a replayable
// request once more with that other token; a streamed upload fails, and
// the next request uses the new token. Both are called concurrently.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
	Rejected(token string)
}

// RenewingSource is a TokenSource of short-lived tokens, marked by
// RenewsTokens. A streamed upload whose token such a source gave was
// refused with 401 fails with storage.ErrCredentialRenewed instead of the
// access refusal a fixed token's 401 is: the token may have expired or
// been renewed while the upload ran, or revoked alone; the retry asks
// the source again and gets a new token either way. Only a refusal the
// source's renewal meets too (a revoked login key, a suspended account)
// fails the retry.
type RenewingSource interface {
	TokenSource
	RenewsTokens()
}

// staticToken is the TokenSource of a fixed Config.Token.
type staticToken string

func (t staticToken) Token(context.Context) (string, error) { return string(t), nil }

func (staticToken) Rejected(string) {}

// tokenSource validates cfg's token setting and returns its source.
func tokenSource(cfg Config) (TokenSource, error) {
	if cfg.Tokens != nil {
		return cfg.Tokens, nil
	}
	if err := ValidateToken(cfg.Token); err != nil {
		return nil, err
	}
	return staticToken(cfg.Token), nil
}

const defaultRetries = 2

func defaultBackoff(n int) time.Duration { return time.Duration(n) * 200 * time.Millisecond }

// Store is an ObjectStore bound to one space and one notebook prefix. All
// methods are safe for concurrent use.
type Store struct {
	client    Doer
	root      string // <endpoint>/v1
	space     string // <endpoint>/v1/spaces/<space>
	name      string
	prefix    string
	tokens    TokenSource
	userAgent string
	retries   int
	backoff   func(int) time.Duration
}

var _ storage.ObjectStore = (*Store)(nil)

// New validates the configuration and returns a store. It makes no request;
// CheckAccess proves the server and the token before first use.
func New(cfg Config) (*Store, error) {
	if err := ValidateEndpoint(cfg.Endpoint); err != nil {
		return nil, err
	}
	if err := ValidateSpace(cfg.Space); err != nil {
		return nil, err
	}
	if err := storage.ValidatePrefix(cfg.Prefix); err != nil {
		return nil, err
	}
	tokens, err := tokenSource(cfg)
	if err != nil {
		return nil, err
	}
	s := newClient(cfg, tokens)
	s.space = s.root + "/spaces/" + cfg.Space
	s.name = cfg.Space
	s.prefix = cfg.Prefix
	return s, nil
}

// newClient binds the transport settings of cfg: client, server root,
// token source, retries. It validates nothing and addresses no space.
func newClient(cfg Config, tokens TokenSource) *Store {
	client := cfg.Client
	if client == nil {
		// Never follow a redirect: Go would turn a PUT into a body-less
		// GET whose 2xx looks like a stored write, and would forward the
		// token. A 3xx answer is a refusal instead.
		client = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}
	}
	retries := cfg.Retries
	if retries == 0 {
		retries = defaultRetries
	}
	backoff := cfg.Backoff
	if backoff == nil {
		backoff = defaultBackoff
	}
	return &Store{
		client:    client,
		root:      strings.TrimSuffix(cfg.Endpoint, "/") + "/v1",
		tokens:    tokens,
		userAgent: cfg.UserAgent,
		retries:   retries,
		backoff:   backoff,
	}
}

// Access is what a token may do in its space.
type Access string

const (
	AccessRead  Access = "read"
	AccessWrite Access = "write"
)

// TokenInfo is the server's description of a token: the one space it
// reaches, under the token owner's own name for it.
type TokenInfo struct {
	Space  string
	Access Access
	// ExpiresAt is when the token stops working; zero for a token that
	// never expires.
	ExpiresAt time.Time
}

// ErrTokenLookupUnsupported reports a server without GET /v1/token, which
// cannot name the space a token reaches; the space must then be given.
var ErrTokenLookupUnsupported = errors.New("httpstore: the server cannot name the space a token reaches")

// tokenBody is the GET /v1/token answer.
type tokenBody struct {
	Space     string  `json:"space"`
	Access    string  `json:"access"`
	ExpiresAt *string `json:"expiresAt"`
}

// DescribeToken asks the server which space the token reaches
// (GET /v1/token), after the same tokenless server check as CheckAccess,
// so the token only goes to an endpoint that answered as this API.
// cfg.Space and cfg.Prefix are ignored. A token that reaches no space, or
// that the server refuses, is ErrAccessDenied; a server without the
// endpoint is ErrTokenLookupUnsupported; an answer outside the API grammar
// is ErrIncompatible.
func DescribeToken(ctx context.Context, cfg Config) (TokenInfo, error) {
	if err := ValidateEndpoint(cfg.Endpoint); err != nil {
		return TokenInfo{}, err
	}
	tokens, err := tokenSource(cfg)
	if err != nil {
		return TokenInfo{}, err
	}
	s := newClient(cfg, tokens)
	if err := s.checkServer(ctx); err != nil {
		return TokenInfo{}, err
	}
	resp, err := s.do(ctx, func() (*http.Request, error) {
		return s.request(ctx, http.MethodGet, s.root+"/token", nil, true)
	})
	if err != nil {
		return TokenInfo{}, fmt.Errorf("httpstore: describe token: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer drain(resp)
		return TokenInfo{}, fmt.Errorf("httpstore: describe token: %w", tokenLookupError(s, resp))
	}
	var body tokenBody
	if err := decodeJSON(resp, &body); err != nil {
		return TokenInfo{}, fmt.Errorf("httpstore: describe token: %w: %w", err, storage.ErrIncompatible)
	}
	return body.info()
}

// tokenLookupError maps a refused GET /v1/token. Only no_space means the
// token reaches nothing; any other 404, or a 405, is a server that predates
// the endpoint.
func tokenLookupError(s *Store, resp *http.Response) error {
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
		return s.statusError(resp)
	}
	body := readAPIError(resp)
	refusal := newRefusal(resp.StatusCode, body)
	if resp.StatusCode == http.StatusNotFound && body.Reason == "no_space" {
		refusal.Err = storage.ErrAccessDenied
		return fmt.Errorf("the token reaches no space: it was made without one, or its space is gone: %w", refusal)
	}
	return fmt.Errorf("%s: %w", refusal.Detail, ErrTokenLookupUnsupported)
}

func (b tokenBody) info() (TokenInfo, error) {
	if err := ValidateSpace(b.Space); err != nil {
		return TokenInfo{}, fmt.Errorf("httpstore: describe token: the server named no valid space: %w", storage.ErrIncompatible)
	}
	access := Access(b.Access)
	if access != AccessRead && access != AccessWrite {
		return TokenInfo{}, fmt.Errorf("httpstore: describe token: access %q is neither read nor write: %w", Sanitize(b.Access, 16), storage.ErrIncompatible)
	}
	info := TokenInfo{Space: b.Space, Access: access}
	if b.ExpiresAt != nil {
		at, err := time.Parse(time.RFC3339Nano, *b.ExpiresAt)
		if err != nil {
			return TokenInfo{}, fmt.Errorf("httpstore: describe token: expiresAt is not a time: %w", storage.ErrIncompatible)
		}
		info.ExpiresAt = at
	}
	return info, nil
}

// ValidateEndpoint accepts an absolute https URL, or http to this machine
// only, so the token is never sent in clear text over a network.
func ValidateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("httpstore: endpoint must be an absolute http or https URL")
	}
	if u.Scheme != "https" && !IsLoopback(u.Hostname()) {
		return errors.New("httpstore: the endpoint must use https so the token is never sent in clear text")
	}
	return nil
}

// IsLoopback reports whether host names this machine.
func IsLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// serverInfo is the GET /v1 description.
type serverInfo struct {
	API               string `json:"api"`
	Version           int    `json:"version"`
	ConditionalWrites bool   `json:"conditionalWrites"`
}

// checkServer proves, without the token, that the endpoint speaks this API
// version with the conditional-write semantics the protocol needs, so the
// token is never sent to a server that has not identified itself.
// ErrIncompatible reports a server this client does not understand.
func (s *Store) checkServer(ctx context.Context) error {
	resp, err := s.do(ctx, func() (*http.Request, error) {
		return s.request(ctx, http.MethodGet, s.root, nil, false)
	})
	if err != nil {
		return fmt.Errorf("httpstore: describe server: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer drain(resp)
		if resp.StatusCode == http.StatusNotFound {
			// No storage API at this endpoint, whatever the body says.
			return fmt.Errorf("httpstore: describe server: HTTP 404: no storage API at this endpoint: %w", storage.ErrIncompatible)
		}
		err := s.statusError(resp)
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			// The server is busy or down, not a different kind of server.
			return fmt.Errorf("httpstore: describe server: %w", err)
		}
		return fmt.Errorf("httpstore: describe server: %w: %w", err, storage.ErrIncompatible)
	}
	var info serverInfo
	err = decodeJSON(resp, &info)
	if err != nil {
		return fmt.Errorf("httpstore: describe server: %w: %w", err, storage.ErrIncompatible)
	}
	if info.API != API || info.Version != Version {
		return fmt.Errorf("httpstore: server speaks %q version %d, want %q version %d: %w",
			info.API, info.Version, API, Version, storage.ErrIncompatible)
	}
	if !info.ConditionalWrites {
		return fmt.Errorf("httpstore: server does not promise conditional writes: %w", storage.ErrIncompatible)
	}
	return nil
}

// CheckAccess proves the server speaks this API version with the
// conditional-write semantics the protocol needs, and that the token
// reaches the space. It replaces the write probe for hosted stores: the
// server promises the semantics, and a read-only token could not run the
// probe. ErrIncompatible reports a server this client does not understand;
// ErrAccessDenied reports a token that does not reach the space.
func (s *Store) CheckAccess(ctx context.Context) error {
	if err := s.checkServer(ctx); err != nil {
		return err
	}
	resp, err := s.do(ctx, func() (*http.Request, error) {
		return s.request(ctx, http.MethodGet, s.space+"/usage", nil, true)
	})
	if err != nil {
		return fmt.Errorf("httpstore: check space %q: %w", s.name, err)
	}
	defer drain(resp)
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return fmt.Errorf("httpstore: check space %q: %w", s.name, s.usageNotFound(resp))
	default:
		return fmt.Errorf("httpstore: check space %q: %w", s.name, s.statusError(resp))
	}
}

func (s *Store) ReadObject(ctx context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error) {
	resp, err := s.do(ctx, func() (*http.Request, error) {
		return s.request(ctx, http.MethodGet, s.objectURL(key), nil, true)
	})
	if err != nil {
		return nil, storage.ObjectInfo{}, fmt.Errorf("httpstore: get %s: %w", key, err)
	}
	if resp.StatusCode != http.StatusOK {
		defer drain(resp)
		return nil, storage.ObjectInfo{}, fmt.Errorf("httpstore: get %s: %w", key, s.statusError(resp))
	}
	fields := map[string]string{}
	for _, name := range []string{storage.MetaSHA256, storage.MetaSize, storage.MetaKind, storage.MetaGeneration} {
		if v := resp.Header.Get(name); v != "" {
			fields[name] = v
		}
	}
	meta, err := storage.ParseMetadata(fields)
	if err != nil {
		drain(resp)
		return nil, storage.ObjectInfo{}, fmt.Errorf("httpstore: get %s: %w: %w", key, storage.ErrIntegrity, err)
	}
	return resp.Body, storage.ObjectInfo{
		Size: resp.ContentLength,
		ETag: storage.ETag(resp.Header.Get("ETag")),
		Meta: meta,
	}, nil
}

func (s *Store) PutObject(ctx context.Context, key string, r io.Reader, meta storage.Metadata) error {
	req, err := s.request(ctx, http.MethodPut, s.objectURL(key), io.NopCloser(r), true)
	if err != nil {
		return fmt.Errorf("httpstore: put %s: %w", key, err)
	}
	req.ContentLength = int64(meta.Size)
	if meta.Size == 0 {
		req.Body = http.NoBody
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	for name, v := range meta.Fields() {
		req.Header.Set(name, v)
	}
	resp, err := s.send(req)
	if err != nil {
		return fmt.Errorf("httpstore: put %s: %w", key, err)
	}
	defer drain(resp)
	if !success(resp.StatusCode) {
		return fmt.Errorf("httpstore: put %s: %w", key, s.writeError(resp))
	}
	return nil
}

func (s *Store) CreateObject(ctx context.Context, key string, data []byte) (storage.ETag, error) {
	etag, err := s.putSmall(ctx, key, data, "If-None-Match", "*")
	if err != nil {
		return "", fmt.Errorf("httpstore: create %s: %w", key, err)
	}
	return etag, nil
}

func (s *Store) ReplaceObject(ctx context.Context, key string, etag storage.ETag, data []byte) (storage.ETag, error) {
	// The API answers If-Match on an absent object with 412, so a 404
	// here means the space is gone or the grant was revoked.
	next, err := s.putSmall(ctx, key, data, "If-Match", string(etag))
	if err != nil {
		return "", fmt.Errorf("httpstore: replace %s: %w", key, err)
	}
	return next, nil
}

func (s *Store) putSmall(ctx context.Context, key string, data []byte, condition, value string) (storage.ETag, error) {
	req, err := s.request(ctx, http.MethodPut, s.objectURL(key), bytes.NewReader(data), true)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set(condition, value)
	resp, err := s.send(req)
	if err != nil {
		return "", err
	}
	defer drain(resp)
	if !success(resp.StatusCode) {
		return "", s.writeError(resp)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		return "", fmt.Errorf("server returned no etag: %w", storage.ErrTransport)
	}
	return storage.ETag(etag), nil
}

// listPage is one GET objects answer.
type listPage struct {
	Keys   []string `json:"keys"`
	Cursor *string  `json:"cursor"`
}

func (s *Store) ListObjects(ctx context.Context, prefix string, fn func(key string) error) error {
	full := storage.JoinKey(s.prefix, prefix)
	strip := storage.JoinKey(s.prefix, "")
	cursor := ""
	seen := map[string]bool{}
	for {
		q := url.Values{"prefix": {full}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		resp, err := s.do(ctx, func() (*http.Request, error) {
			return s.request(ctx, http.MethodGet, s.space+"/objects?"+q.Encode(), nil, true)
		})
		if err != nil {
			return fmt.Errorf("httpstore: list %s: %w", prefix, err)
		}
		if resp.StatusCode != http.StatusOK {
			err := s.writeError(resp)
			drain(resp)
			return fmt.Errorf("httpstore: list %s: %w", prefix, err)
		}
		var page listPage
		if err := decodeJSON(resp, &page); err != nil {
			return fmt.Errorf("httpstore: list %s: %w: %w", prefix, err, storage.ErrTransport)
		}
		for _, key := range page.Keys {
			if !strings.HasPrefix(key, full) {
				return fmt.Errorf("httpstore: list %s: server returned a key outside the prefix: %w", prefix, storage.ErrIntegrity)
			}
			if err := fn(strings.TrimPrefix(key, strip)); err != nil {
				return err
			}
		}
		if page.Cursor == nil || *page.Cursor == "" {
			return nil
		}
		if seen[*page.Cursor] {
			return fmt.Errorf("httpstore: list %s: server repeated a cursor: %w", prefix, storage.ErrTransport)
		}
		seen[*page.Cursor] = true
		cursor = *page.Cursor
	}
}

func (s *Store) DeleteObjects(ctx context.Context, keys []string) error {
	for start := 0; start < len(keys); start += deleteBatch {
		end := min(start+deleteBatch, len(keys))
		full := make([]string, 0, end-start)
		for _, key := range keys[start:end] {
			full = append(full, storage.JoinKey(s.prefix, key))
		}
		body, err := json.Marshal(struct {
			Keys []string `json:"keys"`
		}{full})
		if err != nil {
			return fmt.Errorf("httpstore: delete: %w", err)
		}
		resp, err := s.do(ctx, func() (*http.Request, error) {
			req, err := s.request(ctx, http.MethodPost, s.space+"/delete", bytes.NewReader(body), true)
			if err == nil {
				req.Header.Set("Content-Type", "application/json")
			}
			return req, err
		})
		if err != nil {
			return fmt.Errorf("httpstore: delete: %w", err)
		}
		status := resp.StatusCode
		var werr error
		if !success(status) {
			werr = s.writeError(resp)
		}
		drain(resp)
		if werr != nil {
			return fmt.Errorf("httpstore: delete: %w", werr)
		}
	}
	return nil
}

// objectURL addresses one object: the joined key, each segment escaped.
func (s *Store) objectURL(key string) string {
	segments := strings.Split(storage.JoinKey(s.prefix, key), "/")
	for i, seg := range segments {
		if seg == "." || seg == ".." {
			// Keys never hold dot segments; escape them so no proxy or
			// server resolves one out of the space.
			seg = strings.ReplaceAll(seg, ".", "%2E")
			segments[i] = seg
			continue
		}
		segments[i] = url.PathEscape(seg)
	}
	return s.space + "/objects/" + strings.Join(segments, "/")
}

func (s *Store) request(ctx context.Context, method, target string, body io.Reader, auth bool) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if auth {
		token, err := s.tokens.Token(ctx)
		if err != nil {
			return nil, fmt.Errorf("token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	// Ask for the stored bytes: a transparently decompressed body would
	// lose its length and differ from the pack descriptor.
	req.Header.Set("Accept-Encoding", "identity")
	if s.userAgent != "" {
		req.Header.Set("User-Agent", s.userAgent)
	}
	return req, nil
}

// send performs one request. A failure before any response is ambiguous
// for a write: the request may have landed. A 401 reports the token to the
// source; a replayable request goes once more when the source then has
// another token. A streamed one returns the 401, or, for a RenewingSource,
// a storage.Refusal of storage.ErrCredentialRenewed that keeps the
// server's sanitized message, so the caller retries instead of reporting a
// refused credential.
func (s *Store) send(req *http.Request) (*http.Response, error) {
	resp, err := s.sendOnce(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	used, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return resp, nil
	}
	s.tokens.Rejected(used)
	if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
		if _, renewing := s.tokens.(RenewingSource); !renewing {
			return resp, nil
		}
		refusal := newRefusal(resp.StatusCode, readAPIError(resp))
		refusal.Err = storage.ErrCredentialRenewed
		drain(resp)
		return nil, fmt.Errorf("the server refused the token of an upload that cannot be sent again: %w", refusal)
	}
	next, err := s.tokens.Token(req.Context())
	if err != nil {
		drain(resp)
		return nil, fmt.Errorf("the server refused the token and no other could be had: %w", err)
	}
	if next == used {
		return resp, nil
	}
	retry := req.Clone(req.Context())
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return resp, nil
		}
		retry.Body = body
	}
	drain(resp)
	retry.Header.Set("Authorization", "Bearer "+next)
	return s.sendOnce(retry)
}

func (s *Store) sendOnce(req *http.Request) (*http.Response, error) {
	resp, err := s.client.Do(req)
	if err != nil {
		if ctxErr := req.Context().Err(); ctxErr != nil {
			return nil, fmt.Errorf("%w: %w", ctxErr, storage.ErrTransport)
		}
		return nil, fmt.Errorf("%s: %w", oneLine(err), storage.ErrTransport)
	}
	return resp, nil
}

// do sends an idempotent request, retrying a transport failure or a 5xx
// answer within the retry bound. The last answer or error is returned.
func (s *Store) do(ctx context.Context, build func() (*http.Request, error)) (*http.Response, error) {
	attempts := 1 + s.retries
	var lastErr error
	for n := 1; ; n++ {
		req, err := build()
		if err != nil {
			return nil, err
		}
		resp, err := s.send(req)
		retry := n < attempts && ctx.Err() == nil
		switch {
		case err != nil && retry:
			lastErr = err
		case err != nil:
			return nil, err
		case resp.StatusCode >= 500 && resp.StatusCode != http.StatusInsufficientStorage && retry:
			drain(resp)
			lastErr = nil
		default:
			return resp, nil
		}
		timer := time.NewTimer(s.backoff(n))
		select {
		case <-ctx.Done():
			timer.Stop()
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, fmt.Errorf("%w: %w", ctx.Err(), storage.ErrTransport)
		case <-timer.C:
		}
	}
}

// apiError is the JSON error body of every refusal.
type apiError struct {
	Code    string `json:"error"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// statusError maps a refusal to a storage semantic error. The status and
// the reason decide the category; the server's code and reason are kept as
// diagnostic text, and its message, which is written for the person running
// the client, travels as storage.Refusal.Message.
func (s *Store) statusError(resp *http.Response) error {
	body := readAPIError(resp)
	code := strings.ToLower(Sanitize(body.Code, 64))
	reason := strings.ToLower(Sanitize(body.Reason, 64))
	refusal := newRefusal(resp.StatusCode, body)
	switch {
	case (resp.StatusCode == http.StatusInsufficientStorage || code == "quota_exceeded") && reason == "request_limit":
		refusal.Err = storage.ErrRequestLimit
	case resp.StatusCode == http.StatusInsufficientStorage || code == "quota_exceeded":
		refusal.Err = storage.ErrQuotaExceeded
	case resp.StatusCode == http.StatusTooManyRequests || code == "rate_limited":
		refusal.Err = storage.ErrRateLimited
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		refusal.Err = storage.ErrAccessDenied
	case resp.StatusCode == http.StatusNotFound && body.Code == "not_found" && body.Reason == reasonNoObject:
		refusal.Err = storage.ErrNotFound
	case resp.StatusCode == http.StatusNotFound:
		// Only no_object means absent. A missing space, an unknown route,
		// or an older gateway's 404 without a reason must never read as an
		// empty store, or a pull would delete the caller's notes.
		refusal.Err = storage.ErrAccessDenied
		return fmt.Errorf("%s: %w", s.unreachable(body.Reason), refusal)
	case resp.StatusCode == http.StatusPreconditionFailed:
		refusal.Err = storage.ErrPreconditionFailed
	case resp.StatusCode == http.StatusRequestEntityTooLarge:
		refusal.Err = storage.ErrTooLarge
	case resp.StatusCode >= 500:
		refusal.Err = storage.ErrTransport
	default:
		return fmt.Errorf("server refused the request: %s", appendMessage(refusal.Detail, refusal.Message))
	}
	return refusal
}

func appendMessage(detail, msg string) string {
	if msg == "" {
		return detail
	}
	return detail + ": " + msg
}

// readAPIError decodes a bounded error body; an undecodable body is the
// zero value.
func readAPIError(resp *http.Response) apiError {
	var body apiError
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
	_ = json.Unmarshal(raw, &body)
	return body
}

// newRefusal carries a refusal's diagnostic detail (status, lowercased code
// and reason) and the server's sanitized message; the caller sets Err.
func newRefusal(status int, body apiError) *storage.Refusal {
	detail := "HTTP " + strconv.Itoa(status)
	if code := strings.ToLower(Sanitize(body.Code, 64)); code != "" {
		detail += " " + code
	}
	if reason := strings.ToLower(Sanitize(body.Reason, 64)); reason != "" {
		detail += " (" + reason + ")"
	}
	return &storage.Refusal{Detail: detail, Message: Sanitize(body.Message, 300)}
}

// usageNotFound maps a 404 of the usage check, which addresses the space,
// so no 404 there means an absent object: no_space keeps its own
// description, and anything else says the endpoint serves no space API.
// The gateway's message travels as the refusal's, like any other.
func (s *Store) usageNotFound(resp *http.Response) error {
	body := readAPIError(resp)
	refusal := newRefusal(resp.StatusCode, body)
	refusal.Err = storage.ErrAccessDenied
	if body.Reason == "no_space" {
		return fmt.Errorf("%s: %w", s.unreachable(body.Reason), refusal)
	}
	return fmt.Errorf("the endpoint has no such space API; check --endpoint: %w", refusal)
}

// reasonNoObject is the only 404 reason that means an absent object
// (the gateway's API.md, Read).
const reasonNoObject = "no_object"

// unreachable describes a 404 that is not an absent object.
func (s *Store) unreachable(reason string) string {
	switch reason {
	case "no_space":
		return fmt.Sprintf("space %q does not exist or the token was not granted it", s.name)
	case "no_endpoint":
		return "the server has no such endpoint; check --endpoint"
	default:
		return fmt.Sprintf("space %q is not reachable: the server answered 404 without saying what is missing", s.name)
	}
}

// writeError maps a refusal of a request addressed to the space. A 404
// there is never an absent object, even one that says no_object.
func (s *Store) writeError(resp *http.Response) error {
	err := s.statusError(resp)
	if errors.Is(err, storage.ErrNotFound) {
		return fmt.Errorf("space %q answered 404 to a request addressed to the space: %w", s.name, storage.ErrAccessDenied)
	}
	return err
}

func success(status int) bool { return status >= 200 && status < 300 }

func decodeJSON(resp *http.Response, v any) error {
	defer drain(resp)
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(v); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// drain discards a bounded remainder and closes the body, so the
// connection can be reused.
func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, errorBodyLimit))
	_ = resp.Body.Close()
}

// Sanitize keeps printable ASCII, collapses white space, and bounds the
// length, so server text stays one safe diagnostic line.
func Sanitize(s string, limit int) string {
	var b strings.Builder
	for _, r := range s {
		if r >= ' ' && r <= '~' {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if len(out) > limit {
		out = out[:limit] + "..."
	}
	return out
}

func oneLine(err error) string { return strings.Join(strings.Fields(err.Error()), " ") }
