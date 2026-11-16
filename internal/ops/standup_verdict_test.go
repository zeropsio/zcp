package ops

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/platform"
)

// SV-READY — While a stage still builds and every pool is done, the headline
// is "ready".
// witness: internal/mate/status.go | poolsDone = poolsDone && r.Kind != RowPool → poolsDone = poolsDone && r.Kind != RowStage
func TestStandupVerdict_SVReady_PoolsDoneStageBuilding_Ready(t *testing.T) {
	t.Parallel()
	got := mate.StandupHeadlineOf([]mate.StandupRow{
		{Hostname: "webpool", Kind: mate.RowPool, State: mate.RowDone},
		{Hostname: "webstage", Kind: mate.RowStage, State: mate.RowBuilding},
	})
	if got != mate.HeadlineReady {
		t.Fatalf("headline = %q, want %q", got, mate.HeadlineReady)
	}
}

// SV-DONE — Once every pool and every stage is done, the headline is "done",
// not "ready".
// witness: internal/mate/status.go | return HeadlineDone → return HeadlineReady
func TestStandupVerdict_SVDone_EveryRowDone_Done(t *testing.T) {
	t.Parallel()
	got := mate.StandupHeadlineOf([]mate.StandupRow{
		{Hostname: "webpool", Kind: mate.RowPool, State: mate.RowDone},
		{Hostname: "webstage", Kind: mate.RowStage, State: mate.RowDone},
	})
	if got != mate.HeadlineDone {
		t.Fatalf("headline = %q, want %q", got, mate.HeadlineDone)
	}
}

// SV-BUILDING — While a pool still builds, the headline is "building", never
// "ready", even when the stage is done.
// witness: internal/mate/status.go | if poolsDone && pools > 0 { → if pools > 0 {
func TestStandupVerdict_SVBuilding_PoolBuildingStageDone_Building(t *testing.T) {
	t.Parallel()
	got := mate.StandupHeadlineOf([]mate.StandupRow{
		{Hostname: "webpool", Kind: mate.RowPool, State: mate.RowBuilding},
		{Hostname: "webstage", Kind: mate.RowStage, State: mate.RowDone},
	})
	if got != mate.HeadlineBuilding {
		t.Fatalf("headline = %q, want %q", got, mate.HeadlineBuilding)
	}
}

// SV-F17 — With two pools where one failed and the other is done, the
// headline is "failed", never "done".
// witness: internal/mate/status.go | if r.State == RowFailed { → if r.State == RowFailed && r.Kind == RowStage {
func TestStandupVerdict_SVF17_OnePartFailedOneDone_Failed(t *testing.T) {
	t.Parallel()
	got := mate.StandupHeadlineOf([]mate.StandupRow{
		{Hostname: "webpool", Kind: mate.RowPool, State: mate.RowDone},
		{Hostname: "apipool", Kind: mate.RowPool, State: mate.RowFailed, Class: platform.ClassFailed},
	})
	if got != mate.HeadlineFailed {
		t.Fatalf("headline = %q, want %q", got, mate.HeadlineFailed)
	}
}

// SV-F6 — With the pools done and the stage failed, the headline is
// "failed", so the agent's turn starts to fix the stage.
// witness: internal/mate/status.go | if r.State == RowFailed { → if r.State == RowFailed && r.Kind == RowPool {
func TestStandupVerdict_SVF6_PoolsDoneStageFailed_Failed(t *testing.T) {
	t.Parallel()
	got := mate.StandupHeadlineOf([]mate.StandupRow{
		{Hostname: "webpool", Kind: mate.RowPool, State: mate.RowDone},
		{Hostname: "webstage", Kind: mate.RowStage, State: mate.RowFailed, Class: platform.ClassFailed},
	})
	if got != mate.HeadlineFailed {
		t.Fatalf("headline = %q, want %q", got, mate.HeadlineFailed)
	}
}

// SV-POOL-DONE — A pool whose version is active and whose bearer reference
// resolves is "done", leasable.
// witness: internal/ops/standup_verdict.go | bearerMissing := in.Kind == mate.RowPool && !in.BearerResolves → bearerMissing := in.Kind == mate.RowPool
func TestStandupVerdict_SVPoolDone_ActiveAndBearerResolves_Done(t *testing.T) {
	t.Parallel()
	row := StandupRowOf(StandupRowInput{
		Hostname: "webpool", Kind: mate.RowPool, Commit: "3f1c2a9",
		AppVersionID: "nQZWI8p", AppVersionStatus: "ACTIVE", ProcessID: "pr1", ProcessStatus: "FINISHED",
		BearerResolves: true,
	})
	if row.State != mate.RowDone {
		t.Fatalf("state = %q, want %q", row.State, mate.RowDone)
	}
}

// SV-F2 — A service still being created, with no version and no failure, is
// "building", never "done".
// witness: internal/ops/standup_verdict.go | case active && !bearerMissing: → case !bearerMissing:
func TestStandupVerdict_SVF2_CreatingService_Building(t *testing.T) {
	t.Parallel()
	row := StandupRowOf(StandupRowInput{Hostname: "webstage", Kind: mate.RowStage, Commit: "3f1c2a9"})
	if row.State != mate.RowBuilding {
		t.Fatalf("state = %q, want %q", row.State, mate.RowBuilding)
	}
}

