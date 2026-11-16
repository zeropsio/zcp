package ops

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/platform"
)

// SV-READY — While a stage still builds and every pool is done, the headline
// is "ready".
// witness: internal/ops/standup_verdict.go | if r.Kind == mate.RowPool { → if r.Kind == mate.RowStage {
func TestStandupVerdict_SVReady_PoolsDoneStageBuilding_Ready(t *testing.T) {
	t.Parallel()
	got := StandupHeadlineOf([]mate.StandupRow{
		{Hostname: "webpool", Kind: mate.RowPool, State: mate.RowDone},
		{Hostname: "webstage", Kind: mate.RowStage, State: mate.RowBuilding},
	})
	if got != mate.HeadlineReady {
		t.Fatalf("headline = %q, want %q", got, mate.HeadlineReady)
	}
}

// SV-F17 — With two parts where one failed and the other is done, the
// headline is "failed", never "done".
// witness: internal/ops/standup_verdict.go | return mate.HeadlineFailed → continue
func TestStandupVerdict_SVF17_OnePartFailedOneDone_Failed(t *testing.T) {
	t.Parallel()
	got := StandupHeadlineOf([]mate.StandupRow{
		{Hostname: "webpool", Kind: mate.RowPool, State: mate.RowDone},
		{Hostname: "apipool", Kind: mate.RowPool, State: mate.RowFailed, Class: platform.ClassFailed},
	})
	if got != mate.HeadlineFailed {
		t.Fatalf("headline = %q, want %q", got, mate.HeadlineFailed)
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
	if strings.Contains(strings.ToLower(row.Cause), "restart") {
		t.Errorf("cause %q tells to restart", row.Cause)
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
	if !strings.HasPrefix(row.Cause, words) {
		t.Errorf("cause %q does not start with the refusal's words", row.Cause)
	}
}

// SV-F5 — A failed first pool build is one "failed" row with its commit and
// build process, whose next step fixes the code on zcp's own container, not
// a bearer cause.
// witness: internal/ops/standup_verdict.go | if in.Kind == mate.RowPool { → if in.Kind == mate.RowStage {
func TestStandupVerdict_SVF5_FailedFirstPoolBuild_OneRowFixOnZcp(t *testing.T) {
	t.Parallel()
	row := StandupRowOf(StandupRowInput{
		Hostname: "pwpool", Kind: mate.RowPool, Commit: "3f1c2a9",
		AppVersionID: "yNHYDse6", AppVersionStatus: "BUILD_FAILED", ProcessID: "pr2", ProcessStatus: "FAILED",
	})
	if row.State != mate.RowFailed || row.Class != platform.ClassFailed {
		t.Fatalf("state, class = %q, %q, want %q, %q", row.State, row.Class, mate.RowFailed, platform.ClassFailed)
	}
	if row.Commit != "3f1c2a9" || row.ProcessID != "pr2" {
		t.Errorf("commit, process = %q, %q, want 3f1c2a9, pr2", row.Commit, row.ProcessID)
	}
	if !strings.Contains(row.Cause, "zcp's own container") || strings.Contains(row.Cause, "LEASE_TOKEN") {
		t.Errorf("cause %q: want the fix on zcp's own container and no bearer cause", row.Cause)
	}
}
