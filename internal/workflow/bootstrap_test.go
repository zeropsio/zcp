// Tests for: bootstrap conductor — 3-step state machine with attestations
// (Option A: infra-only; code + deploy owned by develop flow).
package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

func TestStepDetails_AllStepsCovered(t *testing.T) {
	t.Parallel()
	expectedNames := []string{"discover", "provision", "close"}
	for _, name := range expectedNames {
		detail := lookupDetail(name)
		if detail.Name == "" {
			t.Errorf("missing StepDetail for %q", name)
			continue
		}
		if len(detail.Tools) == 0 {
			t.Errorf("step %q has no Tools", name)
		}
		if detail.Verification == "" {
			t.Errorf("step %q has empty Verification", name)
		}
	}
}

func TestNewBootstrapState_3Steps(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()

	if !bs.Active {
		t.Error("expected Active to be true")
	}
	if bs.CurrentStep != 0 {
		t.Errorf("CurrentStep: want 0, got %d", bs.CurrentStep)
	}
	if len(bs.Steps) != 3 {
		t.Fatalf("Steps count: want 3, got %d", len(bs.Steps))
	}

	expectedNames := []string{"discover", "provision", "close"}
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

	stepNames := []string{"discover", "provision", "close"}
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
	if bs.CurrentStep != 3 {
		t.Errorf("CurrentStep: want 3, got %d", bs.CurrentStep)
	}
}

// TestSkipStep_Success — close step is skippable when plan has no runtime targets.
func TestSkipStep_Success(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	// Managed-only plan — runtime targets empty.
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{}}
	// Advance to close (index 2).
	for i := range 2 {
		bs.Steps[i].Status = "complete"
	}
	bs.CurrentStep = 2
	bs.Steps[2].Status = "in_progress"

	err := bs.SkipStep("close", "managed-only bootstrap, no runtime services to register")
	if err != nil {
		t.Fatalf("SkipStep: %v", err)
	}

	if bs.Steps[2].Status != "skipped" {
		t.Errorf("step[2].Status: want skipped, got %s", bs.Steps[2].Status)
	}
	if bs.Steps[2].SkipReason != "managed-only bootstrap, no runtime services to register" {
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

	err := bs.SkipStep("close", "reason")
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
	if resp.Progress.Total != 3 {
		t.Errorf("Progress.Total: want 3, got %d", resp.Progress.Total)
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
}

func TestBuildResponse_MiddleStep(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Steps[0].Status = "complete"
	bs.Steps[0].Attestation = "done"
	bs.CurrentStep = 1
	bs.Steps[1].Status = "in_progress"

	resp := bs.BuildResponse("sess-2", "test", 0, EnvLocal, nil)
	if resp.Progress.Completed != 1 {
		t.Errorf("Progress.Completed: want 1, got %d", resp.Progress.Completed)
	}
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if resp.Current.Name != "provision" {
		t.Errorf("Current.Name: want provision, got %s", resp.Current.Name)
	}
	if resp.Current.Index != 1 {
		t.Errorf("Current.Index: want 1, got %d", resp.Current.Index)
	}
}

func TestBuildResponse_AllDone(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	for i := range bs.Steps {
		bs.Steps[i].Status = "complete"
	}
	bs.CurrentStep = 3
	bs.Active = false

	resp := bs.BuildResponse("sess-3", "test", 0, EnvLocal, nil)
	if resp.Current != nil {
		t.Error("Current should be nil when all done")
	}
	if resp.Progress.Completed != 3 {
		t.Errorf("Progress.Completed: want 3, got %d", resp.Progress.Completed)
	}
	if !strings.Contains(strings.ToLower(resp.Message), "complete") {
		t.Errorf("Message should contain 'complete', got: %s", resp.Message)
	}
}

func TestBuildResponse_WithSkipped(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	// Managed-only plan so close is skippable.
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{}}
	for i := range 2 {
		bs.Steps[i].Status = "complete"
	}
	bs.Steps[2].Status = "skipped"
	bs.Steps[2].SkipReason = "managed-only"
	bs.CurrentStep = 3

	resp := bs.BuildResponse("sess-4", "test", 0, EnvLocal, nil)
	if resp.Progress.Completed != 3 {
		t.Errorf("Progress.Completed: want 3 (2 complete + 1 skipped), got %d", resp.Progress.Completed)
	}

	found := false
	for _, s := range resp.Progress.Steps {
		if s.Name == "close" && s.Status == "skipped" {
			found = true
			break
		}
	}
	if !found {
		t.Error("close should appear as 'skipped' in progress steps")
	}
}

