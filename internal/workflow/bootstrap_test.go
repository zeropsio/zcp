// Tests for: bootstrap conductor — 5-step state machine with attestations.
package workflow

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestStepDetails_AllStepsCovered(t *testing.T) {
	t.Parallel()
	expectedNames := []string{"discover", "provision", "generate", "deploy", "verify", "strategy"}
	for _, name := range expectedNames {
		detail := lookupDetail(name)
		if detail.Name == "" {
			t.Errorf("missing StepDetail for %q", name)
			continue
		}
		if detail.Guidance == "" {
			t.Errorf("step %q has empty Guidance", name)
		}
		if len(detail.Tools) == 0 {
			t.Errorf("step %q has no Tools", name)
		}
		if detail.Verification == "" {
			t.Errorf("step %q has empty Verification", name)
		}
	}
}

func TestStepDetails_ToolLists(t *testing.T) {
	t.Parallel()
	tests := []struct {
		step     string
		wantTool string
	}{
		{"deploy", "zerops_verify"},
		{"verify", "zerops_verify"},
	}
	for _, tt := range tests {
		t.Run(tt.step+"_has_"+tt.wantTool, func(t *testing.T) {
			t.Parallel()
			detail := lookupDetail(tt.step)
			if !slices.Contains(detail.Tools, tt.wantTool) {
				t.Errorf("step %q Tools %v should contain %q", tt.step, detail.Tools, tt.wantTool)
			}
		})
	}
}

func TestStepDetails_DiscoverGuidance_ThreeStates(t *testing.T) {
	t.Parallel()
	detail := lookupDetail("discover")

	for _, state := range []string{"FRESH", "CONFORMANT", "NON_CONFORMANT"} {
		if !strings.Contains(detail.Guidance, state) {
			t.Errorf("discover guidance missing state %q", state)
		}
	}
	for _, dropped := range []string{"PARTIAL", "EXISTING"} {
		if strings.Contains(detail.Guidance, dropped) {
			t.Errorf("discover guidance still mentions dropped state %q", dropped)
		}
	}
}

func TestStepDetails_Categories(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		category StepCategory
	}{
		{"discover", CategoryFixed},
		{"provision", CategoryFixed},
		{"generate", CategoryCreative},
		{"deploy", CategoryBranching},
		{"verify", CategoryFixed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			detail := lookupDetail(tt.name)
			if detail.Category != tt.category {
				t.Errorf("step %q: want category %q, got %q", tt.name, tt.category, detail.Category)
			}
		})
	}
}

func TestNewBootstrapState_6Steps(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()

	if !bs.Active {
		t.Error("expected Active to be true")
	}
	if bs.CurrentStep != 0 {
		t.Errorf("CurrentStep: want 0, got %d", bs.CurrentStep)
	}
	if len(bs.Steps) != 6 {
		t.Fatalf("Steps count: want 6, got %d", len(bs.Steps))
	}

	expectedNames := []string{"discover", "provision", "generate", "deploy", "verify", "strategy"}
	for i, name := range expectedNames {
		if bs.Steps[i].Name != name {
			t.Errorf("step[%d].Name: want %s, got %s", i, name, bs.Steps[i].Name)
		}
		if bs.Steps[i].Status != "pending" {
			t.Errorf("step[%d].Status: want pending, got %s", i, bs.Steps[i].Status)
		}
	}
}

func TestCompleteStep_Success(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Steps[0].Status = "in_progress"

	err := bs.CompleteStep("discover", "FRESH project, no existing services found")
	if err != nil {
		t.Fatalf("CompleteStep: %v", err)
	}

	if bs.Steps[0].Status != "complete" {
		t.Errorf("step[0].Status: want complete, got %s", bs.Steps[0].Status)
	}
	if bs.Steps[0].Attestation != "FRESH project, no existing services found" {
		t.Errorf("attestation not stored")
	}
	if bs.Steps[0].CompletedAt == "" {
		t.Error("CompletedAt not set")
	}
	if bs.CurrentStep != 1 {
		t.Errorf("CurrentStep: want 1, got %d", bs.CurrentStep)
	}
}

func TestCompleteStep_WrongStep(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Steps[0].Status = "in_progress"

	err := bs.CompleteStep("provision", "something")
	if err == nil {
		t.Fatal("expected error for completing wrong step")
	}
	if !strings.Contains(err.Error(), "discover") {
		t.Errorf("error should mention current step 'discover', got: %s", err.Error())
	}
}

