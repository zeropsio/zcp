package workflow

import "time"

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

// IdleScenario discriminates the three sub-cases of PhaseIdle so atoms can
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
	Hostname      string         `json:"hostname"`
	TypeVersion   string         `json:"typeVersion"`
	RuntimeClass  RuntimeClass   `json:"runtimeClass"`
	Status        string         `json:"status"`
	Bootstrapped  bool           `json:"bootstrapped"`
	Deployed      bool           `json:"deployed,omitempty"`
	Resumable     bool           `json:"resumable,omitempty"`
	Mode          Mode           `json:"mode,omitempty"`
	Strategy      DeployStrategy `json:"strategy,omitempty"`
	Trigger       PushGitTrigger `json:"trigger,omitempty"` // set only when Strategy==push-git
	StageHostname string         `json:"stageHostname,omitempty"`
}

// RuntimeClass partitions services by how deploy + start behaviour differ.
type RuntimeClass string

const (
	RuntimeDynamic     RuntimeClass = "dynamic"
	RuntimeStatic      RuntimeClass = "static"
	RuntimeImplicitWeb RuntimeClass = "implicit-webserver"
	RuntimeManaged     RuntimeClass = "managed"
	RuntimeUnknown     RuntimeClass = "unknown"
)

// Mode is the bootstrapped service's deploy mode in envelope terms.
// Distinct from the untyped-string meta.Mode ("dev", "standard", "simple"):
// envelope splits the dev half of a standard pair (ModeStandard) from its
// stage half (ModeStage) so atoms can target one role without matching the
// other. Dev-only services get ModeDev; simple-mode single services get
// ModeSimple.
type Mode string

const (
	ModeDev      Mode = "dev"
	ModeStandard Mode = "standard"
	ModeStage    Mode = "stage"
	ModeSimple   Mode = "simple"
)

// DeployStrategy is the developer-chosen deploy mechanism. The string
// values live in service_meta.go so existing untyped-string callers and
// the envelope model share one vocabulary. Use the named type on
// typed-surface code (envelope, plan) and plain strings on persistence.
type DeployStrategy string

// StrategyUnset is the sentinel for "no strategy chosen yet" — surfaced
// to atoms as the `strategies: [unset]` axis value.
const StrategyUnset DeployStrategy = "unset"

// PushGitTrigger is the downstream trigger chosen for push-git services.
// Valid only when DeployStrategy == "push-git". TriggerUnset is the
// envelope sentinel ("unset") that atoms filter on via `triggers: [unset]`
// — a push-git meta whose PushGitTrigger field is still empty string on
// disk surfaces as this value in the snapshot so intro atoms can match.
// Webhook/Actions are the two user-selectable values.
type PushGitTrigger string

const (
	TriggerUnset   PushGitTrigger = "unset"
	TriggerWebhook PushGitTrigger = "webhook"
	TriggerActions PushGitTrigger = "actions"
)

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

// AttemptInfo is a single deploy/verify record.
type AttemptInfo struct {
	At        time.Time `json:"at"`
	Success   bool      `json:"success"`
	Iteration int       `json:"iteration"`
}

// RecipeSessionSummary echoes the active recipe match when one exists.
type RecipeSessionSummary struct {
	Slug       string  `json:"slug"`
	Confidence float64 `json:"confidence"`
}

// BootstrapSessionSummary mirrors the persistent BootstrapSession at envelope
// build time. Populated only when a BootstrapSession file exists for the
// current PID — its presence is the primary signal that atoms should target a
// specific bootstrap route (recipe/classic/adopt).
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
