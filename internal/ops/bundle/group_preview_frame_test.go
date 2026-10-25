package bundle

import (
	"strings"
	"testing"
)

// Mate's preview pane frames a Mate's stage. The stage builds the setup the
// application's stage and production build, so a Mate lets the pane frame the
// stage through a variant of that setup — one that `extends` it and widens
// `frame-ancestors` by Mate's origins — and deploys the stage with it. The
// AI Agent tier re-creates a Mate, so its stage half keeps the variant; a
// group environment builds the setup the variant extends, and production keeps
// the app's strict framing.
const (
	previewFrameDev = `  - setup: dev
    build:
      base: nodejs@22
    run:
      base: nodejs@22
      start: zsc noop --silent
`
	previewFrameStaticProd = `  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm run build
      deployFiles: dist/~
    run:
      base: static
      routing:
        headers:
          - for: "/*"
            values:
              X-Frame-Options: "'DENY' always"
              Content-Security-Policy: '"frame-ancestors ''none''" always'
`
	previewFrameStaticVariant = `  - setup: preview
    extends: prod
    run:
      routing:
        headers:
          - for: "/*"
            values:
              Content-Security-Policy: '"frame-ancestors https://mate.zerops.io https://*.zerops.app" always'
`
	previewFrameEnvProd = `  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
    run:
      base: nodejs@22
      start: npm start
`
	previewFrameEnvVariant = `  - setup: preview
    extends: prod
    run:
      envVariables:
        FRAME_ANCESTORS: "'self' https://mate.zerops.io https://*.zerops.app"
`
	previewFrameNoExtends = `  - setup: preview
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
    run:
      base: nodejs@22
      start: npm start
      envVariables:
        FRAME_ANCESTORS: "'self' https://mate.zerops.io https://*.zerops.app"
`
	previewFramePlainExtends = `  - setup: base
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
    run:
      base: nodejs@22
      start: npm start
  - setup: prod
    extends: base
    run:
      envVariables:
        NODE_ENV: production
`
)

func TestBuildGroupRecipe_PreviewVariant_GroupEnvironmentsBuildTheSetupItExtends(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		yaml          string
		recordedStage string
		// wantAgent is what the AI Agent tier's stage half builds, wantGroup
		// what every group environment's runtime builds.
		wantAgent, wantGroup string
		wantWarning          string
		noWarning            string
	}{
		{
			name: "a static stage's variant widens its headers", recordedStage: "preview",
			yaml:      "zerops:\n" + previewFrameDev + previewFrameStaticProd + previewFrameStaticVariant,
			wantAgent: "preview", wantGroup: "prod", noWarning: "Mate's preview pane",
		},
		{
			name: "a variant sets the value the app's headers read", recordedStage: "preview",
			yaml:      "zerops:\n" + previewFrameDev + previewFrameEnvProd + previewFrameEnvVariant,
			wantAgent: "preview", wantGroup: "prod", noWarning: "Mate's preview pane",
		},
		{
			name: "a framed stage setup that extends nothing is built framed, and said", recordedStage: "preview",
			yaml:      "zerops:\n" + previewFrameDev + previewFrameEnvProd + previewFrameNoExtends,
			wantAgent: "preview", wantGroup: "preview",
			wantWarning: `its stage's setup "preview" lets Mate's preview pane frame the app`,
		},
		{
			name: "a stage setup that extends another without Mate's framing is the stage's own", recordedStage: "prod",
			yaml:      "zerops:\n" + previewFrameDev + previewFramePlainExtends,
			wantAgent: "prod", wantGroup: "prod", noWarning: "Mate's preview pane",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := groupInputsFixture()
			in.Runtimes[0].SetupName = "dev"
			in.Runtimes[0].Stages[0].SetupName = tt.recordedStage
			in.Runtimes[0].ZeropsYAMLBody = tt.yaml
			layout, warnings, err := BuildGroupRecipe(in)
			if err != nil {
				t.Fatalf("BuildGroupRecipe: %v", err)
			}
			if tt.wantWarning != "" && !warningsContain(warnings, tt.wantWarning) {
				t.Errorf("warnings %v are missing %q", warnings, tt.wantWarning)
			}
			if tt.noWarning != "" && warningsContain(warnings, tt.noWarning) {
				t.Errorf("warnings %v say %q", warnings, tt.noWarning)
			}
			files := groupFiles(t, layout)
			agent := serviceEntries(t, tierDoc(t, files, "0 — AI Agent"))
			if got, _ := agent["apidev"]["zeropsSetup"].(string); got != "dev" {
				t.Errorf("AI Agent apidev zeropsSetup = %q, want dev", got)
			}
			if got, _ := agent["apistage"]["zeropsSetup"].(string); got != tt.wantAgent {
				t.Errorf("AI Agent apistage zeropsSetup = %q, want %q", got, tt.wantAgent)
			}
			for _, dir := range []string{"3 — Stage", "4 — Small Production"} {
				if got, _ := serviceEntries(t, tierDoc(t, files, dir))["api"]["zeropsSetup"].(string); got != tt.wantGroup {
					t.Errorf("%s api zeropsSetup = %q, want %q", dir, got, tt.wantGroup)
				}
			}
		})
	}
}

// A part other than a pair's first is named after the setup it builds in the
// group environments: a variant names it by the setup it extends, never by the
// variant's own name.
func TestBuildGroupRecipe_PreviewVariant_PartsAreNamedByTheSetupTheyBuild(t *testing.T) {
	t.Parallel()
	in := groupInputsFixture()
	in.Runtimes[0].SetupName = "dev"
	in.Runtimes[0].Stages = []GroupStage{
		{Hostname: "apistage", SetupName: "web-preview"},
		{Hostname: "workerstage", SetupName: "worker-preview"},
	}
	in.Runtimes[0].ZeropsYAMLBody = "zerops:\n" + previewFrameDev +
		strings.ReplaceAll(previewFrameEnvProd, "setup: prod", "setup: web") +
		strings.NewReplacer("setup: preview", "setup: web-preview", "extends: prod", "extends: web").Replace(previewFrameEnvVariant) +
		strings.ReplaceAll(previewFrameEnvProd, "setup: prod", "setup: worker") +
		strings.NewReplacer("setup: preview", "setup: worker-preview", "extends: prod", "extends: worker").Replace(previewFrameEnvVariant)
	layout, warnings, err := BuildGroupRecipe(in)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	files := groupFiles(t, layout)
	agent := serviceEntries(t, tierDoc(t, files, "0 — AI Agent"))
	for host, want := range map[string]string{"apistage": "web-preview", "workerstage": "worker-preview"} {
		if got, _ := agent[host]["zeropsSetup"].(string); got != want {
			t.Errorf("AI Agent %s zeropsSetup = %q, want %q", host, got, want)
		}
	}
	for _, dir := range []string{"3 — Stage", "4 — Small Production"} {
		services := serviceEntries(t, tierDoc(t, files, dir))
		for host, want := range map[string]string{"api": "web", "worker": "worker"} {
			if got, _ := services[host]["zeropsSetup"].(string); got != want {
				t.Errorf("%s %s zeropsSetup = %q, want %q (warnings %v)", dir, host, got, want, warnings)
			}
		}
		if _, named := services["worker-preview"]; named {
			t.Errorf("%s names a runtime after the variant", dir)
		}
	}
}
