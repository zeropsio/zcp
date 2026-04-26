package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// --- handleStrategy: update mode ---

func TestHandleStrategy_ValidUpdate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		strategies map[string]string
	}{
		{
			"push-dev",
			map[string]string{"appdev": topology.StrategyPushDev},
		},
		{
			"push-git",
			map[string]string{"appdev": topology.StrategyPushGit},
		},
		{
			"manual",
			map[string]string{"appdev": topology.StrategyManual},
		},
		{
			"multiple services",
			map[string]string{"appdev": topology.StrategyPushDev, "apidev": topology.StrategyPushDev},
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
			// G11: response MUST NOT carry a `next` hint — the canonical
			// "what next" surface is `zerops_workflow action="status"`,
			// reading the live envelope. Pre-fix this carried a hand-
			// rolled per-strategy hint that drifted from the atom guidance.
			if _, hasNext := resp["next"]; hasNext {
				t.Errorf("response must not include `next` field; agent calls status for next plan; got: %q", resp["next"])
			}

			// Verify meta was persisted with strategy + confirmed flag.
			for hostname, strategy := range tt.strategies {
				meta, readErr := workflow.ReadServiceMeta(dir, hostname)
				if readErr != nil {
					t.Fatalf("read meta %s: %v", hostname, readErr)
				}
				if meta.DeployStrategy != topology.DeployStrategy(strategy) {
					t.Errorf("%s DeployStrategy: want %q, got %q", hostname, strategy, meta.DeployStrategy)
				}
				if !meta.StrategyConfirmed {
					t.Errorf("%s StrategyConfirmed: want true", hostname)
				}
			}
		})
	}
}

