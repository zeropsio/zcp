package tools

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// The stand-up's answer: one entry per service of the tier, each with what
// happened to it and the model's next call — the report the model carries on
// from, whether the stand-up finished or stopped.

const (
	// standUp states.
	standupReady   = "ready"
	standupPartial = "partial"
	standupFailed  = "failed"
	// standupDevelopment is every dev half standing with the stages queued
	// for the next call.
	standupDevelopment = "development"
	// standupPoolsBuilt is every pair the stand-up builds standing (or none
	// in the tier) and every part's dev pool given its image of main: the
	// parts' stages are the model's, each deployed from a lease of its pool.
	standupPoolsBuilt = "pools-built"

	// Roles.
	standupRoleDev           = "dev"
	standupRoleDevPool       = "dev pool"
	standupRoleStage         = "stage"
	standupRoleManaged       = "managed"
	standupRolePlatformBuild = "built by the platform"
	standupRoleNotStoodUp    = "not stood up"

	// Adopted / wired.
	standupNow     = "now"
	standupAlready = "already"

	// Deploy states.
	standupDeployed        = "deployed"
	standupAlreadyDeployed = "already deployed"
	standupDeployFailed    = "failed"
	standupInFlight        = "still building"
	standupNotDeployed     = "not deployed"
	// standupQueued is a stage whose dev half this call deployed: it builds on
	// the next call.
	standupQueued = "queued"

	// Dev server states.
	standupDevServerNotStarted = "not started"
	standupDevServerRunning    = "running"
	standupDevServerDown       = "did not come up"
	// standupDevServerUnread: zcp could not read the dev half's supervisor.
	standupDevServerUnread = "unread"

	// standupAgain is how every stopped report ends: the call is idempotent.
	standupAgain = "then call zerops_standup again — it continues from here and skips what is done."
)

// standupResponse is zerops_standup's result.
type standupResponse struct {
	StandUp   string                  `json:"standUp"`
	GroupRepo string                  `json:"groupRepo"`
	Tier      string                  `json:"tier"`
	Message   string                  `json:"message"`
	Services  []standupService        `json:"services"`
	Next      string                  `json:"next"`
	Envelope  *workflow.StateEnvelope `json:"envelope,omitempty"`
}

// standupService is one service of the tier after the stand-up.
type standupService struct {
	Hostname   string `json:"hostname"`
	Role       string `json:"role"`
	Pair       string `json:"pair,omitempty"`
	Repository string `json:"repository,omitempty"`
	Setup      string `json:"setup,omitempty"`
	// Status is the platform's, for a service the stand-up leaves as it is.
	Status    string            `json:"status,omitempty"`
	Adopted   string            `json:"adopted,omitempty"`
	Wired     string            `json:"wired,omitempty"`
	Failed    string            `json:"failed,omitempty"`
	Deploy    *standupDeploy    `json:"deploy,omitempty"`
	DevServer *standupDevServer `json:"devServer,omitempty"`
	// CodeVolume is a variant-B dev half's code volume: filled once, and
	// every dev service's supervisor reloaded.
	CodeVolume *standupCodeVolume `json:"codeVolume,omitempty"`
	Next       string             `json:"next"`
}

