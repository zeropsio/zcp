package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A new Mate stands up from its recipe in one zcp call (docs/spec-mate.md,
// D32). By the time the person's client sends "Stand up development of the
// project.", their browser has imported the Mate's project from the group's
// AI Agent tier — managed services, then zcp, then the runtimes, every dev
// half running and empty (startWithoutCode) and every stage half waiting for
// its first deploy (READY_TO_DEPLOY) — and the broker is writing this
// container's Git variables. What is left is zcp's: read the tier, adopt each
// pair, put the repository's main into its dev half on the Mate's branch,
// deploy every dev half at once and answer once they stand, then — on the
// model's second call — each stage once its dev half and the stages above it
// by the tier's priority stand.
//
// Done by the model, that took sixteen minutes on the Beviro trial
// (2026-09-29): an improvised adopt, then one deploy after another, and a
// stage it could not place. Done here, the model makes one call and is left
// with the part only it can do — starting each dev server, whose command is
// recorded nowhere zcp can read — or, when something fails, with exactly what
// failed and the next call for it: the model is always the backup.
//
// It touches its own project only. The group's stage and production are the
// broker's, and the tier read is the Mate's own, `0 — AI Agent`.

const (
	// standupGitWait bounds the wait for the Git variables the broker writes.
	standupGitWait = 3 * time.Minute
	standupGitPoll = 5 * time.Second
	// standupRuntimeWait bounds the wait for the runtimes the browser imports
	// right before the person signs the agent in: the first turn can arrive
	// while they are still being created.
	standupRuntimeWait = 5 * time.Minute
	standupRuntimePoll = 5 * time.Second
	// standupBatchMax is the most halves deploying at once: past five the
	// platform's build queue may fall back to serial scheduling
	// (zerops_deploy_batch's own advice).
	standupBatchMax = 5
	// standupBootWait bounds the wait for the container's own import of the
	// runtimes (MATE_SETUP_RUNTIMES) — the import's own bound.
	standupBootWait = 20 * time.Minute
	standupBootPoll = 5 * time.Second
	// standupReflogIntent heads the stand-up's entry in AGENTS.md's reflog.
	standupReflogIntent = "Stand up development from the group's recipe"
)

// StandupInput is zerops_standup's input: none — the stand-up reads everything
// it needs from the project, the container and the group's repository.
type StandupInput struct{}

// standupDeps is what a stand-up needs: the batch deploy it shares with
// zerops_deploy_batch (and through it the client, SSH, auth and state), the
// mounter, and where and how long to wait for the Git variables.
type standupDeps struct {
	batch       batchDeployer
	mounter     ops.Mounter
	liveEnvPath string
	gitWait     time.Duration
	gitPoll     time.Duration
	runtimeWait time.Duration
	runtimePoll time.Duration
	// statusPath is the setup status file the stand-up writes its section of
	// and reads the boot import's from ("" writes and waits on nothing);
	// bootWait/bootPoll bound the wait for that import, trackPoll the looks
	// at a batch's processes (standup_status.go).
	statusPath string
	bootWait   time.Duration
	bootPoll   time.Duration
	trackPoll  time.Duration
	// beat is how often a running stand-up rewrites the file (0 is
	// mate.StandupBeat).
	beat time.Duration
}

// RegisterStandup registers zerops_standup. The server registers it only in a
// Mate — a container with ZCP_MATE_ENABLED and an SSH deployer — since only a
// Mate has a group's recipe to stand up from (docs/spec-mate.md §2.0).
func RegisterStandup(
	srv *mcp.Server,
	client platform.Client,
	httpClient ops.HTTPDoer,
	projectID string,
	sshDeployer ops.SSHDeployer,
	mounter ops.Mounter,
	authInfo *auth.Info,
	logFetcher platform.LogFetcher,
	rtInfo runtime.Info,
	stateDir string,
) {
	registerStandup(srv, standupDeps{
		batch: batchDeployer{
			client: client, httpClient: httpClient, projectID: projectID, sshDeployer: sshDeployer,
			authInfo: authInfo, logFetcher: logFetcher, rtInfo: rtInfo, stateDir: stateDir,
		},
		mounter:     mounter,
		liveEnvPath: mate.LiveEnvStorePath,
		gitWait:     standupGitWait,
		gitPoll:     standupGitPoll,
		runtimeWait: standupRuntimeWait,
		runtimePoll: standupRuntimePoll,
		statusPath:  mate.StatusFilePath(),
		bootWait:    standupBootWait,
		bootPoll:    standupBootPoll,
		trackPoll:   standupTrackPoll,
	})
}

func registerStandup(srv *mcp.Server, d standupDeps) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "zerops_standup",
		Description: "Stands up this Mate's development from the group repo's AI Agent tier: adopts each dev/stage pair, " +
			"checks main out into each dev half, deploys dev halves, then stages on a second call in build order, reports each service's next step. " +
			"Call it first when the person's message is \"Stand up development of the project.\" Idempotent: call again after a fix.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Stand up development from the recipe",
			IdempotentHint:  true,
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ StandupInput) (*mcp.CallToolResult, any, error) {
		progress := newStandupProgress(buildProgressCallback(ctx, req))
		progress.status = newStandupStatus(d.statusPath)
		result := d.run(ctx, progress)
		progress.quiesce(ctx)
		return result, nil, nil
	})
}

