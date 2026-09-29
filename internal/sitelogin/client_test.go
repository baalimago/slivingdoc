package sitelogin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
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
		Key: testToken, Access: "write", Endpoint: endpoint,
		ExpiresAt: time.Date(2026, 12, 26, 12, 0, 0, 0, time.UTC),
		Account:   "ada@example.test",
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
		"http://127.0.0.1:8788":            "http://127.0.0.1:8788",
		"http://localhost:8788/":           "http://localhost:8788",
		"https://WWW.Slivingdoc.dev":       "https://www.slivingdoc.dev",
		"http://[::1]:9000":                "http://[::1]:9000",
		"https://www.slivingdoc.dev/":      "https://www.slivingdoc.dev",
		"https://www.slivingdoc.dev:443":   DefaultSite,
		"https://WWW.slivingdoc.dev.:443/": DefaultSite,
		"https://www.slivingdoc.dev:8443":  "https://www.slivingdoc.dev:8443",
		"http://localhost:80":              "http://localhost",
		"http://[::1]:80":                  "http://[::1]",
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

	a, err := client.Start(context.Background(), StartRequest{Access: credentials.AccessRead, Client: "  my\thost\x00" + strings.Repeat("x", 80)})
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	starts := site.Starts()
	if len(starts) != 1 || starts[0].Access != "read" {
		t.Fatalf("start requests = %+v", starts)
	}
	if got := starts[0].Client; len(got) != clientLimit || strings.ContainsAny(got, "\t\x00") || !strings.HasPrefix(got, "myhostx") {
		t.Fatalf("client label = %q, want printable ASCII cut to %d", got, clientLimit)
	}
	if a.UserCode != "BCDF-GHJK" || a.CompleteURI != site.URL()+"/cli/login#BCDF-GHJK" || a.URI != site.URL()+"/cli/login" {
		t.Fatalf("approval = %+v", a)
	}
	if a.Interval != time.Second || !a.Deadline.Equal(clk.now.Add(10*time.Minute)) {
		t.Fatalf("approval timing = %v until %v", a.Interval, a.Deadline)
	}

	got, err := client.Wait(context.Background(), a)
	if err != nil {
		t.Fatalf("Wait() = %v", err)
	}
	if got.Key != testToken || got.Access != credentials.AccessWrite || got.Endpoint != endpoint ||
		got.Expires.Describe() != "until 2026-12-26 12:00 UTC" || got.Account != "ada@example.test" {
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
	if _, err := client.Wait(context.Background(), a); !errors.Is(err, ErrCodeExpired) || strings.Contains(err.Error(), TokenHint) {
		t.Fatalf("Wait() = %v, want ErrCodeExpired without the hint: every poll was answered", err)
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

func TestIssuedKeyIsValidated(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*sitetest.Issue)
	}{
		{"key with white space", func(i *sitetest.Issue) { i.Key = "sld bad" }},
		{"bad access", func(i *sitetest.Issue) { i.Access = "admin" }},
		{"plain http remote endpoint", func(i *sitetest.Issue) { i.Endpoint = "http://api.example.test" }},
		{"endpoint not a URL", func(i *sitetest.Issue) { i.Endpoint = "api" }},
		{"no account", func(i *sitetest.Issue) { i.Account = "" }},
		{"account with a control character", func(i *sitetest.Issue) { i.Account = "ada\x1b[2J@example.test" }},
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
				t.Fatalf("Wait() = %v, want ErrProtocol without the key", err)
			}
			// A sendable key comes back for the caller to revoke.
			var rejected *RejectedError
			if got := errors.As(err, &rejected); got != (is.Key == testToken) || (got && rejected.Credential != testToken) {
				t.Fatalf("Wait() = %#v, want the key returned for revocation exactly when it is sendable", err)
			}
		})
	}
}

// broken is a body that breaks off after a few bytes.
func broken(prefix string) io.ReadCloser {
	return io.NopCloser(io.MultiReader(strings.NewReader(prefix), iotest.ErrReader(errors.New("unexpected EOF"))))
}

