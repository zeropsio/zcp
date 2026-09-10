package eval

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// artifactPromotionRowID builds the stable row id for one O7
// artifact-promotion field (docs/spec-eval-farm.md §4.4 O7):
// `artifact_promotion/<target>/<field>`.
func artifactPromotionRowID(target, field string) string {
	return "artifact_promotion/" + target + "/" + field
}

// artifactPromotionFields lists the four O7 row fields, in report order.
var artifactPromotionFields = []string{"created_after_start", "source_cli", "no_git_source", "dev_unchanged"}

// evaluateArtifactPromotionRows evaluates the O7 artifact-promotion oracle
// (docs/spec-eval-farm.md §4.4 O7) for one ArtifactPromotionEntry: after a
// dev (entry.From) → stage (entry.To) promote, the target's ACTIVE
// appVersion must have been created after runStart, carry source "CLI",
// carry no publicGitSource, and the dev service's ACTIVE appVersion id must
// be unchanged from baseline.UnrelatedAppVersion. A target appVersion with
// source "GIT" (an independent buildFromGit build) fails source_cli and,
// when it carries a publicGitSource, no_git_source too. An unreadable
// SearchAppVersions call blocks all four rows.
func evaluateArtifactPromotionRows(
	ctx context.Context,
	entry ArtifactPromotionEntry,
	client platform.Client,
	projectID string, //nolint:unparam // real projectID variance arrives when S5b wires this into generateRequiredChecks; this slice's own tests all use one project on purpose.
	runStart time.Time,
	baseline *ScenarioBaseline,
) []RequiredCheck {
	if client == nil {
		return blockedArtifactPromotionRows(entry.To, "no platform client available", "")
	}

	services, err := client.ListServicesDirect(ctx, projectID)
	if err != nil {
		return blockedArtifactPromotionRows(entry.To, fmt.Sprintf("ListServicesDirect failed: %v", err), "ListServicesDirect")
	}
	targetID, targetFound := serviceIDByHostname(services, entry.To)
	devID, devFound := serviceIDByHostname(services, entry.From)
	if !targetFound {
		return blockedArtifactPromotionRows(entry.To, fmt.Sprintf("target service %q not found in project", entry.To), "ListServicesDirect")
	}
	if !devFound {
		return blockedArtifactPromotionRows(entry.To, fmt.Sprintf("dev service %q not found in project", entry.From), "ListServicesDirect")
	}

	appVersions, err := client.SearchAppVersions(ctx, projectID, 0)
	if err != nil {
		return blockedArtifactPromotionRows(entry.To, fmt.Sprintf("SearchAppVersions failed: %v", err), "SearchAppVersions")
	}
	byService := groupAppVersionsByService(appVersions)

	targetActive := activeAppVersion(byService[targetID])
	devActive := activeAppVersion(byService[devID])

	rows := []RequiredCheck{
		artifactPromotionCreatedAfterStartRow(entry.To, targetActive, runStart),
		artifactPromotionSourceCliRow(entry.To, targetActive),
		artifactPromotionNoGitSourceRow(entry.To, targetActive),
		artifactPromotionDevUnchangedRow(entry.To, devActive, baseline),
	}
	return rows
}

// serviceIDByHostname finds the service ID matching hostname among
// services, mirroring ZCP's service-by-hostname convention (CLAUDE.md).
func serviceIDByHostname(services []platform.ServiceStack, hostname string) (string, bool) {
	for _, svc := range services {
		if svc.Name == hostname {
			return svc.ID, true
		}
	}
	return "", false
}

// activeAppVersion returns the ACTIVE entry from a service's appVersion
// history, or nil if none is ACTIVE.
func activeAppVersion(history []platform.AppVersionEvent) *platform.AppVersionEvent {
	sort.SliceStable(history, func(i, j int) bool { return history[i].Created < history[j].Created })
	for i := range history {
		if history[i].Status == "ACTIVE" {
			return &history[i]
		}
	}
	return nil
}

