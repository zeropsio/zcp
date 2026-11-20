// Tests for: handleWorkSessionClose — close always succeeds.
//
// Close is session cleanup, not commitment: any code edits live on the
// SSHFS mount, any deploys live on the platform. Close removes the per-PID
// session file; nothing of substance is lost. Auto-close (scope-all-green)
// is the "task done, verified" signal; manual close is "I'm done here, for
// whatever reason". Both delete the file.
//
// The previous close guard blocked close-without-deploy to catch "agent
// edited code then forgot to deploy" regressions. In practice it created
// friction on legitimate pivots (the agent worked around it via direct
// tools), and with the scope-explicit invariant restored (auto-close fires
// reliably on task completion) the guard's raison d'être evaporated.
package tools

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// closeTestEngine builds an engine rooted at a fresh temp dir. Returns the
// engine and the dir so callers can seed work sessions directly.
func closeTestEngine(t *testing.T) (*workflow.Engine, string) {
	t.Helper()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	t.Cleanup(func() {
		// Always clean the PID's work session — tests can leave one on disk
		// if they seeded but the test helper didn't delete via close.
		_ = workflow.DeleteWorkSession(dir, os.Getpid())
		_ = workflow.UnregisterSession(dir, workflow.WorkSessionID(os.Getpid()))
	})
	return engine, dir
}

// seedOpenWorkSession writes a work session with the given deploy history.
// `deploySucceeded` controls whether the single deploy attempt carries a
// non-empty SucceededAt — informational since the guard is gone but still
// useful to confirm close doesn't care about deploy state either way.
func seedOpenWorkSession(t *testing.T, dir string, deploySucceeded bool) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	ws := workflow.NewWorkSession("proj1", string(workflow.EnvContainer), "test intent", []string{"appdev"})
	if deploySucceeded {
		ws.Deploys = map[string][]workflow.DeployAttempt{
			"appdev": {{AttemptedAt: now, SucceededAt: now, Setup: "dev"}},
		}
	}
	if err := workflow.SaveWorkSession(dir, ws); err != nil {
		t.Fatalf("SaveWorkSession: %v", err)
	}
}

// seedAutoClosedSession writes a work session that already auto-closed
// (ClosedAt + CloseReasonAutoComplete). A follow-up manual close must still
// remove the file — close is idempotent.
func seedAutoClosedSession(t *testing.T, dir string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	ws := workflow.NewWorkSession("proj1", string(workflow.EnvContainer), "done", []string{"appdev"})
	ws.Deploys = map[string][]workflow.DeployAttempt{
		"appdev": {{AttemptedAt: now, SucceededAt: now}},
	}
	ws.Verifies = map[string][]workflow.VerifyAttempt{
		"appdev": {{AttemptedAt: now, PassedAt: now, Passed: true}},
	}
	ws.ClosedAt = now
	ws.CloseReason = workflow.CloseReasonAutoComplete
	if err := workflow.SaveWorkSession(dir, ws); err != nil {
		t.Fatalf("SaveWorkSession: %v", err)
	}
}

// closeInput shortens the WorkflowInput constructor — only the workflow
// value is relevant now that the close guard is gone.
func closeInput() WorkflowInput {
	return WorkflowInput{
		Workflow: "develop",
		Action:   "close",
	}
}

func TestHandleWorkSessionClose_SuccessfulDeploy_Closes(t *testing.T) {
	t.Parallel()
	engine, dir := closeTestEngine(t)
	seedOpenWorkSession(t, dir, true /*deploySucceeded*/)

	result, _, err := handleWorkSessionClose(context.Background(), engine, nil, "", runtime.Info{}, closeInput())
	if err != nil {
		t.Fatalf("handleWorkSessionClose: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", extractText(result))
	}
	if ws, _ := workflow.CurrentWorkSession(dir); ws != nil {
		t.Error("work session should be deleted after successful close")
	}
}

// Close without any deploy history must succeed — the old guard blocked
// this with CLOSE_BLOCKED; the new contract treats close as session
// cleanup with no deploy precondition.
func TestHandleWorkSessionClose_NoDeploy_Closes(t *testing.T) {
	t.Parallel()
	engine, dir := closeTestEngine(t)
	seedOpenWorkSession(t, dir, false /*deploySucceeded*/)

	result, _, err := handleWorkSessionClose(context.Background(), engine, nil, "", runtime.Info{}, closeInput())
	if err != nil {
		t.Fatalf("handleWorkSessionClose: %v", err)
	}
	if result.IsError {
		t.Fatalf("close must succeed without deploy history, got:\n%s", extractText(result))
	}
	if ws, _ := workflow.CurrentWorkSession(dir); ws != nil {
		t.Error("work session should be deleted after close")
	}
}

