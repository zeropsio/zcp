package port

import (
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
)

// FixClassKind names the deterministic next-action category the port
// deploy-debug loop derives from an observed FailureClass + its signals. It is
// the loop's dispatch vocabulary: the handler reads the agent-observed
// FailureClassification FIRST (never parses logs), maps it through DeriveFixClass,
// and hands the agent the guidance for its next turn. Spec: plan §3 (the
// fix-class dispatch block).
type FixClassKind string

const (
	// FixAddPrepareCommands — a build step invoked a tool the build base lacks
	// (build:command-not-found). Add the install to build.prepareCommands.
	FixAddPrepareCommands FixClassKind = "add-prepare-commands"
	// FixWireDepRefs — the runtime couldn't reach a dependency
	// (init:db-connection-refused). Wire ${dep_*} env refs (and add the managed
	// dep service if it isn't in the topology yet).
	FixWireDepRefs FixClassKind = "wire-dep-refs"
	// FixSetEnv — the runtime is missing a required env var
	// (init:missing-env-var). Set it via zerops_env.
	FixSetEnv FixClassKind = "set-env"
	// FixMoveToInitCommands — a migration ran at the wrong lifecycle phase
	// (init:migration-failed). Move it to run.initCommands behind
	// `zsc execOnce ${appVersionId} --retryUntilSuccessful`.
	FixMoveToInitCommands FixClassKind = "move-to-init-commands"
	// FixGlueYAML — config/validation rejected the request (config class). Fix
	// the glue zerops.yaml and re-validate live. Glue edits are ungated.
	FixGlueYAML FixClassKind = "fix-glue-yaml"
	// FixGitPushSetup — auth/credential rejected (credential class). Chain to
	// action="git-push-setup" to configure git-push capability.
	FixGitPushSetup FixClassKind = "git-push-setup"
	// FixRetry — a transport-layer error (network class). Retry; the platform
	// was unreachable and the build/deploy never ran.
	FixRetry FixClassKind = "retry"
	// FixEscalate — NOT a fix. A failure class the loop cannot tune away in-band
	// (build:oom-killed — build-container resources are not import-tunable). This
	// is the Phase 2 escalation trigger; Phase 1 only flags it.
	FixEscalate FixClassKind = "escalate"
	// FixInspect — no deterministic fix derivable (FailureClassOther / an
	// unrecognized signal). The agent must read diagnostics and decide.
	FixInspect FixClassKind = "inspect"
)

// PortFixClass is the derived next-action guidance for one observed failure. It
// is a PURE function of (FailureClass, signals[]) — table-testable, no I/O. The
// handler records it on the PortSession attempt history and returns Guidance to
// the agent.
type PortFixClass struct {
	// Kind is the deterministic action category.
	Kind FixClassKind `json:"kind"`
	// Guidance is the human-readable instruction for the agent's next turn.
	Guidance string `json:"guidance"`
	// Escalate is true only for FixEscalate — the loop has hit a constraint it
	// cannot tune in-band (build OOM). Phase 2 consumes this as a stall/bail
	// trigger; Phase 1 surfaces it honestly.
	Escalate bool `json:"escalate,omitempty"`
	// RequiresImportOverride is true when the only available fix is an
	// import.yaml edit to an EXISTING hostname (resources, type version, mounts,
	// startWithoutCode) — which trips the import-override gate
	// (ErrDiagnosisRequired from iteration 2 on) and costs an iteration plus a
	// full container/env re-set. The fix-class table PREFERS glue-zerops.yaml
	// edits; this flag fires only when an import edit is unavoidable, so the
	// guidance can warn the agent of the override tax. Spec: plan §11.
	RequiresImportOverride bool `json:"requiresImportOverride,omitempty"`
}

// Signal IDs the port fix-class table dispatches on. They mirror the ops
// classifier signal library (internal/ops/deploy_failure_signals.go) — the loop
// reads them off the live DeployFailureClassification.Signals, never re-derives.
const (
	signalBuildCommandNotFound = "build:command-not-found"
	signalBuildOOMKilled       = "build:oom-killed"
	signalInitDBConnRefused    = "init:db-connection-refused"
	signalInitMissingEnvVar    = "init:missing-env-var"
	signalInitMigrationFailed  = "init:migration-failed"
)

