package workflow

import (
	"strings"
	"testing"
)

// TestModeExpansionAtom_FiresOnlyForSingleSlotModes pins the axis filter on
// `develop-mode-expansion.md` — the atom must fire for services in dev or
// simple mode (where a stage pair is missing and expansion is meaningful)
// and must NOT fire for standard mode (which already has a stage pair).
//
// Regression guard: without the modes axis, this atom would surface on
// standard-mode services and advise the agent to "add a stage" that
// already exists.
func TestModeExpansionAtom_FiresOnlyForSingleSlotModes(t *testing.T) {
	t.Parallel()

	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	cases := []struct {
		name     string
		mode     Mode
		wantFire bool
	}{
		{"dev_fires", ModeDev, true},
		{"simple_fires", ModeSimple, true},
		{"standard_suppressed", ModeStandard, false},
		{"stage_suppressed", ModeStage, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: RuntimeDynamic, Mode: tt.mode,
					Strategy: "push-dev",
				}},
			}
			bodies, err := Synthesize(env, corpus)
			if err != nil {
				t.Fatalf("Synthesize: %v", err)
			}
			joined := strings.Join(bodies, "\n")
			fired := strings.Contains(joined, "Mode expansion — add a stage pair")
			if fired != tt.wantFire {
				t.Errorf("mode=%s: expansion atom fired=%v, want %v", tt.mode, fired, tt.wantFire)
			}
		})
	}
}
