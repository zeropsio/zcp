package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

func TestParseAtom_FrontmatterParsing(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		content     string
		wantID      string
		wantPhases  []Phase
		wantRunts   []topology.RuntimeClass
		wantModes   []topology.Mode
		wantEnv     []Environment
		wantPrio    int
		wantBodyHas string
		wantErr     bool
	}{
		{
			name: "minimal_valid",
			content: `---
id: test-atom
phases: [develop-active]
---
body content`,
			wantID:      "test-atom",
			wantPhases:  []Phase{PhaseDevelopActive},
			wantPrio:    5,
			wantBodyHas: "body content",
		},
		{
			name: "full_axes",
			content: `---
id: full
priority: 2
phases: [develop-active, develop-closed-auto]
modes: [dev]
environments: [container, local]
runtimes: [dynamic]
title: "Dynamic runtime start"
---
SSH into the service and start the process.`,
			wantID:      "full",
			wantPhases:  []Phase{PhaseDevelopActive, PhaseDevelopClosed},
			wantModes:   []topology.Mode{topology.ModeDev},
			wantEnv:     []Environment{EnvContainer, EnvLocal},
			wantRunts:   []topology.RuntimeClass{topology.RuntimeDynamic},
			wantPrio:    2,
			wantBodyHas: "SSH into the service",
		},
		{
			name: "routes_axis",
			content: `---
id: adopt-only
priority: 3
phases: [bootstrap-active]
routes: [adopt]
---
Adopt flow guidance.`,
			wantID:      "adopt-only",
			wantPhases:  []Phase{PhaseBootstrapActive},
			wantPrio:    3,
			wantBodyHas: "Adopt flow guidance.",
		},
		{
			name: "empty_axes_except_phases",
			content: `---
id: any-mode
phases: [idle]
---
Applies broadly.`,
			wantID:      "any-mode",
			wantPhases:  []Phase{PhaseIdle},
			wantPrio:    5,
			wantBodyHas: "Applies broadly.",
		},
		{
			name: "invalid_priority_defaults",
			content: `---
id: bad-priority
priority: 42
phases: [idle]
---
x`,
			wantID:     "bad-priority",
			wantPhases: []Phase{PhaseIdle},
			wantPrio:   5,
		},
		{
			name: "missing_id",
			content: `---
phases: [idle]
---
body`,
			wantErr: true,
		},
		{
			name: "missing_phases",
			content: `---
id: no-phase
---
body`,
			wantErr: true,
		},
		{
			name: "missing_opening_delimiter",
			content: `id: x
phases: [idle]
---
body`,
			wantErr: true,
		},
		{
			name: "missing_closing_delimiter",
			content: `---
id: x
phases: [idle]
body`,
			wantErr: true,
		},
		{
			name: "malformed_frontmatter_line",
			content: `---
id: x
phases: [idle]
not a valid line
---
body`,
			wantErr: true,
		},
		{
			name: "priority_zero_renders_first",
			content: `---
id: low
priority: 0
phases: [idle]
---
body`,
			wantID:     "low",
			wantPhases: []Phase{PhaseIdle},
			wantPrio:   0,
		},
		{
			name: "priority_negative_defaults",
			content: `---
id: neg
priority: -1
phases: [idle]
---
body`,
			wantID:     "neg",
			wantPhases: []Phase{PhaseIdle},
			wantPrio:   5,
		},
		{
			name: "priority_above_nine_defaults",
			content: `---
id: high
priority: 10
phases: [idle]
---
body`,
			wantID:     "high",
			wantPhases: []Phase{PhaseIdle},
			wantPrio:   5,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			atom, err := ParseAtom(tt.content)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got atom=%+v", atom)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if atom.ID != tt.wantID {
				t.Errorf("ID: want %q got %q", tt.wantID, atom.ID)
			}
			if !equalPhases(atom.Axes.Phases, tt.wantPhases) {
				t.Errorf("Phases: want %v got %v", tt.wantPhases, atom.Axes.Phases)
			}
			if tt.wantModes != nil && !equalModes(atom.Axes.Modes, tt.wantModes) {
				t.Errorf("Modes: want %v got %v", tt.wantModes, atom.Axes.Modes)
			}
			if tt.wantEnv != nil && !equalEnvs(atom.Axes.Environments, tt.wantEnv) {
				t.Errorf("Environments: want %v got %v", tt.wantEnv, atom.Axes.Environments)
			}
			if tt.wantRunts != nil && !equalRuntimes(atom.Axes.Runtimes, tt.wantRunts) {
				t.Errorf("Runtimes: want %v got %v", tt.wantRunts, atom.Axes.Runtimes)
			}
			if atom.Priority != tt.wantPrio {
				t.Errorf("Priority: want %d got %d", tt.wantPrio, atom.Priority)
			}
			if tt.wantBodyHas != "" && !strings.Contains(atom.Body, tt.wantBodyHas) {
				t.Errorf("Body missing %q: %q", tt.wantBodyHas, atom.Body)
			}
		})
	}
}

