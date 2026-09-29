package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/git2"
	"github.com/baalimago/slivingdoc/internal/httpstore"
	"github.com/baalimago/slivingdoc/internal/httpstore/gatewaytest"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/sitelogin"
	"github.com/baalimago/slivingdoc/internal/sitelogin/sitetest"
	"github.com/baalimago/slivingdoc/internal/storage"
)

// testClock is a clock a test moves by hand.
type testClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

const mintEndpoint = "https://api.example.test"

// mintRig is a site that issued loginToken for mintEndpoint, reaching a
// read-write space notes and a read-only space team, and a token source
// over it with a clock the test moves.
func mintRig(t *testing.T, access credentials.Access) (*sitetest.Site, *mintedTokens, *testClock) {
	t.Helper()
	site := sitetest.Start(t)
	site.Issued(loginToken, mintEndpoint)
	site.SetSpaces(loginToken, notesSpace, space("team", "bob@example.test", "read"))
	clock := &testClock{at: time.Now()}
	login := credentials.Login{ID: credentials.ID{Site: site.URL(), Endpoint: mintEndpoint}, Key: loginToken, Access: access, Account: "ada@example.test"}
	tokens, err := newMintedTokens(login, "notes", nil, clock.now)
	if err != nil {
		t.Fatalf("newMintedTokens() = %v", err)
	}
	return site, tokens, clock
}

func mustToken(t *testing.T, tokens *mintedTokens) string {
	t.Helper()
	token, err := tokens.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() = %v", err)
	}
	return token
}

func TestMintedTokensRenewBeforeTheyExpire(t *testing.T) {
	site, tokens, clock := mintRig(t, credentials.AccessWrite)
	site.SetMintLifetime(100 * time.Second)
	first := mustToken(t, tokens)
	if first == loginToken {
		t.Fatal("the key itself was returned as the space token")
	}
	clock.advance(79 * time.Second)
	if got := mustToken(t, tokens); got != first || len(site.Mints()) != 1 {
		t.Fatalf("Token() at 79%% of the lifetime = a new token after %d mints, want the same one", len(site.Mints()))
	}
	clock.advance(2 * time.Second)
	second := mustToken(t, tokens)
	if second == first || len(site.Mints()) != 2 {
		t.Fatalf("Token() past 80%% of the lifetime minted %d times, want a second token", len(site.Mints()))
	}
	if mints := site.Mints(); mints[0].Access != "write" || mints[0].Space != "notes" {
		t.Fatalf("mints = %+v, want write tokens of notes", mints)
	}

	tokens.Rejected(first)
	if got := mustToken(t, tokens); got != second {
		t.Fatal("a refusal of an older token discarded the current one")
	}
	tokens.Rejected(second)
	if got := mustToken(t, tokens); got == second || len(site.Mints()) != 3 {
		t.Fatalf("Token() after the current one was refused = the same token after %d mints, want a new one", len(site.Mints()))
	}
}

func TestMintedTokensOfAReadLoginAreRead(t *testing.T) {
	site, tokens, _ := mintRig(t, credentials.AccessRead)
	mustToken(t, tokens)
	if mints := site.Mints(); len(mints) != 1 || mints[0].Access != "read" {
		t.Fatalf("mints = %+v, want one read token", mints)
	}
	// A write login still reads a read-only space: it asks for no more
	// than the grant allows.
	_, writer, _ := mintRig(t, credentials.AccessWrite)
	writer.space = "team"
	if _, err := writer.Token(context.Background()); err != nil {
		t.Fatalf("Token() of a read-only space for a write login = %v", err)
	}
}

