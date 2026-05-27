package workflow

import (
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// Convention table — locked source of truth for what every mode shape
// writes into ServiceMeta.{PrimarySetupName, StageSetupName} at recipe
// bootstrap. Drift in `recipeSetupNamesForTarget` breaks this test
// immediately rather than surfacing later via flow-eval.
func TestRecipeSetupNamesForTarget_AllModes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		mode        topology.Mode
		wantPrimary string
		wantStage   string
	}{
		{"standard pair", topology.PlanModeStandard, "dev", "prod"},
		{"dev singleton", topology.PlanModeDev, "dev", ""},
		{"simple singleton", topology.PlanModeSimple, "prod", ""},
		// LocalStage: writeBootstrapOutputs collapses Hostname==StageHostname
		// for the local-recipe-stripped-dev shape, and SetupNameFor reads
		// StageSetupName when targetHostname==StageHostname. So the
		// canonical write lands on StageSetupName, not PrimarySetupName.
		{"local stage (dev stripped)", topology.PlanModeLocalStage, "", "prod"},
		{"local only (no zerops side)", topology.PlanModeLocalOnly, "", ""},
		// Unknown / undocumented mode: silent ("", "") so a typo in
		// future code doesn't write a poisoned value.
		{"unknown mode falls through to empty", topology.Mode("future-mode"), "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotPrimary, gotStage := recipeSetupNamesForTarget(tt.mode)
			if gotPrimary != tt.wantPrimary {
				t.Errorf("PrimarySetupName: got %q, want %q", gotPrimary, tt.wantPrimary)
			}
			if gotStage != tt.wantStage {
				t.Errorf("StageSetupName: got %q, want %q", gotStage, tt.wantStage)
			}
		})
	}
}

