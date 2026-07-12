package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
)

// TestFindActiveLaunchState_Empty_ReturnsNil pins the no-state-dir +
// empty-state-dir paths: helper returns (nil, nil, nil) so the status
// handler falls through to the generic envelope.
func TestFindActiveLaunchState_Empty_ReturnsNil(t *testing.T) {
	t.Parallel()

	// No directory at all.
	active, all, err := findActiveLaunchState(t.TempDir(), "src")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if active != nil || all != nil {
		t.Errorf("expected nil/nil for empty dir, got %+v / %+v", active, all)
	}

	// Empty inputs.
	if a, l, e := findActiveLaunchState("", "src"); a != nil || l != nil || e != nil {
		t.Errorf("empty stateDir: got non-nil")
	}
	if a, l, e := findActiveLaunchState(t.TempDir(), ""); a != nil || l != nil || e != nil {
		t.Errorf("empty sourceProjectID: got non-nil")
	}
}

// TestFindActiveLaunchState_SingleActive_ReturnsIt pins the happy path:
// one non-terminal state file → return it.
func TestFindActiveLaunchState_SingleActive_ReturnsIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	state := &launchState{
		LaunchID:          "abc12345",
		SourceProjectID:   "src",
		TargetProjectName: "myapp-prod",
		Status:            topology.LaunchStatusReadyToLaunch,
	}
	if err := writeLaunchState(dir, state); err != nil {
		t.Fatalf("write: %v", err)
	}

	active, all, err := findActiveLaunchState(dir, "src")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if active == nil {
		t.Fatal("expected non-nil active state")
	}
	if active.LaunchID != "abc12345" || active.TargetProjectName != "myapp-prod" {
		t.Errorf("unexpected active state: %+v", active)
	}
	if len(all) != 1 {
		t.Errorf("expected len(all)=1, got %d", len(all))
	}
}

// TestFindActiveLaunchState_TerminalIgnored pins that LaunchStatusLaunched
// and LaunchStatusFailed are filtered out — terminal states must not
// hijack the status response.
func TestFindActiveLaunchState_TerminalIgnored(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	for _, status := range []topology.LaunchProductionStatus{topology.LaunchStatusLaunched, topology.LaunchStatusFailed} {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()
			sub := t.TempDir()
			state := &launchState{
				LaunchID:          "t" + string(status),
				SourceProjectID:   "src",
				TargetProjectName: "myapp-prod",
				Status:            status,
			}
			if err := writeLaunchState(sub, state); err != nil {
				t.Fatalf("write: %v", err)
			}
			active, all, err := findActiveLaunchState(sub, "src")
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if active != nil || len(all) != 0 {
				t.Errorf("terminal status %q must not surface: got %+v / len=%d", status, active, len(all))
			}
		})
	}
	_ = dir // outer subtests use their own dirs
}

// TestFindActiveLaunchState_DifferentSourceProjectIgnored pins
// project-scoping: state for source A is invisible when called with
// source B.
func TestFindActiveLaunchState_DifferentSourceProjectIgnored(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	state := &launchState{
		LaunchID:          "abc12345",
		SourceProjectID:   "src-A",
		TargetProjectName: "myapp-prod",
		Status:            topology.LaunchStatusReadyToLaunch,
	}
	if err := writeLaunchState(dir, state); err != nil {
		t.Fatalf("write: %v", err)
	}

	active, all, err := findActiveLaunchState(dir, "src-B")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if active != nil || len(all) != 0 {
		t.Errorf("expected no state for src-B; got %+v / %d", active, len(all))
	}
}

