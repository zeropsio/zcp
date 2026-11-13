package tools

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A part's dev pool gets its first image in the stand-up. A lease is how a
// pool's containers are reached, and it needs the pool's API_TOKEN, which a
// Mate's own key reads masked and an env reference to it carries only once
// the pool has been deployed (ops.orchestratorFor): no lease of a pool works
// before it has an image, so its first image cannot come from one. It comes
// from an ordinary deploy instead, which takes no pool token: main checked
// out in the part's stage — brought up running and empty when the Mate's
// birth left it waiting for its first deploy — and the pool cross-deployed
// from that checkout with its own setup, beside the dev halves. Once it
// lands, the reference to the pool's token resolves in this container's live
// env and every lease of the pool works. The stage itself is deployed from a
// lease of its pool, as every later stage deploy is: a self-deploy from the
// checkout it holds is refused unless its setup deploys the whole tree
// (DM-2), and a stage's build seldom does.

// standupIsolationKeys are the settings the Mate's birth gives every service
// beside its tier entry, which a re-import replacing the service must carry.
var standupIsolationKeys = []string{"envIsolation", "sshIsolation"}

// openPoolStage readies a pool pair's stage to hold main for its pool's first
// image — nothing when the pool has an image already. A stage the birth left
// waiting for its first deploy is brought up empty (startPoolStage); one that
// runs code holds a build, never main, and is the model's to deal with.
func (d standupDeps) openPoolStage(ctx context.Context, src standupSource, sp *standupPair, live map[string]*platform.ServiceStack, progress *standupProgress) {
	p := sp.pair
	pool, stage := p.Dev.Hostname, p.FirstStage().Hostname
	if svc := live[pool]; svc != nil && svc.HasDeployedCode() {
		return
	}
	st := live[stage]
	switch {
	case st.HasDeployedCode():
		sp.failedHost = stage
		sp.fail(fmt.Sprintf("%s has no image yet, and its stage %s already runs a build: the stand-up checks main out only into a stage that runs no code, never over a build it serves", pool, stage),
			fmt.Sprintf("Give %s its first image from a container that holds main's checkout — the stage itself when its build deploys the whole repository (deployFiles [.]): zerops_deploy sourceService=%q targetService=%q setup=%q, %s",
				pool, stage, pool, p.Dev.Setup, standupAgain))
		return
	case !st.IsLive() && !d.startPoolStage(ctx, src, sp, st, progress):
		return
	}
	sp.seedsPool = true
}

