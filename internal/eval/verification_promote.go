package eval

import (
	"context"
	"fmt"
	"sync"
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
// target's ACTIVE appVersion, as opposed to dev_unchanged (finding E2:
// dev_unchanged is graded straight from the direct-read service list and
// the scenario baseline — it never touches SearchAppVersions).
var artifactPromotionTargetFields = []string{"created_after_start", "source_cli", "no_git_source"}

// artifactPromotionIndexRetryMu guards artifactPromotionIndexRetryBackoffs.
var (
	artifactPromotionIndexRetryMu       sync.RWMutex
	artifactPromotionIndexRetryBackoffs = []time.Duration{
		500 * time.Millisecond,
		1 * time.Second,
		2 * time.Second,
	}
)

// OverrideArtifactPromotionIndexRetryForTest swaps the backoff sequence used
// when a target's direct-read active appVersion id is not yet visible in the
// ES-backed SearchAppVersions index (finding E2, docs/spec-eval-farm.md
// §4.4 O7, CLAUDE.md's ES-lag caveat). Returns a restorer the caller defers.
// Test-only.
func OverrideArtifactPromotionIndexRetryForTest(backoffs []time.Duration) func() {
	artifactPromotionIndexRetryMu.Lock()
	prev := artifactPromotionIndexRetryBackoffs
	artifactPromotionIndexRetryBackoffs = backoffs
	artifactPromotionIndexRetryMu.Unlock()
	return func() {
		artifactPromotionIndexRetryMu.Lock()
		artifactPromotionIndexRetryBackoffs = prev
		artifactPromotionIndexRetryMu.Unlock()
	}
}

func artifactPromotionIndexRetrySnapshot() []time.Duration {
	artifactPromotionIndexRetryMu.RLock()
	defer artifactPromotionIndexRetryMu.RUnlock()
	out := make([]time.Duration, len(artifactPromotionIndexRetryBackoffs))
	copy(out, artifactPromotionIndexRetryBackoffs)
	return out
}

// evaluateArtifactPromotionRows evaluates the O7 artifact-promotion oracle
// (docs/spec-eval-farm.md §4.4 O7) for one ArtifactPromotionEntry: after a
// dev (entry.From) → stage (entry.To) promote, the target's ACTIVE
// appVersion must have been created after runStart, carry source "CLI",
// carry no publicGitSource, and the dev service's ACTIVE appVersion id must
// be unchanged from baseline.
//
// Finding E2 (live gate8): the target's active appVersion id is read from
// the direct, lag-free ListServicesDirect (ServiceStack.ActiveAppVersion.ID)
// rather than by scanning SearchAppVersions for a Status=="ACTIVE" entry —
// the ES-backed search can lag long enough that no entry for the service
// reads ACTIVE yet, which previously produced a false "no ACTIVE appVersion
// found" on a target that was genuinely ACTIVE. That id is then looked up
// by exact match inside SearchAppVersions (regardless of its own Status
// field) to read created/source/publicGitSource; when the id is not yet
// indexed there, the lookup retries with a short backoff before blocking.
// dev_unchanged never touches SearchAppVersions at all: it compares the
// dev service's direct-read active id straight against the scenario
// baseline.
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

	targetActiveID := activeAppVersionID(targetSvc)
	if targetActiveID == "" {
		rows := make([]RequiredCheck, 0, len(artifactPromotionFields))
		for _, field := range artifactPromotionTargetFields {
			rows = append(rows, notFoundArtifactPromotionRow(entry.To, field, "no ACTIVE appVersion found for target service"))
		}
		return append(rows, devUnchangedRow)
	}

	targetActive, found, err := findIndexedAppVersion(ctx, client, projectID, targetActiveID)
	if err != nil {
		msg := fmt.Sprintf("SearchAppVersions failed: %v", err)
		rows := make([]RequiredCheck, 0, len(artifactPromotionFields))
		for _, field := range artifactPromotionTargetFields {
			rows = append(rows, blockedArtifactPromotionRow(entry.To, field, "SearchAppVersions", msg))
		}
		return append(rows, devUnchangedRow)
	}
	if !found {
		msg := fmt.Sprintf("appVersion %s not yet indexed", targetActiveID)
		rows := make([]RequiredCheck, 0, len(artifactPromotionFields))
		for _, field := range artifactPromotionTargetFields {
			rows = append(rows, blockedArtifactPromotionRow(entry.To, field, "SearchAppVersions", msg))
		}
		return append(rows, devUnchangedRow)
	}

	rows := []RequiredCheck{
		artifactPromotionCreatedAfterStartRow(entry.To, targetActive, runStart),
		artifactPromotionSourceCliRow(entry.To, targetActive),
		artifactPromotionNoGitSourceRow(entry.To, targetActive),
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

// findAppVersionByID returns the event in events whose ID equals id, or nil.
// Matches by id alone — never by Status — since the ES-backed search's
// Status field can lag behind the direct-read service list (finding E2).
func findAppVersionByID(events []platform.AppVersionEvent, id string) *platform.AppVersionEvent {
	for i := range events {
		if events[i].ID == id {
			return &events[i]
		}
	}
	return nil
}

// findIndexedAppVersion looks up id (the direct-read active appVersion id)
// within the ES-backed SearchAppVersions index, retrying with a short
// backoff when id is not yet visible there. found is false — with a nil
// error — when every attempt completes without finding id; the caller
// reports "not yet indexed" (finding E2).
func findIndexedAppVersion(ctx context.Context, client platform.Client, projectID, id string) (event platform.AppVersionEvent, found bool, err error) {
	events, err := client.SearchAppVersions(ctx, projectID, 0)
	if err != nil {
		return platform.AppVersionEvent{}, false, err
	}
	if ev := findAppVersionByID(events, id); ev != nil {
		return *ev, true, nil
	}
	for _, delay := range artifactPromotionIndexRetrySnapshot() {
		select {
		case <-ctx.Done():
			return platform.AppVersionEvent{}, false, nil
		case <-time.After(delay):
		}
		events, err = client.SearchAppVersions(ctx, projectID, 0)
		if err != nil {
			return platform.AppVersionEvent{}, false, err
		}
		if ev := findAppVersionByID(events, id); ev != nil {
			return *ev, true, nil
		}
	}
	return platform.AppVersionEvent{}, false, nil
}

func artifactPromotionCreatedAfterStartRow(target string, active platform.AppVersionEvent, runStart time.Time) RequiredCheck {
	field := "created_after_start"
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

func artifactPromotionSourceCliRow(target string, active platform.AppVersionEvent) RequiredCheck {
	field := "source_cli"
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

func artifactPromotionNoGitSourceRow(target string, active platform.AppVersionEvent) RequiredCheck {
	field := "no_git_source"
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

// artifactPromotionDevUnchangedRow grades dev_unchanged straight from the
// direct-read dev service id (devActiveID, from ListServicesDirect) against
// the scenario baseline — it never depends on SearchAppVersions succeeding
// or having indexed anything (finding E2).
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
