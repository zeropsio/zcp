// Tests for: group recipe — what serves a Mate itself stays on the AI Agent
// tier its project is made from, a stage of a pair among it.
package bundle

import (
	"strings"
	"testing"
)

// flipZeropsYAML is flip's zerops.yaml: the dev loop, the app's production
// setup reading the database and the object storage, and the owner's cloud
// agent, built from the same repository and keeping its home on agenthome.
const flipZeropsYAML = `zerops:
  - setup: flipdev
    build:
      base: nodejs@22
    run:
      base: nodejs@22
      start: npm run dev
      envVariables:
        DATABASE_URL: ${db_connectionString}
        S3_ENDPOINT: ${storage_apiUrl}
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
      deployFiles: ./
    run:
      base: nodejs@22
      start: npm start
      envVariables:
        DATABASE_URL: ${db_connectionString}
        S3_ENDPOINT: ${storage_apiUrl}
  - setup: agentrunner
    build:
      base: ubuntu@24.04
      deployFiles: ./runner
    run:
      base: ubuntu@24.04
      start: ./runner/agent-runner
      volume:
        hostname: agenthome
        mountPath: /home/agent
`

// flipStudioInputs is the flip.studio Mate's project as it ran on
// 2026-10-28: the flipdev pair with two stages — flipstage, the app, and
// agentrunner, the owner's cloud agent, its FLIP_SERVER the Mate's own dev
// address and its token a secret zcp can only generate — beside the
// database and object storage the app reads, and agenthome, the local
// storage the agent keeps its home on.
func flipStudioInputs() GroupRecipeInputs {
	return GroupRecipeInputs{
		Name:              "flip",
		Title:             "flip",
		MateProjectName:   "flip-studio",
		MateSubdomainHost: "1df2.prg1.zerops.app",
		Runtimes: []GroupRuntime{{
			DevHostname: "flipdev",
			Stages: []GroupStage{
				{Hostname: "flipstage", SetupName: "prod", ServiceType: "nodejs@22", SubdomainEnabled: true},
				{Hostname: "agentrunner", SetupName: "agentrunner", ServiceType: "ubuntu@24.04", ServiceEnvs: []ProjectEnvVar{
					{Key: "FLIP_SERVER", Value: "https://flipdev-1df2-3000.prg1.zerops.app"},
					{Key: "CLAUDE_CODE_OAUTH_TOKEN", Value: "sk-ant-oat01-Xq3vR8pLm2", Sensitive: true},
				}},
			},
			ServiceType:      "nodejs@22",
			RepoURL:          "https://hq.example/git/org/flip/flipdev.git",
			SetupName:        "flipdev",
			ZeropsYAMLBody:   flipZeropsYAML,
			SubdomainEnabled: true,
		}},
		ManagedServices: []ManagedServiceEntry{
			{Hostname: "agenthome", Type: "local-storage"},
			{Hostname: "db", Type: "postgresql:single@18"},
			{Hostname: "storage", Type: "object-storage", QuotaGBytes: 2},
		},
		ProjectHostnames: []string{"agentrunner", "agenthome", "storage", "db", "flipdev", "flipstage", "zcp", "core"},
	}
}

// TestBuildGroupRecipe_MateWiredStage_OnlyInTheAIAgentTier is flip.studio's
// group recipe (2026-10-28) as the project really is: the owner's cloud
// agent is no utility but a stage of the flipdev pair, built from flip's own
// repository by its own setup, which mounts agenthome. A stage whose own
// variables name an address of another service of the Mate's own project is
// wired to that Mate, as a utility is: it and the volume only it uses stay
// on the AI Agent tier, and Stage and Small Production carry the app — its
// part, the database and the object storage it reads — and say what they
// left out.
func TestBuildGroupRecipe_MateWiredStage_OnlyInTheAIAgentTier(t *testing.T) {
	t.Parallel()
	layout, warnings, err := BuildGroupRecipe(flipStudioInputs())
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	if len(layout.Tiers) != len(groupTierDirs) {
		t.Fatalf("tiers = %d, want %d", len(layout.Tiers), len(groupTierDirs))
	}
	for _, tier := range layout.Tiers {
		types := tierServiceTypes(t, tier.ImportYAML)
		agent := tier.Title == "AI Agent"
		for _, host := range []string{"agentrunner", "agenthome"} {
			if _, carried := types[host]; carried != agent {
				t.Errorf("tier %q carries %s = %v, want it on the AI Agent tier alone:\n%s", tier.Title, host, carried, tier.ImportYAML)
			}
		}
		app := []string{"db", "storage", "flip"}
		if agent {
			app = []string{"db", "storage", "flipdev", "flipstage"}
		}
		for _, host := range app {
			if _, carried := types[host]; !carried {
				t.Errorf("tier %q does not carry %s:\n%s", tier.Title, host, tier.ImportYAML)
			}
		}
		if !agent && strings.Contains(tier.ImportYAML, "-1df2-") {
			t.Errorf("tier %q names an address of the Mate's own project:\n%s", tier.Title, tier.ImportYAML)
		}
	}
	said := strings.Join(warnings, "\n")
	for _, want := range []string{`"agentrunner" stays on the AI Agent tier only`, "FLIP_SERVER", `"agenthome" stays on the AI Agent tier only`} {
		if !strings.Contains(said, want) {
			t.Errorf("warnings do not say %q:\n%s", want, said)
		}
	}
}