// SV-BEARER-WAIT — A pool whose version is active but whose bearer reference
// does not resolve yet is "building", not leasable from the active version
// alone.
// witness: internal/ops/standup_verdict.go | case active && !bearerMissing: → case active:
func TestStandupVerdict_SVBearerWait_ActivePoolUnresolvedBearer_Building(t *testing.T) {
	t.Parallel()
	row := StandupRowOf(StandupRowInput{
		Hostname: "webpool", Kind: mate.RowPool, Commit: "3f1c2a9",
		AppVersionID: "nQZWI8p", AppVersionStatus: "ACTIVE", ProcessID: "pr1", ProcessStatus: "FINISHED",
	})
	if row.State != mate.RowBuilding {
		t.Fatalf("state = %q, want %q", row.State, mate.RowBuilding)
	}
}

// SV-F10 — A pool whose bearer still does not resolve once the wait is over
// is "failed" and its cause names the variable <pool>_LEASE_TOKEN, never
// "restart zcp".
// witness: internal/ops/standup_verdict.go | variable := in.Hostname + "_" + leaseTokenAliasKey → variable := in.Hostname + "_" + leaseTokenKey
func TestStandupVerdict_SVF10_BearerUnresolvedAfterWait_FailedNamesVariable(t *testing.T) {
	t.Parallel()
	row := StandupRowOf(StandupRowInput{
		Hostname: "webpool", Kind: mate.RowPool, Commit: "3f1c2a9",
		AppVersionID: "nQZWI8p", AppVersionStatus: "ACTIVE", ProcessID: "pr1", ProcessStatus: "FINISHED",
		Class: platform.ClassUnreachable,
	})
	if row.State != mate.RowFailed {
		t.Fatalf("state = %q, want %q", row.State, mate.RowFailed)
	}
	if !strings.Contains(row.Cause, "webpool_LEASE_TOKEN") {
		t.Errorf("cause %q does not name webpool_LEASE_TOKEN", row.Cause)
	}
	if strings.Contains(strings.ToLower(row.Cause+row.Next), "restart") {
		t.Errorf("cause %q, next %q tell to restart", row.Cause, row.Next)
	}
}

// SV-F4 — A refusal before any build is "failed" with class "refused" and
// the refusal's own words.
// witness: internal/ops/standup_verdict.go | row.State, row.Class = mate.RowFailed, in.Class → row.State, row.Class = mate.RowFailed, platform.ClassFailed
func TestStandupVerdict_SVF4_RefusedBeforeBuild_FailedRefused(t *testing.T) {
	t.Parallel()
	const words = "zerops.yaml on main has no setup webpool"
	row := StandupRowOf(StandupRowInput{
		Hostname: "webpool", Kind: mate.RowPool, Commit: "3f1c2a9",
		Class: platform.ClassRefused, Cause: words,
	})
	if row.State != mate.RowFailed || row.Class != platform.ClassRefused {
		t.Fatalf("state, class = %q, %q, want %q, %q", row.State, row.Class, mate.RowFailed, platform.ClassRefused)
	}
	if row.Cause != words {
		t.Errorf("cause = %q, want the refusal's words %q", row.Cause, words)
	}
}

// SV-F5 — A failed first pool build is one "failed" row with its commit and
// build process and the build's words, not the bearer failure met beside it,
// and its next step fixes the code on zcp's own container.
// witness: internal/ops/standup_verdict.go | case versionFailed(in.AppVersionStatus, in.ProcessStatus): → case in.Class == "" && versionFailed(in.AppVersionStatus, in.ProcessStatus):
func TestStandupVerdict_SVF5_FailedFirstPoolBuild_OneRowFixOnZcp(t *testing.T) {
	t.Parallel()
	const bearerWords = "pwpool_LEASE_TOKEN is still ${pwpool_API_TOKEN}"
	row := StandupRowOf(StandupRowInput{
		Hostname: "pwpool", Kind: mate.RowPool, Commit: "3f1c2a9",
		AppVersionID: "yNHYDse6", AppVersionStatus: "BUILD_FAILED", ProcessID: "pr2", ProcessStatus: "FAILED",
		Class: platform.ClassUnreachable, Cause: bearerWords,
	})
	if row.State != mate.RowFailed || row.Class != platform.ClassFailed {
		t.Fatalf("state, class = %q, %q, want %q, %q", row.State, row.Class, mate.RowFailed, platform.ClassFailed)
	}
	if row.Commit != "3f1c2a9" || row.ProcessID != "pr2" {
		t.Errorf("commit, process = %q, %q, want 3f1c2a9, pr2", row.Commit, row.ProcessID)
	}
	if !strings.Contains(row.Cause, "BUILD_FAILED") || strings.Contains(row.Cause, bearerWords) {
		t.Errorf("cause %q: want the build's words, not the bearer's", row.Cause)
	}
	if !strings.Contains(row.Next, "zcp's own container") {
		t.Errorf("next %q does not fix the code on zcp's own container", row.Next)
	}
}
