package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestValidateFeatureSubagent_DispatchArtifacts covers the v14 mtime-based
// dispatch-artifact floor. v13 demonstrated that an attestation-only gate is
// cosmetic — the main agent typed "single author across all three codebases"
// into the substep completion call without ever invoking an Agent tool, and
// the gate accepted it. This check walks the codebase mounts for source
// files newer than the init-commands baseline and rejects when:
//
//  1. Too few feature files exist (agent never wrote enough)
//  2. The mtime spread is wider than a single Agent tool call would produce
//     (agent inlined the work over many minutes instead of dispatching)
//
// Each table entry sets up a fake mount with a chosen artifact pattern and
// asserts the validator's pass/fail decision. The mount base override is
// scoped per-test so the cases run in parallel without colliding.
func TestValidateFeatureSubagent_DispatchArtifacts(t *testing.T) {
	// recipeMountBaseOverride is a process-wide mutable global, so this
	// test cannot use t.Parallel — sub-tests would race on the override.

	plan := showcaseDispatchPlan()
	baseline := time.Now().Add(-30 * time.Minute)
	baselineISO := baseline.Format(time.RFC3339)
	state := &RecipeState{
		Steps: []RecipeStep{
			{Name: RecipeStepDeploy, SubSteps: []RecipeSubStep{
				{Name: SubStepInitCommands, Status: stepComplete, CompletedAt: baselineISO},
			}},
		},
	}

	const validAttestation = "feature sub-agent wrote items service, controller, dto, NATS module, ItemsPanel, and worker consumer; expanded seed to 20 rows"

	tests := []struct {
		name string
		// setup writes files into mountBase. Each file is given the offset
		// from baseline (positive = after baseline) when it should be mtimed.
		setup      func(mountBase string)
		wantPassed bool
		wantNeedle string // substring expected in the failure issues, when fail
	}{
		{
			name: "burst dispatch passes — 8 files within 30s",
			setup: func(base string) {
				writeFeatureFile(t, base, "apidev/src/items/items.service.ts", baseline.Add(2*time.Minute))
				writeFeatureFile(t, base, "apidev/src/items/items.controller.ts", baseline.Add(2*time.Minute+5*time.Second))
				writeFeatureFile(t, base, "apidev/src/items/dto/create-item.dto.ts", baseline.Add(2*time.Minute+10*time.Second))
				writeFeatureFile(t, base, "apidev/src/items/items.module.ts", baseline.Add(2*time.Minute+12*time.Second))
				writeFeatureFile(t, base, "apidev/src/nats/nats.module.ts", baseline.Add(2*time.Minute+15*time.Second))
				writeFeatureFile(t, base, "appdev/src/lib/ItemsPanel.svelte", baseline.Add(2*time.Minute+20*time.Second))
				writeFeatureFile(t, base, "appdev/src/lib/SearchPanel.svelte", baseline.Add(2*time.Minute+25*time.Second))
				writeFeatureFile(t, base, "workerdev/src/jobs/jobs.controller.ts", baseline.Add(2*time.Minute+30*time.Second))
			},
			wantPassed: true,
		},
		{
			name: "v13 inline pattern fails — files spread 40 min",
			setup: func(base string) {
				// The v13 main-agent inline pattern: feature files dribbled
				// in across debugging rounds. mtime spread = 40 minutes.
				writeFeatureFile(t, base, "apidev/src/items/items.service.ts", baseline.Add(3*time.Minute))
				writeFeatureFile(t, base, "apidev/src/items/items.controller.ts", baseline.Add(8*time.Minute))
				writeFeatureFile(t, base, "apidev/src/items/dto/create-item.dto.ts", baseline.Add(15*time.Minute))
				writeFeatureFile(t, base, "apidev/src/items/items.module.ts", baseline.Add(22*time.Minute))
				writeFeatureFile(t, base, "apidev/src/nats/nats.module.ts", baseline.Add(28*time.Minute))
				writeFeatureFile(t, base, "appdev/src/lib/ItemsPanel.svelte", baseline.Add(35*time.Minute))
				writeFeatureFile(t, base, "appdev/src/lib/SearchPanel.svelte", baseline.Add(40*time.Minute))
				writeFeatureFile(t, base, "workerdev/src/jobs/jobs.controller.ts", baseline.Add(43*time.Minute))
			},
			wantPassed: false,
			wantNeedle: "wall-clock",
		},
		{
			name: "too few files fails — 3 source files",
			setup: func(base string) {
				writeFeatureFile(t, base, "apidev/src/items/items.service.ts", baseline.Add(2*time.Minute))
				writeFeatureFile(t, base, "apidev/src/items/items.controller.ts", baseline.Add(2*time.Minute+10*time.Second))
				writeFeatureFile(t, base, "appdev/src/lib/ItemsPanel.svelte", baseline.Add(2*time.Minute+20*time.Second))
			},
			wantPassed: false,
			wantNeedle: "post-baseline source files",
		},
		{
			name: "no post-baseline files fails — agent never wrote anything",
			setup: func(base string) {
				// One file before the baseline (scaffold output) — must
				// not count toward the floor.
				writeFeatureFile(t, base, "apidev/src/health/health.controller.ts", baseline.Add(-5*time.Minute))
			},
			wantPassed: false,
			wantNeedle: "post-baseline source files",
		},
		{
			name: "node_modules and dist are skipped",
			setup: func(base string) {
				// Dump a bunch of files in node_modules/dist with fresh
				// mtimes — the walker must skip them. Without skip dirs,
				// these would push count over the floor and falsely pass.
				for i := range 20 {
					writeFeatureFile(t, base, filepath.Join("apidev/node_modules/foo", fileName(i)), baseline.Add(2*time.Minute))
					writeFeatureFile(t, base, filepath.Join("apidev/dist", fileName(i)), baseline.Add(2*time.Minute))
				}
				// Real feature files: only 2 — should fail floor.
				writeFeatureFile(t, base, "apidev/src/items/items.service.ts", baseline.Add(2*time.Minute))
				writeFeatureFile(t, base, "appdev/src/lib/ItemsPanel.svelte", baseline.Add(2*time.Minute+5*time.Second))
			},
			wantPassed: false,
			wantNeedle: "post-baseline source files",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			oldBase := recipeMountBaseOverride
			recipeMountBaseOverride = tmp
			defer func() { recipeMountBaseOverride = oldBase }()

			tt.setup(tmp)

			result := validateFeatureSubagent(context.Background(), plan, state, validAttestation)
			if result == nil {
				t.Fatal("validator returned nil")
			}
			if result.Passed != tt.wantPassed {
				t.Errorf("passed=%v want %v; issues=%v", result.Passed, tt.wantPassed, result.Issues)
			}
			if !tt.wantPassed && tt.wantNeedle != "" {
				found := false
				for _, issue := range result.Issues {
					if strings.Contains(issue, tt.wantNeedle) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected issue containing %q, got: %v", tt.wantNeedle, result.Issues)
				}
			}
		})
	}
}

