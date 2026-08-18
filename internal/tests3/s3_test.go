package tests3

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

// recorder captures the failure a policy decision produces without ending
// the enclosing test.
type recorder struct {
	helpers int
	fatal   string
	failed  bool
}

func (r *recorder) Helper() { r.helpers++ }

func (r *recorder) Fatalf(format string, args ...any) {
	r.failed = true
	r.fatal = fmt.Sprintf(format, args...)
}

// TestRequireFailsWhenDockerIsUnavailable pins the rule that replaced the
// old skip-detecting wrapper around the integration run: there is one test
// command, and it cannot report success while the storage protocol went
// unexercised. An unreachable daemon is a failure carrying its cause, never
// a skip.
func TestRequireFailsWhenDockerIsUnavailable(t *testing.T) {
	t.Parallel()
	cause := errors.New("docker daemon: connection refused")
	rec := &recorder{}
	if got := require(rec, nil, fmt.Errorf("docker unavailable: %w", cause)); got != nil {
		t.Fatalf("require() = %v, want nil when the suite is unavailable", got)
	}
	if !rec.failed {
		t.Fatal("an unavailable Docker daemon did not fail the test")
	}
	if !strings.Contains(rec.fatal, cause.Error()) {
		t.Fatalf("failure = %q, want the underlying cause", rec.fatal)
	}
	if !strings.Contains(rec.fatal, "Docker is required") {
		t.Fatalf("failure = %q, want an actionable diagnostic", rec.fatal)
	}
}

// TestRequireReturnsTheAvailableSuite proves the policy is more than
// always-fail: a started suite passes through untouched.
func TestRequireReturnsTheAvailableSuite(t *testing.T) {
	t.Parallel()
	want := &Suite{Endpoint: "http://127.0.0.1:8333"}
	rec := &recorder{}
	if got := require(rec, want, nil); got != want {
		t.Fatalf("require() = %v, want the started suite", got)
	}
	if rec.failed {
		t.Fatalf("an available suite failed the test: %s", rec.fatal)
	}
	if rec.helpers == 0 {
		t.Fatal("require() did not mark itself a test helper")
	}
}

func TestAttachAcceptsLoopbackEndpoint(t *testing.T) {
	t.Parallel()
	for _, endpoint := range []string{
		"http://127.0.0.1:8333",
		"http://localhost:4567",
		"http://[::1]:8333",
	} {
		t.Run(endpoint, func(t *testing.T) {
			t.Parallel()
			suite, err := attach(endpoint)
			if err != nil {
				t.Fatalf("attach(%q) = %v", endpoint, err)
			}
			if suite.Endpoint != endpoint {
				t.Fatalf("endpoint = %q, want %q", suite.Endpoint, endpoint)
			}
			if suite.Raw == nil {
				t.Fatal("attached suite has no raw S3 client")
			}
			if suite.ctr != nil {
				t.Fatal("attached suite owns a container")
			}
		})
	}
}

func TestAttachRejectsNonLoopbackEndpoint(t *testing.T) {
	t.Parallel()
	for _, endpoint := range []string{
		"https://127.0.0.1:8333",
		"http://127.0.0.1",
		"http://s3.example.com:8333",
		"http://192.0.2.1:8333",
		"http://user:pass@127.0.0.1:8333",
		"http://127.0.0.1:8333/prefix",
		"http://127.0.0.1:8333?query=value",
	} {
		t.Run(endpoint, func(t *testing.T) {
			t.Parallel()
			if _, err := attach(endpoint); err == nil {
				t.Fatalf("attach(%q) = nil, want a validation error", endpoint)
			}
		})
	}
}

func TestEndpointFromFile(t *testing.T) {
	t.Parallel()
	missing := t.TempDir() + "/missing"
	if endpoint, ready, err := endpointFromFile(missing); err != nil || ready || endpoint != "" {
		t.Fatalf("endpointFromFile(missing) = %q, %t, %v, want not-ready nil", endpoint, ready, err)
	}

	path := t.TempDir() + "/endpoint"
	if err := os.WriteFile(path, []byte("http://127.0.0.1:8333\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	endpoint, ready, err := endpointFromFile(path)
	if err != nil || !ready || endpoint != "http://127.0.0.1:8333" {
		t.Fatalf("endpointFromFile(ready) = %q, %t, %v", endpoint, ready, err)
	}

	if err := os.WriteFile(path, []byte("error: docker unavailable\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ready, err := endpointFromFile(path); err == nil || ready || !strings.Contains(err.Error(), "docker unavailable") {
		t.Fatalf("endpointFromFile(error) = ready %t, err %v", ready, err)
	}
}