// TestFindActiveLaunchState_MultipleActiveSortedByLastUpdate pins the
// deterministic disambiguation: most-recently-updated state wins the
// `active` slot; full slice is sorted descending.
func TestFindActiveLaunchState_MultipleActiveSortedByLastUpdate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Write in non-time order, then mutate LastUpdate stamps directly
	// on disk so we know which is newer.
	oldState := &launchState{
		LaunchID:          "old00000",
		SourceProjectID:   "src",
		TargetProjectName: "myapp-prod-old",
		Status:            topology.LaunchStatusReadyToLaunch,
	}
	if err := writeLaunchState(dir, oldState); err != nil {
		t.Fatalf("write old: %v", err)
	}
	// Read back + rewrite with an artificially older LastUpdate.
	state, err := readLaunchState(dir, "old00000")
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	state.LastUpdate = time.Now().UTC().Add(-1 * time.Hour)
	if err := writeRawLaunchStateForTest(t, dir, state); err != nil {
		t.Fatalf("write raw: %v", err)
	}

	newState := &launchState{
		LaunchID:          "new00000",
		SourceProjectID:   "src",
		TargetProjectName: "myapp-prod-new",
		Status:            topology.LaunchStatusClassifyPrompt,
	}
	if err := writeLaunchState(dir, newState); err != nil {
		t.Fatalf("write new: %v", err)
	}

	active, all, err := findActiveLaunchState(dir, "src")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if active == nil {
		t.Fatal("expected non-nil active")
	}
	if active.LaunchID != "new00000" {
		t.Errorf("active.LaunchID: got %q want new00000 (most recent)", active.LaunchID)
	}
	if len(all) != 2 {
		t.Fatalf("expected len(all)=2, got %d", len(all))
	}
	if all[0].LaunchID != "new00000" || all[1].LaunchID != "old00000" {
		t.Errorf("sort order: got [%q, %q] want [new00000, old00000]", all[0].LaunchID, all[1].LaunchID)
	}
}

// writeRawLaunchStateForTest bypasses writeLaunchState's auto-stamp of
// LastUpdate so tests can set deterministic timestamps. Used only by
// the sort-order test.
func writeRawLaunchStateForTest(t *testing.T, stateDir string, state *launchState) error {
	t.Helper()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	path := launchStatePath(stateDir, state.LaunchID)
	return os.WriteFile(path, data, 0o600)
}

// TestStatusAction_NoActiveLaunch_FallsThrough pins regression: when
// no launch state exists, status returns the normal generic envelope
// (handleLifecycleStatus path). No launch-active envelope leaks.
func TestStatusAction_NoActiveLaunch_FallsThrough(t *testing.T) {
	t.Parallel()
	// findActiveLaunchState returns nil, so handler should fall through.
	// We exercise the helper directly here — the actual handler path
	// is covered by integration tests in the workflow package.
	active, _, _ := findActiveLaunchState(t.TempDir(), "src")
	if active != nil {
		t.Errorf("expected nil active state, got %+v", active)
	}
}

// TestRenderLaunchActiveRecovery_SingleActive verifies the envelope
// shape for the common case: one active launch, no ambiguity. No
// launchKey field, kind="launch-active", productionProjectName in
// next-call hint.
func TestRenderLaunchActiveRecovery_SingleActive(t *testing.T) {
	t.Parallel()
	state := &launchState{
		LaunchID:              "abc",
		SourceProjectID:       "src",
		TargetProjectName:     "myapp-prod",
		TargetProjectID:       "tgt-id",
		TargetServiceHostname: "appdev",
		Status:                topology.LaunchStatusReadyToLaunch,
		LastUpdate:            time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
	}
	result := renderLaunchActiveRecovery(nil, state, []*launchState{state})
	body := extractText(result)
	if !strings.Contains(body, `"kind":"launch-active"`) {
		t.Errorf("kind field missing or wrong: %s", body)
	}
	if !strings.Contains(body, `"targetProjectName":"myapp-prod"`) {
		t.Errorf("targetProjectName missing: %s", body)
	}
	// Next-call hint is JSON-escaped (productionProjectName=\"myapp-prod\")
	// — assert on the unescaped substring that survives.
	if !strings.Contains(body, `productionProjectName=`) {
		t.Errorf("next-call hint missing productionProjectName parameter: %s", body)
	}
	if !strings.Contains(body, `myapp-prod`) {
		t.Errorf("next-call hint missing target name: %s", body)
	}
	// P-LP-1 spot-check — envelope must not carry the key as a struct
	// field (`"launchKey": ...`). The guidance text legitimately mentions
	// the field name when telling the agent when to supply one; that's
	// not a leak. Pattern below catches only JSON property emission.
	if strings.Contains(body, `"launchKey":`) {
		t.Errorf("envelope leaks launchKey field: %s", body)
	}
}

