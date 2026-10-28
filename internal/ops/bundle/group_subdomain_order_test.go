// Tests for: group recipe — a service a tier's runtime reads through
// ${<host>_zeropsSubdomain} is created before that runtime, since the
// reference resolves only in a container started after the service it names
// was deployed.
package bundle

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// tierPriorities reads a tier's runtimes as hostname → priority, the managed
// services left out.
func tierPriorities(t *testing.T, body string, managed ...string) map[string]int {
	t.Helper()
	out := map[string]int{}
	for host, entry := range tierEntries(t, body) {
		if slices.Contains(managed, host) {
			continue
		}
		out[host], _ = entry["priority"].(int)
	}
	return out
}

// flipManaged are the flip Mate's managed services, created first at 10.
var flipManaged = []string{"agenthome", "db", "storage"}

// TestBuildGroupRecipe_SubdomainReference_TheServiceItNamesComesFirst: a
// service's zeropsSubdomain resolves in another only when the service was
// deployed before the other's container started; a reader started with it
// or before it reads the literal ${<host>_zeropsSubdomain} until it restarts
// (live, Mate2 lab, 2026-10-28). The platform creates a tier's services a
// priority at a time, each wave deployed before the next starts, so every
// service a tier's runtime reads that way gets a higher priority than the
// runtime, in every tier: a reference the rewrite writes, one a person
// wrote, and one in the setup the tier builds for the runtime — chained when
// the service read reads another. A variable of the service that is no
// public address orders nothing, and neither does a service's own address.
// The header says so.
func TestBuildGroupRecipe_SubdomainReference_TheServiceItNamesComesFirst(t *testing.T) {
	t.Parallel()
	docs := func(in *GroupRecipeInputs) {
		in.Utilities = []GroupUtility{{Hostname: "docs", ServiceType: "nodejs@22", BuildFromGit: "https://github.com/acme/docs"}}
		in.ProjectHostnames = append(in.ProjectHostnames, "docs")
		in.Runtimes[0].ZeropsYAMLBody = strings.Replace(flipServerZeropsYAML,
			"        BETTER_AUTH_URL: ${FLIP_PUBLIC_URL}\n",
			"        BETTER_AUTH_URL: ${FLIP_PUBLIC_URL}\n        DOCS_URL: ${docs_zeropsSubdomain}\n", 1)
	}
	server := func(value string) func(*GroupRecipeInputs) {
		return func(in *GroupRecipeInputs) { in.Runtimes[0].Stages[1].ServiceEnvs[0].Value = value }
	}
	tests := []struct {
		name string
		edit func(*GroupRecipeInputs)
		// agent and group are the runtimes' priorities on the AI Agent tier
		// and in the group environments.
		agent, group map[string]int
	}{
		{
			name:  "flip's case: the reference the rewrite writes",
			edit:  func(*GroupRecipeInputs) {},
			agent: map[string]int{"flipdev": 1, "flipstage": 2, "agentrunner": 1},
			group: map[string]int{"flip": 2, "agentrunner": 1},
		},
		{
			name:  "a reference a person wrote",
			edit:  server("${flipstage_zeropsSubdomain}/api"),
			agent: map[string]int{"flipdev": 1, "flipstage": 2, "agentrunner": 1},
			group: map[string]int{"flip": 2, "agentrunner": 1},
		},
		{
			name:  "a reference in the app's setup, chained through the app to the agent",
			edit:  docs,
			agent: map[string]int{"flipdev": 1, "flipstage": 2, "agentrunner": 1, "docs": 3},
			group: map[string]int{"flip": 2, "agentrunner": 1, "docs": 3},
		},
		{
			name:  "a variable that is no public address",
			edit:  server("http://${flipstage_hostname}:5195"),
			agent: map[string]int{"flipdev": 1, "flipstage": 1, "agentrunner": 1},
			group: map[string]int{"flip": 1, "agentrunner": 1},
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
				want, reader, target := tt.group, "agentrunner", "flip"
				if tier.Title == "AI Agent" {
					want, target = tt.agent, "flipstage"
				}
				if got := tierPriorities(t, tier.ImportYAML, flipManaged...); !maps.Equal(got, want) {
					t.Errorf("tier %q: priorities = %v, want %v", tier.Title, got, want)
				}
				// The file lists the services in the order they are created.
				if want[target] > want[reader] && strings.Index(tier.ImportYAML, "hostname: "+target+"\n") > strings.Index(tier.ImportYAML, "hostname: "+reader+"\n") {
					t.Errorf("tier %q lists %s after %s, which reads it", tier.Title, target, reader)
				}
				header := strings.Join(strings.Fields(strings.ReplaceAll(tier.ImportYAML, "#", " ")), " ")
				if !strings.Contains(header, "whose variables name its public address") {
					t.Errorf("tier %q's header does not say a runtime comes before those naming its public address:\n%s", tier.Title, tier.ImportYAML)
				}
			}
			for _, w := range warnings {
				if strings.Contains(w, "no priority") {
					t.Errorf("an orderable reference is warned of: %s", w)
				}
			}
		})
	}
}