// buildStandupResponse is the report: the pairs, dev half, further dev
// services, then every stage, in the order they were deployed, then every
// service the stand-up left as it is.
func buildStandupResponse(src standupSource, pairs []*standupPair, live map[string]*platform.ServiceStack) standupResponse {
	resp := standupResponse{
		GroupRepo: src.groupRepo,
		Tier:      hq.RecipeTierPaths[hq.RecipeTierMate] + "@" + hqBase,
	}
	stood, devs, queued := 0, 0, 0
	var pools, built []string
	names := make([]string, 0, len(pairs))
	for _, sp := range pairs {
		if sp.pair.DevPool {
			resp.Services = append(resp.Services, standupPoolService(sp), standupPoolStageService(sp, live))
			names = append(names, sp.pair.Dev.Hostname+"→"+sp.pair.FirstStage().Hostname)
			pools = append(pools, sp.pair.Dev.Hostname)
			if sp.devStands() {
				built = append(built, sp.pair.Dev.Hostname)
			}
			continue
		}
		resp.Services = append(resp.Services, standupDevService(sp))
		if cv := sp.codeVolume; cv != nil {
			for _, ds := range cv.devServices {
				resp.Services = append(resp.Services, standupFurtherDevService(sp, ds))
			}
		}
		for _, st := range sp.stages {
			resp.Services = append(resp.Services, standupStageService(sp, st))
		}
		names = append(names, sp.pair.Dev.Hostname+"→"+strings.Join(stageHostnames(sp.pair), "+"))
		switch {
		case sp.stoodUp():
			stood++
		case sp.stagesQueued():
			queued++
		}
		if sp.devStands() {
			devs++
		}
	}
	for _, s := range src.tier.Skipped {
		resp.Services = append(resp.Services, standupSkippedService(s, live[s.Hostname]))
	}

	containers := len(pairs) - len(pools)
	switch {
	case containers == 0 && len(built) == len(pools):
		resp.StandUp = standupPoolsBuilt
		resp.Next = standupPoolsNext
	case containers == 0:
		resp.StandUp = standupPartial
		if len(built) == 0 {
			resp.StandUp = standupFailed
		}
		resp.Next = fmt.Sprintf("The stand-up stopped short: %d of %d parts' dev pools have their image. Each service's next step names the call for what failed; "+
			"once a cause is fixed, zerops_standup continues from where it stopped and skips what is done.", len(built), len(pools))
		if len(built) > 0 {
			resp.Next += " For the pools that have it: " + standupPoolsNext
		}
	case stood == containers:
		resp.StandUp = standupReady
		resp.Next = "Every pair stands: each dev half runs main's code, and each stage runs main as this Mate's preview. " +
			"Start each dev half's dev server as its next step says if it is not running yet, verify, then tell the person this Mate's preview is up, with its address — a preview with test data, never \"stage\" or \"online\". " +
			"Nothing was committed, pushed or proposed."
	case devs == containers && stood+queued == containers:
		resp.StandUp = standupDevelopment
		resp.Next = standupDevelopmentNext
	case devs == 0 && stood == 0:
		resp.StandUp = standupFailed
		resp.Next = standupStoppedNext(stood, containers, queued)
	default:
		resp.StandUp = standupPartial
		resp.Next = standupStoppedNext(stood, containers, queued)
	}
	if containers > 0 && len(pools) > 0 {
		switch {
		case len(built) < len(pools) && (resp.StandUp == standupReady || resp.StandUp == standupDevelopment):
			resp.StandUp = standupPartial
			resp.Next = standupStoppedNext(stood, containers, queued)
		case resp.StandUp == standupReady:
			resp.StandUp = standupPoolsBuilt
		}
		if len(built) > 0 {
			resp.Next += " For the parts whose dev half is a dev pool: " + standupPoolsNext
		}
	}
	resp.Message = fmt.Sprintf("%d of %d pairs stand and %d of %d dev halves run, from %s's %s (%s).",
		stood, containers, devs, containers, src.groupRepo, hq.RecipeTierPaths[hq.RecipeTierMate], strings.Join(names, ", "))
	if len(pools) > 0 {
		resp.Message += fmt.Sprintf(" %d of %d parts' dev pools have their image of main (%s); each of their stages is deployed from a lease of its pool.",
			len(built), len(pools), strings.Join(built, ", "))
	}
	return resp
}