// startPoolStage brings a pool pair's stage up running and empty by the
// platform's route for a runtime never deployed: a re-import with
// startWithoutCode and override (develop-ready-to-deploy). Override replaces
// the service, so the import is the stage's own tier entry with the
// isolation the live stage carries (workflow.MateTierStartEmpty), and
// nothing else is lost: the stage never held a build. A stage with any other
// history — stopped, or a deploy that failed — is never replaced: what failed
// is worth reading first. Reports whether the stage runs.
func (d standupDeps) startPoolStage(ctx context.Context, src standupSource, sp *standupPair, st *platform.ServiceStack, progress *standupProgress) bool {
	pool, stage := sp.pair.Dev.Hostname, st.Name
	stop := func(what, next string) bool {
		sp.failedHost = stage
		sp.fail(what, next)
		return false
	}
	again := "Call zerops_standup again; it continues from here."
	state, err := ops.ComputeRecoveryState(ctx, d.batch.client, nil, d.batch.projectID, stage, st.Status)
	if err != nil {
		return stop(fmt.Sprintf("could not read whether %s was ever deployed: %v", stage, err), again)
	}
	if state.Shape != topology.RecoveryFreshMisconfigured {
		return stop(fmt.Sprintf("%s is %s, and main is checked out into it for %s's first image: the stand-up brings up empty only a stage never deployed", stage, st.Status, pool),
			fmt.Sprintf("Read what happened to it with zerops_events serviceHostname=%q and start it (zerops_manage action=\"start\" serviceHostname=%q), %s", stage, stage, standupAgain))
	}
	envs, err := ops.FetchServiceEnv(ctx, d.batch.client, st.ID)
	if err != nil {
		return stop(fmt.Sprintf("could not read %s's settings before bringing it up empty: %v", stage, err), again)
	}
	scalars, _ := ops.EnvScalars(envs)
	isolation := map[string]string{}
	for _, key := range standupIsolationKeys {
		isolation[key] = scalars[key]
	}
	content, err := workflow.MateTierStartEmpty(src.importYAML, stage, isolation)
	if err != nil {
		return stop(fmt.Sprintf("could not shape %s's import from %s's %s: %v", stage, src.groupRepo, hq.RecipeTierPaths[hq.RecipeTierMate], err), again)
	}
	progress.say(fmt.Sprintf("bringing %s up empty for %s's first image", stage, pool))
	result, err := ops.Import(ctx, d.batch.client, d.batch.projectID, content, "", true)
	if err != nil {
		return stop(fmt.Sprintf("bringing %s up empty failed: %v", stage, err), again)
	}
	if len(result.ServiceErrors) > 0 {
		e := result.ServiceErrors[0]
		return stop(fmt.Sprintf("bringing %s up empty was refused: %s (%s)", stage, e.Message, e.Code), again)
	}
	ids := make([]string, 0, len(result.Processes))
	for _, proc := range result.Processes {
		ids = append(ids, proc.ProcessID)
	}
	if len(ids) == 0 {
		return true
	}
	waited, err := ops.WaitProcesses(ctx, d.batch.client, ids, progress.callback())
	switch {
	case err != nil:
		return stop(fmt.Sprintf("waiting for %s to come up empty failed: %v", stage, err), again)
	case waited.TimedOut:
		return stop(fmt.Sprintf("%s did not come up empty within the wait", stage),
			fmt.Sprintf("Wait for it: zerops_process action=\"wait\" service=%q, %s", stage, standupAgain))
	}
	for _, proc := range waited.Processes {
		if proc.Status == platform.ProcessStatusFinished {
			continue
		}
		why := proc.Status
		if proc.FailReason != nil {
			why += ": " + *proc.FailReason
		}
		return stop(fmt.Sprintf("bringing %s up empty ended %s", stage, why),
			fmt.Sprintf("Read what happened with zerops_events serviceHostname=%q, %s", stage, standupAgain))
	}
	return true
}

// seedPoolStage checks the repository's main out in a pool pair's stage, as
// HQ holds it now (ops.BuildMainCheckoutCommand), for the pool's first image:
// relayed, so HQ is answered with the Mate's project key and nothing is
// stored in the stage. Each call takes main afresh, so one after a fix merged
// deploys the fix. It records nothing: the pair's record is the pool's
// (recordPoolRepository), and the stage holds main only until its own first
// deploy replaces it.
func (d standupDeps) seedPoolStage(ctx context.Context, sp *standupPair, progress *standupProgress) {
	stage := sp.checkoutHost()
	progress.say(fmt.Sprintf("checking %s out into %s for %s's first image", sp.repository, stage, sp.pair.Dev.Hostname))
	failed := func(what string) {
		sp.failedHost = stage
		sp.fail(fmt.Sprintf("checking %s out into %s failed: %s", sp.repository, stage, what), "Call zerops_standup again; it continues from here.")
	}
	relayed, err := hqRelayed(d.batch.sshDeployer, d.batch.httpClient, d.batch.rtInfo)
	if err != nil {
		failed(fmt.Sprintf("git cannot reach HQ: %v", err))
		return
	}
	if _, err := relayed.ExecSSH(ctx, stage, ops.BuildMainCheckoutCommand(ops.DefaultWorkingDir, sp.remote)); err != nil {
		failed(withSSHStderr(fmt.Sprintf("%q could not be fetched and checked out", hqBase), err))
	}
}