// flakyDoer fails the first token polls: the first with no answer, the
// second with a 502 whose body breaks while it is read, the others with a
// 502, then forwards to the real client. With breakIssue it breaks the
// body of the success answer that carries the token instead.
type flakyDoer struct {
	failures   int
	seen       int
	breakIssue bool
}

func (d *flakyDoer) Do(req *http.Request) (*http.Response, error) {
	if !strings.HasSuffix(req.URL.Path, tokenPath) {
		return http.DefaultClient.Do(req)
	}
	if d.breakIssue {
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			return resp, err
		}
		resp.Body.Close()
		resp.Body = broken(`{"tok`)
		return resp, nil
	}
	if d.seen < d.failures {
		d.seen++
		if d.seen == 1 {
			return nil, errors.New("connection reset")
		}
		body := io.NopCloser(strings.NewReader(`{"error":"internal"}`))
		if d.seen == 2 {
			body = broken(`{"err`)
		}
		return &http.Response{StatusCode: http.StatusBadGateway, Body: body, Header: http.Header{}}, nil
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
	if err != nil || got.Key != testToken {
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

func TestWaitEndsWhenTheIssuedAnswerBreaks(t *testing.T) {
	site := sitetest.Start(t)
	site.Next(sitetest.Script{Issue: issue()})
	clk := &clock{now: time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)}
	client, err := New(Config{Site: site.URL(), Sleep: clk.Sleep, Now: clk.Now, Client: &flakyDoer{breakIssue: true}})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	a, err := client.Start(context.Background(), StartRequest{Access: credentials.AccessWrite})
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	_, err = client.Wait(context.Background(), a)
	if !errors.Is(err, ErrBrokenAnswer) || errors.Is(err, ErrCodeExpired) || transient(err) ||
		!strings.Contains(err.Error(), "Tokens page") || strings.Contains(err.Error(), testToken) {
		t.Fatalf("Wait() = %v, want ErrBrokenAnswer pointing at the Tokens page", err)
	}
	if site.Polls() != 1 {
		t.Fatalf("polls = %d, want 1: a claimed code is never polled again", site.Polls())
	}
}

// pollDoer answers every token poll with poll and forwards the rest to
// the real client.
type pollDoer struct {
	poll func(*http.Request) (*http.Response, error)
}

func (d pollDoer) Do(req *http.Request) (*http.Response, error) {
	if strings.HasSuffix(req.URL.Path, tokenPath) {
		return d.poll(req)
	}
	return http.DefaultClient.Do(req)
}

func TestWaitHintsAtATokenItNeverGot(t *testing.T) {
	ok := func(body string) func(*http.Request) (*http.Response, error) {
		return func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
		}
	}
	tests := []struct {
		name   string
		poll   func(cancel context.CancelFunc) func(*http.Request) (*http.Response, error)
		wantIs error
	}{
		{"an undecodable success", func(context.CancelFunc) func(*http.Request) (*http.Response, error) { return ok(`{"token":`) }, ErrProtocol},
		{"an oversize success", func(context.CancelFunc) func(*http.Request) (*http.Response, error) {
			return ok(`{"token":"` + strings.Repeat("x", bodyLimit) + `"}`)
		}, ErrProtocol},
		{"an unsendable token", func(context.CancelFunc) func(*http.Request) (*http.Response, error) {
			return ok(`{"token":"sld bad","space":"notes"}`)
		}, ErrProtocol},
		{"cancelled while the poll is in flight", func(cancel context.CancelFunc) func(*http.Request) (*http.Response, error) {
			return func(req *http.Request) (*http.Response, error) {
				cancel()
				return nil, req.Context().Err()
			}
		}, context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			site := sitetest.Start(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			clk := &clock{now: time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)}
			client, err := New(Config{Site: site.URL(), Sleep: clk.Sleep, Now: clk.Now, Client: pollDoer{poll: tt.poll(cancel)}})
			if err != nil {
				t.Fatalf("New() = %v", err)
			}
			a, err := client.Start(ctx, StartRequest{Access: credentials.AccessWrite})
			if err != nil {
				t.Fatalf("Start() = %v", err)
			}
			_, err = client.Wait(ctx, a)
			if !errors.Is(err, tt.wantIs) || !strings.Contains(err.Error(), TokenHint) || strings.Contains(err.Error(), "sld bad") {
				t.Fatalf("Wait() = %v, want %v with the Tokens page hint", err, tt.wantIs)
			}
		})
	}
	t.Run("cancelled before a poll is sent", func(t *testing.T) {
		site := sitetest.Start(t)
		client, _ := newClient(t, site.URL())
		a, err := client.Start(context.Background(), StartRequest{Access: credentials.AccessWrite})
		if err != nil {
			t.Fatalf("Start() = %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := client.Wait(ctx, a); !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), TokenHint) {
			t.Fatalf("Wait() = %v, want a plain cancellation: no poll was sent", err)
		}
	})
}

