package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/knowledge"
)

func TestNeedsMultiBaseGuidance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		plan *RecipePlan
		want bool
	}{
		{
			name: "nil plan",
			plan: nil,
			want: false,
		},
		{
			name: "empty runtime type",
			plan: &RecipePlan{},
			want: false,
		},
		{
			name: "nodejs primary runtime — no multi-base needed",
			plan: &RecipePlan{
				RuntimeType: "nodejs@22",
				Research:    ResearchData{BuildCommands: []string{"npm ci", "npm run build"}},
			},
			want: false,
		},
		{
			name: "bun primary runtime — no multi-base needed",
			plan: &RecipePlan{
				RuntimeType: "bun@1",
				Research:    ResearchData{BuildCommands: []string{"bun install"}},
			},
			want: false,
		},
		{
			name: "deno primary runtime — no multi-base needed",
			plan: &RecipePlan{
				RuntimeType: "deno@2",
				Research:    ResearchData{BuildCommands: []string{"deno cache main.ts"}},
			},
			want: false,
		},
		{
			name: "php-nginx with npm in build commands — triggers",
			plan: &RecipePlan{
				RuntimeType: "php-nginx@8.4",
				Research: ResearchData{BuildCommands: []string{
					"composer install --no-dev",
					"npm ci",
					"npm run build",
				}},
			},
			want: true,
		},
		{
			name: "ruby with yarn in build commands — triggers",
			plan: &RecipePlan{
				RuntimeType: "ruby@3.3",
				Research: ResearchData{BuildCommands: []string{
					"bundle install",
					"yarn install",
					"yarn build",
				}},
			},
			want: true,
		},
		{
			name: "python with pnpm in build commands — triggers",
			plan: &RecipePlan{
				RuntimeType: "python@3.12",
				Research:    ResearchData{BuildCommands: []string{"pip install -r requirements.txt", "pnpm build"}},
			},
			want: true,
		},
		{
			name: "go with only bun command in build — triggers (bun as build tool)",
			plan: &RecipePlan{
				RuntimeType: "go@1",
				Research:    ResearchData{BuildCommands: []string{"go build ./...", "bun run tailwind"}},
			},
			want: true,
		},
		{
			name: "php-nginx pure composer — no JS pipeline, no trigger",
			plan: &RecipePlan{
				RuntimeType: "php-nginx@8.4",
				Research: ResearchData{BuildCommands: []string{
					"composer install --no-dev --optimize-autoloader",
				}},
			},
			want: false,
		},
		{
			name: "go pure build — no trigger",
			plan: &RecipePlan{
				RuntimeType: "go@1",
				Research:    ResearchData{BuildCommands: []string{"go build -o app"}},
			},
			want: false,
		},
		{
			name: "BuildBases with 2+ entries — triggers even without JS in commands",
			plan: &RecipePlan{
				RuntimeType: "php-nginx@8.4",
				BuildBases:  []string{"php@8.4", "nodejs@22"},
				Research:    ResearchData{BuildCommands: []string{"composer install"}},
			},
			want: true,
		},
		{
			name: "BuildBases single entry — no trigger",
			plan: &RecipePlan{
				RuntimeType: "go@1",
				BuildBases:  []string{"go@1"},
				Research:    ResearchData{BuildCommands: []string{"go build"}},
			},
			want: false,
		},
		{
			name: "false-positive guard: 'npm' appearing inside another word should not trigger",
			plan: &RecipePlan{
				RuntimeType: "go@1",
				Research:    ResearchData{BuildCommands: []string{"echo npmguard"}},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := needsMultiBaseGuidance(tt.plan)
			if got != tt.want {
				t.Errorf("needsMultiBaseGuidance() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMultiBaseGuidance_ContainsCoreConcepts(t *testing.T) {
	t.Parallel()
	guide := multiBaseGuidance()

	requiredMarkers := []string{
		"build/run asymmetry",
		"zsc install",
		"run.prepareCommands",
		"startWithoutCode trap",
		"Build container: native multi-base",
		"Run container: single base",
		"prod vs dev divergence",
		"deployFiles",
		"docs.zerops.io/references/zsc",
	}

	for _, marker := range requiredMarkers {
		if !strings.Contains(guide, marker) {
			t.Errorf("multiBaseGuidance() missing required marker %q", marker)
		}
	}
}

func TestAssembleRecipeKnowledge_InjectsMultiBase_ForPHPWithNPM(t *testing.T) {
	t.Parallel()
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	plan := &RecipePlan{
		RuntimeType: "php-nginx@8.4",
		Research: ResearchData{BuildCommands: []string{
			"composer install --no-dev",
			"npm ci",
			"npm run build",
		}},
	}
	out := assembleRecipeKnowledge(RecipeStepGenerate, plan, nil, store)
	if !strings.Contains(out, "Multi-Base Runtime (this recipe needs it)") {
		t.Error("generate step knowledge missing multi-base snippet for PHP+npm plan")
	}
	if !strings.Contains(out, "zsc install") {
		t.Error("multi-base snippet not included in generate guidance for PHP+npm plan")
	}
}

func TestAssembleRecipeKnowledge_OmitsMultiBase_ForNodePlan(t *testing.T) {
	t.Parallel()
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	plan := &RecipePlan{
		RuntimeType: "nodejs@22",
		Research:    ResearchData{BuildCommands: []string{"npm ci", "npm run build"}},
	}
	out := assembleRecipeKnowledge(RecipeStepGenerate, plan, nil, store)
	if strings.Contains(out, "Multi-Base Runtime (this recipe needs it)") {
		t.Error("generate step knowledge included multi-base snippet for Node plan — should be omitted")
	}
}

func TestAssembleRecipeKnowledge_OmitsMultiBase_ForPureGoPlan(t *testing.T) {
	t.Parallel()
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	plan := &RecipePlan{
		RuntimeType: "go@1",
		Research:    ResearchData{BuildCommands: []string{"go build -ldflags \"-s -w\""}},
	}
	out := assembleRecipeKnowledge(RecipeStepGenerate, plan, nil, store)
	if strings.Contains(out, "Multi-Base Runtime (this recipe needs it)") {
		t.Error("generate step knowledge included multi-base snippet for pure Go plan — should be omitted")
	}
}

func TestAssembleRecipeKnowledge_OmitsMultiBase_OutsideGenerateStep(t *testing.T) {
	t.Parallel()
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	plan := &RecipePlan{
		RuntimeType: "php-nginx@8.4",
		Research:    ResearchData{BuildCommands: []string{"composer install", "npm ci"}},
	}
	// Provision also injects knowledge, but multi-base only fires at generate —
	// provision is too early, the agent hasn't written zerops.yaml yet.
	out := assembleRecipeKnowledge(RecipeStepProvision, plan, nil, store)
	if strings.Contains(out, "Multi-Base Runtime (this recipe needs it)") {
		t.Error("provision step should not inject multi-base snippet")
	}
	out = assembleRecipeKnowledge(RecipeStepFinalize, plan, nil, store)
	if strings.Contains(out, "Multi-Base Runtime (this recipe needs it)") {
		t.Error("finalize step should not inject multi-base snippet")
	}
}
