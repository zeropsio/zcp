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

// TestBuildGroupRecipe_StageWiring_DecidesItsTiers: a stage is the Mate's
// by the same rule as a utility — its own variables name an address of
// another service of the Mate's own project, written out — and a volume
// stays in the group tiers while anything they carry mounts or names it:
// the setup a carried part builds, with every setup it extends, or a
// carried part's variable. What only the dev half or the Mate-wired stage
// runs is no such thing. A stage naming its own address, another project's
// or one through the subdomain variables, and a Mate whose subdomain host is
// unknown, reach every tier with their volume.
func TestBuildGroupRecipe_StageWiring_DecidesItsTiers(t *testing.T) {
	t.Parallel()
	agent := func(in *GroupRecipeInputs) *GroupStage { return &in.Runtimes[0].Stages[1] }
	// mountIn has the setup whose run starts with start mount the volume.
	mountIn := func(start string) func(*GroupRecipeInputs) {
		return func(in *GroupRecipeInputs) {
			line := "      start: " + start + "\n"
			if !strings.Contains(flipZeropsYAML, line) {
				t.Fatalf("no setup starts with %q", start)
			}
			in.Runtimes[0].ZeropsYAMLBody = strings.Replace(flipZeropsYAML, line, line+"      volume:\n        hostname: agenthome\n", 1)
		}
	}
	tests := []struct {
		name string
		edit func(*GroupRecipeInputs)
		// mateOwn is what only the AI Agent tier carries; everywhere what
		// every group tier does.
		mateOwn, everywhere []string
	}{
		{
			name:    "the agent stage wired to the Mate's dev, the volume only it mounts",
			edit:    func(*GroupRecipeInputs) {},
			mateOwn: []string{"agentrunner", "agenthome"}, everywhere: []string{"flip", "db", "storage"},
		},
		{
			name:    "only the dev setup mounts the volume beside it",
			edit:    mountIn("npm run dev"),
			mateOwn: []string{"agentrunner", "agenthome"}, everywhere: []string{"flip"},
		},
		{
			name:    "the app's setup mounts the volume too",
			edit:    mountIn("npm start"),
			mateOwn: []string{"agentrunner"}, everywhere: []string{"flip", "agenthome"},
		},
		{
			name: "the app's setup extends one mounting the volume",
			edit: func(in *GroupRecipeInputs) {
				in.Runtimes[0].ZeropsYAMLBody = strings.Replace(flipZeropsYAML, "  - setup: prod\n",
					"  - setup: base\n    run:\n      volume:\n        hostname: agenthome\n  - setup: prod\n    extends: base\n", 1)
			},
			mateOwn: []string{"agentrunner"}, everywhere: []string{"flip", "agenthome"},
		},
		{
			name: "the app's stage names the volume",
			edit: func(in *GroupRecipeInputs) {
				in.Runtimes[0].Stages[0].ServiceEnvs = []ProjectEnvVar{{Key: "HOME_DIR", Value: "/mnt/agenthome"}}
			},
			mateOwn: []string{"agentrunner"}, everywhere: []string{"flip", "agenthome"},
		},
		{
			name: "the agent stage names only its own address",
			edit: func(in *GroupRecipeInputs) {
				agent(in).ServiceEnvs[0].Value = "https://agentrunner-1df2-8080.prg1.zerops.app"
			},
			everywhere: []string{"flip", "agentrunner", "agenthome"},
		},
		{
			name: "the agent stage names another project's address",
			edit: func(in *GroupRecipeInputs) {
				agent(in).ServiceEnvs[0].Value = "https://flipdev-9f3c-3000.prg1.zerops.app"
			},
			everywhere: []string{"flip", "agentrunner", "agenthome"},
		},
		{
			name: "the agent stage names an address through the subdomain variables",
			edit: func(in *GroupRecipeInputs) {
				agent(in).ServiceEnvs[0].Value = "https://flipdev-${zeropsSubdomainHost}-3000.prg1.zerops.app"
			},
			everywhere: []string{"flip", "agentrunner", "agenthome"},
		},
		{
			name:       "no subdomain host known",
			edit:       func(in *GroupRecipeInputs) { in.MateSubdomainHost = "" },
			everywhere: []string{"flip", "agentrunner", "agenthome"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inputs := flipStudioInputs()
			tt.edit(&inputs)
			layout, _, err := BuildGroupRecipe(inputs)
			if err != nil {
				t.Fatalf("BuildGroupRecipe: %v", err)
			}
			if len(layout.Tiers) != len(groupTierDirs) {
				t.Fatalf("tiers = %d, want %d", len(layout.Tiers), len(groupTierDirs))
			}
			for _, tier := range layout.Tiers {
				types := tierServiceTypes(t, tier.ImportYAML)
				agentTier := tier.Title == "AI Agent"
				for _, host := range tt.mateOwn {
					if _, carried := types[host]; carried != agentTier {
						t.Errorf("tier %q carries %s = %v, want it on the AI Agent tier alone", tier.Title, host, carried)
					}
				}
				for _, host := range tt.everywhere {
					if host == "flip" && agentTier {
						host = "flipstage"
					}
					if _, carried := types[host]; !carried {
						t.Errorf("tier %q does not carry %s", tier.Title, host)
					}
				}
			}
		})
	}
}

// TestBuildGroupRecipe_TierClosure_ReadOfAMateWiredStage_Fails: a stage
// wired to the Mate is left out of Stage and Small Production, so a runtime
// those tiers carry whose setup reads it would start an app that cannot
// reach it. The recipe is refused, naming the stage, the runtime, its setup
// and the variable, for the group tiers alone: the AI Agent tier carries the
// stage.
func TestBuildGroupRecipe_TierClosure_ReadOfAMateWiredStage_Fails(t *testing.T) {
	t.Parallel()
	inputs := flipStudioInputs()
	inputs.Runtimes[0].ZeropsYAMLBody = strings.Replace(flipZeropsYAML,
		"        S3_ENDPOINT: ${storage_apiUrl}\n  - setup: agentrunner\n",
		"        S3_ENDPOINT: ${storage_apiUrl}\n        AGENT_URL: http://agentrunner:8080\n  - setup: agentrunner\n", 1)
	layout, warnings, err := BuildGroupRecipe(inputs)
	if err == nil {
		t.Fatalf("composed %d tiers, want the recipe refused (warnings %v)", len(layout.Tiers), warnings)
	}
	msg := err.Error()
	for _, want := range []string{"does not carry", `tier "Stage"`, `tier "Small Production"`, `runtime "flip"`, `setup "prod"`, "run.envVariables.AGENT_URL", `names "agentrunner"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("error does not say %s:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, `tier "AI Agent"`) {
		t.Errorf("error names the AI Agent tier, which carries the stage:\n%s", msg)
	}
}
