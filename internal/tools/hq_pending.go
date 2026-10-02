package tools

import (
	"context"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A delivery HQ did not refuse but could not be reached for is owed: its
// work is committed in the pair's checkout and recorded as pending
// (ServiceMeta.HQ.Pending). SPEC §6.2.3 promises it finishes after an HQ
// outage with nobody asking, and an agent's tool calls may never come — so
// the `zcp service mate` process, which runs as long as the Mate does, keeps
// finishing what is owed (KeepFinishingDeliveries) beside the Mate's
// enrollment. A delivery, a push or a reconcile pass of an agent's zcp
// finishes it too, whichever comes first.

// DeliveryKeepOptions paces KeepFinishingDeliveries; a zero field takes its
// default.
type DeliveryKeepOptions struct {
	// Idle is the wait while nothing is owed: a delivery an agent's zcp
	// records pending is picked up within it. 30 s.
	Idle time.Duration
	// Retry is the wait after a round that left a delivery owed, doubling up
	// to RetryMax; 5 s and 5 min.
	Retry, RetryMax time.Duration
	// Log, when set, hears what a round says whenever it differs from what
	// the round before said.
	Log func(string)
}

func (o DeliveryKeepOptions) withDefaults() DeliveryKeepOptions {
	if o.Idle == 0 {
		o.Idle = 30 * time.Second
	}
	if o.Retry == 0 {
		o.Retry = 5 * time.Second
	}
	if o.RetryMax == 0 {
		o.RetryMax = 5 * time.Minute
	}
	return o
}

// DeliveryRound is one round of finishing what is owed: how many deliveries
// are still owed after it, and what it has to say.
type DeliveryRound func(ctx context.Context) (owed int, lines []string)

// KeepFinishingDeliveries runs round until ctx ends: on the idle wait while
// nothing is owed, and on a growing wait, capped, while a delivery stays owed
// — HQ not answering, or a checkout not ready to take main in. The growth
// starts afresh once nothing is owed.
func KeepFinishingDeliveries(ctx context.Context, round DeliveryRound, opts DeliveryKeepOptions) {
	keepFinishingDeliveries(ctx, round, opts, waitFor)
}

func keepFinishingDeliveries(ctx context.Context, round DeliveryRound, opts DeliveryKeepOptions, pause func(context.Context, time.Duration) error) {
	opts = opts.withDefaults()
	retry := opts.Retry
	said := ""
	for {
		owed, lines := round(ctx)
		if line := strings.Join(lines, "; "); line != said {
			if line != "" && opts.Log != nil {
				opts.Log(line)
			}
			said = line
		}
		wait := opts.Idle
		if owed > 0 {
			wait = retry
			retry = min(2*retry, opts.RetryMax)
		} else {
			retry = opts.Retry
		}
		if pause(ctx, wait) != nil {
			return
		}
	}
}

// waitFor waits d, or until ctx ends.
func waitFor(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// FinishPendingDeliveries is one round over the pairs in stateDir: every
// wired pair with a delivery owed is kept current the way a reconcile pass
// keeps it — wired again where HQ now holds the Mate, its change's outcome
// learned, its owed delivery finished on a clean checkout of the Mate's
// branch. It answers how many are still owed, and the lines worth saying;
// HQ not answering says nothing.
func FinishPendingDeliveries(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir string,
) (owed int, lines []string) {
	if !rt.InContainer || sshDeployer == nil {
		return 0, nil
	}
	hqc, enrolled := openHQ(httpClient)
	if !enrolled {
		return 0, nil
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		return 0, nil
	}
	self := lazySelf(ctx, hqc)
	for _, m := range metas {
		if !hqPairWired(m) || m.HQ.Pending == nil {
			continue
		}
		if line, _ := keepHQPairCurrent(ctx, client, httpClient, sshDeployer, rt, stateDir, hqc, self, m); line != "" {
			lines = append(lines, m.Hostname+": "+line)
		}
		if fresh, _ := workflow.FindServiceMeta(stateDir, m.Hostname); hqPairWired(fresh) && fresh.HQ.Pending != nil {
			owed++
		}
	}
	return owed, lines
}
