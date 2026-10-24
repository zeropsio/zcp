// Tests for: tools/guard.go — the leaseVouch the tools that reach a lease's
// container take (docs/spec-workflows.md §8 DP-2), and the stand-in the tests
// that drive a held lease give them.
package tools

import (
	"context"

	"github.com/zeropsio/zcp/internal/ops"
)

// vouchHeld is a leaseVouch whose orchestrator gives every lease's container
// to zcp, as its record reads: what the tests that drive a held lease need.
func vouchHeld(_ context.Context, lease ops.LeaseTargetInfo, _ string) (ops.LeaseTargetInfo, error) {
	return lease, nil
}

// withVouchHeld gives the deploy tools vouchHeld.
func withVouchHeld() DeployOption { return func(c *deployConfig) { c.vouch = vouchHeld } }

// mountVouchHeld gives zerops_mount vouchHeld.
func mountVouchHeld() MountOption { return func(c *mountConfig) { c.vouch = vouchHeld } }

// verifyVouchHeld gives zerops_verify vouchHeld.
func verifyVouchHeld() VerifyOption { return func(c *verifyConfig) { c.vouch = vouchHeld } }
