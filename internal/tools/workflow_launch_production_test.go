package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
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
	client := newLaunchMockClient().WithProjectEnv([]platform.ProjectEnvVar{
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
	client := newLaunchMockClient().WithProjectEnv([]platform.ProjectEnvVar{
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
	client := newLaunchMockClient().WithProjectEnv([]platform.ProjectEnvVar{
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
	client := newLaunchMockClient().WithProjectEnv([]platform.ProjectEnvVar{
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

// TestHandleLaunchProduction_StageHalfTarget_NormalizedToDevHalf pins
// F13: stage-half input is accepted (no blocker) and normalized
// internally to the canonical dev-half meta key. Both halves of a
// standard pair share the same git source and setup blocks, so the
// distinction is presentational — stage is the validated headline,
// dev is the build key. Pre-F13 the handler rejected stage-half with
// a scope-stage-half-not-promotable blocker, forcing the agent to
// re-call with the dev-half. Karel pushback: stage is the natural
// promotion-source mental model; accept it.
func TestHandleLaunchProduction_StageHalfTarget_NormalizedToDevHalf(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stateDir := writeRuntimeMeta(t, &workflow.ServiceMeta{
		Hostname:       "appdev",
		StageHostname:  "appstage",
		Mode:           topology.ModeStandard,
		BootstrappedAt: "2026-05-01T00:00:00Z",
	})

	client := newLaunchMockClient().WithProjectEnv([]platform.ProjectEnvVar{
		{Key: "LOG_LEVEL", Content: "info"},
	})

	input := WorkflowInput{
		Workflow:              workflowLaunchProduction,
		ProductionProjectName: "myapp-prod",
		TargetService:         "appstage", // stage-half input
		EnvClassifications:    map[string]string{"LOG_LEVEL": "plain-config"},
	}
	result, _, err := handleLaunchProduction(ctx, "source-project-id", client, input, stateDir, runtime.Info{}, nil)
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))

	// Stage-half acceptance: advance past scope-prompt (the F13 fix).
	if resp.Status != "ready-to-launch" {
		t.Fatalf("status: got %q want ready-to-launch (stage-half should advance, not block)", resp.Status)
	}
	// No leftover stage-half rejection blocker.
	for _, b := range resp.Blockers {
		if b.ID == "scope-stage-half-not-promotable" {
			t.Errorf("stage-half blocker should be gone post-F13; got %+v", b)
		}
	}
}

// TestHandleLaunchProduction_DevHalfTarget_Accepted pins the partner
// case: dev-half input progresses normally (no false-positive blocker
// from the stage-half check).
func TestHandleLaunchProduction_DevHalfTarget_Accepted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stateDir := writeRuntimeMeta(t, &workflow.ServiceMeta{
		Hostname:       "appdev",
		StageHostname:  "appstage",
		Mode:           topology.ModeStandard,
		BootstrappedAt: "2026-05-01T00:00:00Z",
	})

	client := newLaunchMockClient().WithProjectEnv([]platform.ProjectEnvVar{
		{Key: "LOG_LEVEL", Content: "info"},
	})

	input := WorkflowInput{
		Workflow:              workflowLaunchProduction,
		ProductionProjectName: "myapp-prod",
		TargetService:         "appdev",
		EnvClassifications:    map[string]string{"LOG_LEVEL": "plain-config"},
	}
	result, _, err := handleLaunchProduction(ctx, "source-project-id", client, input, stateDir, runtime.Info{}, nil)
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))

	if resp.Status != "ready-to-launch" {
		t.Fatalf("status: got %q want ready-to-launch (dev-half input should pass)", resp.Status)
	}
	for _, b := range resp.Blockers {
		if b.ID == "scope-stage-half-not-promotable" {
			t.Errorf("dev-half target should NOT trigger stage-half blocker; got %+v", b)
		}
	}
}

// TestHandleLaunchProduction_ClassifyPrompt_HidesSystemEnvs pins the
// envclass integration at the handler boundary: project envs with
// Type=SYSTEM (zeropsSubdomain*, CDN URLs, envIsolation,
// sshIsolation) MUST NOT appear in classify-prompt rows. The classifier
// (Layer 3) Drops them upstream so the LLM never sees them — agent has
// no business classifying platform-managed values, and the target
// project regenerates equivalents on import. F19 coverage end-to-end.
func TestHandleLaunchProduction_ClassifyPrompt_HidesSystemEnvs(t *testing.T) {
	ctx := context.Background()
	client := newLaunchMockClient().WithProjectEnv([]platform.ProjectEnvVar{
		{Key: "APP_KEY", Content: "secret-value", Type: platform.ProjectEnvUser},
		{Key: "zeropsSubdomainHost", Content: "abc.zerops.app", Type: platform.ProjectEnvSystem},
		{Key: "envIsolation", Content: "project", Type: platform.ProjectEnvSystem, Editable: true},
		{Key: "staticCdnUrl", Content: "https://static.cdn", Type: platform.ProjectEnvSystem},
		{Key: "DB_HOST", Content: "${db_hostname}", Type: platform.ProjectEnvUser},
	})

	input := WorkflowInput{
		Workflow:              workflowLaunchProduction,
		ProductionProjectName: "myapp-prod",
		TargetService:         "app",
	}
	result, _, err := handleLaunchProduction(ctx, "source-project-id", client, input, t.TempDir(), runtime.Info{}, nil)
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	text := extractText(result)
	resp := decodeLaunchResp(t, []byte(text))

	if resp.Status != "classify-prompt" {
		t.Fatalf("status: got %q want classify-prompt\nbody:\n%s", resp.Status, text)
	}
	if len(resp.Classifications) != 2 {
		t.Fatalf("classifications rows: got %d want 2 (APP_KEY + DB_HOST only)\n%+v", len(resp.Classifications), resp.Classifications)
	}
	keysSeen := map[string]bool{}
	for _, row := range resp.Classifications {
		keysSeen[row.Key] = true
	}
	if !keysSeen["APP_KEY"] || !keysSeen["DB_HOST"] {
		t.Errorf("expected USER-scope keys in rows: %+v", keysSeen)
	}
	for _, banned := range []string{"zeropsSubdomainHost", "envIsolation", "staticCdnUrl"} {
		if keysSeen[banned] {
			t.Errorf("Type=SYSTEM env %q must not appear in classifications rows", banned)
		}
	}
	// Defense in depth: no env values leak.
	for _, val := range []string{"secret-value", "abc.zerops.app", "https://static.cdn"} {
		if strings.Contains(text, val) {
			t.Errorf("env value %q leaked into response", val)
		}
	}
}

// TestHandleLaunchProduction_AllSystemEnvs_NoPromptFires pins the
// no-loop guarantee: when every source env is Type=SYSTEM, the
// workflow advances past classify-prompt directly (envclass-Drop on
// all entries, no PromptUser-decision env remains).
func TestHandleLaunchProduction_AllSystemEnvs_NoPromptFires(t *testing.T) {
	ctx := context.Background()
	client := newLaunchMockClient().WithProjectEnv([]platform.ProjectEnvVar{
		{Key: "zeropsSubdomainHost", Content: "abc.zerops.app", Type: platform.ProjectEnvSystem},
		{Key: "storageCdnUrl", Content: "https://storage.cdn", Type: platform.ProjectEnvSystem},
	})

	input := WorkflowInput{
		Workflow:              workflowLaunchProduction,
		ProductionProjectName: "myapp-prod",
		TargetService:         "app",
	}
	result, _, err := handleLaunchProduction(ctx, "source-project-id", client, input, t.TempDir(), runtime.Info{}, nil)
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	if resp.Status != "ready-to-launch" {
		t.Errorf("status: got %q want ready-to-launch (all SYSTEM envs should not trigger prompt)", resp.Status)
	}
}
