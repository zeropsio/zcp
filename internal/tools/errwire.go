package tools

import (
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
}

// CheckWire is the wire form of a single check failure. Generic enough
// to carry preflight, verify, and mount checks. The kind discriminator
// tells the agent which tool family produced it. Runnable contract
// fields (PreAttestCmd, ExpectedExit) are preserved when present so the
// agent can re-run the check itself — these come from
// workflow.StepCheck and pre-existed the unification.
type CheckWire struct {
	Kind         string `json:"kind"` // "preflight" | "verify" | "mount"
	Name         string `json:"name"`
	Status       string `json:"status"` // "pass" | "fail" | "skip"
	Detail       string `json:"detail,omitempty"`
	PreAttestCmd string `json:"preAttestCmd,omitempty"` // shell cmd agent can re-run
	ExpectedExit int    `json:"expectedExit,omitempty"` // exit code that means pass
}

// RecoveryHint points the agent at the canonical lifecycle recovery
// surface. P4 contract: status is the single entry point for envelope
// + plan + guidance. The hint never duplicates that data — it only
// names the call.
type RecoveryHint struct {
	Tool   string            `json:"tool"`
	Action string            `json:"action"`
	Args   map[string]string `json:"args,omitempty"`
}

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
