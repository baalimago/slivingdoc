// Package sitetest is a test-only reference server of the site's CLI login
// routes (architecture/login.md): POST /cli/v1/start, /cli/v1/token,
// GET /cli/v1/spaces, POST /cli/v1/space-token and /cli/v1/revoke over a
// local httptest server, answering each approval with a scripted sequence
// instead of a person in a browser.
package sitetest

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Issue is the account CLI key an approved script returns.
type Issue struct {
	Key    string
	Access string
	// ExpiresAt is the key's expiry; the zero time answers null.
	ExpiresAt time.Time
	// Endpoint is the hosted API the key mints tokens for.
	Endpoint string
	// Account is the approver's email.
	Account string
}

// Space is one space a key reaches, as GET /cli/v1/spaces lists it.
type Space struct {
	Name   string `json:"name"`
	Owner  string `json:"owner"`
	Access string `json:"access"`
}

// Minted is one token the site minted from a key.
type Minted struct {
	Key       string
	Token     string
	Space     string
	Access    string
	ExpiresAt time.Time
	Endpoint  string
}

// key is what the site knows of one issued account CLI key.
type key struct {
	endpoint string
	access   string
	spaces   []Space
	children []string
}

// Script is how the site answers the polls of one approval: each code of
// Pending in turn (authorization_pending, slow_down), then Final when it
// is set (access_denied, expired_token), else the Issue. After an issue
// the device code is claimed and every later poll is expired_token.
//
// The remaining fields break the issuing answer instead: Body replaces
// its JSON with raw bytes, Truncate announces more bytes than it sends,
// and Stall holds the poll until the client gives up on it.
type Script struct {
	Pending []string
	Final   string
	Issue   Issue

	Body     string
	Truncate bool
	Stall    bool
}

// StartBody is one recorded start request, decoded by decodeStrict: a
// body with any other key, a key in another case, a null, a value of
// another JSON type, or data after it is refused as invalid_request.
type StartBody struct {
	Access string `json:"access"`
	Client string `json:"client"`
}

type refusal struct {
	status int
	code   string
}

// barrier releases the issuing polls it holds once n of them wait.
type barrier struct {
	n       int
	waiting int
	release chan struct{}
}

type approval struct {
	script  Script
	polls   int
	claimed bool
}

// Site is a running reference site.
type Site struct {
	srv *httptest.Server

	mu        sync.Mutex
	next      Script
	queue     []Script
	interval  int
	expiresIn int
	origin    string
	userCode  string
	complete  string
	revokeErr *refusal
	mintErr   *refusal
	mintBody  string
	lifetime  time.Duration
	minter    func(Minted)
	suspended map[string]bool
	hold      *barrier
	approvals map[string]*approval
	keys      map[string]*key
	minted    map[string]bool
	mints     []Minted
	starts    []StartBody
	revoked   []string
	polls     int
}

