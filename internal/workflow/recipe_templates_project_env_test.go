// Tests for: writeProjectSection rendering of plan.ProjectEnvVariables.
//
// These tests lock the output contract the agent workflow depends on:
//   - project.envVariables merges the shared secret line (when present)
//     with the per-env projectEnvVariables map.
//   - Keys are emitted in deterministic (sorted) order so diffs are stable.
//   - When both secret and projectEnvVariables are absent, no envVariables
//     block is emitted at all (no empty block).
//   - Values are emitted verbatim — interpolation markers like
//     ${zeropsSubdomainHost} must reach the generated file unchanged.
//   - Envs 0-1 and 2-5 can carry different projectEnvVariables maps
//     (dev-pair vs single-slot).
package workflow

import (
	"strings"
	"testing"
)

// envVariablesBlock returns the text between "envVariables:" and the next
// top-level key (services:) for the project: section only. Returns "" if no
// project-level envVariables block exists.
func envVariablesBlock(yaml string) string {
	// Find project: block start.
	projectIdx := strings.Index(yaml, "project:")
	if projectIdx == -1 {
		return ""
	}
	// Find "services:" which follows project.
	servicesIdx := strings.Index(yaml[projectIdx:], "\nservices:")
	if servicesIdx == -1 {
		servicesIdx = len(yaml) - projectIdx
	}
	projectBlock := yaml[projectIdx : projectIdx+servicesIdx]

	// Find envVariables: within project block.
	envIdx := strings.Index(projectBlock, "  envVariables:")
	if envIdx == -1 {
		return ""
	}
	// Take everything from envVariables: to end of project block.
	return projectBlock[envIdx:]
}

func TestGenerateEnvImportYAML_ProjectEnvVariables_Env01Shape(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan() // has shared secret APP_KEY
	plan.ProjectEnvVariables = map[string]map[string]string{
		"0": {
			"DEV_API_URL":        "https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app",
			"DEV_FRONTEND_URL":   "https://appdev-${zeropsSubdomainHost}.prg1.zerops.app",
			"STAGE_API_URL":      "https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app",
			"STAGE_FRONTEND_URL": "https://appstage-${zeropsSubdomainHost}.prg1.zerops.app",
		},
	}

	yaml := GenerateEnvImportYAML(plan, 0)
	block := envVariablesBlock(yaml)
	if block == "" {
		t.Fatalf("expected project.envVariables block, got none\nyaml:\n%s", yaml)
	}

	// All four project vars must appear with verbatim values.
	wantLines := []string{
		"    APP_KEY: <@generateRandomString(<32>)>",
		"    DEV_API_URL: https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app",
		"    DEV_FRONTEND_URL: https://appdev-${zeropsSubdomainHost}.prg1.zerops.app",
		"    STAGE_API_URL: https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app",
		"    STAGE_FRONTEND_URL: https://appstage-${zeropsSubdomainHost}.prg1.zerops.app",
	}
	for _, line := range wantLines {
		if !strings.Contains(block, line) {
			t.Errorf("expected line %q in envVariables block\nblock:\n%s", line, block)
		}
	}
}

func TestGenerateEnvImportYAML_ProjectEnvVariables_Env2PlusShape(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()
	plan.ProjectEnvVariables = map[string]map[string]string{
		"2": {
			"STAGE_API_URL":      "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app",
			"STAGE_FRONTEND_URL": "https://app-${zeropsSubdomainHost}.prg1.zerops.app",
		},
	}

	yaml := GenerateEnvImportYAML(plan, 2)
	block := envVariablesBlock(yaml)

	// Env 2-5 must carry STAGE_* only (no DEV_*).
	if !strings.Contains(block, "STAGE_API_URL: https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app") {
		t.Errorf("expected STAGE_API_URL line\nblock:\n%s", block)
	}
	if !strings.Contains(block, "STAGE_FRONTEND_URL: https://app-${zeropsSubdomainHost}.prg1.zerops.app") {
		t.Errorf("expected STAGE_FRONTEND_URL line\nblock:\n%s", block)
	}
	if strings.Contains(block, "DEV_API_URL") {
		t.Errorf("env 2 must NOT carry DEV_* vars\nblock:\n%s", block)
	}
}

func TestGenerateEnvImportYAML_ProjectEnvVariables_SortedKeys(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()
	// Intentionally reverse-ordered input to prove sort.
	plan.ProjectEnvVariables = map[string]map[string]string{
		"2": {
			"ZED_VAR":   "z",
			"ALPHA_VAR": "a",
			"MID_VAR":   "m",
		},
	}

	yaml := GenerateEnvImportYAML(plan, 2)
	block := envVariablesBlock(yaml)

	// Find line order of the three vars. Shared secret APP_KEY comes first.
	alphaIdx := strings.Index(block, "ALPHA_VAR:")
	midIdx := strings.Index(block, "MID_VAR:")
	zedIdx := strings.Index(block, "ZED_VAR:")
	if alphaIdx == -1 || midIdx == -1 || zedIdx == -1 {
		t.Fatalf("missing expected vars\nblock:\n%s", block)
	}
	if alphaIdx >= midIdx || midIdx >= zedIdx {
		t.Errorf("expected sorted order ALPHA < MID < ZED, got offsets %d, %d, %d\nblock:\n%s", alphaIdx, midIdx, zedIdx, block)
	}
}