// TestMintedTokensStayOnTheirFirstSpace proves a source is bound to the
// space id of its first mint: a renewal the site answers for another space
// of the same name is refused and its token revoked, so a running process
// never writes its workspaces into a space that replaced theirs.
func TestMintedTokensStayOnTheirFirstSpace(t *testing.T) {
	site, tokens, _ := mintRig(t, credentials.AccessWrite)
	replaced := notesSpace
	replaced.ID = "notes-first"
	site.SetSpaces(loginToken, replaced)
	mustToken(t, tokens)
	if got := tokens.boundSpace(); got != "notes-first" {
		t.Fatalf("boundSpace() = %q, want the first mint's id", got)
	}
	tokens.Rejected(mustToken(t, tokens))
	held := mustToken(t, tokens)

	replaced.ID = "notes-second"
	site.SetSpaces(loginToken, replaced)
	tokens.Rejected(held)
	_, err := tokens.Token(context.Background())
	if !errors.Is(err, storage.ErrAccessDenied) || !strings.Contains(err.Error(), "no longer the space this process started with") {
		t.Fatalf("Token() after the space was replaced = %v, want an access refusal", err)
	}
	mints := site.Mints()
	if revoked := site.Revoked(); len(mints) != 3 || len(revoked) != 1 || revoked[0] != mints[2].Token {
		t.Fatalf("revoked = %v after mints %+v, want the token of the other space", revoked, mints)
	}
	if got := tokens.boundSpace(); got != "notes-first" {
		t.Fatalf("boundSpace() = %q after the refusal, want the first id kept", got)
	}
}

func TestMintedTokensMapTheSitesRefusals(t *testing.T) {
	for _, row := range []struct {
		name  string
		setup func(*sitetest.Site, *mintedTokens)
		want  error
		text  string
	}{
		{"a revoked key", func(s *sitetest.Site, _ *mintedTokens) { s.Revoke(loginToken) }, storage.ErrAccessDenied, "no longer accepts the stored login"},
		{"an unknown space", func(_ *sitetest.Site, m *mintedTokens) { m.space = "other" }, storage.ErrAccessDenied, `reaches no space "other"`},
		{"a suspended space", func(s *sitetest.Site, _ *mintedTokens) { s.Suspend("notes") }, storage.ErrAccessDenied, `space "notes" is suspended`},
		{"a write token of a read-only space", func(_ *sitetest.Site, m *mintedTokens) { m.space, m.access = "team", credentials.AccessWrite }, storage.ErrAccessDenied, `may not use space "team"`},
		{"busy", func(s *sitetest.Site, _ *mintedTokens) { s.RefuseMint(http.StatusTooManyRequests, "busy") }, storage.ErrRateLimited, "busy minting"},
		{"a failing site", func(s *sitetest.Site, _ *mintedTokens) { s.RefuseMint(http.StatusBadGateway, "bad_gateway") }, storage.ErrTransport, "HTTP 502"},
		{"a refusal outside the contract", func(s *sitetest.Site, _ *mintedTokens) { s.RefuseMint(http.StatusBadRequest, "invalid_request") }, storage.ErrIncompatible, "refused to mint"},
		{"a broken answer", func(s *sitetest.Site, _ *mintedTokens) { s.BreakMint("{") }, storage.ErrIncompatible, "not the expected JSON"},
		{"a closed site", func(s *sitetest.Site, _ *mintedTokens) { s.Close() }, storage.ErrTransport, "unreachable"},
	} {
		t.Run(row.name, func(t *testing.T) {
			site, tokens, _ := mintRig(t, credentials.AccessWrite)
			row.setup(site, tokens)
			_, err := tokens.Token(context.Background())
			if !errors.Is(err, row.want) || !strings.Contains(err.Error(), row.text) || strings.Contains(err.Error(), loginToken) {
				t.Fatalf("Token() = %v, want %v saying %q without the key", err, row.want, row.text)
			}
		})
	}
}

