package recipe

import (
	"slices"
	"strings"
	"testing"
)

// Run-23 F-21/F-22/F-23 — feature-phase pass split tests. The brief
// composer takes a FeaturePass discriminator; backend pass loads
// contract authoring + worker subscription shape (when applicable);
// frontend pass loads design tokens + Tailwind componentry +
// integration validator (when the plan ships a UI codebase).

// TestBuildFeatureBrief_BackendPass_ExcludesDesignSystem — F-21.
// Design tokens / Tailwind / integration-validator must NOT load on
// the backend pass.
func TestBuildFeatureBrief_BackendPass_ExcludesDesignSystem(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	brief, err := BuildFeatureBrief(plan, FeaturePassBackend)
	if err != nil {
		t.Fatalf("BuildFeatureBrief backend: %v", err)
	}
	if slices.Contains(brief.Parts, "design-tokens") {
		t.Errorf("backend pass unexpectedly loaded design-tokens (Parts=%v)", brief.Parts)
	}
	if strings.Contains(brief.Body, "#00A49A") {
		t.Errorf("backend pass body unexpectedly carries primary-color anchor")
	}
	if slices.Contains(brief.Parts, "briefs/feature/tailwind_componentry.md") {
		t.Errorf("backend pass unexpectedly loaded tailwind atom")
	}
	if slices.Contains(brief.Parts, "briefs/feature/integration_validator.md") {
		t.Errorf("backend pass unexpectedly loaded integration-validator atom")
	}
}

// TestBuildFeatureBrief_FrontendPass_IncludesDesignSystem — F-21.
// Design tokens + Tailwind + integration-validator load on the
// frontend pass when the plan ships a UI codebase.
func TestBuildFeatureBrief_FrontendPass_IncludesDesignSystem(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	brief, err := BuildFeatureBrief(plan, FeaturePassFrontend)
	if err != nil {
		t.Fatalf("BuildFeatureBrief frontend: %v", err)
	}
	if !slices.Contains(brief.Parts, "design-tokens") {
		t.Errorf("frontend pass missing design-tokens (Parts=%v)", brief.Parts)
	}
	if !slices.Contains(brief.Parts, "briefs/feature/tailwind_componentry.md") {
		t.Errorf("frontend pass missing tailwind atom (Parts=%v)", brief.Parts)
	}
	if !slices.Contains(brief.Parts, "briefs/feature/integration_validator.md") {
		t.Errorf("frontend pass missing integration-validator atom (Parts=%v)", brief.Parts)
	}
}

// TestBuildFeatureBrief_BackendPass_LoadsContractAtom — F-22. Contract
// authoring + curl smoke-test atom is backend-only.
func TestBuildFeatureBrief_BackendPass_LoadsContractAtom(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	brief, err := BuildFeatureBrief(plan, FeaturePassBackend)
	if err != nil {
		t.Fatalf("BuildFeatureBrief backend: %v", err)
	}
	if !slices.Contains(brief.Parts, "briefs/feature/contract_authoring.md") {
		t.Errorf("backend pass missing contract_authoring atom (Parts=%v)", brief.Parts)
	}
	for _, anchor := range []string{
		"contract`-kind facts",
		"curl_verification",
		"curl smoke-test",
	} {
		if !strings.Contains(brief.Body, anchor) {
			t.Errorf("backend pass body missing contract-authoring anchor %q", anchor)
		}
	}
}

// TestBuildFeatureBrief_FrontendPass_OmittedAtomsWhenNoFrontendCodebase
// — F-21. API-only / worker-only recipes don't render visible UI;
// design tokens + Tailwind + integration-validator atoms must NOT
// load even on the frontend pass.
func TestBuildFeatureBrief_FrontendPass_OmittedAtomsWhenNoFrontendCodebase(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase-headless",
		Framework: "synth",
		Tier:      "showcase",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
			{Hostname: "worker", Role: RoleWorker, BaseRuntime: "nodejs@22", IsWorker: true},
		},
	}
	brief, err := BuildFeatureBrief(plan, FeaturePassFrontend)
	if err != nil {
		t.Fatalf("BuildFeatureBrief frontend: %v", err)
	}
	if slices.Contains(brief.Parts, "design-tokens") {
		t.Errorf("frontend pass unexpectedly loaded design-tokens for headless plan (Parts=%v)", brief.Parts)
	}
	if slices.Contains(brief.Parts, "briefs/feature/tailwind_componentry.md") {
		t.Errorf("frontend pass unexpectedly loaded tailwind atom for headless plan (Parts=%v)", brief.Parts)
	}
	if slices.Contains(brief.Parts, "briefs/feature/integration_validator.md") {
		t.Errorf("frontend pass unexpectedly loaded integration-validator atom for headless plan (Parts=%v)", brief.Parts)
	}
}

