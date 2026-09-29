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

// TestRead404MapsByReason proves that only a 404 whose raw code is
// not_found and raw reason no_object reads as an absent object. Every other
// 404, including one without a reason (an older gateway) or with the tokens
// in another case, is ErrAccessDenied and never ErrNotFound, so a pull
// cannot mistake an unreachable space for an empty notebook.
func TestRead404MapsByReason(t *testing.T) {
	tests := []struct {
		body string
		want error
		text string
	}{
		{`{"error":"not_found","reason":"no_object","message":"no such object"}`, storage.ErrNotFound, "no such object"},
		{`{"error":"NOT_FOUND","reason":"NO_OBJECT"}`, storage.ErrAccessDenied, "without saying what is missing"},
		{`{"error":"not_found","reason":"no_space","message":"no such space for this token"}`, storage.ErrAccessDenied, "does not exist or the token was not granted it"},
		{`{"error":"not_found","reason":"no_endpoint","message":"no such endpoint"}`, storage.ErrAccessDenied, "check --endpoint"},
		{`{"error":"not_found"}`, storage.ErrAccessDenied, "without saying what is missing"},
		{`{"reason":"no_object"}`, storage.ErrAccessDenied, "without saying what is missing"},
		{`{"error":"not_found","reason":"gone"}`, storage.ErrAccessDenied, "without saying what is missing"},
		{`<html>not found</html>`, storage.ErrAccessDenied, "without saying what is missing"},
	}
	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, tt.body)
			}))
			t.Cleanup(srv.Close)
			_, _, err := newStore(t, srv.URL, "").ReadObject(context.Background(), storage.CurrentKey)
			if !errors.Is(err, tt.want) {
				t.Fatalf("read = %v, want %v", err, tt.want)
			}
			if tt.want != storage.ErrNotFound && errors.Is(err, storage.ErrNotFound) {
				t.Fatalf("read = %v, must not be ErrNotFound", err)
			}
			if !strings.Contains(err.Error(), tt.text) {
				t.Fatalf("read = %v, want it to say %q", err, tt.text)
			}
		})
	}
}

// get404Body fetches path with the test token and returns the 404 body.
func get404Body(t *testing.T, g *gatewaytest.Gateway, path string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, g.URL()+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET %s = %d %s, want 404", path, resp.StatusCode, body)
	}
	return string(body)
}

// Every way a space is out of reach answers the byte-identical no_space
// body, so space names cannot be probed; no_object comes only once the
// token and grant checks passed.
func TestGatewayNoSpaceIsOneBody(t *testing.T) {
	_, g := newGatewayStore(t)
	object := "/v1/spaces/%s/objects/nb/current"
	absent := get404Body(t, g, fmt.Sprintf(object, testSpace))
	if !strings.Contains(absent, `"reason":"no_object"`) {
		t.Fatalf("absent object body = %s, want no_object", absent)
	}
	g.AddSpace("other", 1<<20)
	bodies := map[string]string{
		"missing space":   get404Body(t, g, fmt.Sprintf(object, "missing")),
		"ungranted space": get404Body(t, g, fmt.Sprintf(object, "other")),
		"invalid name":    get404Body(t, g, fmt.Sprintf(object, "Not_A_Name")),
	}
	g.DeleteSpace(testSpace)
	bodies["deleted space"] = get404Body(t, g, fmt.Sprintf(object, testSpace))
	want := bodies["missing space"]
	if !strings.Contains(want, `"reason":"no_space"`) {
		t.Fatalf("missing space body = %s, want no_space", want)
	}
	for name, body := range bodies {
		if body != want {
			t.Fatalf("%s body = %s, want the byte-identical %s", name, body, want)
		}
	}
}

// The reference gateway answers every 404 with the real gateway's reason:
// a missing object no_object, a space the token cannot reach no_space
// (deleted, or the grant moved), and an unknown route no_endpoint.
func TestGateway404Reasons(t *testing.T) {
	s, g := newGatewayStore(t)
	if _, _, err := s.ReadObject(context.Background(), storage.CurrentKey); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("read of an absent object = %v, want ErrNotFound", err)
	}
	for _, path := range []string{"/v1/nope", "/v1/spaces/" + testSpace + "/nope"} {
		resp, err := http.Get(g.URL() + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(body), `"reason":"no_endpoint"`) {
			t.Fatalf("GET %s = %d %s, want 404 no_endpoint", path, resp.StatusCode, body)
		}
	}
	for _, tt := range []struct {
		method, path string
		status       int
		want         string
	}{
		{http.MethodGet, "/v1/spaces/Not_A_Name/usage", http.StatusNotFound, `"reason":"no_space"`},
		{http.MethodDelete, "/v1/spaces/" + testSpace + "/usage", http.StatusMethodNotAllowed, `"error":"method_not_allowed"`},
	} {
		req, err := http.NewRequest(tt.method, g.URL()+tt.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tt.status || !strings.Contains(string(body), tt.want) {
			t.Fatalf("%s %s = %d %s, want %d %s", tt.method, tt.path, resp.StatusCode, body, tt.status, tt.want)
		}
	}
	g.AddSpace("other", 1<<20)
	g.Grant(testToken, "other", false)
	if _, _, err := s.ReadObject(context.Background(), storage.CurrentKey); !errors.Is(err, storage.ErrAccessDenied) {
		t.Fatalf("read after the grant moved = %v, want ErrAccessDenied", err)
	}
	g.Grant(testToken, testSpace, false)
	g.DeleteSpace(testSpace)
	_, _, err := s.ReadObject(context.Background(), storage.CurrentKey)
	var refusal *storage.Refusal
	if !errors.Is(err, storage.ErrAccessDenied) || !errors.As(err, &refusal) || refusal.Message != "no such space for this token" {
		t.Fatalf("read of a deleted space = %v, want ErrAccessDenied carrying the gateway message", err)
	}
}

