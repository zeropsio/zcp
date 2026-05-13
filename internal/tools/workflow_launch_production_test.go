package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// sentinelLaunchKey is a value that, if it ever appears in a response,
// reveals a P-LP-1 violation. Used by every test that exercises the
// handler with a real launch-key value.
const sentinelLaunchKey = "ZCP-LAUNCH-KEY-SENTINEL-DO-NOT-LEAK"

func newLaunchMockClient() *platform.Mock {
	return platform.NewMock()
}

func decodeLaunchResp(t *testing.T, b []byte) launchProductionResponse {
	t.Helper()
	var r launchProductionResponse
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("decode response: %v\nbody:\n%s", err, string(b))
	}
	return r
}

// TestHandleLaunchProduction_ScopePrompt_MissingProductionProjectName
// asserts the scope-prompt status fires when the required scope field
// is absent.
func TestHandleLaunchProduction_ScopePrompt_MissingProductionProjectName(t *testing.T) {
	ctx := context.Background()
	client := newLaunchMockClient()

	input := WorkflowInput{
		Workflow: workflowLaunchProduction,
		// ProductionProjectName intentionally empty
	}

	result, _, err := handleLaunchProduction(ctx, "source-project-id", client, input, "/tmp", runtime.Info{}, nil)
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	text := extractText(result)
	resp := decodeLaunchResp(t, []byte(text))

	if resp.Status != "scope-prompt" {
		t.Errorf("status: got %q want scope-prompt", resp.Status)
	}
	if resp.Workflow != workflowLaunchProduction {
		t.Errorf("workflow: got %q want %q", resp.Workflow, workflowLaunchProduction)
	}
	if len(resp.Blockers) == 0 {
		t.Error("expected at least one blocker")
	}
	if resp.Blockers[0].Category != "scope" {
		t.Errorf("blocker category: got %q want scope", resp.Blockers[0].Category)
	}
}

// TestHandleLaunchProduction_ClassifyPrompt fires when project envs exist
// and classifications are incomplete.
func TestHandleLaunchProduction_ClassifyPrompt(t *testing.T) {
	ctx := context.Background()
	client := newLaunchMockClient().WithProjectEnv([]platform.EnvVar{
		{ID: "e1", Key: "LOG_LEVEL", Content: "info"},
		{ID: "e2", Key: "STRIPE_SECRET", Content: "sk_live_xxx"},
		{ID: "e3", Key: "DB_HOST", Content: "${db_hostname}"},
	})

	input := WorkflowInput{
		Workflow:              workflowLaunchProduction,
		ProductionProjectName: "myapp-prod",
		TargetService:         "app",
		// EnvClassifications empty — every env unclassified
	}

	result, _, err := handleLaunchProduction(ctx, "source-project-id", client, input, "/tmp", runtime.Info{}, nil)
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	text := extractText(result)
	resp := decodeLaunchResp(t, []byte(text))

	if resp.Status != "classify-prompt" {
		t.Fatalf("status: got %q want classify-prompt\nresponse: %s", resp.Status, text)
	}
	if len(resp.Classifications) != 3 {
		t.Errorf("expected 3 classification rows, got %d", len(resp.Classifications))
	}
	// Verify no env Value leaks into the rows
	for _, row := range resp.Classifications {
		if strings.Contains(strings.ToLower(text), "sk_live_xxx") {
			t.Errorf("classify-prompt response leaks raw env value sk_live_xxx; row=%+v", row)
		}
	}
}

// TestHandleLaunchProduction_ClassifyPrompt_PartialClassifications still
// fires the prompt when only some envs are classified.
func TestHandleLaunchProduction_ClassifyPrompt_PartialClassifications(t *testing.T) {
	ctx := context.Background()
	client := newLaunchMockClient().WithProjectEnv([]platform.EnvVar{
		{Key: "LOG_LEVEL", Content: "info"},
		{Key: "STRIPE_SECRET", Content: "sk_test_xxx"},
	})

	input := WorkflowInput{
		Workflow:              workflowLaunchProduction,
		ProductionProjectName: "myapp-prod",
		TargetService:         "app",
		EnvClassifications:    map[string]string{"LOG_LEVEL": "plain-config"}, // only one of two
	}

	result, _, err := handleLaunchProduction(ctx, "source-project-id", client, input, "/tmp", runtime.Info{}, nil)
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))

	if resp.Status != "classify-prompt" {
		t.Fatalf("status: got %q want classify-prompt", resp.Status)
	}
}

// TestHandleLaunchProduction_ReadyToLaunch_NoLaunchKey fires when scope
// and classifications are complete but launchKey isn't supplied.
func TestHandleLaunchProduction_ReadyToLaunch_NoLaunchKey(t *testing.T) {
	ctx := context.Background()
	client := newLaunchMockClient().WithProjectEnv([]platform.EnvVar{
		{Key: "LOG_LEVEL", Content: "info"},
	})

	input := WorkflowInput{
		Workflow:              workflowLaunchProduction,
		ProductionProjectName: "myapp-prod",
		TargetService:         "app",
		Region:                "eu-central",
		EnvClassifications:    map[string]string{"LOG_LEVEL": "plain-config"},
	}

	result, _, err := handleLaunchProduction(ctx, "source-project-id", client, input, "/tmp", runtime.Info{}, nil)
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))

	if resp.Status != "ready-to-launch" {
		t.Fatalf("status: got %q want ready-to-launch", resp.Status)
	}
	if resp.Inputs == nil || resp.Inputs.ProductionProjectName != "myapp-prod" {
		t.Errorf("inputs echo missing or wrong: %+v", resp.Inputs)
	}
}

