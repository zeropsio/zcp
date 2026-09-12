package eval

import (
	"context"
	"fmt"
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

// artifactPromotionTargetFields are the three fields evaluated from the
// target's ACTIVE appVersion, as opposed to dev_unchanged (dev_unchanged is
// graded straight from the direct-read service list and the scenario
// baseline).
var artifactPromotionTargetFields = []string{"created_after_start", "source_cli", "no_git_source"}

// evaluateArtifactPromotionRows evaluates the O7 artifact-promotion oracle
// (docs/spec-eval-farm.md §4.4 O7) for one ArtifactPromotionEntry: after a
// dev (entry.From) → stage (entry.To) promote, the target's ACTIVE
// appVersion must have been created after runStart, carry source "CLI",
// carry no publicGitSource, and the dev service's ACTIVE appVersion id must
// be unchanged from baseline.
//
// Finding E2/T4 (live gate8): every field is read straight off the direct,
// lag-free ListServicesDirect (ServiceStack.ActiveAppVersion) — never
// through the ES-backed SearchAppVersions index. The prior implementation
// found the target's active appVersion id via ListServicesDirect but then
// re-resolved created/source/publicGitSource by looking that id up inside
// SearchAppVersions, retrying while the ES index caught up; a finished
// cross-deploy could still read "not yet indexed" for several seconds. The
// direct read already carries created/source/publicGitSource (mapped by
// platform.mapActiveAppVersion), so there is nothing left to search for.
func evaluateArtifactPromotionRows(
	ctx context.Context,
	entry ArtifactPromotionEntry,
	client platform.Client,
	projectID string,
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
	targetSvc := findServiceByHostname(services, entry.To)
	devSvc := findServiceByHostname(services, entry.From)
	if targetSvc == nil {
		return blockedArtifactPromotionRows(entry.To, fmt.Sprintf("target service %q not found in project", entry.To), "ListServicesDirect")
	}
	if devSvc == nil {
		return blockedArtifactPromotionRows(entry.To, fmt.Sprintf("dev service %q not found in project", entry.From), "ListServicesDirect")
	}

	devUnchangedRow := artifactPromotionDevUnchangedRow(entry.To, entry.From, activeAppVersionID(devSvc), baseline)

	targetActive := targetSvc.ActiveAppVersion
	if targetActive == nil || targetActive.ID == "" {
		rows := make([]RequiredCheck, 0, len(artifactPromotionFields))
		for _, field := range artifactPromotionTargetFields {
			rows = append(rows, notFoundArtifactPromotionRow(entry.To, field, "no ACTIVE appVersion found for target service"))
		}
		return append(rows, devUnchangedRow)
	}

	rows := []RequiredCheck{
		artifactPromotionCreatedAfterStartRow(entry.To, *targetActive, runStart),
		artifactPromotionSourceCliRow(entry.To, *targetActive),
		artifactPromotionNoGitSourceRow(entry.To, *targetActive),
		devUnchangedRow,
	}
	return rows
}

// activeAppVersionID returns svc's active appVersion id from a direct
// (ListServicesDirect) read, or "" when the service has none.
func activeAppVersionID(svc *platform.ServiceStack) string {
	if svc.ActiveAppVersion == nil {
		return ""
	}
	return svc.ActiveAppVersion.ID
}

func artifactPromotionCreatedAfterStartRow(target string, active platform.ActiveAppVersionDigest, runStart time.Time) RequiredCheck {
	field := "created_after_start"
	created, err := time.Parse(time.RFC3339, active.Created)
	if err != nil {
		return RequiredCheck{
			ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
			Result: CheckBlocked, Source: "ListServicesDirect",
			Message: fmt.Sprintf("could not parse appVersion created timestamp %q: %v", active.Created, err),
		}
	}
	if !created.After(runStart) {
		return RequiredCheck{
			ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
			Result: CheckFailed, Expected: "created after run start", Observed: active.Created, Source: "ListServicesDirect",
			Message: fmt.Sprintf("target's ACTIVE appVersion created %s is not after run start %s", active.Created, runStart.Format(time.RFC3339)),
		}
	}
	return RequiredCheck{
		ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
		Result: CheckPassed, Expected: "created after run start", Observed: active.Created, Source: "ListServicesDirect",
		Message: "target's ACTIVE appVersion was created after run start",
	}
}

func artifactPromotionSourceCliRow(target string, active platform.ActiveAppVersionDigest) RequiredCheck {
	field := "source_cli"
	if active.Source != "CLI" {
		return RequiredCheck{
			ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
			Result: CheckFailed, Expected: "CLI", Observed: active.Source, Source: "ListServicesDirect",
			Message: fmt.Sprintf("target's ACTIVE appVersion source is %q, not the cross-deploy push source CLI", active.Source),
		}
	}
	return RequiredCheck{
		ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
		Result: CheckPassed, Expected: "CLI", Observed: active.Source, Source: "ListServicesDirect",
		Message: "target's ACTIVE appVersion source is CLI",
	}
}

func artifactPromotionNoGitSourceRow(target string, active platform.ActiveAppVersionDigest) RequiredCheck {
	field := "no_git_source"
	if active.PublicGitSource != nil {
		return RequiredCheck{
			ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
			Result: CheckFailed, Expected: "no publicGitSource", Observed: active.PublicGitSource.GitURL, Source: "ListServicesDirect",
			Message: "target's ACTIVE appVersion carries a publicGitSource — it was built from git, not cross-deployed",
		}
	}
	return RequiredCheck{
		ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
		Result: CheckPassed, Expected: "no publicGitSource", Observed: "none", Source: "ListServicesDirect",
		Message: "target's ACTIVE appVersion carries no publicGitSource",
	}
}

// artifactPromotionDevUnchangedRow grades dev_unchanged straight from the
// direct-read dev service id (devActiveID, from ListServicesDirect) against
// the scenario baseline.
func artifactPromotionDevUnchangedRow(target, devHostname, devActiveID string, baseline *ScenarioBaseline) RequiredCheck {
	field := "dev_unchanged"
	var baselineAppVersion string
	var haveBaseline bool
	if baseline != nil {
		baselineAppVersion, haveBaseline = baseline.AppVersions[devHostname]
	}
	if !haveBaseline || baselineAppVersion == "" {
		return RequiredCheck{
			ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
			Result: CheckBlocked, Message: fmt.Sprintf("no baseline for %s", devHostname),
		}
	}
	if devActiveID == "" {
		return notFoundArtifactPromotionRow(target, field, "no ACTIVE appVersion found for dev service")
	}
	if devActiveID != baselineAppVersion {
		return RequiredCheck{
			ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
			Result: CheckFailed, Expected: baselineAppVersion, Observed: devActiveID, Source: "ListServicesDirect",
			Message: "dev service's ACTIVE appVersion id changed from the scenario baseline",
		}
	}
	return RequiredCheck{
		ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
		Result: CheckPassed, Expected: baselineAppVersion, Observed: devActiveID, Source: "ListServicesDirect",
		Message: "dev service's ACTIVE appVersion id is unchanged from baseline",
	}
}

func notFoundArtifactPromotionRow(target, field, message string) RequiredCheck {
	return RequiredCheck{
		ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
		Result: CheckFailed, Message: message,
	}
}

func blockedArtifactPromotionRow(target, field, source, message string) RequiredCheck {
	return RequiredCheck{
		ID: artifactPromotionRowID(target, field), Check: "artifact_promotion", Scope: target,
		Result: CheckBlocked, Source: source, Message: message,
	}
}

func blockedArtifactPromotionRows(target, message, source string) []RequiredCheck {
	rows := make([]RequiredCheck, 0, len(artifactPromotionFields))
	for _, field := range artifactPromotionFields {
		rows = append(rows, blockedArtifactPromotionRow(target, field, source, message))
	}
	return rows
}