// standupPoolsNext is the model's way on for the parts whose dev pool has its
// image: each service's next step names the calls.
const standupPoolsNext = "Each such part's pool has its image of main, so it leases now. " +
	"Lease a container of every such pool on main in one call — zerops_lease action=\"acquire\" pools=[…] repo=\"<the repository its pool's next step names>\" branch=\"main\" — " +
	"then, per part, deploy its stage from its lease and verify it as the stage's next step gives, and start the part's dev servers in its lease as the pool's next step says. " +
	"Until its stage is deployed a part has no preview: never tell the person it is up before. " +
	"From then on, work on a part goes through zerops_workflow action=\"start\" workflow=\"develop\" scope=[\"<pool>\"]."

// standupPoolService is a part's dev pool after the stand-up: recorded as the
// pair's key, its first image deployed from main checked out in its stage
// (standup_pool.go) — or what stopped it — and once it has one, the lease
// the part's dev servers run in.
func standupPoolService(sp *standupPair) standupService {
	p := sp.pair
	pool, stage := p.Dev.Hostname, p.FirstStage().Hostname
	s := standupService{
		Hostname: pool, Role: standupRoleDevPool, Pair: stage,
		Repository: sp.repository, Setup: p.Dev.Setup,
		Adopted: sp.adopted, Wired: sp.wired, Deploy: sp.devDeploy,
	}
	byHand := fmt.Sprintf("zerops_deploy sourceService=%q targetService=%q setup=%q sha=\"HEAD\"", stage, pool, p.Dev.Setup)
	switch d := sp.devDeploy; {
	case sp.failed != "" && sp.failedHost == stage:
		s.Deploy = &standupDeploy{Status: standupNotDeployed, Reason: "its first image is deployed from main checked out in " + stage + ", which did not get there"}
		s.Next = sp.next
	case sp.failed != "":
		s.Failed = sp.failed
		s.Next = sp.next
	case d == nil:
		s.Next = standupCallAgain
	case d.running():
		s.Next = fmt.Sprintf("Its image is main's, so it leases: %s takes a container of it on main, whose dev servers are the part's — "+
			"the acquire starts the ones the repository declares; start any other with zerops_dev_server action=\"start\" hostname=\"<the lease's alias>\". "+
			"Every other branch you work on takes a lease of its own.", standupPoolAcquire(sp))
	case d.Status == standupDeployFailed:
		s.Next = fmt.Sprintf("Its first image did not deploy from main (the reason and the logs above): the repository's zerops.yaml needs a setup %q that builds and starts zcp-devsup. "+
			"Fix it on a branch and propose it; once that is merged, call zerops_standup again — it checks main out afresh and continues from here. Or deploy it by hand from main checked out in %s: %s.", p.Dev.Setup, stage, byHand)
	default:
		s.Next = standupDeployNext(d, pool, byHand)
	}
	return s
}

// standupPoolStageService is a pool pair's stage after the stand-up: it holds
// main for the pool's first image and is deployed from a lease of the pool,
// never by the stand-up.
func standupPoolStageService(sp *standupPair, live map[string]*platform.ServiceStack) standupService {
	p := sp.pair
	pool, stage := p.Dev.Hostname, p.FirstStage()
	s := standupService{
		Hostname: stage.Hostname, Role: standupRoleStage, Pair: pool,
		Repository: sp.repository, Setup: stage.Setup, Adopted: sp.adopted,
	}
	switch svc := live[stage.Hostname]; {
	case sp.failed != "" && sp.failedHost == stage.Hostname:
		s.Failed = sp.failed
		s.Next = sp.next
	case sp.failed != "":
		s.Deploy = &standupDeploy{Status: standupNotDeployed, Reason: pool + " did not stand up, and the stage is deployed from a lease of it"}
		s.Next = sp.next
	case svc != nil && svc.HasDeployedCode():
		s.Deploy = &standupDeploy{Status: standupAlreadyDeployed}
		s.Next = fmt.Sprintf("Verify it: zerops_verify serviceHostname=%q. It is this Mate's preview of %s's part.", stage.Hostname, pool)
	case sp.devStands():
		s.Deploy = &standupDeploy{Status: standupNotDeployed, Reason: "a part's stage is deployed from a lease of its pool; until then it holds main's checkout and runs no build"}
		s.Next = fmt.Sprintf("Deploy it from a lease of %s on main (%s): zerops_deploy sourceService=\"<the lease's alias>\" targetService=%q setup=%q, then verify it: zerops_verify serviceHostname=%q. It is this Mate's preview of %s's part.",
			pool, standupPoolAcquire(sp), stage.Hostname, stage.Setup, stage.Hostname, pool)
	default:
		s.Deploy = &standupDeploy{Status: standupNotDeployed, Reason: pool + " has no image yet, and the stage is deployed from a lease of it"}
		s.Next = fmt.Sprintf("Once %s has its image (its next step), deploy it from a lease of %s on main: zerops_deploy sourceService=\"<the lease's alias>\" targetService=%q setup=%q.",
			pool, pool, stage.Hostname, stage.Setup)
	}
	return s
}