func TestWaitReportsTheLastFailureAtExpiry(t *testing.T) {
	site := sitetest.Start(t)
	site.SetTiming(5, 60)
	client, clk := flakyClient(t, site.URL(), 1000)
	a, err := client.Start(context.Background(), StartRequest{Access: credentials.AccessWrite})
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	_, err = client.Wait(context.Background(), a)
	if !errors.Is(err, ErrCodeExpired) || !strings.Contains(err.Error(), "HTTP 502") || !strings.Contains(err.Error(), TokenHint) {
		t.Fatalf("Wait() = %v, want ErrCodeExpired naming the last failure, with the hint", err)
	}
	if !clk.now.Equal(a.Deadline) {
		t.Fatalf("Wait() slept until %v, want to stop at the deadline %v", clk.now, a.Deadline)
	}
}

func TestWaitHintsWhenStoppedAfterALostPoll(t *testing.T) {
	site := sitetest.Start(t)
	site.Next(sitetest.Script{Issue: issue()})
	sleeps := 0
	stop := func(ctx context.Context, _ time.Duration) error {
		sleeps++
		if sleeps > 1 {
			return context.Canceled
		}
		return nil
	}
	client, err := New(Config{Site: site.URL(), Sleep: stop, Client: &flakyDoer{failures: 1}})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	a, err := client.Start(context.Background(), StartRequest{Access: credentials.AccessWrite})
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	_, err = client.Wait(context.Background(), a)
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), TokenHint) {
		t.Fatalf("Wait() = %v, want the stop with the hint: the unanswered poll may have claimed the code", err)
	}
}