// Belt-and-suspenders happy path: hostname→zeropsSetup map extracted
// from the same recipe yaml shapes the corpus emits today.
func TestExtractZeropsSetupMapFromImportYAML(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want map[string]string
	}{
		{
			name: "standard pair with managed dep",
			body: "services:\n  - hostname: appdev\n    type: nodejs@22\n    zeropsSetup: dev\n  - hostname: appstage\n    type: nodejs@22\n    zeropsSetup: prod\n  - hostname: db\n    type: postgresql@16\n",
			want: map[string]string{"appdev": "dev", "appstage": "prod"},
		},
		{
			name: "simple singleton",
			body: "services:\n  - hostname: app\n    type: php-nginx@8.4\n    zeropsSetup: prod\n",
			want: map[string]string{"app": "prod"},
		},
		{
			name: "stripped local-stage (dev removed)",
			body: "services:\n  - hostname: appstage\n    type: nodejs@22\n    zeropsSetup: prod\n",
			want: map[string]string{"appstage": "prod"},
		},
		{
			name: "managed deps only — empty map",
			body: "services:\n  - hostname: db\n    type: postgresql@16\n",
			want: map[string]string{},
		},
		{
			name: "absent services key — nil",
			body: "project:\n  name: x\n",
			want: nil,
		},
		{
			name: "malformed yaml — nil",
			body: "services: [unclosed",
			want: nil,
		},
		{
			name: "empty body — nil",
			body: "",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractZeropsSetupMapFromImportYAML(tt.body)
			if !mapsEqual(got, tt.want) {
				t.Errorf("extractZeropsSetupMapFromImportYAML: got %v, want %v", got, tt.want)
			}
		})
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// Gate R end-to-end: feed writeBootstrapOutputs a recipe-route plan +
// matching ImportYAML; assert the meta on disk reads the conventional
// setup name through SetupNameFor for each in-scope hostname.
//
// Direct call into writeBootstrapOutputs (not via BootstrapStart →
// BootstrapComplete) so the test can inject a synthetic RecipeMatch
// without going through corpus resolution.
func TestWriteBootstrapOutputs_RecipeRoute_WritesConventionalSetupNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                   string
		env                    Environment
		runtime                RuntimeTarget
		recipeImportYAML       string
		readByHostname         string
		wantSetupNameForRead   string
		secondaryReadHostname  string
		secondaryWantSetupName string
	}{
		{
			name: "standard pair — dev half reads dev, stage half reads prod",
			env:  EnvContainer,
			runtime: RuntimeTarget{
				DevHostname: "appdev", Type: "nodejs@22",
				BootstrapMode: topology.PlanModeStandard, ExplicitStage: "appstage",
			},
			recipeImportYAML:       "services:\n  - hostname: appdev\n    type: nodejs@22\n    zeropsSetup: dev\n  - hostname: appstage\n    type: nodejs@22\n    zeropsSetup: prod\n",
			readByHostname:         "appdev",
			wantSetupNameForRead:   "dev",
			secondaryReadHostname:  "appstage",
			secondaryWantSetupName: "prod",
		},
		{
			name: "dev singleton — reads dev",
			env:  EnvContainer,
			runtime: RuntimeTarget{
				DevHostname: "appdev", Type: "nodejs@22",
				BootstrapMode: topology.PlanModeDev,
			},
			recipeImportYAML:     "services:\n  - hostname: appdev\n    type: nodejs@22\n    zeropsSetup: dev\n",
			readByHostname:       "appdev",
			wantSetupNameForRead: "dev",
		},
		{
			name: "simple singleton — reads prod",
			env:  EnvContainer,
			runtime: RuntimeTarget{
				DevHostname: "app", Type: "php-nginx@8.4",
				BootstrapMode: topology.PlanModeSimple,
			},
			recipeImportYAML:     "services:\n  - hostname: app\n    type: php-nginx@8.4\n    zeropsSetup: prod\n",
			readByHostname:       "app",
			wantSetupNameForRead: "prod",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			eng := NewEngine(dir, tt.env, nil)

			state := &WorkflowState{
				SessionID: "sess-test",
				Intent:    "recipe convention test",
				Bootstrap: &BootstrapState{
					Active: true,
					Plan: &ServicePlan{
						Targets: []BootstrapTarget{{Runtime: tt.runtime}},
					},
					Route:       BootstrapRouteRecipe,
					RecipeMatch: &RecipeMatch{ImportYAML: tt.recipeImportYAML},
				},
			}

			eng.writeBootstrapOutputs(state)

			meta, err := ReadServiceMeta(dir, tt.readByHostname)
			if err != nil || meta == nil {
				t.Fatalf("ReadServiceMeta(%s): meta=%v err=%v", tt.readByHostname, meta, err)
			}
			if got := meta.SetupNameFor(tt.readByHostname); got != tt.wantSetupNameForRead {
				t.Errorf("SetupNameFor(%s): got %q, want %q (primary=%q stage=%q)",
					tt.readByHostname, got, tt.wantSetupNameForRead,
					meta.PrimarySetupName, meta.StageSetupName)
			}
			if tt.secondaryReadHostname != "" {
				if got := meta.SetupNameFor(tt.secondaryReadHostname); got != tt.secondaryWantSetupName {
					t.Errorf("SetupNameFor(%s): got %q, want %q",
						tt.secondaryReadHostname, got, tt.secondaryWantSetupName)
				}
			}
		})
	}
}

// LocalStage carve-out: dev hostname empty + stage hostname set + env=Local
// triggers the writeBootstrapOutputs collapse. The conventional StageSetupName
// must land on the resulting meta + read back as "prod" via SetupNameFor.
func TestWriteBootstrapOutputs_RecipeRoute_LocalStageCarveOut(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	// DevHostname empty after recipe localize stripped it; stage remains.
	state := &WorkflowState{
		SessionID: "sess-local-stage",
		Intent:    "local recipe with stripped dev",
		Bootstrap: &BootstrapState{
			Active: true,
			Plan: &ServicePlan{
				Targets: []BootstrapTarget{{
					Runtime: RuntimeTarget{
						DevHostname:   "",
						Type:          "nodejs@22",
						BootstrapMode: topology.PlanModeStandard,
						ExplicitStage: "appstage",
					},
				}},
			},
			Route: BootstrapRouteRecipe,
			RecipeMatch: &RecipeMatch{
				ImportYAML: "services:\n  - hostname: appstage\n    type: nodejs@22\n    zeropsSetup: prod\n",
			},
		},
	}

	eng.writeBootstrapOutputs(state)

	meta, err := ReadServiceMeta(dir, "appstage")
	if err != nil || meta == nil {
		t.Fatalf("ReadServiceMeta(appstage): meta=%v err=%v", meta, err)
	}
	if meta.Mode != topology.PlanModeLocalStage {
		t.Errorf("Mode: got %q, want PlanModeLocalStage (carve-out)", meta.Mode)
	}
	if got := meta.SetupNameFor("appstage"); got != "prod" {
		t.Errorf("SetupNameFor(appstage): got %q, want prod (primary=%q stage=%q)",
			got, meta.PrimarySetupName, meta.StageSetupName)
	}
}