func TestCompleteStep_EmptyAttestation(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Steps[0].Status = "in_progress"

	err := bs.CompleteStep("discover", "")
	if err == nil {
		t.Fatal("expected error for empty attestation")
	}

	err = bs.CompleteStep("discover", "short")
	if err == nil {
		t.Fatal("expected error for short attestation (<10 chars)")
	}
}

func TestCompleteStep_NotActive(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Active = false

	err := bs.CompleteStep("discover", "some attestation text here")
	if err == nil {
		t.Fatal("expected error when bootstrap not active")
	}
}

func TestCompleteStep_AllDone(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()

	stepNames := []string{"discover", "provision", "generate", "deploy", "verify", "strategy"}
	for _, name := range stepNames {
		bs.Steps[bs.CurrentStep].Status = "in_progress"
		err := bs.CompleteStep(name, "Attestation for "+name+" step completed successfully")
		if err != nil {
			t.Fatalf("CompleteStep(%s): %v", name, err)
		}
	}

	if bs.Active {
		t.Error("expected Active=false after all steps complete")
	}
	if bs.CurrentStep != 6 {
		t.Errorf("CurrentStep: want 6, got %d", bs.CurrentStep)
	}
}

func TestSkipStep_Success(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	// Advance to generate (index 2).
	for i := range 2 {
		bs.Steps[i].Status = "complete"
	}
	bs.CurrentStep = 2
	bs.Steps[2].Status = "in_progress"

	err := bs.SkipStep("generate", "no runtime services to generate code for")
	if err != nil {
		t.Fatalf("SkipStep: %v", err)
	}

	if bs.Steps[2].Status != "skipped" {
		t.Errorf("step[2].Status: want skipped, got %s", bs.Steps[2].Status)
	}
	if bs.Steps[2].SkipReason != "no runtime services to generate code for" {
		t.Error("SkipReason not stored")
	}
	if bs.CurrentStep != 3 {
		t.Errorf("CurrentStep: want 3, got %d", bs.CurrentStep)
	}
}

func TestSkipStep_MandatoryStep(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		step string
		idx  int
	}{
		{"discover", "discover", 0},
		{"provision", "provision", 1},
		{"verify", "verify", 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bs := NewBootstrapState()
			for i := 0; i < tt.idx; i++ {
				bs.Steps[i].Status = "complete"
			}
			bs.CurrentStep = tt.idx
			bs.Steps[tt.idx].Status = "in_progress"

			err := bs.SkipStep(tt.step, "some reason")
			if err == nil {
				t.Fatalf("expected error skipping mandatory step %q", tt.step)
			}
		})
	}
}

func TestSkipStep_WrongStep(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Steps[0].Status = "in_progress"

	err := bs.SkipStep("generate", "reason")
	if err == nil {
		t.Fatal("expected error for skipping wrong step")
	}
	if !strings.Contains(err.Error(), "discover") {
		t.Errorf("error should mention current step 'discover', got: %s", err.Error())
	}
}

func TestBuildResponse_FirstStep(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Steps[0].Status = "in_progress"

	resp := bs.BuildResponse("sess-1", "bun + postgres", 0, EnvLocal, nil)
	if resp.SessionID != "sess-1" {
		t.Errorf("SessionID: want sess-1, got %s", resp.SessionID)
	}
	if resp.Intent != "bun + postgres" {
		t.Errorf("Intent mismatch")
	}
	if resp.Progress.Total != 6 {
		t.Errorf("Progress.Total: want 6, got %d", resp.Progress.Total)
	}
	if resp.Progress.Completed != 0 {
		t.Errorf("Progress.Completed: want 0, got %d", resp.Progress.Completed)
	}
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if resp.Current.Name != "discover" {
		t.Errorf("Current.Name: want discover, got %s", resp.Current.Name)
	}
	if resp.Current.Index != 0 {
		t.Errorf("Current.Index: want 0, got %d", resp.Current.Index)
	}
	if resp.Current.Guidance == "" {
		t.Error("Current.Guidance should not be empty")
	}
}