// standupPoolAcquire is the call that leases a container of the pair's pool
// on main, naming the repository on the pool's first acquire.
func standupPoolAcquire(sp *standupPair) string {
	repo := sp.remote
	if repo == "" {
		repo = sp.pair.Repository
	}
	return fmt.Sprintf("zerops_lease action=\"acquire\" pool=%q repo=%q branch=\"main\"", sp.pair.Dev.Hostname, repo)
}

// standupCallAgain is the next step of a half the call left undecided.
const standupCallAgain = "Call zerops_standup again."

// standupDevelopmentNext is the model's way on once every dev half stands and
// the stages are queued: development first, then the stages on a second call.
const standupDevelopmentNext = "Every dev half runs main's code; the stages are queued. " +
	"Start each dev half's dev server with zerops_dev_server as its next step says, verify it, and tell the person development is up, with each dev half's address. " +
	"Then call zerops_standup again in this turn: it builds the stages — this Mate's preview of main — each after its dev half and what its build reads, and waits for them. " +
	"Until that call the stages wait for their first deploy (READY_TO_DEPLOY), which breaks nothing. Nothing was committed, pushed or proposed."

// standupStoppedNext is the model's way on from a stand-up that stopped short:
// zcp's own tools, from each service's next step, and the stand-up again once
// a cause is fixed.
func standupStoppedNext(stood, pairs, queued int) string {
	next := fmt.Sprintf("The stand-up stopped short: %d of %d pairs stand. Carry on in this turn with zcp's own tools — each service's next step names the call "+
		"(adopt by hand: zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\"; deploy: zerops_deploy_batch; dev servers: zerops_dev_server). "+
		"Once a cause is fixed, zerops_standup continues from where it stopped and skips what is done.", stood, pairs)
	if stood > 0 || queued > 0 {
		next += " For the dev halves that run, start each dev server as its next step says."
	}
	if queued > 0 {
		next += " The stages marked queued build on the next zerops_standup call."
	}
	return next
}

func standupDevService(sp *standupPair) standupService {
	p := sp.pair
	s := standupService{
		Hostname: p.Dev.Hostname, Role: standupRoleDev, Pair: p.FirstStage().Hostname,
		Repository: sp.repository, Setup: p.Dev.Setup,
		Adopted: sp.adopted, Wired: sp.wired, Failed: sp.failed,
		Deploy: sp.devDeploy, DevServer: sp.devServer, CodeVolume: sp.codeVolume,
	}
	switch d := sp.devDeploy; {
	case sp.failed != "":
		s.Next = sp.next
	case d == nil:
		s.Next = standupCallAgain
	case d.running():
		s.Next = standupDevServerNext(p.Dev.Hostname, p.Dev.Setup, sp.devServer)
	default:
		s.Next = standupDeployNext(d, p.Dev.Hostname, fmt.Sprintf("zerops_deploy targetService=%q setup=%q", p.Dev.Hostname, p.Dev.Setup))
	}
	return s
}

