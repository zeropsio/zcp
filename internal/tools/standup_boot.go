package tools

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/platform/watch"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
)

// StandupAtBoot is the stand-up zcp's Mate service runs at its start, beside
// the server it supervises (internal/service mateLaunchSetup), where a pooled
// part has no build: adopt, ensure each pool's bearer, commit-deploy main to
// every pool and stage without a build, and write the verdict to the status
// file at statusPath. It writes rows for pooled parts only; with none it
// returns at once. env is the container's live env store as it is at each
// call (as KeepMateDevGit takes it); each line said goes to log.
func StandupAtBoot(ctx context.Context, env func() func(string) string, statusPath string, log func(string)) {
	lookup := env()
	rt := runtime.DetectFrom(lookup)
	if !rt.InContainer || !rt.MateEnabled {
		return
	}
	key := lookup(hq.EnvKey)
	if key == "" {
		log(fmt.Sprintf("the stand-up at boot did not run: %s is not in zcp's environment", hq.EnvKey))
		return
	}
	apiHost := mate.ResolveAPIHost(lookup("ZCP_API_HOST"))
	readCtx, cancel := context.WithTimeout(ctx, platform.DefaultAPITimeout)
	client, err := platform.NewProjectClient(readCtx, key, apiHost, rt.ProjectID)
	var orgID string
	if err == nil {
		orgID, err = client.OrgID(readCtx)
	}
	cancel()
	if err != nil {
		log(fmt.Sprintf("the stand-up at boot did not run: the platform client: %v", err))
		return
	}
	rt.OrgID = orgID
	httpClient := &http.Client{Timeout: hqCallTimeout}
	d := standupDeps{
		batch: batchDeployer{
			client: client, httpClient: httpClient, projectID: rt.ProjectID,
			sshDeployer: platform.NewSystemSSHDeployer(), logFetcher: platform.NewLogFetcher(),
			rtInfo: rt, stateDir: filepath.Join(topology.CodeMountRoot, ".zcp", "state"),
		},
		identity:    func() hq.Identity { return hqIdentity(rt) },
		runtimeWait: standupRuntimeWait,
		pushes:      watch.ProjectPushes(apiHost, func() string { return key }, orgID),
		statusPath:  statusPath,
		status:      newStandupStatus(statusPath),
		source:      ops.NewHQDeploySource(httpClient, hqIdentity(rt)),
	}
	progress := newStandupProgress(func(message string, _, _ float64) { log("stand-up: " + message) })
	progress.status = d.status
	d.runAtBoot(ctx, progress)
}

// runAtBoot is the pooled path alone, for a project that holds a Swarm pool:
// one run per project (a run already going is reported, never joined), the
// section begun only once the tier names a pooled part — a classic Mate's
// section is never touched — or, when the tier cannot be read, only where a
// pool has no build, the case the boot run is for.
func (d standupDeps) runAtBoot(ctx context.Context, progress *standupProgress) {
	live, err := d.liveServices(ctx)
	if err != nil {
		progress.say(fmt.Sprintf("did not run: %v", err))
		return
	}
	if !projectHasPool(live) {
		return
	}
	release, held, err := holdStandup(d.batch.stateDir)
	switch {
	case err != nil:
		progress.say(fmt.Sprintf("did not run: %v", err))
		return
	case !held:
		progress.say(runningStandupLine(d.statusPath))
		return
	}
	defer release()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	d.session = newStandupPushes(d.pushes)

	hqc, src, stop := d.openTier(ctx, progress)
	if stop != nil {
		progress.say(stop.message)
		if poolWithoutBuild(live) {
			progress.st().begin()
			progress.st().end(stop.message)
		}
		return
	}
	tier := src.tier
	tier.Pairs = nil
	for _, p := range src.tier.Pairs {
		if p.DevPool {
			tier.Pairs = append(tier.Pairs, p)
		}
	}
	if len(tier.Pairs) == 0 {
		return
	}
	src.tier = tier
	progress.st().begin()
	rows := d.startPooledRows(ctx, tier)
	pairs, live := d.preparePairs(ctx, hqc, src, progress)
	outcome := d.buildPooled(ctx, hqc, pairs, live, rows, progress)
	progress.st().end("")
	progress.say(fmt.Sprintf("ended %s; %s", outcome.verdict.Headline, standupPooledNext(outcome)))
}

// poolWithoutBuild reports a Swarm pool of the project with no build.
func poolWithoutBuild(live map[string]*platform.ServiceStack) bool {
	for _, svc := range live {
		if topology.IsPoolType(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) && !svc.HasDeployedCode() {
			return true
		}
	}
	return false
}
