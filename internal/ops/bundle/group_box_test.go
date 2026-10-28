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
// setup it extends ("" for none) and its run variables.
type setupVars struct {
	name, extends string
	vars          map[string]string
}

// zeropsYAMLWithVars writes a zerops.yaml declaring the setups, each with its
// run variables.
func zeropsYAMLWithVars(setups ...setupVars) string {
	var b strings.Builder
	b.WriteString("zerops:\n")
	for _, s := range setups {
		fmt.Fprintf(&b, "  - setup: %s\n", s.name)
		if s.extends != "" {
			fmt.Fprintf(&b, "    extends: %s\n", s.extends)
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
