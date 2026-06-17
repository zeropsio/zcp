package tools

import (
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// ErrorWire is the canonical JSON shape for every tool error response.
// Composes a platform-layer PlatformError with optional tool-layer
// extensions (multi-check failures, recovery hint). PlatformError stays
// pure platform-layer; this DTO is the layer-4 (entry point) wire form
// that crosses the MCP boundary.
//
// Schema invariant pinned by errwire_contract_test.go:
//   - code + error always present (typed).
//   - envelope/plan never present (P4 contract — status is the recovery primitive).
//   - all extension fields use omitempty so absence-means-empty.
type ErrorWire struct {
	// From PlatformError (always present).
	Code       string                 `json:"code"`
	Error      string                 `json:"error"`
	Suggestion string                 `json:"suggestion,omitempty"`
	APICode    string                 `json:"apiCode,omitempty"`
	Diagnostic string                 `json:"diagnostic,omitempty"`
	APIMeta    []platform.APIMetaItem `json:"apiMeta,omitempty"`

	// Multi-check failures (preflight, verify, mount). Always carries
	// its kind discriminator so the agent can interpret semantics by
	// tool family.
	Checks []CheckWire `json:"checks,omitempty"`

	// Recovery pointer. Small, static, no I/O to compute.
	// Always points at the canonical lifecycle recovery surface.
	Recovery *RecoveryHint `json:"recovery,omitempty"`

	// FailureClassification carries the structured deploy-failure analysis
	// when the error came from a deploy path that classified the
	// transport/preflight phase. Same shape as
	// DeployResult.FailureClassification (see topology pkg).
	// Populated via WithFailureClassification — handlers that aren't
	// deploy-related leave it nil.
	FailureClassification *topology.DeployFailureClassification `json:"failureClassification,omitempty"`

	// WouldDestroy carries the structured "what's about to be erased"
	// payload on a diagnose-before-destruct refusal (ErrDiagnosisRequired).
	// Populated via WithWouldDestroy by destructive-tool handlers that
	// gate on agent acknowledgment. Plan v4 §3.1.
	WouldDestroy *DiagnosedDestruction `json:"wouldDestroy,omitempty"`
}

// CheckWire is the wire form of a single check failure. Generic enough
// to carry preflight, verify, and mount checks. The kind discriminator
// tells the agent which tool family produced it. Runnable contract
// fields (PreAttestCmd, ExpectedExit) are preserved when present so the
// agent can re-run the check itself — these come from
// workflow.StepCheck and pre-existed the unification.
//
// Recovery carries the structured next-tool pointer when the check
// rejected on a recoverable condition (e.g. service in non-running
// terminal status with a diagnose-before-destruct gate). Omitted when
// the check has no actionable recovery beyond "fix and re-run".
type CheckWire struct {
	Kind         string        `json:"kind"` // "preflight" | "verify" | "mount"
	Name         string        `json:"name"`
	Status       string        `json:"status"` // "pass" | "fail" | "skip"
	Detail       string        `json:"detail,omitempty"`
	PreAttestCmd string        `json:"preAttestCmd,omitempty"` // shell cmd agent can re-run
	ExpectedExit int           `json:"expectedExit,omitempty"` // exit code that means pass
	Recovery     *RecoveryHint `json:"recovery,omitempty"`
}

// RecoveryHint is the wire-form alias of topology.Recovery — promoted to
// layer-2 vocabulary so workflow.StepCheck.Recovery and CheckWire.Recovery
// share one struct. P4 contract: status is the canonical lifecycle entry
// point; the hint never duplicates envelope/plan/guidance, it only names
// the call.
type RecoveryHint = topology.Recovery

// ErrorOption configures the wire DTO. Composable.
type ErrorOption func(*ErrorWire)

// WithChecks attaches multi-check failures with a kind discriminator.
// Workflow-layer StepCheck values are converted to the wire form here
// (preserving the layer boundary — workflow types never serialize
// directly).
func WithChecks(kind string, checks []workflow.StepCheck) ErrorOption {
	return func(w *ErrorWire) {
		if len(checks) == 0 {
			return
		}
		out := make([]CheckWire, 0, len(checks))
		for _, c := range checks {
			out = append(out, CheckWire{
				Kind:         kind,
				Name:         c.Name,
				Status:       c.Status,
				Detail:       c.Detail,
				PreAttestCmd: c.PreAttestCmd,
				ExpectedExit: c.ExpectedExit,
				Recovery:     c.Recovery,
			})
		}
		w.Checks = out
	}
}

// WithRecoveryStatus attaches the canonical recovery hint pointing at
// zerops_workflow action="status". Use in every workflow-aware handler
// (the ~18 with engine in scope at the error point per plan §3.6) so
// the agent has an explicit pointer to call status before retrying or
// asking the user.
func WithRecoveryStatus() ErrorOption {
	return WithRecovery(&RecoveryHint{
		Tool:   "zerops_workflow",
		Action: "status",
	})
}

// WithRecovery attaches a custom recovery hint. Reserved for future
// non-status recoveries (e.g. recipe action=status when v3 store
// becomes visible to the registry).
func WithRecovery(hint *RecoveryHint) ErrorOption {
	return func(w *ErrorWire) {
		w.Recovery = hint
	}
}

// WithFailureClassification attaches the structured deploy-failure analysis
// to a transport/preflight error response. nil arg is a no-op so deploy
// handlers can call it unconditionally — `WithFailureClassification(nil)`
// keeps the wire shape free of empty objects.
func WithFailureClassification(c *topology.DeployFailureClassification) ErrorOption {
	return func(w *ErrorWire) {
		if c == nil {
			return
		}
		w.FailureClassification = c
	}
}

// credentialAskMechanism is the single owner of HOW to collect a user-owned
// credential: structured when the harness exposes it, plain text otherwise.
// Every credential-ask TELL derives from it (errwire contract, launch
// credentialsRequired, build-integration local gh strings) so none hard-codes
// "AskUserQuestion" as the only path — a harness that lacks/denies the
// structured ask must not dead-end the agent (matches the atoms' own hedge).
const credentialAskMechanism = "AskUserQuestion when your harness exposes it, else ask the user in plain text"

// credentialUserOwnedContract is appended to the suggestion of every
// credential-class error (B6b). Agents fabricated PATs after a generic probe
// failure in 4 independent battery runs because "re-call with corrected
// inputs" reads like something the agent should produce. The contract names
// the actor explicitly: a token is the USER's secret, never the agent's to
// invent.
const credentialUserOwnedContract = "This credential is a user-held secret — surface this failure and ask the user (" + credentialAskMechanism + ") for a corrected value; NEVER generate, guess, or mutate a token."

// credentialErrorCodes are the platform error codes whose suggestion gets the
// user-owned-credential contract appended. Single owner so every credential
// failure (git-push probe, deploy, build-integration) speaks the same contract.
var credentialErrorCodes = map[string]bool{
	platform.ErrGitTokenInvalid: true,
	platform.ErrGitTokenMissing: true,
}

// appendCredentialContract appends the user-owned-credential contract to the
// wire suggestion when the code is credential-class and the contract is not
// already present (idempotent — handlers may also state it).
func appendCredentialContract(w *ErrorWire) {
	if !credentialErrorCodes[w.Code] {
		return
	}
	if strings.Contains(w.Suggestion, "user-held secret") {
		return
	}
	if w.Suggestion == "" {
		w.Suggestion = credentialUserOwnedContract
		return
	}
	w.Suggestion += " " + credentialUserOwnedContract
}

// platformErrorToWire builds the base ErrorWire from a typed
// PlatformError. Generic errors are wrapped as ErrUnknown by the caller
// before reaching this helper — the wire shape never carries a plain
// string error, every code is typed.
func platformErrorToWire(pe *platform.PlatformError) ErrorWire {
	return ErrorWire{
		Code:       pe.Code,
		Error:      pe.Message,
		Suggestion: pe.Suggestion,
		APICode:    pe.APICode,
		Diagnostic: pe.Diagnostic,
		APIMeta:    pe.APIMeta,
	}
}