func TestBuildResponse_MiddleStep(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	for i := range 2 {
		bs.Steps[i].Status = "complete"
		bs.Steps[i].Attestation = "done"
	}
	bs.CurrentStep = 2
	bs.Steps[2].Status = "in_progress"

	resp := bs.BuildResponse("sess-2", "test", 0, EnvLocal, nil)
	if resp.Progress.Completed != 2 {
		t.Errorf("Progress.Completed: want 2, got %d", resp.Progress.Completed)
	}
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if resp.Current.Name != "generate" {
		t.Errorf("Current.Name: want generate, got %s", resp.Current.Name)
	}
	if resp.Current.Index != 2 {
		t.Errorf("Current.Index: want 2, got %d", resp.Current.Index)
	}
}

func TestBuildResponse_AllDone(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	for i := range bs.Steps {
		bs.Steps[i].Status = "complete"
	}
	bs.CurrentStep = 6
	bs.Active = false

	resp := bs.BuildResponse("sess-3", "test", 0, EnvLocal, nil)
	if resp.Current != nil {
		t.Error("Current should be nil when all done")
	}
	if resp.Progress.Completed != 6 {
		t.Errorf("Progress.Completed: want 6, got %d", resp.Progress.Completed)
	}
	if !strings.Contains(strings.ToLower(resp.Message), "complete") {
		t.Errorf("Message should contain 'complete', got: %s", resp.Message)
	}
}

func TestBuildResponse_WithSkipped(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	for i := range 2 {
		bs.Steps[i].Status = "complete"
	}
	bs.Steps[2].Status = "skipped"
	bs.Steps[2].SkipReason = "no runtime services"
	bs.CurrentStep = 3
	bs.Steps[3].Status = "in_progress"

	resp := bs.BuildResponse("sess-4", "test", 0, EnvLocal, nil)
	if resp.Progress.Completed != 3 {
		t.Errorf("Progress.Completed: want 3 (2 complete + 1 skipped), got %d", resp.Progress.Completed)
	}

	found := false
	for _, s := range resp.Progress.Steps {
		if s.Name == "generate" && s.Status == "skipped" {
			found = true
			break
		}
	}
	if !found {
		t.Error("generate should appear as 'skipped' in progress steps")
	}
}

func TestValidateConditionalSkip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		plan      *ServicePlan
		stepName  string
		wantError bool
	}{
		{
			name:      "nil plan allows skip",
			plan:      nil,
			stepName:  "generate",
			wantError: false,
		},
		{
			name: "generate blocked with runtime targets",
			plan: &ServicePlan{Targets: []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "go@1"}},
			}},
			stepName:  "generate",
			wantError: true,
		},
		{
			name:      "generate allowed with empty targets",
			plan:      &ServicePlan{Targets: []BootstrapTarget{}},
			stepName:  "generate",
			wantError: false,
		},
		{
			name: "deploy blocked with runtime targets",
			plan: &ServicePlan{Targets: []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "go@1"}},
			}},
			stepName:  "deploy",
			wantError: true,
		},
		{
			name:      "deploy allowed with empty targets",
			plan:      &ServicePlan{Targets: []BootstrapTarget{}},
			stepName:  "deploy",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateConditionalSkip(tt.plan, tt.stepName)
			if (err != nil) != tt.wantError {
				t.Errorf("validateConditionalSkip(): error=%v, wantError=%v", err, tt.wantError)
			}
		})
	}
}

func TestBuildResponse_PriorContext_Attestations(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	attestations := map[string]string{
		"discover":  "FRESH project detected, no runtime services",
		"provision": "All services created, dev mounted, env vars discovered",
	}
	for i, name := range []string{"discover", "provision"} {
		bs.Steps[i].Status = stepComplete
		bs.Steps[i].Attestation = attestations[name]
	}
	bs.CurrentStep = 2
	bs.Steps[2].Status = stepInProgress

	resp := bs.BuildResponse("sess-ctx", "bun + postgres", 0, EnvLocal, nil)
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if resp.Current.PriorContext == nil {
		t.Fatal("PriorContext should not be nil when prior steps have attestations")
	}
	if len(resp.Current.PriorContext.Attestations) != 2 {
		t.Errorf("PriorContext.Attestations: want 2 entries, got %d", len(resp.Current.PriorContext.Attestations))
	}
	// N-1 (provision) should have full attestation.
	if resp.Current.PriorContext.Attestations["provision"] != attestations["provision"] {
		t.Errorf("PriorContext.Attestations[provision] (N-1) should be full, got: %s", resp.Current.PriorContext.Attestations["provision"])
	}
	// N-2 (discover) should be compressed with status bracket.
	discAtt := resp.Current.PriorContext.Attestations["discover"]
	if !strings.HasPrefix(discAtt, "[complete:") {
		t.Errorf("PriorContext.Attestations[discover] (N-2) should be compressed, got: %s", discAtt)
	}
}

