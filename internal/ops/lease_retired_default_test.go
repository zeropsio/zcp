package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestLeaseAcquire_ADefaultRecordedOnHQ_IsNoHostForThePoolsToken (D8, live
// 2026-10-27): a pool's default repository recorded while it was on the
// Mate's HQ is an HQ repository — its host never was the person's token's.
// Once the Mate links to another HQ, an acquire naming no repository clones
// that address, and git asks for a credential there: that is no missing or
// refused person's token (DP-6), so the refusal is PREREQUISITE_MISSING and
// asks for none, and the token is never answered to that host.
func TestLeaseAcquire_ADefaultRecordedOnHQ_IsNoHostForThePoolsToken(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, out string
	}{
		{name: "git asked for a credential", out: "fatal: could not read Username for 'https://hq.example.invalid': terminal prompts disabled"},
		{name: "the host refused one", out: "fatal: Authentication failed for '" + leaseHQRepo + "/'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			inAMate(fx)
			fx.deps.SSH = &leaseSSH{}
			fx.swarm.acquired = acquiredAs(3)
			if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, leaseHQRepo, "feat"); err != nil {
				t.Fatalf("the first acquire, of the HQ repository: %v", err)
			}
			if got := readPoolDefaultRepo(fx.stateDir, testPoolHost); got != leaseHQRepo {
				t.Fatalf("the pool's default = %q, want the HQ repository", got)
			}
			// The Mate links to another HQ now.
			fx.deps.HQAddress = func() string { return "https://hq-now.example.invalid" }
			fx.swarm.acquired = acquiredAs(4)
			fx.deps.SSH = &leaseSSH{fn: func(_, command string) ([]byte, error) {
				if !strings.Contains(command, "fetch -q --prune origin") {
					return nil, nil
				}
				return []byte(tc.out), sshFailure(tc.out, exitError(t, 128))
			}}

			_, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, "", "other")

			var pe *platform.PlatformError
			if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing {
				t.Fatalf("err = %v (code %q), want PREREQUISITE_MISSING, never a token code", err, codeOf(err))
			}
			if !strings.Contains(pe.Suggestion, "Never ask") {
				t.Errorf("suggestion %q: want it to ask for no token", pe.Suggestion)
			}
			if scope, _ := leaseTokenScope(fx.stateDir, testPoolHost, fx.deps.hqAddress(), leaseHQRepo); scope != "" {
				t.Errorf("the pool's token answers %q, the host of a repository recorded on HQ", scope)
			}
		})
	}
}

// codeOf is err's platform error code, "" for none.
func codeOf(err error) string {
	var pe *platform.PlatformError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}
