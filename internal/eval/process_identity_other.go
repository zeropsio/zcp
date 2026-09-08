//go:build !linux

package eval

import (
	"context"
	"time"
)

// observeProcessIdentity on non-Linux platforms cannot read /proc, so §10.4
// classifies every poll as unsupported: "on macOS it reports 'Process
// identity: blocked (unsupported OS)'". The caller (pollProcessIdentity)
// de-duplicates repeated unsupported observations.
func observeProcessIdentity(_ context.Context, _, _, _ string, _ map[int]bool) []ProcessIdentity {
	return []ProcessIdentity{{Classification: processIdentityUnsupported, ObservedAt: time.Now().UTC()}}
}
