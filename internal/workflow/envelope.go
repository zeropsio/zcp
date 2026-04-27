package workflow

import (
	"time"

	"github.com/zeropsio/zcp/internal/topology"
)

// StateEnvelope captures all state needed to (a) synthesize knowledge,
// (b) produce the next-action plan. It is computed once per tool response
// and embedded verbatim in the response payload. Any two envelopes that
// serialize to the same JSON MUST produce the same synthesis — this is
// the compaction-safety invariant.
//
// Serialization invariant: slices are sorted by stable keys (services by
// hostname, attempts by time). encoding/json already sorts map keys
// alphabetically, so plain json.Marshal is deterministic once slice order
// is controlled at construction.
type StateEnvelope struct {
	Phase        Phase                    `json:"phase"`
	Environment  Environment              `json:"environment"`
	IdleScenario IdleScenario             `json:"idleScenario,omitempty"`
	SelfService  *SelfService             `json:"selfService,omitempty"`
	Project      ProjectSummary           `json:"project"`
	Services     []ServiceSnapshot        `json:"services"`
	WorkSession  *WorkSessionSummary      `json:"workSession,omitempty"`
	Bootstrap    *BootstrapSessionSummary `json:"bootstrap,omitempty"`
	Recipe       *RecipeSessionSummary    `json:"recipe,omitempty"`
	Generated    time.Time                `json:"generated"`
}

// IdleScenario discriminates the sub-cases of PhaseIdle so atoms can
// filter on a single mutually-exclusive value instead of racing on overlapping
// service-count heuristics. Empty when Phase != idle.
type IdleScenario string

const (
	IdleEmpty        IdleScenario = "empty"        // no user services at all
	IdleBootstrapped IdleScenario = "bootstrapped" // at least one bootstrapped service
	IdleAdopt        IdleScenario = "adopt"        // only unmanaged runtimes, none bootstrapped
	// IdleIncomplete fires when at least one ServiceMeta is incomplete AND
	// carries a non-empty BootstrapSession — a prior bootstrap session that
	// was interrupted before writing BootstrappedAt. Resume takes priority
	// over adopt because ZCP already owns the service slot via that session.
	IdleIncomplete IdleScenario = "incomplete"
)

// DeployState marks a bootstrapped service's deploy progression. The
// develop workflow branches on this: never-deployed services enter the
// first-deploy branch (scaffold + write + first deploy + stamp FirstDeployedAt);
// deployed services run the normal edit-loop branch.
type DeployState string

const (
	DeployStateNeverDeployed DeployState = "never-deployed"
	DeployStateDeployed      DeployState = "deployed"
)

// Phase enumerates the lifecycle states the envelope can describe.
type Phase string

const (
	PhaseIdle            Phase = "idle"
	PhaseBootstrapActive Phase = "bootstrap-active"
	PhaseDevelopActive   Phase = "develop-active"
	PhaseDevelopClosed   Phase = "develop-closed-auto"
	PhaseRecipeActive    Phase = "recipe-active"
	PhaseStrategySetup   Phase = "strategy-setup"
	PhaseExportActive    Phase = "export-active"
)

// SelfService names the ZCP host service when running in container environment.
type SelfService struct {
	Hostname string `json:"hostname"`
}

// ProjectSummary identifies the project the envelope describes.
type ProjectSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ServiceSnapshot is one service's point-in-time state inside the envelope.
type ServiceSnapshot struct {
	Hostname      string                  `json:"hostname"`
	TypeVersion   string                  `json:"typeVersion"`
	RuntimeClass  topology.RuntimeClass   `json:"runtimeClass"`
	Status        string                  `json:"status"`
	Bootstrapped  bool                    `json:"bootstrapped"`
	Deployed      bool                    `json:"deployed,omitempty"`
	Resumable     bool                    `json:"resumable,omitempty"`
	Mode          topology.Mode           `json:"mode,omitempty"`
	Strategy      topology.DeployStrategy `json:"strategy,omitempty"`
	Trigger       topology.PushGitTrigger `json:"trigger,omitempty"` // set only when Strategy==push-git
	StageHostname string                  `json:"stageHostname,omitempty"`
}

