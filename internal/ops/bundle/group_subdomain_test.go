// Tests for: group recipe — a value naming the public address of a service of
// the Mate's own project is written, in every tier, as a reference to that
// service's address in the environment the tier makes.
package bundle

import (
	"slices"
	"strings"
	"testing"
)

// flipServerZeropsYAML is flip's zerops.yaml as its repository holds it: the
// dev loop and the app each serve HTTP on 5195, the cloud agent on no port.
const flipServerZeropsYAML = `zerops:
  - setup: dev
    build:
      base: ubuntu/nodejs@24
    run:
      base: ubuntu/nodejs@24
      ports:
        - port: 5195
          httpSupport: true
      start: zcp-devsup run
  - setup: prod
    build:
      base: ubuntu/nodejs@24
    run:
      base: ubuntu/nodejs@24
      ports:
        - port: 5195
          httpSupport: true
      envVariables:
        DATABASE_URL: ${db_connectionString}/${db_dbName}
        BETTER_AUTH_URL: ${FLIP_PUBLIC_URL}
      start: node hosted/server/src/main.ts
  - setup: agentrunner
    build:
      base: ubuntu/nodejs@24
    run:
      base: ubuntu/nodejs@24
      volume:
        hostname: agenthome
        mountPath: /mnt/agenthome
      start: node hosted/agent-runner/run.mjs
`

// flipChangeTwoInputs is the flip.studio Mate as its group change #2 was
// composed from (2026-10-28): the flipdev pair with two stages — flipstage,
// the app, whose FLIP_PUBLIC_URL a person had set to its own
// ${zeropsSubdomain}, and agentrunner, the owner's cloud agent, whose
// FLIP_SERVER is the stage's public address written out — beside the agent's
// home, the database and the object storage. The Mate's project's subdomain
// label is 3901.
func flipChangeTwoInputs() GroupRecipeInputs {
	return GroupRecipeInputs{
		Name:              "flip-studio",
		MateProjectName:   "flip.studio - Filip",
		MateSubdomainHost: "3901.prg1.zerops.app",
		Runtimes: []GroupRuntime{{
			DevHostname:      "flipdev",
			ServiceType:      "ubuntu/nodejs@24",
			RepoURL:          "https://hq.example/git/org/app/flipdev",
			SetupName:        "dev",
			ZeropsYAMLBody:   flipServerZeropsYAML,
			SubdomainEnabled: true,
			ServiceEnvs:      []ProjectEnvVar{{Key: "FLIP_PUBLIC_URL", Value: "https://flipdev-3901-5195.prg1.zerops.app"}},
			Stages: []GroupStage{
				{Hostname: "flipstage", SetupName: "prod", ServiceType: "ubuntu/nodejs@24", SubdomainEnabled: true,
					ServiceEnvs: []ProjectEnvVar{{Key: "FLIP_PUBLIC_URL", Value: "${zeropsSubdomain}"}}},
				{Hostname: "agentrunner", SetupName: "agentrunner", ServiceType: "ubuntu/nodejs@24",
					ServiceEnvs: []ProjectEnvVar{
						{Key: "FLIP_SERVER", Value: "https://flipstage-3901-5195.prg1.zerops.app"},
						{Key: "FLIP_AGENT_LINK_CODE", Value: "Zp4mQ8rT2vX9", Sensitive: true},
					}},
			},
		}},
		ManagedServices: []ManagedServiceEntry{
			{Hostname: "agenthome", Type: "local-storage"},
			{Hostname: "db", Type: "postgresql:single@17"},
			{Hostname: "storage", Type: "object-storage", QuotaGBytes: 20},
		},
		ProjectHostnames: []string{"agenthome", "agentrunner", "db", "flipdev", "flipstage", "storage", "zcp"},
	}
}

// tierVaultValue is the value of key in the vault of host in a tier's
// import.yaml — the project's vault when host is "" — and whether it is
// there.
func tierVaultValue(t *testing.T, body, host, key string) (string, bool) {
	t.Helper()
	owner := mappingValue(tierMapping(t, body), "project")
	if host != "" {
		owner = serviceNode(t, body, host)
	}
	value, ok := scalarMap(mappingValue(owner, "vault"))[key]
	return value, ok
}