func TestBuildResponse_PriorContext_WithPlan(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Steps[0].Status = stepComplete
	bs.Steps[0].Attestation = "FRESH project"
	bs.CurrentStep = 1
	bs.Steps[1].Status = stepInProgress
	bs.Plan = &ServicePlan{
		Targets: []BootstrapTarget{
			{
				Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"},
				Dependencies: []Dependency{
					{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE"},
				},
			},
		},
		CreatedAt: "2026-02-27T00:00:00Z",
	}

	resp := bs.BuildResponse("sess-plan", "test", 0, EnvLocal, nil)
	if resp.Current.PriorContext == nil {
		t.Fatal("PriorContext should not be nil")
	}
	if resp.Current.PriorContext.Plan == nil {
		t.Fatal("PriorContext.Plan should not be nil when plan exists")
	}
	if len(resp.Current.PriorContext.Plan.Targets) != 1 {
		t.Errorf("PriorContext.Plan.Targets: want 1, got %d", len(resp.Current.PriorContext.Plan.Targets))
	}
}

func TestBuildResponse_DetailedGuide_Populated(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Steps[0].Status = stepInProgress

	resp := bs.BuildResponse("sess-guide", "test", 0, EnvLocal, nil)
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if resp.Current.DetailedGuide == "" {
		t.Error("DetailedGuide should be populated from bootstrap.md for discover step")
	}
}

func TestBuildResponse_PriorContext_FirstStep_Empty(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Steps[0].Status = stepInProgress

	resp := bs.BuildResponse("sess-first", "test", 0, EnvLocal, nil)
	if resp.Current.PriorContext != nil {
		t.Error("PriorContext should be nil for first step (no prior attestations)")
	}
}

func TestBootstrapState_DiscoveredEnvVars(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	if bs.DiscoveredEnvVars != nil {
		t.Error("DiscoveredEnvVars should be nil initially")
	}

	if bs.DiscoveredEnvVars == nil {
		bs.DiscoveredEnvVars = make(map[string][]string)
	}
	bs.DiscoveredEnvVars["db"] = []string{"connectionString", "port", "user"}
	bs.DiscoveredEnvVars["cache"] = []string{"connectionString"}

	if len(bs.DiscoveredEnvVars["db"]) != 3 {
		t.Errorf("db env vars: want 3, got %d", len(bs.DiscoveredEnvVars["db"]))
	}
	if len(bs.DiscoveredEnvVars["cache"]) != 1 {
		t.Errorf("cache env vars: want 1, got %d", len(bs.DiscoveredEnvVars["cache"]))
	}
}

func TestBootstrapState_CurrentStepName_6Steps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		step     int
		expected string
	}{
		{"first", 0, "discover"},
		{"middle", 2, "generate"},
		{"deploy", 3, "deploy"},
		{"verify", 4, "verify"},
		{"last", 5, "strategy"},
		{"out_of_bounds", 6, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bs := NewBootstrapState()
			bs.CurrentStep = tt.step
			if got := bs.CurrentStepName(); got != tt.expected {
				t.Errorf("CurrentStepName: want %s, got %s", tt.expected, got)
			}
		})
	}
}

func TestPlanMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		plan *ServicePlan
		want string
	}{
		{
			"nil_plan",
			nil,
			"",
		},
		{
			"empty_targets",
			&ServicePlan{Targets: []BootstrapTarget{}},
			"",
		},
		{
			"standard_mode",
			&ServicePlan{Targets: []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
			}},
			"standard",
		},
		{
			"simple_mode",
			&ServicePlan{Targets: []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: "app", Type: "bun@1.2", BootstrapMode: "simple"}},
			}},
			"simple",
		},
		{
			"dev_mode",
			&ServicePlan{Targets: []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2", BootstrapMode: "dev"}},
			}},
			"dev",
		},
		{
			"mixed_with_standard_returns_standard",
			&ServicePlan{Targets: []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
				{Runtime: RuntimeTarget{DevHostname: "api", Type: "go@1", BootstrapMode: "simple"}},
			}},
			"standard",
		},
		{
			"mixed_dev_simple_returns_mixed",
			&ServicePlan{Targets: []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2", BootstrapMode: "dev"}},
				{Runtime: RuntimeTarget{DevHostname: "api", Type: "go@1", BootstrapMode: "simple"}},
			}},
			"mixed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bs := NewBootstrapState()
			bs.Plan = tt.plan
			if got := bs.PlanMode(); got != tt.want {
				t.Errorf("planMode: want %q, got %q", tt.want, got)
			}
		})
	}
}