// WorkSessionSummary mirrors the persistent WorkSession at envelope build time.
type WorkSessionSummary struct {
	Intent      string                   `json:"intent"`
	Services    []string                 `json:"services"`
	CreatedAt   time.Time                `json:"createdAt"`
	ClosedAt    *time.Time               `json:"closedAt,omitempty"`
	CloseReason string                   `json:"closeReason,omitempty"`
	Deploys     map[string][]AttemptInfo `json:"deploys,omitempty"`
	Verifies    map[string][]AttemptInfo `json:"verifies,omitempty"`
}

// AttemptInfo is a single deploy/verify record as projected into the
// envelope. Carries enough diagnostic data that `action="status"`
// reconstructs why an attempt failed without a separate logs round-trip.
//
// `Reason` is the failure message (empty when Success=true). `FailureClass`
// is a typed category populated at the record site where the failure mode
// is known from the operation result; it is informational — the LLM reads
// `Reason` for human content, while `BuildPlan` may key off `FailureClass`
// to phrase a targeted Primary rationale.
//
// `Setup` and `Strategy` are deploy-only; `Summary` is verify-only (the
// brief outcome string, e.g. "healthy" or the failing check name). The
// projection drops empty values via `omitempty` to keep envelope JSON
// stable across attempt types.
type AttemptInfo struct {
	At           time.Time    `json:"at"`
	Success      bool         `json:"success"`
	Iteration    int          `json:"iteration"`
	Setup        string       `json:"setup,omitempty"`
	Strategy     string       `json:"strategy,omitempty"`
	Reason       string       `json:"reason,omitempty"`
	FailureClass FailureClass `json:"failureClass,omitempty"`
	Summary      string       `json:"summary,omitempty"`
}

// FailureClass is the typed category of a failed attempt. Populated at the
// record site where the operation result distinguishes the failure mode.
// Empty when the attempt succeeded.
//
// Categories are intentionally coarse — fine-grained recovery lives in the
// atom corpus + iteration-tier logic, not here. The class drives `BuildPlan`
// rationale phrasing ("build failed; fix and redeploy" vs. "container
// didn't start") and may surface in render text.
type FailureClass string

const (
	// FailureClassBuild — Zerops build pipeline failed (compile, install,
	// dependency resolution).
	FailureClassBuild FailureClass = "build"
	// FailureClassStart — service didn't reach RUNNING / ACTIVE after deploy.
	// READY_TO_DEPLOY post-deploy lands here.
	FailureClassStart FailureClass = "start"
	// FailureClassVerify — verify check failed (HTTP probe, status, logs).
	// Populated by recordVerifyToWorkSession when the check fails.
	FailureClassVerify FailureClass = "verify"
	// FailureClassNetwork — transport-layer error (SSH, API timeout, DNS).
	// The operation could not reach the platform.
	FailureClassNetwork FailureClass = "network"
	// FailureClassConfig — zerops.yaml or service config validation
	// rejected the request.
	FailureClassConfig FailureClass = "config"
	// FailureClassOther — catch-all for failure modes that don't fit a
	// specific category. Reason field still carries the raw message.
	FailureClassOther FailureClass = "other"
)

// RecipeSessionSummary echoes the active recipe match when one exists.
type RecipeSessionSummary struct {
	Slug       string  `json:"slug"`
	Confidence float64 `json:"confidence"`
}

// BootstrapSessionSummary is the bootstrap projection on the envelope used
// for atom filtering. ComputeEnvelope leaves it nil; the bootstrap conductor
// builds a synthetic instance per render in
// bootstrap_guide_assembly.go::synthesisEnvelope from the live BootstrapState.
// Its presence signals that atoms should target a specific bootstrap route
// (recipe/classic/adopt).
//
// Step names the current bootstrap step the agent is working on (discover,
// provision, generate, deploy, close) so atoms can scope themselves to a
// single step. Empty string means "step not applicable" (used outside the
// active-bootstrap conductor).
type BootstrapSessionSummary struct {
	Route       BootstrapRoute `json:"route"`
	Step        string         `json:"step,omitempty"`
	Intent      string         `json:"intent,omitempty"`
	RecipeMatch *RecipeMatch   `json:"recipeMatch,omitempty"`
	Closed      bool           `json:"closed,omitempty"`
}
