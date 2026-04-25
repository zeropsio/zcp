package topology

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
// ModeSimple. ModeLocalStage / ModeLocalOnly cover local-machine topologies.
type Mode string

const (
	ModeDev        Mode = "dev"
	ModeStandard   Mode = "standard"
	ModeStage      Mode = "stage"
	ModeSimple     Mode = "simple"
	ModeLocalStage Mode = "local-stage"
	ModeLocalOnly  Mode = "local-only"
)

// DeployStrategy is the developer-chosen deploy mechanism. Use the named
// type on typed-surface code (envelope, plan); the same string values flow
// through persistence so legacy callers and the typed API share one
// vocabulary.
type DeployStrategy string

// StrategyUnset is the envelope sentinel surfaced to atoms as
// `strategies: [unset]`. The other three are the user-selectable deploy
// mechanisms.
//
// StrategyUnset is typed so it can compare directly with
// ServiceMeta.DeployStrategy. The other three are untyped string
// constants so they remain assignable to both the typed workflow
// surface (ServiceMeta.DeployStrategy) and to plain string fields used
// by the deploy tool (DeployAttempt.Strategy uses the deploy-tool wire
// vocabulary, which overlaps with these values).
const StrategyUnset DeployStrategy = "unset"

const (
	StrategyPushDev = "push-dev"
	StrategyPushGit = "push-git"
	StrategyManual  = "manual"
)

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
