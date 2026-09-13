package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseScenario_Valid_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fixture  string
		wantID   string
		wantSeed SeedMode
	}{
		{"empty_seed", "empty_seed.md", "test-empty", ModeEmpty},
		{"imported_seed", "imported_seed.md", "test-imported", ModeImported},
		{"deployed_seed", "deployed_seed.md", "test-deployed", ModeDeployed},
		{"settled_seed", "settled_seed.md", "test-settled", ModeSettled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join("testdata", "scenarios", tt.fixture)
			sc, err := ParseScenario(path)
			if err != nil {
				t.Fatalf("ParseScenario: %v", err)
			}

			if sc.ID != tt.wantID {
				t.Errorf("ID: got %q, want %q", sc.ID, tt.wantID)
			}
			if sc.Seed != tt.wantSeed {
				t.Errorf("Seed: got %q, want %q", sc.Seed, tt.wantSeed)
			}
			if sc.Prompt == "" {
				t.Error("Prompt: empty")
			}
		})
	}
}

func TestParseScenario_Invalid_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		fixture    string
		wantErrMsg string
	}{
		{"missing_id", "missing_id.md", "id required"},
		{"bad_seed", "bad_seed.md", "invalid seed mode"},
		{"no_frontmatter", "no_frontmatter.md", "missing frontmatter"},
		{"missing_fixture_for_imported", "imported_no_fixture.md", "fixture required"},
		{"empty_prompt", "empty_prompt.md", "prompt body required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join("testdata", "scenarios", tt.fixture)
			_, err := ParseScenario(path)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErrMsg) {
				t.Errorf("error: got %q, want substring %q", err.Error(), tt.wantErrMsg)
			}
		})
	}
}

func TestParseScenario_Prompt_PreservesBody(t *testing.T) {
	t.Parallel()

	path := filepath.Join("testdata", "scenarios", "empty_seed.md")
	sc, err := ParseScenario(path)
	if err != nil {
		t.Fatalf("ParseScenario: %v", err)
	}

	if !strings.Contains(sc.Prompt, "Create a simple web app") {
		t.Errorf("prompt body missing task text, got: %q", sc.Prompt)
	}
	if strings.Contains(sc.Prompt, "---") {
		t.Error("prompt should not contain frontmatter delimiters")
	}
}

