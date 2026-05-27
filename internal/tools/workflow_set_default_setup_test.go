package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// Happy path: meta exists, no yaml on disk, set-default-setup writes
// PrimarySetupName + responds with the written value.
func TestHandleSetDefaultSetup_NoYAML_WritesMeta(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeDev,
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-04-01",
	}); err != nil {
		t.Fatal(err)
	}

	result, _, _ := handleSetDefaultSetup(
		context.Background(), platform.NewMock(), "p1",
		WorkflowInput{TargetService: "appdev", Setup: "custom-name"},
		stateDir,
	)
	if result.IsError {
		t.Fatalf("expected success; got: %s", getTextContent(t, result))
	}

	var resp setDefaultSetupResponse
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != "written" {
		t.Errorf("Status: got %q, want written", resp.Status)
	}
	if resp.PrimarySetupName != "custom-name" {
		t.Errorf("PrimarySetupName: got %q, want custom-name", resp.PrimarySetupName)
	}

	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta.PrimarySetupName != "custom-name" {
		t.Errorf("on-disk PrimarySetupName: got %q, want custom-name", meta.PrimarySetupName)
	}
}

// Pair shape: explicit StageSetup writes StageSetupName too.
func TestHandleSetDefaultSetup_PairWritesBothFields(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:      "appdev",
		Mode:          topology.PlanModeStandard,
		StageHostname: "appstage",
	}); err != nil {
		t.Fatal(err)
	}

	result, _, _ := handleSetDefaultSetup(
		context.Background(), platform.NewMock(), "p1",
		WorkflowInput{
			TargetService: "appdev",
			Setup:         "dev",
			StageSetup:    "prod",
		},
		stateDir,
	)
	if result.IsError {
		t.Fatalf("expected success; got: %s", getTextContent(t, result))
	}
	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta.PrimarySetupName != "dev" {
		t.Errorf("PrimarySetupName: got %q", meta.PrimarySetupName)
	}
	if meta.StageSetupName != "prod" {
		t.Errorf("StageSetupName: got %q", meta.StageSetupName)
	}
}

// Singleton meta (no StageHostname): StageSetup is silently ignored,
// no extra field written.
func TestHandleSetDefaultSetup_SingletonIgnoresStageSetup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeDev,
	}); err != nil {
		t.Fatal(err)
	}

	result, _, _ := handleSetDefaultSetup(
		context.Background(), platform.NewMock(), "p1",
		WorkflowInput{TargetService: "appdev", Setup: "dev", StageSetup: "prod"},
		stateDir,
	)
	if result.IsError {
		t.Fatalf("expected success; got: %s", getTextContent(t, result))
	}
	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta.StageSetupName != "" {
		t.Errorf("StageSetupName must stay empty for singleton; got %q", meta.StageSetupName)
	}
}

// Validation: when yaml is reachable + setup not in it, return
// requiresSetupInput with availableSetups populated.
func TestHandleSetDefaultSetup_SetupNotInYAML_ReturnsRequiresSetupInput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	scaffoldServiceYaml(t, dir, "appdev", `zerops:
  - setup: dev
  - setup: prod
`)
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeDev,
	}); err != nil {
		t.Fatal(err)
	}

	result, _, _ := handleSetDefaultSetup(
		context.Background(), platform.NewMock(), "p1",
		WorkflowInput{TargetService: "appdev", Setup: "nonexistent"},
		stateDir,
	)
	if result.IsError {
		t.Fatalf("expected success-shaped result (blocker in body); got: %s",
			getTextContent(t, result))
	}
	var resp RequiresSetupInputResponse
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Reason != "requiresSetupInput" {
		t.Errorf("Reason: got %q, want requiresSetupInput", resp.Reason)
	}
	if len(resp.AvailableSetups) != 2 {
		t.Errorf("AvailableSetups: got %v, want [dev prod]", resp.AvailableSetups)
	}

	// Meta MUST NOT have been mutated.
	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta.PrimarySetupName != "" {
		t.Errorf("PrimarySetupName must stay empty after invalid set; got %q", meta.PrimarySetupName)
	}
}

