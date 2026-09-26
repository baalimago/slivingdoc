package httpstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/httpstore/gatewaytest"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/contract"
)

const (
	testSpace = "notes"
	testToken = "sld_0123456789abcdef_c2VjcmV0LXRva2VuLXZhbHVlLWZvci10ZXN0cy0wMDAwMA"
)

var prefixSeq atomic.Uint64

func noBackoff(int) time.Duration { return 0 }

// newGatewayStore starts a gateway with one read-write space and returns a
// store on a fresh notebook prefix inside it.
func newGatewayStore(t *testing.T) (*Store, *gatewaytest.Gateway) {
	t.Helper()
	g := gatewaytest.Start(t)
	g.AddSpace(testSpace, 1<<30)
	g.Grant(testToken, testSpace, false)
	return newStore(t, g.URL(), fmt.Sprintf("nb-%d", prefixSeq.Add(1))), g
}

func newStore(t *testing.T, endpoint, prefix string) *Store {
	t.Helper()
	s, err := New(Config{Endpoint: endpoint, Space: testSpace, Prefix: prefix, Token: testToken, Backoff: noBackoff})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestContractSuite(t *testing.T) {
	contract.Run(t, func(t *testing.T) storage.ObjectStore {
		s, _ := newGatewayStore(t)
		return s
	})
}

func TestContractSuiteWithoutPrefix(t *testing.T) {
	contract.Run(t, func(t *testing.T) storage.ObjectStore {
		g := gatewaytest.Start(t)
		g.AddSpace(testSpace, 1<<30)
		g.Grant(testToken, testSpace, false)
		return newStore(t, g.URL(), "")
	})
}

func TestCheckAccess(t *testing.T) {
	s, g := newGatewayStore(t)
	if err := s.CheckAccess(context.Background()); err != nil {
		t.Fatalf("CheckAccess: %v", err)
	}

	g.Grant("sld_other", "elsewhere", false)
	other, err := New(Config{Endpoint: g.URL(), Space: testSpace, Token: "sld_other", Backoff: noBackoff})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := other.CheckAccess(context.Background()); !errors.Is(err, storage.ErrAccessDenied) {
		t.Fatalf("CheckAccess for an ungranted space = %v, want ErrAccessDenied", err)
	}

	unknown, err := New(Config{Endpoint: g.URL(), Space: testSpace, Token: "sld_unknown", Backoff: noBackoff})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = unknown.CheckAccess(context.Background())
	if !errors.Is(err, storage.ErrAccessDenied) {
		t.Fatalf("CheckAccess with an unknown token = %v, want ErrAccessDenied", err)
	}
}

func TestCheckAccessRefusesUnknownServers(t *testing.T) {
	tests := []struct {
		name string
		body string
		code int
	}{
		{"other api", `{"api":"other","version":1,"conditionalWrites":true}`, http.StatusOK},
		{"newer version", `{"api":"slivingdoc-storage","version":2,"conditionalWrites":true}`, http.StatusOK},
		{"no conditional writes", `{"api":"slivingdoc-storage","version":1,"conditionalWrites":false}`, http.StatusOK},
		{"not json", `<html>hello</html>`, http.StatusOK},
		{"not found", `{"error":"not_found"}`, http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" {
					t.Errorf("GET /v1 carried a token")
				}
				w.WriteHeader(tt.code)
				_, _ = io.WriteString(w, tt.body)
			}))
			t.Cleanup(srv.Close)
			err := newStore(t, srv.URL, "").CheckAccess(context.Background())
			if !errors.Is(err, storage.ErrIncompatible) {
				t.Fatalf("CheckAccess = %v, want ErrIncompatible", err)
			}
		})
	}
}