// Start runs a site until the test ends. Approvals poll every second and
// live ten minutes, and minted tokens live an hour, unless SetTiming and
// SetMintLifetime say otherwise.
func Start(t *testing.T) *Site {
	t.Helper()
	s := &Site{
		interval: 1, expiresIn: 600, userCode: "BCDF-GHJK", lifetime: time.Hour,
		approvals: map[string]*approval{}, keys: map[string]*key{}, minted: map[string]bool{}, suspended: map[string]bool{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cli/v1/start", jsonOnly(s.start))
	mux.HandleFunc("POST /cli/v1/token", jsonOnly(s.token))
	mux.HandleFunc("GET /cli/v1/spaces", s.spaces)
	mux.HandleFunc("POST /cli/v1/space-token", jsonOnly(s.mint))
	mux.HandleFunc("POST /cli/v1/revoke", s.revoke)
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

// URL is the site origin.
func (s *Site) URL() string { return s.srv.URL }

// Next sets the script of the approvals started from now on.
func (s *Site) Next(script Script) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next = script
}

// Queue sets the scripts of the next approvals, one per start in order;
// once they are used up, starts take Next's script again.
func (s *Site) Queue(scripts ...Script) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queue = append(s.queue, scripts...)
}

// SetOrigin makes later starts name origin instead of the server's own
// URL in the approval page addresses, as a site behind another name
// would.
func (s *Site) SetOrigin(origin string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.origin = origin
}

// SetUserCode sets the code later starts answer; the default is
// BCDF-GHJK.
func (s *Site) SetUserCode(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userCode = code
}

// SetCompleteURI makes later starts answer uri as the approval page with
// the code filled in, instead of the page the code belongs to.
func (s *Site) SetCompleteURI(uri string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.complete = uri
}

// RefuseRevoke makes the next revoke answer status with the error code,
// whatever the credential.
func (s *Site) RefuseRevoke(status int, code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revokeErr = &refusal{status: status, code: code}
}

// HoldIssues holds the next issuing polls until n of them wait, then
// answers those n together; later issuing polls are not held.
func (s *Site) HoldIssues(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hold = &barrier{n: n, release: make(chan struct{})}
}

// SetTiming sets the interval and lifetime, in seconds, that later starts
// answer.
func (s *Site) SetTiming(interval, expiresIn int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interval, s.expiresIn = interval, expiresIn
}

// Starts returns every start request so far.
func (s *Site) Starts() []StartBody {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]StartBody(nil), s.starts...)
}

// Revoked returns every key and token revoked through POST
// /cli/v1/revoke so far, in order: a revoked key is followed by the tokens
// minted from it. Revoke's withdrawals are not listed.
func (s *Site) Revoked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.revoked...)
}

// Polls counts the token polls of every approval.
func (s *Site) Polls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.polls
}

// Revoke withdraws a key and its minted tokens, or one minted token, as
// if its owner revoked it on the Tokens page.
func (s *Site) Revoke(credential string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.revoked)
	s.withdraw(credential)
	s.revoked = s.revoked[:n]
}

// Close stops the site, so every later request fails to connect.
func (s *Site) Close() { s.srv.Close() }

// Issued marks key as issued by this site for endpoint, so it lists
// spaces, mints tokens and can be revoked; a script's issue does this
// itself. Its access is write.
func (s *Site) Issued(k, endpoint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.issue(k, endpoint, "write")
}

func (s *Site) issue(k, endpoint, access string) {
	if old, ok := s.keys[k]; ok {
		old.endpoint, old.access = endpoint, access
		return
	}
	s.keys[k] = &key{endpoint: endpoint, access: access}
}

// SetSpaces sets the spaces key reaches, whether or not it is issued yet;
// a space's access is capped by the key's when listed or minted.
func (s *Site) SetSpaces(k string, spaces ...Space) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.keys[k]; !ok {
		s.keys[k] = &key{}
	}
	s.keys[k].spaces = append([]Space(nil), spaces...)
}

// Suspend makes the space's owner suspended: it is left out of every list
// and minting a token for it answers space_suspended.
func (s *Site) Suspend(space string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.suspended[space] = true
}

// RefuseMint makes the next mint answer status with the error code.
func (s *Site) RefuseMint(status int, code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mintErr = &refusal{status: status, code: code}
}

// BreakMint makes the next successful mint answer body instead of its
// JSON; the token it minted is still recorded and live.
func (s *Site) BreakMint(body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mintBody = body
}

// SetMintLifetime sets how long later minted tokens live.
func (s *Site) SetMintLifetime(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lifetime = d
}

// OnMint calls fn with every token minted from now on, before the answer
// is sent: a test grants it on its gateway there.
func (s *Site) OnMint(fn func(Minted)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.minter = fn
}

// Mints returns every token minted so far, in order.
func (s *Site) Mints() []Minted {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Minted(nil), s.mints...)
}

