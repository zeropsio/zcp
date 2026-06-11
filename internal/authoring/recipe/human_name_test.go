package recipe

import (
	"strings"
	"testing"
)

// TestHumanName_DerivesFromSlugAndFramework — pins the V3-axis fix:
// rendered titles must NOT carry the raw slug-stem (`# nestjs-showcase
// — api`); they carry the human name derived from slug + framework
// (`# Zerops x NestJS Showcase API`). Lookup table covers the framework
// segment; remaining segments title-case.
func TestHumanName_DerivesFromSlugAndFramework(t *testing.T) {
	t.Parallel()
	cases := []struct {
		slug, framework, name, want string
	}{
		{"nestjs-showcase", "nestjs", "", "NestJS Showcase"},
		{"laravel-jetstream", "laravel", "", "Laravel Jetstream"},
		{"nextjs-blog", "nextjs", "", "Next.js Blog"},
		{"myapp-foo", "", "", "Myapp Foo"},
		// Plan.Name overrides derivation when the slug-derived form is wrong
		{"weird-slug", "", "Custom Brand Name", "Custom Brand Name"},
		// Empty slug returns empty
		{"", "", "", ""},
	}
	for _, tc := range cases {
		plan := &Plan{Slug: tc.slug, Framework: tc.framework, Name: tc.name}
		got := HumanName(plan)
		if got != tc.want {
			t.Errorf("HumanName({slug=%q, framework=%q, name=%q}) = %q; want %q",
				tc.slug, tc.framework, tc.name, got, tc.want)
		}
	}
}

// TestCodebaseRoleLabel_MultiCodebaseSuffix — multi-codebase recipes
// suffix the rendered title with " API"/" Frontend"/" Worker"/" Monolith"
// based on Codebase.Role. Single-codebase recipes return "" (jetstream-
// shape, no suffix).
func TestCodebaseRoleLabel_MultiCodebaseSuffix(t *testing.T) {
	t.Parallel()
	multi := &Plan{
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI},
			{Hostname: "app", Role: RoleFrontend},
			{Hostname: "worker", Role: RoleWorker},
		},
	}
	cases := []struct {
		hostname, want string
	}{
		{"api", " API"},
		{"app", " Frontend"},
		{"worker", " Worker"},
		{"unknown", ""},
	}
	for _, tc := range cases {
		got := CodebaseRoleLabel(multi, tc.hostname)
		if got != tc.want {
			t.Errorf("CodebaseRoleLabel(multi, %q) = %q; want %q", tc.hostname, got, tc.want)
		}
	}
	// Single-codebase recipes get NO suffix even when role is set.
	single := &Plan{
		Codebases: []Codebase{
			{Hostname: "app", Role: RoleMonolith},
		},
	}
	if got := CodebaseRoleLabel(single, "app"); got != "" {
		t.Errorf("CodebaseRoleLabel(single, app) = %q; want \"\" (no suffix on single-codebase)", got)
	}
}

// TestAssembleCodebaseREADME_V3TitleShape — pins the rendered codebase
// README title across every multi-codebase role: api/app/worker each
// get their human name + role suffix; no raw slug-stem leaks. The
// content between the markers is engine-emitted (HumanName +
// CodebaseRoleLabel), not agent-authored.
func TestAssembleCodebaseREADME_V3TitleShape(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	plan.Slug = "nestjs-showcase"
	plan.Framework = "nestjs"
	plan.Codebases = []Codebase{
		{Hostname: "api", Role: RoleAPI, SourceRoot: ""},
		{Hostname: "app", Role: RoleFrontend, SourceRoot: ""},
		{Hostname: "worker", Role: RoleWorker, SourceRoot: ""},
	}
	cases := []struct {
		host      string
		wantTitle string
	}{
		{"api", "# Zerops x NestJS Showcase API"},
		{"app", "# Zerops x NestJS Showcase Frontend"},
		{"worker", "# Zerops x NestJS Showcase Worker"},
	}
	for _, tc := range cases {
		out, _, err := AssembleCodebaseREADME(plan, tc.host)
		if err != nil {
			t.Fatalf("AssembleCodebaseREADME(%s): %v", tc.host, err)
		}
		titleLine := strings.SplitN(out, "\n", 2)[0]
		if titleLine != tc.wantTitle {
			t.Errorf("%s title not rendered with V3 shape\nwant: %q\ngot:  %q", tc.host, tc.wantTitle, titleLine)
		}
		if strings.Contains(titleLine, "nestjs-showcase") {
			t.Errorf("%s V3 leak — slug-stem in title line: %q", tc.host, titleLine)
		}
	}
}

// TestAssembleRootREADME_V3TitleShape — root recipe README title is
// "<NAME> Recipe" (jetstream-shape: `# Laravel Jetstream Recipe`).
func TestAssembleRootREADME_V3TitleShape(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	plan.Slug = "nestjs-showcase"
	plan.Framework = "nestjs"
	out, _, err := AssembleRootREADME(plan)
	if err != nil {
		t.Fatalf("AssembleRootREADME: %v", err)
	}
	if !strings.HasPrefix(out, "# NestJS Showcase Recipe\n") {
		titleLine := strings.SplitN(out, "\n", 2)[0]
		t.Errorf("root title not rendered with V3 shape; got first line: %q", titleLine)
	}
}

// TestAssembleRootREADME_LifecycleFramingLine — the root README carries
// a porter-meta one-liner between the cover image and the tier list,
// mirroring the jetstream golden's framing of the 6-tier lifecycle.
// Recipe-agnostic boilerplate, engine-emitted (not agent-authored).
func TestAssembleRootREADME_LifecycleFramingLine(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	plan.Slug = "any-slug"
	plan.Framework = "nestjs"
	out, _, err := AssembleRootREADME(plan)
	if err != nil {
		t.Fatalf("AssembleRootREADME: %v", err)
	}
	wantFraming := "Offered in examples for the whole development lifecycle"
	if !strings.Contains(out, wantFraming) {
		t.Errorf("root README missing porter-meta lifecycle one-liner; want %q in body", wantFraming)
	}
	// The framing line must sit between the cover image and the tier list
	// (jetstream-golden placement). Cover line precedes; tier list follows.
	coverIdx := strings.Index(out, "cover-nestjs.svg")
	framingIdx := strings.Index(out, wantFraming)
	tierIdx := strings.Index(out, "[[info]](environments/")
	if coverIdx < 0 || framingIdx < 0 || tierIdx < 0 {
		t.Fatalf("expected anchors missing: cover=%d framing=%d tier=%d", coverIdx, framingIdx, tierIdx)
	}
	if coverIdx >= framingIdx || framingIdx >= tierIdx {
		t.Errorf("framing line out-of-order; want cover < framing < tier; got cover=%d framing=%d tier=%d",
			coverIdx, framingIdx, tierIdx)
	}
}