// TestContractAuthoringAtom_TeachesCurlSmokeTest — F-22. The contract
// authoring atom names contract-fact recording + curl smoke-test
// discipline at backend close.
func TestContractAuthoringAtom_TeachesCurlSmokeTest(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/feature/contract_authoring.md")
	if err != nil {
		t.Fatalf("read contract_authoring atom: %v", err)
	}
	for _, anchor := range []string{
		"contract`-kind facts",
		"curl smoke-test",
		"curl_verification",
		"queueGroups",
	} {
		if !strings.Contains(body, anchor) {
			t.Errorf("contract_authoring atom missing anchor %q", anchor)
		}
	}
}

// TestTailwindAtom_GroundsInDesignSystemTokens — F-22. The Tailwind
// atom must point at the design-system theme + name the canonical
// Tailwind utility shapes for the recipe tokens.
func TestTailwindAtom_GroundsInDesignSystemTokens(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/feature/tailwind_componentry.md")
	if err != nil {
		t.Fatalf("read tailwind_componentry atom: %v", err)
	}
	for _, anchor := range []string{
		"zerops://themes/design-system",
		"--zerops-primary",
		"--zerops-radius-card",
		"shadcn",
		"Don't hardcode hex",
	} {
		if !strings.Contains(body, anchor) {
			t.Errorf("tailwind_componentry atom missing anchor %q", anchor)
		}
	}
}

// TestIntegrationValidatorAtom_PinsBrokenContractStressCase — Fix 8b.
// FIX_SPEC.md F-23 open-question 3 stress-tested the cross-codebase
// edit authority bar with a 200-OK + wrong-field-name case (api
// returns `{result}` instead of contracted `{output}`). The atom
// must teach `{result}` vs `{output}` (spec-aligned), not `{result}`
// vs `{items}` (which was a draft slip). The prose must frame this
// as a broken contract (200 OK, wrong field name) so the agent picks
// ACT, not HOLD-as-ergonomic.
func TestIntegrationValidatorAtom_PinsBrokenContractStressCase(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/feature/integration_validator.md")
	if err != nil {
		t.Fatalf("read integration_validator atom: %v", err)
	}
	if !strings.Contains(body, "{result:") {
		t.Error("integration_validator atom missing `{result:` token (left side of broken-contract example)")
	}
	if !strings.Contains(body, "{output:") {
		t.Error("integration_validator atom missing `{output:` token (right side; spec-aligned contracted shape)")
	}
	if strings.Contains(body, "{items: [...]}") {
		t.Error("integration_validator atom still references {items:} — Fix 8a should have replaced it with {output:}")
	}
	if !strings.Contains(body, "200 OK, wrong field name") {
		t.Error("integration_validator atom missing the 200-OK-wrong-field-name framing — required so the agent classifies the case as broken-contract, not ergonomic")
	}
}

// TestIntegrationValidatorAtom_TeachesCurlBeforeUI — F-23. The
// integration-validator atom mandates curl-before-UI, the bounded
// cross-codebase edit authority rule, and the redeploy requirement
// after backend edits.
func TestIntegrationValidatorAtom_TeachesCurlBeforeUI(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/feature/integration_validator.md")
	if err != nil {
		t.Fatalf("read integration_validator atom: %v", err)
	}
	for _, anchor := range []string{
		"Curl + browser-walk before claiming UI works",
		"Cross-codebase edit authority",
		"curl FAIL",
		"Redeploy what was changed",
	} {
		if !strings.Contains(body, anchor) {
			t.Errorf("integration_validator atom missing anchor %q", anchor)
		}
	}
}