// TestGatewayBeforeNextObject proves the gateway's hook runs once, only
// for the matching object request and before serving it, so a space deleted
// in it answers that very request with 404 no_space.
func TestGatewayBeforeNextObject(t *testing.T) {
	s, g := newGatewayStore(t)
	ctx := context.Background()
	var runs atomic.Int32
	g.BeforeNextObject(http.MethodGet, storage.JoinKey(s.prefix, storage.CurrentKey), func() {
		runs.Add(1)
		g.DeleteSpace(testSpace)
	})
	if err := s.CheckAccess(ctx); err != nil || runs.Load() != 0 {
		t.Fatalf("CheckAccess = %v with %d hook runs, want nil and 0", err, runs.Load())
	}
	if _, _, err := s.ReadObject(ctx, storage.CurrentKey); !errors.Is(err, storage.ErrAccessDenied) || runs.Load() != 1 {
		t.Fatalf("read with the hook = %v after %d runs, want ErrAccessDenied after 1", err, runs.Load())
	}
	g.AddSpace(testSpace, 1<<20)
	if _, _, err := s.ReadObject(ctx, storage.CurrentKey); !errors.Is(err, storage.ErrNotFound) || runs.Load() != 1 {
		t.Fatalf("read after the hook = %v after %d runs, want ErrNotFound after 1", err, runs.Load())
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

// A limit is an answer, not a failure: 429 and 507 are never retried, so
// the next request is served normally.
func TestLimitAnswersAreNotRetried(t *testing.T) {
	for _, tt := range []struct {
		status int
		code   string
		want   error
	}{
		{http.StatusTooManyRequests, "rate_limited", storage.ErrRateLimited},
		{http.StatusInsufficientStorage, "quota_exceeded", storage.ErrQuotaExceeded},
	} {
		t.Run(tt.code, func(t *testing.T) {
			s, g := newGatewayStore(t)
			if _, err := s.CreateObject(context.Background(), storage.CurrentKey, []byte("v1")); err != nil {
				t.Fatalf("create: %v", err)
			}
			g.RefuseNext(http.MethodGet, tt.status, tt.code)
			before := g.Requests()
			if _, _, err := s.ReadObject(context.Background(), storage.CurrentKey); !errors.Is(err, tt.want) {
				t.Fatalf("read answered %d = %v, want %v", tt.status, err, tt.want)
			}
			if n := g.Requests() - before; n != 1 {
				t.Fatalf("read answered %d made %d requests, want 1", tt.status, n)
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
			// An older gateway's 404 without a reason fails closed.
			name: "read not found without a reason",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			call: func(s *Store) error {
				_, _, err := s.ReadObject(context.Background(), storage.CurrentKey)
				return err
			},
			want: storage.ErrAccessDenied,
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

// A 404 of the startup usage check is ErrAccessDenied whatever its reason,
// and names the fix by reason: no_space the space and grant, anything else
// --endpoint. The gateway's message travels as the refusal's.
func TestCheckAccessUsage404ByReason(t *testing.T) {
	tests := []struct {
		body string
		text string
	}{
		{`{"error":"not_found","reason":"no_space","message":"no such space for this token"}`, "does not exist or the token was not granted it"},
		{`{"error":"not_found","reason":"no_endpoint","message":"no such endpoint"}`, "no such space API; check --endpoint"},
		{`{"error":"not_found","reason":"no_object","message":"no such object"}`, "no such space API; check --endpoint"},
		{``, "no such space API; check --endpoint"},
	}
	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1" {
					_, _ = io.WriteString(w, `{"api":"slivingdoc-storage","version":1,"conditionalWrites":true}`)
					return
				}
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, tt.body)
			}))
			t.Cleanup(srv.Close)
			err := newStore(t, srv.URL, "").CheckAccess(context.Background())
			if !errors.Is(err, storage.ErrAccessDenied) || errors.Is(err, storage.ErrIncompatible) || !strings.Contains(err.Error(), tt.text) {
				t.Fatalf("CheckAccess = %v, want ErrAccessDenied saying %q", err, tt.text)
			}
			var refusal *storage.Refusal
			if tt.body != "" && (!errors.As(err, &refusal) || refusal.Message == "") {
				t.Fatalf("CheckAccess = %v, want the gateway message on the refusal", err)
			}
		})
	}
}