func equalPhases(a, b []Phase) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalModes(a, b []topology.Mode) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalEnvs(a, b []Environment) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalRuntimes(a, b []topology.RuntimeClass) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestParseAtom_RoutesAxis isolates the routes frontmatter parsing since the
// other cases don't exercise it. Confirms both single-value and multi-value
// route lists round-trip into AxisVector.Routes.
func TestParseAtom_RoutesAxis(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		frontmatter string
		want        []BootstrapRoute
	}{
		{"single", "routes: [recipe]", []BootstrapRoute{BootstrapRouteRecipe}},
		{"multiple", "routes: [classic, adopt]", []BootstrapRoute{BootstrapRouteClassic, BootstrapRouteAdopt}},
		{"absent", "", nil},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := "---\nid: t\nphases: [bootstrap-active]\n"
			if tt.frontmatter != "" {
				content += tt.frontmatter + "\n"
			}
			content += "---\nbody"
			atom, err := ParseAtom(content)
			if err != nil {
				t.Fatalf("ParseAtom: %v", err)
			}
			if len(atom.Axes.Routes) != len(tt.want) {
				t.Fatalf("Routes len = %d, want %d (%v)", len(atom.Axes.Routes), len(tt.want), atom.Axes.Routes)
			}
			for i, w := range tt.want {
				if atom.Axes.Routes[i] != w {
					t.Errorf("Routes[%d] = %q, want %q", i, atom.Axes.Routes[i], w)
				}
			}
		})
	}
}

// TestParseAtom_ReferencesFields exercises the authoring-contract
// frontmatter that lists Go struct fields cited in the atom body
// (pkg.Type.Field form). Validated by TestAtomReferenceFieldIntegrity
// in Phase 2; the parser only needs to round-trip the list.
func TestParseAtom_ReferencesFields(t *testing.T) {
	t.Parallel()

	content := `---
id: test
phases: [develop-active]
references-fields: [ops.DeployResult.Status, ops.DeployResult.SubdomainURL]
---
body`

	atom, err := ParseAtom(content)
	if err != nil {
		t.Fatalf("ParseAtom: %v", err)
	}
	want := []string{"ops.DeployResult.Status", "ops.DeployResult.SubdomainURL"}
	if len(atom.ReferencesFields) != len(want) {
		t.Fatalf("ReferencesFields len = %d, want %d", len(atom.ReferencesFields), len(want))
	}
	for i, w := range want {
		if atom.ReferencesFields[i] != w {
			t.Errorf("ReferencesFields[%d] = %q, want %q", i, atom.ReferencesFields[i], w)
		}
	}
}

// TestParseAtom_ReferencesAtoms exercises atom-to-atom cross-reference
// frontmatter. Validated by TestAtomReferencesAtomsIntegrity in Phase 2;
// the parser only needs to round-trip the list.
func TestParseAtom_ReferencesAtoms(t *testing.T) {
	t.Parallel()

	content := `---
id: test
phases: [develop-active]
references-atoms: [develop-auto-close-semantics, develop-dev-server-reason-codes]
---
body`

	atom, err := ParseAtom(content)
	if err != nil {
		t.Fatalf("ParseAtom: %v", err)
	}
	want := []string{"develop-auto-close-semantics", "develop-dev-server-reason-codes"}
	if len(atom.ReferencesAtoms) != len(want) {
		t.Fatalf("ReferencesAtoms len = %d, want %d", len(atom.ReferencesAtoms), len(want))
	}
	for i, w := range want {
		if atom.ReferencesAtoms[i] != w {
			t.Errorf("ReferencesAtoms[%d] = %q, want %q", i, atom.ReferencesAtoms[i], w)
		}
	}
}