func TestWaitHintsAtExpiryAfterALostPoll(t *testing.T) {
	site := sitetest.Start(t)
	site.Next(sitetest.Script{Final: "expired_token"})
	client, _ := flakyClient(t, site.URL(), 2)
	a, err := client.Start(context.Background(), StartRequest{Access: credentials.AccessWrite})
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	_, err = client.Wait(context.Background(), a)
	if !errors.Is(err, ErrCodeExpired) || !strings.Contains(err.Error(), TokenHint) {
		t.Fatalf("Wait() = %v, want expired_token with the hint: an unanswered poll may have claimed the code", err)
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
	good := `"deviceCode":"dev","userCode":"BCDF-GHJK","verificationUri":"SITE/cli/login","verificationUriComplete":"SITE/cli/login#BCDF-GHJK"`
	withCode := func(code, complete string) string {
		return `{"deviceCode":"dev","userCode":"` + code + `","verificationUri":"SITE/cli/login","verificationUriComplete":"` + complete + `","interval":5,"expiresIn":600}`
	}
	tests := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"not json", http.StatusOK, `nope`, ErrProtocol},
		{"no device code", http.StatusOK, `{"userCode":"BCDF-GHJK","verificationUri":"SITE/cli/login","verificationUriComplete":"SITE/cli/login#BCDF-GHJK","interval":5,"expiresIn":600}`, ErrProtocol},
		{"control characters in the code", http.StatusOK, withCode(`\u001b[31m`, "SITE/cli/login#BCDF-GHJK"), ErrProtocol},
		{"a vowel in the code", http.StatusOK, withCode("BCDA-GHJK", "SITE/cli/login#BCDA-GHJK"), ErrProtocol},
		{"a lower-case code", http.StatusOK, withCode("bcdf-ghjk", "SITE/cli/login#bcdf-ghjk"), ErrProtocol},
		{"a code without its dash", http.StatusOK, withCode("BCDFGHJK", "SITE/cli/login#BCDFGHJK"), ErrProtocol},
		{"complete page on another host", http.StatusOK, withCode("BCDF-GHJK", "https://evil.example.test/cli/login#BCDF-GHJK"), ErrProtocol},
		{"complete page with another scheme", http.StatusOK, withCode("BCDF-GHJK", "file:///etc/passwd"), ErrProtocol},
		{"the code in a query", http.StatusOK, withCode("BCDF-GHJK", "SITE/cli/login?code=BCDF-GHJK"), ErrProtocol},
		{"another code in the page", http.StatusOK, withCode("BCDF-GHJK", "SITE/cli/login#GHJK-BCDF"), ErrProtocol},
		{"another path", http.StatusOK, withCode("BCDF-GHJK", "SITE/approve#BCDF-GHJK"), ErrProtocol},
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

func TestStartDefaultsTheIntervalAndBoundsTheLifetime(t *testing.T) {
	good := `"deviceCode":"dev","userCode":"BCDF-GHJK","verificationUri":"SITE/ignored","verificationUriComplete":"SITE/cli/login#BCDF-GHJK"`
	tests := []struct {
		name     string
		tail     string
		interval time.Duration
		lifetime time.Duration
	}{
		{"no interval", `"expiresIn":600`, 5 * time.Second, 10 * time.Minute},
		{"a zero interval", `"interval":0,"expiresIn":600`, time.Second, 10 * time.Minute},
		{"a day-long lifetime", `"interval":2,"expiresIn":86400`, 2 * time.Second, 30 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			site := answer(t, http.StatusOK, `{`+good+`,`+tt.tail+`}`)
			client, clk := newClient(t, site)
			a, err := client.Start(context.Background(), StartRequest{Access: credentials.AccessWrite})
			if err != nil {
				t.Fatalf("Start() = %v", err)
			}
			if a.Interval != tt.interval || !a.Deadline.Equal(clk.now.Add(tt.lifetime)) {
				t.Fatalf("approval timing = %v until %v, want %v for %v", a.Interval, a.Deadline, tt.interval, tt.lifetime)
			}
			// The pages are built locally; the site's verificationUri is
			// never shown.
			if a.URI != site+"/cli/login" || a.CompleteURI != site+"/cli/login#BCDF-GHJK" {
				t.Fatalf("approval pages = %q, %q", a.URI, a.CompleteURI)
			}
		})
	}
}

// recordingSite answers start and token like the contract and records the
// content type and body size of every request.
type recordingSite struct {
	mu       sync.Mutex
	requests []string
}