// TestFactKind_CurlVerification_ValidatesShape — F-22. The
// curl_verification kind requires subject + service + why so the
// frontend pass has a usable contract receipt.
func TestFactKind_CurlVerification_ValidatesShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		fact    FactRecord
		wantErr bool
	}{
		{
			name: "complete record passes",
			fact: FactRecord{
				Topic:   "items-list-curl",
				Kind:    FactKindCurlVerification,
				Subject: "GET /api/items",
				Service: "apistage",
				Why:     "200 OK; response shape {items: [], total: 0, page: 1}",
			},
		},
		{
			name:    "missing subject errors",
			fact:    FactRecord{Topic: "x", Kind: FactKindCurlVerification, Service: "apistage", Why: "ok"},
			wantErr: true,
		},
		{
			name:    "missing service errors",
			fact:    FactRecord{Topic: "x", Kind: FactKindCurlVerification, Subject: "GET /x", Why: "ok"},
			wantErr: true,
		},
		{
			name:    "missing why errors",
			fact:    FactRecord{Topic: "x", Kind: FactKindCurlVerification, Subject: "GET /x", Service: "apistage"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.fact.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestFactKind_BrowserVerification_ValidatesShape — F-23 / Nit 2.
// Mirror of the curl_verification test. browser_verification requires
// subject + service + why so the frontend pass's visual close-out
// signal carries usable evidence into the next session.
func TestFactKind_BrowserVerification_ValidatesShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		fact    FactRecord
		wantErr bool
	}{
		{
			name: "complete record passes",
			fact: FactRecord{
				Topic:   "items-grid-panel",
				Kind:    FactKindBrowserVerification,
				Subject: "items panel",
				Service: "appstage",
				Why:     "20 items render in a grid; pagination shows page 1 of 2",
			},
		},
		{
			name:    "missing subject errors",
			fact:    FactRecord{Topic: "x", Kind: FactKindBrowserVerification, Service: "appstage", Why: "ok"},
			wantErr: true,
		},
		{
			name:    "missing service errors",
			fact:    FactRecord{Topic: "x", Kind: FactKindBrowserVerification, Subject: "panel", Why: "ok"},
			wantErr: true,
		},
		{
			name:    "missing why errors",
			fact:    FactRecord{Topic: "x", Kind: FactKindBrowserVerification, Subject: "panel", Service: "appstage"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.fact.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestBuildFeatureBrief_RejectsEmptyPass — F-21. The composer requires
// pass="backend" or "frontend"; empty pass returns an error rather
// than silently selecting a default.
func TestBuildFeatureBrief_RejectsEmptyPass(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	if _, err := BuildFeatureBrief(plan, FeaturePass("")); err == nil {
		t.Error("expected error for empty pass")
	}
	if _, err := BuildFeatureBrief(plan, FeaturePass("nope")); err == nil {
		t.Error("expected error for unknown pass")
	}
}

// TestBuildFeatureBrief_BackendPass_LoadsWorkerSubscription — F-21
// regression. The worker_subscription_shape atom moved to backend-only
// (worker source is authored at backend; frontend consumes via curl).
func TestBuildFeatureBrief_BackendPass_LoadsWorkerSubscription(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan() // tier=showcase + worker codebase
	backendBrief, err := BuildFeatureBrief(plan, FeaturePassBackend)
	if err != nil {
		t.Fatalf("BuildFeatureBrief backend: %v", err)
	}
	if !slices.Contains(backendBrief.Parts, "briefs/feature/worker_subscription_shape.md") {
		t.Errorf("backend pass missing worker_subscription_shape (Parts=%v)", backendBrief.Parts)
	}

	frontendBrief, err := BuildFeatureBrief(plan, FeaturePassFrontend)
	if err != nil {
		t.Fatalf("BuildFeatureBrief frontend: %v", err)
	}
	if slices.Contains(frontendBrief.Parts, "briefs/feature/worker_subscription_shape.md") {
		t.Errorf("frontend pass unexpectedly loaded worker_subscription_shape (Parts=%v)", frontendBrief.Parts)
	}
}