// Missing meta surfaces ErrServiceNotFound.
func TestHandleSetDefaultSetup_NoMeta_Refuses(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	result, _, _ := handleSetDefaultSetup(
		context.Background(), platform.NewMock(), "p1",
		WorkflowInput{TargetService: "appdev", Setup: "dev"},
		stateDir,
	)
	if !result.IsError {
		t.Fatal("expected error result for missing meta")
	}
}

// Missing required inputs: targetService / setup.
func TestHandleSetDefaultSetup_RequiredInputs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		input WorkflowInput
	}{
		{"missing targetService", WorkflowInput{Setup: "dev"}},
		{"missing setup", WorkflowInput{TargetService: "appdev"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, _, _ := handleSetDefaultSetup(
				context.Background(), platform.NewMock(), "p1", tt.input, stateDir,
			)
			if !result.IsError {
				t.Errorf("expected error for %q; got: %s", tt.name, getTextContent(t, result))
			}
		})
	}
}

// staleMetaSetup wire shape — pins the 3-recovery-options invariant.
func TestStaleMetaSetupShape(t *testing.T) {
	t.Parallel()
	resp := buildStaleMetaSetupResponse("appdev", "dev", []string{"development", "production"}, "development")

	if resp.Status != "blocked" {
		t.Errorf("Status: got %q", resp.Status)
	}
	if resp.Reason != "staleMetaSetup" {
		t.Errorf("Reason: got %q", resp.Reason)
	}
	if resp.Service != "appdev" || resp.MetaSetup != "dev" {
		t.Errorf("Service/MetaSetup: got %q/%q", resp.Service, resp.MetaSetup)
	}
	if len(resp.LiveYamlSetups) != 2 {
		t.Errorf("LiveYamlSetups: got %v", resp.LiveYamlSetups)
	}

	// Plan §staleMetaSetup: ALL 3 recovery options always present.
	if len(resp.Recovery.Options) != 3 {
		t.Fatalf("Recovery.Options: got %d, want 3 (deterministic shape)", len(resp.Recovery.Options))
	}
	wantLabels := []string{
		"Restore yaml block name to match meta",
		"Update meta to match yaml (permanent)",
		"One-shot deploy with override",
	}
	for i, want := range wantLabels {
		if resp.Recovery.Options[i].Label != want {
			t.Errorf("Recovery.Options[%d].Label: got %q, want %q",
				i, resp.Recovery.Options[i].Label, want)
		}
	}
	// The set-default-setup recovery references the same tool/action
	// chain the requiresSetupInput shape uses, so agents handle them
	// with identical dispatch logic.
	if resp.Recovery.Options[1].Tool != "zerops_workflow" {
		t.Errorf("update-meta Tool: got %q", resp.Recovery.Options[1].Tool)
	}
	if resp.Recovery.Options[1].Action != "set-default-setup" {
		t.Errorf("update-meta Action: got %q", resp.Recovery.Options[1].Action)
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"status", "reason", "service", "metaSetup", "liveYamlSetups", "recovery"} {
		if _, ok := parsed[k]; !ok {
			t.Errorf("wire JSON missing field %q (raw=%s)", k, raw)
		}
	}
}

// Suggested-new-setup empty → placeholder lands in Args so agent fills it.
func TestBuildStaleMetaSetupResponse_PlaceholderWhenNoSuggestion(t *testing.T) {
	t.Parallel()
	resp := buildStaleMetaSetupResponse("appdev", "dev", []string{"alpha", "beta"}, "")
	args := resp.Recovery.Options[1].Args
	if got, _ := args["setup"].(string); got != "<choose-from-liveYamlSetups>" {
		t.Errorf("setup placeholder: got %v", args["setup"])
	}
}