func TestReadOnlyGrantReadsButCannotWrite(t *testing.T) {
	s, g := newGatewayStore(t)
	if _, err := s.CreateObject(context.Background(), storage.CurrentKey, []byte("v1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	g.Grant("sld_reader", testSpace, true)
	reader, err := New(Config{Endpoint: g.URL(), Space: testSpace, Prefix: s.prefix, Token: "sld_reader", Backoff: noBackoff})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := reader.CheckAccess(context.Background()); err != nil {
		t.Fatalf("read-only CheckAccess: %v", err)
	}
	rc, _, err := reader.ReadObject(context.Background(), storage.CurrentKey)
	if err != nil {
		t.Fatalf("read-only read: %v", err)
	}
	rc.Close()
	if _, err := reader.CreateObject(context.Background(), "probe/01973e12-8b34-7b01-9e2f-000000000001", []byte("x")); !errors.Is(err, storage.ErrAccessDenied) {
		t.Fatalf("read-only create = %v, want ErrAccessDenied", err)
	}
}

func TestQuotaRefusesPackUpload(t *testing.T) {
	s, g := newGatewayStore(t)
	g.SetQuota(testSpace, 10)
	data := []byte("more than ten bytes of pack")
	key, meta := packFixture(t, data, 1)
	err := s.PutObject(context.Background(), key.String(), bytes.NewReader(data), meta)
	if !errors.Is(err, storage.ErrQuotaExceeded) {
		t.Fatalf("put over quota = %v, want ErrQuotaExceeded", err)
	}
	if got := g.Stored(testSpace); got != 0 {
		t.Fatalf("stored bytes after refusal = %d, want 0", got)
	}
	if _, _, err := s.ReadObject(context.Background(), storage.CurrentKey); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("read while full = %v, want ErrNotFound (reads keep working)", err)
	}
}

func TestStatusMapping(t *testing.T) {
	tests := []struct {
		status int
		code   string
		reason string
		want   error
	}{
		{http.StatusInsufficientStorage, "quota_exceeded", "storage_full", storage.ErrQuotaExceeded},
		{http.StatusInsufficientStorage, "quota_exceeded", "", storage.ErrQuotaExceeded},
		{http.StatusInsufficientStorage, "quota_exceeded", "request_limit", storage.ErrRequestLimit},
		{http.StatusInsufficientStorage, "QUOTA_EXCEEDED", "REQUEST_LIMIT", storage.ErrRequestLimit},
		{http.StatusForbidden, "quota_exceeded", "storage_full", storage.ErrQuotaExceeded},
		{http.StatusTooManyRequests, "rate_limited", "slow_reads", storage.ErrRateLimited},
		{http.StatusTooManyRequests, "", "", storage.ErrRateLimited},
		{http.StatusUnauthorized, "unauthorized", "", storage.ErrAccessDenied},
		{http.StatusForbidden, "forbidden", "", storage.ErrAccessDenied},
		{http.StatusNotFound, "not_found", "", storage.ErrAccessDenied},
		{http.StatusRequestEntityTooLarge, "too_large", "", storage.ErrTooLarge},
		{http.StatusBadGateway, "backend_unavailable", "", storage.ErrTransport},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d %s %s", tt.status, tt.code, tt.reason), func(t *testing.T) {
			s, g := newGatewayStore(t)
			g.RefuseNextWithReason(http.MethodPut, tt.status, tt.code, tt.reason)
			data := []byte("pack")
			key, meta := packFixture(t, data, 1)
			err := s.PutObject(context.Background(), key.String(), bytes.NewReader(data), meta)
			if !errors.Is(err, tt.want) {
				t.Fatalf("put = %v, want %v", err, tt.want)
			}
			if strings.Contains(err.Error(), testToken) {
				t.Fatalf("error %q leaks the token", err)
			}
			var refusal *storage.Refusal
			if tt.reason != "" && (!errors.As(err, &refusal) || !strings.Contains(refusal.Message, tt.reason)) {
				t.Fatalf("put = %v, want a storage.Refusal carrying the server message", err)
			}
		})
	}
}

func TestClientErrorIsNotTransport(t *testing.T) {
	s, g := newGatewayStore(t)
	g.RefuseNext(http.MethodPut, http.StatusBadRequest, "invalid_key")
	_, err := s.CreateObject(context.Background(), storage.CurrentKey, []byte("v1"))
	if err == nil || errors.Is(err, storage.ErrTransport) {
		t.Fatalf("create after 400 = %v, want a definite non-transport error", err)
	}
	if !strings.Contains(err.Error(), "invalid_key") {
		t.Fatalf("create after 400 = %v, want the server code in the text", err)
	}
}