// TestParseScenario_UserPersonaAndSim covers the user-sim schema fields:
// userPersona is a free-form string the user-sim simulator runs against, and
// userSim is an optional config block (model override, max iterations, stage
// timeout). Absence of these fields must remain valid — most scenarios run
// with the default persona.
func TestParseScenario_UserPersonaAndSim(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	scPath := filepath.Join(dir, "scenario.md")
	content := `---
id: persona-test
description: persona + user-sim config
seed: empty
userPersona: |
  You are a developer who knows Python but not Zerops.
  Compatible substitutions are fine; mention them in the summary.
userSim:
  model: claude-haiku-4-5-20251001
  maxTurns: 6
  stageTimeoutSeconds: 900
expect:
  mustCallTools:
    - zerops_workflow
---

Set up a Python service.
`
	if err := os.WriteFile(scPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	sc, err := ParseScenario(scPath)
	if err != nil {
		t.Fatalf("ParseScenario: %v", err)
	}
	if !strings.Contains(sc.UserPersona, "Compatible substitutions") {
		t.Errorf("UserPersona: missing expected substring, got %q", sc.UserPersona)
	}
	if sc.UserSim == nil {
		t.Fatal("UserSim: nil, want config block populated")
	}
	if sc.UserSim.Model != "claude-haiku-4-5-20251001" {
		t.Errorf("UserSim.Model: got %q", sc.UserSim.Model)
	}
	if sc.UserSim.MaxTurns != 6 {
		t.Errorf("UserSim.MaxTurns: got %d, want 6", sc.UserSim.MaxTurns)
	}
	if sc.UserSim.StageTimeoutSeconds != 900 {
		t.Errorf("UserSim.StageTimeoutSeconds: got %d, want 900", sc.UserSim.StageTimeoutSeconds)
	}
}

// TestParseScenario_NoUserSim_DefaultsAllowed asserts that a scenario without
// userPersona / userSim still parses cleanly — the runner falls back to the
// default persona and built-in caps.
func TestParseScenario_NoUserSim_DefaultsAllowed(t *testing.T) {
	t.Parallel()
	path := filepath.Join("testdata", "scenarios", "empty_seed.md")
	sc, err := ParseScenario(path)
	if err != nil {
		t.Fatalf("ParseScenario: %v", err)
	}
	if sc.UserPersona != "" {
		t.Errorf("UserPersona: want empty for default fallback, got %q", sc.UserPersona)
	}
	if sc.UserSim != nil {
		t.Errorf("UserSim: want nil for default fallback, got %+v", sc.UserSim)
	}
}

// TestParseScenario_ExcludeFromAll covers the all-run guard frontmatter:
// excludeFromAll marks a scenario as descriptive-tag-only for `behavioral
// all` selection — set true for scenarios that must never run inside a
// routine full-suite sweep (e.g. they consume a live one-shot credential),
// while direct execution by scenario id stays allowed regardless.
func TestParseScenario_ExcludeFromAll(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		front string
		want  bool
	}{
		{"explicit true", "excludeFromAll: true\n", true},
		{"explicit false", "excludeFromAll: false\n", false},
		{"absent defaults false", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			scPath := filepath.Join(dir, "scenario.md")
			content := "---\nid: exclude-from-all-test\ndescription: test\nseed: empty\n" + tt.front + "---\n\nDo the thing.\n"
			if err := os.WriteFile(scPath, []byte(content), 0o600); err != nil {
				t.Fatalf("write scenario: %v", err)
			}
			sc, err := ParseScenario(scPath)
			if err != nil {
				t.Fatalf("ParseScenario: %v", err)
			}
			if sc.ExcludeFromAll != tt.want {
				t.Errorf("ExcludeFromAll: got %v, want %v", sc.ExcludeFromAll, tt.want)
			}
		})
	}
}