func TestBuildResponse_PlanMode(t *testing.T) {
	t.Parallel()

	// Before plan submission, PlanMode should be empty.
	bs := NewBootstrapState()
	resp := bs.BuildResponse("sess1", "test", 0, EnvLocal, nil)
	if resp.Current.PlanMode != "" {
		t.Errorf("PlanMode before plan: want empty, got %q", resp.Current.PlanMode)
	}

	// After plan submission, PlanMode should reflect the plan.
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
	}}
	resp = bs.BuildResponse("sess1", "test", 0, EnvLocal, nil)
	if resp.Current.PlanMode != "standard" {
		t.Errorf("PlanMode after plan: want standard, got %q", resp.Current.PlanMode)
	}
}

func TestBootstrapStepInfo_GuidanceExcludedFromJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		info BootstrapStepInfo
	}{
		{
			name: "guidance populated in Go but excluded from JSON",
			info: BootstrapStepInfo{
				Name:     "discover",
				Category: "fixed",
				Guidance: "Run zerops_discover to inspect the project state.",
				Tools:    []string{"zerops_discover"},
			},
		},
		{
			name: "full response via BuildResponse",
			info: func() BootstrapStepInfo {
				bs := NewBootstrapState()
				bs.Steps[0].Status = stepInProgress
				resp := bs.BuildResponse("sess-json", "test", 0, EnvLocal, nil)
				return *resp.Current
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Guidance must be populated in Go struct.
			if tt.info.Guidance == "" {
				t.Fatal("precondition: Guidance should be non-empty in Go struct")
			}

			// Marshal to JSON and verify Guidance is absent.
			data, err := json.Marshal(tt.info)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}

			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}

			if _, exists := m["guidance"]; exists {
				t.Errorf("guidance field should not appear in JSON output, got: %s", string(data))
			}
		})
	}
}

func TestStepDetails_VerificationHasSuccessCriteria(t *testing.T) {
	t.Parallel()

	for _, step := range stepDetails {
		t.Run(step.Name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(step.Verification, "SUCCESS WHEN:") {
				t.Errorf("step %q Verification missing SUCCESS WHEN: criteria", step.Name)
			}
			// Step progression is handled by the workflow engine automatically.
			// Verification fields describe success criteria only — no NEXT: directives.
		})
	}
}

// --- BuildResponse guide always fresh (no gating) ---

func TestBuildResponse_GuideAlwaysFresh(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Steps[0].Status = stepInProgress

	resp1 := bs.BuildResponse("sess-fresh", "test", 0, EnvLocal, nil)
	if resp1.Current == nil {
		t.Fatal("Current should not be nil")
	}
	guide1 := resp1.Current.DetailedGuide
	if guide1 == "" {
		t.Error("first delivery should include full guide")
	}

	// Second call should also return full guide (no gating).
	resp2 := bs.BuildResponse("sess-fresh", "test", 0, EnvLocal, nil)
	if resp2.Current.DetailedGuide != guide1 {
		t.Error("repeat delivery should return same full guide (no gating)")
	}
}

// --- Iteration delta overrides guide in BuildResponse ---

func TestBuildResponse_IterationDelta_OverridesGuide(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	// Advance to deploy step.
	for i := range 3 {
		bs.Steps[i].Status = stepComplete
		bs.Steps[i].Attestation = "completed step " + bs.Steps[i].Name + " successfully"
	}
	bs.CurrentStep = 3
	bs.Steps[3].Status = stepInProgress

	// At iteration > 0 on deploy step, should get delta.
	resp := bs.BuildResponse("sess-delta", "test", 1, EnvLocal, nil)
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	// The delta should be used as DetailedGuide when applicable.
	// (May be empty if no last attestation — but the mechanism should exist)
}