// TestParseAtom_ReferencesFields_InvalidShape asserts the parser rejects
// malformed pkg.Type.Field references early, catching typos before they
// reach TestAtomReferenceFieldIntegrity's AST resolution.
func TestParseAtom_ReferencesFields_InvalidShape(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		entries string
	}{
		{"missing_type", "[ops.Status]"},
		{"package_uppercase", "[Ops.DeployResult.Status]"},
		{"empty_component", "[ops..Status]"},
		{"digit_package", "[0ps.DeployResult.Status]"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := `---
id: test
phases: [develop-active]
references-fields: ` + tt.entries + `
---
body`
			_, err := ParseAtom(content)
			if err == nil {
				t.Fatalf("expected error for entries %q, got nil", tt.entries)
			}
			if !strings.Contains(err.Error(), "references-fields") {
				t.Errorf("error %q should mention references-fields", err.Error())
			}
		})
	}
}

// TestParseAtom_PinnedByScenarios exercises the optional test-anchor
// frontmatter. Informational only — no runtime validation.
func TestParseAtom_PinnedByScenarios(t *testing.T) {
	t.Parallel()

	content := `---
id: test
phases: [develop-active]
pinned-by-scenario: [S7_DevelopClosedAuto, S3_AdoptOnlyUnmanaged]
---
body`

	atom, err := ParseAtom(content)
	if err != nil {
		t.Fatalf("ParseAtom: %v", err)
	}
	want := []string{"S7_DevelopClosedAuto", "S3_AdoptOnlyUnmanaged"}
	if len(atom.PinnedByScenarios) != len(want) {
		t.Fatalf("PinnedByScenarios len = %d, want %d", len(atom.PinnedByScenarios), len(want))
	}
	for i, w := range want {
		if atom.PinnedByScenarios[i] != w {
			t.Errorf("PinnedByScenarios[%d] = %q, want %q", i, atom.PinnedByScenarios[i], w)
		}
	}
}

