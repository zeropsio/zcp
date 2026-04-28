package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestModeExpansionAtom_FiresOnlyForSingleSlotModes pins the axis filter on
// `develop-mode-expansion.md` — the atom must fire for deployed services in
// dev or simple mode (where a stage pair is missing and expansion is
// meaningful) and must NOT fire for standard mode (already has a stage pair)
// nor during the first-deploy branch (expansion is a post-deploy decision).
//
// Regression guard: without the modes + deployStates axes, this atom would
// either surface on standard-mode services and advise the agent to "add a
// stage" that already exists, or fire during first-deploy before the current
// single-slot setup is validated.
func TestModeExpansionAtom_FiresOnlyForSingleSlotModes(t *testing.T) {
	t.Parallel()

	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	cases := []struct {
		name     string
		mode     topology.Mode
		deployed bool
		wantFire bool
	}{
		{"dev_deployed_fires", topology.ModeDev, true, true},
		{"simple_deployed_fires", topology.ModeSimple, true, true},
		{"dev_never_deployed_suppressed", topology.ModeDev, false, false},
		{"simple_never_deployed_suppressed", topology.ModeSimple, false, false},
		{"standard_suppressed", topology.ModeStandard, true, false},
		{"stage_suppressed", topology.ModeStage, true, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: tt.mode,
					CloseDeployMode: topology.CloseModeAuto, Bootstrapped: true, Deployed: tt.deployed,
				}},
			}
			bodies, err := SynthesizeBodies(env, corpus)
			if err != nil {
				t.Fatalf("Synthesize: %v", err)
			}
			joined := strings.Join(bodies, "\n")
			fired := strings.Contains(joined, "Mode expansion — add a stage pair")
			if fired != tt.wantFire {
				t.Errorf("mode=%s deployed=%v: expansion atom fired=%v, want %v", tt.mode, tt.deployed, fired, tt.wantFire)
			}
		})
	}
}