// Auto-closed sessions (ClosedAt set) still get removed by an explicit
// follow-up close. The handler is idempotent.
func TestHandleWorkSessionClose_AutoClosedSession_Closes(t *testing.T) {
	t.Parallel()
	engine, dir := closeTestEngine(t)
	seedAutoClosedSession(t, dir)

	result, _, err := handleWorkSessionClose(context.Background(), engine, nil, "", runtime.Info{}, closeInput())
	if err != nil {
		t.Fatalf("handleWorkSessionClose: %v", err)
	}
	if result.IsError {
		t.Fatalf("auto-closed session close must succeed, got:\n%s", extractText(result))
	}
	if ws, _ := workflow.CurrentWorkSession(dir); ws != nil {
		t.Error("auto-closed session should be removed after explicit close")
	}
}

// No session on disk: the call is idempotent and succeeds. An LLM calling
// close twice after context compaction should not get an error — the
// second call is a legal no-op.
func TestHandleWorkSessionClose_NoSession_NoOp(t *testing.T) {
	t.Parallel()
	engine, _ := closeTestEngine(t)
	result, _, err := handleWorkSessionClose(context.Background(), engine, nil, "", runtime.Info{}, closeInput())
	if err != nil {
		t.Fatalf("handleWorkSessionClose: %v", err)
	}
	if result.IsError {
		t.Fatalf("close without session must not error, got:\n%s", extractText(result))
	}
}

// handleWorkSessionClose must reject a non-develop workflow value.
func TestHandleWorkSessionClose_WrongWorkflow_Rejected(t *testing.T) {
	t.Parallel()
	engine, _ := closeTestEngine(t)
	result, _, err := handleWorkSessionClose(context.Background(), engine, nil, "", runtime.Info{}, WorkflowInput{
		Workflow: "bootstrap",
		Action:   "close",
	})
	if err != nil {
		t.Fatalf("handleWorkSessionClose: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error for workflow=bootstrap close")
	}
}

// End-to-end through RegisterWorkflow: confirms close surfaces at the MCP
// tool boundary and always succeeds.
func TestWorkflowTool_CloseAlwaysSucceeds(t *testing.T) {
	t.Parallel()
	engine, dir := closeTestEngine(t)
	seedOpenWorkSession(t, dir, false /*deploySucceeded*/)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, engine, nil, dir, "", nil, nil, runtime.Info{}, "")

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "close",
		"workflow": "develop",
	})
	if result.IsError {
		t.Fatalf("close must succeed, got:\n%s", getTextContent(t, result))
	}
	if ws, _ := workflow.CurrentWorkSession(dir); ws != nil {
		t.Error("work session not deleted after close")
	}
}

// Guard against context mutation: close shouldn't care about input fields
// unrelated to close (intent, plan, etc.).
func TestHandleWorkSessionClose_IgnoresUnrelatedInputFields(t *testing.T) {
	t.Parallel()
	engine, dir := closeTestEngine(t)
	seedOpenWorkSession(t, dir, true)

	in := WorkflowInput{
		Workflow:    "develop",
		Action:      "close",
		Intent:      "whatever",
		Attestation: "whatever",
		Step:        "discover",
	}
	result, _, err := handleWorkSessionClose(context.Background(), engine, nil, "", runtime.Info{}, in)
	if err != nil {
		t.Fatalf("handleWorkSessionClose: %v", err)
	}
	if result.IsError {
		t.Fatalf("close should ignore unrelated fields, got:\n%s", extractText(result))
	}
}

// unproposedHQ is the HQ the Mate of these tests presents itself to.
const unproposedHQ = "https://hq.example.com"