func TestGenerateEnvImportYAML_ProjectEnvVariables_SharedSecretFirst(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan() // APP_KEY shared secret
	plan.ProjectEnvVariables = map[string]map[string]string{
		"0": {
			"ALPHA_VAR": "a",
		},
	}

	yaml := GenerateEnvImportYAML(plan, 0)
	block := envVariablesBlock(yaml)

	// APP_KEY (shared secret) must come before ALPHA_VAR (plain project env).
	appKeyIdx := strings.Index(block, "APP_KEY:")
	alphaIdx := strings.Index(block, "ALPHA_VAR:")
	if appKeyIdx == -1 {
		t.Fatalf("shared secret APP_KEY missing\nblock:\n%s", block)
	}
	if alphaIdx == -1 {
		t.Fatalf("ALPHA_VAR missing\nblock:\n%s", block)
	}
	if appKeyIdx >= alphaIdx {
		t.Errorf("shared secret must come before project env vars (APP_KEY idx %d, ALPHA idx %d)", appKeyIdx, alphaIdx)
	}
}

func TestGenerateEnvImportYAML_ProjectEnvVariables_EmptyWhenAbsentAndNoSecret(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()
	plan.Research.NeedsAppSecret = false
	plan.Research.AppSecretKey = ""
	plan.ProjectEnvVariables = nil

	yaml := GenerateEnvImportYAML(plan, 0)
	block := envVariablesBlock(yaml)
	if block != "" {
		t.Errorf("expected NO envVariables block when no secret and no projectEnvVars, got:\n%s", block)
	}
}

func TestGenerateEnvImportYAML_ProjectEnvVariables_OnlyProjectVarsNoSecret(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()
	plan.Research.NeedsAppSecret = false
	plan.Research.AppSecretKey = ""
	plan.ProjectEnvVariables = map[string]map[string]string{
		"2": {"STAGE_API_URL": "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app"},
	}

	yaml := GenerateEnvImportYAML(plan, 2)
	block := envVariablesBlock(yaml)
	if block == "" {
		t.Fatalf("expected envVariables block with just project vars\nyaml:\n%s", yaml)
	}
	if !strings.Contains(block, "STAGE_API_URL:") {
		t.Errorf("missing STAGE_API_URL\nblock:\n%s", block)
	}
	if strings.Contains(block, "APP_KEY") {
		t.Errorf("APP_KEY should NOT appear (no secret)\nblock:\n%s", block)
	}
}

// TestGenerateEnvImportYAML_DevSlotMinRam locks the dev-slot memory floor
// at 1 GB. v5 hit OOM on `appdev` (type static with dev nodejs override) at
// 0.25 GB default and had to manually scale before npm install survived.
// The fix is predicate-driven: any target going through writeDevService gets
// the 1 GB profile, independent of service type.
func TestGenerateEnvImportYAML_DevSlotMinRam(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		plan *RecipePlan
	}{
		{"minimal_php", testMinimalPlan()},
		{"showcase_php", testShowcasePlan()},
		{"dual_runtime_static_frontend", testDualRuntimePlan()},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			for _, envIdx := range []int{0, 1} {
				yaml := GenerateEnvImportYAML(c.plan, envIdx)
				// Locate every dev-slot service block and assert its minRam.
				// Dev slots are identifiable by "zeropsSetup: dev".
				lines := strings.Split(yaml, "\n")
				devBlocks := 0
				for i, line := range lines {
					if !strings.Contains(line, "zeropsSetup: dev") {
						continue
					}
					devBlocks++
					// Scan forward to find the verticalAutoscaling block for THIS service.
					// Stops at the next "  - hostname:" (start of next service) or EOF.
					found := false
					for j := i; j < len(lines); j++ {
						if j > i && strings.HasPrefix(lines[j], "  - hostname:") {
							break
						}
						if strings.Contains(lines[j], "minRam: 1") {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("env %d: dev slot at line %d lacks minRam: 1\nyaml:\n%s", envIdx, i, yaml)
					}
				}
				if devBlocks == 0 {
					// Minimal plans may have no runtime dev slot in env 0 if the
					// sole target isn't a runtime type — but our test plans all
					// have at least one runtime target, so this is a sanity check.
					t.Errorf("env %d: expected at least one dev slot in plan %s, found none", envIdx, c.name)
				}
			}
		})
	}
}

// TestGenerateFinalize_Idempotent is the canonical regression guard for v5's
// generate-finalize-wipes-manual-edits bug. Two identical calls must produce
// byte-identical output.
func TestGenerateFinalize_Idempotent(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()
	plan.ProjectEnvVariables = map[string]map[string]string{
		"0": {
			"DEV_API_URL":   "https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app",
			"STAGE_API_URL": "https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app",
		},
		"2": {
			"STAGE_API_URL": "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app",
		},
	}

	first := make(map[int]string, 6)
	second := make(map[int]string, 6)
	for i := range 6 {
		first[i] = GenerateEnvImportYAML(plan, i)
	}
	for i := range 6 {
		second[i] = GenerateEnvImportYAML(plan, i)
	}
	for i := range 6 {
		if first[i] != second[i] {
			t.Errorf("env %d not byte-identical on re-render (non-deterministic output)", i)
		}
	}
}
