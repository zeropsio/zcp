package ops

import (
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// DeployFailurePhase is the lifecycle phase where the failure occurred.
// Independent of Status because some failure phases (transport, preflight)
// never produce a platform-side AppVersion event.
type DeployFailurePhase string

const (
	// PhaseTransport — SSH / zcli could not reach the platform; no build was
	// triggered. Maps to the "result, err := ops.Deploy*" error path.
	PhaseTransport DeployFailurePhase = "transport"
	// PhasePreflight — pre-deploy validation rejected the request before any
	// build started (zerops.yaml validation, deploy-mode contract, missing
	// prerequisites).
	PhasePreflight DeployFailurePhase = "preflight"
	// PhaseBuild — Zerops build pipeline failed (buildCommands, dependency
	// install). Status=BUILD_FAILED.
	PhaseBuild DeployFailurePhase = "build"
	// PhasePrepare — runtime prepare failed (prepareCommands ran before
	// deploy files arrived). Status=PREPARING_RUNTIME_FAILED.
	PhasePrepare DeployFailurePhase = "prepare"
	// PhaseInit — runtime container started but init crashed
	// (run.initCommands, missing modules at runtime, port binding).
	// Status=DEPLOY_FAILED.
	PhaseInit DeployFailurePhase = "init"
)

// FailurePhaseFromStatus maps a platform AppVersion status string to the
// canonical DeployFailurePhase. Returns "" for non-failure statuses.
func FailurePhaseFromStatus(status string) DeployFailurePhase {
	switch status {
	case platform.BuildStatusBuildFailed:
		return PhaseBuild
	case platform.BuildStatusPreparingRuntimeFail:
		return PhasePrepare
	case platform.BuildStatusDeployFailed:
		return PhaseInit
	}
	return ""
}

// FailureInput is the full set of facts ClassifyDeployFailure consumes.
// Callers populate whichever fields they have; the classifier ignores
// missing inputs rather than guessing.
type FailureInput struct {
	// Phase is the lifecycle stage where the failure occurred. Required —
	// the classifier scopes pattern matching by phase.
	Phase DeployFailurePhase
	// Status is the raw platform AppVersion status (BUILD_FAILED, etc.) when
	// known. Empty for transport/preflight failures.
	Status string
	// Strategy identifies the deploy mechanism (push-dev / push-git /
	// manual). Used by credential-class signal matching — push-git auth
	// failures look different from push-dev SSH failures.
	Strategy string
	// BuildLogs is the recent tail of build-container output (from
	// FetchBuildLogs / FetchBuildWarnings).
	BuildLogs []string
	// RuntimeLogs is the recent tail of runtime-container output (from
	// FetchRuntimeLogs).
	RuntimeLogs []string
	// TransportErr is the SSH/zcli/git error returned when no build was
	// ever triggered. Populated for PhaseTransport; nil for post-trigger
	// phases.
	TransportErr error
	// APIErr is the typed PlatformError when the failure originated from a
	// platform API rejection (PhasePreflight, transport-layer SSH errors
	// classified by classifySSHError). Carries APICode / APIMeta / raw
	// SSH output in Diagnostic.
	APIErr *platform.PlatformError
}

// ClassifyDeployFailure inspects a FailureInput and returns the best matching
// DeployFailureClassification, or nil when no classification can be made.
//
// Best-effort by design: when no signal matches, the caller should fall back
// to raw log inspection. The classifier never invents a diagnosis — when in
// doubt it returns nil and lets the agent read logs directly.
//
// Pattern library is in deploy_failure_signals.go. New failure modes get a
// new signal entry there; classification logic stays in this function.
func ClassifyDeployFailure(in FailureInput) *topology.DeployFailureClassification {
	if in.Phase == "" {
		return nil
	}
	for _, sig := range failureSignals() {
		if !sig.appliesToPhase(in.Phase) {
			continue
		}
		if !sig.appliesToStrategy(in.Strategy) {
			continue
		}
		if !sig.matchesAPICode(in.APIErr) {
			continue
		}
		matched := sig.matchLogs(in)
		if matched == "" && sig.requiresLogMatch() {
			continue
		}
		out := sig.build(matched)
		if out == nil {
			continue
		}
		return out
	}

	// No specific signal — fall back to a phase-level baseline so the agent
	// at least sees the category instead of nothing.
	return baselineForPhase(in)
}

// baselineForPhase emits a classification when no specific signal matched
// but the phase itself is informative (BUILD_FAILED with no recognized log
// pattern → category=build, point at buildLogs).
func baselineForPhase(in FailureInput) *topology.DeployFailureClassification {
	switch in.Phase {
	case PhaseBuild:
		return &topology.DeployFailureClassification{
			Category:        topology.FailureClassBuild,
			LikelyCause:     "Build pipeline failed; no recognized log pattern matched.",
			SuggestedAction: "Read buildLogs for the exact stderr; fix buildCommands or dependencies in zerops.yaml.",
			Signals:         []string{"phase:build"},
		}
	case PhasePrepare:
		return &topology.DeployFailureClassification{
			Category:        topology.FailureClassStart,
			LikelyCause:     "run.prepareCommands exited non-zero before deploy files arrived.",
			SuggestedAction: "Read buildLogs for the failing prepare step; check sudo prefix on package installs.",
			Signals:         []string{"phase:prepare"},
		}
	case PhaseInit:
		return &topology.DeployFailureClassification{
			Category:        topology.FailureClassStart,
			LikelyCause:     "Container started but a run.initCommand crashed it.",
			SuggestedAction: "Read runtimeLogs for the failing init step; check env vars / DB connectivity / cache paths.",
			Signals:         []string{"phase:init"},
		}
	case PhaseTransport:
		// Transport baseline only fires when no signal matched. The error
		// itself is the diagnosis — return network without LikelyCause so
		// the agent uses the wrapped APIErr.Diagnostic.
		return &topology.DeployFailureClassification{
			Category:        topology.FailureClassNetwork,
			LikelyCause:     "Transport-layer error reaching the platform.",
			SuggestedAction: "Check the diagnostic field for the underlying SSH/zcli/git error.",
			Signals:         []string{"phase:transport"},
		}
	case PhasePreflight:
		return &topology.DeployFailureClassification{
			Category:        topology.FailureClassConfig,
			LikelyCause:     "Pre-deploy validation rejected the request.",
			SuggestedAction: "Check the apiMeta field for field-level validation reasons.",
			Signals:         []string{"phase:preflight"},
		}
	}
	return nil
}

// failureSignal pairs a matcher against a classification builder. The
// matcher inspects FailureInput; the builder produces the final
// DeployFailureClassification when the signal fires.
type failureSignal struct {
	id            string
	phases        []DeployFailurePhase
	strategies    []string // empty == any
	apiCode       string   // empty == any
	logRegex      *regexp.Regexp
	logSubstrings []string
	requireLog    bool // when true, signal only fires if a log pattern matched
	build         func(match string) *topology.DeployFailureClassification
}

func (s failureSignal) appliesToPhase(p DeployFailurePhase) bool {
	return slices.Contains(s.phases, p)
}

func (s failureSignal) appliesToStrategy(strategy string) bool {
	if len(s.strategies) == 0 {
		return true
	}
	return slices.Contains(s.strategies, strategy)
}

func (s failureSignal) matchesAPICode(pe *platform.PlatformError) bool {
	if s.apiCode == "" {
		return true
	}
	if pe == nil {
		return false
	}
	return pe.Code == s.apiCode || pe.APICode == s.apiCode
}

func (s failureSignal) requiresLogMatch() bool { return s.requireLog }

// matchLogs returns the matched substring (regex group 0 if regex,
// substring otherwise) so the builder can inject it into LikelyCause.
// Empty when nothing matched.
func (s failureSignal) matchLogs(in FailureInput) string {
	hay := joinSearchSpace(in)
	if hay == "" {
		return ""
	}
	if s.logRegex != nil {
		if m := s.logRegex.FindString(hay); m != "" {
			return m
		}
	}
	for _, sub := range s.logSubstrings {
		if sub == "" {
			continue
		}
		if strings.Contains(hay, sub) {
			return sub
		}
	}
	return ""
}

// joinSearchSpace builds the haystack the classifier scans. Includes
// build/runtime logs and any TransportErr / APIErr diagnostic content.
func joinSearchSpace(in FailureInput) string {
	var parts []string
	if len(in.BuildLogs) > 0 {
		parts = append(parts, strings.Join(in.BuildLogs, "\n"))
	}
	if len(in.RuntimeLogs) > 0 {
		parts = append(parts, strings.Join(in.RuntimeLogs, "\n"))
	}
	if in.TransportErr != nil {
		parts = append(parts, in.TransportErr.Error())
		var sshErr *platform.SSHExecError
		if errors.As(in.TransportErr, &sshErr) && sshErr.Output != "" {
			parts = append(parts, sshErr.Output)
		}
	}
	if in.APIErr != nil {
		if in.APIErr.Diagnostic != "" {
			parts = append(parts, in.APIErr.Diagnostic)
		}
		if in.APIErr.Message != "" {
			parts = append(parts, in.APIErr.Message)
		}
	}
	return strings.Join(parts, "\n")
}
