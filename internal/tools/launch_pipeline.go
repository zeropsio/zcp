package tools

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// defaultPipelineTagRegex is the production-recommended tag regex per
// Zerops docs (references/github-integration.mdx:41-47). Surfaces in the
// recommendation payload when the user has not overridden via
// WorkflowInput.PipelineTagRegex.
const defaultPipelineTagRegex = `^v\d+\.\d+\.\d+$`

// defaultPipelineEventType is the production-recommended trigger event
// for ongoing CD. TAG events are explicit, semver-disciplined releases
// (vs branch-push which risks deploying broken commits). Matches
// platform enum value of GithubIntegrationEventTypeEnumTag.
const defaultPipelineEventType = "TAG"

// pipelineDashboardBase is the Zerops dashboard URL host. Per-service
// deep-links are composed via fmt.Sprintf — kept as a constant for the
// avoid-magic-string lint.
const pipelineDashboardBase = "https://app.zerops.io"

// pipelineSkipReasonOptedOut is the canonical SkipReason value set on
// pipelineConfigEntry when SkipPipelineSetup=true. Used by
// pipelineSkipRecorded to distinguish explicit user-opt-out from
// transient lookup failures. Surfaced verbatim in atom guidance.
const pipelineSkipReasonOptedOut = "user-opted-out"

// deriveRepositoryFullName extracts `owner/repo` from a git remote URL.
// Recognized inputs:
//   - https://github.com/owner/repo
//   - https://github.com/owner/repo.git
//   - https://gitlab.com/owner/repo
//   - git@github.com:owner/repo.git
//
// Falls back to the raw input when no recognized pattern matches — the
// agent can still see the URL in the recommendation payload and reason
// about it. The recommendation is non-authoritative; user types into
// dashboard.
func deriveRepositoryFullName(repoURL string) string {
	if repoURL == "" {
		return ""
	}
	// SSH form: git@github.com:owner/repo.git → owner/repo
	if strings.HasPrefix(repoURL, "git@") {
		_, after, found := strings.Cut(repoURL, ":")
		if found {
			after = strings.TrimSuffix(after, ".git")
			return after
		}
	}
	// HTTPS form: parse + drop leading slash + drop .git suffix
	if u, err := url.Parse(repoURL); err == nil && u.Path != "" {
		path := strings.TrimPrefix(u.Path, "/")
		path = strings.TrimSuffix(path, ".git")
		if path != "" {
			return path
		}
	}
	return repoURL
}

// deriveDashboardDeepLink composes the Zerops dashboard URL pointing at
// the build-integration config page for a specific service-stack.
// Matches the live URL shape verified by `webhookConfirmResponse`
// (`/service-stack/<id>/deploy`, 2026-05-19) — the legacy
// `/dashboard/project/<proj>/service-stack/<id>/service-stack-source-code`
// slug 404s. ProjectID is not part of this URL — the service-stack ID
// alone resolves the page on the Zerops dashboard.
func deriveDashboardDeepLink(serviceStackID string) string {
	if serviceStackID == "" {
		return ""
	}
	return fmt.Sprintf("%s/service-stack/%s/deploy",
		pipelineDashboardBase, serviceStackID)
}

// pipelineRecommendation composes the suggested integration config the
// agent echoes to the user when guiding dashboard setup. Returns nil
// when the runtime's source repo URL is empty (defensive — should not
// happen in practice because launch requires buildFromGit) OR when
// zeropsYamlSetup is empty (plan §P5 — no "prod" default; caller must
// resolve the setup name via cascade before calling).
func pipelineRecommendation(repoURL, tagRegexOverride, zeropsYamlSetup string) *pipelineConfigRecommendation {
	if repoURL == "" || zeropsYamlSetup == "" {
		return nil
	}
	regex := tagRegexOverride
	if regex == "" {
		regex = defaultPipelineTagRegex
	}
	return &pipelineConfigRecommendation{
		RepositoryFullName: deriveRepositoryFullName(repoURL),
		EventType:          defaultPipelineEventType,
		TagRegex:           regex,
		ZeropsYamlSetup:    zeropsYamlSetup,
	}
}

// pipelineCheckInputs feeds executeLaunchPipelineCheck. Pulled into a
// struct so the function signature stays under maintainability-index
// thresholds (caller composes once, function reads).
type pipelineCheckInputs struct {
	// SkipPipelineSetup short-circuits the check entirely; all configured
	// service entries get a SkipReason set.
	SkipPipelineSetup bool
	// TagRegexOverride is the user-supplied regex for the recommendation
	// (empty → defaultPipelineTagRegex).
	TagRegexOverride string
	// RuntimeHostname is the single runtime service whose pipeline status
	// we read. Identifies which entry in state.ImportedServices to probe.
	RuntimeHostname string
	// RepoURL is the source git remote URL (buildFromGit value). Used to
	// derive the recommendation's repositoryFullName.
	RepoURL string
	// ZeropsYamlSetup is the production zerops.yaml setup-block name the
	// runtime resolves at deploy time (resolved by the launch handler via
	// plan §P5 cascade — meta first, no "prod" default). When empty, the
	// pipeline recommendation is omitted from the blocker payload.
	ZeropsYamlSetup string
}

