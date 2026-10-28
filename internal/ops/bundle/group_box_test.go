// Tests for: group recipe — the Zerops Box a tier carries for the runtimes
// that read it.
package bundle

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/schema"
)

// setupVars is one zerops.yaml setup the Box tests write: its name, the
// setup it extends ("" for none), its run variables and its build ones.
type setupVars struct {
	name, extends string
	vars, build   map[string]string
}

// zeropsYAMLWithVars writes a zerops.yaml declaring the setups, each with its
// variables.
func zeropsYAMLWithVars(setups ...setupVars) string {
	var b strings.Builder
	b.WriteString("zerops:\n")
	for _, s := range setups {
		fmt.Fprintf(&b, "  - setup: %s\n", s.name)
		if s.extends != "" {
			fmt.Fprintf(&b, "    extends: %s\n", s.extends)
		}
		if len(s.build) > 0 {
			b.WriteString("    build:\n      base: go@1.22\n      envVariables:\n")
			for _, key := range slices.Sorted(maps.Keys(s.build)) {
				fmt.Fprintf(&b, "        %s: %q\n", key, s.build[key])
			}
		}
		b.WriteString("    run:\n      base: go@1.22\n      start: ./runner\n")
		if len(s.vars) > 0 {
			b.WriteString("      envVariables:\n")
			for _, key := range slices.Sorted(maps.Keys(s.vars)) {
				fmt.Fprintf(&b, "        %s: %q\n", key, s.vars[key])
			}
		}
	}
	return b.String()
}

// boxRunnerInputs is Noola's runner as the Mate's project runs it: a pair
// built from one repository — its dev service, or its part's dev pool, and
// its stage building runnerprod — beside the broker it reads and the
// project's Box.
func boxRunnerInputs(body string, pool bool) GroupRecipeInputs {
	runtime := GroupRuntime{
		DevHostname:    "runnerdev",
		Stages:         []GroupStage{{Hostname: "runnerstage", SetupName: "runnerprod", ServiceType: "alpine/go@1"}},
		ServiceType:    "alpine/go@1",
		RepoURL:        "https://hq.example/git/org/app/noola.git",
		SetupName:      "runnerdev",
		ZeropsYAMLBody: body,
	}
	if pool {
		runtime.DevHostname, runtime.DevPool, runtime.ServiceType, runtime.SetupName = "runnerpool", true, "swarm@1", "runnerpool"
	}
	return GroupRecipeInputs{
		Name:            "noola",
		MateProjectName: "noola-mate",
		Runtimes:        []GroupRuntime{runtime},
		ManagedServices: []ManagedServiceEntry{{Hostname: "broker", Type: "nats:single@2.12"}},
		Boxes:           []GroupBox{{Hostname: "box", ServiceType: "ubuntu/zbox@1"}},
	}
}

// boxReads is how Noola's runner reads its Box: the workspace API by its
// address on the project network, and the key by reference.
var boxReads = map[string]string{
	"BOX_API_URL": "http://box:8080",
	"BOX_API_KEY": "${box_ZBOX_WORKSPACE_API_KEY}",
}

// groupTierDirs are the tiers BuildGroupRecipe composes, as directories.
var groupTierDirs = []string{"0 — AI Agent", "3 — Stage", "4 — Small Production"}