// run is the whole stand-up. A refusal before anything is touched — no Git
// access, no tier, a tier with no pair — is an error result naming the
// fallback; past that, the answer is the per-service report, whatever it
// holds.
func (d standupDeps) run(ctx context.Context, progress *standupProgress) *mcp.CallToolResult {
	progress.st().begin()
	stopBeat := progress.st().beat(d.beat)
	result := d.stand(ctx, progress)
	stopBeat()
	if result.IsError {
		progress.st().end(refusalText(result))
	} else {
		progress.st().end("")
	}
	return result
}

// stand is the stand-up's work; run reports how it ended.
func (d standupDeps) stand(ctx context.Context, progress *standupProgress) *mcp.CallToolResult {
	if d.importWaitsClosedOff() {
		return standupRefusal(platform.ErrPrerequisiteMissing, openMateRefusal,
			"Nothing was touched. Tell the person to press Finish setup on this Mate in the app; the container then imports the runtimes, and zerops_standup stands them up.")
	}
	wiring := d.awaitGitAccess(ctx, progress)
	if !wiring.Ready() {
		return standupRefusal(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("Git access has not reached this Mate: %s is not on this service after %s. The broker writes the three once it has made this Mate's Gitea bot, and the stand-up needs them to check the recipe's repositories out.",
				strings.Join(wiring.MissingKeys(), ", "), d.gitWait),
			"Nothing was touched. Call zerops_standup again in a few minutes. To carry on without it, adopt the pairs with zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\" — their repositories are wired and checked out once the variables land, and a first deploy follows.")
	}
	src, refusal := d.readTier(ctx, wiring, progress)
	if refusal != nil {
		return refusal
	}

	pairs, live := d.preparePairs(ctx, wiring, src, progress)
	d.deployAll(ctx, pairs, live, src.tier.ProjectEnvs, progress)
	d.observeDevServers(pairs)

	resp := buildStandupResponse(src, pairs, live)
	resp.Envelope = freshEnvelope(ctx, d.batch.stateDir, d.batch.client, d.batch.projectID, d.batch.rtInfo)
	return jsonResult(resp)
}

// awaitGitAccess reads the container's Git variables — from the live env
// store, which the platform rewrites within seconds of the broker's write
// (giteaEnvLookup) — until all three are there or the wait is over.
func (d standupDeps) awaitGitAccess(ctx context.Context, progress *standupProgress) ops.GiteaWiring {
	deadline := time.Now().Add(d.gitWait)
	for {
		wiring := ops.ReadGiteaWiring(giteaEnvLookup(d.liveEnvPath))
		if wiring.Ready() || !time.Now().Before(deadline) {
			return wiring
		}
		progress.say("waiting for this Mate's Git access (" + strings.Join(wiring.MissingKeys(), ", ") + " not on this service yet)")
		select {
		case <-ctx.Done():
			return wiring
		case <-time.After(d.gitPoll):
		}
	}
}

