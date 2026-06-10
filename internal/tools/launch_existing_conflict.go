package tools

import (
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// existingProjectMergeStrategy enumerates the agent-supplied
// resolutions for a target-project hostname collision. Mirrors the
// WorkflowInput.MergeStrategy map values; centralized here so the
// gate + composer + atom share one taxonomy.
type existingProjectMergeStrategy string

const (
	// mergeStrategySkip drops the colliding promotable from the
	// composed bundle. Additive launch — the existing target service
	// is left untouched. Default when no entry is supplied for a
	// hostname (the gate then refuses with "ack required" until the
	// agent explicitly states the strategy per conflict).
	mergeStrategySkip existingProjectMergeStrategy = "skip"
	// mergeStrategyReplace overwrites the existing target service.
	// REQUIRES ConfirmDestructive ack matching every replace-flagged
	// hostname (diagnose-before-destruct invariant extends here).
	mergeStrategyReplace existingProjectMergeStrategy = "replace"
)

// conflictKind distinguishes the SOURCE of a colliding hostname: a
// promoted runtime (overridable via import) vs a managed dependency
// (the platform restricts override to runtimes, so replace can never
// work for managed — only skip is offered).
type conflictKind string

const (
	conflictKindRuntime conflictKind = "runtime"
	conflictKindManaged conflictKind = "managed"
)

// existingProjectConflict captures one hostname collision detected at
// the existing-project gate. Promoted is the prod-side hostname (after
// derivation); ExistingType + ExistingStatus surface the target's
// current shape so the agent can give the user enough context to
// choose skip vs replace. Kind is derived from the desired (bundle)
// source, not the existing service's type.
type existingProjectConflict struct {
	Hostname       string                       `json:"hostname"`
	Kind           conflictKind                 `json:"kind"`
	ExistingType   string                       `json:"existingType,omitempty"`
	ExistingStatus string                       `json:"existingStatus,omitempty"`
	Strategy       existingProjectMergeStrategy `json:"strategy,omitempty"`
}

// detectExistingProjectConflicts compares the prod-hostname set the
// launch bundle would create against the target project's current
// services. Returns one conflict entry per collision, in stable
// (sorted) order, tagged with the desired-side Kind (runtime vs
// managed). Empty result = no conflicts → gate clears.
func detectExistingProjectConflicts(
	promotedHostnames []string,
	managedHostnames []string,
	existingServices []platform.ServiceStack,
) []existingProjectConflict {
	if len(existingServices) == 0 {
		return nil
	}
	// Desired-side kind: a hostname promoted as a runtime is overridable;
	// a managed dep is not. Runtime wins if a hostname appears in both
	// (it would be promoted as a runtime).
	kindOf := make(map[string]conflictKind, len(promotedHostnames)+len(managedHostnames))
	for _, h := range managedHostnames {
		kindOf[h] = conflictKindManaged
	}
	for _, h := range promotedHostnames {
		kindOf[h] = conflictKindRuntime
	}
	var conflicts []existingProjectConflict
	seen := make(map[string]bool)
	for _, svc := range existingServices {
		kind, want := kindOf[svc.Name]
		if !want || seen[svc.Name] {
			continue
		}
		seen[svc.Name] = true
		conflicts = append(conflicts, existingProjectConflict{
			Hostname:       svc.Name,
			Kind:           kind,
			ExistingType:   svc.ServiceStackTypeInfo.ServiceStackTypeVersionName,
			ExistingStatus: svc.Status,
		})
	}
	sort.Slice(conflicts, func(i, j int) bool {
		return conflicts[i].Hostname < conflicts[j].Hostname
	})
	return conflicts
}

// resolveExistingProjectStrategies applies the agent-supplied
// WorkflowInput.MergeStrategy to the detected conflicts. Returns
// the conflict list annotated with the chosen strategy plus a list
// of conflicts still missing an agent ack (the gate surfaces these
// as unresolved until the agent re-calls with a complete map).
func resolveExistingProjectStrategies(
	conflicts []existingProjectConflict,
	mergeStrategy map[string]string,
) (resolved []existingProjectConflict, unresolved []existingProjectConflict) {
	resolved = make([]existingProjectConflict, 0, len(conflicts))
	for _, c := range conflicts {
		strategyValue := strings.ToLower(strings.TrimSpace(mergeStrategy[c.Hostname]))
		switch existingProjectMergeStrategy(strategyValue) {
		case mergeStrategySkip:
			c.Strategy = mergeStrategySkip
			resolved = append(resolved, c)
		case mergeStrategyReplace:
			// Managed services cannot be replaced (the platform restricts
			// override to runtimes) — re-surface as unresolved so the agent
			// re-chooses skip. Runtime replace is valid.
			if c.Kind == conflictKindManaged {
				unresolved = append(unresolved, c)
				continue
			}
			c.Strategy = mergeStrategyReplace
			resolved = append(resolved, c)
		default:
			unresolved = append(unresolved, c)
		}
	}
	return resolved, unresolved
}

// missingDestructiveAckForReplaces returns the hostnames flagged
// `replace` that the agent's ConfirmDestructive ack does NOT cover.
// Replace requires operation="launch-production-replace" plus every
// replace-flagged hostname listed in acknowledgedTargets. Skip-only
// resolutions don't need an ack — additive launches are non-destructive.
func missingDestructiveAckForReplaces(resolved []existingProjectConflict, ack *DestructiveAck) []string {
	var replaceHosts []string
	for _, c := range resolved {
		if c.Strategy == mergeStrategyReplace {
			replaceHosts = append(replaceHosts, c.Hostname)
		}
	}
	if len(replaceHosts) == 0 {
		return nil
	}
	if ack == nil || ack.Operation != "launch-production-replace" {
		return replaceHosts
	}
	ackSet := make(map[string]bool, len(ack.AcknowledgedTargets))
	for _, h := range ack.AcknowledgedTargets {
		ackSet[h] = true
	}
	var missing []string
	for _, h := range replaceHosts {
		if !ackSet[h] {
			missing = append(missing, h)
		}
	}
	return missing
}

// applyMergeResolutionsToBundle applies the resolved conflict strategies to
// the bundle inputs: skip drops the matching promotable / managed dep;
// replace sets Override=true on the matching runtime so the composer emits
// `override: true` and the platform overwrites the existing service on
// import (without it the import is rejected on the colliding hostname).
// Returns the (possibly mutated) inputs and whether anything changed, so
// the caller knows to recompose.
func applyMergeResolutionsToBundle(inputs ops.LaunchBundleInputs, resolved []existingProjectConflict) (ops.LaunchBundleInputs, bool) {
	skipSet := make(map[string]bool, len(resolved))
	replaceSet := make(map[string]bool, len(resolved))
	for _, c := range resolved {
		switch c.Strategy {
		case mergeStrategySkip:
			skipSet[c.Hostname] = true
		case mergeStrategyReplace:
			replaceSet[c.Hostname] = true
		}
	}
	if len(skipSet) == 0 && len(replaceSet) == 0 {
		return inputs, false
	}
	keptRuntimes := make([]ops.LaunchRuntimeInput, 0, len(inputs.Runtimes))
	for _, r := range inputs.Runtimes {
		if skipSet[r.ProdHostname] {
			continue
		}
		if replaceSet[r.ProdHostname] {
			r.Override = true
		}
		keptRuntimes = append(keptRuntimes, r)
	}
	keptManaged := make([]ops.ManagedServiceEntry, 0, len(inputs.ManagedServices))
	for _, m := range inputs.ManagedServices {
		if skipSet[m.Hostname] {
			continue
		}
		keptManaged = append(keptManaged, m)
	}
	inputs.Runtimes = keptRuntimes
	inputs.ManagedServices = keptManaged
	return inputs, true
}

// existingProjectConflictPromptResponse builds the structured
// response surfaced when the existing-project gate detects conflicts
// that the agent has not yet acknowledged via MergeStrategy. Carries
// one conflict entry per collision + the chained next-call shape.
func existingProjectConflictPromptResponse(
	corpus []workflow.KnowledgeAtom,
	input WorkflowInput,
	conflicts []existingProjectConflict,
) *mcp.CallToolResult {
	guidance := atomBody(corpus, "launch-existing-project-conflict")
	if guidance == "" {
		guidance = "Target project has services whose hostnames collide with the launch bundle. Ask the user per conflict whether to skip (additive — leave the existing service alone) or replace (overwrite — requires confirmDestructive ack), then re-call with mergeStrategy populated."
	}
	blockers := make([]topology.Blocker, 0, len(conflicts))
	for _, c := range conflicts {
		var msg string
		if c.Kind == conflictKindManaged {
			// Managed deps are not overridable — skip is the only option.
			msg = fmt.Sprintf(
				"Target project already has managed service %q (type=%s, status=%s). Managed services cannot be replaced (the platform overrides only runtimes), so the only option is skip (leave the existing managed service in place and reuse it). Re-call launch with mergeStrategy={%q: \"skip\"}.",
				c.Hostname, c.ExistingType, c.ExistingStatus, c.Hostname,
			)
		} else {
			msg = fmt.Sprintf(
				"Target project already has service %q (type=%s, status=%s). Ask user: skip (leave existing alone, do not promote this entry) OR replace (overwrite — emits `override: true` so the import replaces it; requires confirmDestructive ack with operation=\"launch-production-replace\" + acknowledgedTargets including %q). Then re-call launch with mergeStrategy={%q: \"skip\"|\"replace\"}.",
				c.Hostname, c.ExistingType, c.ExistingStatus, c.Hostname, c.Hostname,
			)
		}
		blockers = append(blockers, topology.Blocker{
			ID:       "existing-project-conflict-" + c.Hostname,
			Severity: topology.BlockerSeverityBlock,
			Category: topology.BlockerCategoryOther,
			Message:  msg,
		})
	}
	resp := launchProductionResponse{
		Workflow: workflowLaunchProduction,
		Status:   topology.LaunchStatusExistingProjectConflictPrompt,
		Phase:    workflow.PhaseLaunchProductionActive,
		Guidance: guidance,
		Blockers: blockers,
		Inputs:   echoInputs(input),
	}
	return jsonResult(resp)
}