func TestValidateSkip_ConditionalCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		plan      *ServicePlan
		stepName  string
		wantError bool
	}{
		{
			name:      "close_nil_plan_allowed",
			plan:      nil,
			stepName:  "close",
			wantError: false,
		},
		{
			name: "close_with_runtime_targets_blocked",
			plan: &ServicePlan{Targets: []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "go@1"}},
			}},
			stepName:  "close",
			wantError: true,
		},
		{
			name:      "close_empty_targets_allowed",
			plan:      &ServicePlan{Targets: []BootstrapTarget{}},
			stepName:  "close",
			wantError: false,
		},
		{
			name: "close_all_existing_allowed",
			plan: &ServicePlan{Targets: []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: "legacy", Type: "go@1", IsExisting: true}},
			}},
			stepName:  "close",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateSkip(tt.plan, tt.stepName)
			if (err != nil) != tt.wantError {
				t.Errorf("validateSkip(): error=%v, wantError=%v", err, tt.wantError)
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
		t.Error("DetailedGuide should be populated from the atom corpus for discover step")
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

func TestBootstrapState_CurrentStepName_3Steps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		step     int
		expected string
	}{
		{"first", 0, "discover"},
		{"middle", 1, "provision"},
		{"last", 2, "close"},
		{"out_of_bounds", 3, ""},
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
		want topology.Mode
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
				{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2", BootstrapMode: "standard", ExplicitStage: "appstage"}},
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
				{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2", BootstrapMode: "standard", ExplicitStage: "appstage"}},
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
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2", BootstrapMode: "standard", ExplicitStage: "appstage"}},
	}}
	resp = bs.BuildResponse("sess1", "test", 0, EnvLocal, nil)
	if resp.Current.PlanMode != "standard" {
		t.Errorf("PlanMode after plan: want standard, got %q", resp.Current.PlanMode)
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

// --- Iteration at provision yields hard-stop (Option A: bootstrap doesn't iterate) ---

func TestBuildResponse_Iteration_YieldsHardStop(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2", BootstrapMode: "standard", ExplicitStage: "appstage"}},
	}}
	bs.Steps[0].Status = stepComplete
	bs.Steps[0].Attestation = "discover complete"
	bs.CurrentStep = 1
	bs.Steps[1].Status = stepInProgress

	resp := bs.BuildResponse("sess-iter", "test", 1, EnvLocal, nil)
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if !strings.Contains(resp.Current.DetailedGuide, "STOP") {
		t.Errorf("iteration>0 should yield hard-stop guide, got:\n%s", resp.Current.DetailedGuide)
	}
}