// DeriveFixClass turns an observed (class, signals) pair into deterministic
// next-action guidance. Pure: given the same inputs it always returns the same
// PortFixClass. The agent observes the failure via the EXISTING deploy tools
// (the FailureClassification with its Signals), then passes class + signals to
// the port iterate handler, which calls this. The handler does NOT itself deploy.
//
// The dispatch reads the FailureClass FIRST, then refines on the signal IDs:
//
//	build + command-not-found → add build.prepareCommands install
//	build + oom-killed        → ESCALATE (build resources are not import-tunable)
//	start + db-connection-refused → wire ${dep_*} refs + add managed dep
//	start + missing-env       → zerops_env set
//	start + migration-failed  → move to run.initCommands + zsc execOnce --retryUntilSuccessful
//	config                    → fix glue zerops.yaml + re-validate live
//	credential                → chain to git-push-setup
//	network                   → retry
//	other / unmatched         → inspect diagnostics
func DeriveFixClass(class topology.FailureClass, signals []string) PortFixClass {
	has := func(id string) bool {
		for _, s := range signals {
			if strings.EqualFold(s, id) {
				return true
			}
		}
		return false
	}

	switch class {
	case topology.FailureClassBuild:
		if has(signalBuildOOMKilled) {
			return PortFixClass{
				Kind:     FixEscalate,
				Escalate: true,
				Guidance: "Build OOM-killed. This is NOT a fixable iteration: build-container resources are not import-tunable (build.verticalAutoscaling is schema-unsupported). Treat as an escalation trigger — switch acquisition strategy (source-build → prebuilt-binary) or bail with a partial FitCeiling. Do NOT redeploy unchanged.",
			}
		}
		if has(signalBuildCommandNotFound) {
			return PortFixClass{
				Kind:     FixAddPrepareCommands,
				Guidance: "Build invoked a tool the build base lacks. Add the install to build.prepareCommands in the glue zerops.yaml, then redeploy. This is a glue-zerops.yaml edit (ungated).",
			}
		}
		return PortFixClass{
			Kind:     FixInspect,
			Guidance: "Build failed with no recognized signal. Read buildLogs from the deploy response, identify the failing step, and fix it in build.* of the glue zerops.yaml before redeploying.",
		}

	case topology.FailureClassStart:
		switch {
		case has(signalInitDBConnRefused):
			return PortFixClass{
				Kind:     FixWireDepRefs,
				Guidance: "Runtime could not reach a dependency. Wire the ${dep_*} env references (host/port/credentials) into run.envVariables of the glue zerops.yaml. If the managed dependency service isn't in the topology yet, add it via zerops_import (NEW hostname — additive, ungated), then redeploy.",
			}
		case has(signalInitMissingEnvVar):
			return PortFixClass{
				Kind:     FixSetEnv,
				Guidance: "Runtime is missing a required env var. Set it with zerops_env action=set (or add it to run.envVariables of the glue zerops.yaml), then redeploy. Env set is ungated.",
			}
		case has(signalInitMigrationFailed):
			return PortFixClass{
				Kind:     FixMoveToInitCommands,
				Guidance: "A migration failed at the wrong lifecycle phase. Move it from prepare/build into run.initCommands behind `zsc execOnce ${appVersionId} --retryUntilSuccessful` so it runs once after the container is up and survives the cross-service startup race. Edit the glue zerops.yaml, then redeploy.",
			}
		default:
			return PortFixClass{
				Kind:     FixInspect,
				Guidance: "Runtime failed to start with no recognized signal. Read runtimeLogs from the deploy response, identify why the process exited, and fix run.* in the glue zerops.yaml before redeploying.",
			}
		}

	case topology.FailureClassConfig:
		return PortFixClass{
			Kind:     FixGlueYAML,
			Guidance: "Config/validation rejected the request. Fix the glue zerops.yaml and re-validate live (the deploy pre-flight validates against the platform). Glue-zerops.yaml edits are ungated — prefer them over import.yaml edits, which to an existing hostname require override=true and cost an iteration.",
		}

	case topology.FailureClassCredential:
		return PortFixClass{
			Kind:     FixGitPushSetup,
			Guidance: "Auth/credential rejected (e.g. missing GIT_TOKEN, git remote auth). Chain to zerops_workflow action=\"git-push-setup\" service=<hostname> remoteUrl=<url> gitToken=<pat> to configure git-push capability, then redeploy. Credential failures never escalate the acquisition strategy.",
		}

	case topology.FailureClassNetwork:
		return PortFixClass{
			Kind:     FixRetry,
			Guidance: "Transport-layer error — the platform was unreachable and the build/deploy never ran. Retry the same deploy unchanged. Network failures never escalate the acquisition strategy.",
		}

	case topology.FailureClassVerify, topology.FailureClassOther:
		return PortFixClass{
			Kind:     FixInspect,
			Guidance: "No deterministic fix derivable from this failure class. Read the deploy/verify diagnostics (failureClassification, buildLogs, runtimeLogs) and decide the next action.",
		}

	default:
		return PortFixClass{
			Kind:     FixInspect,
			Guidance: "Unrecognized failure class. Read the deploy diagnostics and decide the next action.",
		}
	}
}

// DeriveImportOverrideFixClass flags the case the §11 budget warns about: when
// the only available fix is an import.yaml edit to an EXISTING hostname
// (resources / type version / mounts / startWithoutCode) that the glue
// zerops.yaml cannot express. It trips the import-override gate
// (ErrDiagnosisRequired from iteration 2 on), costs an iteration, and wipes the
// container/env state on override. The base fix-class table always prefers a
// glue edit; the handler calls this only when the agent reports that the fix is
// inherently an import-level change. Kept distinct from DeriveFixClass so the
// override tax is never silently folded into an ungated glue guidance.
func DeriveImportOverrideFixClass(class topology.FailureClass, signals []string) PortFixClass {
	base := DeriveFixClass(class, signals)
	base.RequiresImportOverride = true
	base.Guidance = "This fix requires an import.yaml edit to an existing hostname, which trips the import-override gate: it needs override=true with a confirmDestructive ack, COSTS an iteration (two-call dance), and WIPES the container/env state (full redeploy + env re-set). Prefer a glue-zerops.yaml edit if at all possible. " + base.Guidance
	return base
}
