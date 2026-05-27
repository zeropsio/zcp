package tools

import (
	"encoding/json"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

// TestRequiresSetupInputShape pins the wire shape (named struct, not
// inline map) across the 3 canonical emit cases per plan:
//  1. Total cascade miss (no yaml seen anywhere)
//  2. Multi-block yaml ambiguity (availableSetups populated)
//  3. Singleton hostname (no stageSetup arg in recovery)
//
// Failure here = downstream agent prompts (set-default-setup) break.
func TestRequiresSetupInputShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		service       string
		blocker       *workflow.ErrRequiresSetupInput
		wantAvailable []string
		wantReason    string
	}{
		{
			name:    "total miss — no yaml seen",
			service: "apistage",
			blocker: &workflow.ErrRequiresSetupInput{
				Service:        "apistage",
				TargetHostname: "apistage",
				Reason:         "no canonical setup-name in local meta or platform sources; no local yaml supplied",
			},
			wantReason: "no canonical setup-name in local meta or platform sources; no local yaml supplied",
		},
		{
			name:    "multi-block ambiguity",
			service: "frontend",
			blocker: &workflow.ErrRequiresSetupInput{
				Service:         "frontend",
				TargetHostname:  "frontend",
				AvailableSetups: []string{"web", "api"},
				Reason:          "no setup matched hostname / suffix conventions and yaml has multiple blocks",
			},
			wantAvailable: []string{"web", "api"},
			wantReason:    "no setup matched hostname / suffix conventions and yaml has multiple blocks",
		},
		{
			name:    "archive yaml unreadable",
			service: "appdev",
			blocker: &workflow.ErrRequiresSetupInput{
				Service:         "appdev",
				TargetHostname:  "appdev",
				AvailableSetups: []string{"dev", "prod"},
				Reason:          "archive zerops.yaml has multiple setups; no hostname/suffix match",
			},
			wantAvailable: []string{"dev", "prod"},
			wantReason:    "archive zerops.yaml has multiple setups; no hostname/suffix match",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildRequiresSetupInputResponse(tt.service, tt.blocker)

			if got.Status != "blocked" {
				t.Errorf("Status: got %q, want blocked", got.Status)
			}
			if got.Reason != "requiresSetupInput" {
				t.Errorf("Reason: got %q, want requiresSetupInput", got.Reason)
			}
			if got.Service != tt.service {
				t.Errorf("Service: got %q, want %q", got.Service, tt.service)
			}
			if got.TargetHostname != tt.blocker.TargetHostname {
				t.Errorf("TargetHostname: got %q, want %q",
					got.TargetHostname, tt.blocker.TargetHostname)
			}
			if got.AmbiguityReason != tt.wantReason {
				t.Errorf("AmbiguityReason: got %q, want %q",
					got.AmbiguityReason, tt.wantReason)
			}
			if !slicesEqual(got.AvailableSetups, tt.wantAvailable) {
				t.Errorf("AvailableSetups: got %v, want %v",
					got.AvailableSetups, tt.wantAvailable)
			}
			// Recovery contract: existing tool + action; placeholder Setup
			// arg so agent knows it must pick from availableSetups.
			if got.Recovery.Tool != "zerops_workflow" {
				t.Errorf("Recovery.Tool: got %q, want zerops_workflow", got.Recovery.Tool)
			}
			if got.Recovery.Action != "set-default-setup" {
				t.Errorf("Recovery.Action: got %q, want set-default-setup", got.Recovery.Action)
			}
			if got.Recovery.Args.Service != tt.service {
				t.Errorf("Recovery.Args.Service: got %q, want %q",
					got.Recovery.Args.Service, tt.service)
			}
			if got.Recovery.Args.Setup != "<choose-from-availableSetups>" {
				t.Errorf("Recovery.Args.Setup: got %q, want placeholder", got.Recovery.Args.Setup)
			}
			if got.Recovery.Args.StageSetup != "" {
				t.Errorf("Recovery.Args.StageSetup must be empty for non-pair input; got %q",
					got.Recovery.Args.StageSetup)
			}

			// Round-trip JSON to lock the wire field names.
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			var parsed map[string]any
			if err := json.Unmarshal(raw, &parsed); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			for _, k := range []string{"status", "reason", "service", "targetHostname", "ambiguityReason", "recovery"} {
				if _, ok := parsed[k]; !ok {
					t.Errorf("wire JSON missing required field %q (raw=%s)", k, raw)
				}
			}
			rec, _ := parsed["recovery"].(map[string]any)
			if rec == nil {
				t.Fatal("recovery field is not an object")
			}
			for _, k := range []string{"tool", "action", "args"} {
				if _, ok := rec[k]; !ok {
					t.Errorf("recovery wire JSON missing %q (raw=%s)", k, raw)
				}
			}
		})
	}
}

// Nil blocker is a programming-error guard, not a happy path — the
// helper returns the zero value rather than panicking so callers can
// audit before serializing.
func TestBuildRequiresSetupInputResponse_NilBlocker(t *testing.T) {
	t.Parallel()
	got := buildRequiresSetupInputResponse("svc", nil)
	if got.Status != "" || got.Reason != "" {
		t.Errorf("nil blocker should return zero value; got %+v", got)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