// TestRenderLaunchActiveRecovery_MultipleActive surfaces the ambiguity
// hint when more than one non-terminal launch matches.
func TestRenderLaunchActiveRecovery_MultipleActive(t *testing.T) {
	t.Parallel()
	s1 := &launchState{LaunchID: "a", SourceProjectID: "src", TargetProjectName: "myapp-prod-1", Status: topology.LaunchStatusReadyToLaunch}
	s2 := &launchState{LaunchID: "b", SourceProjectID: "src", TargetProjectName: "myapp-prod-2", Status: topology.LaunchStatusClassifyPrompt}

	result := renderLaunchActiveRecovery(nil, s1, []*launchState{s1, s2})
	body := extractText(result)
	if !strings.Contains(body, `"ambiguousChoices"`) {
		t.Errorf("ambiguousChoices field missing in multi-active envelope: %s", body)
	}
	if !strings.Contains(body, "myapp-prod-2") {
		t.Errorf("second-choice target missing: %s", body)
	}
	if !strings.Contains(body, "Multiple active launches") {
		t.Errorf("ambiguity guidance suffix missing: %s", body)
	}
}

// TestFindActiveLaunchState_NoAdminClientConstructed pins P-LP-2:
// status-side recovery MUST be read-only over the state directory. If
// any code path tries to construct a ProjectAdminClient, it would
// hit the package factory — overriding the factory with a panicking
// implementation exposes a violation immediately.
func TestFindActiveLaunchState_NoAdminClientConstructed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	state := &launchState{
		LaunchID:          "abc",
		SourceProjectID:   "src",
		TargetProjectName: "myapp-prod",
		Status:            topology.LaunchStatusReadyToLaunch,
	}
	if err := writeLaunchState(dir, state); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Override the factory with a panicker; if findActiveLaunchState OR
	// renderLaunchActiveRecovery ever calls the factory, the test fails.
	restore := setProjectAdminClientFactory(func(_, _ string) (platform.ProjectAdminClient, error) {
		t.Fatal("P-LP-2 violation: ProjectAdminClient factory invoked on read-only status path")
		return nil, errors.New("p-lp-2-pin: factory must not be called")
	})
	defer restore()

	active, all, err := findActiveLaunchState(dir, "src")
	if err != nil {
		t.Fatalf("findActiveLaunchState: %v", err)
	}
	_ = renderLaunchActiveRecovery(nil, active, all)
}

// TestStatusAction_HandlerLevel_SurfacesLaunchActive is the integration
// pin: when status is called with an active launch on disk, the
// response carries the launch-active envelope (kind, productionProjectName,
// next-call hint).
func TestStatusAction_HandlerLevel_SurfacesLaunchActive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()

	// Seed an active launch state.
	state := &launchState{
		LaunchID:          "abc",
		SourceProjectID:   "p1",
		TargetProjectName: "myapp-prod",
		Status:            topology.LaunchStatusReadyToLaunch,
	}
	if err := writeLaunchState(dir, state); err != nil {
		t.Fatalf("write: %v", err)
	}

	mock := platform.NewMock().WithProject(&platform.Project{ID: "p1", Name: "src"})
	input := WorkflowInput{Action: "status"}

	// Status routes through handleWorkflowAction. The active-launch
	// short-circuit fires BEFORE handleLifecycleStatus and emits the
	// launch-active envelope.
	result := handleStatusForTest(ctx, mock, dir, "p1", input, runtime.Info{})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	body := extractText(result)
	if !strings.Contains(body, `"kind":"launch-active"`) {
		t.Errorf("status response missing launch-active envelope:\n%s", body)
	}
	if !strings.Contains(body, "myapp-prod") {
		t.Errorf("response missing targetProjectName: %s", body)
	}
}

