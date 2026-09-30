package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// The stand-up deploys in waves by the tier's priority, highest first — the
// order the recipe's author gave the import, so what a runtime needs at build
// time (an API a storefront's build reads) is up before it. A wave holds every
// pair of one priority, and a lower wave starts only once every pair above it
// stands: a pair that did not stand up holds the rest, which report what they
// wait for.
//
// Within a wave, the dev halves go as one batch and the stage cross-deploys
// as the next — never in one batch together. Both read the dev half's
// /var/www, and ops.DeploySSH serialises every deploy from one source
// container on a per-source git lock held across the whole `zcli push`, which
// blocks until the pipeline ends: in one batch the pair's two deploys would
// still run one after the other, in whichever order the goroutines take the
// lock. With the dev push first, its SSH session dies as the new container
// replaces the old one (the "common exit 255"), and the cross-deploy then
// dials a container that may still be starting and uploads the dev build's
// output rather than the checkout; with the cross-deploy first, it reads the
// checkout. Two outcomes from one input, for no speed gained. As the next
// batch, the stage deploys after its dev half's build succeeded and its new
// container answered SSH (pollDeployBuild waits for it), reads what that
// build deployed — the whole repository, deployFiles [.] — and is skipped
// when the dev half failed, whose stage would only fail the same way.

