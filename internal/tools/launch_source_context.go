package tools

import (
	"context"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
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
	// AvailableRuntimes lists the hostnames of USER-category service
	// stacks in the source project (managed services excluded). Always
	// safe for the agent to compare against `targetService` input.
	AvailableRuntimes []string `json:"availableRuntimes,omitempty"`
	// SuggestedRuntime is populated when AvailableRuntimes has exactly
	// one entry. When set, the agent should default `targetService` to
	// this value without asking. When empty (zero or 2+ runtimes), the
	// agent must select via user input from AvailableRuntimes.
	SuggestedRuntime string `json:"suggestedRuntime,omitempty"`
}

// gatherLaunchSourceContext probes the source project for hint data
// the scope-prompt response surfaces. Best-effort: returns nil when no
// useful data could be collected (no client, missing projectID, every
// platform call errored).
//
// Errors are NOT propagated — the launch flow continues with empty
// hints and forces user-driven input via scope-prompt blockers.
func gatherLaunchSourceContext(ctx context.Context, client platform.Client, sourceProjectID string) *launchSourceContext {
	if client == nil || sourceProjectID == "" {
		return nil
	}
	out := &launchSourceContext{}
	if proj, err := client.GetProject(ctx, sourceProjectID); err == nil && proj != nil {
		out.SourceProjectName = proj.Name
		out.SuggestedTargetName = deriveProductionProjectName(proj.Name)
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
			out.AvailableRuntimes = append(out.AvailableRuntimes, s.Name)
		}
		if len(out.AvailableRuntimes) == 1 {
			out.SuggestedRuntime = out.AvailableRuntimes[0]
		}
	}
	if out.SourceProjectName == "" && len(out.AvailableRuntimes) == 0 {
		return nil
	}
	return out
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
