// Tests for: workflow bootstrap subflow — 10-step state tracking.
package workflow

import (
	"testing"
)

func TestNewBootstrapState_InitialState(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()

	if !bs.Active {
		t.Error("expected Active to be true")
	}
	if bs.CurrentStep != 0 {
		t.Errorf("CurrentStep: want 0, got %d", bs.CurrentStep)
	}
	if len(bs.Steps) != 10 {
		t.Fatalf("Steps count: want 10, got %d", len(bs.Steps))
	}

	expectedNames := []string{
		"plan", "recipe-search", "generate-import", "import-services",
		"wait-services", "mount-dev", "discover-services", "finalize",
		"spawn-subagents", "aggregate-results",
	}
	for i, name := range expectedNames {
		if bs.Steps[i].Name != name {
			t.Errorf("step[%d].Name: want %s, got %s", i, name, bs.Steps[i].Name)
		}
		if bs.Steps[i].Status != "pending" {
			t.Errorf("step[%d].Status: want pending, got %s", i, bs.Steps[i].Status)
		}
	}
}

func TestBootstrapState_CurrentStepName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		step     int
		expected string
	}{
		{"first_step", 0, "plan"},
		{"middle_step", 4, "wait-services"},
		{"last_step", 9, "aggregate-results"},
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

func TestBootstrapState_CurrentStepName_OutOfBounds(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.CurrentStep = 10 // past last step
	if got := bs.CurrentStepName(); got != "" {
		t.Errorf("CurrentStepName out of bounds: want empty, got %s", got)
	}
}

func TestBootstrapState_MarkStepComplete(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()

	if err := bs.MarkStepComplete("plan"); err != nil {
		t.Fatalf("MarkStepComplete(plan): %v", err)
	}
	if bs.Steps[0].Status != "complete" {
		t.Errorf("step[0].Status: want complete, got %s", bs.Steps[0].Status)
	}
}

func TestBootstrapState_MarkStepComplete_NotFound(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()

	if err := bs.MarkStepComplete("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent step")
	}
}

func TestBootstrapState_AdvanceStep_FullSequence(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()

	expectedSteps := []string{
		"plan", "recipe-search", "generate-import", "import-services",
		"wait-services", "mount-dev", "discover-services", "finalize",
		"spawn-subagents", "aggregate-results",
	}

	for i, expected := range expectedSteps {
		stepName, guidance, done := bs.AdvanceStep()
		if stepName != expected {
			t.Errorf("step %d: want name %s, got %s", i, expected, stepName)
		}
		if guidance == "" {
			t.Errorf("step %d: expected non-empty guidance", i)
		}

		// Mark current step complete to advance.
		if err := bs.MarkStepComplete(stepName); err != nil {
			t.Fatalf("MarkStepComplete(%s): %v", stepName, err)
		}

		// Last step should report done after marking complete.
		if i == len(expectedSteps)-1 {
			// AdvanceStep after completing last step should return done.
			_, _, doneFinal := bs.AdvanceStep()
			if !doneFinal {
				t.Error("expected done=true after completing all steps")
			}
		} else if done {
			t.Errorf("step %d: unexpected done=true", i)
		}
	}
}

func TestBootstrapState_AdvanceStep_StubbedSteps(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()

	// Advance to spawn-subagents (step 8).
	for range 8 {
		name, _, _ := bs.AdvanceStep()
		if err := bs.MarkStepComplete(name); err != nil {
			t.Fatalf("MarkStepComplete(%s): %v", name, err)
		}
	}

	// Step 8: spawn-subagents should have Task tool guidance.
	name, guidance, _ := bs.AdvanceStep()
	if name != "spawn-subagents" {
		t.Fatalf("expected spawn-subagents, got %s", name)
	}
	if guidance == "" {
		t.Error("expected guidance for stubbed step spawn-subagents")
	}

	if err := bs.MarkStepComplete("spawn-subagents"); err != nil {
		t.Fatalf("MarkStepComplete(spawn-subagents): %v", err)
	}

	// Step 9: aggregate-results should also have guidance.
	name, guidance, _ = bs.AdvanceStep()
	if name != "aggregate-results" {
		t.Fatalf("expected aggregate-results, got %s", name)
	}
	if guidance == "" {
		t.Error("expected guidance for stubbed step aggregate-results")
	}
}

func TestBootstrapState_AdvanceStep_AlreadyDone(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Active = false
	bs.CurrentStep = 10

	name, _, done := bs.AdvanceStep()
	if !done {
		t.Error("expected done=true when bootstrap is inactive")
	}
	if name != "" {
		t.Errorf("expected empty step name when done, got %s", name)
	}
}
