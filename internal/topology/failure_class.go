package topology

// FailureClass is the typed category of a failed deploy/verify attempt.
// Coarse on purpose — fine-grained recovery lives in atom corpus + iteration
// logic. The class drives BuildPlan rationale phrasing ("build failed; fix
// and redeploy" vs. "container didn't start") and is one field of the
// richer DeployFailureClassification surfaced to the agent.
//
// Lives in topology so ops + workflow can both reference one enum without
// either importing the other (they are peer layer-3 packages — see
// docs/spec-architecture.md).
type FailureClass string

const (
	// FailureClassBuild — Zerops build pipeline failed (compile, install,
	// dependency resolution, buildCommands).
	FailureClassBuild FailureClass = "build"
	// FailureClassStart — runtime didn't reach RUNNING / ACTIVE after
	// deploy. PREPARING_RUNTIME_FAILED, READY_TO_DEPLOY post-deploy, and
	// DEPLOY_FAILED all map here — recovery is "fix run.* in zerops.yaml
	// or runtime config", not "fix the build pipeline".
	FailureClassStart FailureClass = "start"
	// FailureClassVerify — verify check failed (HTTP probe, status, logs).
	// Populated by recordVerifyToWorkSession when the check fails.
	FailureClassVerify FailureClass = "verify"
	// FailureClassNetwork — transport-layer error (SSH connection refused,
	// API timeout, DNS failure). The operation could not reach the
	// platform; the build/deploy never ran.
	FailureClassNetwork FailureClass = "network"
	// FailureClassConfig — zerops.yaml or service config validation
	// rejected the request (bad YAML, schema violation, missing setup
	// entry, deploy-mode contract violation).
	FailureClassConfig FailureClass = "config"
	// FailureClassCredential — auth/credential rejected: missing GIT_TOKEN,
	// invalid zcli login, .netrc/SSH-key auth failure on git remote. Split
	// from FailureClassNetwork because the recovery is "fix credentials",
	// not "fix connectivity".
	FailureClassCredential FailureClass = "credential"
	// FailureClassOther — catch-all for failure modes that don't fit a
	// specific category. Reason field still carries the raw message.
	FailureClassOther FailureClass = "other"
)

// DeployFailureClassification is the structured failure summary attached to
// failed deploy responses (DeployResult.FailureClassification, ErrorWire.
// FailureClassification). The agent reads this instead of parsing logs to
// pick a recovery — see ticket E2 in plans/engine-atom-rendering-
// improvements-2026-04-27.md.
//
// All fields are best-effort. When the classifier cannot match a known
// pattern, Category is FailureClassOther, LikelyCause is empty, and
// SuggestedAction points at the raw logs. The field's presence is the
// signal that classification ran; absence means the upstream caller
// hadn't classified yet.
type DeployFailureClassification struct {
	// Category is the coarse class (build/start/verify/network/config/
	// credential/other). Stable across releases.
	Category FailureClass `json:"category"`
	// LikelyCause is a one-sentence diagnosis ("Build OOM-killed",
	// "Port already in use", "GIT_TOKEN missing"). Empty when no
	// pattern matched.
	LikelyCause string `json:"likelyCause,omitempty"`
	// SuggestedAction is one concrete next step the agent should take —
	// a tool call, a YAML edit, or a check. Empty when no pattern
	// matched. Never a multi-step recovery — atoms own those.
	SuggestedAction string `json:"suggestedAction,omitempty"`
	// Signals is the list of signal IDs that matched (e.g.
	// "build:command-not-found", "runtime:port-in-use"). Lets the
	// classifier evolve under tests without churning the message text.
	Signals []string `json:"signals,omitempty"`
}
