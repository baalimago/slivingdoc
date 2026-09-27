package httpstore

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/baalimago/slivingdoc/internal/httpstore/gatewaytest"
	"github.com/baalimago/slivingdoc/internal/storage"
)

// rotating is a TokenSource that moves to its next token once the current
// one is rejected, and fails once it has none left. renews makes it a
// source of short-lived tokens (RenewingSource).
type rotating struct {
	mu       sync.Mutex
	tokens   []string
	rejected []string
	renews   bool
}

func (r *rotating) Renews() bool { return r.renews }

var errNoMoreTokens = errors.New("no more tokens")

func (r *rotating) Token(context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.tokens) == 0 {
		return "", errNoMoreTokens
	}
	return r.tokens[0], nil
}

func (r *rotating) Rejected(token string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rejected = append(r.rejected, token)
	if len(r.tokens) > 0 && r.tokens[0] == token {
		r.tokens = r.tokens[1:]
	}
}

const secondToken = "sld_fedcba9876543210_c2Vjb25kLXRva2VuLXZhbHVlLWZvci10ZXN0cy0wMDAwMA"

func rotatingStore(t *testing.T, tokens ...string) (*Store, *gatewaytest.Gateway, *rotating) {
	t.Helper()
	g := gatewaytest.Start(t)
	g.AddSpace(testSpace, 1<<30)
	g.Grant(testToken, testSpace, false)
	g.Grant(secondToken, testSpace, false)
	src := &rotating{tokens: tokens}
	s, err := New(Config{Endpoint: g.URL(), Space: testSpace, Tokens: src, Backoff: noBackoff})
	if err != nil {
		t.Fatalf("New() with a token source = %v", err)
	}
	return s, g, src
}

func TestRefusedTokenIsReplacedOnce(t *testing.T) {
	s, g, src := rotatingStore(t, testToken, secondToken)
	if err := s.CheckAccess(context.Background()); err != nil {
		t.Fatalf("CheckAccess() = %v", err)
	}
	g.Revoke(testToken)
	before := g.Requests()
	if _, err := s.CreateObject(context.Background(), storage.CurrentKey, []byte("v1")); err != nil {
		t.Fatalf("CreateObject() after the token was revoked = %v, want it sent again with the next token", err)
	}
	if got := g.Requests() - before; got != 2 || len(src.rejected) != 1 || src.rejected[0] != testToken {
		t.Fatalf("sent %d requests and rejected %v; want one refusal and one retry", got, src.rejected)
	}
	if g.Used()[secondToken] == 0 {
		t.Fatal("the next token never reached the gateway")
	}
	rc, _, err := s.ReadObject(context.Background(), storage.CurrentKey)
	if err != nil {
		t.Fatalf("ReadObject() = %v", err)
	}
	rc.Close()
}

// TestStreamedUploadIsNotReplayed proves a streamed upload refused with 401
// is sent once: for a source of short-lived tokens it fails with
// ErrCredentialRenewed, which a retry resolves, and otherwise with the
// access refusal; either way the next upload carries the next token.
func TestStreamedUploadIsNotReplayed(t *testing.T) {
	for _, tt := range []struct {
		name   string
		renews bool
		want   error
		not    error
	}{
		{"renewing source", true, storage.ErrCredentialRenewed, storage.ErrAccessDenied},
		{"fixed source", false, storage.ErrAccessDenied, storage.ErrCredentialRenewed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, g, src := rotatingStore(t, testToken, secondToken)
			src.renews = tt.renews
			g.Revoke(testToken)
			data := []byte("pack bytes")
			key, meta := packFixture(t, data, 1)
			before := g.Requests()
			err := s.PutObject(context.Background(), key.String(), bytes.NewReader(data), meta)
			if !errors.Is(err, tt.want) || errors.Is(err, tt.not) || errors.Is(err, storage.ErrTransport) ||
				g.Requests()-before != 1 || len(src.rejected) != 1 {
				t.Fatalf("PutObject() with a revoked token = %v after %d requests, want one %v", err, g.Requests()-before, tt.want)
			}
			if err := s.PutObject(context.Background(), key.String(), bytes.NewReader(data), meta); err != nil {
				t.Fatalf("the next PutObject() = %v, want the next token", err)
			}
		})
	}
}

func TestRefusedTokenWithoutAnother(t *testing.T) {
	s, g, _ := rotatingStore(t, testToken)
	g.Revoke(testToken)
	if _, err := s.CreateObject(context.Background(), storage.CurrentKey, []byte("v1")); !errors.Is(err, errNoMoreTokens) {
		t.Fatalf("CreateObject() = %v, want the source's failure", err)
	}
	if _, err := s.CreateObject(context.Background(), storage.CurrentKey, []byte("v1")); !errors.Is(err, errNoMoreTokens) {
		t.Fatalf("CreateObject() without a token = %v, want the source's failure before sending", err)
	}
}

func TestStaticTokenIsNotRetried(t *testing.T) {
	s, g := newGatewayStore(t)
	g.Revoke(testToken)
	before := g.Requests()
	if _, err := s.CreateObject(context.Background(), storage.CurrentKey, []byte("v1")); !errors.Is(err, storage.ErrAccessDenied) {
		t.Fatalf("CreateObject() with a revoked static token = %v, want ErrAccessDenied", err)
	}
	if got := g.Requests() - before; got != 1 {
		t.Fatalf("sent %d requests, want 1: a fixed token has no other to try", got)
	}
}

func TestDescribeTokenTakesATokenSource(t *testing.T) {
	g := gatewaytest.Start(t)
	g.AddSpace(testSpace, 1<<20)
	g.Grant(testToken, testSpace, true)
	info, err := DescribeToken(context.Background(), Config{Endpoint: g.URL(), Tokens: &rotating{tokens: []string{testToken}}, Backoff: noBackoff})
	if err != nil || info.Space != testSpace || info.Access != AccessRead {
		t.Fatalf("DescribeToken() = %+v, %v", info, err)
	}
}