// TestParseScenario_PreseedScript covers the state-detection scenarios that
// need local state pre-populated after init wipes the workdir. The frontmatter
// field resolves relative to the scenario file so authors don't have to
// duplicate path prefixes.
func TestParseScenario_PreseedScript(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	scPath := filepath.Join(dir, "scenario.md")
	content := `---
id: preseed-test
description: preseed frontmatter check
seed: empty
preseedScript: scripts/seed-state.sh
expect:
  mustCallTools:
    - zerops_workflow
---

Run the thing.
`
	if err := os.WriteFile(scPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	sc, err := ParseScenario(scPath)
	if err != nil {
		t.Fatalf("ParseScenario: %v", err)
	}
	if sc.PreseedScript != "scripts/seed-state.sh" {
		t.Errorf("PreseedScript: got %q, want scripts/seed-state.sh", sc.PreseedScript)
	}
}

// TestScenario_RequiredWithoutChecks_RejectedBeforeMutation pins
// docs/spec-testing-architecture.md §10.1: mode=required with no executable
// check is a parse error, mode=bogus is rejected, and — driven through the
// runner — the rejection happens before any platform call or cleanup.
func TestScenario_RequiredWithoutChecks_RejectedBeforeMutation(t *testing.T) { // non-parallel: process environment
	dir := t.TempDir()
	noChecks := `---
id: required-no-checks
seed: empty
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
---
Do the thing.
`
	path := filepath.Join(dir, "no-checks.md")
	if err := os.WriteFile(path, []byte(noChecks), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseScenario(path); err == nil {
		t.Fatal("expected parse error for required mode with no executable check")
	} else if !strings.Contains(err.Error(), "executable check") {
		t.Errorf("error should mention executable check, got: %v", err)
	}

	bogus := `---
id: bogus-mode
seed: empty
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: bogus
  noFailedProcesses: true
---
Do the thing.
`
	bogusPath := filepath.Join(dir, "bogus.md")
	if err := os.WriteFile(bogusPath, []byte(bogus), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseScenario(bogusPath); err == nil {
		t.Fatal("expected parse error for invalid verification.mode")
	} else if !strings.Contains(err.Error(), "verification.mode") {
		t.Errorf("error should mention verification.mode, got: %v", err)
	}
}

// TestScenarioParse_RequiredWithOnlyLiveness_Accepted pins E11: the
// required-mode executable-check set is every gating family
// generateRequiredChecks emits rows for, not just expectedServices/
// noFailedProcesses/nodePostgresRecord — a required scenario declaring only
// liveness (or unchanged, never, artifactPromotion, launchShape,
// noFabricatedSecret) must parse, not reject as "no executable check".
func TestScenarioParse_RequiredWithOnlyLiveness_Accepted(t *testing.T) { // non-parallel: process environment
	dir := t.TempDir()
	content := `---
id: required-liveness-only
seed: empty
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  liveness:
    service: appdev
    marker: team-notes
---
Do the thing.
`
	path := filepath.Join(dir, "liveness-only.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	sc, err := ParseScenario(path)
	if err != nil {
		t.Fatalf("unexpected parse error for required mode with only liveness declared: %v", err)
	}
	if sc.Verification == nil || sc.Verification.Liveness == nil || sc.Verification.Liveness.Service != "appdev" {
		t.Fatalf("expected Liveness to be parsed, got %+v", sc.Verification)
	}
}

// TestScenario_NodePostgresRecord_ParsedAndCountsAsExecutable pins
// docs/spec-testing-architecture.md §10.3: the nodePostgresRecord block
// parses into VerificationConfig, counts as an executable check for
// required mode on its own, and rejects a block missing any hostname.
func TestScenario_NodePostgresRecord_ParsedAndCountsAsExecutable(t *testing.T) { // non-parallel: process environment
	dir := t.TempDir()
	good := `---
id: node-postgres-good
seed: empty
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  nodePostgresRecord:
    stage: appstage
    database: db
    unrelated: other
---
Do the thing.
`
	path := filepath.Join(dir, "good.md")
	if err := os.WriteFile(path, []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	sc, err := ParseScenario(path)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if sc.Verification == nil || sc.Verification.NodePostgresRecord == nil {
		t.Fatal("expected VerificationConfig.NodePostgresRecord to be set")
	}
	npr := sc.Verification.NodePostgresRecord
	if npr.Stage != "appstage" || npr.Database != "db" || npr.Unrelated != "other" {
		t.Errorf("NodePostgresRecord = %+v, want stage=appstage database=db unrelated=other", npr)
	}

	missing := `---
id: node-postgres-missing-hostname
seed: empty
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  nodePostgresRecord:
    stage: appstage
    database: ""
    unrelated: other
---
Do the thing.
`
	missingPath := filepath.Join(dir, "missing.md")
	if err := os.WriteFile(missingPath, []byte(missing), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseScenario(missingPath); err == nil {
		t.Fatal("expected parse error for nodePostgresRecord missing a hostname")
	} else if !strings.Contains(err.Error(), "nodePostgresRecord") {
		t.Errorf("error should mention nodePostgresRecord, got: %v", err)
	}
}

// TestScenarioParse_NodePostgresRecord_UnrelatedOptional_Accepted pins
// docs/spec-testing-architecture.md §10.3: a two-service topology has no
// unrelated host to name, so nodePostgresRecord.unrelated is optional —
// stage and database alone parse and validate cleanly. The standalone
// verification.unchanged field (FM-29) is now O1's replacement for the
// "unrelated unchanged" check when a scenario omits unrelated.
func TestScenarioParse_NodePostgresRecord_UnrelatedOptional_Accepted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	good := `---
id: node-postgres-no-unrelated
seed: empty
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  nodePostgresRecord:
    stage: api
    database: db
---
Do the thing.
`
	path := filepath.Join(dir, "good.md")
	if err := os.WriteFile(path, []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	sc, err := ParseScenario(path)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	npr := sc.Verification.NodePostgresRecord
	if npr == nil || npr.Stage != "api" || npr.Database != "db" || npr.Unrelated != "" {
		t.Errorf("NodePostgresRecord = %+v, want stage=api database=db unrelated=\"\"", npr)
	}
}

func TestScenarioParse_FarmVerificationFields_Accepted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	good := `---
id: farm-verification-fields
seed: empty
verification:
  mode: observe
  spec: spec-workflows.md §4.3
  allowFailed: [api]
  liveness:
    service: appdev
    marker: team-notes
  unchanged: [appstage]
  never:
    - zerops_import{override=true}
    - zerops_delete
  askWhen: [GIT_TOKEN_MISSING]
---
Do the thing.
`
	path := filepath.Join(dir, "good.md")
	if err := os.WriteFile(path, []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	sc, err := ParseScenario(path)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	v := sc.Verification
	if v == nil {
		t.Fatal("expected Verification to be set")
	}
	if v.Spec != "spec-workflows.md §4.3" {
		t.Errorf("Spec = %q", v.Spec)
	}
	if len(v.AllowFailed) != 1 || v.AllowFailed[0] != "api" {
		t.Errorf("AllowFailed = %v", v.AllowFailed)
	}
	if v.Liveness == nil || v.Liveness.Service != "appdev" || v.Liveness.Marker != "team-notes" {
		t.Errorf("Liveness = %+v", v.Liveness)
	}
	if len(v.Unchanged) != 1 || v.Unchanged[0] != "appstage" {
		t.Errorf("Unchanged = %v", v.Unchanged)
	}
	if len(v.Never) != 2 || v.Never[0] != "zerops_import{override=true}" {
		t.Errorf("Never = %v", v.Never)
	}
	if len(v.AskWhen) != 1 || v.AskWhen[0] != "GIT_TOKEN_MISSING" {
		t.Errorf("AskWhen = %v", v.AskWhen)
	}

	reach := `---
id: farm-verification-reach-rejected
seed: empty
verification:
  mode: observe
  reach: [discover, complete]
---
Do the thing.
`
	reachPath := filepath.Join(dir, "reach.md")
	if err := os.WriteFile(reachPath, []byte(reach), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseScenario(reachPath); err == nil {
		t.Fatal("expected parse error for reach field")
	} else if !strings.Contains(err.Error(), "reach") {
		t.Errorf("error should mention reach, got: %v", err)
	}

	badExpr := `---
id: farm-verification-bad-never-expr
seed: empty
verification:
  mode: observe
  never: ["not a valid ( expr"]
---
Do the thing.
`
	badExprPath := filepath.Join(dir, "bad-expr.md")
	if err := os.WriteFile(badExprPath, []byte(badExpr), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseScenario(badExprPath); err == nil {
		t.Fatal("expected parse error for malformed never expression")
	} else if !strings.Contains(err.Error(), "never") {
		t.Errorf("error should mention never, got: %v", err)
	}
}

// TestScenarioParse_SeedScalarAndBlock_BothDecode pins docs/spec-eval-farm.md
// §4.1: `seed:` accepts either the legacy bare scalar (mode only) or the
// block mapping form (mode/fixture/ref/expect), and both decode to the same
// Scenario.Seed/Fixture shape existing callers rely on.
func TestScenarioParse_SeedScalarAndBlock_BothDecode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	scalar := `---
id: seed-scalar
seed: deployed
fixture: fixtures/whatever.yaml
---
Do the thing.
`
	scalarPath := filepath.Join(dir, "scalar.md")
	if err := os.WriteFile(scalarPath, []byte(scalar), 0o600); err != nil {
		t.Fatal(err)
	}
	sc, err := ParseScenario(scalarPath)
	if err != nil {
		t.Fatalf("scalar form: unexpected parse error: %v", err)
	}
	if sc.Seed != ModeDeployed {
		t.Errorf("scalar form: Seed = %q, want %q", sc.Seed, ModeDeployed)
	}
	if sc.Fixture != "fixtures/whatever.yaml" {
		t.Errorf("scalar form: Fixture = %q", sc.Fixture)
	}
	if sc.SeedExpect != nil {
		t.Errorf("scalar form: SeedExpect = %+v, want nil", sc.SeedExpect)
	}

	block := `---
id: seed-block
seed:
  mode: settled
  fixture: fixtures/api-crash-on-start.yaml
  ref: 3f2a9c1
  expect:
    services: [{hostname: api, status: [ACTIVE]}]
    processes: [{service: api, action: stack.build, status: FINISHED}]
    probe: {service: api, cmd: "test -f /var/www/zerops.yaml"}
---
Do the thing.
`
	blockPath := filepath.Join(dir, "block.md")
	if err := os.WriteFile(blockPath, []byte(block), 0o600); err != nil {
		t.Fatal(err)
	}
	sc, err = ParseScenario(blockPath)
	if err != nil {
		t.Fatalf("block form: unexpected parse error: %v", err)
	}
	if sc.Seed != ModeSettled {
		t.Errorf("block form: Seed = %q, want %q", sc.Seed, ModeSettled)
	}
	if sc.Fixture != "fixtures/api-crash-on-start.yaml" {
		t.Errorf("block form: Fixture = %q", sc.Fixture)
	}
	if sc.SeedRef != "3f2a9c1" {
		t.Errorf("block form: SeedRef = %q", sc.SeedRef)
	}
	if sc.SeedExpect == nil {
		t.Fatal("block form: SeedExpect = nil, want populated")
	}
	if len(sc.SeedExpect.Services) != 1 || sc.SeedExpect.Services[0].Hostname != "api" {
		t.Errorf("block form: SeedExpect.Services = %+v", sc.SeedExpect.Services)
	}
	if len(sc.SeedExpect.Processes) != 1 || sc.SeedExpect.Processes[0].Action != "stack.build" {
		t.Errorf("block form: SeedExpect.Processes = %+v", sc.SeedExpect.Processes)
	}
	if sc.SeedExpect.Probe == nil || sc.SeedExpect.Probe.Service != "api" {
		t.Errorf("block form: SeedExpect.Probe = %+v", sc.SeedExpect.Probe)
	}
}

// TestScenarioValidate_SeedBlockSettledWithoutExpect_Rejected pins FM-64: a
// block-form `seed.mode: settled` with no `expect` is a validate() error.
// The legacy bare scalar `seed: settled` stays valid (unchanged behavior).
func TestScenarioValidate_SeedBlockSettledWithoutExpect_Rejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	rejected := `---
id: seed-block-settled-no-expect
seed:
  mode: settled
  fixture: fixtures/whatever.yaml
---
Do the thing.
`
	path := filepath.Join(dir, "rejected.md")
	if err := os.WriteFile(path, []byte(rejected), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseScenario(path); err == nil {
		t.Fatal("expected validate() error for block-form settled without expect")
	} else if !strings.Contains(err.Error(), "seed.expect") {
		t.Errorf("error should mention seed.expect, got: %v", err)
	}

	legacyOK := `---
id: seed-scalar-settled-legacy
seed: settled
fixture: fixtures/whatever.yaml
---
Do the thing.
`
	legacyPath := filepath.Join(dir, "legacy.md")
	if err := os.WriteFile(legacyPath, []byte(legacyOK), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseScenario(legacyPath); err != nil {
		t.Errorf("legacy scalar seed: settled should stay valid without expect, got: %v", err)
	}
}

// TestScenarioValidate_FixtureTopLevelAndSeedFixture_Rejected pins
// docs/spec-eval-farm.md §4.1: declaring fixture at both the top level and
// inside seed.fixture is a validate()-time error, not a silent override.
func TestScenarioValidate_FixtureTopLevelAndSeedFixture_Rejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	content := `---
id: fixture-conflict
seed:
  mode: imported
  fixture: fixtures/from-seed-block.yaml
fixture: fixtures/from-top-level.yaml
---
Do the thing.
`
	path := filepath.Join(dir, "conflict.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseScenario(path); err == nil {
		t.Fatal("expected parse error for fixture declared both at top level and in seed.fixture")
	} else if !strings.Contains(err.Error(), "fixture") {
		t.Errorf("error should mention fixture, got: %v", err)
	}
}

// TestScenarioValidate_NeverAndAllowSameShape_Rejected pins FM-58: an
// `allow` entry naming a shape the file's own `never` list already names is
// a validate() error — allow only lifts the runner-injected default.
func TestScenarioValidate_NeverAndAllowSameShape_Rejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	content := `---
id: allow-same-as-never
seed: empty
verification:
  mode: observe
  never: [zerops_delete]
  allow: [{call: "zerops_delete", reason: "test"}]
---
Do the thing.
`
	path := filepath.Join(dir, "same-shape.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseScenario(path); err == nil {
		t.Fatal("expected validate() error for allow naming a shape the file's own never already names")
	} else if !strings.Contains(err.Error(), "allow") {
		t.Errorf("error should mention allow, got: %v", err)
	}
}

// TestScenarioValidate_AllowWithoutReason_Rejected pins FM-58: an `allow`
// entry with an empty reason is a validate() error.
func TestScenarioValidate_AllowWithoutReason_Rejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	content := `---
id: allow-no-reason
seed: empty
verification:
  mode: observe
  allow: [{call: "zerops_import{override=true}"}]
---
Do the thing.
`
	path := filepath.Join(dir, "no-reason.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseScenario(path); err == nil {
		t.Fatal("expected validate() error for allow entry with no reason")
	} else if !strings.Contains(err.Error(), "reason") {
		t.Errorf("error should mention reason, got: %v", err)
	}
}

// TestScenarioValidate_LaunchShape_ProdProjectXorIDEnv_Rejected pins
// docs/spec-eval-farm.md §4.4 O6/§4.1: verification.launchShape requires
// exactly one of prodProject / prodProjectIdEnv — neither set, and both
// set, are both validate() errors.
func TestScenarioValidate_LaunchShape_ProdProjectXorIDEnv_Rejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	neither := `---
id: launch-shape-neither
seed: empty
verification:
  mode: observe
  launchShape: {}
---
Do the thing.
`
	neitherPath := filepath.Join(dir, "neither.md")
	if err := os.WriteFile(neitherPath, []byte(neither), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseScenario(neitherPath); err == nil {
		t.Fatal("expected validate() error when neither prodProject nor prodProjectIdEnv is set")
	} else if !strings.Contains(err.Error(), "prodProject") {
		t.Errorf("error should mention prodProject/prodProjectIdEnv, got: %v", err)
	}

	both := `---
id: launch-shape-both
seed: empty
verification:
  mode: observe
  launchShape: {prodProject: "zcp-farm-prod", prodProjectIdEnv: ZCP_E2E_EXISTING_PROJECT_ID}
---
Do the thing.
`
	bothPath := filepath.Join(dir, "both.md")
	if err := os.WriteFile(bothPath, []byte(both), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseScenario(bothPath); err == nil {
		t.Fatal("expected validate() error when both prodProject and prodProjectIdEnv are set")
	} else if !strings.Contains(err.Error(), "prodProject") {
		t.Errorf("error should mention prodProject/prodProjectIdEnv, got: %v", err)
	}

	envOnly := `---
id: launch-shape-env-only
seed: empty
verification:
  mode: observe
  launchShape: {prodProjectIdEnv: ZCP_E2E_EXISTING_PROJECT_ID}
---
Do the thing.
`
	envOnlyPath := filepath.Join(dir, "env-only.md")
	if err := os.WriteFile(envOnlyPath, []byte(envOnly), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseScenario(envOnlyPath); err != nil {
		t.Errorf("prodProjectIdEnv alone should be valid, got: %v", err)
	}
}
