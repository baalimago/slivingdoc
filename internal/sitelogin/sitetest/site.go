// Package sitetest is a test-only reference server of the site's CLI login
// routes (architecture/login.md): POST /cli/v1/start, /cli/v1/token and
// /cli/v1/revoke over a local httptest server, answering each approval
// with a scripted sequence instead of a person in a browser.
package sitetest

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Issue is the token an approved script returns.
type Issue struct {
	Token  string
	Space  string
	Access string
	// ExpiresAt is the token's expiry; the zero time answers null.
	ExpiresAt time.Time
	Endpoint  string
	// Account is the approver's email and Owner the space owner's.
	Account string
	Owner   string
}

// Script is how the site answers the polls of one approval: each code of
// Pending in turn (authorization_pending, slow_down), then Final when it
// is set (access_denied, expired_token), else the Issue. After an issue
// the device code is claimed and every later poll is expired_token.
type Script struct {
	Pending []string
	Final   string
	Issue   Issue
}

// StartBody is one recorded start request.
type StartBody struct {
	Space  string `json:"space"`
	Access string `json:"access"`
	Client string `json:"client"`
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
	interval  int
	expiresIn int
	approvals map[string]*approval
	issued    map[string]bool
	starts    []StartBody
	revoked   []string
	polls     int
}

// Start runs a site until the test ends. Approvals poll every second and
// live ten minutes unless SetTiming says otherwise.
func Start(t *testing.T) *Site {
	t.Helper()
	s := &Site{interval: 1, expiresIn: 600, approvals: map[string]*approval{}, issued: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cli/v1/start", s.start)
	mux.HandleFunc("POST /cli/v1/token", s.token)
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

// Revoked returns every token revoked so far, in order.
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

// Revoke withdraws token as if its owner revoked it on the Tokens page.
func (s *Site) Revoke(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.issued, token)
}

// Close stops the site, so every later request fails to connect.
func (s *Site) Close() { s.srv.Close() }

// Issued marks token as issued by this site, so a revoke of it succeeds;
// a script's issue does this itself.
func (s *Site) Issued(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.issued[token] = true
}

func (s *Site) start(w http.ResponseWriter, r *http.Request) {
	var body StartBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || (body.Access != "write" && body.Access != "read") {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	device := base64.RawURLEncoding.EncodeToString(raw)
	const userCode = "BCDF-GHJK"
	s.mu.Lock()
	s.starts = append(s.starts, body)
	s.approvals[device] = &approval{script: s.next}
	interval, expiresIn := s.interval, s.expiresIn
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"deviceCode":              device,
		"userCode":                userCode,
		"verificationUri":         s.srv.URL + "/cli/login",
		"verificationUriComplete": s.srv.URL + "/cli/login?code=" + userCode,
		"interval":                interval,
		"expiresIn":               expiresIn,
	})
}

func (s *Site) token(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DeviceCode string `json:"deviceCode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.DeviceCode == "" {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.polls++
	a, ok := s.approvals[body.DeviceCode]
	if !ok || a.claimed {
		writeError(w, http.StatusBadRequest, "expired_token")
		return
	}
	if a.polls < len(a.script.Pending) {
		code := a.script.Pending[a.polls]
		a.polls++
		writeError(w, http.StatusBadRequest, code)
		return
	}
	if a.script.Final != "" {
		writeError(w, http.StatusBadRequest, a.script.Final)
		return
	}
	a.claimed = true
	issue := a.script.Issue
	s.issued[issue.Token] = true
	var expires any
	if !issue.ExpiresAt.IsZero() {
		expires = issue.ExpiresAt.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":     issue.Token,
		"space":     issue.Space,
		"access":    issue.Access,
		"expiresAt": expires,
		"endpoint":  issue.Endpoint,
		"account":   issue.Account,
		"owner":     issue.Owner,
	})
}

func (s *Site) revoke(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	s.mu.Lock()
	defer s.mu.Unlock()
	if !ok || !s.issued[token] {
		writeError(w, http.StatusUnauthorized, "invalid_token")
		return
	}
	delete(s.issued, token)
	s.revoked = append(s.revoked, token)
	w.WriteHeader(http.StatusNoContent)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code, "message": "sitetest: " + code})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
