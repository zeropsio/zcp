package tools

import (
	"strings"

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
	// in production. Resolved per plan §P5 cascade:
	//   per-promotable ProdSetupNameOverride →
	//   workflow-level ProdSetupNameOverride →
	//   source meta.StageSetupName →
	//   source meta.PrimarySetupName →
	//   empty (handler surfaces scope-prompt blocker; no "prod" default).
	SetupName string
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
			if meta.StageHostname == p.Hostname && meta.Hostname != "" {
				pushHost = meta.Hostname
			}
		}
		prodHost := strings.TrimSpace(p.ProdHostname)
		if prodHost == "" {
			prodHost = derivePromotedProdHostname(pushHost)
		}
		if prodHost == "" {
			prodHost = pushHost
		}
		out = append(out, resolvedLaunchRuntime{
			ChoiceHostname: p.Hostname,
			PushHostname:   pushHost,
			ProdHostname:   prodHost,
			SetupName:      resolveLaunchSetupName(p, workflowOverride, meta),
		})
	}
	return out
}

// resolveLaunchSetupName implements the plan §P5 launch-setup cascade:
//
//	per-promotable ProdSetupNameOverride →
//	workflow-level ProdSetupNameOverride →
//	source meta.ProdSetupName (recorded at a prior launch finalize) →
//	source meta.StageSetupName →
//	source meta.PrimarySetupName →
//	"prod" legacy default (deferred from full P5; see
//	  ops/bundle/launch.go BuildLaunch for the same fallback rationale)
//
// The deferred "prod" tail keeps existing flow-eval scenarios and
// launch handler tests working until they're updated to seed the
// new ServiceMeta setup-name fields.
func resolveLaunchSetupName(p LaunchPromotableInput, workflowOverride string, meta *workflow.ServiceMeta) string {
	if v := strings.TrimSpace(p.ProdSetupNameOverride); v != "" {
		return v
	}
	if workflowOverride != "" {
		return workflowOverride
	}
	if meta != nil {
		if meta.ProdSetupName != "" {
			return meta.ProdSetupName
		}
		if meta.StageSetupName != "" {
			return meta.StageSetupName
		}
		if meta.PrimarySetupName != "" {
			return meta.PrimarySetupName
		}
	}
	return setupNameProd
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