// The reference gateway serves its description at /v1 and /v1/, and, like
// the real one, refuses a list prefix outside packs/ as invalid_key before
// it looks at the token.
func TestGatewayDescriptionAndListPrefix(t *testing.T) {
	_, g := newGatewayStore(t)
	for _, tt := range []struct {
		path   string
		status int
		want   string
	}{
		{"/v1", http.StatusOK, `"api":"slivingdoc-storage"`},
		{"/v1/", http.StatusOK, `"api":"slivingdoc-storage"`},
		{"/v1/spaces/" + testSpace + "/objects?prefix=nb/current", http.StatusBadRequest, `"error":"invalid_key"`},
	} {
		resp, err := http.Get(g.URL() + tt.path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tt.status || !strings.Contains(string(body), tt.want) {
			t.Fatalf("GET %s without a token = %d %s, want %d %s", tt.path, resp.StatusCode, body, tt.status, tt.want)
		}
	}
}

func describeConfig(endpoint, token string) Config {
	return Config{Endpoint: endpoint, Token: token, Backoff: noBackoff}
}

func TestDescribeTokenNamesTheTokensSpace(t *testing.T) {
	g := gatewaytest.Start(t)
	g.AddSpace(testSpace, 1<<20)
	g.Grant(testToken, testSpace, false)
	g.Grant("sld_reader", testSpace, true)
	for _, row := range []struct {
		token string
		want  Access
	}{{testToken, AccessWrite}, {"sld_reader", AccessRead}} {
		info, err := DescribeToken(context.Background(), describeConfig(g.URL(), row.token))
		if err != nil {
			t.Fatalf("DescribeToken(%s) = %v", row.token, err)
		}
		if info != (TokenInfo{Space: testSpace, SpaceID: testSpace, Access: row.want}) {
			t.Fatalf("DescribeToken(%s) = %+v, want space %q with its id, %s access and no expiry", row.token, info, testSpace, row.want)
		}
	}
}

func TestDescribeTokenRefusals(t *testing.T) {
	g := gatewaytest.Start(t)
	g.AddSpace(testSpace, 1<<20)
	g.Grant(testToken, testSpace, false)
	g.Grant("sld_orphan", "gone", false)

	if _, err := DescribeToken(context.Background(), describeConfig(g.URL(), "sld_unknown")); !errors.Is(err, storage.ErrAccessDenied) {
		t.Fatalf("DescribeToken(unknown token) = %v, want ErrAccessDenied", err)
	}
	_, err := DescribeToken(context.Background(), describeConfig(g.URL(), "sld_orphan"))
	if !errors.Is(err, storage.ErrAccessDenied) || !strings.Contains(err.Error(), "reaches no space") {
		t.Fatalf("DescribeToken(token without a space) = %v, want ErrAccessDenied naming the missing space", err)
	}
	g.DisableTokenLookup()
	if _, err := DescribeToken(context.Background(), describeConfig(g.URL(), testToken)); !errors.Is(err, ErrTokenLookupUnsupported) {
		t.Fatalf("DescribeToken(older server) = %v, want ErrTokenLookupUnsupported", err)
	}
}

func TestDescribeTokenRefusesInvalidConfig(t *testing.T) {
	if _, err := DescribeToken(context.Background(), describeConfig("http://api.example.test", testToken)); err == nil {
		t.Fatal("DescribeToken over plain http to a remote host = nil, want a refusal")
	}
	if _, err := DescribeToken(context.Background(), describeConfig("https://api.example.test", "bad token")); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("DescribeToken(token with white space) = %v, want ErrInvalidToken", err)
	}
}