// standupSource is the tier a stand-up works from and where it came from.
type standupSource struct {
	org       string
	groupRepo string
	tier      workflow.MateTier
}

// readTier finds the group repo — its org is the one a wired pair names, or
// else the one the bot is a member of (its group's `read` team) — and reads
// and parses the AI Agent tier on its main.
func (d standupDeps) readTier(ctx context.Context, wiring ops.GiteaWiring, progress *standupProgress) (standupSource, *mcp.CallToolResult) {
	const fallback = "Nothing was touched. Carry on with zcp's own tools: zerops_discover, then adopt the pairs with zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\", then deploy each dev half and its stage."
	progress.say("reading the group's recipe")
	orgs, err := d.groupOrgs(ctx, wiring)
	if err != nil {
		return standupSource{}, standupRefusal(platform.ErrAPIError,
			fmt.Sprintf("Could not read which group this Mate belongs to on Gitea: %v.", err), fallback)
	}
	if len(orgs) == 0 {
		return standupSource{}, standupRefusal(platform.ErrPrerequisiteMissing,
			"This Mate's Gitea bot belongs to no group yet, so there is no group repo to read the recipe from — the broker adds it to its group's team once the Mate is registered.", fallback)
	}
	var (
		found   []standupSource
		bodies  []string
		without []string
	)
	for _, org := range orgs {
		groupRepo := org + "/group"
		body, ok, err := ops.ReadGiteaFile(ctx, d.batch.httpClient, wiring.GiteaURL, wiring.Token, groupRepo, giteaProtectedBase, workflow.MateTierImportPath)
		if err != nil {
			return standupSource{}, standupRefusal(platform.ErrAPIError,
				fmt.Sprintf("Could not read %s@%s:%s: %v.", groupRepo, giteaProtectedBase, workflow.MateTierImportPath, err), fallback)
		}
		if !ok {
			without = append(without, groupRepo)
			continue
		}
		found = append(found, standupSource{org: org, groupRepo: groupRepo})
		bodies = append(bodies, body)
	}
	switch len(found) {
	case 0:
		return standupSource{}, standupRefusal(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("%s has no %s on %s: the project's recipe is not merged yet, so there is nothing to stand up from.",
				strings.Join(without, ", "), workflow.MateTierImportPath, giteaProtectedBase), fallback)
	case 1:
	default:
		repos := make([]string, 0, len(found))
		for _, f := range found {
			repos = append(repos, f.groupRepo)
		}
		return standupSource{}, standupRefusal(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("This Mate's Gitea bot is in more than one group with an AI Agent tier (%s), so which recipe is this project's is not known.", strings.Join(repos, ", ")), fallback)
	}
	src := found[0]
	tier, err := workflow.ParseMateTier(bodies[0], wiring.GiteaURL, src.org)
	if err != nil {
		return standupSource{}, standupRefusal(platform.ErrInvalidImportYml,
			fmt.Sprintf("%s@%s:%s cannot be stood up: %v.", src.groupRepo, giteaProtectedBase, workflow.MateTierImportPath, err), fallback)
	}
	src.tier = tier
	return src, nil
}

// groupOrgs names the org the group repo is in: the one a wired pair's
// repository names, else every org whose group repository the bot reads.
func (d standupDeps) groupOrgs(ctx context.Context, wiring ops.GiteaWiring) ([]string, error) {
	if metas, err := workflow.ListServiceMetas(d.batch.stateDir); err == nil {
		if org := knownGiteaOrg(metas); org != "" {
			return []string{org}, nil
		}
	}
	orgs, err := ops.GiteaGroupOrgs(ctx, d.batch.httpClient, wiring.GiteaURL, wiring.Token)
	if err != nil {
		return nil, fmt.Errorf("list the bot's repositories: %w", err)
	}
	return orgs, nil
}

