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
	// SuggestedRuntime is populated when AvailableRuntimes has exactly
	// one entry — the agent should default `targetService` to this value
	// (always the dev-half hostname) without asking. When empty (zero or
	// 2+ runtimes), the agent must select via user input.
	SuggestedRuntime string `json:"suggestedRuntime,omitempty"`
}

// runtimeChoice is one promotable runtime in `AvailableRuntimes`. The
// shape preserves pair-keyed identity (one logical service per dev/stage
// pair) and surfaces the platform-level type so the agent can briefly
// disclose what the user is choosing among.
type runtimeChoice struct {
	// Hostname is the dev-half hostname — the canonical `targetService`
	// value. The launch handler treats stage-half input as an error
	// (scope-prompt blocker `scope-stage-half-not-promotable`).
	Hostname string `json:"hostname"`
	// Type is the service-stack type version name as the platform
	// reports it (e.g. "nodejs@22", "php-nginx@8.4"). Empty when type
	// info is unavailable.
	Type string `json:"type,omitempty"`
	// Mode is the ZCP topology mode for this runtime (dev | simple |
	// standard | local-stage | local-only). Empty when ZCP has no
	// ServiceMeta for the service (raw-platform discovery only).
	Mode string `json:"mode,omitempty"`
	// StageHostname is the stage-half of a standard or local-stage pair.
	// Empty for dev/simple/local-only runtimes. When set, the agent
	// should disclose: "promoting `<hostname>` ships the dev-stage
	// pair's published source — `<stageHostname>` is the staging half."
	StageHostname string `json:"stageHostname,omitempty"`
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
// local-stage pair, the stage-half is hidden from AvailableRuntimes
// (only the dev-half surfaces, with stageHostname exposed on the
// runtimeChoice). Without a state directory or matching metas, the
// function falls back to raw service-list output — each USER-category
// hostname becomes its own runtimeChoice with empty Mode/StageHostname.
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
			// the launch flow, NOT selected as targetService.
			if s.ServiceStackTypeInfo.ServiceStackTypeCategoryName != "USER" {
				continue
			}
			if isZCPSelfService(s, rt) {
				continue
			}
			meta := lookupMeta(metaByHost, s.Name)
			// Stage-half collapse: when this hostname is the stage-half
			// of a managed dev/stage pair, skip — the dev-half will
			// surface independently with stageHostname populated.
			if meta != nil && meta.StageHostname == s.Name {
				continue
			}
			out.AvailableRuntimes = append(out.AvailableRuntimes, runtimeChoice{
				Hostname:      s.Name,
				Type:          s.ServiceStackTypeInfo.ServiceStackTypeVersionName,
				Mode:          metaModeString(meta),
				StageHostname: metaStageHostname(meta),
			})
		}
		if len(out.AvailableRuntimes) == 1 {
			out.SuggestedRuntime = out.AvailableRuntimes[0].Hostname
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

// metaStageHostname returns the stage-half hostname when this meta is a
// standard or local-stage pair, otherwise empty.
func metaStageHostname(meta *workflow.ServiceMeta) string {
	if meta == nil {
		return ""
	}
	return meta.StageHostname
}

// stageHalfForTarget resolves the user-supplied targetService through
// the pair-keyed ServiceMeta index. Returns (devHostname, true) when
// the input is the stage-half of a known pair so the handler can fire
// the `scope-stage-half-not-promotable` blocker; returns ("", false)
// for dev-half input, standalone runtimes, or unknown hostnames.
func stageHalfForTarget(stateDir, targetService string) (string, bool) {
	if stateDir == "" || targetService == "" {
		return "", false
	}
	meta, err := workflow.FindServiceMeta(stateDir, targetService)
	if err != nil || meta == nil {
		return "", false
	}
	if meta.StageHostname == targetService {
		return meta.Hostname, true
	}
	return "", false
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