// TestBuildGroupRecipe_SubdomainReference_ACycleKeepsOneSideAsWritten: two
// runtimes naming each other's public address cannot both come first. The
// rewrite keeps one side's address as written and says so, rather than write
// a reference certain to reach its reader as the literal text, and the side
// it writes is ordered: the service it names comes first. References a
// person wrote are theirs: both stay, and the pass says no priority orders
// them.
func TestBuildGroupRecipe_SubdomainReference_ACycleKeepsOneSideAsWritten(t *testing.T) {
	t.Parallel()
	const (
		agentAddress = "https://agentrunner-3901-8080.prg1.zerops.app"
		stageAddress = "https://flipstage-3901-5195.prg1.zerops.app"
	)
	// both has the app name the agent at value, the agent the app at server,
	// and the agent serve HTTP on 8080.
	both := func(value, server string) GroupRecipeInputs {
		in := flipChangeTwoInputs()
		in.Runtimes[0].ZeropsYAMLBody = strings.Replace(flipServerZeropsYAML,
			"      volume:\n", "      ports:\n        - port: 8080\n          httpSupport: true\n      volume:\n", 1)
		in.Runtimes[0].Stages[0].ServiceEnvs = append(in.Runtimes[0].Stages[0].ServiceEnvs, ProjectEnvVar{Key: "AGENT_URL", Value: value})
		in.Runtimes[0].Stages[1].ServiceEnvs[0].Value = server
		return in
	}

	t.Run("addresses the rewrite would write as references", func(t *testing.T) {
		t.Parallel()
		layout, warnings, err := BuildGroupRecipe(both(agentAddress, stageAddress))
		if err != nil {
			t.Fatalf("BuildGroupRecipe: %v", err)
		}
		for _, tier := range layout.Tiers {
			app := "flip"
			if tier.Title == "AI Agent" {
				app = "flipstage"
			}
			agentURL, _ := tierVaultValue(t, tier.ImportYAML, app, "AGENT_URL")
			server, _ := tierVaultValue(t, tier.ImportYAML, "agentrunner", "FLIP_SERVER")
			priorities := tierPriorities(t, tier.ImportYAML, flipManaged...)
			switch {
			case agentURL == agentAddress && server == "${"+app+"_zeropsSubdomain}":
				if priorities[app] <= priorities["agentrunner"] {
					t.Errorf("tier %q: %s at %d, not above agentrunner at %d, which reads it", tier.Title, app, priorities[app], priorities["agentrunner"])
				}
			case server == stageAddress && agentURL == "${agentrunner_zeropsSubdomain}":
				if priorities["agentrunner"] <= priorities[app] {
					t.Errorf("tier %q: agentrunner at %d, not above %s at %d, which reads it", tier.Title, priorities["agentrunner"], app, priorities[app])
				}
			default:
				t.Errorf("tier %q: AGENT_URL = %q, FLIP_SERVER = %q, want exactly one a reference and the other as written", tier.Title, agentURL, server)
			}
		}
		if !slices.ContainsFunc(warnings, func(w string) bool {
			return strings.Contains(w, "no priority") && (strings.Contains(w, "agentrunner-3901-8080") || strings.Contains(w, "flipstage-3901-5195"))
		}) {
			t.Errorf("no warning says which address is kept and that no priority orders the two:\n%s", strings.Join(warnings, "\n"))
		}
	})

	t.Run("references a person wrote", func(t *testing.T) {
		t.Parallel()
		layout, warnings, err := BuildGroupRecipe(both("${agentrunner_zeropsSubdomain}", "${flipstage_zeropsSubdomain}"))
		if err != nil {
			t.Fatalf("BuildGroupRecipe: %v", err)
		}
		for _, tier := range layout.Tiers {
			app := "flip"
			if tier.Title == "AI Agent" {
				app = "flipstage"
			}
			if got, _ := tierVaultValue(t, tier.ImportYAML, app, "AGENT_URL"); got != "${agentrunner_zeropsSubdomain}" {
				t.Errorf("tier %q: AGENT_URL = %q, want the person's reference", tier.Title, got)
			}
			if got, _ := tierVaultValue(t, tier.ImportYAML, "agentrunner", "FLIP_SERVER"); got != "${"+app+"_zeropsSubdomain}" {
				t.Errorf("tier %q: FLIP_SERVER = %q, want the person's reference", tier.Title, got)
			}
		}
		if !slices.ContainsFunc(warnings, func(w string) bool {
			return strings.Contains(w, "no priority") && strings.Contains(w, "_zeropsSubdomain}")
		}) {
			t.Errorf("no warning says no priority orders the references:\n%s", strings.Join(warnings, "\n"))
		}
	})
}

// TestBuildGroupRecipe_SubdomainReference_InAProjectVariable_Said: every
// service reads a project variable, so a reference to one service's public
// address there resolves only in the services started after it — no
// priority puts one service before all the others. A reference a person
// wrote there stays theirs, and the pass says so.
func TestBuildGroupRecipe_SubdomainReference_InAProjectVariable_Said(t *testing.T) {
	t.Parallel()
	inputs := flipChangeTwoInputs()
	inputs.ProjectEnvs = []ProjectEnvVar{{Key: "FLIP_URL", Value: "${flipstage_zeropsSubdomain}"}}
	layout, warnings, err := BuildGroupRecipe(inputs)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	if got, _ := tierVaultValue(t, tierBody(t, layout.Tiers, "Stage"), "", "FLIP_URL"); got != "${flip_zeropsSubdomain}" {
		t.Errorf("Stage: FLIP_URL = %q, want the person's reference, renamed", got)
	}
	if !slices.ContainsFunc(warnings, func(w string) bool {
		return strings.Contains(w, "FLIP_URL") && strings.Contains(w, "every service")
	}) {
		t.Errorf("no warning says every service reads FLIP_URL:\n%s", strings.Join(warnings, "\n"))
	}
}