func standupStageService(sp *standupPair, st *standupStage) standupService {
	p := sp.pair
	stage := st.rt
	s := standupService{
		Hostname: stage.Hostname, Role: standupRoleStage, Pair: p.Dev.Hostname,
		Repository: sp.repository, Setup: stage.Setup, Adopted: sp.adopted,
		Deploy: st.deploy,
	}
	byHand := fmt.Sprintf("zerops_deploy sourceService=%q targetService=%q setup=%q", p.Dev.Hostname, stage.Hostname, stage.Setup)
	switch d := st.deploy; {
	case sp.failed != "" && sp.failedHost == stage.Hostname:
		s.Failed = sp.failed
		s.Next = sp.next
	case sp.failed != "":
		s.Deploy = &standupDeploy{Status: standupNotDeployed, Reason: p.Dev.Hostname + " did not stand up, and the stage is built from it"}
		s.Next = sp.next
	case d == nil:
		s.Next = standupCallAgain
	case d.running():
		s.Next = fmt.Sprintf("Verify it: zerops_verify serviceHostname=%q. It is this Mate's preview of main", stage.Hostname)
		if d.URL != "" {
			s.Next += ", at " + d.URL
		}
		s.Next += "."
	default:
		s.Next = standupDeployNext(d, stage.Hostname, byHand)
	}
	return s
}

// standupDeployNext is the next step for a half that does not run code.
func standupDeployNext(d *standupDeploy, hostname, byHand string) string {
	switch d.Status {
	case standupQueued:
		return fmt.Sprintf("It is queued: %s. Until then it waits for its first deploy (READY_TO_DEPLOY). Or deploy it by hand: %s.", d.Reason, byHand)
	case standupInFlight:
		return fmt.Sprintf("Wait for its build: zerops_process action=\"wait\" service=%q, %s", hostname, standupAgain)
	case standupDeployFailed:
		fix := "Read the reason and the logs above, fix the cause in the dev half's checkout"
		if d.Failure != nil && d.Failure.SuggestedAction != "" {
			fix = d.Failure.SuggestedAction + " Fix the cause in the dev half's checkout"
		} else if d.next != "" {
			fix = d.next + " Fix the cause in the dev half's checkout"
		}
		return fmt.Sprintf("%s, %s Or deploy it by hand: %s.", fix, standupAgain, byHand)
	default:
		return fmt.Sprintf("It waits: %s. Once that stands, %s Or deploy it by hand: %s.", d.Reason, standupAgain, byHand)
	}
}

// standupDevServerNext is a running dev half's next step: its dev server.
func standupDevServerNext(hostname, setup string, ds *standupDevServer) string {
	verify := fmt.Sprintf("zerops_verify serviceHostname=%q", hostname)
	if ds == nil {
		return "Verify it: " + verify + "."
	}
	switch ds.State {
	case standupDevServerRunning:
		return "Its supervisor runs the dev server; verify it: " + verify + "."
	case standupDevServerDown:
		return fmt.Sprintf("The dev server its supervisor runs did not come up (%s): fix the cause, then zerops_dev_server action=\"restart\" hostname=%q, then %s.", ds.Why, hostname, verify)
	case standupDevServerUnread:
		return fmt.Sprintf("zcp could not read its dev servers (%s): zerops_dev_server action=\"status\" hostname=%q says whether one runs; start it if none does, then %s.", ds.Why, hostname, verify)
	}
	port := "<the port it listens on>"
	if ds.Port > 0 {
		port = fmt.Sprint(ds.Port)
	}
	return fmt.Sprintf("Start its dev server: zerops_dev_server action=\"start\" hostname=%q command=\"<the repository's dev command>\" port=%s — "+
		"no dev command is recorded where zcp reads it, so take it from the repository (its package.json scripts, the comments beside the %s setup in zerops.yaml). Then %s.",
		hostname, port, setup, verify)
}

