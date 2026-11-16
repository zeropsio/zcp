package ops

import (
	"context"
	"errors"
)

// EnsureBearer is the one writer of a pool's bearer reference on this zcp
// service, for an acquire (or the stand-up) that finds no usable bearer: it
// writes the reference under its own lock, waits a bounded time for it to
// resolve in the live env and returns the bearer. On a reference that does not
// resolve in time it fails naming the variable. The read sources (liveBearer,
// the pool's env) stay as they are.
func EnsureBearer(ctx context.Context, d LeaseDeps, pool string) (string, error) {
	return "", errors.New("not implemented")
}
