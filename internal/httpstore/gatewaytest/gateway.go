// Package gatewaytest runs an in-process reference server of the hosted
// storage API, version 1, for tests. It follows the contract document of
// the hosted gateway: routing, key grammar and list prefix before authentication,
// bearer tokens granted per space, read-only grants, pack quota,
// conditional small-object writes, cursor listing, and batched deletes.
// Every 404 is not_found with the gateway's reason: no_endpoint for an
// unknown route, one byte-identical no_space body for any space out of
// reach, and no_object, after the token and grant checks, for an absent
// object; any other backend failure is 502 backend_unavailable. Each space is backed by the
// deterministic in-memory store.
package gatewaytest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
)

// The advertised size limits (API version 1).
const (
	MaxPackBytes  = 99614720
	MaxSmallBytes = 1 << 20
)

var (
	keyRE   = regexp.MustCompile(`^(?:(.+)/)?(current|probe/[0-9a-f-]{36}|packs/(?:checkpoints|increments)/\d+-[0-9a-f-]{36}\.pack)$`)
	routeRE = regexp.MustCompile(`^/v1/spaces/([^/]+)/(.+)$`)
	spaceRE = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

// Gateway is one running reference server.
type Gateway struct {
	srv *httptest.Server

	mu       sync.Mutex
	spaces   map[string]*space
	grants   map[string]grant
	refusals []refusal
	pageSize int
	requests int
	used     map[string]int
}

type space struct {
	store  *fake.Store
	quota  int64
	stored map[string]int64
}

type grant struct {
	space    string
	readOnly bool
}

type refusal struct {
	method string
	status int
	code   string
	reason string
}

// Start runs a gateway for the test and stops it at cleanup.
func Start(t testing.TB) *Gateway {
	t.Helper()
	g := &Gateway{spaces: map[string]*space{}, grants: map[string]grant{}, used: map[string]int{}, pageSize: 1000}
	g.srv = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.srv.Close)
	return g
}

// URL is the server base URL, without /v1.
func (g *Gateway) URL() string { return g.srv.URL }

// AddSpace creates an empty space with the given quota in bytes.
func (g *Gateway) AddSpace(name string, quota int64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.spaces[name] = &space{store: fake.New(""), quota: quota, stored: map[string]int64{}}
}

// Grant gives token access to space, read-only or read-write.
func (g *Gateway) Grant(token, space string, readOnly bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.grants[token] = grant{space: space, readOnly: readOnly}
}

// DeleteSpace removes a space and everything it holds; its grants stay
// and now answer 404 no_space, as for a space deleted on the gateway.
func (g *Gateway) DeleteSpace(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.spaces, name)
}

// SetQuota changes a space's quota in bytes.
func (g *Gateway) SetQuota(name string, quota int64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.spaces[name].quota = quota
}

// SetPageSize bounds the keys of one list page.
func (g *Gateway) SetPageSize(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pageSize = n
}

// RefuseNext answers the next request with method (any method when empty)
// with the given status and error code instead of serving it.
func (g *Gateway) RefuseNext(method string, status int, code string) {
	g.RefuseNextWithReason(method, status, code, "")
}

// RefuseNextWithReason is RefuseNext with the error body's reason field,
// which quota_exceeded and rate_limited refusals carry.
func (g *Gateway) RefuseNextWithReason(method string, status int, code, reason string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.refusals = append(g.refusals, refusal{method: method, status: status, code: code, reason: reason})
}

// Stored returns the pack bytes a space holds.
func (g *Gateway) Stored(name string) int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	var total int64
	for _, n := range g.spaces[name].stored {
		total += n
	}
	return total
}

// Requests counts the requests served, refusals included.
func (g *Gateway) Requests() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.requests
}

// Used counts the requests that presented token as their bearer
// credential, whether or not the gateway knows it.
func (g *Gateway) Used(token string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.used[token]
}