// standupRefusal is a stand-up that stopped before touching anything: an
// error result the model reads as "carry on without it", with the adopt route
// as its recovery.
func standupRefusal(code, message, suggestion string) *mcp.CallToolResult {
	return convertError(platform.NewPlatformError(code, message, suggestion),
		WithRecovery(&RecoveryHint{
			Tool:   "zerops_workflow",
			Action: "start",
			Args:   map[string]string{"workflow": "bootstrap", "route": "adopt"},
		}))
}

// standupPair is one pair's way through the stand-up.
type standupPair struct {
	pair       workflow.MateTierPair
	repository string // org/name
	dev, stage *platform.ServiceStack
	adopted    string // standupNow | standupAlready
	wired      string
	branch     string
	// failed is what stopped the pair before any deploy, exactly; next is
	// the model's call for it; failedHost is the half it is about, when one.
	failed, next, failedHost string
	devDeploy                *standupDeploy
	stageDeploy              *standupDeploy
	devResult                *ops.DeployResult
	// devRanBefore is a dev half that ran code when the call began: its
	// stage deploys on this call, else on the next.
	devRanBefore bool
	devServer    *standupDevServer
}

// stoodUp is a pair whose two halves both run code.
func (p *standupPair) stoodUp() bool {
	return p.failed == "" && p.devDeploy.running() && p.stageDeploy.running()
}

// fail stops the pair with what failed and the model's next step.
func (p *standupPair) fail(what, next string) {
	if p.failed == "" {
		p.failed, p.next = what, next
	}
}

// preparePairs takes every pair of the tier to the point it can deploy: its
// services settled and present, adopted, its dev half mounted, its
// repository checked out on the Mate's branch. A pair that cannot get there
// is stopped with what failed; the others go on.
func (d standupDeps) preparePairs(ctx context.Context, wiring ops.GiteaWiring, src standupSource, progress *standupProgress) ([]*standupPair, map[string]*platform.ServiceStack) {
	pairs := make([]*standupPair, 0, len(src.tier.Pairs))
	for _, p := range src.tier.Pairs {
		pairs = append(pairs, &standupPair{pair: p, repository: src.org + "/" + p.RepoName})
	}
	bootFailed := d.awaitBootImport(ctx, progress)
	live, err := d.awaitRuntimes(ctx, src.tier, progress)
	if err != nil {
		for _, sp := range pairs {
			sp.fail(fmt.Sprintf("could not list this project's services: %v", err), "Retry zerops_standup; if it persists, check the API with zerops_discover.")
		}
		return pairs, live
	}
	if d.settle(ctx, src.tier, live, progress) {
		if fresh, err := d.liveServices(ctx); err == nil {
			live = fresh
		}
	}

	open := d.projectOpen(ctx, src.tier, live)
	var adoptedNow []workflow.BootstrapTarget
	for _, sp := range pairs {
		sp.dev, sp.stage = live[sp.pair.Dev.Hostname], live[sp.pair.Stage.Hostname]
		if !d.presentAndRunning(sp, src, bootFailed, open) {
			continue
		}
		if target, now := d.adopt(sp, managedDependencies(src.tier, live)); now {
			adoptedNow = append(adoptedNow, target)
		}
	}
	d.recordReflog(adoptedNow)

	for _, sp := range pairs {
		if sp.failed == "" {
			d.mountDevHalf(ctx, sp)
		}
	}

	var wg sync.WaitGroup
	for _, sp := range pairs {
		if sp.failed != "" {
			continue
		}
		wg.Go(func() { d.wire(ctx, wiring, sp, progress) })
	}
	wg.Wait()
	for _, sp := range pairs {
		if sp.failed == "" {
			continue
		}
		host := sp.failedHost
		if host == "" {
			host = sp.pair.Dev.Hostname
		}
		progress.st().step(host, mate.StepBuild, mate.StepFailed, "", sp.failed)
	}
	return pairs, live
}

