package tools

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/platform/watch"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// standupBootRead bounds the boot's first look: the live services and the
// HQ tier. Past it the boot leaves the stand-up to today's path.
const standupBootRead = 15 * time.Second

// standupBootBound bounds the boot's run; a row still building at it is
// failed as unreachable, naming the bound, and the agent's zerops_standup
// waits on what is in flight.
const standupBootBound = 20 * time.Minute

// StandupAtBoot is the stand-up zcp's Mate service runs at its start, before
// the server it supervises launches (internal/service mateLaunchSetup). Under
// standupBootRead it reads the live services and the HQ tier. Where a pooled
// part has no build — a pool or a stage of it with no active version, whatever
// earlier versions failed or were cancelled — it takes the
// project's one stand-up, writes the verdict with every pooled row building,
// and returns the rest of the run: adopt, ensure each pool's bearer,
// commit-deploy main to every pool and stage without a build, the verdict
// written as it goes. Otherwise — a classic tier, nothing to build, a stand-up
// already running, or a read that failed or ran out — it writes nothing,
// says why in one line, and returns nil, and today's path applies. env is the container's live env
// store as it is at each call (as KeepMateDevGit takes it); each line said
// goes to log.
func StandupAtBoot(ctx context.Context, env func() func(string) string, statusPath string, log func(string)) (run func()) {
	lookup := env()
	rt := runtime.DetectFrom(lookup)
	if !rt.InContainer || !rt.MateEnabled {
		return nil
	}
	key := lookup(hq.EnvKey)
	if key == "" {
		log(fmt.Sprintf("the stand-up at boot did not run: %s is not in zcp's environment", hq.EnvKey))
		return nil
	}
	readCtx, cancel := context.WithTimeout(ctx, standupBootRead)
	defer cancel()
	apiHost := mate.ResolveAPIHost(lookup("ZCP_API_HOST"))
	client, err := platform.NewProjectClient(readCtx, key, apiHost, rt.ProjectID)
	var orgID string
	if err == nil {
		orgID, err = client.OrgID(readCtx)
	}
	if err != nil {
		log(fmt.Sprintf("the stand-up at boot did not run: the platform client: %v", err))
		return nil
	}
	rt.OrgID = orgID
	httpClient := &http.Client{Timeout: hqCallTimeout}
	d := standupDeps{
		batch: batchDeployer{
			client: client, httpClient: httpClient, projectID: rt.ProjectID,
			sshDeployer: platform.NewSystemSSHDeployer(),
			rtInfo:      rt, stateDir: runtime.ContainerStateDir,
		},
		identity:    func() hq.Identity { return hqIdentity(rt) },
		runtimeWait: standupRuntimeWait,
		pushes:      watch.ProjectPushes(apiHost, func() string { return key }, orgID),
		statusPath:  statusPath,
		status:      newStandupStatus(statusPath),
		source:      ops.NewHQDeploySource(httpClient, hqIdentity(rt)),
		runBound:    standupBootBound,
	}
	progress := newStandupProgress(func(message string, _, _ float64) { log("stand-up: " + message) })
	progress.status = d.status
	return d.bootRun(ctx, readCtx, progress)
}

// bootRun is the boot's first look under readCtx and, where a pooled part
// has no build, the rest of the run under ctx; nil where there is nothing
// for it. The section is begun only then: a classic Mate's is never touched.
func (d standupDeps) bootRun(ctx, readCtx context.Context, progress *standupProgress) func() {
	live, err := d.liveServices(readCtx)
	if err != nil {
		progress.say(fmt.Sprintf("did not run: %v", err))
		return nil
	}
	if !projectHasPool(live) {
		return nil
	}
	hqc, src, stop := d.openTier(readCtx, progress)
	if stop != nil {
		progress.say("did not run: " + stop.message)
		return nil
	}
	// A tier with a classic pair keeps today's path whole: the agent's
	// zerops_standup stands every pair up.
	for _, p := range src.tier.Pairs {
		if !p.DevPool {
			progress.say("did not run: the tier has a classic pair, which the agent's zerops_standup stands up")
			return nil
		}
	}
	tier := src.tier
	if !pooledPartToBuild(tier, live) {
		progress.say("did not run: every pooled service has a build")
		return nil
	}
	release, held, err := holdStandup(d.batch.stateDir)
	switch {
	case err != nil:
		progress.say(fmt.Sprintf("did not run: %v", err))
		return nil
	case !held:
		progress.say(runningStandupLine(d.statusPath))
		return nil
	}
	progress.st().begin()
	rows := d.startPooledRows(readCtx, tier)
	return func() {
		defer release()
		ctx, cancel := context.WithTimeout(ctx, d.runBound)
		defer cancel()
		d.session = newStandupPushes(d.pushes)
		pairs, live := d.preparePairs(ctx, hqc, src, progress)
		outcome := d.buildPooled(ctx, hqc, pairs, live, rows, progress)
		progress.st().end("")
		progress.say(fmt.Sprintf("ended %s; %s", outcome.verdict.Headline, standupPooledNext(outcome)))
	}
}

// pooledPartToBuild reports a pooled part of the tier whose pool or a stage
// the boot builds: not created yet, or with no build — no active version with
// code, whatever versions failed, were cancelled or are in flight (a build in
// flight is joined, never built again: ops.CommitDeploy).
func pooledPartToBuild(tier workflow.MateTier, live map[string]*platform.ServiceStack) bool {
	for _, p := range tier.Pairs {
		for _, rt := range append([]workflow.MateTierRuntime{p.Dev}, p.Stages...) {
			if svc := live[rt.Hostname]; svc == nil || !svc.HasDeployedCode() {
				return true
			}
		}
	}
	return false
}