// A Box is infrastructure an app uses, so the recipe used to leave every Box
// out — and Noola's runner, whose setups name the project's Box, came up in
// every environment made from a tier with nothing to run a job on. A tier
// carries a Box exactly where a runtime it carries reads it: the setup the
// tier builds for that runtime names the Box in a variable — a
// `${box_key}` reference, or the Box as the host of an address on the
// project network (`http://box:8080`, `box.zerops:8081`, `box:8080`). A Box
// nothing reads stays out, and the composer says so. Spec:
// spec-workflows.md §8 PS-3.
func TestBuildGroupRecipe_Box_CarriedWhereARuntimeReadsIt(t *testing.T) {
	t.Parallel()
	broker := map[string]string{"NATS_PORT": "${broker_port}"}
	tests := []struct {
		name string
		body string
		pool bool
		// wantTiers are the tiers that carry the Box.
		wantTiers []string
	}{
		{
			name: "both setups read it, by address and by reference",
			body: zeropsYAMLWithVars(
				setupVars{name: "runnerdev", vars: boxReads},
				setupVars{name: "runnerprod", vars: boxReads}),
			wantTiers: groupTierDirs,
		},
		{
			name: "only the dev setup reads it",
			body: zeropsYAMLWithVars(
				setupVars{name: "runnerdev", vars: boxReads},
				setupVars{name: "runnerprod", vars: broker}),
			wantTiers: []string{"0 — AI Agent"},
		},
		{
			name: "only the stage's setup reads it, by reference",
			body: zeropsYAMLWithVars(
				setupVars{name: "runnerdev", vars: broker},
				setupVars{name: "runnerprod", vars: map[string]string{"BOX_API_KEY": "${box_ZBOX_WORKSPACE_API_KEY}"}}),
			wantTiers: groupTierDirs,
		},
		{
			name: "an address on the project network under its internal name",
			body: zeropsYAMLWithVars(
				setupVars{name: "runnerdev", vars: broker},
				setupVars{name: "runnerprod", vars: map[string]string{"SANDBOX_INGRESS": "http://box.zerops:8081"}}),
			wantTiers: groupTierDirs,
		},
		{
			name: "a host and port alone",
			body: zeropsYAMLWithVars(
				setupVars{name: "runnerdev", vars: broker},
				setupVars{name: "runnerprod", vars: map[string]string{"BOX_ADDR": "box:8080"}}),
			wantTiers: groupTierDirs,
		},
		{
			name: "the setup the stage's extends reads it",
			body: zeropsYAMLWithVars(
				setupVars{name: "runnerbase", vars: boxReads},
				setupVars{name: "runnerdev", vars: broker},
				setupVars{name: "runnerprod", extends: "runnerbase", vars: broker}),
			wantTiers: groupTierDirs,
		},
		{
			name: "a part's dev pool reads it, its stage does not",
			body: zeropsYAMLWithVars(
				setupVars{name: "runnerpool", vars: boxReads},
				setupVars{name: "runnerprod", vars: broker}),
			pool:      true,
			wantTiers: []string{"0 — AI Agent"},
		},
		{
			name: "nothing reads it",
			body: zeropsYAMLWithVars(
				setupVars{name: "runnerdev", vars: broker},
				setupVars{name: "runnerprod", vars: broker}),
		},
		{
			name: "a word, another host, a public address or a domain is no read",
			body: zeropsYAMLWithVars(
				setupVars{name: "runnerdev", vars: broker},
				setupVars{name: "runnerprod", vars: map[string]string{
					"SANDBOX_KIND": "box",
					"OTHER_API":    "http://boxes:8080",
					"OTHER_KEY":    "${boxes_apiKey}",
					"PUBLIC_URL":   "https://box-${zeropsSubdomainHost}-8080.prg1.zerops.app",
					"DOCS_URL":     "https://box.example.com/docs",
				}}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			layout, warnings, err := BuildGroupRecipe(boxRunnerInputs(tt.body, tt.pool))
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
			saysNotCarried := warningsContain(warnings, `Zerops Box "box"`) && warningsContain(warnings, "not carried")
			if len(tt.wantTiers) == 0 && !saysNotCarried {
				t.Errorf("warnings %v do not say the Box nothing reads is not carried", warnings)
			}
			if len(tt.wantTiers) > 0 && saysNotCarried {
				t.Errorf("warnings %v say a carried Box is not carried", warnings)
			}
		})
	}
}

