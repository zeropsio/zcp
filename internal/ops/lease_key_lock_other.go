//go:build !unix

package ops

import (
	"context"
	"time"
)

// lockLeaseKey is a no-op on non-Unix platforms, like the alias lock: a lease
// needs the container side (sudo, sshfs) only Unix has.
func lockLeaseKey(context.Context, string) (func(), error) {
	return func() {}, nil
}

// lockLeaseFileWithin is a no-op on non-Unix platforms, like lockLeaseKey.
func lockLeaseFileWithin(string, time.Duration) (func(), error) {
	return func() {}, nil
}
