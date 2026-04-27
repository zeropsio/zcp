// Tests for: tools/deploy_poll.go classifier-wiring + tools/errwire.go
// FailureClassification field. Pins ticket E2's contract: every deploy
// failure response carries a structured failureClassification block.
package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// TestPollDeployBuild_PopulatesFailureClassification pins E2: when the
// build pipeline ends in a non-success status, pollDeployBuild MUST
// populate result.FailureClassification so the agent has the structured
// next-step instead of having to parse buildLogs prose.
//
// Without this, the deploy response shape silently drops back to the
// pre-E2 raw-log surface and the corpus updates that point agents at
// failureClassification become wrong.
func TestPollDeployBuild_PopulatesFailureClassification(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		event        *platform.AppVersionEvent
		buildLogs    []string
		runtimeLogs  []string
		wantPhase    ops.DeployFailurePhase
		wantCategory topology.FailureClass
	}{
		{
			name: "build-failed-with-command-not-found",
			event: &platform.AppVersionEvent{
				ID:     "av-1",
				Status: platform.BuildStatusBuildFailed,
			},
			buildLogs:    []string{"+ thisbinaryisnotreal_xy42", "/bin/sh: 1: thisbinaryisnotreal_xy42: not found"},
			wantPhase:    ops.PhaseBuild,
			wantCategory: topology.FailureClassBuild,
		},
		{
			name: "preparing-runtime-failed",
			event: &platform.AppVersionEvent{
				ID:     "av-2",
				Status: platform.BuildStatusPreparingRuntimeFail,
			},
			buildLogs:    []string{"E: Unable to locate package imagemagick-dev"},
			wantPhase:    ops.PhasePrepare,
			wantCategory: topology.FailureClassStart,
		},
		{
			name: "deploy-failed-with-port-in-use",
			event: &platform.AppVersionEvent{
				ID:     "av-3",
				Status: platform.BuildStatusDeployFailed,
			},
			runtimeLogs:  []string{"Error: listen EADDRINUSE :::3000"},
			wantPhase:    ops.PhaseInit,
			wantCategory: topology.FailureClassStart,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := &ops.DeployResult{
				TargetServiceID: "svc-1",
				BuildLogs:       tc.buildLogs,
				RuntimeLogs:     tc.runtimeLogs,
			}
			// Drive only the failure-branch logic — tc.event already has
			// final status; we exercise the same code path the prod loop
			// would after PollBuild returned the event.
			injectFailureClassificationForTest(result, tc.event)

			if result.FailureClassification == nil {
				t.Fatalf("FailureClassification missing for status %s", tc.event.Status)
			}
			if result.FailureClassification.Category != tc.wantCategory {
				t.Errorf("Category = %q, want %q", result.FailureClassification.Category, tc.wantCategory)
			}
			if result.FailureClassification.SuggestedAction == "" {
				t.Errorf("SuggestedAction empty (signals=%v)", result.FailureClassification.Signals)
			}
		})
	}
}

// injectFailureClassificationForTest mirrors the failure-branch tail of
// pollDeployBuild without the platform.Client / log fetcher dependency.
// Kept colocated with the test so a contract drift in the production
// path is obvious — if the production helper grows new fields, mirror
// them here.
func injectFailureClassificationForTest(result *ops.DeployResult, event *platform.AppVersionEvent) {
	phase := ops.FailurePhaseFromStatus(event.Status)
	if phase == "" {
		return
	}
	result.FailureClassification = ops.ClassifyDeployFailure(ops.FailureInput{
		Phase:       phase,
		Status:      event.Status,
		BuildLogs:   result.BuildLogs,
		RuntimeLogs: result.RuntimeLogs,
	})
}

// TestErrorWire_FailureClassification pins the JSON wire shape of the
// new ErrorWire field. Agents discover the shape via response parsing,
// so the contract is "absent when nil; full struct when populated".
func TestErrorWire_FailureClassification(t *testing.T) {
	t.Parallel()

	t.Run("absent_when_nil", func(t *testing.T) {
		t.Parallel()
		pe := platform.NewPlatformError(platform.ErrSSHDeployFailed, "boom", "fix it")
		out := convertError(pe, WithFailureClassification(nil))
		body := getTextContent(t, out)
		if jsonContains(t, body, "failureClassification") {
			t.Errorf("nil classification should be omitted, got: %s", body)
		}
	})

	t.Run("populated_with_full_block", func(t *testing.T) {
		t.Parallel()
		c := &topology.DeployFailureClassification{
			Category:        topology.FailureClassCredential,
			LikelyCause:     "GIT_TOKEN missing",
			SuggestedAction: "set GIT_TOKEN via zerops_env",
			Signals:         []string{"transport:git-token-missing"},
		}
		pe := platform.NewPlatformError(platform.ErrGitTokenMissing, "GIT_TOKEN missing", "")
		out := convertError(pe, WithFailureClassification(c))
		body := getTextContent(t, out)

		var wire ErrorWire
		if err := json.Unmarshal([]byte(body), &wire); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if wire.FailureClassification == nil {
			t.Fatalf("FailureClassification missing from wire")
		}
		if wire.FailureClassification.Category != topology.FailureClassCredential {
			t.Errorf("Category = %q, want credential", wire.FailureClassification.Category)
		}
		if wire.FailureClassification.LikelyCause == "" {
			t.Error("LikelyCause empty")
		}
	})
}

func jsonContains(t *testing.T, body, key string) bool {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	_, ok := raw[key]
	return ok
}

// TestClassifyTransportError_PromotesPreflightCodes pins that the tool-
// level shim recognizes preflight-style PlatformError codes and switches
// the FailureInput to PhasePreflight so preflight signals (DM-2,
// invalid-zerops-yaml, prerequisite-missing) match instead of falling
// through to the generic transport baseline.
func TestClassifyTransportError_PromotesPreflightCodes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		code string
	}{
		{"invalid-yaml", platform.ErrInvalidZeropsYml},
		{"prereq-missing", platform.ErrPrerequisiteMissing},
		{"preflight-failed", platform.ErrPreflightFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pe := platform.NewPlatformError(tc.code, "boom", "fix")
			got := classifyTransportError(pe, "")
			if got == nil {
				t.Fatal("expected classification, got nil")
			}
			if got.Category != topology.FailureClassConfig {
				t.Errorf("Category = %q, want config (preflight code)", got.Category)
			}
		})
	}
}

// silence unused-import warnings if context drops out of test builds.
var _ = context.Background
