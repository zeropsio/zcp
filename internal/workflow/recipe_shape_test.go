package workflow

import (
	"strings"
	"testing"
)

func TestInferRecipeShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		yaml         string
		wantMode     string
		wantRuntimes int
	}{
		{
			name: "standard_dev_plus_prod",
			yaml: `services:
  - hostname: appdev
    type: nodejs@22
    zeropsSetup: dev
  - hostname: appstage
    type: nodejs@22
    zeropsSetup: prod
  - hostname: db
    type: postgresql@18
`,
			wantMode:     "standard",
			wantRuntimes: 2,
		},
		{
			name: "simple_single_prod",
			yaml: `services:
  - hostname: app
    type: nodejs@22
    zeropsSetup: prod
  - hostname: db
    type: postgresql@18
`,
			wantMode:     "simple",
			wantRuntimes: 1,
		},
		{
			name: "dev_single_dev",
			yaml: `services:
  - hostname: app
    type: nodejs@22
    zeropsSetup: dev
`,
			wantMode:     "dev",
			wantRuntimes: 1,
		},
		{
			name: "managed_only_no_runtime",
			yaml: `services:
  - hostname: db
    type: postgresql@18
`,
			wantMode:     "",
			wantRuntimes: 0,
		},
		{
			name:         "invalid_yaml",
			yaml:         "::: not yaml",
			wantMode:     "",
			wantRuntimes: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mode, count := InferRecipeShape(tt.yaml)
			if mode != tt.wantMode {
				t.Errorf("mode: got %q, want %q", mode, tt.wantMode)
			}
			if count != tt.wantRuntimes {
				t.Errorf("runtimes: got %d, want %d", count, tt.wantRuntimes)
			}
		})
	}
}

func TestValidateBootstrapRecipeMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		match    *RecipeMatch
		targets  []BootstrapTarget
		wantErr  bool
		errMatch string
	}{
		{
			name:  "nil_match_no_check",
			match: nil,
			targets: []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: "app", Type: "nodejs@22", BootstrapMode: "simple"}},
			},
		},
		{
			name:  "empty_mode_no_check",
			match: &RecipeMatch{Slug: "foo", Mode: ""},
			targets: []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: "app", Type: "nodejs@22", BootstrapMode: "simple"}},
			},
		},
		{
			name:  "standard_matches_standard",
			match: &RecipeMatch{Slug: "nestjs-minimal", Mode: "standard"},
			targets: []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "standard"}},
			},
		},
		{
			name:  "standard_vs_simple_rejected",
			match: &RecipeMatch{Slug: "nestjs-minimal", Mode: "standard"},
			targets: []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: "app", Type: "nodejs@22", BootstrapMode: "simple"}},
			},
			wantErr:  true,
			errMatch: "recipe \"nestjs-minimal\" is standard mode",
		},
		{
			name:  "simple_vs_standard_rejected",
			match: &RecipeMatch{Slug: "nextjs-ssr-hello-world", Mode: "simple"},
			targets: []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "standard"}},
			},
			wantErr:  true,
			errMatch: "recipe \"nextjs-ssr-hello-world\" is simple mode",
		},
		{
			name:  "missing_mode_on_target_defaults_to_standard_matches",
			match: &RecipeMatch{Slug: "r", Mode: "standard"},
			targets: []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"}}, // empty mode defaults to standard
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateBootstrapRecipeMode(tt.match, tt.targets)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				if tt.errMatch != "" && !strings.Contains(err.Error(), tt.errMatch) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errMatch)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