// handleStatusForTest is a thin shim that exercises the relevant
// status branch directly so the test does not need a full
// workflow.Engine setup. Mirrors the integration path inside
// handleWorkflowAction.
func handleStatusForTest(_ context.Context, _ platform.Client, stateDir, projectID string, _ WorkflowInput, _ runtime.Info) *mcp.CallToolResult {
	if launchActive, allLaunches, _ := findActiveLaunchState(stateDir, projectID); launchActive != nil {
		return renderLaunchActiveRecovery(nil, launchActive, allLaunches)
	}
	if recent, _, _ := findRecentLaunchState(stateDir, projectID); recent != nil && isTerminalLaunchStatus(recent.Status) {
		return renderLaunchTerminalRecovery(nil, recent)
	}
	return nil
}

// --- FIX 1 PR 1: terminal-state surfacing -------------------------------

// TestFindRecentLaunchState_IncludesTerminal pins the new sister to
// findActiveLaunchState: terminal states (failed / launched) MUST be
// returned. findActiveLaunchState filters them out for pipeline-resume
// callers; findRecentLaunchState carries them for action="status".
func TestFindRecentLaunchState_IncludesTerminal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	state := &launchState{
		LaunchID:          "term1",
		SourceProjectID:   "src",
		TargetProjectName: "myapp-prod",
		Status:            topology.LaunchStatusFailed,
		LastUpdate:        time.Now(),
	}
	if err := writeLaunchState(dir, state); err != nil {
		t.Fatalf("write: %v", err)
	}

	recent, all, err := findRecentLaunchState(dir, "src")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if recent == nil {
		t.Fatal("expected non-nil terminal state — findRecentLaunchState MUST surface failed")
	}
	if recent.Status != topology.LaunchStatusFailed {
		t.Errorf("status = %q, want failed", recent.Status)
	}
	if len(all) != 1 {
		t.Errorf("len(all) = %d, want 1", len(all))
	}

	// Confirm findActiveLaunchState still filters terminal (P-LP-1 callers).
	active, _, _ := findActiveLaunchState(dir, "src")
	if active != nil {
		t.Errorf("findActiveLaunchState must STILL filter terminal — got %+v", active)
	}
}

// TestFindRecentLaunchState_PrefersMostRecent pins ordering: when both
// active and terminal exist, the most-recent by LastUpdate wins.
func TestFindRecentLaunchState_PrefersMostRecent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	older := &launchState{
		LaunchID:          "older",
		SourceProjectID:   "src",
		TargetProjectName: "myapp-prod-1",
		Status:            topology.LaunchStatusLaunched,
		LastUpdate:        time.Now().Add(-2 * time.Hour),
	}
	newer := &launchState{
		LaunchID:          "newer",
		SourceProjectID:   "src",
		TargetProjectName: "myapp-prod-2",
		Status:            topology.LaunchStatusFailed,
		LastUpdate:        time.Now(),
	}
	for _, s := range []*launchState{older, newer} {
		if err := writeLaunchState(dir, s); err != nil {
			t.Fatalf("write %s: %v", s.LaunchID, err)
		}
	}

	recent, all, err := findRecentLaunchState(dir, "src")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if recent == nil || recent.LaunchID != "newer" {
		t.Errorf("expected newer launch, got %+v", recent)
	}
	if len(all) != 2 {
		t.Errorf("len(all) = %d, want 2", len(all))
	}
}