func TestBuildPriorContext_CompressesOlderSteps(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	// Complete both prior steps with long attestations.
	longAttestation := strings.Repeat("a", 100)
	for i, name := range []string{"discover", "provision"} {
		bs.Steps[i].Status = stepComplete
		bs.Steps[i].Attestation = longAttestation + " " + name
	}
	bs.CurrentStep = 2
	bs.Steps[2].Status = stepInProgress

	ctx := bs.buildPriorContext()
	if ctx == nil {
		t.Fatal("buildPriorContext should not be nil with prior attestations")
	}

	// N-1 (provision, index 1) should be full.
	provAtt := ctx.Attestations["provision"]
	if provAtt != longAttestation+" provision" {
		t.Errorf("N-1 step should have full attestation, got length %d", len(provAtt))
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
}

func TestBuildPriorContext_ShortAttestationNotTruncated(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	shortAtt := "Short attestation"
	for i, name := range []string{"discover", "provision"} {
		bs.Steps[i].Status = stepComplete
		bs.Steps[i].Attestation = shortAtt + " " + name
	}
	bs.CurrentStep = 2
	bs.Steps[2].Status = stepInProgress

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

// --- 3-step Option A structural tests ---

// TestStepDetails_3Steps_OptionA ensures bootstrap steps are the 3 infra-only
// steps (no generate/deploy — those belong to develop).
func TestStepDetails_3Steps_OptionA(t *testing.T) {
	t.Parallel()
	expectedNames := []string{"discover", "provision", "close"}
	if len(stepDetails) != 3 {
		t.Fatalf("stepDetails count: want 3, got %d", len(stepDetails))
	}
	for i, name := range expectedNames {
		if stepDetails[i].Name != name {
			t.Errorf("stepDetails[%d].Name: want %q, got %q", i, name, stepDetails[i].Name)
		}
	}
	// Verify code/deploy steps are gone — owned by develop now.
	for _, removed := range []string{"generate", "deploy", "verify", "strategy"} {
		if lookupDetail(removed).Name != "" {
			t.Errorf("%q step should not exist in Option A stepDetails", removed)
		}
	}
}

// TestValidateSkip covers mandatory vs skippable rules for the 3-step flow.
func TestValidateSkip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		plan      *ServicePlan
		stepName  string
		wantError bool
		wantMsg   string
	}{
		// discover/provision: always mandatory.
		{"discover_always_mandatory", nil, "discover", true, "mandatory"},
		{"provision_always_mandatory", nil, "provision", true, "mandatory"},
		// close: skippable when no runtime targets require registration.
		{"close_nil_plan_allowed", nil, "close", false, ""},
		{"close_empty_targets_allowed", &ServicePlan{Targets: []BootstrapTarget{}}, "close", false, ""},
		{"close_with_targets_blocked", &ServicePlan{Targets: []BootstrapTarget{
			{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
		}}, "close", true, "runtime services"},
		{"close_all_existing_allowed", &ServicePlan{Targets: []BootstrapTarget{
			{Runtime: RuntimeTarget{DevHostname: "legacy", Type: "bun@1.2", IsExisting: true}},
		}}, "close", false, ""},
		// generate/deploy no longer exist in Option A — they are unknown steps.
		{"generate_unknown", nil, "generate", true, "unknown step"},
		{"deploy_unknown", nil, "deploy", true, "unknown step"},
		{"bogus_unknown", nil, "bogus", true, "unknown step"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateSkip(tt.plan, tt.stepName)
			if (err != nil) != tt.wantError {
				t.Errorf("validateSkip(%q): error=%v, wantError=%v", tt.stepName, err, tt.wantError)
			}
			if tt.wantMsg != "" && err != nil {
				if !strings.Contains(err.Error(), tt.wantMsg) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantMsg)
				}
			}
		})
	}
}

// TestCompleteStep_3StepsDeactivates ensures completing all three steps
// deactivates bootstrap.
func TestCompleteStep_3StepsDeactivates(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()

	stepNames := []string{"discover", "provision", "close"}
	for _, name := range stepNames {
		bs.Steps[bs.CurrentStep].Status = stepInProgress
		err := bs.CompleteStep(name, "Attestation for "+name+" completed successfully")
		if err != nil {
			t.Fatalf("CompleteStep(%s): %v", name, err)
		}
	}

	if bs.Active {
		t.Error("expected Active=false after all 3 steps complete")
	}
	if bs.CurrentStep != 3 {
		t.Errorf("CurrentStep: want 3, got %d", bs.CurrentStep)
	}
}