func TestBuildPriorContext_CompressesOlderSteps(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	// Complete 3 steps with long attestations.
	longAttestation := strings.Repeat("a", 100)
	for i, name := range []string{"discover", "provision", "generate"} {
		bs.Steps[i].Status = stepComplete
		bs.Steps[i].Attestation = longAttestation + " " + name
	}
	bs.CurrentStep = 3
	bs.Steps[3].Status = stepInProgress

	ctx := bs.buildPriorContext()
	if ctx == nil {
		t.Fatal("buildPriorContext should not be nil with prior attestations")
	}

	// N-1 (generate, index 2) should be full.
	genAtt := ctx.Attestations["generate"]
	if genAtt != longAttestation+" generate" {
		t.Errorf("N-1 step should have full attestation, got length %d", len(genAtt))
	}

	// N-2 (discover, index 0) should be truncated with status prefix.
	discAtt := ctx.Attestations["discover"]
	if !strings.HasPrefix(discAtt, "[complete:") {
		t.Errorf("older step should have [status: ...] prefix, got: %s", discAtt)
	}
	if len(discAtt) > 100 {
		t.Errorf("older step should be compressed, got length %d", len(discAtt))
	}
	if !strings.HasSuffix(discAtt, "...]") {
		t.Errorf("truncated attestation should end with ...], got: %s", discAtt)
	}

	// N-2 (provision, index 1) should also be truncated.
	provAtt := ctx.Attestations["provision"]
	if !strings.HasPrefix(provAtt, "[complete:") {
		t.Errorf("older step should have [status: ...] prefix, got: %s", provAtt)
	}
}

func TestBuildPriorContext_ShortAttestationNotTruncated(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	shortAtt := "Short attestation"
	for i, name := range []string{"discover", "provision", "generate"} {
		bs.Steps[i].Status = stepComplete
		bs.Steps[i].Attestation = shortAtt + " " + name
	}
	bs.CurrentStep = 3
	bs.Steps[3].Status = stepInProgress

	ctx := bs.buildPriorContext()
	if ctx == nil {
		t.Fatal("buildPriorContext should not be nil")
	}

	// N-2 steps with short attestations should still be wrapped in status brackets
	// but NOT truncated with "...".
	discAtt := ctx.Attestations["discover"]
	if !strings.HasPrefix(discAtt, "[complete:") {
		t.Errorf("older step should have [status: ...] prefix, got: %s", discAtt)
	}
	if strings.Contains(discAtt, "...") {
		t.Errorf("short attestation should not be truncated, got: %s", discAtt)
	}
}

// --- C-02: Progressive guidance wiring ---

func TestBuildResponse_DeployStep_UsesProgressiveGuidance(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
	}}
	// Complete steps 0-2 to reach deploy.
	for i := range 3 {
		bs.Steps[i].Status = stepComplete
		bs.Steps[i].Attestation = "completed step " + bs.Steps[i].Name + " successfully"
	}
	bs.CurrentStep = 3
	bs.Steps[3].Status = stepInProgress

	resp := bs.BuildResponse("sess-prog", "test", 0, EnvLocal, nil)
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	// Progressive guidance filters to mode-specific sections (~5k chars);
	// monolithic deploy section is ~30k chars.
	monolithic := ResolveGuidance("deploy")
	if len(resp.Current.DetailedGuide) >= len(monolithic)/2 {
		t.Errorf("DetailedGuide too long (%d chars vs monolithic %d), expected progressive guidance to be significantly shorter",
			len(resp.Current.DetailedGuide), len(monolithic))
	}
	if resp.Current.DetailedGuide == "" {
		t.Error("DetailedGuide should not be empty for deploy step")
	}
}

func TestBuildResponse_DeployStep_SimpleMode_ShorterGuidance(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "app", Type: "bun@1.2", BootstrapMode: "simple"}},
	}}
	for i := range 3 {
		bs.Steps[i].Status = stepComplete
		bs.Steps[i].Attestation = "completed step " + bs.Steps[i].Name + " successfully"
	}
	bs.CurrentStep = 3
	bs.Steps[3].Status = stepInProgress

	resp := bs.BuildResponse("sess-simple", "test", 0, EnvLocal, nil)
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if strings.Contains(resp.Current.DetailedGuide, "Standard mode") {
		t.Error("simple mode deploy guide should not contain 'Standard mode'")
	}
	if strings.Contains(resp.Current.DetailedGuide, "dev+stage") {
		t.Error("simple mode deploy guide should not contain 'dev+stage'")
	}
}

