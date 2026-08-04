package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOnboardingScenarios_ExistAndParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		id                   string
		wantSeed             SeedMode
		wantPrompt           string
		promptContains       []string
		wantFixture          string
		preseedContains      string
		personaContains      []string
		expectationsContains []string
	}{
		{
			id:                   "onboard-trigger-fresh",
			wantSeed:             ModeEmpty,
			wantPrompt:           "onboard me to Zerops",
			expectationsContains: []string{`zerops://playbooks/onboarding`, "greeting", "no status or discover call before it", "**Build something**", "**Try a ready-made recipe**", "**What are Zerops & ZCP?**", "Or just tell me what you want"},
		},
		{
			id:                   "onboard-trigger-variant",
			wantSeed:             ModeEmpty,
			promptContains:       []string{"get me started with Zerops"},
			expectationsContains: []string{"greeting", "no status or discover call before it", "**Build something**", "**Try a ready-made recipe**", "**What are Zerops & ZCP?**", "Or just tell me what you want"},
		},
		{
			id:                   "onboard-trigger-negative",
			wantSeed:             ModeEmpty,
			promptContains:       []string{"PostgreSQL"},
			expectationsContains: []string{"No onboarding playbook", "MUST NOT fetch", "normal routing", "**Build something**", "**Try a ready-made recipe**", "**What are Zerops & ZCP?**"},
		},
		{
			id:                   "onboard-populated",
			wantSeed:             ModeDeployed,
			wantFixture:          "fixtures/nodejs-standard-deployed.yaml",
			personaContains:      []string{"Try a ready-made recipe", "don't set anything up yet"},
			expectationsContains: []string{"Continue this project", "**Try a ready-made recipe**", "Withheld consent", "nothing is provisioned"},
		},
		{
			id:                   "onboard-guided-on",
			wantSeed:             ModeEmpty,
			preseedContains:      "onboard-guided-on.sh",
			personaContains:      []string{"choose exactly", "Try a ready-made recipe"},
			expectationsContains: []string{"get one plain", "standard bootstrap recipe route", "no state read", "**Build something**", "**Try a ready-made recipe**", "**What are Zerops & ZCP?**"},
		},
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
			if tt.wantPrompt != "" && sc.Prompt != tt.wantPrompt {
				t.Errorf("Prompt: got %q, want %q", sc.Prompt, tt.wantPrompt)
			}
			for _, needle := range tt.promptContains {
				if !strings.Contains(sc.Prompt, needle) {
					t.Errorf("Prompt: missing %q in %q", needle, sc.Prompt)
				}
			}
			if tt.wantFixture != "" && sc.Fixture != tt.wantFixture {
				t.Errorf("Fixture: got %q, want %q", sc.Fixture, tt.wantFixture)
			}
			if tt.preseedContains != "" && !strings.Contains(sc.PreseedScript, tt.preseedContains) {
				t.Errorf("PreseedScript: missing %q in %q", tt.preseedContains, sc.PreseedScript)
			}
			for _, needle := range tt.personaContains {
				if !strings.Contains(sc.UserPersona, needle) {
					t.Errorf("UserPersona: missing %q", needle)
				}
			}

			var sb strings.Builder
			sb.WriteString(sc.Description)
			for _, friction := range sc.NotableFriction {
				sb.WriteString("\n")
				sb.WriteString(friction.Description)
			}
			expectations := sb.String()
			for _, needle := range tt.expectationsContains {
				if !strings.Contains(expectations, needle) {
					t.Errorf("expectations: missing %q", needle)
				}
			}
		})
	}
}