func (g *Gateway) serve(w http.ResponseWriter, r *http.Request) {
	if rf, ok := g.nextRefusal(r.Method); ok {
		writeReasonError(w, rf.status, rf.code, rf.reason)
		return
	}
	if (r.URL.Path == "/v1" || r.URL.Path == "/v1/") && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{
			"api": "slivingdoc-storage", "version": 1,
			"maxPackBytes": MaxPackBytes, "maxSmallBytes": MaxSmallBytes, "conditionalWrites": true,
		})
		return
	}
	m := routeRE.FindStringSubmatch(r.URL.Path)
	if m == nil {
		notFound(w, reasonNoEndpoint)
		return
	}
	name, rest := m[1], "/"+m[2]
	switch operation(r.Method, rest) {
	case routeUnknown:
		notFound(w, reasonNoEndpoint)
		return
	case routeWrongMethod:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if !spaceRE.MatchString(name) {
		notFound(w, reasonNoSpace)
		return
	}
	key, isObject := strings.CutPrefix(rest, "/objects/")
	if isObject && !validKey(key) {
		writeError(w, http.StatusBadRequest, "invalid_key")
		return
	}
	if rest == "/objects" && !validListPrefix(r.URL.Query().Get("prefix")) {
		writeError(w, http.StatusBadRequest, "invalid_key")
		return
	}
	sp, gr, ok := g.authorize(w, r, name)
	if !ok {
		return
	}
	write := r.Method != http.MethodGet
	if write && gr.readOnly {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	switch {
	case rest == "/usage" && r.Method == http.MethodGet:
		g.usage(w, sp)
	case rest == "/objects" && r.Method == http.MethodGet:
		g.list(w, r, sp)
	case rest == "/delete" && r.Method == http.MethodPost:
		g.delete(w, r, sp)
	case isObject && r.Method == http.MethodGet:
		read(w, r, sp, key)
	case isObject && r.Method == http.MethodPut && strings.Contains(key, "packs/"):
		g.putPack(w, r, sp, key)
	case isObject && r.Method == http.MethodPut:
		putSmall(w, r, sp, key)
	default:
		writeError(w, http.StatusBadRequest, "bad_request")
	}
}

func (g *Gateway) nextRefusal(method string) (refusal, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.requests++
	for i, rf := range g.refusals {
		if rf.method == "" || rf.method == method {
			g.refusals = append(g.refusals[:i], g.refusals[i+1:]...)
			return rf, true
		}
	}
	return refusal{}, false
}

// routeMatch classifies a request against the gateway's operation table.
type routeMatch int

const (
	routeOK routeMatch = iota
	routeWrongMethod
	routeUnknown
)

// operation mirrors the gateway's operation table: objects/<key> takes GET
// and PUT, objects and usage GET, delete POST; anything else is unknown.
func operation(method, rest string) routeMatch {
	allowed := map[string]bool{}
	switch {
	case strings.HasPrefix(rest, "/objects/"):
		allowed[http.MethodGet], allowed[http.MethodPut] = true, true
	case rest == "/objects", rest == "/usage":
		allowed[http.MethodGet] = true
	case rest == "/delete":
		allowed[http.MethodPost] = true
	default:
		return routeUnknown
	}
	if !allowed[method] {
		return routeWrongMethod
	}
	return routeOK
}

// authorize resolves the bearer token's grant for the named space and
// answers the refusal itself: 401 for an unknown token, 404 no_space for a
// space that does not exist or that the token was not granted.
func (g *Gateway) authorize(w http.ResponseWriter, r *http.Request, name string) (*space, grant, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	g.mu.Lock()
	if ok {
		g.used[token]++
	}
	gr, known := g.grants[token]
	sp, exists := g.spaces[name]
	g.mu.Unlock()
	if !ok || !known {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return nil, grant{}, false
	}
	if !exists || gr.space != name {
		notFound(w, reasonNoSpace)
		return nil, grant{}, false
	}
	return sp, gr, true
}

// validListPrefix accepts [<notebook prefix>/]packs/..., the only
// listable namespace; serve checks it before the token.
func validListPrefix(prefix string) bool {
	return strings.HasPrefix(prefix, "packs/") || strings.Contains(prefix, "/packs/")
}

func validKey(key string) bool {
	m := keyRE.FindStringSubmatch(key)
	if m == nil {
		return false
	}
	return storage.ValidatePrefix(m[1]) == nil
}

func (g *Gateway) usage(w http.ResponseWriter, sp *space) {
	g.mu.Lock()
	var stored int64
	for _, n := range sp.stored {
		stored += n
	}
	quota := sp.quota
	g.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]int64{"quotaBytes": quota, "storedBytes": stored, "reservedBytes": 0})
}

