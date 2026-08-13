package notebook

import (
	"context"
	"math/rand/v2"
	"time"
)

// BackoffWaiter waits between CAS retries. The notebook resolves the wait
// policy; tests inject a deterministic waiter so retry bounds are exact.
type BackoffWaiter interface {
	Wait(ctx context.Context, attempt int) error
}

// exponentialBackoff is the bounded full-jitter backoff of architecture
// section 11.2: the wait ceiling doubles each retry from base up to max,
// and each wait is uniform in [0, ceiling), so a zero wait is possible and
// valid.
type exponentialBackoff struct {
	base time.Duration
	max  time.Duration
}

func newExponentialBackoff(base, max time.Duration) *exponentialBackoff {
	return &exponentialBackoff{base: base, max: max}
}

// Wait sleeps a full-jitter backoff interval for the given retry attempt
// (1-based) or returns the context error.
func (b *exponentialBackoff) Wait(ctx context.Context, attempt int) error {
	ceiling := b.base
	for i := 1; i < attempt && ceiling < b.max; i++ {
		ceiling *= 2
	}
	if ceiling > b.max || ceiling <= 0 {
		ceiling = b.max
	}
	wait := time.Duration(rand.N(int64(ceiling)))
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