// TestRenderLaunchTerminalRecovery_FailedWithTarget_PointsAtReset pins
// the failed-WITH-target envelope shape: the resume gate refuses a
// direct retry there (partial project exists), so kind="launch-failed"
// and NextCall includes action="reset".
func TestRenderLaunchTerminalRecovery_FailedWithTarget_PointsAtReset(t *testing.T) {
	t.Parallel()
	terminal := &launchState{
		LaunchID:          "f1",
		SourceProjectID:   "src",
		TargetProjectName: "myapp-prod",
		TargetProjectID:   "tgt-pid",
		Status:            topology.LaunchStatusFailed,
		LastUpdate:        time.Now(),
	}
	result := renderLaunchTerminalRecovery(nil, terminal)
	if result == nil {
		t.Fatal("nil result")
	}
	body := mustExtractEnvelope(t, result)
	if body.Kind != "launch-failed" {
		t.Errorf("Kind = %q, want launch-failed", body.Kind)
	}
	if !strings.Contains(body.NextCall, `action="reset"`) {
		t.Errorf("NextCall must include action=\"reset\", got %q", body.NextCall)
	}
	if !strings.Contains(body.NextCall, terminal.TargetProjectName) {
		t.Errorf("NextCall must echo TargetProjectName, got %q", body.NextCall)
	}
}

// TestRenderLaunchTerminalRecovery_FailedNoTargetDelegated_PointsAtRetry
// pins the 2026-07-12 audit #1 fix: a failed delegated launch with NO
// target project is directly retryable (resume-gate branch 2) and the
// staged token from the prior attempt is reused with zero delegation
// calls (§4.5). The status guidance must direct that retry — NOT reset,
// which deletes the staged token while the one-time delegation is
// already consumed.
func TestRenderLaunchTerminalRecovery_FailedNoTargetDelegated_PointsAtRetry(t *testing.T) {
	t.Parallel()
	terminal := &launchState{
		LaunchID:              "f2",
		SourceProjectID:       "src",
		TargetProjectName:     "myapp-prod",
		TargetServiceHostname: "app", // staging was reached — staged reuse is real
		Status:                topology.LaunchStatusFailed,
		TokenAcquisition:      "delegated",
		MintedTokenName:       "zcp-launch-myapp-prod",
		LastUpdate:            time.Now(),
	}
	result := renderLaunchTerminalRecovery(nil, terminal)
	body := mustExtractEnvelope(t, result)
	if body.Kind != "launch-failed" {
		t.Errorf("Kind = %q, want launch-failed", body.Kind)
	}
	if !strings.Contains(body.NextCall, `action="start"`) || !strings.Contains(body.NextCall, "confirmLaunch=true") {
		t.Errorf(`NextCall must direct the delegated retry (action="start" ... confirmLaunch=true), got %q`, body.NextCall)
	}
	if strings.Contains(body.NextCall, `action="reset"`) {
		t.Errorf("NextCall must NOT point a retryable delegated failure at reset, got %q", body.NextCall)
	}
	if !strings.Contains(body.Guidance, "staged") {
		t.Errorf("guidance must say the retry reuses the already-staged token; got %q", body.Guidance)
	}
	if !strings.Contains(body.Guidance, "reset") || !strings.Contains(body.Guidance, "delete") {
		t.Errorf("guidance must warn that reset deletes the staged token (abandonment only); got %q", body.Guidance)
	}
	// D-7 loop closure: the envelope surfaces the non-secret forensics so
	// a post-compaction status can still name the dashboard token.
	if body.TokenAcquisition != "delegated" {
		t.Errorf("TokenAcquisition = %q, want delegated", body.TokenAcquisition)
	}
	if body.MintedTokenName != "zcp-launch-myapp-prod" {
		t.Errorf("MintedTokenName = %q, want zcp-launch-myapp-prod", body.MintedTokenName)
	}
}