// A Box is written as a Box import takes it: its hostname, its type and the
// managed services' priority — it is created first, before every runtime
// that reads it — and its public access as it runs. No mode, no HA variant
// and no scale on any tier: a Box is one VM, and Small Production's promotion
// of the managed services never reaches it. Every tier passes the embedded
// import schema, which lists the Box type.
func TestBuildGroupRecipe_Box_WrittenAsABoxImport(t *testing.T) {
	t.Parallel()
	body := zeropsYAMLWithVars(
		setupVars{name: "runnerdev", vars: boxReads},
		setupVars{name: "runnerprod", vars: boxReads})
	tests := []struct {
		name      string
		subdomain bool
		wantKeys  []string
	}{
		{name: "public access off", wantKeys: []string{"hostname", "priority", "type"}},
		{name: "public access on", subdomain: true, wantKeys: []string{"enableSubdomainAccess", "hostname", "priority", "type"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inputs := boxRunnerInputs(body, false)
			inputs.Boxes[0].SubdomainEnabled = tt.subdomain
			layout, _, err := BuildGroupRecipe(inputs)
			if err != nil {
				t.Fatalf("BuildGroupRecipe: %v", err)
			}
			files := groupFiles(t, layout)
			for _, dir := range groupTierDirs {
				if errs := schema.ValidateImportYAML(files[dir+"/import.yaml"]); len(errs) > 0 {
					t.Errorf("%s fails the import schema: %v", dir, errs)
				}
				box, ok := serviceEntries(t, tierDoc(t, files, dir))["box"]
				if !ok {
					t.Fatalf("%s carries no Box", dir)
				}
				if keys := slices.Sorted(maps.Keys(box)); !slices.Equal(keys, tt.wantKeys) {
					t.Errorf("%s: Box keys = %v, want %v", dir, keys, tt.wantKeys)
				}
				if box["type"] != "ubuntu/zbox@1" {
					t.Errorf("%s: Box type = %v, want ubuntu/zbox@1 as it runs", dir, box["type"])
				}
				if box["priority"] != managedPriority {
					t.Errorf("%s: Box priority = %v, want %d — before the runtimes that read it", dir, box["priority"], managedPriority)
				}
				if tt.subdomain && box["enableSubdomainAccess"] != true {
					t.Errorf("%s: Box enableSubdomainAccess = %v, want true as it runs", dir, box["enableSubdomainAccess"])
				}
			}
		})
	}
}

// A group environment names a part after its stage's stem, and a part can
// be named like the Box a runtime reads: Zerops Apps' `box` part runs as
// `boxdev`/`boxstage` in its Mate, and a Box named `box` beside it would
// make a stage with two services of one hostname, which the import refuses
// whole. The Box keeps its name — it is what the runtimes' setups read — and
// the part is left out of the group environments and said, as a part named
// like a managed service is.
func TestBuildGroupRecipe_Box_PartNamedLikeTheBox_LeftOutAndSaid(t *testing.T) {
	t.Parallel()
	inputs := boxRunnerInputs(zeropsYAMLWithVars(
		setupVars{name: "runnerdev", vars: boxReads},
		setupVars{name: "runnerprod", vars: boxReads}), false)
	inputs.Runtimes = append(inputs.Runtimes, GroupRuntime{
		DevHostname:    "boxdev",
		Stages:         []GroupStage{{Hostname: "boxstage", SetupName: "boxprod", ServiceType: "alpine/static"}},
		ServiceType:    "alpine/static",
		RepoURL:        "https://hq.example/git/org/app/apps.git",
		SetupName:      "boxdev",
		ZeropsYAMLBody: zeropsYAMLWithVars(setupVars{name: "boxdev"}, setupVars{name: "boxprod"}),
	})
	layout, warnings, err := BuildGroupRecipe(inputs)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	files := groupFiles(t, layout)
	for _, dir := range groupTierDirs {
		raw, _ := tierDoc(t, files, dir)["services"].([]any)
		seen := map[string]int{}
		for _, item := range raw {
			entry, _ := item.(map[string]any)
			host, _ := entry["hostname"].(string)
			seen[host]++
		}
		if seen["box"] != 1 {
			t.Errorf("%s names %q %d times, want once — the Box", dir, "box", seen["box"])
		}
		if box := serviceEntries(t, tierDoc(t, files, dir))["box"]; box["type"] != "ubuntu/zbox@1" {
			t.Errorf("%s: box = %v, want the Box", dir, box)
		}
	}
	if !warningsContain(warnings, `part "boxprod" of "boxdev" is left out of the group environments: another of their services is already named "box"`) {
		t.Errorf("warnings %v do not say the part named like the Box is left out", warnings)
	}
}

