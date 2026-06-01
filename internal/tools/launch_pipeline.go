package tools

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
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
	// Runtimes is the set of PRODUCTION-side runtimes to probe — one per
	// promoted runtime, keyed by the prod hostname the platform assigned
	// (matches ImportedServices[].Name). Carries per-runtime RepoURL +
	// SetupName for the recommendation. The check iterates THIS list (not
	// the source hostname): a dev/stage promotion imports `app` while the
	// source is `appdev`, so matching on the source hostname silently
	// probed nothing and reported "configured" (LAUNCH-1).
	Runtimes []launchRuntimeProd
}

// executeLaunchPipelineCheck reads pipeline-integration status for each
// promoted runtime in inputs.Runtimes, matching it to its imported
// service by PROD hostname, and updates state.PipelineConfigurations in
// place (keyed by prod hostname). ZCP never PUTs (Path B); read-only. P-LP-7.
//
// Behavior when inputs.SkipPipelineSetup is true: every runtime's entry
// gets SkipReason=pipelineSkipReasonOptedOut and the check is otherwise a
// no-op (no GetStatus calls).
//
// A promoted runtime whose prod hostname is NOT in ImportedServices is
// recorded as an UNCONFIGURED (pending) entry — never silently treated as
// configured (that silent path was LAUNCH-1).
//
// Per-runtime errors do NOT abort the loop — each entry independently
// records whether it was reachable. The launched response surfaces
// per-runtime blockers; the launch itself remains succeeded.
// pipelineIntegrationReader is the minimal read-only capability
// executeLaunchPipelineCheck needs. Both platform.ProjectAdminClient
// (new-project / launchKey path) and platform.Client (existing-project
// token path) satisfy it, so both mutation paths share the check.
type pipelineIntegrationReader interface {
	GetServiceStackIntegrationStatus(ctx context.Context, serviceID string) (platform.IntegrationStatus, error)
}

func executeLaunchPipelineCheck(
	ctx context.Context,
	admin pipelineIntegrationReader,
	state *launchState,
	inputs pipelineCheckInputs,
) {
	if state.PipelineConfigurations == nil {
		state.PipelineConfigurations = make(map[string]pipelineConfigEntry)
	}
	// Index imported services by prod hostname (== ImportedServices[].Name).
	importedByName := make(map[string]importedServiceEntry, len(state.ImportedServices))
	for _, svc := range state.ImportedServices {
		importedByName[svc.Name] = svc
	}

	for _, rt := range inputs.Runtimes {
		entry := pipelineConfigEntry{}
		if inputs.SkipPipelineSetup {
			entry.SkipReason = pipelineSkipReasonOptedOut
			state.PipelineConfigurations[rt.ProdHostname] = entry
			continue
		}
		svc, found := importedByName[rt.ProdHostname]
		if !found {
			// The promoted runtime is missing from the import result. Record
			// as an unconfigured (pending) entry so it surfaces a blocker —
			// NEVER swallow it into a silent "configured".
			entry.Recommendation = pipelineRecommendation(rt.RepoURL, inputs.TagRegexOverride, rt.SetupName)
			state.PipelineConfigurations[rt.ProdHostname] = entry
			continue
		}
		entry.DeepLink = deriveDashboardDeepLink(svc.ID)
		entry.Recommendation = pipelineRecommendation(rt.RepoURL, inputs.TagRegexOverride, rt.SetupName)

		status, err := admin.GetServiceStackIntegrationStatus(ctx, svc.ID)
		if err != nil {
			entry.SkipReason = "lookup-failed: " + err.Error()
			state.PipelineConfigurations[rt.ProdHostname] = entry
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
		state.PipelineConfigurations[rt.ProdHostname] = entry
	}
	state.PipelineCheckedAt = time.Now().UTC()
}

// runtimeProdsFromBundleInputs derives the persisted prod-runtime list
// from the composed bundle inputs — one entry per promoted runtime,
// carrying the prod hostname the platform will assign + per-runtime
// RepoURL + SetupName for the pipeline recommendation.
func runtimeProdsFromBundleInputs(inputs ops.LaunchBundleInputs) []launchRuntimeProd {
	out := make([]launchRuntimeProd, 0, len(inputs.Runtimes))
	for _, r := range inputs.Runtimes {
		out = append(out, launchRuntimeProd{
			ProdHostname: r.ProdHostname,
			RepoURL:      r.RepoURL,
			SetupName:    r.SetupName,
		})
	}
	return out
}

// pipelineRuntimesForState returns the prod-runtime set to probe on a
// resume call. New states carry the authoritative RuntimeProds; for
// pre-RuntimeProds state files it falls back to the single runtime
// derived from the source hostname (derivePromotedProdHostname) so an
// old in-flight launch still re-checks the right prod service instead of
// silently reporting "configured".
func pipelineRuntimesForState(stateDir string, input WorkflowInput, state *launchState) []launchRuntimeProd {
	if len(state.RuntimeProds) > 0 {
		return state.RuntimeProds
	}
	prod := derivePromotedProdHostname(state.TargetServiceHostname)
	if prod == "" {
		return nil
	}
	return []launchRuntimeProd{{
		ProdHostname: prod,
		RepoURL:      state.SourceRepoURL,
		SetupName:    launchTargetSetupName(stateDir, state.TargetServiceHostname, input),
	}}
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