// writeUnproposedLeaseRecord records zcp's lease noolapool-1 of the Mate's HQ
// repository app-1/noola on branch.
func writeUnproposedLeaseRecord(t *testing.T, stateDir, branch string) {
	t.Helper()
	dir := filepath.Join(stateDir, "leases")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	record := `{"poolHostname":"noolapool","poolServiceId":"svc-noolapool","containerId":"c-1","containerNumber":1,` +
		`"containerHostname":"node-id-1.runtime.noolapool.zerops","lease":"secret-lease-value","branch":"` + branch + `",` +
		`"repo":"` + unproposedHQ + `/git/org-1/app-1/noola.git","acquiredAt":"2026-10-27T09:30:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "noolapool-1.json"), []byte(record), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestHandleWorkSessionClose_InAMate_NamesALeaseBranchNoChangeProposes: in a
// Mate, closing the work names each branch zcp's leases of its HQ repository
// hold that no change proposes yet, with the open-change call — Odin's
// 206-commit dev-pools had no change until the person asked (2026-10-27).
func TestHandleWorkSessionClose_InAMate_NamesALeaseBranchNoChangeProposes(t *testing.T) {
	t.Parallel()
	mate := runtime.Info{InContainer: true, MateEnabled: true, OrgID: "org-1", HQ: &runtime.HQLink{Origin: unproposedHQ}}
	tests := []struct {
		name   string
		rt     runtime.Info
		branch string
		change bool
		want   bool
	}{
		{name: "a lease branch no change proposes", rt: mate, branch: "dev-pools", want: true},
		{name: "its change is open", rt: mate, branch: "dev-pools", change: true},
		{name: "a lease of main", rt: mate, branch: "main"},
		{name: "outside a Mate", rt: runtime.Info{InContainer: true}, branch: "dev-pools"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			engine, dir := closeTestEngine(t)
			writeUnproposedLeaseRecord(t, dir, tt.branch)
			if tt.change {
				if err := workflow.RecordHQChange(dir, workflow.HQChange{AppID: "app-1", Repo: "noola", Number: 1, Branch: tt.branch}); err != nil {
					t.Fatal(err)
				}
			}
			result, _, err := handleWorkSessionClose(context.Background(), engine, nil, "", tt.rt, closeInput())
			if err != nil || result.IsError {
				t.Fatalf("close: %v %s", err, extractText(result))
			}
			text := extractText(result)
			named := strings.Contains(text, `action="open-change"`) && strings.Contains(text, "dev-pools") && strings.Contains(text, "noolapool-1")
			if named != tt.want {
				t.Errorf("close names the open-change of dev-pools = %v, want %v: %s", named, tt.want, text)
			}
		})
	}
}

// TestUnproposedLeaseBranches_OffersOnlyTheMatesOwnOpenChangesInTheRepository:
// the close-out note naming a lease branch no change proposes also offers
// the Mate's own open changes in that repository to add the work to — never
// one a person handed it, one in another repository or one told merged, which
// AGENTS.md's "What one change holds" never adds to or which no longer takes
// work (docs/spec-mate.md §10.10, docs/spec-workflows.md DP-37).
// witness: internal/tools/hq_change_unproposed.go | c.Source == workflow.HQChangeFromHanded || c.Branch == "" → c.Branch == ""
func TestUnproposedLeaseBranches_OffersOnlyTheMatesOwnOpenChangesInTheRepository(t *testing.T) {
	t.Parallel()
	mate := runtime.Info{InContainer: true, MateEnabled: true, OrgID: "org-1", HQ: &runtime.HQLink{Origin: unproposedHQ}}
	tests := []struct {
		name   string
		change *workflow.HQChange
		want   []int
	}{
		{name: "its own open change in the repository", change: &workflow.HQChange{AppID: "app-1", Repo: "noola", Number: 2, Branch: "polish", Source: workflow.HQChangeFromOpen}, want: []int{2}},
		{name: "a change handed to it", change: &workflow.HQChange{AppID: "app-1", Repo: "noola", Number: 3, Branch: "mate/gone/3", Source: workflow.HQChangeFromHanded}},
		{name: "its open change in another repository", change: &workflow.HQChange{AppID: "app-1", Repo: "api", Number: 4, Branch: "polish", Source: workflow.HQChangeFromOpen}},
		{name: "its change told merged", change: &workflow.HQChange{AppID: "app-1", Repo: "noola", Number: 5, Branch: "polish", Source: workflow.HQChangeFromOpen, Told: &workflow.HQChangeTold{State: hq.ChangeMerged}}},
		{name: "no change at all"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeUnproposedLeaseRecord(t, dir, "dev-pools")
			if tt.change != nil {
				if err := workflow.RecordHQChange(dir, *tt.change); err != nil {
					t.Fatal(err)
				}
			}
			branches := unproposedLeaseBranches(dir, mate, nil)
			if len(branches) != 1 || branches[0].branch != "dev-pools" {
				t.Fatalf("unproposed branches = %+v, want dev-pools alone", branches)
			}
			got := make([]int, 0, len(branches[0].addTo))
			for _, c := range branches[0].addTo {
				got = append(got, c.Number)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("open changes offered = %v, want %v", got, tt.want)
			}
		})
	}
}