// Pair-keyed invariant (spec-workflows.md §8 E8): setting strategy on a stage
// hostname resolves to the same dev-keyed meta that owns the pair. Before
// FindServiceMeta promotion, a stage hostname surfaced as
// "Service appstage is not bootstrapped" because the direct file read missed
// the pair file.
func TestHandleStrategy_StageHostname_ResolvesToPair(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// One meta file representing the standard pair, keyed by dev hostname.
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:       "appdev",
		StageHostname:  "appstage",
		Mode:           topology.PlanModeStandard,
		BootstrappedAt: "2026-04-22",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	// Agent asks to set strategy on the stage half. Must resolve to the
	// pair-meta and succeed.
	input := WorkflowInput{Strategies: map[string]string{"appstage": topology.StrategyPushDev}}
	result, _, err := handleStrategy(input, dir, runtime.Info{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("strategy set on stage hostname must succeed (pair-keyed invariant), got: %s",
			resultText(t, result))
	}

	// Strategy lands on the pair-meta (dev-keyed file).
	meta, err := workflow.ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta(appdev): %v", err)
	}
	if meta.DeployStrategy != topology.StrategyPushDev {
		t.Errorf("DeployStrategy on dev-keyed meta: want %q, got %q",
			topology.StrategyPushDev, meta.DeployStrategy)
	}
	if !meta.StrategyConfirmed {
		t.Error("StrategyConfirmed: want true after strategy set")
	}
	// No second file created for the stage hostname — pair is one file.
	if stageMeta, _ := workflow.ReadServiceMeta(dir, "appstage"); stageMeta != nil {
		t.Errorf("stage-keyed meta file must not be created (pair-keyed invariant)")
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
		DeployStrategy: topology.StrategyPushDev,
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
	// Parse into a permissive map first to assert the absence of `next`
	// (G11: response must not carry a free-form hint).
	var raw map[string]any
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		t.Fatalf("unmarshal listing response: %v (text: %s)", err, text)
	}
	if _, hasNext := raw["next"]; hasNext {
		t.Errorf("listing response must not include `next` field; per-entry Hint suffices and status owns the lifecycle Plan; got: %v", raw["next"])
	}

	var resp struct {
		Status   string `json:"status"`
		Services []struct {
			Hostname string   `json:"hostname"`
			Current  string   `json:"current"`
			Options  []string `json:"options"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal typed response: %v (text: %s)", err, text)
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
	if byHost["appdev"] != topology.StrategyPushDev {
		t.Errorf("appdev current: want %q, got %q", topology.StrategyPushDev, byHost["appdev"])
	}
	if byHost["apidev"] != string(topology.StrategyUnset) {
		t.Errorf("apidev current: want %q, got %q", topology.StrategyUnset, byHost["apidev"])
	}
	if _, seen := byHost["incomplete"]; seen {
		t.Error("listing must skip incomplete metas")
	}
}

// F3 regression: strategy action must NOT auto-create a ServiceMeta for a hostname
// that ZCP never bootstrapped. An orphan meta with empty Mode/BootstrappedAt poisons
// every downstream consumer (router, briefing, hostname locks).
func TestHandleStrategy_UnknownHostname_RefusesToCreateOrphan(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	input := WorkflowInput{Strategies: map[string]string{"newservice": topology.StrategyPushGit}}
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
		Hostname: "appdev", Mode: topology.PlanModeDev, BootstrapSession: "sess1",
		// no BootstrappedAt -> incomplete
	}); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	input := WorkflowInput{Strategies: map[string]string{"appdev": topology.StrategyPushGit}}
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
				Mode:           topology.PlanModeDev,
				BootstrappedAt: "2026-04-05",
			}); err != nil {
				t.Fatalf("write meta: %v", err)
			}

			input := WorkflowInput{
				Strategies: map[string]string{"appdev": topology.StrategyPushGit},
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
		strategies   map[string]topology.DeployStrategy
		wantContains string
		wantNonEmpty bool
	}{
		{"push-dev", map[string]topology.DeployStrategy{"a": topology.StrategyPushDev}, "Push-Dev", true},
		{"manual", map[string]topology.DeployStrategy{"a": topology.StrategyManual}, "Manual", true},
		{"duplicate deduplicates", map[string]topology.DeployStrategy{"a": topology.StrategyPushDev, "b": topology.StrategyPushDev}, "Push-Dev", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Container env: matches both develop-push-dev-deploy-container
			// and develop-manual-deploy-container atoms; sufficient for
			// the wantContains substring assertions.
			rt := runtime.Info{InContainer: true}
			guidance := buildStrategyGuidance(rt, tt.strategies)
			if tt.wantNonEmpty && guidance == "" {
				t.Fatal("expected non-empty guidance")
			}
			if tt.wantContains != "" && !strings.Contains(guidance, tt.wantContains) {
				t.Errorf("should contain %q, got: %.200s", tt.wantContains, guidance)
			}
		})
	}
}

func TestBuildStrategyGuidance_PerServiceRendering(t *testing.T) {
	t.Parallel()
	rt := runtime.Info{InContainer: true}
	once := buildStrategyGuidance(rt, map[string]topology.DeployStrategy{
		"appdev": topology.StrategyPushDev,
	})
	twice := buildStrategyGuidance(rt, map[string]topology.DeployStrategy{
		"appdev": topology.StrategyPushDev,
		"apidev": topology.StrategyPushDev,
	})
	// Phase 2 (C2/F3) of the pipeline-repair plan: atoms with service-
	// scoped axes render once PER MATCHED SERVICE, so multi-service input
	// produces multi-service output where each rendered body carries the
	// matched service's hostname. Pre-fix the function manually
	// deduplicated atoms into a single rendering using a global
	// hostname picker — wrong host commands in multi-service projects.
	//
	// Here apidev and appdev are both push-dev; both services satisfy
	// the strategies axis on the strategy-specific atoms. Each atom
	// renders twice (once per service). Output for `twice` MUST contain
	// both hostnames; output for `once` contains only appdev.
	if !strings.Contains(once, "appdev") {
		t.Errorf("single-service guidance should mention appdev, got: %.200s", once)
	}
	if !strings.Contains(twice, "appdev") || !strings.Contains(twice, "apidev") {
		t.Errorf("multi-service guidance should mention both hostnames, got: %.300s", twice)
	}
}

// --- allStrategiesAre + anyStrategyIs ---

func TestAllStrategiesAre(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		strategies map[string]topology.DeployStrategy
		target     topology.DeployStrategy
		want       bool
	}{
		{"all same", map[string]topology.DeployStrategy{"a": "push-dev", "b": "push-dev"}, "push-dev", true},
		{"mixed", map[string]topology.DeployStrategy{"a": "push-dev", "b": "push-git"}, "push-dev", false},
		{"empty map", map[string]topology.DeployStrategy{}, "push-dev", false},
		{"nil map", nil, "push-dev", false},
		{"single match", map[string]topology.DeployStrategy{"a": "manual"}, "manual", true},
		{"single no match", map[string]topology.DeployStrategy{"a": "manual"}, "push-dev", false},
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
		strategies map[string]topology.DeployStrategy
		target     topology.DeployStrategy
		want       bool
	}{
		{"none", map[string]topology.DeployStrategy{"a": "push-dev"}, "push-git", false},
		{"one of mixed", map[string]topology.DeployStrategy{"a": "push-dev", "b": "push-git"}, "push-git", true},
		{"all match", map[string]topology.DeployStrategy{"a": "manual", "b": "manual"}, "manual", true},
		{"empty", map[string]topology.DeployStrategy{}, "push-git", false},
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