// awaitRuntimes reads the project's services until every pair's halves are
// there — the dev half running (startWithoutCode), the stage half created —
// or the wait is over. The browser imports the runtimes right before the
// person signs the agent in, so the stand-up can be asked for while they are
// still being created; what is still missing after the wait is reported by
// presentAndRunning.
func (d standupDeps) awaitRuntimes(ctx context.Context, tier workflow.MateTier, progress *standupProgress) (map[string]*platform.ServiceStack, error) {
	deadline := time.Now().Add(d.runtimeWait)
	for {
		live, err := d.liveServices(ctx)
		if err != nil {
			return nil, err
		}
		pending := runtimesNotUp(tier, live)
		if len(pending) == 0 || !time.Now().Before(deadline) {
			return live, nil
		}
		progress.say("waiting for the import to create " + strings.Join(pending, ", "))
		select {
		case <-ctx.Done():
			return live, nil
		case <-time.After(d.runtimePoll):
		}
	}
}

// runtimesNotUp names every half the stand-up still waits for: a dev half not
// there or not running yet, a stage half not there.
func runtimesNotUp(tier workflow.MateTier, live map[string]*platform.ServiceStack) []string {
	var pending []string
	for _, p := range tier.Pairs {
		if dev := live[p.Dev.Hostname]; dev == nil || !dev.IsLive() {
			pending = append(pending, p.Dev.Hostname)
		}
		if live[p.Stage.Hostname] == nil {
			pending = append(pending, p.Stage.Hostname)
		}
	}
	return pending
}

// liveServices reads the project's services straight from the database — the
// only read that tells a runtime imported empty from a deployed one.
func (d standupDeps) liveServices(ctx context.Context) (map[string]*platform.ServiceStack, error) {
	services, err := d.batch.client.ListServicesDirect(ctx, d.batch.projectID)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	live := make(map[string]*platform.ServiceStack, len(services))
	for i := range services {
		live[services[i].Name] = &services[i]
	}
	return live, nil
}

// settle waits for whatever the import still has in flight on the pairs'
// halves and the managed services they reach, so no pair is adopted mid-import
// and no dev deploy runs its init commands against a database still starting.
// Reports whether it waited.
func (d standupDeps) settle(ctx context.Context, tier workflow.MateTier, live map[string]*platform.ServiceStack, progress *standupProgress) bool {
	idToHost := map[string]string{}
	add := func(hostname string) {
		if svc := live[hostname]; svc != nil {
			idToHost[svc.ID] = hostname
		}
	}
	for _, p := range tier.Pairs {
		add(p.Dev.Hostname)
		add(p.Stage.Hostname)
	}
	for _, s := range tier.Skipped {
		if s.Reason == workflow.MateTierSkipManaged {
			add(s.Hostname)
		}
	}
	activity, err := ops.ProjectActivity(ctx, d.batch.client, d.batch.projectID, idToHost)
	if err != nil || len(activity) == 0 {
		return false
	}
	var ids, hosts []string
	for host, liveOps := range activity {
		hosts = append(hosts, host)
		for _, op := range liveOps {
			ids = append(ids, op.ProcessID)
		}
	}
	progress.say("waiting for the import to finish on " + strings.Join(hosts, ", "))
	if _, err := ops.WaitProcesses(ctx, d.batch.client, ids, progress.callback()); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: stand-up: wait for the import: %v\n", err)
	}
	return true
}