func read(w http.ResponseWriter, r *http.Request, sp *space, key string) {
	rc, info, err := sp.store.ReadObject(r.Context(), key)
	if errors.Is(err, storage.ErrNotFound) {
		notFound(w, reasonNoObject)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "backend_unavailable")
		return
	}
	defer rc.Close()
	w.Header().Set("ETag", string(info.ETag))
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	if info.Meta.Kind != "" {
		for name, v := range info.Meta.Fields() {
			w.Header().Set(name, v)
		}
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

func (g *Gateway) putPack(w http.ResponseWriter, r *http.Request, sp *space, key string) {
	if r.Header.Get("If-Match") != "" || r.Header.Get("If-None-Match") != "" {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if r.ContentLength < 0 {
		writeError(w, http.StatusLengthRequired, "length_required")
		return
	}
	if r.ContentLength > MaxPackBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large")
		return
	}
	fields := map[string]string{}
	for _, name := range []string{storage.MetaSHA256, storage.MetaSize, storage.MetaKind, storage.MetaGeneration} {
		fields[name] = r.Header.Get(name)
	}
	meta, err := storage.ParseMetadata(fields)
	if err != nil || meta.Size != uint64(r.ContentLength) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	g.mu.Lock()
	var stored int64
	for _, n := range sp.stored {
		stored += n
	}
	// As the hosted gateway does, a checkpoint pack may take the space
	// up to twice its quota, so a full space can compact itself.
	limit := sp.quota
	if meta.Kind == storage.KindCheckpoint {
		limit = 2 * sp.quota
	}
	full := stored+r.ContentLength > limit
	g.mu.Unlock()
	if full {
		writeReasonError(w, http.StatusInsufficientStorage, "quota_exceeded", "storage_full")
		return
	}
	data, err := io.ReadAll(r.Body)
	if err != nil || int64(len(data)) != r.ContentLength {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if err := sp.store.PutObject(context.Background(), key, bytes.NewReader(data), meta); err != nil {
		writeError(w, http.StatusBadGateway, "backend_unavailable")
		return
	}
	g.mu.Lock()
	sp.stored[key] = int64(len(data))
	g.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func putSmall(w http.ResponseWriter, r *http.Request, sp *space, key string) {
	ifMatch, ifNone := r.Header.Get("If-Match"), r.Header.Get("If-None-Match")
	if (ifMatch == "") == (ifNone == "") || (ifNone != "" && ifNone != "*") {
		writeError(w, http.StatusPreconditionRequired, "precondition_required")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, MaxSmallBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if len(data) > MaxSmallBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large")
		return
	}
	var etag storage.ETag
	if ifNone != "" {
		etag, err = sp.store.CreateObject(r.Context(), key, data)
	} else {
		etag, err = sp.store.ReplaceObject(r.Context(), key, storage.ETag(ifMatch), data)
	}
	switch {
	case errors.Is(err, storage.ErrPreconditionFailed):
		writeError(w, http.StatusPreconditionFailed, "precondition_failed")
	case err != nil:
		writeError(w, http.StatusBadGateway, "backend_unavailable")
	default:
		w.Header().Set("ETag", string(etag))
		writeJSON(w, http.StatusOK, map[string]string{"etag": string(etag)})
	}
}

func (g *Gateway) list(w http.ResponseWriter, r *http.Request, sp *space) {
	prefix := r.URL.Query().Get("prefix")
	var keys []string
	if err := sp.store.ListObjects(r.Context(), prefix, func(key string) error {
		keys = append(keys, key)
		return nil
	}); err != nil {
		writeError(w, http.StatusBadGateway, "backend_unavailable")
		return
	}
	sort.Strings(keys)
	start := 0
	if c := r.URL.Query().Get("cursor"); c != "" {
		n, err := strconv.Atoi(c)
		if err != nil || n < 0 || n > len(keys) {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		start = n
	}
	g.mu.Lock()
	end := min(start+g.pageSize, len(keys))
	g.mu.Unlock()
	var cursor any
	if end < len(keys) {
		cursor = strconv.Itoa(end)
	}
	page := keys[start:end]
	if page == nil {
		page = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": page, "cursor": cursor})
}

func (g *Gateway) delete(w http.ResponseWriter, r *http.Request, sp *space) {
	var body struct {
		Keys []string `json:"keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Keys) == 0 || len(body.Keys) > 1000 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	for _, key := range body.Keys {
		if !validKey(key) {
			writeError(w, http.StatusBadRequest, "invalid_key")
			return
		}
	}
	if err := sp.store.DeleteObjects(r.Context(), body.Keys); err != nil {
		writeError(w, http.StatusBadGateway, "backend_unavailable")
		return
	}
	g.mu.Lock()
	for _, key := range body.Keys {
		delete(sp.stored, key)
	}
	g.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// The reasons of a 404 not_found, and the gateway's message for each. Only
// reasonNoObject means an absent object.
const (
	reasonNoObject   = "no_object"
	reasonNoSpace    = "no_space"
	reasonNoEndpoint = "no_endpoint"
)

var notFoundMessages = map[string]string{
	reasonNoObject:   "no such object",
	reasonNoSpace:    "no such space for this token",
	reasonNoEndpoint: "no such endpoint",
}

// notFound answers 404 not_found with reason and the gateway's message.
func notFound(w http.ResponseWriter, reason string) {
	writeJSON(w, http.StatusNotFound, map[string]any{
		"error": "not_found", "message": notFoundMessages[reason], "retryable": false, "reason": reason,
	})
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeReasonError(w, status, code, "")
}

func writeReasonError(w http.ResponseWriter, status int, code, reason string) {
	retryable := status == http.StatusTooManyRequests || status >= 500 && status != http.StatusInsufficientStorage
	body := map[string]any{"error": code, "message": fmt.Sprintf("refused: %s", code), "retryable": retryable}
	if reason != "" {
		body["reason"] = reason
		body["message"] = fmt.Sprintf("refused: %s (%s)", code, reason)
	}
	writeJSON(w, status, body)
}