func (s *Site) start(w http.ResponseWriter, r *http.Request) {
	var body StartBody
	if err := decodeStrict(r, &body, "access", "client"); err != nil || (body.Access != "write" && body.Access != "read") {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	device := base64.RawURLEncoding.EncodeToString(raw)
	s.mu.Lock()
	s.starts = append(s.starts, body)
	script := s.next
	if len(s.queue) > 0 {
		script, s.queue = s.queue[0], s.queue[1:]
	}
	s.approvals[device] = &approval{script: script}
	interval, expiresIn, userCode := s.interval, s.expiresIn, s.userCode
	origin := s.origin
	if origin == "" {
		origin = s.srv.URL
	}
	complete := s.complete
	if complete == "" {
		complete = origin + "/cli/login#" + userCode
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"deviceCode":              device,
		"userCode":                userCode,
		"verificationUri":         origin + "/cli/login",
		"verificationUriComplete": complete,
		"interval":                interval,
		"expiresIn":               expiresIn,
	})
}

func (s *Site) token(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DeviceCode string `json:"deviceCode"`
	}
	if err := decodeStrict(r, &body, "deviceCode"); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	s.mu.Lock()
	s.polls++
	a, ok := s.approvals[body.DeviceCode]
	if !ok || a.claimed {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, "expired_token")
		return
	}
	if a.polls < len(a.script.Pending) {
		code := a.script.Pending[a.polls]
		a.polls++
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, code)
		return
	}
	if a.script.Final != "" {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, a.script.Final)
		return
	}
	a.claimed = true
	script := a.script
	s.issue(script.Issue.Key, script.Issue.Endpoint, script.Issue.Access)
	release := s.join()
	s.mu.Unlock()
	if release != nil {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
	}
	if script.Stall {
		<-r.Context().Done()
		return
	}
	issue := script.Issue
	var expires any
	if !issue.ExpiresAt.IsZero() {
		expires = issue.ExpiresAt.UTC().Format(time.RFC3339)
	}
	data, _ := json.Marshal(map[string]any{
		"key":       issue.Key,
		"access":    issue.Access,
		"expiresAt": expires,
		"endpoint":  issue.Endpoint,
		"account":   issue.Account,
	})
	if script.Body != "" {
		data = []byte(script.Body)
	}
	w.Header().Set("Content-Type", "application/json")
	if script.Truncate {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)+64))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// join enters an issuing poll into the barrier, if one is set, and
// returns the channel that releases it; the caller holds s.mu.
func (s *Site) join() chan struct{} {
	b := s.hold
	if b == nil {
		return nil
	}
	b.waiting++
	if b.waiting == b.n {
		close(b.release)
		s.hold = nil
	}
	return b.release
}

// liveKey returns the issued key the request's bearer names; a minted
// token, or a key the site does not know, is nil.
func (s *Site) liveKey(r *http.Request) (string, *key) {
	bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return "", nil
	}
	k := s.keys[bearer]
	if k == nil || k.endpoint == "" {
		return "", nil
	}
	return bearer, k
}

// capped is a space's access capped by its key's.
func capped(sp Space, k *key) string {
	if k.access == "read" {
		return "read"
	}
	return sp.Access
}

func (s *Site) spaces(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, k := s.liveKey(r)
	if k == nil {
		writeError(w, http.StatusUnauthorized, "invalid_token")
		return
	}
	list := []Space{}
	for _, sp := range k.spaces {
		if !s.suspended[sp.Name] {
			list = append(list, Space{Name: sp.Name, Owner: sp.Owner, Access: capped(sp, k)})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"spaces": list})
}