func TestMintedTokensRevokeWhatTheyCannotUse(t *testing.T) {
	t.Run("another endpoint", func(t *testing.T) {
		site, tokens, _ := mintRig(t, credentials.AccessWrite)
		tokens.endpoint = "https://elsewhere.example.test"
		_, err := tokens.Token(context.Background())
		if !errors.Is(err, storage.ErrIncompatible) || !strings.Contains(err.Error(), "another storage endpoint") {
			t.Fatalf("Token() = %v, want the endpoint refusal", err)
		}
		if got := site.Revoked(); len(got) != 1 || got[0] != site.Mints()[0].Token {
			t.Fatalf("revoked = %v, want the minted token", got)
		}
	})
	t.Run("already expired by this clock", func(t *testing.T) {
		site, tokens, clock := mintRig(t, credentials.AccessWrite)
		clock.advance(2 * time.Hour)
		_, err := tokens.Token(context.Background())
		if !errors.Is(err, storage.ErrIncompatible) || !strings.Contains(err.Error(), "check the system clock") {
			t.Fatalf("Token() = %v, want the clock refusal", err)
		}
		if got := site.Revoked(); len(got) != 1 {
			t.Fatalf("revoked = %v, want the minted token", got)
		}
	})
	t.Run("an answer for another space", func(t *testing.T) {
		site, tokens, _ := mintRig(t, credentials.AccessWrite)
		site.OnMint(func(m sitetest.Minted) {
			site.BreakMint(fmt.Sprintf(`{"token":%q,"space":"team","access":"write","expiresAt":"2030-01-01T00:00:00Z","endpoint":%q,"owner":"a@x"}`, m.Token, mintEndpoint))
		})
		site.BreakMint(`placeholder`)
		// The first mint answers the placeholder, and the second the
		// first mint's token under another space, as the hook set it.
		if _, err := tokens.Token(context.Background()); !errors.Is(err, storage.ErrIncompatible) {
			t.Fatalf("Token() = %v, want ErrIncompatible", err)
		}
		_, err := tokens.Token(context.Background())
		if !errors.Is(err, storage.ErrIncompatible) || !strings.Contains(err.Error(), "another space") {
			t.Fatalf("Token() = %v, want the space refusal", err)
		}
		if got := site.Revoked(); len(got) != 1 || got[0] != site.Mints()[0].Token {
			t.Fatalf("revoked = %v, want the token the answer carried", got)
		}
	})
	t.Run("an unusable site", func(t *testing.T) {
		if _, err := newMintedTokens(credentials.Login{ID: credentials.ID{Site: "http://www.example.test"}}, "notes", nil, nil); err == nil {
			t.Fatal("newMintedTokens() with a plain http site = nil, want a refusal")
		}
	})
}

func TestMintRefusalNamesTheFix(t *testing.T) {
	login := &credentials.Login{ID: credentials.ID{Site: "https://site.example.test"}}
	for _, row := range []struct {
		err  error
		cfg  config
		want string
	}{
		{storage.ErrAccessDenied, config{bucket: "notes", bucketFrom: bucketFromLogin}, "'slivingdoc space <name>' to choose the default"},
		{storage.ErrAccessDenied, config{bucket: "notes", bucketFrom: bucketFromSpaceFlag}, "check --space, run 'slivingdoc space'"},
		{storage.ErrRateLimited, config{bucket: "notes"}, "try again later"},
		{storage.ErrTransport, config{bucket: "notes"}, "try again later"},
		{storage.ErrIncompatible, config{bucket: "notes"}, "update slivingdoc"},
	} {
		row.cfg.login = login
		err := mintRefusal(fmt.Errorf("x %s: %w", loginToken, row.err), row.cfg)
		if !strings.Contains(err.Error(), `could not mint a token for space "notes" at https://site.example.test`) ||
			!strings.Contains(err.Error(), row.want) || strings.Contains(err.Error(), loginToken) {
			t.Fatalf("mintRefusal(%v) = %v, want it to contain %q and no key", row.err, err, row.want)
		}
	}
}

// loginProcess is a process whose stored login for g reaches the space
// notes at site, which grants every token it mints on g.
func loginProcess(t *testing.T) (process, *gatewaytest.Gateway, *sitetest.Site) {
	t.Helper()
	g := gatewaytest.Start(t)
	g.AddSpace("notes", 1<<20)
	site := sitetest.Start(t)
	site.Issued(loginToken, g.URL())
	site.SetSpaces(loginToken, notesSpace)
	site.OnMint(func(m sitetest.Minted) { g.Grant(m.Token, m.Space, m.Access == "read") })
	login := entry(g.URL(), loginToken)
	login.Site = site.URL()
	p := testProcess([]string{writeLogins(t, defaults(g.URL(), "notes"), login)})
	p.storeFactory = realStoreFactory
	return p, g, site
}