// TestHandleLaunchProduction_NoSourceEnvs_AdvancesToReadyToLaunch verifies
// that an empty source-envs list short-circuits classify-prompt and
// advances directly to ready-to-launch.
func TestHandleLaunchProduction_NoSourceEnvs_AdvancesToReadyToLaunch(t *testing.T) {
	ctx := context.Background()
	client := newLaunchMockClient() // no envs

	input := WorkflowInput{
		Workflow:              workflowLaunchProduction,
		ProductionProjectName: "myapp-prod",
		TargetService:         "app",
	}

	result, _, err := handleLaunchProduction(ctx, "source-project-id", client, input, "/tmp", runtime.Info{}, nil)
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))

	if resp.Status != "ready-to-launch" {
		t.Fatalf("status: got %q want ready-to-launch", resp.Status)
	}
}

// TestHandleLaunchProduction_LaunchKeyNeverInResponse pins P-LP-1: the
// LaunchKey value never appears anywhere in the JSON-serialized
// response, no matter which branch fires.
func TestHandleLaunchProduction_LaunchKeyNeverInResponse(t *testing.T) {
	ctx := context.Background()
	client := newLaunchMockClient().WithProjectEnv([]platform.EnvVar{
		{Key: "LOG_LEVEL", Content: "info"},
	})

	scenarios := []struct {
		name  string
		input WorkflowInput
	}{
		{
			name: "scope-prompt with key",
			input: WorkflowInput{
				Workflow:  workflowLaunchProduction,
				LaunchKey: sentinelLaunchKey,
			},
		},
		{
			name: "classify-prompt with key",
			input: WorkflowInput{
				Workflow:              workflowLaunchProduction,
				ProductionProjectName: "myapp-prod",
				TargetService:         "app",
				LaunchKey:             sentinelLaunchKey,
			},
		},
		{
			name: "ready-to-launch with key (treats as mutation-deferred placeholder)",
			input: WorkflowInput{
				Workflow:              workflowLaunchProduction,
				ProductionProjectName: "myapp-prod",
				TargetService:         "app",
				EnvClassifications:    map[string]string{"LOG_LEVEL": "plain-config"},
				LaunchKey:             sentinelLaunchKey,
			},
		},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			result, _, err := handleLaunchProduction(ctx, "source-project-id", client, sc.input, "/tmp", runtime.Info{}, nil)
			if err != nil {
				t.Fatalf("handleLaunchProduction: %v", err)
			}
			text := extractText(result)
			if strings.Contains(text, sentinelLaunchKey) {
				t.Fatalf("P-LP-1 violation: launchKey %q leaked into response\nbody:\n%s", sentinelLaunchKey, text)
			}
		})
	}
}

// TestHandleLaunchProduction_NilClientReturnsError pins the nil-client guard.
func TestHandleLaunchProduction_NilClientReturnsError(t *testing.T) {
	ctx := context.Background()
	result, _, err := handleLaunchProduction(ctx, "source-project-id", nil, WorkflowInput{}, "/tmp", runtime.Info{}, nil)
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	// Error response is delivered via the result itself (IsError true).
	if result == nil || !result.IsError {
		t.Errorf("expected IsError=true for nil client, got %v", result)
	}
}

// TestHandleLaunchProduction_EmptyProjectIDReturnsError pins the
// empty-projectID guard.
func TestHandleLaunchProduction_EmptyProjectIDReturnsError(t *testing.T) {
	ctx := context.Background()
	result, _, err := handleLaunchProduction(ctx, "", newLaunchMockClient(), WorkflowInput{}, "/tmp", runtime.Info{}, nil)
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	if result == nil || !result.IsError {
		t.Errorf("expected IsError=true for empty projectID, got %v", result)
	}
}

// TestWorkflowInputLaunchKey_JSONTagOmits verifies the field tag is `-`
// so encoding/json never marshals LaunchKey into any output.
// Compile-time-adjacent guarantee, but explicit test surfaces drift.
func TestWorkflowInputLaunchKey_JSONTagOmits(t *testing.T) {
	wi := WorkflowInput{LaunchKey: sentinelLaunchKey}
	b, err := json.Marshal(wi)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), sentinelLaunchKey) {
		t.Fatalf("LaunchKey leaked into WorkflowInput JSON marshal: %s", string(b))
	}
	// Also confirm it doesn't show under any of the JSON tags we set
	if strings.Contains(string(b), `"launchKey"`) {
		t.Fatalf("WorkflowInput emitted a launchKey JSON field; should be omitted entirely: %s", string(b))
	}
}