// TestRenderLaunchTerminalRecovery_FailedNoTargetDelegatedPreStage_NoStagedClaim
// pins the Codex-review fix (2026-07-12): a delegated abort BEFORE
// staging (race/indeterminate/empty-token/admin-factory failure) has NO
// staged token — the guidance must not promise "the staged token is
// reused"; it directs the self-correcting confirmLaunch retry and the
// possible dashboard-token recovery instead.
func TestRenderLaunchTerminalRecovery_FailedNoTargetDelegatedPreStage_NoStagedClaim(t *testing.T) {
	t.Parallel()
	terminal := &launchState{
		LaunchID:          "f4",
		SourceProjectID:   "src",
		TargetProjectName: "myapp-prod",
		Status:            topology.LaunchStatusFailed,
		TokenAcquisition:  "delegated",
		MintedTokenName:   "zcp-launch-myapp-prod",
		LastUpdate:        time.Now(),
	}
	result := renderLaunchTerminalRecovery(nil, terminal)
	body := mustExtractEnvelope(t, result)
	if strings.Contains(body.Guidance, "is reused") {
		t.Errorf("pre-staging abort must not claim a staged token is reused; got %q", body.Guidance)
	}
	if !strings.Contains(body.NextCall, "confirmLaunch=true") {
		t.Errorf("pre-staging delegated retry still goes through confirmLaunch (self-correcting probe), got %q", body.NextCall)
	}
	if !strings.Contains(body.Guidance, "zcp-launch-myapp-prod") || !strings.Contains(body.Guidance, "ask the user") {
		t.Errorf("guidance must name the possibly-created dashboard token and route regeneration through the user; got %q", body.Guidance)
	}
}

// TestRenderLaunchActiveRecovery_SurfacesDelegatedForensics pins the
// non-terminal sibling of the D-7 loop closure: a crash between the
// pre-mint state write and the abort leaves a `launching` state whose
// TokenAcquisition/MintedTokenName are the only surviving record that
// a standing dashboard token may exist — the launch-active envelope
// must surface them (plan-review adjustment, 2026-07-12).
func TestRenderLaunchActiveRecovery_SurfacesDelegatedForensics(t *testing.T) {
	t.Parallel()
	active := &launchState{
		LaunchID:          "a1",
		SourceProjectID:   "src",
		TargetProjectName: "myapp-prod",
		Status:            topology.LaunchStatusLaunching,
		TokenAcquisition:  "delegated",
		MintedTokenName:   "zcp-launch-myapp-prod",
		LastUpdate:        time.Now(),
	}
	result := renderLaunchActiveRecovery(nil, active, []*launchState{active})
	body := mustExtractEnvelope(t, result)
	if body.TokenAcquisition != "delegated" {
		t.Errorf("TokenAcquisition = %q, want delegated", body.TokenAcquisition)
	}
	if body.MintedTokenName != "zcp-launch-myapp-prod" {
		t.Errorf("MintedTokenName = %q, want zcp-launch-myapp-prod", body.MintedTokenName)
	}
}

// TestRenderLaunchTerminalRecovery_FailedNoTargetManual_PointsAtRetry
// pins the manual sibling: retryable, NextCall directs action="start",
// and none of the delegated staged-token narrative leaks in.
func TestRenderLaunchTerminalRecovery_FailedNoTargetManual_PointsAtRetry(t *testing.T) {
	t.Parallel()
	terminal := &launchState{
		LaunchID:          "f3",
		SourceProjectID:   "src",
		TargetProjectName: "myapp-prod",
		Status:            topology.LaunchStatusFailed,
		LastUpdate:        time.Now(),
	}
	result := renderLaunchTerminalRecovery(nil, terminal)
	body := mustExtractEnvelope(t, result)
	if !strings.Contains(body.NextCall, `action="start"`) {
		t.Errorf(`NextCall must direct a retry (action="start"), got %q`, body.NextCall)
	}
	if strings.Contains(body.NextCall, "confirmLaunch") {
		t.Errorf("manual-acquisition failure must not push confirmLaunch in NextCall, got %q", body.NextCall)
	}
	if strings.Contains(body.Guidance, "staged token") {
		t.Errorf("manual-acquisition guidance must not carry the delegated staged-token narrative; got %q", body.Guidance)
	}
	if body.TokenAcquisition != "" || body.MintedTokenName != "" {
		t.Errorf("manual failure must not surface delegated forensics; got acquisition=%q name=%q", body.TokenAcquisition, body.MintedTokenName)
	}
}

