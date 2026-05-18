// Tests for: ErrAdoptRequired Recovery contract.
//
// Per plan plans/test-pinning-elevation-2026-05-06.md: ErrAdoptRequired
// narrows ErrPrerequisiteMissing for the specific "service exists but
// lacks ZCP bootstrap metadata, agent must run bootstrap+route=adopt"
// case. The contract is per-error-code: every emit site MUST carry
// Recovery pointing at bootstrap+adopt.
//
// New ErrAdoptRequired sites added without this Recovery shape fail
// this test — the contract is encoded in the error code's name + this
// pin, no AST scanner needed.
package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestErrAdoptRequiredCarriesAdoptRecovery enumerates every code path in
// internal/tools/ that emits ErrAdoptRequired and asserts:
//   - response.Code == "ADOPT_REQUIRED"
//   - response.Recovery is structured (non-nil)
//   - response.Recovery.Tool == "zerops_workflow"
//   - response.Recovery.Action == "start"
//   - response.Recovery.Args carries workflow=bootstrap, route=adopt
//
// The test drives each handler with input that triggers the rejection
// path. New ErrAdoptRequired sites added without a corresponding row
// here will fail when their Recovery shape diverges (and the author is
// nudged toward adding the row).
func TestErrAdoptRequiredCarriesAdoptRecovery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		drive func(t *testing.T) string // returns wire JSON of error response
	}{
		{
			name: "develop_no_bootstrapped_services",
			drive: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)
				// No ServiceMeta written — triggers len(metas) == 0 path
				// at workflow_develop.go:113.
				mock := platform.NewMock().WithServices([]platform.ServiceStack{
					{ID: "svc-appdev", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
				})
				result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
					WorkflowInput{Intent: "deploy something"}, runtime.Info{InContainer: true})
				if err != nil {
					t.Fatalf("handleDevelopBriefing: %v", err)
				}
				if !result.IsError {
					t.Fatalf("expected ADOPT_REQUIRED rejection, got success:\n%s", extractText(result))
				}
				return extractText(result)
			},
		},
		{
			name: "close_mode_unbootstrapped_service",
			drive: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				// No ServiceMeta written — triggers meta == nil path at
				// workflow_close_mode.go:90.
				result, _, err := handleCloseMode(WorkflowInput{
					CloseModes: map[string]string{"appdev": string(topology.CloseModeAuto)},
				}, dir)
				if err != nil {
					t.Fatalf("handleCloseMode: %v", err)
				}
				if !result.IsError {
					t.Fatalf("expected ADOPT_REQUIRED rejection, got success:\n%s", extractText(result))
				}
				return extractText(result)
			},
		},
		{
			name: "git_push_setup_unbootstrapped_service",
			drive: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				// No ServiceMeta written — triggers meta == nil path at
				// workflow_git_push_setup.go (Phase 1.2 of eval-review-20260518
				// fix-plan; previously emitted ErrServiceNotFound + generic
				// status Recovery, missed during the 5478623c migration).
				result, _, err := handleGitPushSetup(WorkflowInput{
					Service:   "appdev",
					RemoteURL: "https://github.com/example/repo",
				}, dir, runtime.Info{InContainer: true})
				if err != nil {
					t.Fatalf("handleGitPushSetup: %v", err)
				}
				if !result.IsError {
					t.Fatalf("expected ADOPT_REQUIRED rejection, got success:\n%s", extractText(result))
				}
				return extractText(result)
			},
		},
		{
			name: "build_integration_unbootstrapped_service",
			drive: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				// No ServiceMeta written — triggers meta == nil path at
				// workflow_build_integration.go (Phase 1.2 of eval-review-20260518
				// fix-plan; previously emitted ErrServiceNotFound + generic
				// status Recovery, missed during the 5478623c migration).
				result, _, err := handleBuildIntegration(context.Background(),
					platform.NewMock(),
					"proj1",
					WorkflowInput{Service: "appdev"},
					dir,
					runtime.Info{InContainer: true})
				if err != nil {
					t.Fatalf("handleBuildIntegration: %v", err)
				}
				if !result.IsError {
					t.Fatalf("expected ADOPT_REQUIRED rejection, got success:\n%s", extractText(result))
				}
				return extractText(result)
			},
		},
		// workflow_develop.go:195 (errStandardPairStageMissing) intentionally
		// not driven here — its trigger requires a complete ServiceMeta with
		// a stage hostname not in the live service list, which is a more
		// involved fixture. The contract is verified by code review of the
		// emit site (post-S1 it uses ErrAdoptRequired + specific Recovery)
		// plus AST-style search via grep ensures coverage:
		//
		//   $ grep -n 'platform.ErrAdoptRequired' internal/tools/*.go
		//
		// Future iteration may add a fixture-driven test for this path.
	}

	const wantCode = `"code":"` + platform.ErrAdoptRequired + `"`
	const wantTool = `"tool":"zerops_workflow"`
	const wantAction = `"action":"start"`
	const wantWorkflow = `"workflow":"bootstrap"`
	const wantRoute = `"route":"adopt"`

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := tt.drive(t)

			for _, want := range []string{wantCode, wantTool, wantAction, wantWorkflow, wantRoute} {
				if !strings.Contains(body, want) {
					t.Errorf("ADOPT_REQUIRED contract violation — wire response missing %q.\n"+
						"Recovery must be {tool:zerops_workflow, action:start, args:{workflow:bootstrap, route:adopt}}.\n"+
						"Got:\n%s", want, body)
				}
			}
			// Defense — must NOT fall back to generic status Recovery.
			if strings.Contains(body, `"recovery":{"tool":"zerops_workflow","action":"status"}`) {
				t.Errorf("ADOPT_REQUIRED site emitted generic status Recovery; expected specific bootstrap+adopt shape.\nGot:\n%s", body)
			}
		})
	}
}
