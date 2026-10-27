package bundle

import (
	"maps"
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
	// previewFrameAddressVariant is Mate's preview variant of a page that
	// forbids framing: it frames nothing and points the app's own address and
	// cookie domain at the Mate's stage.
	previewFrameAddressVariant = `  - setup: preview
    extends: prod
    run:
      envVariables:
        Z_URL: https://apistage-1f2a-3000.prg1.zerops.app
        Z_COOKIE_DOMAIN: apistage-1f2a-3000.prg1.zerops.app
`
	// previewFrameBuildlessBase is a production setup over a shared base that
	// builds nothing (the wordpress recipe's shape): its address is a
	// reference every environment resolves for itself.
	previewFrameBuildlessBase = `  - setup: base
    run:
      base: php-nginx@8.4
      start: php-fpm
  - setup: prod
    extends: base
    build:
      base: php@8.4
      buildCommands:
        - composer install --no-dev
      deployFiles: ./
    run:
      envVariables:
        APP_URL: ${zeropsSubdomain}
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
		{
			name: "a variant that frames nothing points the app's addresses at the Mate's stage", recordedStage: "preview",
			yaml:      "zerops:\n" + previewFrameDev + previewFrameEnvProd + previewFrameAddressVariant,
			wantAgent: "preview", wantGroup: "prod", noWarning: "Mate's preview pane",
		},
		{
			name: "a stage setup over a base that builds nothing is the stage's own", recordedStage: "prod",
			yaml:      "zerops:\n" + previewFrameDev + previewFrameBuildlessBase,
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

// An app that talks to Mate names its address in a setting: that is no
// framing, so its stage's setup — even one that extends another — is what
// the group environments build, and nothing is said.
func TestBuildGroupRecipe_PreviewVariant_ASettingNamingMateIsNoFraming(t *testing.T) {
	t.Parallel()
	in := groupInputsFixture()
	in.Runtimes[0].SetupName = "dev"
	in.Runtimes[0].Stages[0].SetupName = "prod"
	in.Runtimes[0].ZeropsYAMLBody = "zerops:\n" + previewFrameDev +
		strings.ReplaceAll(previewFramePlainExtends, "NODE_ENV: production", "MATE_APP_URL: https://mate.zerops.io")
	layout, warnings, err := BuildGroupRecipe(in)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	if warningsContain(warnings, "Mate's preview pane") {
		t.Errorf("warnings %v read a setting as framing", warnings)
	}
	files := groupFiles(t, layout)
	for _, dir := range []string{"3 — Stage", "4 — Small Production"} {
		if got, _ := serviceEntries(t, tierDoc(t, files, dir))["api"]["zeropsSetup"].(string); got != "prod" {
			t.Errorf("%s api zeropsSetup = %q, want the stage's prod", dir, got)
		}
	}
}

// poolMonorepoPreviewVariants is what a Mate of Zerops Apps gave its stages
// (2026-10-27): Mate's preview variant of each part's setup, pointing the
// app's addresses and cookies at the Mate's own stages. hq and box forbid
// framing, so theirs frame nothing; the web's lets Mate's preview pane frame
// it by one project's Mate origins.
const poolMonorepoPreviewVariants = `  - setup: webpreview
    extends: web
    run:
      envVariables:
        Z_URL: https://webstage-1f2a-3000.prg1.zerops.app
        FRAME_ANCESTORS: "'self' https://mate.zerops.io https://zcp-1f2a-3773.prg1.zerops.app"
  - setup: hqpreview
    extends: hq
    run:
      envVariables:
        Z_URL: https://hqstage-1f2a.prg1.zerops.app
        Z_COOKIE_DOMAIN: hqstage-1f2a.prg1.zerops.app
  - setup: boxpreview
    extends: box
    run:
      envVariables:
        Z_URL: https://boxstage-1f2a.prg1.zerops.app
        Z_HQ_URL: https://hqstage-1f2a.prg1.zerops.app
`

// poolMonorepoPoolsYAML is the pool monorepo's zerops.yaml with its dev
// pools' setups alone.
const poolMonorepoPoolsYAML = `zerops:
  - setup: webpool
    run:
      base: nodejs@24
      start: zcp-devsup run
  - setup: hqpool
    run:
      base: nodejs@24
      start: zcp-devsup run
  - setup: boxpool
    run:
      base: nodejs@24
      start: zcp-devsup run
`

// TestBuildGroupRecipe_PoolMonorepoPreviewVariants_StageAndProductionBuildTheBases:
// each stage of a pool-keyed monorepo builds Mate's preview variant of its
// part's setup. The AI Agent tier re-creates a Mate, whose stages are its
// preview, so it keeps the variants; Stage and Small Production build the
// setups the variants extend — the variants point at one Mate's stages and
// let one project's Mate origins frame the app. Without a zerops.yaml, an
// `extends`, or the setup it names, every tier names the stage's own setup.
func TestBuildGroupRecipe_PoolMonorepoPreviewVariants_StageAndProductionBuildTheBases(t *testing.T) {
	t.Parallel()
	variants := map[string]string{"web": "webpreview", "hq": "hqpreview", "box": "boxpreview"}
	bases := map[string]string{"web": "web", "hq": "hq", "box": "box"}
	standalone := strings.NewReplacer("    extends: web\n", "", "    extends: hq\n", "", "    extends: box\n", "").
		Replace(poolMonorepoPreviewVariants)
	tests := []struct {
		name      string
		body      string
		wantGroup map[string]string
	}{
		{name: "variants extending web, hq and box", body: poolMonorepoYAML + poolMonorepoPreviewVariants, wantGroup: bases},
		{name: "no zerops.yaml was read", body: "", wantGroup: variants},
		{name: "the variants extend nothing", body: poolMonorepoYAML + standalone, wantGroup: variants},
		{name: "the setups the variants extend are not declared", body: poolMonorepoPoolsYAML + poolMonorepoPreviewVariants, wantGroup: variants},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := poolMonorepoInputs()
			for i := range in.Runtimes {
				in.Runtimes[i].ZeropsYAMLBody = tt.body
				in.Runtimes[i].Stages[0].SetupName = GroupPromotedHostname(in.Runtimes[i].DevHostname) + "preview"
			}
			layout, warnings, err := BuildGroupRecipe(in)
			if err != nil {
				t.Fatalf("BuildGroupRecipe: %v", err)
			}
			if tt.body != "" && warningsContain(warnings, "is not declared") {
				t.Errorf("warnings %v say a setup a tier names is not declared", warnings)
			}
			wantAgent := map[string]string{"webstage": "webpreview", "hqstage": "hqpreview", "boxstage": "boxpreview"}
			if got := tierRuntimes(t, tierDocBody(t, layout, "AI Agent")); !maps.Equal(got, wantAgent) {
				t.Errorf("AI Agent runtimes = %v, want %v", got, wantAgent)
			}
			for _, title := range []string{"Stage", "Small Production"} {
				if got := tierRuntimes(t, tierDocBody(t, layout, title)); !maps.Equal(got, tt.wantGroup) {
					t.Errorf("%s runtimes = %v, want %v (warnings %v)", title, got, tt.wantGroup, warnings)
				}
			}
		})
	}
}
