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
	"strconv"
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

// StartBody is one recorded start request.
type StartBody struct {
	Space  string `json:"space"`
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
	hold      *barrier
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
	s := &Site{interval: 1, expiresIn: 600, userCode: "BCDF-GHJK", approvals: map[string]*approval{}, issued: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cli/v1/start", jsonOnly(s.start))
	mux.HandleFunc("POST /cli/v1/token", jsonOnly(s.token))
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
// whatever the token.
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.DeviceCode == "" {
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
	s.issued[script.Issue.Token] = true
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
		"token":     issue.Token,
		"space":     issue.Space,
		"access":    issue.Access,
		"expiresAt": expires,
		"endpoint":  issue.Endpoint,
		"account":   issue.Account,
		"owner":     issue.Owner,
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

func (s *Site) revoke(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	s.mu.Lock()
	defer s.mu.Unlock()
	if rf := s.revokeErr; rf != nil {
		s.revokeErr = nil
		writeError(w, rf.status, rf.code)
		return
	}
	if !ok || !s.issued[token] {
		writeError(w, http.StatusUnauthorized, "invalid_token")
		return
	}
	delete(s.issued, token)
	s.revoked = append(s.revoked, token)
	w.WriteHeader(http.StatusNoContent)
}

// requestLimit is the site's bound on a request body.
const requestLimit = 4 << 10

// jsonOnly enforces what the real site enforces on start and token: a JSON
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

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code, "message": "sitetest: " + code})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