// The API answers If-Match on an absent object with 412, so a 404 on a
// replace means the space is gone or the grant was revoked.
func TestReplaceAnswered404IsAccessDenied(t *testing.T) {
	s, g := newGatewayStore(t)
	g.RefuseNext(http.MethodPut, http.StatusNotFound, "not_found")
	_, err := s.ReplaceObject(context.Background(), storage.CurrentKey, "etag", []byte("v2"))
	if !errors.Is(err, storage.ErrAccessDenied) {
		t.Fatalf("replace answered 404 = %v, want ErrAccessDenied", err)
	}
}

func TestReplaceMissingObjectIsPreconditionFailure(t *testing.T) {
	s, _ := newGatewayStore(t)
	_, err := s.ReplaceObject(context.Background(), storage.CurrentKey, "\"etag\"", []byte("v2"))
	if !errors.Is(err, storage.ErrPreconditionFailed) {
		t.Fatalf("replace of an absent object = %v, want ErrPreconditionFailed", err)
	}
}

// A redirect is never followed: a followed PUT becomes a body-less GET whose
// 2xx would look like a stored write, and the token would travel with it.
func TestRedirectIsARefusal(t *testing.T) {
	var followed atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			followed.Add(1)
			w.Header().Set("ETag", `"fake"`)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	s, err := New(Config{Endpoint: srv.URL, Space: testSpace, Token: testToken})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := s.CreateObject(ctx, storage.CurrentKey, []byte("v1")); err == nil {
		t.Fatal("create answered with a redirect succeeded")
	}
	data := []byte("pack")
	key, meta := packFixture(t, data, 1)
	if err := s.PutObject(ctx, key.String(), bytes.NewReader(data), meta); err == nil {
		t.Fatal("put answered with a redirect succeeded")
	}
	if n := followed.Load(); n != 0 {
		t.Fatalf("the client followed %d redirects", n)
	}
}

// A busy or failing server at startup is not an incompatible one.
func TestCheckAccessBusyServerIsNotIncompatible(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			s, g := newGatewayStore(t)
			for range 3 {
				g.RefuseNext(http.MethodGet, status, "busy")
			}
			err := s.CheckAccess(context.Background())
			if err == nil || errors.Is(err, storage.ErrIncompatible) {
				t.Fatalf("CheckAccess against a busy server = %v, want a non-incompatible error", err)
			}
		})
	}
}