func TestSetupMintsFromTheStoredLogin(t *testing.T) {
	p, g, site := loginProcess(t)
	rt, err := setup(p)
	if err != nil {
		t.Fatalf("setup() = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	used := g.Used()
	if len(site.Mints()) != 1 || used[site.Mints()[0].Token] == 0 || used[loginToken] != 0 {
		t.Fatalf("gateway requests per token = %v after %d mints; want the minted token only, never the key", used, len(site.Mints()))
	}
	if rt.cfg.bucket != "notes" || rt.cfg.bucketFrom != bucketFromLogin || rt.cfg.tokens == nil {
		t.Fatalf("runtime = bucket %q from %v, want the default space and a token source", rt.cfg.bucket, rt.cfg.bucketFrom)
	}
}

func TestSetupRefusesALoginThatCannotMint(t *testing.T) {
	p, g, site := loginProcess(t)
	site.SetSpaces(loginToken)
	_, err := setup(p)
	if err == nil || !strings.Contains(err.Error(), `could not mint a token for space "notes"`) ||
		!strings.Contains(err.Error(), "slivingdoc space") || strings.Contains(err.Error(), loginToken) {
		t.Fatalf("setup() = %v, want the mint refusal naming the space command", err)
	}
	if g.Requests() != 0 {
		t.Fatal("a process that could not mint reached the storage API")
	}
	p.env = append(p.env, "SLIVINGDOC_SPACE=Bad_Space")
	if _, err := setup(p); err == nil || !strings.Contains(err.Error(), "invalid space name") {
		t.Fatalf("setup() with an invalid space = %v", err)
	}
}

// TestNewMintedTokensUsesTheDefaults proves a nil client and clock take
// the production defaults.
func TestNewMintedTokensUsesTheDefaults(t *testing.T) {
	tokens, err := newMintedTokens(credentials.Login{ID: credentials.ID{Site: sitelogin.DefaultSite}}, "notes", nil, nil)
	if err != nil || tokens.now == nil || tokens.client.Site() != sitelogin.DefaultSite {
		t.Fatalf("newMintedTokens() = %+v, %v", tokens, err)
	}
}

// TestMintedTokensCompareWallClocks proves renewal is scheduled on the wall
// clock: the renewal time carries no monotonic reading, so a machine that
// slept past it (its monotonic clock stopped meanwhile) renews on waking.
func TestMintedTokensCompareWallClocks(t *testing.T) {
	site, tokens, _ := mintRig(t, credentials.AccessWrite)
	tokens.now = time.Now // a reading with a monotonic clock
	site.SetMintLifetime(time.Hour)
	mustToken(t, tokens)
	tokens.mu.Lock()
	renewAt := tokens.renewAt
	tokens.mu.Unlock()
	if strings.Contains(renewAt.String(), "m=") {
		t.Fatalf("renewal time %v carries a monotonic reading", renewAt)
	}
	// Past the renewal time on the wall clock alone, a new token is minted.
	tokens.now = func() time.Time { return renewAt.Add(time.Second) }
	mustToken(t, tokens)
	if got := len(site.Mints()); got != 2 {
		t.Fatalf("mints = %d after the wall clock passed the renewal time, want 2", got)
	}
}

// TestMintedTokensAreSafeConcurrently runs Token and Rejected from many
// goroutines at once, under -race: every caller gets a token the site
// minted, and refusals never leave a caller without one.
func TestMintedTokensAreSafeConcurrently(t *testing.T) {
	site, tokens, _ := mintRig(t, credentials.AccessWrite)
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for range 32 {
		wg.Go(func() {
			token, err := tokens.Token(context.Background())
			if err != nil {
				errs <- err
				return
			}
			tokens.Rejected(token)
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("Token() = %v", err)
	}
	token := mustToken(t, tokens)
	minted := map[string]bool{}
	for _, m := range site.Mints() {
		minted[m.Token] = true
	}
	if !minted[token] || token == loginToken {
		t.Fatal("Token() = a token the site never minted")
	}
}

// TestMintedTokensWaitWithinTheCallersContext proves a caller waiting for
// another caller's mint gives up when its own context ends, and the mint
// in flight still completes for its caller.
func TestMintedTokensWaitWithinTheCallersContext(t *testing.T) {
	site, tokens, _ := mintRig(t, credentials.AccessWrite)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	site.OnMint(func(sitetest.Minted) {
		once.Do(func() { close(entered) })
		<-release
	})
	first := make(chan error, 1)
	go func() {
		_, err := tokens.Token(context.Background())
		first <- err
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := tokens.Token(ctx); !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, storage.ErrTransport) {
		t.Fatalf("Token() while another mint runs, past the caller's deadline = %v, want its deadline", err)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("the mint in flight = %v", err)
	}
}

// TestStreamedUploadRefusedUnderAStoredLogin proves, over a real minted
// token source, a real hosted store and the real engine, that a pack
// upload the gateway refuses with 401 is a retryable PACK_UPLOAD failure
// that says the token may have expired or been renewed, never the access
// refusal naming SLIVINGDOC_TOKEN, and that the retry mints a new token
// and publishes.
func TestStreamedUploadRefusedUnderAStoredLogin(t *testing.T) {
	g := gatewaytest.Start(t)
	g.AddSpace("notes", 1<<20)
	site := sitetest.Start(t)
	site.Issued(loginToken, g.URL())
	site.SetSpaces(loginToken, notesSpace)
	site.OnMint(func(m sitetest.Minted) { g.Grant(m.Token, m.Space, m.Access == "read") })
	login := credentials.Login{ID: credentials.ID{Site: site.URL(), Endpoint: g.URL()}, Key: loginToken, Access: credentials.AccessWrite, Account: "ada@example.test"}
	tokens, err := newMintedTokens(login, "notes", nil, nil)
	if err != nil {
		t.Fatalf("newMintedTokens() = %v", err)
	}
	store, err := httpstore.New(httpstore.Config{Endpoint: g.URL(), Space: "notes", Tokens: tokens})
	if err != nil {
		t.Fatalf("httpstore.New() = %v", err)
	}
	eng := git2.New()
	if err := eng.Open(); err != nil {
		t.Fatalf("git2.Open() = %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	cfg := testServiceConfig(t)
	svc, err := NewService(eng, store, cfg.serviceConfig(), nil)
	if err != nil {
		t.Fatalf("NewService() = %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	path := filepath.Join(cfg.workspaceRoot, "notes")
	if _, err := svc.Pull(context.Background(), path); err != nil {
		t.Fatalf("Pull() = %v", err)
	}
	writeNote(t, path, "a.md", "hello")

	g.RefuseNext(http.MethodPut, http.StatusUnauthorized, "invalid_token")
	_, err = svc.Commit(context.Background(), path, "first")
	var refused *notebook.Error
	if !errors.As(err, &refused) || refused.Code != notebook.CodeStorageFailure || refused.Reason != notebook.ReasonPackUpload ||
		refused.Action != notebook.ActionRetry || !errors.Is(err, storage.ErrCredentialRenewed) {
		t.Fatalf("Commit() with the upload refused = %v, want a retryable PACK_UPLOAD of a renewed credential", err)
	}
	if !strings.Contains(refused.Message, "may have expired or been renewed") || strings.Contains(refused.Message, "SLIVINGDOC_TOKEN") {
		t.Fatalf("message = %q, want the renewal hint without token advice", refused.Message)
	}
	if _, err := svc.Commit(context.Background(), path, "first"); err != nil {
		t.Fatalf("Commit() retried = %v, want it published with a new token", err)
	}
	if mints := site.Mints(); len(mints) != 2 {
		t.Fatalf("mints = %d, want the refused token replaced by a second one", len(mints))
	}
}
