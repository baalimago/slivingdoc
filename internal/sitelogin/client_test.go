package sitelogin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/sitelogin/sitetest"
)

const (
	testToken = "sld_0123456789abcdef_c2l0ZWxvZ2luLXRlc3QtdG9rZW4tZm9yLXRoZS1jbGllbnQ"
	endpoint  = "https://api.slivingdoc.dev"
)

// clock is a fake clock that the fake sleeper advances, recording every
// wait the client asks for.
type clock struct {
	now    time.Time
	sleeps []time.Duration
}

func (c *clock) Now() time.Time { return c.now }

func (c *clock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
	return nil
}

func newClient(t *testing.T, site string) (*Client, *clock) {
	t.Helper()
	c := &clock{now: time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)}
	client, err := New(Config{Site: site, UserAgent: "slivingdoc/test", Sleep: c.Sleep, Now: c.Now})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	return client, c
}

func issue() sitetest.Issue {
	return sitetest.Issue{
		Token: testToken, Space: "notes", Access: "write", Endpoint: endpoint,
		ExpiresAt: time.Date(2026, 12, 26, 12, 0, 0, 0, time.UTC),
		Account:   "ada@example.test", Owner: "bob@example.test",
	}
}

func TestNewValidatesTheSite(t *testing.T) {
	for _, site := range []string{
		"", "ftp://example.test", "http://www.example.test", "https://user@www.example.test",
		"https://www.example.test/cli", "https://www.example.test?x=1", "https://www.example.test#x", "://",
	} {
		if _, err := New(Config{Site: site}); err == nil {
			t.Errorf("New(%q) = nil, want a refusal", site)
		}
	}
	for site, want := range map[string]string{
		"http://127.0.0.1:8788":       "http://127.0.0.1:8788",
		"http://localhost:8788/":      "http://localhost:8788",
		"https://WWW.Slivingdoc.dev":  "https://www.slivingdoc.dev",
		"http://[::1]:9000":           "http://[::1]:9000",
		"https://www.slivingdoc.dev/": "https://www.slivingdoc.dev",
	} {
		c, err := New(Config{Site: site})
		if err != nil || c.Site() != want {
			t.Errorf("New(%q) = %v, %v; want site %q", site, c, err, want)
		}
	}
}

func TestLoginApproved(t *testing.T) {
	site := sitetest.Start(t)
	site.Next(sitetest.Script{Pending: []string{"authorization_pending", "slow_down", "authorization_pending"}, Issue: issue()})
	client, clk := newClient(t, site.URL())

	a, err := client.Start(context.Background(), StartRequest{Space: "notes", Access: credentials.AccessRead, Client: "  my\thost\x00" + strings.Repeat("x", 80)})
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	starts := site.Starts()
	if len(starts) != 1 || starts[0].Space != "notes" || starts[0].Access != "read" {
		t.Fatalf("start requests = %+v", starts)
	}
	if got := starts[0].Client; len(got) != clientLimit || strings.ContainsAny(got, "\t\x00") || !strings.HasPrefix(got, "myhostx") {
		t.Fatalf("client label = %q, want printable ASCII cut to %d", got, clientLimit)
	}
	if a.UserCode != "BCDF-GHJK" || a.CompleteURI != site.URL()+"/cli/login?code=BCDF-GHJK" || a.URI != site.URL()+"/cli/login" {
		t.Fatalf("approval = %+v", a)
	}
	if a.Interval != time.Second || !a.Deadline.Equal(clk.now.Add(10*time.Minute)) {
		t.Fatalf("approval timing = %v until %v", a.Interval, a.Deadline)
	}

	got, err := client.Wait(context.Background(), a)
	if err != nil {
		t.Fatalf("Wait() = %v", err)
	}
	if got.Token != testToken || got.Space != "notes" || got.Access != credentials.AccessWrite || got.Endpoint != endpoint ||
		got.Expires.Describe() != "until 2026-12-26 12:00 UTC" || got.Account != "ada@example.test" || got.Owner != "bob@example.test" {
		t.Fatalf("Wait() = %+v", got)
	}
	want := []time.Duration{time.Second, time.Second, 6 * time.Second, 6 * time.Second}
	if !reflect.DeepEqual(clk.sleeps, want) {
		t.Fatalf("waits = %v, want %v (slow_down adds five seconds)", clk.sleeps, want)
	}
	if _, err := client.Wait(context.Background(), a); !errors.Is(err, ErrCodeExpired) {
		t.Fatalf("second Wait() of a claimed code = %v, want ErrCodeExpired", err)
	}
}