// presentAndRunning stops a pair whose halves the import did not create
// within the wait, or whose dev half is not running: the repository is
// checked out INTO the dev half, so it must be up, and a runtime imported
// without startWithoutCode is not. A missing half is the browser's import
// refused or not finished; the model imports it from the tier with zcp's own
// import, shaped the way the browser imports it.
// projectOpen reports, when a half is missing in a Mate the new press made,
// a project not tagged closed off yet: nothing may be imported into it
// (refuseOpenMate), so the stand-up never asks the model to.
func (d standupDeps) projectOpen(ctx context.Context, tier workflow.MateTier, live map[string]*platform.ServiceStack) bool {
	missing := false
	for _, p := range tier.Pairs {
		if live[p.Dev.Hostname] == nil || live[p.Stage.Hostname] == nil {
			missing = true
		}
	}
	if !missing || !newFlowMate(d.batch.rtInfo, d.liveEnvPath) {
		return false
	}
	closed, err := ops.ReadProjectClosedOff(ctx, d.batch.client, d.batch.projectID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zcp: stand-up: %v\n", err)
		return false
	}
	return !closed
}

func (d standupDeps) presentAndRunning(sp *standupPair, src standupSource, bootFailed map[string]string, open bool) bool {
	p := sp.pair
	for _, half := range []struct {
		rt  workflow.MateTierRuntime
		svc *platform.ServiceStack
		dev bool
	}{{p.Dev, sp.dev, true}, {p.Stage, sp.stage, false}} {
		if half.svc != nil {
			continue
		}
		shape := "a stage half is imported with no build, waiting for its first deploy"
		entry := fmt.Sprintf("services: [{hostname: %s, type: %s}]", half.rt.Hostname, half.rt.Type)
		if half.dev {
			shape = "a dev half is imported running and empty, its build taken out"
			entry = fmt.Sprintf("services: [{hostname: %s, type: %s, startWithoutCode: true}]", half.rt.Hostname, half.rt.Type)
		}
		sp.failedHost = half.rt.Hostname
		why := "the runtimes are imported when the Mate is made, and this one's import was refused or has not finished"
		if reason := bootFailed[half.rt.Hostname]; reason != "" {
			why = "the container's import of the runtimes failed on it: " + reason
		} else if reason := bootFailed[""]; reason != "" {
			why = "the container's import of the runtimes failed: " + reason
		}
		if open {
			sp.fail(fmt.Sprintf("%s is not in this project after %s: %s", half.rt.Hostname, d.runtimeWait, why),
				openMateRefusal+" Then call zerops_standup again.")
			return false
		}
		sp.fail(fmt.Sprintf("%s is not in this project after %s: %s", half.rt.Hostname, d.runtimeWait, why),
			fmt.Sprintf("Import it from the tier with zerops_import content=%q (%s; add its envSecrets and scaling from %s's %s entry), then call zerops_standup again.",
				entry, shape, src.groupRepo, workflow.MateTierImportPath))
		return false
	}
	if !sp.dev.IsLive() {
		sp.failedHost = p.Dev.Hostname
		sp.fail(fmt.Sprintf("%s is %s, not running, after %s: a dev half is imported running and empty (startWithoutCode: true) so its repository can be checked out into it", p.Dev.Hostname, sp.dev.Status, d.runtimeWait),
			fmt.Sprintf("Start it (zerops_manage action=\"start\" serviceHostname=%q) or wait for its import to finish, then call zerops_standup again.", p.Dev.Hostname))
		return false
	}
	return true
}

// adopt records the pair as the adopt route would (workflow.AdoptPair).
// Returns the target and whether it was adopted by this call.
func (d standupDeps) adopt(sp *standupPair, deps []workflow.Dependency) (workflow.BootstrapTarget, bool) {
	p := sp.pair
	target := workflow.BootstrapTarget{
		Runtime: workflow.RuntimeTarget{
			DevHostname:      p.Dev.Hostname,
			ExplicitStage:    p.Stage.Hostname,
			Type:             p.Dev.Type,
			BootstrapMode:    topology.PlanModeStandard,
			IsExisting:       true,
			PrimarySetupName: p.Dev.Setup,
			StageSetupName:   p.Stage.Setup,
		},
		Dependencies: deps,
	}
	if !topology.TypesAreEquivalent(p.Dev.Type, p.Stage.Type) {
		target.Runtime.StageType = p.Stage.Type
	}
	existing, _ := workflow.ReadServiceMeta(d.batch.stateDir, p.Dev.Hostname)
	already := existing != nil && existing.IsComplete() && existing.StageHostname == p.Stage.Hostname
	if err := workflow.AdoptPair(d.batch.stateDir, target, time.Now().UTC().Format("2006-01-02")); err != nil {
		sp.fail(fmt.Sprintf("adopting %s→%s failed: %v", p.Dev.Hostname, p.Stage.Hostname, err),
			"Adopt the pair by hand with zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\" and an explicit standard plan for it.")
		return target, false
	}
	if already {
		sp.adopted = standupAlready
		return target, false
	}
	sp.adopted = standupNow
	return target, true
}