func (s *Site) mint(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Space  string `json:"space"`
		Access string `json:"access"`
	}
	if err := decodeStrict(r, &body, "space", "access"); err != nil || body.Space == "" || (body.Access != "" && body.Access != "write" && body.Access != "read") {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	s.mu.Lock()
	bearer, k := s.liveKey(r)
	if k == nil {
		s.mu.Unlock()
		writeError(w, http.StatusUnauthorized, "invalid_token")
		return
	}
	if rf := s.mintErr; rf != nil {
		s.mintErr = nil
		s.mu.Unlock()
		writeError(w, rf.status, rf.code)
		return
	}
	i := slices.IndexFunc(k.spaces, func(sp Space) bool { return sp.Name == body.Space })
	switch {
	case i < 0:
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "no_space")
		return
	case s.suspended[body.Space]:
		s.mu.Unlock()
		writeError(w, http.StatusForbidden, "space_suspended")
		return
	}
	sp := k.spaces[i]
	access := capped(sp, k)
	switch {
	case body.Access == "write" && access != "write":
		s.mu.Unlock()
		writeError(w, http.StatusForbidden, "access_denied")
		return
	case body.Access == "read":
		access = "read"
	}
	m := Minted{
		Key: bearer, Token: newToken(), Space: sp.Name, Access: access,
		ExpiresAt: time.Now().Add(s.lifetime).UTC(), Endpoint: k.endpoint,
	}
	k.children = append(k.children, m.Token)
	s.minted[m.Token] = true
	s.mints = append(s.mints, m)
	minter, broken := s.minter, s.mintBody
	s.mintBody = ""
	s.mu.Unlock()
	if minter != nil {
		minter(m)
	}
	if broken != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(broken))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": m.Token, "space": m.Space, "access": m.Access,
		"expiresAt": m.ExpiresAt.Format(time.RFC3339Nano), "endpoint": m.Endpoint, "owner": sp.Owner,
	})
}

// newToken is a fresh token in the site's format.
func newToken() string {
	id := make([]byte, 8)
	secret := make([]byte, 32)
	_, _ = rand.Read(id)
	_, _ = rand.Read(secret)
	return "sld_" + hex.EncodeToString(id) + "_" + base64.RawURLEncoding.EncodeToString(secret)
}

// withdraw revokes a key with its children, or one minted token, and
// reports whether the site knew it; the caller holds s.mu.
func (s *Site) withdraw(credential string) bool {
	if k, ok := s.keys[credential]; ok && k.endpoint != "" {
		delete(s.keys, credential)
		s.revoked = append(s.revoked, credential)
		for _, child := range k.children {
			if s.minted[child] {
				delete(s.minted, child)
				s.revoked = append(s.revoked, child)
			}
		}
		return true
	}
	if s.minted[credential] {
		delete(s.minted, credential)
		s.revoked = append(s.revoked, credential)
		return true
	}
	return false
}

func (s *Site) revoke(w http.ResponseWriter, r *http.Request) {
	credential, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	s.mu.Lock()
	defer s.mu.Unlock()
	if rf := s.revokeErr; rf != nil {
		s.revokeErr = nil
		writeError(w, rf.status, rf.code)
		return
	}
	if !ok || !s.withdraw(credential) {
		writeError(w, http.StatusUnauthorized, "invalid_token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// requestLimit is the site's bound on a request body.
const requestLimit = 4 << 10

// jsonOnly enforces what the real site enforces on its POST routes: a JSON
// content type and a body under requestLimit.
func jsonOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type")
			return
		}
		if r.ContentLength < 0 || r.ContentLength >= requestLimit {
			writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, requestLimit)
		next(w, r)
	}
}

// decodeStrict decodes a request body as the site does: one JSON object
// whose keys are among names, spelled exactly (encoding/json alone would
// match them ignoring case), none of them null, each of v's JSON type, and
// nothing after it. Anything else is an error, which each route answers
// with 400 invalid_request.
func decodeStrict(r *http.Request, v any, names ...string) error {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var fields map[string]json.RawMessage
	if err := dec.Decode(&fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("the body is not an object")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("data after the object")
	}
	for name, value := range fields {
		if !slices.Contains(names, name) {
			return fmt.Errorf("unknown field %q", name)
		}
		if string(value) == "null" {
			return fmt.Errorf("field %q is null", name)
		}
	}
	return json.Unmarshal(data, v)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code, "message": "sitetest: " + code})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