// TestBuildGroupRecipe_FlipServer_NamesTheEnvironmentsOwnStage is flip.studio's
// group change #2: agentrunner's FLIP_SERVER, the Mate's stage's public
// address written out, came out in Stage and Small Production renamed to
// https://flip-3901-5195.prg1.zerops.app — a host that exists nowhere, and
// even with the right name it would point back into the Mate's project. A
// person set it by hand to ${flip_zeropsSubdomain}. Every tier now writes it
// as the address of the service it names in the environment the tier makes:
// the group environments' flip, and on the AI Agent tier — whose new project
// has a subdomain label of its own — flipstage. The dev service's own address
// is its own ${zeropsSubdomain}, and the stage's, which a person already set
// so, stays it. No tier names the Mate's own project.
func TestBuildGroupRecipe_FlipServer_NamesTheEnvironmentsOwnStage(t *testing.T) {
	t.Parallel()
	layout, warnings, err := BuildGroupRecipe(flipChangeTwoInputs())
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	tests := []struct {
		tier, agent, app, appURL, server string
	}{
		{tier: "AI Agent", agent: "agentrunner", app: "flipstage", server: "${flipstage_zeropsSubdomain}"},
		{tier: "Stage", agent: "agentrunner", app: "flip", server: "${flip_zeropsSubdomain}"},
		{tier: "Small Production", agent: "agentrunner", app: "flip", server: "${flip_zeropsSubdomain}"},
	}
	for _, tt := range tests {
		body := tierBody(t, layout.Tiers, tt.tier)
		if got, _ := tierVaultValue(t, body, tt.agent, "FLIP_SERVER"); got != tt.server {
			t.Errorf("tier %q: agentrunner FLIP_SERVER = %q, want %q", tt.tier, got, tt.server)
		}
		if got, _ := tierVaultValue(t, body, tt.app, "FLIP_PUBLIC_URL"); got != "${zeropsSubdomain}" {
			t.Errorf("tier %q: %s FLIP_PUBLIC_URL = %q, want ${zeropsSubdomain}", tt.tier, tt.app, got)
		}
		if tt.tier == "AI Agent" {
			if got, _ := tierVaultValue(t, body, "flipdev", "FLIP_PUBLIC_URL"); got != "${zeropsSubdomain}" {
				t.Errorf("tier %q: flipdev FLIP_PUBLIC_URL = %q, want ${zeropsSubdomain}", tt.tier, got)
			}
		}
		if strings.Contains(body, "-3901-") {
			t.Errorf("tier %q names an address of the Mate's own project:\n%s", tt.tier, body)
		}
	}
	for _, w := range warnings {
		if strings.Contains(w, "FLIP_SERVER") || strings.Contains(w, "FLIP_PUBLIC_URL") {
			t.Errorf("a reference written in every tier is warned of: %s", w)
		}
	}
}

