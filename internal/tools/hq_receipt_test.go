package tools

import (
	"reflect"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestChangeReceipt_OnlyForAPairOfSeveralStages(t *testing.T) {
	t.Parallel()
	ws := &workflow.WorkSession{Deploys: map[string][]workflow.DeployAttempt{
		"appstage": {{AttemptedAt: "2026-10-14T21:05:00Z", SucceededAt: "2026-10-14T21:06:00Z", SHA: "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"}},
	}}
	for _, stages := range [][]receiptStage{nil, {{Hostname: "appstage", Setup: "app"}}} {
		if got := changeReceipt(stages, ws, time.Now()); got != nil {
			t.Errorf("changeReceipt(%v) = %+v, want none", stages, got)
		}
	}
}

func TestChangeReceipt_EachPartsPreviewAsItsLastDeployLeftIt(t *testing.T) {
	t.Parallel()
	const commit = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	ws := &workflow.WorkSession{
		Deploys: map[string][]workflow.DeployAttempt{
			// A failed deploy before the one that worked does not count.
			"webstage": {
				{AttemptedAt: "2026-10-14T21:01:00Z", Error: "build failed"},
				{AttemptedAt: "2026-10-14T21:05:00Z", SucceededAt: "2026-10-14T21:06:00Z", SHA: commit},
			},
			// A deploy of uncommitted work names no commit.
			"apistage": {{AttemptedAt: "2026-10-14T21:06:30Z", SucceededAt: "2026-10-14T21:07:00Z", SHA: commit, Dirty: true}},
			"workerstage": {
				{AttemptedAt: "2026-10-14T21:02:00Z", SucceededAt: "2026-10-14T21:03:00Z", SHA: commit},
				{AttemptedAt: "2026-10-14T21:08:00Z", Error: "deploy status BUILD_FAILED"},
			},
		},
		Verifies: map[string][]workflow.VerifyAttempt{
			"webstage": {{AttemptedAt: "2026-10-14T21:06:10Z", Passed: true}},
			// A verify that passed before the last deploy says nothing of it.
			"apistage":    {{AttemptedAt: "2026-10-14T21:04:00Z", Passed: true}},
			"workerstage": {{AttemptedAt: "2026-10-14T21:03:30Z", Passed: true}},
		},
	}
	stages := []receiptStage{
		{Hostname: "webstage", Setup: "web"},
		{Hostname: "apistage", Setup: "api"},
		{Hostname: "workerstage", Setup: "worker"},
		{Hostname: "cronstage", Setup: "cron"},
	}
	got := changeReceipt(stages, ws, time.Date(2026, 10, 14, 21, 10, 0, 0, time.UTC))
	want := &hq.ChangeReceipt{At: "2026-10-14T21:10:00Z", Parts: []hq.ChangeReceiptPart{
		{Setup: "web", PreviewHostname: "webstage", Deploy: "ok", DeployedAt: "2026-10-14T21:06:00Z", WorkspaceClean: true, DeployedCommit: commit, Verified: true},
		{Setup: "api", PreviewHostname: "apistage", Deploy: "ok", DeployedAt: "2026-10-14T21:07:00Z"},
		{Setup: "worker", PreviewHostname: "workerstage", Deploy: "failed", DeployedAt: "2026-10-14T21:08:00Z", WorkspaceClean: true},
		{Setup: "cron", PreviewHostname: "cronstage", Deploy: "none"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("changeReceipt =\n%+v\nwant\n%+v", got, want)
	}
}