func TestLoginOutcomes(t *testing.T) {
	tests := []struct {
		name   string
		script sitetest.Script
		want   error
	}{
		{"denied", sitetest.Script{Pending: []string{"authorization_pending"}, Final: "access_denied"}, ErrDenied},
		{"busy", sitetest.Script{Final: "busy"}, ErrRefused},
		{"expired", sitetest.Script{Final: "expired_token"}, ErrCodeExpired},
		{"invalid request", sitetest.Script{Final: "invalid_request"}, ErrRefused},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			site := sitetest.Start(t)
			site.Next(tt.script)
			client, _ := newClient(t, site.URL())
			a, err := client.Start(context.Background(), StartRequest{Access: credentials.AccessWrite})
			if err != nil {
				t.Fatalf("Start() = %v", err)
			}
			if _, err := client.Wait(context.Background(), a); !errors.Is(err, tt.want) {
				t.Fatalf("Wait() = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestWaitStopsAtTheDeadline(t *testing.T) {
	site := sitetest.Start(t)
	pending := make([]string, 100)
	for i := range pending {
		pending[i] = "authorization_pending"
	}
	site.Next(sitetest.Script{Pending: pending})
	site.SetTiming(5, 12)
	client, _ := newClient(t, site.URL())
	a, err := client.Start(context.Background(), StartRequest{Access: credentials.AccessWrite})
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	if _, err := client.Wait(context.Background(), a); !errors.Is(err, ErrCodeExpired) {
		t.Fatalf("Wait() = %v, want ErrCodeExpired", err)
	}
	if got := site.Polls(); got != 2 {
		t.Fatalf("polls = %d, want 2 (at 5 s and 10 s; 15 s is past the 12 s life)", got)
	}
}

func TestWaitStopsWhenCancelled(t *testing.T) {
	site := sitetest.Start(t)
	client, _ := newClient(t, site.URL())
	a, err := client.Start(context.Background(), StartRequest{Access: credentials.AccessWrite})
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Wait(ctx, a); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait() = %v, want context.Canceled", err)
	}
}

func TestIssuedTokenIsValidated(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*sitetest.Issue)
	}{
		{"token with white space", func(i *sitetest.Issue) { i.Token = "sld bad" }},
		{"bad space", func(i *sitetest.Issue) { i.Space = "No_Space" }},
		{"bad access", func(i *sitetest.Issue) { i.Access = "admin" }},
		{"plain http remote endpoint", func(i *sitetest.Issue) { i.Endpoint = "http://api.example.test" }},
		{"endpoint not a URL", func(i *sitetest.Issue) { i.Endpoint = "api" }},
		{"no account", func(i *sitetest.Issue) { i.Account = "" }},
		{"owner with a control character", func(i *sitetest.Issue) { i.Owner = "bob\x1b[2J@example.test" }},
		{"no expiry", func(i *sitetest.Issue) { i.ExpiresAt = time.Time{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			site := sitetest.Start(t)
			is := issue()
			tt.mutate(&is)
			site.Next(sitetest.Script{Issue: is})
			client, _ := newClient(t, site.URL())
			a, err := client.Start(context.Background(), StartRequest{Access: credentials.AccessWrite})
			if err != nil {
				t.Fatalf("Start() = %v", err)
			}
			_, err = client.Wait(context.Background(), a)
			if !errors.Is(err, ErrProtocol) || strings.Contains(err.Error(), "sld bad") || strings.Contains(err.Error(), testToken) {
				t.Fatalf("Wait() = %v, want ErrProtocol without the token", err)
			}
			// A sendable token comes back for the caller to revoke.
			var rejected *RejectedTokenError
			if got := errors.As(err, &rejected); got != (is.Token == testToken) || (got && rejected.Token != testToken) {
				t.Fatalf("Wait() = %#v, want the token returned for revocation exactly when it is sendable", err)
			}
		})
	}
}

// flakyDoer fails the first token polls: the first with no answer, the
// others with a 502, then forwards to the real client.
type flakyDoer struct {
	failures int
	seen     int
}

func (d *flakyDoer) Do(req *http.Request) (*http.Response, error) {
	if strings.HasSuffix(req.URL.Path, tokenPath) && d.seen < d.failures {
		d.seen++
		if d.seen == 1 {
			return nil, errors.New("connection reset")
		}
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(strings.NewReader(`{"error":"internal"}`)),
			Header:     http.Header{},
		}, nil
	}
	return http.DefaultClient.Do(req)
}

func flakyClient(t *testing.T, site string, failures int) (*Client, *clock) {
	t.Helper()
	c := &clock{now: time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)}
	client, err := New(Config{Site: site, Sleep: c.Sleep, Now: c.Now, Client: &flakyDoer{failures: failures}})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	return client, c
}

func TestWaitRetriesTransientFailures(t *testing.T) {
	site := sitetest.Start(t)
	site.Next(sitetest.Script{Issue: issue()})
	client, clk := flakyClient(t, site.URL(), 3)
	a, err := client.Start(context.Background(), StartRequest{Access: credentials.AccessWrite})
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	got, err := client.Wait(context.Background(), a)
	if err != nil || got.Token != testToken {
		t.Fatalf("Wait() = %+v, %v; want the token after the failures", got, err)
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	if !reflect.DeepEqual(clk.sleeps, want) {
		t.Fatalf("waits = %v, want %v (doubling after each failure)", clk.sleeps, want)
	}
	if retryWait(time.Second, 20) != maxRetryWait {
		t.Fatal("the backoff is not capped")
	}
}

func TestWaitReportsTheLastFailureAtExpiry(t *testing.T) {
	site := sitetest.Start(t)
	site.SetTiming(5, 60)
	client, _ := flakyClient(t, site.URL(), 1000)
	a, err := client.Start(context.Background(), StartRequest{Access: credentials.AccessWrite})
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	_, err = client.Wait(context.Background(), a)
	if !errors.Is(err, ErrCodeExpired) || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("Wait() = %v, want ErrCodeExpired naming the last failure", err)
	}
}