// TestBuildGroupRecipe_MateAddress_WrittenAsTheEnvironmentsReference: what a
// value says after the address — a path, a query — is kept, and the address
// alone becomes the reference. zeropsSubdomain holds the whole https://
// address with no slash after it, one line per HTTP port the service serves
// (spec-mate §10.10), and the platform has no variable per port; so a
// reference stands for the address only where the service the tier carries
// under that name builds a setup declaring one HTTP port, the one the
// address names (80 when it names none), and the value writes it as
// https:// with no port after it. Anything else — several ports, another
// port, no port declared, a service whose ports the recipe does not read, a
// bare host — is kept as written and said, never guessed; so is an address
// in a project variable: every service reads it, and a reference resolves
// only in a service started after the one it names was deployed, which no
// priority gives every service at once. An address of a service the tier
// does not carry is kept as it is and said nothing of, a URL off Zerops is
// no address, and neither is renamed.
func TestBuildGroupRecipe_MateAddress_WrittenAsTheEnvironmentsReference(t *testing.T) {
	t.Parallel()
	const stageAddress = "https://flipstage-3901-5195.prg1.zerops.app"
	server := func(value string) func(*GroupRecipeInputs) {
		return func(in *GroupRecipeInputs) { in.Runtimes[0].Stages[1].ServiceEnvs[0].Value = value }
	}
	appPorts := func(ports string) func(*GroupRecipeInputs) {
		return func(in *GroupRecipeInputs) {
			const declared = "      ports:\n        - port: 5195\n          httpSupport: true\n      envVariables:\n        DATABASE_URL"
			if !strings.Contains(flipServerZeropsYAML, declared) {
				t.Fatal("the app's setup declares no port to replace")
			}
			in.Runtimes[0].ZeropsYAMLBody = strings.Replace(flipServerZeropsYAML, declared, ports+"      envVariables:\n        DATABASE_URL", 1)
		}
	}
	tests := []struct {
		name string
		edit func(*GroupRecipeInputs)
		// agentHost and groupHost hold the value on the AI Agent tier and in
		// the group environments — "" for the project's vault — under key.
		agentHost, groupHost, key string
		agentWant, groupWant      string
		// said are what one warning says, and its tiers; unsaid what no
		// warning about key says.
		said   []string
		unsaid []string
	}{
		{
			name:      "the path and the query after the address kept",
			edit:      server(stageAddress + "/api/v1?region=eu"),
			agentHost: "agentrunner", groupHost: "agentrunner", key: "FLIP_SERVER",
			agentWant: "${flipstage_zeropsSubdomain}/api/v1?region=eu",
			groupWant: "${flip_zeropsSubdomain}/api/v1?region=eu",
		},
		{
			// A label is the platform's name for the project, as public as
			// the address, and as often has a letter in it as not: it never
			// makes the value a secret the tier generates.
			name: "the usual label, a letter among its digits",
			edit: func(in *GroupRecipeInputs) {
				in.MateSubdomainHost = "1df2.prg1.zerops.app"
				in.Runtimes[0].Stages[1].ServiceEnvs[0].Value = "https://flipstage-1df2-5195.prg1.zerops.app"
			},
			agentHost: "agentrunner", groupHost: "agentrunner", key: "FLIP_SERVER",
			agentWant: "${flipstage_zeropsSubdomain}", groupWant: "${flip_zeropsSubdomain}",
			unsaid: []string{"FLIP_SERVER"},
		},
		{
			name: "the service's own address, with its trailing slash",
			edit: func(in *GroupRecipeInputs) {
				in.Runtimes[0].Stages[0].ServiceEnvs[0].Value = stageAddress + "/"
			},
			agentHost: "flipstage", groupHost: "flip", key: "FLIP_PUBLIC_URL",
			agentWant: "${zeropsSubdomain}/", groupWant: "${zeropsSubdomain}/",
		},
		{
			name:      "several HTTP ports: a reference would list an address for each",
			edit:      appPorts("      ports:\n        - port: 3000\n          httpSupport: true\n        - port: 5195\n          httpSupport: true\n"),
			agentHost: "agentrunner", groupHost: "agentrunner", key: "FLIP_SERVER",
			agentWant: stageAddress, groupWant: stageAddress,
			said: []string{"FLIP_SERVER", "flipstage-3901-5195.prg1.zerops.app", "3000 and 5195", "Stage and Small Production"},
		},
		{
			name:      "one HTTP port, not the one the address names",
			edit:      appPorts("      ports:\n        - port: 3000\n          httpSupport: true\n        - port: 5195\n"),
			agentHost: "agentrunner", groupHost: "agentrunner", key: "FLIP_SERVER",
			agentWant: stageAddress, groupWant: stageAddress,
			said: []string{"FLIP_SERVER", "flipstage-3901-5195.prg1.zerops.app", "3000"},
		},
		{
			name:      "no port declared",
			edit:      appPorts(""),
			agentHost: "agentrunner", groupHost: "agentrunner", key: "FLIP_SERVER",
			agentWant: stageAddress, groupWant: stageAddress,
			said: []string{"FLIP_SERVER", "flipstage-3901-5195.prg1.zerops.app"},
		},
		{
			name:      "a bare host, which a reference does not write",
			edit:      server("flipstage-3901-5195.prg1.zerops.app"),
			agentHost: "agentrunner", groupHost: "agentrunner", key: "FLIP_SERVER",
			agentWant: "flipstage-3901-5195.prg1.zerops.app", groupWant: "flipstage-3901-5195.prg1.zerops.app",
			said: []string{"FLIP_SERVER", "flipstage-3901-5195.prg1.zerops.app"},
		},
		{
			name: "a project variable, which every service reads: no priority orders the reference",
			edit: func(in *GroupRecipeInputs) {
				in.ProjectEnvs = []ProjectEnvVar{{Key: "APP_URL", Value: stageAddress}}
			},
			key:       "APP_URL",
			agentWant: stageAddress, groupWant: stageAddress,
			said: []string{"APP_URL", "flipstage-3901-5195.prg1.zerops.app", "project variable", "every service"},
		},
		{
			name: "a service the group environments do not carry: the Mate's mail catcher",
			edit: func(in *GroupRecipeInputs) {
				in.Utilities = []GroupUtility{{Hostname: "mailpit", ServiceType: "alpine@3.21", BuildFromGit: "https://github.com/zerops-recipe-apps/mailpit-app"}}
				in.ProjectHostnames = append(in.ProjectHostnames, "mailpit")
				in.Runtimes[0].Stages[0].ServiceEnvs = append(in.Runtimes[0].Stages[0].ServiceEnvs,
					ProjectEnvVar{Key: "MAIL_UI", Value: "https://mailpit-3901-8025.prg1.zerops.app"})
			},
			agentHost: "flipstage", groupHost: "flip", key: "MAIL_UI",
			agentWant: "https://mailpit-3901-8025.prg1.zerops.app", groupWant: "https://mailpit-3901-8025.prg1.zerops.app",
			// The AI Agent tier carries the mail catcher, whose ports the
			// recipe does not read.
			said:   []string{"MAIL_UI", "mailpit-3901-8025.prg1.zerops.app", "AI Agent"},
			unsaid: []string{"Stage"},
		},
		{
			name:      "a URL off Zerops",
			edit:      server("https://s.flip.studio/api"),
			agentHost: "agentrunner", groupHost: "agentrunner", key: "FLIP_SERVER",
			agentWant: "https://s.flip.studio/api", groupWant: "https://s.flip.studio/api",
			unsaid: []string{"FLIP_SERVER"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inputs := flipChangeTwoInputs()
			tt.edit(&inputs)
			layout, warnings, err := BuildGroupRecipe(inputs)
			if err != nil {
				t.Fatalf("BuildGroupRecipe: %v", err)
			}
			for _, tier := range layout.Tiers {
				host, want := tt.groupHost, tt.groupWant
				if tier.Title == "AI Agent" {
					host, want = tt.agentHost, tt.agentWant
				}
				if got, ok := tierVaultValue(t, tier.ImportYAML, host, tt.key); !ok || got != want {
					t.Errorf("tier %q: %s %s = %q (written %v), want %q", tier.Title, host, tt.key, got, ok, want)
				}
			}
			var about []string
			for _, w := range warnings {
				if strings.Contains(w, tt.key) {
					about = append(about, w)
				}
			}
			if len(tt.said) > 0 && !slices.ContainsFunc(about, func(w string) bool {
				return !slices.ContainsFunc(tt.said, func(want string) bool { return !strings.Contains(w, want) })
			}) {
				t.Errorf("no warning says all of %q:\n%s", tt.said, strings.Join(warnings, "\n"))
			}
			for _, w := range about {
				for _, not := range tt.unsaid {
					if strings.Contains(w, not) {
						t.Errorf("a warning about %s says %q: %s", tt.key, not, w)
					}
				}
			}
		})
	}
}