func TestIdempotentRequestsRetryServerErrors(t *testing.T) {
	s, g := newGatewayStore(t)
	if _, err := s.CreateObject(context.Background(), storage.CurrentKey, []byte("v1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	g.RefuseNext(http.MethodGet, http.StatusBadGateway, "backend_unavailable")
	g.RefuseNext(http.MethodGet, http.StatusServiceUnavailable, "backend_unavailable")
	rc, _, err := s.ReadObject(context.Background(), storage.CurrentKey)
	if err != nil {
		t.Fatalf("read after two 5xx answers: %v", err)
	}
	rc.Close()

	for range 3 {
		g.RefuseNext(http.MethodGet, http.StatusBadGateway, "backend_unavailable")
	}
	if _, _, err := s.ReadObject(context.Background(), storage.CurrentKey); !errors.Is(err, storage.ErrTransport) {
		t.Fatalf("read after three 5xx answers = %v, want ErrTransport", err)
	}
}

func TestWritesAreNotRetried(t *testing.T) {
	s, g := newGatewayStore(t)
	g.RefuseNext(http.MethodPut, http.StatusBadGateway, "backend_unavailable")
	before := g.Requests()
	if _, err := s.CreateObject(context.Background(), storage.CurrentKey, []byte("v1")); !errors.Is(err, storage.ErrTransport) {
		t.Fatalf("create after 502 = %v, want ErrTransport", err)
	}
	if got := g.Requests() - before; got != 1 {
		t.Fatalf("create sent %d requests, want 1", got)
	}
}

func TestUnreachableServerIsTransport(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	s := newStore(t, url, "")
	if _, _, err := s.ReadObject(context.Background(), storage.CurrentKey); !errors.Is(err, storage.ErrTransport) {
		t.Fatalf("read from a closed server = %v, want ErrTransport", err)
	}
	if err := s.CheckAccess(context.Background()); !errors.Is(err, storage.ErrTransport) {
		t.Fatalf("CheckAccess on a closed server = %v, want ErrTransport", err)
	}
}

func TestCancelledContextStopsRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	s, err := New(Config{
		Endpoint: srv.URL, Space: testSpace, Token: testToken, Retries: 5,
		Backoff: func(int) time.Duration { return time.Hour },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err = s.ReadObject(ctx, storage.CurrentKey)
	if !errors.Is(err, storage.ErrTransport) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read with an expiring context = %v, want ErrTransport wrapping the deadline", err)
	}
}

func TestListFollowsCursors(t *testing.T) {
	s, g := newGatewayStore(t)
	g.SetPageSize(1)
	var want []string
	for gen := uint64(1); gen <= 3; gen++ {
		data := []byte("pack")
		key, meta := packFixture(t, data, gen)
		if err := s.PutObject(context.Background(), key.String(), bytes.NewReader(data), meta); err != nil {
			t.Fatalf("put: %v", err)
		}
		want = append(want, key.String())
	}
	var got []string
	if err := s.ListObjects(context.Background(), "packs/", func(key string) error {
		got = append(got, key)
		return nil
	}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if fmt.Sprint(got) != fmt.Sprint(slices.Sorted(slices.Values(want))) {
		t.Fatalf("list = %v, want %v", got, slices.Sorted(slices.Values(want)))
	}
	stop := errors.New("stop")
	if err := s.ListObjects(context.Background(), "packs/", func(string) error { return stop }); !errors.Is(err, stop) {
		t.Fatalf("list with a failing callback = %v, want the callback error", err)
	}
}

func TestDeleteBatches(t *testing.T) {
	var batches []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		batches = append(batches, strings.Count(string(body), ".pack"))
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	s := newStore(t, srv.URL, "nb")
	keys := make([]string, 2500)
	for i := range keys {
		keys[i] = fmt.Sprintf("packs/increments/%d-01973e12-8b34-7b01-9e2f-000000000001.pack", i)
	}
	if err := s.DeleteObjects(context.Background(), keys); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if fmt.Sprint(batches) != "[1000 1000 500]" {
		t.Fatalf("delete batches = %v, want [1000 1000 500]", batches)
	}
}

func TestMisbehavingServer(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		call    func(*Store) error
		want    error
	}{
		{
			name: "list key outside the prefix",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, `{"keys":["other/packs/x.pack"],"cursor":null}`)
			},
			call: func(s *Store) error {
				return s.ListObjects(context.Background(), "packs/", func(string) error { return nil })
			},
			want: storage.ErrIntegrity,
		},
		{
			name: "list cursor repeats",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, `{"keys":[],"cursor":"same"}`)
			},
			call: func(s *Store) error {
				return s.ListObjects(context.Background(), "packs/", func(string) error { return nil })
			},
			want: storage.ErrTransport,
		},
		{
			name: "list cursor cycles",
			handler: func(w http.ResponseWriter, r *http.Request) {
				next := "a"
				if r.URL.Query().Get("cursor") == "a" {
					next = "b"
				}
				_, _ = io.WriteString(w, `{"keys":[],"cursor":"`+next+`"}`)
			},
			call: func(s *Store) error {
				return s.ListObjects(context.Background(), "packs/", func(string) error { return nil })
			},
			want: storage.ErrTransport,
		},
		{
			name: "list answer is not json",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, `nope`)
			},
			call: func(s *Store) error {
				return s.ListObjects(context.Background(), "packs/", func(string) error { return nil })
			},
			want: storage.ErrTransport,
		},
		{
			name: "create answers without an etag",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			call: func(s *Store) error {
				_, err := s.CreateObject(context.Background(), storage.CurrentKey, []byte("v1"))
				return err
			},
			want: storage.ErrTransport,
		},
		{
			name: "read with malformed pack metadata",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set(storage.MetaSize, "big")
				_, _ = io.WriteString(w, "bytes")
			},
			call: func(s *Store) error {
				_, _, err := s.ReadObject(context.Background(), storage.CurrentKey)
				return err
			},
			want: storage.ErrIntegrity,
		},
		{
			name: "read not found",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			call: func(s *Store) error {
				_, _, err := s.ReadObject(context.Background(), storage.CurrentKey)
				return err
			},
			want: storage.ErrNotFound,
		},
		{
			name: "delete refused",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			},
			call: func(s *Store) error {
				return s.DeleteObjects(context.Background(), []string{"current"})
			},
			want: storage.ErrAccessDenied,
		},
		{
			name: "list refused",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			call: func(s *Store) error {
				return s.ListObjects(context.Background(), "packs/", func(string) error { return nil })
			},
			want: storage.ErrAccessDenied,
		},
		{
			name: "space check refused",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1" {
					_, _ = io.WriteString(w, `{"api":"slivingdoc-storage","version":1,"conditionalWrites":true}`)
					return
				}
				w.WriteHeader(http.StatusTooManyRequests)
			},
			call: func(s *Store) error { return s.CheckAccess(context.Background()) },
			want: storage.ErrRateLimited,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			t.Cleanup(srv.Close)
			if err := tt.call(newStore(t, srv.URL, "nb")); !errors.Is(err, tt.want) {
				t.Fatalf("call = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestRequestsCarryTokenAndUserAgent(t *testing.T) {
	var auth, agent, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, agent, path = r.Header.Get("Authorization"), r.Header.Get("User-Agent"), r.URL.EscapedPath()
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	s, err := New(Config{Endpoint: srv.URL + "/", Space: testSpace, Prefix: "team notes", Token: testToken, UserAgent: "slivingdoc/test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, _, _ = s.ReadObject(context.Background(), storage.CurrentKey)
	if auth != "Bearer "+testToken || agent != "slivingdoc/test" {
		t.Fatalf("headers = %q, %q; want the bearer token and user agent", auth, agent)
	}
	if path != "/v1/spaces/notes/objects/team%20notes/current" {
		t.Fatalf("path = %q, want the escaped prefix and key", path)
	}
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	good := Config{Endpoint: "https://api.example.test", Space: "notes", Token: "sld_x"}
	tests := []struct {
		name string
		edit func(*Config)
	}{
		{"relative endpoint", func(c *Config) { c.Endpoint = "api.example.test" }},
		{"ftp endpoint", func(c *Config) { c.Endpoint = "ftp://api.example.test" }},
		{"plain http to a remote host", func(c *Config) { c.Endpoint = "http://api.example.test" }},
		{"plain http to a loopback-looking name", func(c *Config) { c.Endpoint = "http://localhost.example.test" }},
		{"uppercase space", func(c *Config) { c.Space = "Notes" }},
		{"trailing hyphen space", func(c *Config) { c.Space = "notes-" }},
		{"long space", func(c *Config) { c.Space = strings.Repeat("a", 64) }},
		{"bad prefix", func(c *Config) { c.Prefix = "a/../b" }},
		{"empty token", func(c *Config) { c.Token = "" }},
		{"token with space", func(c *Config) { c.Token = "sld x" }},
		{"token with newline", func(c *Config) { c.Token = "sld\nx" }},
	}
	if _, err := New(good); err != nil {
		t.Fatalf("New(good) = %v", err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := good
			tt.edit(&cfg)
			if _, err := New(cfg); err == nil {
				t.Fatal("New succeeded, want an error")
			} else if strings.Contains(err.Error(), "sld_x") {
				t.Fatalf("error %q leaks the token", err)
			}
		})
	}
}

func packFixture(t *testing.T, data []byte, generation uint64) (storage.Key, storage.Metadata) {
	t.Helper()
	id, err := storage.NewUUIDv7()
	if err != nil {
		t.Fatalf("NewUUIDv7: %v", err)
	}
	return storage.Key{Kind: storage.KindIncrement, Generation: generation, ID: id}, storage.Metadata{
		SHA256:     storage.SHA256(sha256.Sum256(data)),
		Size:       uint64(len(data)),
		Kind:       storage.KindIncrement,
		Generation: generation,
	}
}