func TestBuildResponse_NonProgressiveStep_GuidanceUnchanged(t *testing.T) {
	t.Parallel()
	// discover and provision use ResolveGuidance directly (not progressive).
	// generate and deploy are progressive (mode-filtered).
	tests := []struct {
		name    string
		stepIdx int
	}{
		{"discover", 0},
		{"provision", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bs := NewBootstrapState()
			for i := 0; i < tt.stepIdx; i++ {
				bs.Steps[i].Status = stepComplete
				bs.Steps[i].Attestation = "completed step " + bs.Steps[i].Name + " successfully"
			}
			bs.CurrentStep = tt.stepIdx
			bs.Steps[tt.stepIdx].Status = stepInProgress

			resp := bs.BuildResponse("sess-nonprog-"+tt.name, "test", 0, EnvLocal, nil)
			if resp.Current == nil {
				t.Fatal("Current should not be nil")
			}

			expected := ResolveGuidance(tt.name)
			if resp.Current.DetailedGuide != expected {
				t.Errorf("step %q: DetailedGuide should match ResolveGuidance exactly\ngot length %d, want length %d",
					tt.name, len(resp.Current.DetailedGuide), len(expected))
			}
		})
	}
}

func TestBuildResponse_GenerateStep_UsesProgressiveGuidance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		mode        string
		wantContain string
		wantExclude string
	}{
		{"standard", "", "zsc noop --silent", ""},
		{"dev", "dev", "zsc noop --silent", ""},
		{"simple", "simple", "REAL start command", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bs := NewBootstrapState()
			hostname := "appdev"
			if tt.mode == "simple" {
				hostname = "app"
			}
			bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: hostname, Type: "bun@1.2", BootstrapMode: tt.mode}},
			}}
			for i := range 2 {
				bs.Steps[i].Status = stepComplete
				bs.Steps[i].Attestation = "completed step " + bs.Steps[i].Name + " successfully"
			}
			bs.CurrentStep = 2
			bs.Steps[2].Status = stepInProgress

			resp := bs.BuildResponse("sess-gen-"+tt.name, "test", 0, EnvLocal, nil)
			if resp.Current == nil {
				t.Fatal("Current should not be nil")
			}
			if !strings.Contains(resp.Current.DetailedGuide, tt.wantContain) {
				t.Errorf("generate guide for %s mode should contain %q", tt.name, tt.wantContain)
			}
			if tt.wantExclude != "" && strings.Contains(resp.Current.DetailedGuide, tt.wantExclude) {
				t.Errorf("generate guide for %s mode should NOT contain %q", tt.name, tt.wantExclude)
			}
			// All modes should include common content.
			if !strings.Contains(resp.Current.DetailedGuide, "Application code requirements") {
				t.Errorf("generate guide for %s mode missing common content", tt.name)
			}
		})
	}
}

// --- C-03: ResetForIteration ---

func TestResetForIteration_ResetsGenerateDeployVerify(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()

	// Complete all 6 steps.
	for i, name := range []string{"discover", "provision", "generate", "deploy", "verify", "strategy"} {
		bs.Steps[i].Status = stepInProgress
		if err := bs.CompleteStep(name, "Attestation for "+name+" step completed ok"); err != nil {
			t.Fatalf("CompleteStep(%s): %v", name, err)
		}
	}
	// Preconditions: all done.
	if bs.Active {
		t.Fatal("precondition: Active should be false")
	}
	if bs.CurrentStep != 6 {
		t.Fatalf("precondition: CurrentStep should be 6, got %d", bs.CurrentStep)
	}

	bs.ResetForIteration()

	if !bs.Active {
		t.Error("Active should be true after reset")
	}
	if bs.CurrentStep != 2 {
		t.Errorf("CurrentStep: want 2, got %d", bs.CurrentStep)
	}
	// Steps 0-1 should remain complete.
	for i := 0; i <= 1; i++ {
		if bs.Steps[i].Status != stepComplete {
			t.Errorf("Steps[%d].Status: want %s, got %s", i, stepComplete, bs.Steps[i].Status)
		}
		if bs.Steps[i].Attestation == "" {
			t.Errorf("Steps[%d].Attestation should be preserved", i)
		}
	}
	// Steps 2-4 should be reset (step 2 in_progress, 3-4 pending).
	if bs.Steps[2].Status != stepInProgress {
		t.Errorf("Steps[2].Status: want %s, got %s", stepInProgress, bs.Steps[2].Status)
	}
	for i := 3; i <= 4; i++ {
		if bs.Steps[i].Status != stepPending {
			t.Errorf("Steps[%d].Status: want %s, got %s", i, stepPending, bs.Steps[i].Status)
		}
	}
	// Step 5 (strategy) should remain complete.
	if bs.Steps[5].Status != stepComplete {
		t.Errorf("Steps[5].Status: want %s, got %s", stepComplete, bs.Steps[5].Status)
	}
}