// TestLaunchOverlayAddendum_FailedBranchesOnRetryability pins the
// project-overlay sibling of the terminal recovery split: reset advice
// only for failed-with-target; the retryable no-target failure points
// at a retry.
func TestLaunchOverlayAddendum_FailedBranchesOnRetryability(t *testing.T) {
	t.Parallel()
	t.Run("failed with target -> reset advice", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := writeLaunchState(dir, &launchState{
			LaunchID: "L3", SourceProjectID: "proj1", TargetProjectName: "myapp-prod",
			TargetProjectID: "tgt-pid", Status: topology.LaunchStatusFailed,
		}); err != nil {
			t.Fatalf("writeLaunchState: %v", err)
		}
		got := launchOverlayAddendum(dir, "proj1")
		if !strings.Contains(got, `action=\"reset\"`) && !strings.Contains(got, `action="reset"`) {
			t.Errorf("failed-with-target overlay must advise reset; got %q", got)
		}
	})
	t.Run("failed delegated no target -> retry advice, no reset", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := writeLaunchState(dir, &launchState{
			LaunchID: "L4", SourceProjectID: "proj1", TargetProjectName: "myapp-prod",
			TargetServiceHostname: "app",
			Status:                topology.LaunchStatusFailed, TokenAcquisition: "delegated",
			MintedTokenName: "zcp-launch-myapp-prod",
		}); err != nil {
			t.Fatalf("writeLaunchState: %v", err)
		}
		got := launchOverlayAddendum(dir, "proj1")
		if strings.Contains(got, "reset") {
			t.Errorf("retryable delegated failure overlay must not advise reset; got %q", got)
		}
		if !strings.Contains(got, "confirmLaunch=true") {
			t.Errorf("retryable delegated failure overlay must direct the confirmLaunch retry; got %q", got)
		}
	})
}

// TestRenderLaunchTerminalRecovery_LaunchedConfirmsCompletion pins the
// launched envelope: kind="launch-completed", no destructive NextCall
// (launch is done — no reset suggested, no further start required).
func TestRenderLaunchTerminalRecovery_LaunchedConfirmsCompletion(t *testing.T) {
	t.Parallel()
	terminal := &launchState{
		LaunchID:          "l1",
		SourceProjectID:   "src",
		TargetProjectName: "myapp-prod",
		TargetProjectID:   "tgt-pid",
		Status:            topology.LaunchStatusLaunched,
		LastUpdate:        time.Now(),
	}
	result := renderLaunchTerminalRecovery(nil, terminal)
	body := mustExtractEnvelope(t, result)
	if body.Kind != "launch-completed" {
		t.Errorf("Kind = %q, want launch-completed", body.Kind)
	}
	if body.NextCall != "" {
		t.Errorf("NextCall must be empty for launched state (launch already done), got %q", body.NextCall)
	}
	if body.TargetProjectID != "tgt-pid" {
		t.Errorf("TargetProjectID = %q, want tgt-pid", body.TargetProjectID)
	}
}

// mustExtractEnvelope parses the MCP CallToolResult JSON body into the
// launchActiveEnvelope shape — both active and terminal recoveries use
// the same wire type.
func mustExtractEnvelope(t *testing.T, result *mcp.CallToolResult) launchActiveEnvelope {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] not TextContent: %T", result.Content[0])
	}
	var env launchActiveEnvelope
	if err := json.Unmarshal([]byte(text.Text), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return env
}

// silence unused-imports guard if mustExtractEnvelope unused in some
// future cleanup pass.
var _ = errors.New