// managedDependencies are the tier's managed services the project holds, as
// the adopt route attaches them: shared, existing.
func managedDependencies(tier workflow.MateTier, live map[string]*platform.ServiceStack) []workflow.Dependency {
	var deps []workflow.Dependency
	for _, s := range tier.Skipped {
		if s.Reason == workflow.MateTierSkipManaged && live[s.Hostname] != nil {
			deps = append(deps, workflow.Dependency{Hostname: s.Hostname, Type: s.Type, Resolution: workflow.ResolutionExists})
		}
	}
	return deps
}

// recordReflog appends the pairs this call adopted to AGENTS.md's reflog,
// the record the adopt route's close leaves too. Best-effort.
func (d standupDeps) recordReflog(targets []workflow.BootstrapTarget) {
	if len(targets) == 0 || d.batch.stateDir == "" {
		return
	}
	agentsMD := filepath.Join(projectRootFromState(d.batch.stateDir), "AGENTS.md")
	if err := workflow.AppendReflogEntry(agentsMD, standupReflogIntent, targets, "zerops_standup", time.Now().UTC().Format("2006-01-02")); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: stand-up reflog: %v\n", err)
	}
}

// mountDevHalf mounts the dev half where the deploy's pre-flight reads its
// zerops.yaml and the Mate's app reads its files, and gives it the repository
// the checkout lands in — what the adopt route's provision does.
func (d standupDeps) mountDevHalf(ctx context.Context, sp *standupPair) {
	host := sp.pair.Dev.Hostname
	// A dev half the import only just started may not take SSH yet, and
	// every step from here on is SSH.
	if err := ops.WaitSSHReady(ctx, d.batch.sshDeployer, host); err != nil {
		sp.fail(fmt.Sprintf("%s runs but does not answer SSH: %v", host, err),
			fmt.Sprintf("Check it with zerops_logs serviceHostname=%q, then call zerops_standup again.", host))
		return
	}
	if d.mounter != nil {
		if _, err := ops.MountService(ctx, d.batch.client, d.batch.projectID, d.mounter, host); err != nil {
			sp.fail(fmt.Sprintf("could not mount %s: %v — its deploy reads zerops.yaml there", host, err),
				fmt.Sprintf("Mount it with zerops_mount action=\"mount\" serviceHostname=%q, then call zerops_standup again.", host))
			return
		}
	}
	if err := ops.InitServiceGit(ctx, d.batch.sshDeployer, host); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: stand-up: InitServiceGit %s: %v\n", host, err)
	}
	if sp.adopted == standupNow {
		adoptRepoBaseline(ctx, d.batch.client, d.batch.projectID, d.batch.sshDeployer, d.batch.stateDir, host)
	}
}

