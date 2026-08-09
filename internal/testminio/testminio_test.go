package testminio

import (
	"errors"
	"fmt"
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

// TestRequireReturnsTheAvailableSuite proves the policy is not simply
// always-fail: a started suite passes through untouched.
func TestRequireReturnsTheAvailableSuite(t *testing.T) {
	t.Parallel()
	want := &Suite{Endpoint: "http://127.0.0.1:9000"}
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
