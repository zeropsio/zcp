package eval

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOnboardingScenarios_ExistAndParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		id       string
		wantSeed SeedMode
	}{
		{id: "onboard-trigger-fresh", wantSeed: ModeEmpty},
		{id: "onboard-trigger-variant", wantSeed: ModeEmpty},
		{id: "onboard-trigger-negative", wantSeed: ModeEmpty},
		{id: "onboard-populated", wantSeed: ModeDeployed},
		{id: "onboard-guided-on", wantSeed: ModeEmpty},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join("..", "..", "eval", "behavioral", "scenarios", tt.id+".md")
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("scenario file %q: %v", path, err)
			}

			sc, err := ParseScenario(path)
			if err != nil {
				t.Fatalf("ParseScenario(%q): %v", path, err)
			}
			if sc.ID != tt.id {
				t.Errorf("ID: got %q, want %q", sc.ID, tt.id)
			}
			if sc.Seed != tt.wantSeed {
				t.Errorf("Seed: got %q, want %q", sc.Seed, tt.wantSeed)
			}
		})
	}
}
