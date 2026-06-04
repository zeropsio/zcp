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
		cls := &topology.DeployFailureClassification{
			Category:    topology.FailureClassBuild,
			LikelyCause: "Build pipeline failed; no recognized log pattern matched.",
			Signals:     []string{"phase:build"},
		}
		// H3a: when the build container exits before any logs were
		// captured (sub-10s failures), pointing the agent at buildLogs
		// they don't have is a self-contradiction with the deploy
		// response's hasLogs-aware suggestion field.
		//
		// Empirically (plans/audit-env-vars-20260515/VERIFY-reserved-names.md
		// — probes P2/P9/P13/P25/P26 against eval-zcp 2026-05-16), the
		// dominant zero-logs <10s case is a reserved env-var key in
		// run.envVariables — HOSTNAME, Path, or path — that crashes
		// runtime-init before any build output. ZCP preflight catches
		// this now (CheckReservedEnvNames in deploy_validate.go), so a
		// platform-side hit at this layer typically means raw zcli push
		// or another tool bypassed our gate. Name the trap explicitly
		// instead of pointing the agent at buildCommands bisection.
		if len(in.BuildLogs) > 0 {
			cls.SuggestedAction = "Read buildLogs for the exact stderr; fix buildCommands or dependencies in zerops.yaml."
		} else {
			cls.SuggestedAction = "Build container exited before producing logs (typically <10s). The most common cause is a reserved key in run.envVariables — HOSTNAME, Path, or path — which crashes runtime-init before any build output. Remove that key from zerops.yaml run.envVariables. If those aren't present, re-check buildCommands syntax + manifests. See the develop-reserved-env-names atom for the full reserved-key set."
		}
		return cls
	case PhasePrepare:
		cls := &topology.DeployFailureClassification{
			Category:    topology.FailureClassStart,
			LikelyCause: "run.prepareCommands exited non-zero before deploy files arrived (NOT buildCommands, NOT initCommands).",
			Signals:     []string{"phase:prepare"},
		}
		const prepareCauses = "Common causes: (1) missing sudo — every package install needs sudo " +
			"(e.g. sudo apk add --no-cache pkg), containers run as the zerops user; " +
			"(2) wrong package name — Alpine PHP extensions use a version prefix (php84-pdo_pgsql, NOT php-pgsql; " +
			"php84-ctype, NOT php-ctype); some extensions are built-in since PHP 8.0 (json, tokenizer) — do not install those; " +
			"(3) referencing /var/www/ paths (empty during prepare — use addToRunPrepare + /home/zerops/ instead)."
		if len(in.BuildLogs) > 0 {
			cls.SuggestedAction = "Fix run.prepareCommands (it exited non-zero before deploy files arrived — NOT buildCommands, NOT initCommands). Read buildLogs for the exact error. " + prepareCauses
		} else {
			cls.SuggestedAction = "Fix run.prepareCommands (it exited non-zero — NOT buildCommands, NOT initCommands). Prepare logs were not captured; fetch via zerops_logs serviceHostname={service} severity=ERROR since=5m. " + prepareCauses
		}
		return cls
	case PhaseInit:
		return &topology.DeployFailureClassification{
			Category: topology.FailureClassStart,
			LikelyCause: "A run.initCommand exited non-zero, which aborts the deploy — the new appVersion is " +
				"DEPLOY_FAILED and was not activated; the previous version keeps serving.",
			SuggestedAction: "Diagnose via appVersion.status (DEPLOY_FAILED) + activationDate (null), NOT service.status " +
				"(it stays ACTIVE on the old version). The deploy response's 'error' field names the failing command; " +
				"fetch its stderr from the RUNTIME container (not buildLogs): zerops_logs serviceHostname={service} severity=ERROR since=5m. " +
				"Common causes: a build-time cache that baked /build/source paths (move e.g. `artisan config:cache` from buildCommands to run.initCommands), " +
				"DB connectivity during migration, or missing env vars at container start.",
			Signals: []string{"phase:init"},
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
