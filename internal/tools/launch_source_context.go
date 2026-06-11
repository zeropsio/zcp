package tools

import (
	"context"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// launchSourceContext is the source-project discovery snapshot surfaced
// in the scope-prompt / classify-prompt / ready-to-launch responses.
// Lets the agent fill in `productionProjectName` and `targetService`
// from contextual data instead of guessing or always asking the user.
//
// Fields are best-effort: when the source-project GET fails or the
// service list is empty, the corresponding fields stay empty and the
// agent falls back to user-driven input. Surfacing in the response is
// a HINT — the user remains authoritative on names + service choice.
type launchSourceContext struct {
	// SourceProjectName is the name of the dev/stage project the launch
	// promotes. Empty when GetProject is unreachable.
	SourceProjectName string `json:"sourceProjectName,omitempty"`
	// SuggestedTargetName transforms the source name into a production
	// candidate (e.g., "myapp-dev" → "myapp-prod"). Empty when no
	// source name is known.
	SuggestedTargetName string `json:"suggestedTargetName,omitempty"`
	// AvailableRuntimes lists the USER-category service stacks in the
	// source project, pair-collapsed: each entry is the DEV-half of a
	// dev/stage pair (with `stageHostname` exposing the other half for
	// agent disclosure) or a standalone runtime. Managed services
	// (databases, KV stores, queues) are excluded. The ZCP control-plane
	// container itself (zcp@1) is filtered out so it never shows up as a
	// promotion candidate.
	AvailableRuntimes []runtimeChoice `json:"availableRuntimes,omitempty"`
	// PromotionHeadline is populated when AvailableRuntimes has exactly
	// one entry — the agent should default `targetService` to this value
	// (the runtimeChoice.Hostname — stage-half for pairs, the only
	// hostname for singletons). When empty (zero or 2+ runtimes), the
	// agent must select via user input.
	//
	// Renamed from SuggestedRuntime in plan §P7 F7: "headline" matches
	// what the user sees on the launch surface ("we'll promote
	// `<hostname>`"), where SuggestedRuntime suggested a generic
	// recommendation. The field still carries the same hostname.
	PromotionHeadline string `json:"promotionHeadline,omitempty"`
	// TargetServiceCanonical is the post-normalization hostname the
	// launch composer will use as `TargetService` after pair-keyed
	// collapse via normalizeTargetServiceForLaunch (stage-half input
	// for a standard pair resolves to the dev-half). Disclosed so the
	// agent can echo the actual key the bundle will reference without
	// re-running the normalization mentally. Empty when
	// PromotionHeadline is empty or the canonical form equals the
	// headline (most common case for stage-half pair promotion and
	// singleton runtimes).
	TargetServiceCanonical string `json:"targetServiceCanonical,omitempty"`
	// ManagedDeps lists the source project's managed services (databases,
	// KV stores, queues) that the launch bundles implicitly alongside the
	// promoted runtime. Display-only at scope time: per-dep include/
	// exclude decisions travel via the `managedDeps` input, and the
	// ready-to-launch preview marks per-dep `referenced` once the bundle
	// is composed (wiring is a compose-time fact — never claimed here).
	ManagedDeps []launchManagedDepInfo `json:"managedDeps,omitempty"`
}

// launchManagedDepInfo is one managed dependency in the scope-time
// display list — name + platform type, nothing the source read can't
// answer authoritatively at this point.
type launchManagedDepInfo struct {
	Hostname string `json:"hostname"`
	Type     string `json:"type,omitempty"`
}

// runtimeChoice is one promotable runtime in `AvailableRuntimes`. The
// shape preserves pair-keyed identity (one logical service per dev/stage
// pair) and surfaces the platform-level type so the agent can briefly
// disclose what the user is choosing among.
type runtimeChoice struct {
	// Hostname is the user-facing promotion headline. For standard or
	// local-stage pairs this is the STAGE-half hostname (the validated
	// last-known-good copy is the natural promotion source). For
	// dev/simple/local-only runtimes it is the only hostname. Either
	// half of a pair is accepted as `targetService`; the handler
	// normalizes to the canonical dev-half key internally.
	Hostname string `json:"hostname"`
	// Type is the service-stack type version name as the platform
	// reports it (e.g. "nodejs@22", "php-nginx@8.4"). Empty when type
	// info is unavailable.
	Type string `json:"type,omitempty"`
	// Mode is the ZCP topology mode for this runtime (dev | simple |
	// standard | local-stage | local-only). Empty when ZCP has no
	// ServiceMeta for the service (raw-platform discovery only). When
	// Mode=standard or local-stage, the entry represents the stage-half
	// of a pair and DevHostname names the iteration-half.
	Mode string `json:"mode,omitempty"`
	// DevHostname is the dev-half of a standard or local-stage pair.
	// Empty for dev/simple/local-only runtimes. Disclosed so the agent
	// can explain "promoting `<hostname>` ships the dev-stage pair's
	// validated source — `<devHostname>` is the iteration half."
	DevHostname string `json:"devHostname,omitempty"`
}

// gatherLaunchSourceContext probes the source project for hint data
// the scope-prompt response surfaces. Best-effort: returns nil when no
// useful data could be collected (no client, missing projectID, every
// platform call errored).
//
// Errors are NOT propagated — the launch flow continues with empty
// hints and forces user-driven input via scope-prompt blockers.
//
// Pair-keyed collapse: when ZCP holds a ServiceMeta for a standard or
// local-stage pair, the dev-half is hidden from AvailableRuntimes
// (only the stage-half surfaces, with devHostname exposed on the
// runtimeChoice). Stage is the validated last-known-good copy and the
// natural promotion headline. Without a state directory or matching
// metas, the function falls back to raw service-list output — each
// USER-category hostname becomes its own runtimeChoice with empty
// Mode/DevHostname.
//
// Self-filter: the ZCP control-plane container's own service-stack
// (type `zcp@1`) is excluded so the launch flow can't suggest
// promoting ZCP itself. Defense-in-depth: hostname-match against
// `rt.ServiceName` also excludes (no-op on local mode where
// rt.ServiceName is empty).
func gatherLaunchSourceContext(ctx context.Context, client platform.Client, sourceProjectID, stateDir string, rt runtime.Info) *launchSourceContext {
	if client == nil || sourceProjectID == "" {
		return nil
	}
	out := &launchSourceContext{}
	if proj, err := client.GetProject(ctx, sourceProjectID); err == nil && proj != nil {
		out.SourceProjectName = proj.Name
		out.SuggestedTargetName = deriveProductionProjectName(proj.Name)
	}

	// Resolve pair-keyed metas. Missing state dir or empty result is
	// expected on local-only sources and on fresh sessions — fall back
	// to raw discovery without pair-collapse.
	var metaByHost map[string]*workflow.ServiceMeta
	if stateDir != "" {
		if metas, err := workflow.ListServiceMetas(stateDir); err == nil {
			metaByHost = workflow.ManagedRuntimeIndex(metas)
		}
	}

	if services, err := ops.ListProjectServices(ctx, client, sourceProjectID); err == nil {
		for _, s := range services {
			// USER category = customer runtime services (nodejs, php,
			// python, etc.). Managed services (postgres, valkey, etc.)
			// carry non-USER categories and are bundled implicitly by
			// the launch flow, NOT selected as targetService — they
			// surface in the ManagedDeps display list instead.
			if s.ServiceStackTypeInfo.ServiceStackTypeCategoryName != "USER" {
				if !s.IsSystem() && !isZCPSelfService(s, rt) {
					out.ManagedDeps = append(out.ManagedDeps, launchManagedDepInfo{
						Hostname: s.Name,
						Type:     s.ServiceStackTypeInfo.ServiceStackTypeVersionName,
					})
				}
				continue
			}
			if isZCPSelfService(s, rt) {
				continue
			}
			meta := lookupMeta(metaByHost, s.Name)
			// Dev-half collapse: when this hostname is the dev-half of a
			// managed pair, skip — the stage-half will surface
			// independently as the promotion headline with devHostname
			// disclosed. Singletons (dev/simple/local-only) fall through
			// to the append below.
			if meta != nil && meta.Hostname == s.Name && meta.StageHostname != "" {
				continue
			}
			out.AvailableRuntimes = append(out.AvailableRuntimes, runtimeChoice{
				Hostname:    s.Name,
				Type:        s.ServiceStackTypeInfo.ServiceStackTypeVersionName,
				Mode:        metaModeString(meta),
				DevHostname: pairDevHostname(meta, s.Name),
			})
		}
		if len(out.AvailableRuntimes) == 1 {
			only := out.AvailableRuntimes[0]
			out.PromotionHeadline = only.Hostname
			// Canonical hostname is the dev-half of a pair (managed key
			// of the bundle's TargetService). Only disclosed when it
			// differs from the headline — same value would be noise.
			if only.DevHostname != "" && only.DevHostname != only.Hostname {
				out.TargetServiceCanonical = only.DevHostname
			}
		}
	}
	if out.SourceProjectName == "" && len(out.AvailableRuntimes) == 0 {
		return nil
	}
	return out
}

// isZCPSelfService filters the ZCP control-plane container's own
// service-stack out of the promotion list. Two-level check:
//  1. Type name `zcp@1` — the platform-known control-plane type. This
//     branch fires regardless of execution context (local or in-container).
//  2. Hostname match against `rt.ServiceName` — defense-in-depth for the
//     in-container case where ZCP knows its own hostname; no-op when
//     `rt.ServiceName` is empty (local mode).
func isZCPSelfService(s platform.ServiceStack, rt runtime.Info) bool {
	if s.ServiceStackTypeInfo.ServiceStackTypeVersionName == "zcp@1" {
		return true
	}
	if rt.ServiceName != "" && s.Name == rt.ServiceName {
		return true
	}
	return false
}

// lookupMeta returns the ServiceMeta for the given hostname when the
// pair-keyed index resolved one; otherwise nil. Wraps the nil-map
// guard so call sites read cleanly.
func lookupMeta(index map[string]*workflow.ServiceMeta, hostname string) *workflow.ServiceMeta {
	if index == nil {
		return nil
	}
	return index[hostname]
}

// metaModeString returns the topology mode as a string for the response
// surface, falling back to empty when no meta is available.
func metaModeString(meta *workflow.ServiceMeta) string {
	if meta == nil {
		return ""
	}
	return string(meta.Mode)
}

// pairDevHostname returns the dev-half hostname for disclosure on a
// stage-half runtimeChoice entry. Returns empty when the meta doesn't
// represent a pair OR when this hostname IS the dev-half (in which
// case the entry already names the only hostname).
func pairDevHostname(meta *workflow.ServiceMeta, hostname string) string {
	if meta == nil || meta.StageHostname == "" {
		return ""
	}
	if meta.StageHostname == hostname {
		return meta.Hostname
	}
	return ""
}

// normalizeTargetServiceForLaunch resolves the user-supplied
// targetService through the pair-keyed ServiceMeta index and returns
// the canonical dev-half hostname (= ServiceMeta primary key). When
// the user supplied the stage-half of a standard pair, this returns
// the corresponding dev-half — both halves of a pair share the same
// git source and setup blocks, so promotion accepts either. When the
// hostname isn't part of any tracked pair, returns the input unchanged
// (lookup fall-through; downstream validation handles unknown hosts).
//
// The dev-half is the canonical key because the bundle composer reads
// ServiceMeta to resolve `setup:` block name + runtime type, and meta
// is keyed by dev-hostname. Stage-half input as TargetHostname into
// the bundle would also poison the prod project's runtime naming.
func normalizeTargetServiceForLaunch(stateDir, targetService string) string {
	if stateDir == "" || targetService == "" {
		return targetService
	}
	meta, err := workflow.FindServiceMeta(stateDir, targetService)
	if err != nil || meta == nil {
		return targetService
	}
	if meta.StageHostname == targetService {
		return meta.Hostname
	}
	return targetService
}

// deriveProductionProjectName transforms a source-project name into a
// production-project suggestion. Recognizes the conventional dev/stage
// suffixes and swaps them for "-prod"; appends "-prod" otherwise.
//
//   - "myapp-dev"         → "myapp-prod"
//   - "myapp-stage"       → "myapp-prod"
//   - "myapp-development" → "myapp-prod"
//   - "myapp"             → "myapp-prod"
//   - ""                  → "" (empty in, empty out)
func deriveProductionProjectName(sourceName string) string {
	if sourceName == "" {
		return ""
	}
	for _, suffix := range []string{"-development", "-staging", "-stage", "-dev"} {
		if trimmed, ok := strings.CutSuffix(sourceName, suffix); ok {
			return trimmed + "-prod"
		}
	}
	return sourceName + "-prod"
}