// answer serves one fixed answer to every request.
func answer(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status == http.StatusFound {
			http.Redirect(w, r, "https://elsewhere.example.test/", status)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(strings.ReplaceAll(body, "SITE", "http://"+r.Host)))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestStartRefusesAnswersOutsideTheContract(t *testing.T) {
	good := `"deviceCode":"dev","userCode":"BCDF-GHJK","verificationUri":"SITE/cli/login","verificationUriComplete":"SITE/cli/login?code=BCDF-GHJK"`
	tests := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"not json", http.StatusOK, `nope`, ErrProtocol},
		{"no device code", http.StatusOK, `{"userCode":"BCDF-GHJK","verificationUri":"SITE/cli/login","verificationUriComplete":"SITE/cli/login","interval":5,"expiresIn":600}`, ErrProtocol},
		{"control characters in the code", http.StatusOK, `{"deviceCode":"dev","userCode":"\u001b[31m","verificationUri":"SITE/cli/login","verificationUriComplete":"SITE/cli/login","interval":5,"expiresIn":600}`, ErrProtocol},
		{"page on another host", http.StatusOK, `{"deviceCode":"dev","userCode":"BCDF-GHJK","verificationUri":"https://evil.example.test/","verificationUriComplete":"SITE/cli/login","interval":5,"expiresIn":600}`, ErrProtocol},
		{"page with another scheme", http.StatusOK, `{"deviceCode":"dev","userCode":"BCDF-GHJK","verificationUri":"SITE/cli/login","verificationUriComplete":"file:///etc/passwd","interval":5,"expiresIn":600}`, ErrProtocol},
		{"no lifetime", http.StatusOK, `{` + good + `,"interval":5,"expiresIn":0}`, ErrProtocol},
		{"negative interval", http.StatusOK, `{` + good + `,"interval":-1,"expiresIn":600}`, ErrProtocol},
		{"refusal", http.StatusBadRequest, `{"error":"invalid_request","message":"bad\nspace"}`, ErrRefused},
		{"redirect", http.StatusFound, ``, ErrRefused},
		{"server error without a body", http.StatusBadGateway, ``, ErrRefused},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := newClient(t, answer(t, tt.status, tt.body))
			_, err := client.Start(context.Background(), StartRequest{Access: credentials.AccessWrite})
			if !errors.Is(err, tt.want) {
				t.Fatalf("Start() = %v, want %v", err, tt.want)
			}
			if strings.ContainsAny(err.Error(), "\n\x1b") {
				t.Fatalf("Start() = %q carries control characters", err)
			}
		})
	}
}

func TestRefusalText(t *testing.T) {
	client, _ := newClient(t, answer(t, http.StatusBadRequest, `{"error":"invalid_request","message":"bad\nspace"}`))
	_, err := client.Start(context.Background(), StartRequest{Access: credentials.AccessWrite})
	if err == nil || err.Error() != "sitelogin: the site answered HTTP 400 invalid_request: bad space" {
		t.Fatalf("Start() = %v", err)
	}
}

func TestRevoke(t *testing.T) {
	site := sitetest.Start(t)
	site.Issued(testToken)
	client, _ := newClient(t, site.URL())
	if err := client.Revoke(context.Background(), testToken); err != nil {
		t.Fatalf("Revoke() = %v", err)
	}
	if got := site.Revoked(); len(got) != 1 || got[0] != testToken {
		t.Fatalf("revoked = %v", got)
	}
	if err := client.Revoke(context.Background(), testToken); err != nil {
		t.Fatalf("Revoke() of a revoked token = %v, want done (401)", err)
	}
	if err := client.Revoke(context.Background(), "sld bad"); err == nil || strings.Contains(err.Error(), "sld bad") {
		t.Fatalf("Revoke() of an unsendable token = %v, want a refusal without it", err)
	}
	broken, _ := newClient(t, answer(t, http.StatusInternalServerError, `{"error":"internal"}`))
	if err := broken.Revoke(context.Background(), testToken); !errors.Is(err, ErrRefused) {
		t.Fatalf("Revoke() against a failing site = %v, want ErrRefused", err)
	}
}

func TestUnreachableSite(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	client, _ := newClient(t, url)
	_, err := client.Start(context.Background(), StartRequest{Access: credentials.AccessWrite})
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("Start() = %v, want an unreachable site", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Start(ctx, StartRequest{Access: credentials.AccessWrite}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Start() cancelled = %v, want context.Canceled", err)
	}
}

func TestDefaultsNeedNoSeams(t *testing.T) {
	c, err := New(Config{Site: DefaultSite})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("default sleep = %v, want context.Canceled", err)
	}
	if err := c.sleep(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("default sleep = %v", err)
	}
	if c.now().IsZero() {
		t.Fatal("default clock is zero")
	}
}
