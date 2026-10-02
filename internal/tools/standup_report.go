package tools

import (
	"fmt"
	"strings"

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

	// Roles.
	standupRoleDev           = "dev"
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
	standupDevServerKept       = "kept"
	standupDevServerRunning    = "running"
	standupDevServerDown       = "did not come up"

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
	Branch     string `json:"branch,omitempty"`
	Setup      string `json:"setup,omitempty"`
	// Status is the platform's, for a service the stand-up leaves as it is.
	Status    string            `json:"status,omitempty"`
	Adopted   string            `json:"adopted,omitempty"`
	Wired     string            `json:"wired,omitempty"`
	Failed    string            `json:"failed,omitempty"`
	Deploy    *standupDeploy    `json:"deploy,omitempty"`
	DevServer *standupDevServer `json:"devServer,omitempty"`
	Next      string            `json:"next"`
}

// buildStandupResponse is the report: the pairs, dev half then stage, in the
// order they were deployed, then every service the stand-up left as it is.
func buildStandupResponse(src standupSource, pairs []*standupPair, live map[string]*platform.ServiceStack) standupResponse {
	resp := standupResponse{
		GroupRepo: src.groupRepo,
		Tier:      workflow.MateTierImportPath + "@" + hqBase,
	}
	stood, devs, queued := 0, 0, 0
	names := make([]string, 0, len(pairs))
	for _, sp := range pairs {
		resp.Services = append(resp.Services, standupDevService(sp), standupStageService(sp))
		names = append(names, sp.pair.Dev.Hostname+"→"+sp.pair.Stage.Hostname)
		switch {
		case sp.stoodUp():
			stood++
		case sp.failed == "" && sp.stageDeploy != nil && sp.stageDeploy.Status == standupQueued:
			queued++
		}
		if sp.failed == "" && sp.devDeploy.running() {
			devs++
		}
	}
	for _, s := range src.tier.Skipped {
		resp.Services = append(resp.Services, standupSkippedService(s, live[s.Hostname]))
	}

	switch {
	case stood == len(pairs):
		resp.StandUp = standupReady
		resp.Next = "Every pair stands: each dev half runs main's code on this Mate's branch, and each stage runs main as this Mate's preview. " +
			"Start each dev half's dev server as its next step says if it is not running yet, verify, then tell the person the stages are up, with their URLs. " +
			"Nothing was committed, pushed or proposed."
	case devs == len(pairs) && stood+queued == len(pairs):
		resp.StandUp = standupDevelopment
		resp.Next = standupDevelopmentNext
	case devs == 0 && stood == 0:
		resp.StandUp = standupFailed
		resp.Next = standupStoppedNext(stood, len(pairs), queued)
	default:
		resp.StandUp = standupPartial
		resp.Next = standupStoppedNext(stood, len(pairs), queued)
	}
	resp.Message = fmt.Sprintf("%d of %d pairs stand and %d of %d dev halves run, from %s's %s (%s).",
		stood, len(pairs), devs, len(pairs), src.groupRepo, workflow.MateTierImportPath, strings.Join(names, ", "))
	return resp
}

// standupDevelopmentNext is the model's way on once every dev half stands and
// the stages are queued: development first, then the stages on a second call.
const standupDevelopmentNext = "Every dev half runs main's code on this Mate's branch; the stages are queued. " +
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
		Hostname: p.Dev.Hostname, Role: standupRoleDev, Pair: p.Stage.Hostname,
		Repository: sp.repository, Branch: sp.branch, Setup: p.Dev.Setup,
		Adopted: sp.adopted, Wired: sp.wired, Failed: sp.failed,
		Deploy: sp.devDeploy, DevServer: sp.devServer,
	}
	switch d := sp.devDeploy; {
	case sp.failed != "":
		s.Next = sp.next
	case d == nil:
		s.Next = "Call zerops_standup again."
	case d.running():
		s.Next = standupDevServerNext(p.Dev.Hostname, p.Dev.Setup, sp.devServer)
	default:
		s.Next = standupDeployNext(d, p.Dev.Hostname, fmt.Sprintf("zerops_deploy targetService=%q setup=%q", p.Dev.Hostname, p.Dev.Setup))
	}
	return s
}

func standupStageService(sp *standupPair) standupService {
	p := sp.pair
	s := standupService{
		Hostname: p.Stage.Hostname, Role: standupRoleStage, Pair: p.Dev.Hostname,
		Repository: sp.repository, Setup: p.Stage.Setup, Adopted: sp.adopted,
		Deploy: sp.stageDeploy,
	}
	byHand := fmt.Sprintf("zerops_deploy sourceService=%q targetService=%q setup=%q", p.Dev.Hostname, p.Stage.Hostname, p.Stage.Setup)
	switch d := sp.stageDeploy; {
	case sp.failed != "" && sp.failedHost == p.Stage.Hostname:
		s.Failed = sp.failed
		s.Next = sp.next
	case sp.failed != "":
		s.Deploy = &standupDeploy{Status: standupNotDeployed, Reason: p.Dev.Hostname + " did not stand up, and the stage is built from it"}
		s.Next = sp.next
	case d == nil:
		s.Next = "Call zerops_standup again."
	case d.running():
		s.Next = fmt.Sprintf("Verify it: zerops_verify serviceHostname=%q. It is this Mate's preview of main", p.Stage.Hostname)
		if d.URL != "" {
			s.Next += ", at " + d.URL
		}
		s.Next += "."
	default:
		s.Next = standupDeployNext(d, p.Stage.Hostname, byHand)
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
		return "zcp started the dev server it keeps on it again; verify it: " + verify + "."
	case standupDevServerKept:
		return fmt.Sprintf("zcp keeps its dev server (%s) and brings it back after a restart; check it with zerops_dev_server action=\"status\" hostname=%q port=%d, then %s.",
			ds.Command, hostname, ds.Port, verify)
	case standupDevServerDown:
		return fmt.Sprintf("The dev server zcp keeps on it did not come up (%s): fix the cause, then zerops_dev_server action=\"restart\" hostname=%q, then %s.", ds.Why, hostname, verify)
	}
	port := "<the port it listens on>"
	if ds.Port > 0 {
		port = fmt.Sprint(ds.Port)
	}
	return fmt.Sprintf("Start its dev server: zerops_dev_server action=\"start\" hostname=%q command=\"<the repository's dev command>\" port=%s — "+
		"no dev command is recorded where zcp reads it, so take it from the repository (its package.json scripts, the comments beside the %s setup in zerops.yaml). Then %s.",
		hostname, port, setup, verify)
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
		return "it builds from " + s.Source + " with no partner by zcp's naming — a stage half ends in `stage`, and its dev half is the repository's other runtime or the one named like it"
	case workflow.MateTierSkipManaged, workflow.MateTierSkipPlatformBuild:
	}
	return string(s.Reason)
}
