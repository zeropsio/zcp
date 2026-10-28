package tools

import (
	"strings"

	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// resolvedLaunchRuntime is the per-runtime internal shape the handler
// passes through the source-control gate, source-state read, and
// composer. Holds both the user's choice hostname (for UX continuity)
// and the canonical dev-half push hostname (= ServiceMeta primary key)
// plus the derived production-side hostname.
type resolvedLaunchRuntime struct {
	// ChoiceHostname is the user-supplied hostname (may be either
	// half of a standard pair).
	ChoiceHostname string
	// PushHostname is the canonical dev-half (= ServiceMeta primary
	// key) — the runtime whose /var/www holds the working tree.
	PushHostname string
	// ProdHostname is the production runtime name embedded in the
	// import.yaml. Derived from PushHostname via
	// derivePromotedProdHostname (stripping mode-suffix) unless the
	// promotable carried an explicit ProdHostname override.
	ProdHostname string
	// SetupName is the zerops.yaml setup block this runtime resolves
	// in production. Resolved per the resolveLaunchSetupName cascade
	// (overrides → meta.ProdSetupName → the stage's setup →
	// meta.PrimarySetupName → legacy "prod" default).
	SetupName string
	// SetupProvenance names which cascade source produced SetupName
	// (setupProvenance* constants). Display/consent metadata: the
	// ready-to-launch preview surfaces it and raises the confirm hint
	// for dev-promoted / legacy-default resolutions.
	SetupProvenance string
	// DevIsPool marks a runtime whose pair is keyed by its dev pool
	// (ServiceMeta.DevIsPool): PushHostname then names the pool, which has
	// no checkout to read — the source is the pair's origin instead
	// (docs/spec-workflows.md §10 P-LP-17, P-LP-19; §13 PM-10).
	DevIsPool bool
	// RuntimeHostname is the live runtime the production runtime is
	// promoted from — its type, user env and scale — and the service the
	// launch token is staged on: PushHostname, or for a pair keyed by its
	// dev pool the stage the call named, else the pair's first. Never the
	// pool, whose every container — someone's lease among them — reads the
	// pool's service env. "" for a pool-keyed pair with no stage, which the
	// gate refuses before any read.
	RuntimeHostname string
}

// promotableHostnameSuffixes is the canonical list of mode suffixes
// the production-hostname derivation strips. Order matters: longer
// suffixes first so "appstage" doesn't accidentally match "-stage"
// when "-staging" is the intended longer form.
//
// Per Karel decision 2026-05-20: `appstage`/`appdev` → `app`,
// `workerstage` → `worker`. Additional suffixes (development, staging)
// covered for symmetry with deriveProductionProjectName.
//
//nolint:gochecknoglobals // immutable lookup table; initialized constant
var promotableHostnameSuffixes = []string{
	"-development",
	"-staging",
	"-stage",
	"-worker", // promotes `worker-dev`/`workerstage` → `worker` when authors used hyphenated suffix
	"-dev",
	"stage", // unhyphenated tail (workerstage, appstage)
	"dev",   // unhyphenated tail (workerdev, appdev)
}

// derivePromotedProdHostname strips the canonical mode suffix from a
// source-side hostname, returning the production runtime name.
//
// Examples:
//
//	appstage    → app
//	appdev      → app
//	workerstage → worker
//	app         → app   (no suffix to strip — passthrough)
//	bun         → bun   (no suffix — passthrough)
//
// Empty input returns empty (caller decides what to do).
func derivePromotedProdHostname(hostname string) string {
	if hostname == "" {
		return ""
	}
	for _, suffix := range promotableHostnameSuffixes {
		if trimmed, ok := strings.CutSuffix(hostname, suffix); ok && trimmed != "" {
			return trimmed
		}
	}
	return hostname
}

// resolveLaunchRuntimes builds the per-runtime resolved shape for
// every promotable named in WorkflowInput.Promotables. When the input
// is empty AND TargetService is set, falls back to a single-runtime
// resolution from TargetService (legacy / single-runtime launch path
// preserved for tests and single-runtime user intent).
//
// The resolver:
//   - Normalizes the user's hostname through pair-keyed
//     ServiceMeta lookup (workflow.FindServiceMeta) so stage-half
//     input lands on the canonical dev-half meta.
//   - Derives the prod hostname unless the promotable carries an
//     override.
//   - Resolves the setup-block name via the plan §P5 cascade (see
//     resolvedLaunchRuntime.SetupName doc); empty result is the
//     legitimate "blocker" signal the handler projects into a
//     requiresSetupInput / scope-prompt response.
//
// Returns nil + an empty list when no runtime is resolvable (caller
// emits the scope-prompt blocker for missing targetService).
func resolveLaunchRuntimes(stateDir string, input WorkflowInput) []resolvedLaunchRuntime {
	promotables := input.Promotables
	if len(promotables) == 0 && input.TargetService != "" {
		promotables = []LaunchPromotableInput{{
			Hostname: input.TargetService,
		}}
	}
	if len(promotables) == 0 {
		return nil
	}
	workflowOverride := strings.TrimSpace(input.ProdSetupNameOverride)
	out := make([]resolvedLaunchRuntime, 0, len(promotables))
	for _, p := range promotables {
		if p.Hostname == "" {
			continue
		}
		pushHost := p.Hostname
		var meta *workflow.ServiceMeta
		if loaded, err := workflow.FindServiceMeta(stateDir, p.Hostname); err == nil && loaded != nil {
			meta = loaded
			if isStageOf(meta, p.Hostname) && meta.Hostname != "" {
				pushHost = meta.Hostname
			}
		}
		runtimeHost := pushHost
		if meta.DevIsPool() {
			stage, ok := meta.StageFor(p.Hostname)
			if !ok {
				stage = meta.FirstStage()
			}
			runtimeHost = stage.Hostname
		}
		prodHost := strings.TrimSpace(p.ProdHostname)
		if prodHost == "" {
			prodHost = derivePromotedProdHostname(runtimeHost)
		}
		if prodHost == "" {
			prodHost = pushHost
		}
		setupName, setupProvenance := resolveLaunchSetupName(p, workflowOverride, meta)
		out = append(out, resolvedLaunchRuntime{
			ChoiceHostname:  p.Hostname,
			PushHostname:    pushHost,
			ProdHostname:    prodHost,
			SetupName:       setupName,
			SetupProvenance: setupProvenance,
			DevIsPool:       meta.DevIsPool(),
			RuntimeHostname: runtimeHost,
		})
	}
	return out
}

// Setup-name provenance labels — surfaced on the ready-to-launch
// consent preview so the operator sees WHERE each runtime's production
// setup name came from. Dev-promoted and legacy-default resolutions
// additionally raise the confirm hint (a dev iteration setup silently
// becoming the production build recipe was the gap-plan P1.3 finding).
const (
	setupProvenanceOverride     = "override"           // explicit user input (per-promotable or workflow-level)
	setupProvenanceRecordedProd = "recorded-prod"      // meta.ProdSetupName from a prior launch finalize
	setupProvenanceStageSetup   = "stage-setup"        // the stage's recorded setup — the validated stage basis
	setupProvenanceDevPromoted  = "dev-setup-promoted" // meta.PrimarySetupName — the dev iteration setup
	setupProvenanceDefault      = "default-prod"       // legacy "prod" fallback, nothing recorded
)

// resolveLaunchSetupName implements the plan §P5 launch-setup cascade
// and names which cascade source produced the value:
//
//	per-promotable ProdSetupNameOverride →
//	workflow-level ProdSetupNameOverride →
//	source meta.ProdSetupName (recorded at a prior launch finalize) →
//	source meta's stage setup (the promoted stage's, else the first stage's) →
//	source meta.PrimarySetupName →
//	"prod" legacy default (deferred from full P5; see
//	  ops/bundle/launch.go BuildLaunch for the same fallback rationale)
//
// The deferred "prod" tail keeps existing flow-eval scenarios and
// launch handler tests working until they're updated to seed the
// new ServiceMeta setup-name fields.
func resolveLaunchSetupName(p LaunchPromotableInput, workflowOverride string, meta *workflow.ServiceMeta) (string, string) {
	if v := strings.TrimSpace(p.ProdSetupNameOverride); v != "" {
		return v, setupProvenanceOverride
	}
	if workflowOverride != "" {
		return workflowOverride, setupProvenanceOverride
	}
	if meta != nil {
		if meta.ProdSetupName != "" {
			return meta.ProdSetupName, setupProvenanceRecordedProd
		}
		stage, ok := meta.StageFor(p.Hostname)
		if !ok {
			stage = meta.FirstStage()
		}
		if stage.SetupName != "" {
			return stage.SetupName, setupProvenanceStageSetup
		}
		if meta.PrimarySetupName != "" {
			return meta.PrimarySetupName, setupProvenanceDevPromoted
		}
	}
	return setupNameProd, setupProvenanceDefault
}

// launchRuntimeHost is the live runtime r promotes — the launch state's
// TargetServiceHostname: RuntimeHostname — the push source for every pair
// but one keyed by its dev pool, whose stage it is (docs/spec-workflows.md
// §10 P-LP-17) — else PushHostname.
func launchRuntimeHost(r resolvedLaunchRuntime) string {
	if r.RuntimeHostname != "" {
		return r.RuntimeHostname
	}
	return r.PushHostname
}

// launchTokenHost is the service r's launch token is staged on and read back
// from (P-LP-14). For a part keyed by its dev pool it is zcp's own service:
// the pool and the stage both run code a lease deploys, and every container
// of the pool reads the pool's env (§10 P-LP-17). Outside a container — no
// own service to name — and for every other pair it is the runtime r
// promotes.
func launchTokenHost(r resolvedLaunchRuntime, rt runtime.Info) string {
	if r.DevIsPool && rt.InContainer && rt.ServiceName != "" {
		return rt.ServiceName
	}
	return launchRuntimeHost(r)
}

// tokenStagedOnField is the launch state's TokenStagedOn for a token staged
// on tokenHost by a launch of runtimeHost: empty when they are one service,
// so a state reads exactly as it did before the field existed.
func tokenStagedOnField(runtimeHost, tokenHost string) string {
	if tokenHost == runtimeHost {
		return ""
	}
	return tokenHost
}

// firstResolvedRuntime returns the first entry, used for legacy call
// sites that still echo the user's one-promotable choice (e.g.
// state-file SourceRepoURL hint, scope-prompt blocker payloads).
// Returns the zero value when the slice is empty.
func firstResolvedRuntime(rs []resolvedLaunchRuntime) resolvedLaunchRuntime {
	if len(rs) == 0 {
		return resolvedLaunchRuntime{}
	}
	return rs[0]
}