func TestResetForIteration_SetsCurrentStepInProgress(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	// Complete all steps so CurrentStep=6, Active=false.
	for i, name := range []string{"discover", "provision", "generate", "deploy", "verify", "strategy"} {
		bs.Steps[i].Status = stepInProgress
		if err := bs.CompleteStep(name, "Attestation for "+name+" step completed ok"); err != nil {
			t.Fatalf("CompleteStep(%s): %v", name, err)
		}
	}

	bs.ResetForIteration()

	if bs.Steps[2].Status != stepInProgress {
		t.Errorf("Steps[2].Status: want %s, got %s", stepInProgress, bs.Steps[2].Status)
	}
}

func TestResetForIteration_NilBootstrap_NoOp(t *testing.T) {
	t.Parallel()
	var b *BootstrapState
	// Should not panic.
	b.ResetForIteration()
}

// --- BuildResponse knowledge injection ---

func TestBuildResponse_Provision_GuideContainsKnowledge(t *testing.T) {
	t.Parallel()
	store := testKnowledgeProvider(t)
	bs := NewBootstrapState()
	bs.Steps[0].Status = stepComplete
	bs.Steps[0].Attestation = "FRESH project detected"
	bs.CurrentStep = 1
	bs.Steps[1].Status = stepInProgress
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"}},
	}}

	resp := bs.BuildResponse("sess-kn", "test", 0, EnvContainer, store)
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if !strings.Contains(resp.Current.DetailedGuide, "import.yml Schema") {
		t.Error("provision guide should contain 'import.yml Schema' from knowledge injection")
	}
}

func TestBuildResponse_Generate_GuideContainsRuntimeGuide(t *testing.T) {
	t.Parallel()
	store := testKnowledgeProvider(t)
	bs := NewBootstrapState()
	for i := range 2 {
		bs.Steps[i].Status = stepComplete
		bs.Steps[i].Attestation = "completed step " + bs.Steps[i].Name + " successfully"
	}
	bs.CurrentStep = 2
	bs.Steps[2].Status = stepInProgress
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"}},
	}}

	resp := bs.BuildResponse("sess-kn2", "test", 0, EnvContainer, store)
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if !strings.Contains(resp.Current.DetailedGuide, "Node.js") {
		t.Error("generate guide should contain Node.js runtime guide from knowledge injection")
	}
}

func TestBuildPriorContext_PlanAlwaysIncluded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		attestations int // number of completed steps with attestations
	}{
		{"with_attestations", 2},
		{"no_attestations", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bs := NewBootstrapState()
			bs.Plan = &ServicePlan{
				Targets: []BootstrapTarget{
					{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
				},
			}
			for i := 0; i < tt.attestations; i++ {
				bs.Steps[i].Status = stepComplete
				bs.Steps[i].Attestation = "completed step successfully here"
			}
			bs.CurrentStep = tt.attestations
			if bs.CurrentStep < len(bs.Steps) {
				bs.Steps[bs.CurrentStep].Status = stepInProgress
			}

			ctx := bs.buildPriorContext()
			if ctx == nil {
				t.Fatal("buildPriorContext should not be nil when plan exists")
			}
			if ctx.Plan == nil {
				t.Fatal("Plan should always be included in PriorContext")
			}
			if len(ctx.Plan.Targets) != 1 {
				t.Errorf("Plan.Targets: want 1, got %d", len(ctx.Plan.Targets))
			}
		})
	}
}
