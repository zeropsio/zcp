package tools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// DiagnosedDestruction is the structured "what's about to be erased"
// payload attached to an ErrDiagnosisRequired error response. Empirical
// scope today is import-override; the shape is intentionally minimal
// (operation/targets/loss) so adding gates to other always-dangerous
// tools (env-set, env-delete, generate-dotenv) is a 30-LOC delta when
// evidence emerges. Plan v4 §3.1.
type DiagnosedDestruction struct {
	// Operation names the destructive action class. Canonical values:
	//   "import-override" — import with override=true tears down matching
	//                       service stacks (containers, deployed code,
	//                       env vars, SSHFS mounts).
	// Future: "env-set", "env-delete", "generate-dotenv".
	Operation string `json:"operation"`
	// Targets are the service hostnames whose state would be erased.
	// Set comparison against confirmDestructive.AcknowledgedTargets.
	Targets []string `json:"targets"`
	// Loss describes what categories of state vanish. Categories are
	// additive — new destructive tools populate the relevant sub-fields.
	Loss DestructionLoss `json:"wouldDestroy"`
}

// DestructionLoss enumerates the categories of state a destructive
// operation removes. Empty fields are omitted from JSON so the wire
// shape is compact.
type DestructionLoss struct {
	ServiceStacks   []string `json:"serviceStacks,omitempty"`
	EnvVars         []string `json:"envVars,omitempty"`
	LocalFiles      []string `json:"localFiles,omitempty"`
	UncommittedCode bool     `json:"uncommittedCode,omitempty"`
}

// DestructiveAck is the agent-supplied acknowledgment that a destructive
// operation may proceed. Sent on the second call after the first call
// returns ErrDiagnosisRequired with a DiagnosedDestruction payload. Set
// comparison on AcknowledgedTargets — order doesn't matter, but every
// target the handler diagnoses must appear (and no extras).
//
// DiagnosedFailureClass is optional today (Phase 3.2 ships it without
// hard enforcement); Phase 4 escalates to required-match if the eval
// suggests agents are treating the ack as a checkbox. The class string
// matches topology.FailureClass values ("build", "start", etc.).
type DestructiveAck struct {
	Operation             string   `json:"operation"`
	AcknowledgedTargets   []string `json:"acknowledgedTargets"`
	DiagnosedFailureClass string   `json:"diagnosedFailureClass,omitempty"`
}

// ValidateDestructiveAck returns nil when ack matches expected; otherwise
// a typed PlatformError carrying ErrDiagnosisRequired so callers can
// convert it through the existing error-wire pipeline. Callers attach a
// WouldDestroy block to the error response separately (see WithWouldDestroy).
//
// Match semantics:
//   - Operation must equal expected.Operation exactly.
//   - AcknowledgedTargets must be a set-equal match for expected.Targets
//     (order-insensitive, no extras, no missing entries).
//   - DiagnosedFailureClass is NOT enforced here in Phase 3.1; Phase 4
//     escalates if the eval suggests agents skip diagnosis.
func ValidateDestructiveAck(ack *DestructiveAck, expected DiagnosedDestruction) error {
	if ack == nil {
		return platform.NewPlatformError(
			platform.ErrDiagnosisRequired,
			fmt.Sprintf("%s requires confirmDestructive after diagnosis", expected.Operation),
			suggestionForFirstCallRefusal(expected),
		)
	}
	if ack.Operation != expected.Operation {
		return platform.NewPlatformError(
			platform.ErrDiagnosisRequired,
			fmt.Sprintf("confirmDestructive.operation=%q does not match expected %q", ack.Operation, expected.Operation),
			"Set confirmDestructive.operation to the operation field on the wouldDestroy payload.",
		)
	}
	if !targetSetsEqual(ack.AcknowledgedTargets, expected.Targets) {
		return platform.NewPlatformError(
			platform.ErrDiagnosisRequired,
			fmt.Sprintf("confirmDestructive.acknowledgedTargets %v does not match expected %v", sortedCopy(ack.AcknowledgedTargets), sortedCopy(expected.Targets)),
			"Acknowledge every target on the wouldDestroy.targets list, no extras.",
		)
	}
	return nil
}

// targetSetsEqual returns true when a and b contain the same hostnames
// regardless of order, with no duplicates affecting the comparison.
func targetSetsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sa := sortedCopy(a)
	sb := sortedCopy(b)
	for i := range sa {
		if sa[i] != sb[i] {
			return false
		}
	}
	return true
}

func sortedCopy(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	sort.Strings(out)
	return out
}

// WithWouldDestroy attaches a structured DiagnosedDestruction block to the
// error response. Used by destructive-tool handlers (zerops_import
// override, future env-set/delete) to populate the first-call refusal so
// the agent has a machine-readable description of what would be erased.
func WithWouldDestroy(d *DiagnosedDestruction) ErrorOption {
	return func(w *ErrorWire) {
		if d == nil {
			return
		}
		// Defensive copy on Targets so handler-side mutation can't reach
		// the wire shape.
		copyTargets := make([]string, len(d.Targets))
		copy(copyTargets, d.Targets)
		out := *d
		out.Targets = copyTargets
		w.WouldDestroy = &out
	}
}

// JoinTargets returns a human-readable comma-separated list of targets.
// Used by error-message construction in destructive-tool handlers.
func (d DiagnosedDestruction) JoinTargets() string {
	return strings.Join(d.Targets, ", ")
}

// suggestionForFirstCallRefusal builds the agent-facing suggestion string
// for the ack==nil case: a one-line restate of "read logs first" plus a
// copy-paste JSON snippet of the next tool call. Pre-fix the agent had to
// hand-construct the ack payload from the wouldDestroy shape; reading the
// suggestion now produces a directly executable retry.
//
// Operation→tool mapping is centralized so each new destructive operation
// (env-set, env-delete, ...) drops in one switch arm and the snippet
// stays in sync with the actual handler signature.
func suggestionForFirstCallRefusal(expected DiagnosedDestruction) string {
	const fallback = "Read zerops_logs / zerops_events for the targets, then re-call with confirmDestructive matching wouldDestroy."
	tool, paramPrefix := retryShapeFor(expected.Operation)
	if tool == "" {
		return fallback
	}
	type ackPayload struct {
		Operation           string   `json:"operation"`
		AcknowledgedTargets []string `json:"acknowledgedTargets"`
	}
	blob, err := json.Marshal(ackPayload{
		Operation:           expected.Operation,
		AcknowledgedTargets: append([]string{}, expected.Targets...),
	})
	if err != nil {
		return fallback
	}
	return fmt.Sprintf("After reading logs, retry with: %s %sconfirmDestructive=%s", tool, paramPrefix, string(blob))
}

// retryShapeFor maps an Operation token to the tool call shape used by
// the suggestion text. Empty tool means "no recipe yet for this
// operation" — caller falls back to generic guidance.
//
// Add new destructive operations here as they land (env-set, env-delete).
func retryShapeFor(operation string) (tool, paramPrefix string) {
	shape, ok := retryShapes[operation]
	if !ok {
		return "", ""
	}
	return shape.tool, shape.paramPrefix
}

var retryShapes = map[string]struct {
	tool        string
	paramPrefix string
}{
	importOverrideOperation: {tool: "zerops_import", paramPrefix: "override=true "},
	launchResetOperation:    {tool: "zerops_workflow", paramPrefix: `action="reset" workflow="launch-production" productionProjectName="<your-target-name>" `},
}
