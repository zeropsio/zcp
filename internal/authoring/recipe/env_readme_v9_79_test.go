package recipe

import (
	"strings"
	"testing"
)

// v9.79.0 Fix 2 — tier README L1 banner is `# {NAME} — {TIER_LABEL}
// Environment`, L2 is `This is {a/an} {tier_label_lower} environment for
// [{NAME} (info + deploy)](recipe-url) recipe on [Zerops](zerops.io).`
// Match jetstream golden /Users/fxck/www/recipes/laravel-jetstream/
// 3 — Stage/README.md:1-2. Drops the standalone deploy button and the
// back-link line — the CTA lives in the L2 banner sentence link.

// TestAssembleEnvREADME_V9_79_BannerShape pins the new L1 + L2 across
// every tier. Tier 0 ("AI Agent") tests the article ("an") + acronym
// preservation; tier 1 ("Remote (CDE)") tests parenthesized acronym
// preservation; tier 4 ("Small Production") + tier 5 ("Highly-available
// Production") test multi-word lowercase. Tier 3 is the canonical
// Stage smoke.
func TestAssembleEnvREADME_V9_79_BannerShape(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		tierIndex   int
		wantTitleH1 string
		wantBanner  string
	}{
		{
			name:        "tier_0_AI_agent_uses_an_article_and_preserves_AI_acronym",
			tierIndex:   0,
			wantTitleH1: "# NestJS Showcase — AI Agent Environment",
			wantBanner:  "This is an AI agent environment for [NestJS Showcase (info + deploy)](https://app.zerops.io/recipes/nestjs-showcase?environment=ai-agent) recipe on [Zerops](https://zerops.io).",
		},
		{
			name:        "tier_1_Remote_CDE_lowercases_first_word_preserves_CDE_acronym",
			tierIndex:   1,
			wantTitleH1: "# NestJS Showcase — Remote (CDE) Environment",
			wantBanner:  "This is a remote (CDE) environment for [NestJS Showcase (info + deploy)](https://app.zerops.io/recipes/nestjs-showcase?environment=remote-cde) recipe on [Zerops](https://zerops.io).",
		},
		{
			name:        "tier_3_Stage_lowercases_to_stage",
			tierIndex:   3,
			wantTitleH1: "# NestJS Showcase — Stage Environment",
			wantBanner:  "This is a stage environment for [NestJS Showcase (info + deploy)](https://app.zerops.io/recipes/nestjs-showcase?environment=stage) recipe on [Zerops](https://zerops.io).",
		},
		{
			name:        "tier_4_Small_Production_lowercases_both_words",
			tierIndex:   4,
			wantTitleH1: "# NestJS Showcase — Small Production Environment",
			wantBanner:  "This is a small production environment for [NestJS Showcase (info + deploy)](https://app.zerops.io/recipes/nestjs-showcase?environment=small-production) recipe on [Zerops](https://zerops.io).",
		},
		{
			name:        "tier_5_Highly_available_Production_keeps_hyphen_lowercases",
			tierIndex:   5,
			wantTitleH1: "# NestJS Showcase — Highly-available Production Environment",
			wantBanner:  "This is a highly-available production environment for [NestJS Showcase (info + deploy)](https://app.zerops.io/recipes/nestjs-showcase?environment=highly-available-production) recipe on [Zerops](https://zerops.io).",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := &Plan{Slug: "nestjs-showcase", Framework: "nestjs"}
			body, _, err := AssembleEnvREADME(plan, tc.tierIndex)
			if err != nil {
				t.Fatalf("AssembleEnvREADME: %v", err)
			}
			lines := strings.Split(body, "\n")
			if len(lines) < 2 {
				t.Fatalf("rendered body has %d lines, want >= 2", len(lines))
			}
			if lines[0] != tc.wantTitleH1 {
				t.Errorf("L1 = %q, want %q", lines[0], tc.wantTitleH1)
			}
			if lines[1] != tc.wantBanner {
				t.Errorf("L2 = %q, want %q", lines[1], tc.wantBanner)
			}
		})
	}
}

// TestAssembleEnvREADME_V9_79_NoStandaloneDeployButton — jetstream
// tier README has NO standalone deploy button; the CTA lives in the
// L2 banner sentence link. Run-37 had a button at the bottom; v9.79.0
// drops it.
func TestAssembleEnvREADME_V9_79_NoStandaloneDeployButton(t *testing.T) {
	t.Parallel()
	plan := &Plan{Slug: "nestjs-showcase", Framework: "nestjs"}
	body, _, err := AssembleEnvREADME(plan, 3)
	if err != nil {
		t.Fatalf("AssembleEnvREADME: %v", err)
	}
	if strings.Contains(body, "[![Deploy on Zerops]") {
		t.Errorf("rendered body still contains a standalone deploy button:\n%s", body)
	}
}

// TestAssembleEnvREADME_V9_79_NoBackLink — the back-link line at L3
// is replaced by the L2 banner sentence; assert it's gone.
func TestAssembleEnvREADME_V9_79_NoBackLink(t *testing.T) {
	t.Parallel()
	plan := &Plan{Slug: "nestjs-showcase", Framework: "nestjs"}
	body, _, err := AssembleEnvREADME(plan, 3)
	if err != nil {
		t.Fatalf("AssembleEnvREADME: %v", err)
	}
	if strings.Contains(body, "← back to recipe root") {
		t.Errorf("rendered body still contains the back-link:\n%s", body)
	}
}

// TestTierLabelLower pins the sentence-form helper across all 6 tiers.
func TestTierLabelLower(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"AI Agent":                    "AI agent",
		"Remote (CDE)":                "remote (CDE)",
		"Local":                       "local",
		"Stage":                       "stage",
		"Small Production":            "small production",
		"Highly-available Production": "highly-available production",
	}
	for in, want := range cases {
		if got := tierLabelLower(in); got != want {
			t.Errorf("tierLabelLower(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestTierArticle pins the article helper across all 6 tier labels.
func TestTierArticle(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"AI Agent":                    "an",
		"Remote (CDE)":                "a",
		"Local":                       "a",
		"Stage":                       "a",
		"Small Production":            "a",
		"Highly-available Production": "a",
	}
	for in, want := range cases {
		if got := tierArticle(in); got != want {
			t.Errorf("tierArticle(%q) = %q, want %q", in, got, want)
		}
	}
}
