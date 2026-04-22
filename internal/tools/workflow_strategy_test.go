package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// --- handleStrategy: update mode ---

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
			`strategy="git-push"`,
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
			result, _, err := handleStrategy(input, dir, runtime.Info{})
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
				if meta.DeployStrategy != workflow.DeployStrategy(strategy) {
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
	result, _, err := handleStrategy(input, dir, runtime.Info{})
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

// TestHandleStrategy_EmptyStrategies_ListingMode verifies that an empty
// strategies map switches handleStrategy into listing mode — returns current
// strategy per service + the set of options. This is the central entry point
// for "what can I configure and what is it now?" without mutating anything.
func TestHandleStrategy_EmptyStrategies_ListingMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Two complete bootstraps, one incomplete (skipped by listing).
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:       "appdev",
		BootstrappedAt: "2026-04-05",
		DeployStrategy: workflow.StrategyPushDev,
	}); err != nil {
		t.Fatalf("write meta appdev: %v", err)
	}
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:       "apidev",
		BootstrappedAt: "2026-04-05",
		// no strategy set → unset
	}); err != nil {
		t.Fatalf("write meta apidev: %v", err)
	}
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "incomplete",
		BootstrapSession: "sess1",
		// no BootstrappedAt → not complete, must not appear in listing
	}); err != nil {
		t.Fatalf("write meta incomplete: %v", err)
	}

	input := WorkflowInput{} // no strategies
	result, _, err := handleStrategy(input, dir, runtime.Info{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(t, result))
	}

	text := resultText(t, result)
	var resp struct {
		Status   string `json:"status"`
		Services []struct {
			Hostname string   `json:"hostname"`
			Current  string   `json:"current"`
			Options  []string `json:"options"`
		} `json:"services"`
		Next string `json:"next"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal listing response: %v (text: %s)", err, text)
	}

	if resp.Status != "list" {
		t.Errorf("status: want list, got %q", resp.Status)
	}
	if len(resp.Services) != 2 {
		t.Fatalf("expected 2 complete services (appdev, apidev), got %d: %+v", len(resp.Services), resp.Services)
	}
	byHost := map[string]string{}
	for _, s := range resp.Services {
		byHost[s.Hostname] = s.Current
		// Every entry must list all three strategies as options.
		if len(s.Options) != 3 {
			t.Errorf("%s: expected 3 options, got %d", s.Hostname, len(s.Options))
		}
	}
	if byHost["appdev"] != workflow.StrategyPushDev {
		t.Errorf("appdev current: want %q, got %q", workflow.StrategyPushDev, byHost["appdev"])
	}
	if byHost["apidev"] != string(workflow.StrategyUnset) {
		t.Errorf("apidev current: want %q, got %q", workflow.StrategyUnset, byHost["apidev"])
	}
	if _, seen := byHost["incomplete"]; seen {
		t.Error("listing must skip incomplete metas")
	}
	if !strings.Contains(resp.Next, "push-git") {
		t.Errorf("next hint should mention push-git setup: %s", resp.Next)
	}
}

// F3 regression: strategy action must NOT auto-create a ServiceMeta for a hostname
// that ZCP never bootstrapped. An orphan meta with empty Mode/BootstrappedAt poisons
// every downstream consumer (router, briefing, hostname locks).
func TestHandleStrategy_UnknownHostname_RefusesToCreateOrphan(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	input := WorkflowInput{Strategies: map[string]string{"newservice": workflow.StrategyPushGit}}
	result, _, err := handleStrategy(input, dir, runtime.Info{})
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
	result, _, err := handleStrategy(input, dir, runtime.Info{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error for incomplete meta, got success: %s", resultText(t, result))
	}
}

// TestHandleStrategy_PushGit_SynthSetup verifies the phase-A.6 atom split:
// setting push-git without a trigger returns the intro+push chain (no
// trigger-specific content yet); setting it with a trigger returns the
// full chain including the chosen trigger's atom.
func TestHandleStrategy_PushGit_SynthSetup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		trigger    string
		wantSubstr []string
		dontWant   []string
	}{
		{
			name:    "no trigger → intro + push (user must pick trigger next)",
			trigger: "",
			wantSubstr: []string{
				"webhook",       // intro lists options
				"actions",       // intro lists options
				"GIT_TOKEN",     // container push atom
				"zerops_deploy", // push action
			},
			dontWant: []string{
				"ZEROPS_TOKEN", // trigger-actions atom must NOT fire without explicit trigger
			},
		},
		{
			name:    "trigger=actions → intro + push + actions",
			trigger: "actions",
			wantSubstr: []string{
				"GIT_TOKEN",     // push atom
				"ZEROPS_TOKEN",  // actions trigger atom
				"zcli push",     // actions workflow YAML
				"ZEROPS_TOKEN",  // secret setup
				"zerops_deploy", // push action
			},
		},
		{
			name:    "trigger=webhook → intro + push + webhook",
			trigger: "webhook",
			wantSubstr: []string{
				"GIT_TOKEN",                // push atom
				"dashboard",                // webhook walkthrough
				"Trigger automatic builds", // webhook dashboard step
				"zerops_deploy",            // push action
			},
			dontWant: []string{
				"ZEROPS_TOKEN", // actions-only
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
				Hostname:       "appdev",
				Mode:           workflow.PlanModeDev,
				Environment:    string(workflow.EnvContainer),
				BootstrappedAt: "2026-04-05",
			}); err != nil {
				t.Fatalf("write meta: %v", err)
			}

			input := WorkflowInput{
				Strategies: map[string]string{"appdev": workflow.StrategyPushGit},
				Trigger:    tt.trigger,
			}
			result, _, err := handleStrategy(input, dir, runtime.Info{InContainer: true})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.IsError {
				t.Fatalf("unexpected tool error: %s", resultText(t, result))
			}

			var resp map[string]string
			if err := json.Unmarshal([]byte(resultText(t, result)), &resp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			g := resp["guidance"]
			if g == "" {
				t.Fatal("expected non-empty guidance for push-git setup")
			}
			for _, want := range tt.wantSubstr {
				if !strings.Contains(g, want) {
					t.Errorf("guidance must contain %q, not found. Got (first 400): %.400s", want, g)
				}
			}
			for _, nope := range tt.dontWant {
				if strings.Contains(g, nope) {
					t.Errorf("guidance must NOT contain %q at this trigger scope. Got:\n%s", nope, g)
				}
			}
			if strings.Contains(g, `workflow="cicd"`) {
				t.Errorf("guidance must not reference retired workflow=cicd: %.400s", g)
			}
		})
	}
}

// --- buildStrategyGuidance: used for non-push-git ---

func TestBuildStrategyGuidance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		strategies   map[string]string
		wantContains string
		wantNonEmpty bool
	}{
		{"push-dev", map[string]string{"a": workflow.StrategyPushDev}, "Push-Dev", true},
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
	once := buildStrategyGuidance(map[string]string{
		"appdev": workflow.StrategyPushDev,
	})
	twice := buildStrategyGuidance(map[string]string{
		"appdev": workflow.StrategyPushDev,
		"apidev": workflow.StrategyPushDev,
	})
	// Deduplication invariant: repeating the same strategy across services
	// must not multiply the matched atom set. Output is byte-identical.
	if once != twice {
		t.Errorf("duplicate strategy should produce identical guidance; once != twice")
	}
}

// --- allStrategiesAre + anyStrategyIs ---

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

func TestAnyStrategyIs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		strategies map[string]string
		target     string
		want       bool
	}{
		{"none", map[string]string{"a": "push-dev"}, "push-git", false},
		{"one of mixed", map[string]string{"a": "push-dev", "b": "push-git"}, "push-git", true},
		{"all match", map[string]string{"a": "manual", "b": "manual"}, "manual", true},
		{"empty", map[string]string{}, "push-git", false},
		{"nil", nil, "push-git", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := anyStrategyIs(tt.strategies, tt.target)
			if got != tt.want {
				t.Errorf("anyStrategyIs(%v, %q) = %v, want %v", tt.strategies, tt.target, got, tt.want)
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