// TestBuildGroupRecipe_MateAddress_SensitiveNeverRewrittenOrSaid: a value the
// platform flags sensitive is generated in every tier; its address is
// neither rewritten nor printed, in a tier or in a warning.
func TestBuildGroupRecipe_MateAddress_SensitiveNeverRewrittenOrSaid(t *testing.T) {
	t.Parallel()
	inputs := flipChangeTwoInputs()
	inputs.Runtimes[0].Stages[1].ServiceEnvs[0].Sensitive = true
	inputs.ProjectEnvs = []ProjectEnvVar{{Key: "APP_URL", Value: "https://flipstage-3901-5195.prg1.zerops.app", Sensitive: true}}
	layout, warnings, err := BuildGroupRecipe(inputs)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	for _, tier := range layout.Tiers {
		got, _ := tierVaultValue(t, tier.ImportYAML, "agentrunner", "FLIP_SERVER")
		if !strings.HasPrefix(got, "<@generateRandomString(") {
			t.Errorf("tier %q: FLIP_SERVER = %q, want a generated value", tier.Title, got)
		}
		if strings.Contains(tier.ImportYAML, "flipstage-3901") || strings.Contains(tier.ImportYAML, "_zeropsSubdomain}") {
			t.Errorf("tier %q prints or rewrites a sensitive value:\n%s", tier.Title, tier.ImportYAML)
		}
	}
	for _, w := range warnings {
		if strings.Contains(w, "flipstage-3901") {
			t.Errorf("a warning prints a sensitive value: %s", w)
		}
	}
}