// executeLaunchPipelineCheck reads pipeline-integration status for each
// runtime in state.ImportedServices that matches inputs.RuntimeHostname,
// and updates state.PipelineConfigurations in place. ZCP never PUTs
// (Path B); the function only reads. P-LP-7.
//
// Behavior when inputs.SkipPipelineSetup is true:
// every matching runtime's entry gets SkipReason=pipelineSkipReasonOptedOut and
// the check is otherwise a no-op (no GetStatus calls).
//
// Per-runtime errors do NOT abort the loop — each entry independently
// records whether it was reachable. The launched response surfaces
// per-runtime blockers; the launch itself remains succeeded.
//
// No error return: by design no failure at this layer is fatal — every
// per-runtime issue is recorded on the corresponding
// pipelineConfigEntry.SkipReason instead.
func executeLaunchPipelineCheck(
	ctx context.Context,
	admin platform.ProjectAdminClient,
	state *launchState,
	inputs pipelineCheckInputs,
) {
	if state.PipelineConfigurations == nil {
		state.PipelineConfigurations = make(map[string]pipelineConfigEntry)
	}
	recommendation := pipelineRecommendation(inputs.RepoURL, inputs.TagRegexOverride, inputs.ZeropsYamlSetup)

	for _, svc := range state.ImportedServices {
		if svc.Name != inputs.RuntimeHostname {
			// Not the runtime — managed services don't carry buildFromGit.
			continue
		}
		entry := pipelineConfigEntry{}
		if inputs.SkipPipelineSetup {
			entry.SkipReason = pipelineSkipReasonOptedOut
			state.PipelineConfigurations[svc.Name] = entry
			continue
		}
		entry.DeepLink = deriveDashboardDeepLink(svc.ID)
		entry.Recommendation = recommendation

		status, err := admin.GetServiceStackIntegrationStatus(ctx, svc.ID)
		if err != nil {
			entry.SkipReason = "lookup-failed: " + err.Error()
			state.PipelineConfigurations[svc.Name] = entry
			continue
		}
		if status.State == platform.IntegrationConfigured {
			entry.Configured = true
			entry.CurrentConfig = &pipelineConfigCurrent{
				Provider:           string(status.Provider),
				RepositoryFullName: status.RepositoryFullName,
				EventType:          string(status.EventType),
				BranchName:         status.BranchName,
				TagRegex:           status.TagRegex,
				ZeropsYamlSetup:    status.ZeropsYamlSetup,
				IsActive:           status.IsActive,
			}
			// Configured — drop recommendation; current truth is the
			// authority and the agent doesn't need to nag the user.
			entry.Recommendation = nil
		}
		state.PipelineConfigurations[svc.Name] = entry
	}
	state.PipelineCheckedAt = time.Now().UTC()
}

// pendingPipelineConfigurations returns true when at least one runtime
// in state.PipelineConfigurations is not configured AND not explicitly
// skipped — i.e. the launched response should surface a blocker for it.
//
// Used to decide whether a resume call with a launchKey should re-run
// executeLaunchPipelineCheck (to refresh state after the user has
// configured via dashboard).
func pendingPipelineConfigurations(state *launchState) bool {
	for _, entry := range state.PipelineConfigurations {
		if !entry.Configured && entry.SkipReason == "" {
			return true
		}
	}
	return false
}

// pipelineBlockers turns state.PipelineConfigurations into the slice of
// Blockers attached to the launched response. Configured entries
// produce no blocker; not-configured entries produce a single blocker
// per runtime carrying the deep-link + recommendation payload.
func pipelineBlockers(state *launchState) []topology.Blocker {
	if len(state.PipelineConfigurations) == 0 {
		return nil
	}
	var out []topology.Blocker
	for hostname, entry := range state.PipelineConfigurations {
		if entry.Configured || entry.SkipReason != "" {
			continue
		}
		msg := fmt.Sprintf("Runtime %q has no CD pipeline integration. Configure in Zerops dashboard.", hostname)
		if entry.DeepLink != "" {
			msg += " Deep-link: " + entry.DeepLink
		}
		if entry.Recommendation != nil {
			msg += fmt.Sprintf(" Recommended: repositoryFullName=%s eventType=%s tagRegex=%s zeropsYamlSetup=%s",
				entry.Recommendation.RepositoryFullName,
				entry.Recommendation.EventType,
				entry.Recommendation.TagRegex,
				entry.Recommendation.ZeropsYamlSetup,
			)
		}
		out = append(out, topology.Blocker{
			ID:       "pipeline-not-configured-" + hostname,
			Severity: topology.BlockerSeverityWarn,
			Category: topology.BlockerCategoryOther,
			Message:  msg,
		})
	}
	return out
}
