package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// witness: internal/workflow/bootstrap_guide_assembly.go | stage: runs what was added to main; the Review build builds a change on it → stage: preview
func TestBootstrapGuide_BGURLs_LabelsStageByEnvironment(t *testing.T) {
	t.Parallel()
	urls := []RuntimeURL{
		{Hostname: "appdev", Role: RuntimeURLRoleDev, URL: "https://dev.example"},
		{Hostname: "appstage", Role: RuntimeURLRoleStage, URL: "https://stage.example", Handoff: true},
	}
	const draft = "Once the draft is ready, tell the person they can open it in Mate."
	const missing = "No runtime URL could be resolved yet — subdomain access may still be propagating. Re-check via `zerops_workflow action=\"status\"` or `zerops_discover`.\n"
	const footer = "\nA hostname not listed above could not be resolved yet — this never blocks close; re-check via `zerops_discover` if needed.\n"
	const standalone = "## Runtime URLs\n\n" +
		"**Hand this URL back to the user as their " + "preview** — a copy with test data, never \"online\", \"live\" or \"stage\", nothing to install or share (stage half — the dev container is idle by design, do not hand that one back): https://stage.example\n\n" +
		"| Hostname | Role | URL |\n|---|---|---|\n" +
		"| `appdev` | dev | https://dev.example |\n" +
		"| `appstage` | stage (handoff) | https://stage.example |\n" + footer
	const mate = "## Runtime URLs\n\n" + draft + "\n\n" +
		"| Hostname | Role | URL |\n|---|---|---|\n" +
		"| `appdev` | dev | https://dev.example |\n" +
		"| `appstage` | stage: runs what was added to main; the Review build builds a change on it | https://stage.example |\n" + footer
	for _, tc := range []struct {
		name string
		urls []RuntimeURL
		mate bool
		want string
	}{
		{"mate stage", urls, true, mate},
		{"mate unresolved", nil, true, "## Runtime URLs\n\n" + draft + "\n\n" + missing},
		{"standalone stage", urls, false, standalone},
		{"standalone unresolved", nil, false, "## Runtime URLs\n\n" + missing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := FormatRuntimeURLsForGuide(tc.urls, tc.mate, draft); got != tc.want {
				t.Errorf("guide = %q, want %q", got, tc.want)
			}
		})
	}
}

// witness: internal/workflow/bootstrap_guide_assembly.go | sb.WriteString(mateRecipeStageNote + "\n\n") →
func TestBootstrapGuide_BGRecipe_MateNamesTheRecipeStage(t *testing.T) {
	t.Parallel()
	const yaml = "services:\n  - hostname: appdev\n    type: nodejs@22\n    zeropsSetup: dev\n  # appstage: production-mode container for staging previews.\n  - hostname: appstage\n    type: nodejs@22\n    zeropsSetup: prod\n"
	for _, tc := range []struct {
		name string
		step string
		plan *ServicePlan
	}{
		{"discover", StepDiscover, nil},
		{"provision", StepProvision, &ServicePlan{Targets: []BootstrapTarget{{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "standard", ExplicitStage: "appstage"}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bs := NewBootstrapState()
			bs.Route = BootstrapRouteRecipe
			bs.RecipeMatch = &RecipeMatch{Slug: "analog-ssr-hello-world", Confidence: 0.97, Mode: topology.PlanModeStandard, ImportYAML: yaml}
			bs.Plan = tc.plan
			standalone := bs.buildGuide(tc.step, 0, EnvContainer, false)
			mate := bs.buildGuide(tc.step, 0, EnvContainer, true)
			if strings.Count(mate, mateRecipeStageNote) != 1 || !strings.Contains(mate, mateRecipeStageNote+"\n\n```yaml\n") {
				t.Fatalf("Mate guide lacks the stage note once before the YAML:\n%s", mate)
			}
			if strings.Contains(standalone, mateRecipeStageNote) || strings.Replace(mate, mateRecipeStageNote+"\n\n", "", 1) != standalone {
				t.Fatalf("standalone guide differs from the Mate guide beyond the note:\n%s", standalone)
			}
		})
	}
}
