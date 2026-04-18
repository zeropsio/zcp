package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/workflow"
)

// --- handleStrategy ---

func TestHandleStrategy_ValidUpdate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		strategies map[string]string
		wantNext   string // substring expected in next hint
	}{
		{
			"push-dev",
			map[string]string{"appdev": workflow.StrategyPushDev},
			`workflow="develop"`,
		},
		{
			"push-git",
			map[string]string{"appdev": workflow.StrategyPushGit},
			`workflow="cicd"`,
		},
		{
			"manual",
			map[string]string{"appdev": workflow.StrategyManual},
			"deploy directly",
		},
		{
			"multiple services",
			map[string]string{"appdev": workflow.StrategyPushDev, "apidev": workflow.StrategyPushDev},
			`workflow="develop"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()

			// Pre-create metas so handleStrategy can read them.
			for hostname := range tt.strategies {
				if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
					Hostname:       hostname,
					BootstrappedAt: "2026-04-05",
				}); err != nil {
					t.Fatalf("write meta %s: %v", hostname, err)
				}
			}

			input := WorkflowInput{Strategies: tt.strategies}
			result, _, err := handleStrategy(nil, input, dir)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.IsError {
				t.Fatalf("unexpected tool error: %s", resultText(t, result))
			}

			// Parse response JSON.
			var resp map[string]string
			text := resultText(t, result)
			if err := json.Unmarshal([]byte(text), &resp); err != nil {
				t.Fatalf("unmarshal response: %v (text: %s)", err, text)
			}
			if resp["status"] != "updated" {
				t.Errorf("status: want updated, got %q", resp["status"])
			}
			if !strings.Contains(resp["next"], tt.wantNext) {
				t.Errorf("next hint should contain %q, got %q", tt.wantNext, resp["next"])
			}

			// Verify meta was persisted with strategy + confirmed flag.
			for hostname, strategy := range tt.strategies {
				meta, readErr := workflow.ReadServiceMeta(dir, hostname)
				if readErr != nil {
					t.Fatalf("read meta %s: %v", hostname, readErr)
				}
				if meta.DeployStrategy != strategy {
					t.Errorf("%s DeployStrategy: want %q, got %q", hostname, strategy, meta.DeployStrategy)
				}
				if !meta.StrategyConfirmed {
					t.Errorf("%s StrategyConfirmed: want true", hostname)
				}
			}
		})
	}
}

func TestHandleStrategy_InvalidStrategy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	input := WorkflowInput{Strategies: map[string]string{"appdev": "invalid-strategy"}}
	result, _, err := handleStrategy(nil, input, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for invalid strategy")
	}
	text := resultText(t, result)
	if !strings.Contains(text, "Invalid strategy") {
		t.Errorf("error should mention 'Invalid strategy', got: %s", text)
	}
}

func TestHandleStrategy_EmptyStrategies(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	input := WorkflowInput{Strategies: map[string]string{}}
	result, _, err := handleStrategy(nil, input, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for empty strategies")
	}
}

// F3 regression: strategy action must NOT auto-create a ServiceMeta for a hostname
// that ZCP never bootstrapped. An orphan meta with empty Mode/BootstrappedAt poisons
// every downstream consumer (router, briefing, hostname locks).
func TestHandleStrategy_UnknownHostname_RefusesToCreateOrphan(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	input := WorkflowInput{Strategies: map[string]string{"newservice": workflow.StrategyPushGit}}
	result, _, err := handleStrategy(nil, input, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error for unknown hostname, got success: %s", resultText(t, result))
	}
	if meta, _ := workflow.ReadServiceMeta(dir, "newservice"); meta != nil {
		t.Error("handleStrategy must NOT create ServiceMeta for unknown hostname")
	}
}

// Incomplete meta (no BootstrappedAt — bootstrap was interrupted) must also be rejected.
// Only completed bootstraps can have their strategy set.
func TestHandleStrategy_IncompleteMeta_Refused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: workflow.PlanModeDev, BootstrapSession: "sess1",
		// no BootstrappedAt -> incomplete
	}); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	input := WorkflowInput{Strategies: map[string]string{"appdev": workflow.StrategyPushGit}}
	result, _, err := handleStrategy(nil, input, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error for incomplete meta, got success: %s", resultText(t, result))
	}
}

func TestHandleStrategy_GuidanceExtracted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:       "appdev",
		BootstrappedAt: "2026-04-05",
	}); err != nil {
		t.Fatalf("write meta: %v", err)
	}

	input := WorkflowInput{Strategies: map[string]string{"appdev": workflow.StrategyPushGit}}
	result, _, err := handleStrategy(nil, input, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(t, result))
	}

	var resp map[string]string
	text := resultText(t, result)
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["guidance"] == "" {
		t.Error("expected non-empty guidance for push-git strategy")
	}
}

// --- buildStrategyGuidance ---

func TestBuildStrategyGuidance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		strategies   map[string]string
		wantContains string
		wantNonEmpty bool
	}{
		{"push-dev", map[string]string{"a": workflow.StrategyPushDev}, "Push-Dev", true},
		{"push-git", map[string]string{"a": workflow.StrategyPushGit}, "Git", true},
		{"manual", map[string]string{"a": workflow.StrategyManual}, "Manual", true},
		{"duplicate deduplicates", map[string]string{"a": workflow.StrategyPushDev, "b": workflow.StrategyPushDev}, "Push-Dev", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			guidance := buildStrategyGuidance(tt.strategies)
			if tt.wantNonEmpty && guidance == "" {
				t.Fatal("expected non-empty guidance")
			}
			if tt.wantContains != "" && !strings.Contains(guidance, tt.wantContains) {
				t.Errorf("should contain %q, got: %.200s", tt.wantContains, guidance)
			}
		})
	}
}

func TestBuildStrategyGuidance_DuplicateOnlyOnce(t *testing.T) {
	t.Parallel()
	guidance := buildStrategyGuidance(map[string]string{
		"appdev": workflow.StrategyPushDev,
		"apidev": workflow.StrategyPushDev,
	})
	// Count separator — multiple sections would have "---" between them.
	if strings.Contains(guidance, "---") {
		t.Error("duplicate strategy should produce only one section, not multiple separated by ---")
	}
}

// --- allStrategiesAre ---

func TestAllStrategiesAre(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		strategies map[string]string
		target     string
		want       bool
	}{
		{"all same", map[string]string{"a": "push-dev", "b": "push-dev"}, "push-dev", true},
		{"mixed", map[string]string{"a": "push-dev", "b": "push-git"}, "push-dev", false},
		{"empty map", map[string]string{}, "push-dev", false},
		{"nil map", nil, "push-dev", false},
		{"single match", map[string]string{"a": "manual"}, "manual", true},
		{"single no match", map[string]string{"a": "manual"}, "push-dev", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := allStrategiesAre(tt.strategies, tt.target)
			if got != tt.want {
				t.Errorf("allStrategiesAre(%v, %q) = %v, want %v", tt.strategies, tt.target, got, tt.want)
			}
		})
	}
}

// --- helpers ---

// resultText extracts text from the first content block of a CallToolResult.
func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result == nil || len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected *mcp.TextContent, got %T", result.Content[0])
	}
	return tc.Text
}