func artifactPromotionCreatedAfterStartRow(target string, active *platform.AppVersionEvent, runStart time.Time) RequiredCheck {
	field := "created_after_start"
	if active == nil {
		return notFoundArtifactPromotionRow(target, field, "no ACTIVE appVersion found for target service")
	}
	created, err := time.Parse(time.RFC3339, active.Created)
	if err != nil {
		return RequiredCheck{
			ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
			Result: CheckBlocked, Source: "SearchAppVersions",
			Message: fmt.Sprintf("could not parse appVersion created timestamp %q: %v", active.Created, err),
		}
	}
	if !created.After(runStart) {
		return RequiredCheck{
			ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
			Result: CheckFailed, Expected: "created after run start", Observed: active.Created, Source: "SearchAppVersions",
			Message: fmt.Sprintf("target's ACTIVE appVersion created %s is not after run start %s", active.Created, runStart.Format(time.RFC3339)),
		}
	}
	return RequiredCheck{
		ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
		Result: CheckPassed, Expected: "created after run start", Observed: active.Created, Source: "SearchAppVersions",
		Message: "target's ACTIVE appVersion was created after run start",
	}
}

func artifactPromotionSourceCliRow(target string, active *platform.AppVersionEvent) RequiredCheck {
	field := "source_cli"
	if active == nil {
		return notFoundArtifactPromotionRow(target, field, "no ACTIVE appVersion found for target service")
	}
	if active.Source != "CLI" {
		return RequiredCheck{
			ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
			Result: CheckFailed, Expected: "CLI", Observed: active.Source, Source: "SearchAppVersions",
			Message: fmt.Sprintf("target's ACTIVE appVersion source is %q, not the cross-deploy push source CLI", active.Source),
		}
	}
	return RequiredCheck{
		ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
		Result: CheckPassed, Expected: "CLI", Observed: active.Source, Source: "SearchAppVersions",
		Message: "target's ACTIVE appVersion source is CLI",
	}
}

func artifactPromotionNoGitSourceRow(target string, active *platform.AppVersionEvent) RequiredCheck {
	field := "no_git_source"
	if active == nil {
		return notFoundArtifactPromotionRow(target, field, "no ACTIVE appVersion found for target service")
	}
	if active.PublicGitSource != nil {
		return RequiredCheck{
			ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
			Result: CheckFailed, Expected: "no publicGitSource", Observed: active.PublicGitSource.GitURL, Source: "SearchAppVersions",
			Message: "target's ACTIVE appVersion carries a publicGitSource — it was built from git, not cross-deployed",
		}
	}
	return RequiredCheck{
		ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
		Result: CheckPassed, Expected: "no publicGitSource", Observed: "none", Source: "SearchAppVersions",
		Message: "target's ACTIVE appVersion carries no publicGitSource",
	}
}

func artifactPromotionDevUnchangedRow(target string, devActive *platform.AppVersionEvent, baseline *ScenarioBaseline) RequiredCheck {
	field := "dev_unchanged"
	if baseline == nil {
		return RequiredCheck{
			ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
			Result: CheckBlocked, Message: "no scenario baseline available to compare the dev service's ACTIVE appVersion against",
		}
	}
	if devActive == nil {
		return notFoundArtifactPromotionRow(target, field, "no ACTIVE appVersion found for dev service")
	}
	if devActive.ID != baseline.UnrelatedAppVersion {
		return RequiredCheck{
			ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
			Result: CheckFailed, Expected: baseline.UnrelatedAppVersion, Observed: devActive.ID, Source: "SearchAppVersions",
			Message: "dev service's ACTIVE appVersion id changed from the scenario baseline",
		}
	}
	return RequiredCheck{
		ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
		Result: CheckPassed, Expected: baseline.UnrelatedAppVersion, Observed: devActive.ID, Source: "SearchAppVersions",
		Message: "dev service's ACTIVE appVersion id is unchanged from baseline",
	}
}

func notFoundArtifactPromotionRow(target, field, message string) RequiredCheck {
	return RequiredCheck{
		ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
		Result: CheckFailed, Message: message,
	}
}

func blockedArtifactPromotionRows(target, message, source string) []RequiredCheck {
	rows := make([]RequiredCheck, 0, len(artifactPromotionFields))
	for _, field := range artifactPromotionFields {
		rows = append(rows, RequiredCheck{
			ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
			Result: CheckBlocked, Source: source, Message: message,
		})
	}
	return rows
}