// TestFeatureDispatchBaseline_PrefersTighterTimestamp locks in the baseline
// resolver: when both generate.CompletedAt and deploy.init-commands.CompletedAt
// are set, the later one wins. The init-commands baseline is tighter and
// rules out scaffolding rounds from counting toward feature work.
func TestFeatureDispatchBaseline_PrefersTighterTimestamp(t *testing.T) {
	t.Parallel()
	earlier := time.Now().Add(-2 * time.Hour)
	later := time.Now().Add(-30 * time.Minute)
	state := &RecipeState{
		Steps: []RecipeStep{
			{Name: RecipeStepGenerate, CompletedAt: earlier.Format(time.RFC3339)},
			{Name: RecipeStepDeploy, SubSteps: []RecipeSubStep{
				{Name: SubStepInitCommands, CompletedAt: later.Format(time.RFC3339)},
			}},
		},
	}
	got := featureDispatchBaseline(state)
	if got.IsZero() {
		t.Fatal("expected non-zero baseline")
	}
	// Allow up to 1 second of RFC3339 truncation.
	if got.Before(later.Add(-time.Second)) || got.After(later.Add(time.Second)) {
		t.Errorf("baseline = %v, expected near %v", got, later)
	}
}

// TestFeatureDispatchBaseline_NoStateReturnsZero — when state is nil or has
// neither generate nor init-commands timestamps, the resolver returns zero,
// which validator code treats as "skip the check, can't enforce".
func TestFeatureDispatchBaseline_NoStateReturnsZero(t *testing.T) {
	t.Parallel()
	if !featureDispatchBaseline(nil).IsZero() {
		t.Error("nil state should return zero baseline")
	}
	empty := &RecipeState{Steps: []RecipeStep{
		{Name: RecipeStepGenerate}, // no CompletedAt
		{Name: RecipeStepDeploy},
	}}
	if !featureDispatchBaseline(empty).IsZero() {
		t.Error("state without timestamps should return zero baseline")
	}
}

// showcaseDispatchPlan builds a multi-codebase showcase plan that the
// dispatch walker can iterate. Hostnames match the on-disk layout the test
// fixtures create.
func showcaseDispatchPlan() *RecipePlan {
	return &RecipePlan{
		Tier: RecipeTierShowcase,
		Slug: "test-showcase",
		Targets: []RecipeTarget{
			{Hostname: "api", Type: "nodejs@22"},
			{Hostname: "app", Type: "nodejs@22"},
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true},
		},
	}
}

// writeFeatureFile creates dir + file under mountBase and sets mtime. Used
// to simulate the codebase mounts a sub-agent (or main agent) would have
// written. The path is expected to be a relative POSIX-style joined path
// like "apidev/src/items/items.service.ts".
func writeFeatureFile(t *testing.T, mountBase, relPath string, mtime time.Time) {
	t.Helper()
	full := filepath.Join(mountBase, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte("// stub\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
	if err := os.Chtimes(full, mtime, mtime); err != nil {
		t.Fatalf("chtimes %s: %v", full, err)
	}
}

func fileName(i int) string {
	return "f" + string(rune('a'+(i%26))) + ".js"
}