func (r *recordingSite) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	r.requests = append(r.requests, fmt.Sprintf("%s %s %d", req.URL.Path, req.Header.Get("Content-Type"), len(body)))
	r.mu.Unlock()
	switch req.URL.Path {
	case startPath:
		fmt.Fprintf(w, `{"deviceCode":"dev","userCode":"BCDF-GHJK","verificationUri":"http://%[1]s/cli/login","verificationUriComplete":"http://%[1]s/cli/login#BCDF-GHJK","interval":1,"expiresIn":60}`, req.Host)
	default:
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"access_denied"}`)
	}
}

func TestRequestsAreSmallJSON(t *testing.T) {
	rec := &recordingSite{}
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	client, _ := newClient(t, srv.URL)
	a, err := client.Start(context.Background(), StartRequest{Access: credentials.AccessWrite, Client: strings.Repeat("h", 500)})
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	if _, err := client.Wait(context.Background(), a); !errors.Is(err, ErrDenied) {
		t.Fatalf("Wait() = %v, want ErrDenied", err)
	}
	if len(rec.requests) != 2 {
		t.Fatalf("requests = %v, want a start and a poll", rec.requests)
	}
	for _, r := range rec.requests {
		var path, ctype string
		var size int
		if _, err := fmt.Sscanf(r, "%s %s %d", &path, &ctype, &size); err != nil {
			t.Fatalf("record %q: %v", r, err)
		}
		if ctype != "application/json" || size == 0 || size >= requestLimit {
			t.Fatalf("%s sent %s with %d bytes, want application/json under %d", path, ctype, size, requestLimit)
		}
	}
	huge, _ := newClient(t, srv.URL)
	if _, err := huge.Start(context.Background(), StartRequest{Access: credentials.Access(strings.Repeat("s", requestLimit))}); err == nil ||
		!strings.Contains(err.Error(), "accepts less than") {
		t.Fatalf("Start() with an oversize body = %v, want a refusal before sending", err)
	}
	if len(rec.requests) != 2 {
		t.Fatal("an oversize request was sent")
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
	site.Issued(testToken, endpoint)
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
	for _, body := range []string{`{"error":"unauthorized"}`, ``} {
		other, _ := newClient(t, answer(t, http.StatusUnauthorized, body))
		if err := other.Revoke(context.Background(), testToken); !errors.Is(err, ErrRefused) {
			t.Fatalf("Revoke() answered 401 %q = %v, want a failure: only invalid_token means already revoked", body, err)
		}
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

// TestReferenceSiteRefusesUnknownFields proves the reference site reads
// request bodies as strictly as the real one: a field outside a route's
// body, a key spelled in another case, a null, a value of another JSON
// type, or data after the object is 400 invalid_request. The client tests
// over it therefore prove the client sends nothing else. A token poll
// whose deviceCode is missing or unknown is a poll answer, expired_token.
func TestReferenceSiteRefusesUnknownFields(t *testing.T) {
	site := sitetest.Start(t)
	site.Issued(testToken, endpoint)
	site.SetSpaces(testToken, sitetest.Space{Name: "notes", Owner: "ada@example.test", Access: "write"})
	for _, row := range []struct{ path, body, want string }{
		{"/cli/v1/start", `{"access":"write","client":"laptop","space":"notes"}`, "invalid_request"},
		{"/cli/v1/start", `{"access":"write","client":5}`, "invalid_request"},
		{"/cli/v1/start", `{"Access":"write"}`, "invalid_request"},
		{"/cli/v1/start", `{"access":"write"} junk`, "invalid_request"},
		{"/cli/v1/start", `{"access":"write"}{}`, "invalid_request"},
		{"/cli/v1/start", `[]`, "invalid_request"},
		{"/cli/v1/token", `{"deviceCode":"x","extra":1}`, "invalid_request"},
		{"/cli/v1/token", `{"DeviceCode":"x"}`, "invalid_request"},
		{"/cli/v1/token", `{}`, "expired_token"},
		{"/cli/v1/token", `{"deviceCode":"unknown"}`, "expired_token"},
		{"/cli/v1/space-token", `{"space":"notes","access":"write","extra":1}`, "invalid_request"},
		{"/cli/v1/space-token", `{"space":"notes","access":"admin"}`, "invalid_request"},
		{"/cli/v1/space-token", `{"space":"notes","access":null}`, "invalid_request"},
	} {
		req, err := http.NewRequest(http.MethodPost, site.URL()+row.path, strings.NewReader(row.body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+testToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("POST %s %s: read the answer: %v", row.path, row.body, err)
		}
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(data), `"error":"`+row.want+`"`) {
			t.Fatalf("POST %s %s = %d %s, want 400 %s", row.path, row.body, resp.StatusCode, data, row.want)
		}
	}
}