// Non-recipe routes (classic, adopt) must leave setup names empty so
// the cascade can discover on first deploy (Gate B / Gate A).
func TestWriteBootstrapOutputs_NonRecipeRoute_LeavesSetupNamesEmpty(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		route BootstrapRoute
	}{
		{"classic route", BootstrapRouteClassic},
		{"adopt route", BootstrapRouteAdopt},
		{"empty / unset route", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			eng := NewEngine(dir, EnvContainer, nil)

			state := &WorkflowState{
				SessionID: "sess-" + string(tt.route),
				Intent:    "non-recipe route",
				Bootstrap: &BootstrapState{
					Active: true,
					Plan: &ServicePlan{
						Targets: []BootstrapTarget{{
							Runtime: RuntimeTarget{
								DevHostname: "appdev", Type: "nodejs@22",
								BootstrapMode: topology.PlanModeStandard, ExplicitStage: "appstage",
							},
						}},
					},
					Route: tt.route,
				},
			}

			eng.writeBootstrapOutputs(state)

			meta, err := ReadServiceMeta(dir, "appdev")
			if err != nil || meta == nil {
				t.Fatalf("ReadServiceMeta: meta=%v err=%v", meta, err)
			}
			if meta.PrimarySetupName != "" {
				t.Errorf("%s must leave PrimarySetupName empty; got %q",
					tt.route, meta.PrimarySetupName)
			}
			if meta.StageSetupName != "" {
				t.Errorf("%s must leave StageSetupName empty; got %q",
					tt.route, meta.StageSetupName)
			}
		})
	}
}

// IsExisting + recipe route: the Gate R write is gated OFF (the !IsExisting
// predicate). Existing meta with non-empty PrimarySetupName flows through
// mergeExistingMeta and lands on disk unchanged.
func TestWriteBootstrapOutputs_RecipeRoute_IsExisting_PreservesExistingSetupName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	existing := &ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeDev,
		BootstrapSession: "earlier",
		BootstrappedAt:   "2026-02-01",
		PrimarySetupName: "custom-setup",
	}
	if err := WriteServiceMeta(dir, existing); err != nil {
		t.Fatalf("seed: %v", err)
	}

	eng := NewEngine(dir, EnvContainer, nil)
	state := &WorkflowState{
		SessionID: "sess-expand",
		Intent:    "expand custom-setup to standard",
		Bootstrap: &BootstrapState{
			Active: true,
			Plan: &ServicePlan{
				Targets: []BootstrapTarget{{
					Runtime: RuntimeTarget{
						DevHostname: "appdev", Type: "nodejs@22",
						IsExisting:    true,
						BootstrapMode: topology.PlanModeStandard, ExplicitStage: "appstage",
					},
				}},
			},
			Route: BootstrapRouteRecipe,
			RecipeMatch: &RecipeMatch{
				ImportYAML: "services:\n  - hostname: appdev\n    type: nodejs@22\n    zeropsSetup: dev\n  - hostname: appstage\n    type: nodejs@22\n    zeropsSetup: prod\n",
			},
		},
	}

	eng.writeBootstrapOutputs(state)

	meta, err := ReadServiceMeta(dir, "appdev")
	if err != nil || meta == nil {
		t.Fatalf("ReadServiceMeta: meta=%v err=%v", meta, err)
	}
	if meta.PrimarySetupName != "custom-setup" {
		t.Errorf("PrimarySetupName: got %q, want custom-setup (existing must win)",
			meta.PrimarySetupName)
	}
}