// standupDeploy is one half's deploy, as the report says it.
type standupDeploy struct {
	Status        string   `json:"status"`
	AppVersionID  string   `json:"appVersionId,omitempty"`
	BuildDuration string   `json:"buildDuration,omitempty"`
	URL           string   `json:"url,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	BuildLogs     []string `json:"buildLogs,omitempty"`
	RuntimeLogs   []string `json:"runtimeLogs,omitempty"`
	// Failure is zcp's classification of a failed deploy.
	Failure *standupFailure `json:"failureClassification,omitempty"`
	next    string
}

// standupFailure is a failed deploy's classification as the report carries it.
type standupFailure struct {
	Category        string `json:"category"`
	LikelyCause     string `json:"likelyCause,omitempty"`
	SuggestedAction string `json:"suggestedAction,omitempty"`
}

// running reports a half that runs code — deployed now or before.
func (d *standupDeploy) running() bool {
	return d != nil && (d.Status == standupDeployed || d.Status == standupAlreadyDeployed)
}

// deployWaves runs every wave in order.
func (d standupDeps) deployWaves(ctx context.Context, pairs []*standupPair, progress *standupProgress) {
	var held []string
	for _, wave := range standupWaves(pairs) {
		if len(held) > 0 {
			reason := fmt.Sprintf("an earlier wave did not stand up (%s): a higher priority is built first, and this pair waits for it", strings.Join(held, ", "))
			for _, sp := range wave {
				if sp.failed != "" {
					continue
				}
				sp.devDeploy = &standupDeploy{Status: standupNotDeployed, Reason: reason}
				sp.stageDeploy = &standupDeploy{Status: standupNotDeployed, Reason: reason}
			}
		} else {
			d.deployWave(ctx, wave, progress)
		}
		for _, sp := range wave {
			if !sp.stoodUp() {
				held = append(held, sp.pair.Dev.Hostname)
			}
		}
	}
}

// standupWaves groups the pairs by priority, highest first; within a wave
// they keep the tier's order.
func standupWaves(pairs []*standupPair) [][]*standupPair {
	byPriority := map[int][]*standupPair{}
	var priorities []int
	for _, sp := range pairs {
		p := sp.pair.Priority()
		if _, seen := byPriority[p]; !seen {
			priorities = append(priorities, p)
		}
		byPriority[p] = append(byPriority[p], sp)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(priorities)))
	waves := make([][]*standupPair, 0, len(priorities))
	for _, p := range priorities {
		waves = append(waves, byPriority[p])
	}
	return waves
}

// deployWave deploys one wave: the dev halves that run no code yet, then the
// stages of the pairs whose dev half runs.
func (d standupDeps) deployWave(ctx context.Context, wave []*standupPair, progress *standupProgress) {
	devs := make([]standupHalf, 0, len(wave))
	for _, sp := range wave {
		if sp.failed != "" {
			continue
		}
		p := sp.pair
		devs = append(devs, standupHalf{pair: sp, target: ops.DeployBatchTarget{SourceService: p.Dev.Hostname, TargetService: p.Dev.Hostname, Setup: p.Dev.Setup}, dev: true})
	}
	d.deployHalves(ctx, devs, progress)

	stages := make([]standupHalf, 0, len(wave))
	for _, sp := range wave {
		if sp.failed != "" {
			continue
		}
		p := sp.pair
		if !sp.devDeploy.running() {
			sp.stageDeploy = &standupDeploy{Status: standupNotDeployed,
				Reason: fmt.Sprintf("%s did not deploy, and the stage is built from it", p.Dev.Hostname)}
			continue
		}
		stages = append(stages, standupHalf{pair: sp, target: ops.DeployBatchTarget{SourceService: p.Dev.Hostname, TargetService: p.Stage.Hostname, Setup: p.Stage.Setup}})
	}
	d.deployHalves(ctx, stages, progress)
}

// standupHalf is one half of a pair a wave deploys.
type standupHalf struct {
	pair   *standupPair
	target ops.DeployBatchTarget
	dev    bool
}

func (h standupHalf) record(deploy *standupDeploy) {
	if h.dev {
		h.pair.devDeploy = deploy
		return
	}
	h.pair.stageDeploy = deploy
}

// deployHalves deploys every half that runs no code yet — one that does is
// left as it is, one still building from an earlier call is waited for — in
// batches of at most standupBatchMax, each through the same gate and
// pre-flight as zerops_deploy_batch, and records what became of each.
func (d standupDeps) deployHalves(ctx context.Context, halves []standupHalf, progress *standupProgress) {
	if len(halves) == 0 {
		return
	}
	live, err := d.liveServices(ctx)
	if err != nil {
		for _, h := range halves {
			h.record(&standupDeploy{Status: standupNotDeployed, Reason: fmt.Sprintf("could not read the project's services: %v", err)})
		}
		return
	}
	if d.awaitInFlight(ctx, halves, live, progress) {
		if fresh, err := d.liveServices(ctx); err == nil {
			live = fresh
		}
	}

	byTarget := map[string]standupHalf{}
	var targets []ops.DeployBatchTarget
	for _, h := range halves {
		svc := live[h.target.TargetService]
		if svc != nil && svc.HasDeployedCode() {
			h.record(&standupDeploy{Status: standupAlreadyDeployed, URL: ops.ResolveSubdomainURL(ctx, d.batch.client, d.batch.projectID, svc)})
			continue
		}
		if blocked := d.batch.gate([]ops.DeployBatchTarget{h.target}); blocked != nil {
			h.record(&standupDeploy{Status: standupDeployFailed, Reason: "refused before the build: " + refusalText(blocked)})
			continue
		}
		resolved, refusal := d.batch.preflight(ctx, h.target)
		if refusal != nil {
			h.record(&standupDeploy{Status: standupDeployFailed, Reason: "refused before the build: " + refusalText(refusal.result())})
			continue
		}
		byTarget[resolved.TargetService] = h
		targets = append(targets, resolved)
	}

	for start := 0; start < len(targets); start += standupBatchMax {
		batch := targets[start:min(start+standupBatchMax, len(targets))]
		names := make([]string, 0, len(batch))
		for _, t := range batch {
			names = append(names, t.TargetService)
		}
		progress.say("deploying " + strings.Join(names, ", "))
		result := d.batch.deploy(ctx, progress.callback(), batch, false)
		for _, entry := range result.Entries {
			h := byTarget[entry.Target.TargetService]
			deploy := standupDeployFrom(entry)
			h.record(deploy)
			if h.dev {
				h.pair.devResult = entry.Result
			}
			// No work session records these deploys (RecordDeployAttempt is
			// a session's), so the durable first-deploy mark is stamped the
			// way a deploy outside one is.
			if deploy.Status == standupDeployed {
				if _, _, err := workflow.RecordExternalDeploy(d.batch.stateDir, entry.Target.TargetService); err != nil {
					fmt.Fprintf(os.Stderr, "zcp: stand-up: record the first deploy of %s: %v\n", entry.Target.TargetService, err)
				}
			}
		}
	}
}

// awaitInFlight waits for a build an earlier call left running on any of the
// halves, so a second call never starts a second build on top of it. Reports
// whether it waited.
func (d standupDeps) awaitInFlight(ctx context.Context, halves []standupHalf, live map[string]*platform.ServiceStack, progress *standupProgress) bool {
	idToHost := map[string]string{}
	for _, h := range halves {
		if svc := live[h.target.TargetService]; svc != nil {
			idToHost[svc.ID] = svc.Name
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
	progress.say("waiting for the build already running on " + strings.Join(hosts, ", "))
	if _, err := ops.WaitProcesses(ctx, d.batch.client, ids, progress.callback()); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: stand-up: wait for a running build: %v\n", err)
	}
	return true
}

// standupDeployFrom reads one batch entry as the report says it.
func standupDeployFrom(entry ops.DeployBatchEntryResult) *standupDeploy {
	r := entry.Result
	switch {
	case entry.Error != "":
		return &standupDeploy{Status: standupDeployFailed, Reason: "the deploy did not start: " + entry.Error}
	case r == nil:
		return &standupDeploy{Status: standupDeployFailed, Reason: "the deploy reported nothing"}
	case r.Status == statusDeployed:
		url := r.SubdomainURL
		if url == "" && r.PublicAccess != nil {
			url = r.PublicAccess.URL
		}
		return &standupDeploy{Status: standupDeployed, AppVersionID: r.AppVersionID, BuildDuration: r.BuildDuration, URL: url, next: r.NextActions}
	case r.TimedOut:
		return &standupDeploy{Status: standupInFlight,
			Reason: "the build is still running past the poll's budget; the platform finishes it — a second zerops_standup waits for it and carries on"}
	}
	deploy := &standupDeploy{
		Status:        standupDeployFailed,
		Reason:        fmt.Sprintf("the deploy ended %s", r.Status),
		BuildDuration: r.BuildDuration,
		BuildLogs:     r.BuildLogs,
		RuntimeLogs:   r.RuntimeLogs,
		next:          r.NextActions,
	}
	if c := r.FailureClassification; c != nil {
		deploy.Failure = &standupFailure{Category: string(c.Category), LikelyCause: c.LikelyCause, SuggestedAction: c.SuggestedAction}
		if c.LikelyCause != "" {
			deploy.Reason += ": " + c.LikelyCause
		}
	}
	return deploy
}

// refusalText is what a refusal says, for the report: its error or message,
// or its whole text when it has neither.
func refusalText(result *mcp.CallToolResult) string {
	var text strings.Builder
	for _, c := range result.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			text.WriteString(t.Text)
		}
	}
	var body struct {
		Error   string `json:"error"`
		Message string `json:"message"`
		Reason  string `json:"ambiguityReason"`
	}
	if json.Unmarshal([]byte(text.String()), &body) == nil {
		for _, said := range []string{body.Error, body.Message, body.Reason} {
			if said != "" {
				return said
			}
		}
	}
	return text.String()
}

// standupDevServer is a dev half's dev server as the report says it.
type standupDevServer struct {
	State   string `json:"state"`
	Command string `json:"command,omitempty"`
	Port    int    `json:"port,omitempty"`
	Why     string `json:"why,omitempty"`
}

// observeDevServers says, for every dev half that runs code, whether a dev
// server runs on it. None is started here: a dev setup idles on a no-op
// keepalive, and the command that serves the app is recorded nowhere zcp
// reads — only as a comment in a hand-written recipe — so starting one is the
// model's, with the port the dev setup declares. A server zcp already keeps
// (one the model started before) is what a deploy brings back.
func (d standupDeps) observeDevServers(pairs []*standupPair) {
	for _, sp := range pairs {
		if !sp.devDeploy.running() {
			continue
		}
		host := sp.pair.Dev.Hostname
		if r := sp.devResult; r != nil && r.DevServer != nil {
			state := standupDevServerRunning
			if !r.DevServer.Running {
				state = standupDevServerDown
			}
			sp.devServer = &standupDevServer{State: state, Why: r.DevServer.Reason}
			continue
		}
		if kept, err := workflow.KeptDevServerFor(d.batch.stateDir, host); err == nil && kept != nil {
			sp.devServer = &standupDevServer{State: standupDevServerKept, Command: kept.Command, Port: kept.Port}
			continue
		}
		sp.devServer = &standupDevServer{
			State: standupDevServerNotStarted,
			Port:  devSetupPort(projectRootFromState(d.batch.stateDir), host, sp.pair.Dev.Setup),
			Why:   "the dev setup idles on a no-op keepalive, and no dev command is recorded where zcp reads it",
		}
	}
}

// devSetupPort is the first port the dev setup declares in the dev half's
// zerops.yaml, read on its mount; 0 when it is not readable.
func devSetupPort(mountRoot, hostname, setup string) int {
	doc, err := ops.ParseZeropsYml(filepath.Join(mountRoot, hostname))
	if err != nil {
		return 0
	}
	entry := doc.FindEntry(setup)
	if entry == nil || len(entry.Run.Ports) == 0 {
		return 0
	}
	return entry.Run.Ports[0].Port
}