// wire puts the recipe's repository into the pair's dev half on the Mate's
// branch — the broker's repository, git-push to it, main fetched and the
// branch cut from it (wireGiteaPair, the reconcile's own wiring) — unless an
// earlier pass did. A repository the recipe names that Gitea does not have is
// refused before the broker is asked: the broker creates what it is asked for.
func (d standupDeps) wire(ctx context.Context, wiring ops.GiteaWiring, sp *standupPair, progress *standupProgress) {
	host := sp.pair.Dev.Hostname
	meta, _ := workflow.FindServiceMeta(d.batch.stateDir, host)
	if meta == nil {
		sp.fail(fmt.Sprintf("%s has no record after its adoption", host), "Call zerops_standup again.")
		return
	}
	if giteaWiredPair(meta) && meta.Gitea.Branch != "" && giteaPairPushes(meta.GitPushState) {
		sp.wired, sp.branch = standupAlready, meta.Gitea.Branch
		return
	}
	progress.say(fmt.Sprintf("checking %s out into %s", sp.repository, host))
	exists, err := ops.GiteaRepositoryExists(ctx, d.batch.httpClient, wiring.GiteaURL, wiring.Token, sp.repository)
	if err != nil {
		sp.fail(fmt.Sprintf("could not read %s on Gitea: %v", sp.repository, err), "Call zerops_standup again; it continues from here.")
		return
	}
	if !exists {
		sp.fail(fmt.Sprintf("%s is not on Gitea: the recipe names a repository the group does not have, so there is no code to stand %s up from", sp.repository, host),
			"Tell the person the recipe's buildFromGit for this pair names a repository that does not exist; the group repo's AI Agent tier needs fixing before this pair can stand up.")
		return
	}
	prior := readGiteaPairState(d.batch.stateDir, host)
	outcome := wireGiteaPair(ctx, d.batch.client, d.batch.httpClient, d.batch.sshDeployer, d.batch.rtInfo, d.batch.stateDir, wiring, meta, sp.pair.RepoName)
	recordGiteaAttempt(d.batch.stateDir, host, prior, time.Now().UTC(), outcome.line, outcome.wired)
	if !outcome.wired {
		next := "Call zerops_standup again; it continues from here."
		if outcome.remedy != "" {
			next = outcome.remedy + " Then call zerops_standup again."
		}
		sp.fail(fmt.Sprintf("checking %s out into %s failed: %s", sp.repository, host, outcome.line), next)
		return
	}
	sp.wired = standupNow
	if wired, _ := workflow.FindServiceMeta(d.batch.stateDir, host); wired != nil && wired.Gitea != nil {
		sp.branch = wired.Gitea.Branch
	}
}

// standupProgress turns the stand-up's steps and its builds' polls into one
// stream of progress notifications. MCP wants a notification's progress to
// grow with every one; the build polls report their own percentages, so the
// stream counts notifications instead and passes each message through.
//
// A notification and the result must never share a chunk on the wire: Claude
// Code's MCP client then reads the progress token as unknown and tears the
// transport down (the race ops.PollBuild orders its returns around). A
// stand-up can refuse milliseconds after its last notification, so the
// answer waits out standupProgressGap after it (quiesce).
type standupProgress struct {
	mu   sync.Mutex
	n    float64
	last time.Time
	send ops.ProgressCallback
	// status is the stand-up's section of the setup status file, nil when
	// there is none to write.
	status *standupStatus
}

// standupProgressGap is the least time between the last notification and the
// result — a build poll's own first interval.
const standupProgressGap = time.Second

func newStandupProgress(send ops.ProgressCallback) *standupProgress {
	return &standupProgress{send: send}
}

func (p *standupProgress) say(message string) {
	if p == nil || p.send == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.n++
	p.last = time.Now()
	p.send(message, p.n, 0)
}

// st is the stream's status writer; nil-safe like the stream.
func (p *standupProgress) st() *standupStatus {
	if p == nil {
		return nil
	}
	return p.status
}

// quiesce holds the result until standupProgressGap has passed since the last
// notification; nothing when none was sent.
func (p *standupProgress) quiesce(ctx context.Context) {
	if p == nil {
		return
	}
	p.mu.Lock()
	last := p.last
	p.mu.Unlock()
	if last.IsZero() {
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Until(last.Add(standupProgressGap))):
	}
}

// callback is the stream as an ops.ProgressCallback, for the polls.
func (p *standupProgress) callback() ops.ProgressCallback {
	if p == nil || p.send == nil {
		return nil
	}
	return func(message string, _, _ float64) { p.say(message) }
}