// standupFurtherDevService is a further dev service of a variant-B pair: its
// own dev setup deployed from the dev half's checkout.
func standupFurtherDevService(sp *standupPair, ds *standupDevServiceRun) standupService {
	dev := sp.pair.Dev.Hostname
	s := standupService{
		Hostname: ds.ref.Hostname, Role: standupRoleDev, Pair: dev,
		Repository: sp.repository, Setup: ds.ref.SetupName,
		Deploy: ds.deploy,
	}
	switch d := ds.deploy; {
	case sp.failed != "":
		s.Deploy = &standupDeploy{Status: standupNotDeployed, Reason: dev + " did not stand up, and its dev setup is deployed from it"}
		s.Next = sp.next
	case d == nil:
		s.Next = standupCallAgain
	case d.running():
		s.Next = fmt.Sprintf("Its dev servers run from the code volume %s by its supervisor; read them with zerops_dev_server action=\"status\" hostname=%q.", sp.codeVolume.volume.Hostname, ds.ref.Hostname)
	default:
		s.Next = standupDeployNext(d, ds.ref.Hostname, fmt.Sprintf("zerops_deploy sourceService=%q targetService=%q setup=%q workingDir=%q", dev, ds.ref.Hostname, ds.ref.SetupName, ops.DefaultWorkingDir))
	}
	return s
}

// standupSkippedService is a service of the tier the stand-up leaves as it is.
func standupSkippedService(s workflow.MateTierSkip, svc *platform.ServiceStack) standupService {
	out := standupService{Hostname: s.Hostname, Status: "not in this project"}
	if svc != nil {
		out.Status = svc.Status
	}
	switch s.Reason {
	case workflow.MateTierSkipManaged:
		out.Role = standupRoleManaged
		out.Next = "Nothing to stand up: the platform created it at import, and the pairs reach it through its variables."
	case workflow.MateTierSkipPlatformBuild:
		out.Role = standupRolePlatformBuild
		out.Next = "Nothing to stand up: the platform builds it at import from " + s.Source + "."
		if svc == nil || !svc.IsLive() {
			out.Next += fmt.Sprintf(" It is %s — read what happened with zerops_events serviceHostname=%q.", out.Status, s.Hostname)
		}
	case workflow.MateTierSkipNoRepository, workflow.MateTierSkipForeign, workflow.MateTierSkipUnpaired:
		out.Role = standupRoleNotStoodUp
		out.Failed = standupSkipReason(s)
		out.Next = "The stand-up leaves it as it is. If the task needs it, adopt it by hand: zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\"."
	case workflow.MateTierSkipPool:
		out.Role = standupRoleNotStoodUp
		out.Failed = standupSkipReason(s)
		out.Next = "The stand-up leaves it as it is: a Swarm pool is no runtime to adopt. Leave it to the app that calls its orchestrator; if it is a part's dev pool, its stage must be named like it in the recipe (`apipool` beside `apistage`)."
	}
	return out
}

// standupSkipReason says why a service of the tier is no pair to stand up.
func standupSkipReason(s workflow.MateTierSkip) string {
	switch s.Reason {
	case workflow.MateTierSkipNoRepository:
		return "the tier names no repository for it, so there is no code to stand it up from"
	case workflow.MateTierSkipForeign:
		return "it builds from " + s.Source + ", a repository of another application than this Mate's, which HQ neither gives nor joins it"
	case workflow.MateTierSkipUnpaired:
		return "it builds from " + s.Source + " with no partner by zcp's naming — a stage half ends in `stage`, and its dev half is the repository's other runtime, the one named like it, or its part's dev pool named like it (`apistage` → `apipool`)"
	case workflow.MateTierSkipPool:
		return "it is a Swarm pool no stage of the tier pairs with — a part's dev pool is named like its stage (`apistage` → `apipool`), and a pool no stage takes is infrastructure an app uses"
	case workflow.MateTierSkipManaged, workflow.MateTierSkipPlatformBuild:
	}
	return string(s.Reason)
}