// mergeExistingMeta direct unit: a fresh meta with conventional values
// + existing meta with EMPTY setup names → conventional values survive
// (migrate-forward-empty). Existing pre-P0 metas that re-bootstrap pick
// up the canonical write this way.
func TestMergeExistingMeta_SetupNamesMigrateForwardEmpty(t *testing.T) {
	t.Parallel()

	fresh := &ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		PrimarySetupName: "dev",
		StageSetupName:   "prod",
	}
	existing := &ServiceMeta{
		Hostname:       "appdev",
		Mode:           topology.PlanModeDev,
		BootstrappedAt: "2026-01-15",
		// Setup names empty (pre-P0 meta shape).
	}

	mergeExistingMeta(fresh, existing)

	if fresh.PrimarySetupName != "dev" {
		t.Errorf("PrimarySetupName: got %q, want dev (migrate-forward-empty)",
			fresh.PrimarySetupName)
	}
	if fresh.StageSetupName != "prod" {
		t.Errorf("StageSetupName: got %q, want prod (migrate-forward-empty)",
			fresh.StageSetupName)
	}
}

// mergeExistingMeta — non-empty existing wins over fresh.
func TestMergeExistingMeta_SetupNamesNonEmptyPreserves(t *testing.T) {
	t.Parallel()

	fresh := &ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		PrimarySetupName: "dev",
		StageSetupName:   "prod",
	}
	existing := &ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeDev,
		BootstrappedAt:   "2026-01-15",
		PrimarySetupName: "user-custom-dev",
		StageSetupName:   "user-custom-stage",
	}

	mergeExistingMeta(fresh, existing)

	if fresh.PrimarySetupName != "user-custom-dev" {
		t.Errorf("PrimarySetupName: got %q, want user-custom-dev (existing wins)",
			fresh.PrimarySetupName)
	}
	if fresh.StageSetupName != "user-custom-stage" {
		t.Errorf("StageSetupName: got %q, want user-custom-stage (existing wins)",
			fresh.StageSetupName)
	}
}

// writeProvisionMetas mirrors the writeBootstrapOutputs gate. A crash
// between provision and close must leave the canonical setup names
// already on disk — recovery doesn't re-derive.
func TestWriteProvisionMetas_RecipeRoute_WritesConventionalSetupNames(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvContainer, nil)

	state := &WorkflowState{
		SessionID: "sess-provision-test",
		Intent:    "provision crash test",
		Bootstrap: &BootstrapState{
			Active: true,
			Plan: &ServicePlan{
				Targets: []BootstrapTarget{{
					Runtime: RuntimeTarget{
						DevHostname: "appdev", Type: "nodejs@22",
						BootstrapMode: topology.PlanModeStandard, ExplicitStage: "appstage",
					},
				}},
			},
			Route: BootstrapRouteRecipe,
			RecipeMatch: &RecipeMatch{
				ImportYAML: "services:\n  - hostname: appdev\n    type: nodejs@22\n    zeropsSetup: dev\n  - hostname: appstage\n    type: nodejs@22\n    zeropsSetup: prod\n",
			},
		},
	}

	eng.writeProvisionMetas(state)

	meta, err := ReadServiceMeta(dir, "appdev")
	if err != nil || meta == nil {
		t.Fatalf("ReadServiceMeta: meta=%v err=%v", meta, err)
	}
	if meta.PrimarySetupName != "dev" {
		t.Errorf("PrimarySetupName: got %q, want dev (provision-time write)",
			meta.PrimarySetupName)
	}
	if meta.StageSetupName != "prod" {
		t.Errorf("StageSetupName: got %q, want prod (provision-time write)",
			meta.StageSetupName)
	}
}

// Convention drift smoke test — pure helper hardening. The function
// is best-effort (stderr log, no fail), so the test verifies it runs to
// completion without panic across edge inputs.
func TestVerifySetupNameConvention_MismatchIsBestEffort(t *testing.T) {
	t.Parallel()
	body := "services:\n  - hostname: appdev\n    type: nodejs@22\n    zeropsSetup: develop\n"
	verifySetupNameConvention(body, "appdev", "", "dev", "")
	verifySetupNameConvention("", "appdev", "appstage", "dev", "prod")
	verifySetupNameConvention("not yaml :{", "appdev", "", "dev", "")
	verifySetupNameConvention(body, "", "", "", "")
}