// TestParseAtom_MultiServiceAxis pins the aggregate-mode opt-in
// (engine ticket E1): atoms declaring `multiService: aggregate` render
// once with `{services-list:TEMPLATE}` directives expanded over matching
// services, instead of duplicating the body per service. Invalid scalar
// values are rejected at parse time.
func TestParseAtom_MultiServiceAxis(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		content     string
		wantMode    MultiServiceMode
		wantErrFrag string
	}{
		{
			name: "aggregate_value_parses",
			content: `---
id: aggregate-atom
phases: [develop-active]
deployStates: [never-deployed]
multiService: aggregate
---
body`,
			wantMode: MultiServiceAggregate,
		},
		{
			name: "default_when_omitted",
			content: `---
id: default-atom
phases: [develop-active]
---
body`,
			wantMode: MultiServicePerService,
		},
		{
			name: "invalid_value_rejected",
			content: `---
id: bad-multi-service
phases: [develop-active]
multiService: peruser
---
body`,
			wantErrFrag: `key "multiService" has invalid value "peruser"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			atom, err := ParseAtom(tc.content)
			if tc.wantErrFrag != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErrFrag)
				}
				if !strings.Contains(err.Error(), tc.wantErrFrag) {
					t.Errorf("error %q missing fragment %q", err.Error(), tc.wantErrFrag)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if atom.Axes.MultiService != tc.wantMode {
				t.Errorf("MultiService = %q, want %q", atom.Axes.MultiService, tc.wantMode)
			}
		})
	}
}

// TestParseAtom_DeployDecompAxes pins parser support for the three
// frontmatter axes introduced by the deploy-strategy decomposition:
// `closeDeployModes`, `gitPushStates`, and `buildIntegrations` (plan
// `plans/deploy-strategy-decomposition-2026-04-28.md` Phase 1.0). Without
// parser support, Phase 8 atom corpus restructure would fail every atom
// that declares these axes — `validAtomFrontmatterKeys` is closed and
// rejects unknown keys at parse time.
//
// Coverage: each axis on its own (positive parse), all three combined,
// each axis with every valid enum value, and invalid-value rejection on
// each axis (the `validAtomEnumValues` enum-set pin).
func TestParseAtom_DeployDecompAxes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		content string
		wantClm []topology.CloseDeployMode
		wantGps []topology.GitPushState
		wantBi  []topology.BuildIntegration
		wantErr bool
		errFrag string
	}{
		{
			name: "closeDeployModes_only",
			content: `---
id: cdm-only
phases: [develop-active]
closeDeployModes: [auto]
---
body`,
			wantClm: []topology.CloseDeployMode{topology.CloseModeAuto},
		},
		{
			name: "gitPushStates_only",
			content: `---
id: gps-only
phases: [develop-active]
gitPushStates: [configured]
---
body`,
			wantGps: []topology.GitPushState{topology.GitPushConfigured},
		},
		{
			name: "buildIntegrations_only",
			content: `---
id: bi-only
phases: [develop-active]
buildIntegrations: [webhook]
---
body`,
			wantBi: []topology.BuildIntegration{topology.BuildIntegrationWebhook},
		},
		{
			name: "all_three_combined",
			content: `---
id: combined
phases: [develop-active]
closeDeployModes: [git-push]
gitPushStates: [configured]
buildIntegrations: [actions]
---
body`,
			wantClm: []topology.CloseDeployMode{topology.CloseModeGitPush},
			wantGps: []topology.GitPushState{topology.GitPushConfigured},
			wantBi:  []topology.BuildIntegration{topology.BuildIntegrationActions},
		},
		{
			name: "closeDeployModes_full_enum",
			content: `---
id: cdm-full
phases: [develop-active]
closeDeployModes: [unset, auto, git-push, manual]
---
body`,
			wantClm: []topology.CloseDeployMode{
				topology.CloseModeUnset,
				topology.CloseModeAuto,
				topology.CloseModeGitPush,
				topology.CloseModeManual,
			},
		},
		{
			name: "gitPushStates_full_enum",
			content: `---
id: gps-full
phases: [develop-active]
gitPushStates: [unconfigured, configured, broken, unknown]
---
body`,
			wantGps: []topology.GitPushState{
				topology.GitPushUnconfigured,
				topology.GitPushConfigured,
				topology.GitPushBroken,
				topology.GitPushUnknown,
			},
		},
		{
			name: "buildIntegrations_full_enum",
			content: `---
id: bi-full
phases: [develop-active]
buildIntegrations: [none, webhook, actions]
---
body`,
			wantBi: []topology.BuildIntegration{
				topology.BuildIntegrationNone,
				topology.BuildIntegrationWebhook,
				topology.BuildIntegrationActions,
			},
		},
		{
			name: "closeDeployModes_invalid_enum",
			content: `---
id: cdm-bad
phases: [develop-active]
closeDeployModes: [auto-close]
---
body`,
			wantErr: true,
			errFrag: `key "closeDeployModes" has invalid value "auto-close"`,
		},
		{
			name: "gitPushStates_invalid_enum",
			content: `---
id: gps-bad
phases: [develop-active]
gitPushStates: [partial]
---
body`,
			wantErr: true,
			errFrag: `key "gitPushStates" has invalid value "partial"`,
		},
		{
			name: "buildIntegrations_invalid_enum",
			content: `---
id: bi-bad
phases: [develop-active]
buildIntegrations: [gitlab]
---
body`,
			wantErr: true,
			errFrag: `key "buildIntegrations" has invalid value "gitlab"`,
		},
		{
			name: "closeDeployModes_bare_scalar_rejected",
			content: `---
id: cdm-scalar
phases: [develop-active]
closeDeployModes: auto
---
body`,
			wantErr: true,
			errFrag: `key "closeDeployModes" must be inline list form`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			atom, err := ParseAtom(tc.content)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.errFrag)
				}
				if !strings.Contains(err.Error(), tc.errFrag) {
					t.Errorf("error %q missing fragment %q", err.Error(), tc.errFrag)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slicesEqual(atom.Axes.CloseDeployModes, tc.wantClm) {
				t.Errorf("CloseDeployModes = %v, want %v", atom.Axes.CloseDeployModes, tc.wantClm)
			}
			if !slicesEqual(atom.Axes.GitPushStates, tc.wantGps) {
				t.Errorf("GitPushStates = %v, want %v", atom.Axes.GitPushStates, tc.wantGps)
			}
			if !slicesEqual(atom.Axes.BuildIntegrations, tc.wantBi) {
				t.Errorf("BuildIntegrations = %v, want %v", atom.Axes.BuildIntegrations, tc.wantBi)
			}
		})
	}
}

// TestParseAtom_ExportStatusAxis pins the new exportStatus: frontmatter
// axis introduced by the atom-corpus-verification plan (Phase 0a). The
// axis is envelope-scoped (matches against StateEnvelope.ExportStatus),
// closed enum of seven values from topology.ExportStatus. Mirrors
// TestParseAtom_DeployDecompAxes — single, multiple, full enum,
// invalid-value rejection, bare-scalar rejection.
func TestParseAtom_ExportStatusAxis(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		content string
		wantES  []topology.ExportStatus
		wantErr bool
		errFrag string
	}{
		{
			name: "single_publish_ready",
			content: `---
id: es-publish
phases: [export-active]
exportStatus: [publish-ready]
---
body`,
			wantES: []topology.ExportStatus{topology.ExportStatusPublishReady},
		},
		{
			name: "multiple_scope_and_variant",
			content: `---
id: es-multi
phases: [export-active]
exportStatus: [scope-prompt, variant-prompt]
---
body`,
			wantES: []topology.ExportStatus{
				topology.ExportStatusScopePrompt,
				topology.ExportStatusVariantPrompt,
			},
		},
		{
			name: "full_enum",
			content: `---
id: es-full
phases: [export-active]
exportStatus: [scope-prompt, variant-prompt, scaffold-required, git-push-setup-required, classify-prompt, validation-failed, publish-ready]
---
body`,
			wantES: []topology.ExportStatus{
				topology.ExportStatusScopePrompt,
				topology.ExportStatusVariantPrompt,
				topology.ExportStatusScaffoldRequired,
				topology.ExportStatusGitPushSetupRequired,
				topology.ExportStatusClassifyPrompt,
				topology.ExportStatusValidationFailed,
				topology.ExportStatusPublishReady,
			},
		},
		{
			name: "absent_axis_yields_nil",
			content: `---
id: es-absent
phases: [export-active]
---
body`,
			wantES: nil,
		},
		{
			name: "invalid_enum_value",
			content: `---
id: es-bad
phases: [export-active]
exportStatus: [publish]
---
body`,
			wantErr: true,
			errFrag: `key "exportStatus" has invalid value "publish"`,
		},
		{
			name: "bare_scalar_rejected",
			content: `---
id: es-scalar
phases: [export-active]
exportStatus: publish-ready
---
body`,
			wantErr: true,
			errFrag: `key "exportStatus" must be inline list form`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			atom, err := ParseAtom(tc.content)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.errFrag)
				}
				if !strings.Contains(err.Error(), tc.errFrag) {
					t.Errorf("error %q missing fragment %q", err.Error(), tc.errFrag)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slicesEqual(atom.Axes.ExportStatuses, tc.wantES) {
				t.Errorf("ExportStatuses = %v, want %v", atom.Axes.ExportStatuses, tc.wantES)
			}
		})
	}
}

// slicesEqual is a small helper for the deploy-decomp axis test —
// reflect.DeepEqual would work but is overkill for typed string slices.
func slicesEqual[T ~string](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestParseAtom_StrictFrontmatter pins Phase 2 (C5) of the pipeline-repair
// plan: the parser rejects malformed frontmatter at parse time instead of
// silently degrading to wildcard-broad atoms. Three classes of failure:
// unknown keys (typos in axis names), non-list values for list-axes (a
// bare scalar where a list is required), and invalid enum values
// (typoed axis values).
func TestParseAtom_StrictFrontmatter(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		content     string
		wantErrFrag string
	}{
		{
			name: "unknown_key",
			content: `---
id: typo-atom
phases: [develop-active]
runtmes: [dynamic]
---
body`,
			wantErrFrag: `unknown atom frontmatter key "runtmes"`,
		},
		{
			name: "bare_scalar_for_list_axis",
			content: `---
id: bare-scalar-atom
phases: [develop-active]
closeDeployModes: auto
---
body`,
			wantErrFrag: `key "closeDeployModes" must be inline list form`,
		},
		{
			name: "invalid_enum_value",
			content: `---
id: bad-enum-atom
phases: [develop-active]
modes: [devmode]
---
body`,
			wantErrFrag: `key "modes" has invalid value "devmode"`,
		},
		{
			name: "invalid_phase_value",
			content: `---
id: bad-phase-atom
phases: [develop]
---
body`,
			wantErrFrag: `key "phases" has invalid value "develop"`,
		},
		{
			name: "valid_minimal_passes",
			content: `---
id: ok-atom
phases: [develop-active]
---
body`,
			wantErrFrag: "",
		},
		{
			name: "valid_full_passes",
			content: `---
id: full-ok-atom
title: Full atom
priority: 3
phases: [develop-active]
modes: [dev, stage]
environments: [container]
closeDeployModes: [auto, git-push]
runtimes: [dynamic]
deployStates: [deployed]
---
body`,
			wantErrFrag: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseAtom(tc.content)
			if tc.wantErrFrag == "" {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErrFrag)
			}
			if !strings.Contains(err.Error(), tc.wantErrFrag) {
				t.Errorf("error %q missing fragment %q", err.Error(), tc.wantErrFrag)
			}
		})
	}
}