// TestBuildGroupRecipe_MateAddress_RewriteAndReadsAgree: a public address
// written out reads nothing (spec-workflows.md §8 PS-3), so it never brings
// a service into a tier, and a reference the rewrite writes names only what
// the tier already carries — it adds no read, no Box and no closure gap. A
// runner's own variable naming the Box's address leaves the Box out of a
// tier nothing else reads it in, and the value is kept as it is; where the
// runner's setup reads the Box, every tier carries it and keeps the address
// as written, since a Box serves several HTTP ports.
func TestBuildGroupRecipe_MateAddress_RewriteAndReadsAgree(t *testing.T) {
	t.Parallel()
	const boxUI = "https://box-1df2-8081.prg1.zerops.app"
	plain := map[string]string{"NATS_PORT": "${broker_port}"}
	tests := []struct {
		name      string
		runner    map[string]string
		wantTiers []string
		said      bool
	}{
		{name: "nothing else reads the Box", runner: plain},
		{name: "the runner's setup reads the Box", runner: boxReads, wantTiers: groupTierDirs, said: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inputs := boxRunnerInputs(zeropsYAMLWithVars(setupVars{name: "runnerdev", vars: tt.runner}, setupVars{name: "runnerprod", vars: tt.runner}), false)
			inputs.MateSubdomainHost = "1df2.prg1.zerops.app"
			inputs.ProjectHostnames = []string{"broker", "box", "runnerdev", "runnerstage", "zcp"}
			inputs.Runtimes[0].Stages[0].ServiceEnvs = []ProjectEnvVar{{Key: "SANDBOX_UI", Value: boxUI}}
			layout, warnings, err := BuildGroupRecipe(inputs)
			if err != nil {
				t.Fatalf("BuildGroupRecipe: %v", err)
			}
			files := groupFiles(t, layout)
			var carried []string
			for _, dir := range groupTierDirs {
				if _, ok := serviceEntries(t, tierDoc(t, files, dir))["box"]; ok {
					carried = append(carried, dir)
				}
			}
			if !slices.Equal(carried, tt.wantTiers) {
				t.Errorf("tiers carrying the Box = %v, want %v", carried, tt.wantTiers)
			}
			for _, tier := range layout.Tiers {
				host := "runner"
				if tier.Title == "AI Agent" {
					host = "runnerstage"
				}
				if got, _ := tierVaultValue(t, tier.ImportYAML, host, "SANDBOX_UI"); got != boxUI {
					t.Errorf("tier %q: SANDBOX_UI = %q, want it as written", tier.Title, got)
				}
			}
			said := slices.ContainsFunc(warnings, func(w string) bool {
				return strings.Contains(w, "SANDBOX_UI") && strings.Contains(w, "box-1df2-8081.prg1.zerops.app")
			})
			if said != tt.said {
				t.Errorf("a warning names the Box's address = %v, want %v:\n%s", said, tt.said, strings.Join(warnings, "\n"))
			}
		})
	}
}
