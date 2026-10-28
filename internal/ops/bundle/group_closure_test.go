// Tests for: group recipe — a tier never ships a runtime whose setup reads a
// service the tier does not carry.
package bundle

import (
	"errors"
	"strings"
	"testing"
)

// closureInputs is the runner beside the services of its Mate's project: the
// broker and Box it reads, the Mate's mail catcher (a utility only the AI
// Agent tier carries), and what the recipe never carries — a runtime it
// leaves out, a Swarm pool, zcp itself.
func closureInputs(dev, prod setupVars) GroupRecipeInputs {
	inputs := boxRunnerInputs(zeropsYAMLWithVars(dev, prod), false)
	inputs.Utilities = []GroupUtility{{Hostname: "mailpit", ServiceType: "alpine@3.21", BuildFromGit: "https://github.com/zerops-recipe-apps/mailpit-app"}}
	inputs.ProjectHostnames = []string{"broker", "box", "devpool", "mailpit", "orphan", "runnerdev", "runnerstage", "zcp"}
	return inputs
}

// A tier that carries a runtime but not a service the runtime's setup reads
// makes environments whose app does not start, and a tier on the group
// repository stays there. So the composer refuses the whole recipe before
// anything is proposed — never a warning buried among the others — and says,
// for each tier, which runtime builds which setup, which variable of it
// reads what, and that the Mate's project runs the service the tier lacks.
// Only a hostname the Mate's project runs is a service: a read is a
// `${host_key}` reference or the service as the host of an address on the
// project network, in the build or run variables of the setup the tier
// builds for the runtime. Spec: spec-workflows.md §8 PS-3a.
func TestBuildGroupRecipe_TierClosure_ReadOfAServiceTheTierLacks_Fails(t *testing.T) {
	t.Parallel()
	plain := map[string]string{"NATS_PORT": "${broker_port}"}
	tests := []struct {
		name      string
		dev, prod setupVars
		// wantTiers name each tier the error names, beside the rest it says.
		wantTiers, wantNot []string
		want               []string
	}{
		{
			name:      "a stage's setup reads the Mate's mail catcher",
			dev:       setupVars{name: "runnerdev", vars: plain},
			prod:      setupVars{name: "runnerprod", vars: map[string]string{"MAIL_URL": "smtp://mailpit:1025"}},
			wantTiers: []string{`tier "Stage"`, `tier "Small Production"`},
			wantNot:   []string{`tier "AI Agent"`},
			want:      []string{`runtime "runner"`, `setup "runnerprod"`, "run.envVariables.MAIL_URL", `"mailpit"`},
		},
		{
			name:      "a stage's setup reads its stage by the Mate's hostname",
			dev:       setupVars{name: "runnerdev", vars: plain},
			prod:      setupVars{name: "runnerprod", vars: map[string]string{"SELF_URL": "${runnerstage_zeropsSubdomain}"}},
			wantTiers: []string{`tier "Stage"`, `tier "Small Production"`},
			wantNot:   []string{`tier "AI Agent"`},
			want:      []string{`setup "runnerprod"`, "run.envVariables.SELF_URL", `"runnerstage"`},
		},
		{
			name:      "the dev setup reads a runtime the recipe leaves out",
			dev:       setupVars{name: "runnerdev", vars: map[string]string{"ORPHAN_URL": "http://orphan:3000"}},
			prod:      setupVars{name: "runnerprod", vars: plain},
			wantTiers: []string{`tier "AI Agent"`},
			wantNot:   []string{`tier "Stage"`, `tier "Small Production"`},
			want:      []string{`runtime "runnerdev"`, `setup "runnerdev"`, "run.envVariables.ORPHAN_URL", `"orphan"`},
		},
		{
			name:      "a stage's build reads a Swarm pool",
			dev:       setupVars{name: "runnerdev", vars: plain},
			prod:      setupVars{name: "runnerprod", vars: plain, build: map[string]string{"POOL_TOKEN": "${devpool_API_TOKEN}"}},
			wantTiers: []string{`tier "AI Agent"`, `tier "Stage"`, `tier "Small Production"`},
			want:      []string{`runtime "runnerstage"`, `runtime "runner"`, "build.envVariables.POOL_TOKEN", `"devpool"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			layout, warnings, err := BuildGroupRecipe(closureInputs(tt.dev, tt.prod))
			if err == nil {
				t.Fatalf("composed %d tiers, want the recipe refused (warnings %v)", len(layout.Tiers), warnings)
			}
			msg := err.Error()
			for _, want := range append(append([]string{"does not carry"}, tt.wantTiers...), tt.want...) {
				if !strings.Contains(msg, want) {
					t.Errorf("error does not say %s:\n%s", want, msg)
				}
			}
			for _, not := range tt.wantNot {
				if strings.Contains(msg, not) {
					t.Errorf("error names %s, which carries the service:\n%s", not, msg)
				}
			}
		})
	}
}

// A recipe whose runtimes read only what their tiers carry composes as
// before: Noola's runner and the data, broker and Box it reads; a project
// variable, or a name no service of the Mate's project has — a group
// environment's own runtime among them; a public address; a bare word; and
// a dev setup reading what only the AI Agent tier carries, which carries it.
func TestBuildGroupRecipe_TierClosure_EveryReadCarried_Composes(t *testing.T) {
	t.Parallel()
	plain := map[string]string{"NATS_PORT": "${broker_port}"}
	tests := []struct {
		name      string
		dev, prod setupVars
	}{
		{
			name: "Noola's runner: the broker and the Box",
			dev:  setupVars{name: "runnerdev", vars: boxReads},
			prod: setupVars{name: "runnerprod", vars: boxReads},
		},
		{
			name: "a project variable, a name no service has, a public address, a word",
			dev:  setupVars{name: "runnerdev", vars: plain},
			prod: setupVars{name: "runnerprod", vars: map[string]string{
				"HOST":      "${zeropsSubdomainHost}",
				"SELF_API":  "http://runner:3000",
				"SELF_KEY":  "${runner_apiKey}",
				"MAIL_UI":   "https://mailpit-${zeropsSubdomainHost}-8025.prg1.zerops.app",
				"SMTP_HOST": "mailpit",
			}},
		},
		{
			name: "the dev setup alone reads the Mate's mail catcher",
			dev:  setupVars{name: "runnerdev", vars: map[string]string{"MAIL_URL": "smtp://mailpit:1025"}},
			prod: setupVars{name: "runnerprod", vars: plain},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			layout, _, err := BuildGroupRecipe(closureInputs(tt.dev, tt.prod))
			if err != nil {
				t.Fatalf("BuildGroupRecipe: %v", err)
			}
			if len(layout.Tiers) != len(groupTierDirs) {
				t.Errorf("tiers = %d, want %d", len(layout.Tiers), len(groupTierDirs))
			}
		})
	}
}

// The check reads what a tier carries, so a runtime a tier does not carry
// reads nothing there: a runtime that serves the Mate itself runs on the AI
// Agent tier alone, and whatever its variables name never refuses Stage or
// Small Production — nor does a runtime of no pair anywhere, since the
// recipe reads no zerops.yaml of one.
func TestBuildGroupRecipe_TierClosure_MateOwnRuntimesReadsNeverRefuse(t *testing.T) {
	t.Parallel()
	plain := map[string]string{"NATS_PORT": "${broker_port}"}
	inputs := closureInputs(setupVars{name: "runnerdev", vars: plain}, setupVars{name: "runnerprod", vars: plain})
	inputs.MateSubdomainHost = "1df2.prg1.zerops.app"
	inputs.Utilities = append(inputs.Utilities, GroupUtility{
		Hostname: "agent", ServiceType: "alpine@3.21", BuildFromGit: "https://github.com/acme/agent-runner",
		ServiceEnvs: []ProjectEnvVar{
			{Key: "SERVER", Value: "https://runnerdev-1df2-8080.prg1.zerops.app"},
			{Key: "MAIL_URL", Value: "smtp://mailpit:1025"},
			{Key: "ORPHAN_URL", Value: "http://orphan:3000"},
		},
	})
	inputs.ProjectHostnames = append(inputs.ProjectHostnames, "agent")
	layout, _, err := BuildGroupRecipe(inputs)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	if len(layout.Tiers) != len(groupTierDirs) {
		t.Errorf("tiers = %d, want %d", len(layout.Tiers), len(groupTierDirs))
	}
}

// The pass makes a closure refusal a required step of the Mate's setup and no
// other refusal, so the refusal tells itself apart by its type, never by its
// words — which stay the composer's, the reads it names among them: one per
// tier, each naming the tier, the runtime, the setup, the variable and the
// service. A refusal for anything else is not one.
func TestBuildGroupRecipe_TierClosure_RefusalIsATierClosureError(t *testing.T) {
	t.Parallel()
	plain := map[string]string{"NATS_PORT": "${broker_port}"}
	_, _, err := BuildGroupRecipe(closureInputs(setupVars{name: "runnerdev", vars: plain},
		setupVars{name: "runnerprod", vars: map[string]string{"MAIL_URL": "smtp://mailpit:1025"}}))
	var unclosed *TierClosureError
	if !errors.As(err, &unclosed) {
		t.Fatalf("err = %v, want a *TierClosureError", err)
	}
	want := []string{
		`tier "Stage": runtime "runner" builds setup "runnerprod", whose run.envVariables.MAIL_URL names "mailpit"`,
		`tier "Small Production": runtime "runner" builds setup "runnerprod", whose run.envVariables.MAIL_URL names "mailpit"`,
	}
	if strings.Join(unclosed.Reads, "\n") != strings.Join(want, "\n") {
		t.Errorf("reads = %q, want %q", unclosed.Reads, want)
	}
	if !strings.Contains(err.Error(), strings.Join(want, "; ")) || !strings.Contains(err.Error(), "or stop the setup reading it") {
		t.Errorf("the refusal does not say the reads and the way out as the composer words them:\n%s", err)
	}

	other := closureInputs(setupVars{name: "runnerdev", vars: plain}, setupVars{name: "runnerprod", vars: plain})
	other.Runtimes[0].ServiceType = ""
	if _, _, err := BuildGroupRecipe(other); err == nil || errors.As(err, &unclosed) {
		t.Errorf("err = %v, want a refusal that is no *TierClosureError", err)
	}
}