func TestDescribeTokenAnswers(t *testing.T) {
	for _, row := range []struct {
		name   string
		status int
		body   string
		want   error
		info   TokenInfo
	}{
		{
			name: "expiry", status: 200, body: `{"space":"notes","access":"read","expiresAt":"2026-12-26T12:00:00.000Z"}`,
			info: TokenInfo{Space: "notes", Access: AccessRead, ExpiresAt: time.Date(2026, 12, 26, 12, 0, 0, 0, time.UTC)},
		},
		{
			name: "space id", status: 200, body: `{"space":"notes","spaceId":"4f1c2a9e-0b7d-4e3a-9c51-2d8e6f0a1b3c","access":"write","expiresAt":null}`,
			info: TokenInfo{Space: "notes", SpaceID: "4f1c2a9e-0b7d-4e3a-9c51-2d8e6f0a1b3c", Access: AccessWrite},
		},
		{name: "invalid space id", status: 200, body: `{"space":"notes","spaceId":"../x","access":"read","expiresAt":null}`, want: storage.ErrIncompatible},
		{name: "long space id", status: 200, body: `{"space":"notes","spaceId":"` + strings.Repeat("a", 65) + `","access":"read","expiresAt":null}`, want: storage.ErrIncompatible},
		{name: "invalid space", status: 200, body: `{"space":"../x","access":"read","expiresAt":null}`, want: storage.ErrIncompatible},
		{name: "unknown access", status: 200, body: `{"space":"notes","access":"admin","expiresAt":null}`, want: storage.ErrIncompatible},
		{name: "bad expiry", status: 200, body: `{"space":"notes","access":"read","expiresAt":"soon"}`, want: storage.ErrIncompatible},
		{name: "not json", status: 200, body: `<html>`, want: storage.ErrIncompatible},
		{name: "bare 404", status: 404, body: ``, want: ErrTokenLookupUnsupported},
		{name: "405", status: 405, body: `{"error":"method_not_allowed"}`, want: ErrTokenLookupUnsupported},
		{name: "suspended owner", status: 403, body: `{"error":"forbidden","message":"ask the space's owner"}`, want: storage.ErrAccessDenied},
		{name: "server down", status: 503, body: ``, want: storage.ErrTransport},
		{name: "no space", status: 404, body: `{"error":"not_found","reason":"no_space"}`, want: storage.ErrAccessDenied},
		{name: "other 404 reason", status: 404, body: `{"error":"not_found","reason":"no_object"}`, want: ErrTokenLookupUnsupported},
		{name: "throttled", status: 429, body: `{"error":"rate_limited","reason":"slow_reads"}`, want: storage.ErrRateLimited},
		{name: "redirect", status: 302, body: ``, want: nil, info: TokenInfo{}},
	} {
		t.Run(row.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1" {
					_, _ = io.WriteString(w, `{"api":"slivingdoc-storage","version":1,"conditionalWrites":true}`)
					return
				}
				if r.URL.Path != "/v1/token" || r.Header.Get("Authorization") != "Bearer "+testToken {
					t.Errorf("request %s %s without the token", r.Method, r.URL.Path)
				}
				if row.status == http.StatusFound {
					w.Header().Set("Location", "https://elsewhere.example.test/v1/token")
				}
				w.WriteHeader(row.status)
				_, _ = io.WriteString(w, row.body)
			}))
			t.Cleanup(srv.Close)
			info, err := DescribeToken(context.Background(), describeConfig(srv.URL, testToken))
			if row.status == http.StatusFound {
				if err == nil || errors.Is(err, ErrTokenLookupUnsupported) || strings.Contains(err.Error(), testToken) {
					t.Fatalf("DescribeToken(redirect) = %+v, %v; want a refusal that is not followed", info, err)
				}
				return
			}
			if row.want != nil {
				if !errors.Is(err, row.want) {
					t.Fatalf("DescribeToken = %+v, %v; want %v", info, err, row.want)
				}
				if strings.Contains(err.Error(), testToken) {
					t.Fatalf("DescribeToken error %q leaks the token", err)
				}
				return
			}
			if err != nil || info != row.info {
				t.Fatalf("DescribeToken = %+v, %v; want %+v", info, err, row.info)
			}
		})
	}
}

// TestDescribeTokenChecksTheServerFirst proves the token only travels to an
// endpoint that first answered, without the token, as this storage API.
func TestDescribeTokenChecksTheServerFirst(t *testing.T) {
	for _, row := range []struct {
		name string
		v1   func(w http.ResponseWriter)
	}{
		{"not found", func(w http.ResponseWriter) { w.WriteHeader(http.StatusNotFound) }},
		{"another API", func(w http.ResponseWriter) {
			_, _ = io.WriteString(w, `{"api":"other","version":1,"conditionalWrites":true}`)
		}},
	} {
		t.Run(row.name, func(t *testing.T) {
			var sawToken atomic.Bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" {
					sawToken.Store(true)
				}
				if r.URL.Path == "/v1" {
					row.v1(w)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			t.Cleanup(srv.Close)
			_, err := DescribeToken(context.Background(), describeConfig(srv.URL, testToken))
			if !errors.Is(err, storage.ErrIncompatible) {
				t.Fatalf("DescribeToken against %s = %v, want ErrIncompatible", row.name, err)
			}
			if sawToken.Load() {
				t.Fatal("DescribeToken sent the token to a server that did not identify as the storage API")
			}
		})
	}
}