// A runtime reads a Box through its own variables too, which a tier carries
// in the runtime's vault — and a runtime of no pair has nothing else: the
// recipe never reads a utility's zerops.yaml. So a Box goes where the vault
// that reads it goes, written as the tier writes it: a stage's own variable
// takes it into every tier that runs the stage's part, a utility a stage and
// a production run takes it there too, and a runtime that serves the Mate
// itself — wired to an address of the Mate's own project, or its dev tooling
// — takes it onto the AI Agent tier alone, the only tier that runs it. A
// value the tier generates, never writes, reads nothing. Spec:
// spec-workflows.md §8 PS-3.
func TestBuildGroupRecipe_Box_ReadByARuntimesOwnVariable(t *testing.T) {
	t.Parallel()
	plain := zeropsYAMLWithVars(
		setupVars{name: "runnerdev", vars: map[string]string{"NATS_PORT": "${broker_port}"}},
		setupVars{name: "runnerprod", vars: map[string]string{"NATS_PORT": "${broker_port}"}})
	keyRef := ProjectEnvVar{Key: "BOX_API_KEY", Value: "${box_ZBOX_WORKSPACE_API_KEY}", Sensitive: true}
	utility := func(host, repo string, envs ...ProjectEnvVar) GroupUtility {
		return GroupUtility{Hostname: host, ServiceType: "alpine@3.21", BuildFromGit: "https://github.com/acme/" + repo, ServiceEnvs: envs}
	}
	tests := []struct {
		name      string
		mutate    func(*GroupRecipeInputs)
		wantTiers []string
	}{
		{
			name:      "a stage's own variable reads it by reference",
			mutate:    func(in *GroupRecipeInputs) { in.Runtimes[0].Stages[0].ServiceEnvs = []ProjectEnvVar{keyRef} },
			wantTiers: groupTierDirs,
		},
		{
			name: "a utility a stage and a production run reads it by address",
			mutate: func(in *GroupRecipeInputs) {
				in.Utilities = []GroupUtility{utility("worker", "worker", ProjectEnvVar{Key: "BOX_API_URL", Value: "http://box:8080"})}
			},
			wantTiers: groupTierDirs,
		},
		{
			name: "a runtime wired to the Mate's own project reads it",
			mutate: func(in *GroupRecipeInputs) {
				in.MateSubdomainHost = "1df2.prg1.zerops.app"
				in.Utilities = []GroupUtility{utility("agent", "agent-runner",
					ProjectEnvVar{Key: "SERVER", Value: "https://runnerdev-1df2-8080.prg1.zerops.app"}, keyRef)}
			},
			wantTiers: []string{"0 — AI Agent"},
		},
		{
			name: "the Mate's dev tooling reads it",
			mutate: func(in *GroupRecipeInputs) {
				in.Utilities = []GroupUtility{utility("mailpit", "recipe-mailpit", ProjectEnvVar{Key: "BOX_API_URL", Value: "http://box:8080"})}
			},
			wantTiers: []string{"0 — AI Agent"},
		},
		{
			name: "a value the tier generates reads nothing",
			mutate: func(in *GroupRecipeInputs) {
				in.Runtimes[0].Stages[0].ServiceEnvs = []ProjectEnvVar{{Key: "BOX_DSN", Value: "http://admin:placeholder-password@box:8080", Sensitive: true}}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inputs := boxRunnerInputs(plain, false)
			tt.mutate(&inputs)
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
				t.Errorf("tiers carrying the Box = %v, want %v (warnings %v)", carried, tt.wantTiers, warnings)
			}
		})
	}
}
