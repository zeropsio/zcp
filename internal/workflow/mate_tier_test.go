package workflow

import (
	"errors"
	"strings"
	"testing"
)

// tierHQ is the Mate's HQ in these tests, and tierApp the application HQ
// holds it in.
const (
	tierHQ  = "https://hq.acme.example"
	tierApp = "app-1"
)

// beviroTier is the shape of a real group's AI Agent tier (the Beviro trial,
// 2026-09-29): pairs whose setups are named after the pair rather than
// `dev`/`prod`, a public-build utility, a managed database, and priorities.
const beviroTier = `#yamlPreprocessor=on
project:
  name: beviro-wren
  envVariables:
    SECRET: <@generateRandomString(<32>)>
services:
  - hostname: medusadev
    type: nodejs@22
    buildFromGit: https://hq.acme.example/git/app-1/medusadev
    zeropsSetup: medusadev
    priority: 5
  - hostname: medusastage
    type: nodejs@22
    buildFromGit: https://hq.acme.example/git/app-1/medusadev
    zeropsSetup: medusaprod
    enableSubdomainAccess: true
    priority: 5
  - hostname: nextstoredev
    type: nodejs@22
    buildFromGit: https://hq.acme.example/git/app-1/nextstoredev.git
    zeropsSetup: nextstoredev
  - hostname: nextstorestage
    type: static@1.0
    buildFromGit: https://hq.acme.example/git/app-1/nextstoredev/
    zeropsSetup: nextstoreprod
  - hostname: mailpit
    type: alpine@3.20
    buildFromGit: https://github.com/zeropsio/recipe-mailpit
    enableSubdomainAccess: true
  - hostname: db
    type: postgresql@17
    mode: NON_HA
    priority: 10
`

func TestParseMateTier(t *testing.T) {
	t.Parallel()
	type pairWant struct {
		repo, repoName           string
		dev, devSetup, devType   string
		stage, stageSetup, sType string
		priority                 int
	}
	tests := []struct {
		name        string
		yaml        string
		wantPairs   []pairWant
		wantSkipped []string // "hostname:reason"
	}{
		{
			name: "a real group's tier: pairs by repository, stage by its suffix, highest priority first",
			yaml: beviroTier,
			wantPairs: []pairWant{
				{repo: tierHQ + "/git/app-1/medusadev", repoName: "medusadev", dev: "medusadev", devSetup: "medusadev", devType: "nodejs@22", stage: "medusastage", stageSetup: "medusaprod", sType: "nodejs@22", priority: 5},
				{repo: tierHQ + "/git/app-1/nextstoredev", repoName: "nextstoredev", dev: "nextstoredev", devSetup: "nextstoredev", devType: "nodejs@22", stage: "nextstorestage", stageSetup: "nextstoreprod", sType: "static@1.0"},
			},
			wantSkipped: []string{"mailpit:platform-build", "db:managed"},
		},
		{
			name: "the stage's partner is the repository's other runtime, whatever its name",
			yaml: `services:
  - hostname: api
    type: go@1
    buildFromGit: https://hq.acme.example/git/app-1/api
    zeropsSetup: dev
  - hostname: apistage
    type: go@1
    buildFromGit: https://hq.acme.example/git/app-1/api
    zeropsSetup: prod
`,
			wantPairs: []pairWant{{repo: tierHQ + "/git/app-1/api", repoName: "api", dev: "api", devSetup: "dev", devType: "go@1", stage: "apistage", stageSetup: "prod", sType: "go@1"}},
		},
		{
			name: "one repository building two pairs: each stage takes the runtime named like it",
			yaml: `services:
  - hostname: workerstage
    type: nodejs@22
    buildFromGit: https://hq.acme.example/git/app-1/mono
    zeropsSetup: workerprod
  - hostname: apidev
    type: nodejs@22
    buildFromGit: https://hq.acme.example/git/app-1/mono
    zeropsSetup: apidev
  - hostname: apistage
    type: nodejs@22
    buildFromGit: https://hq.acme.example/git/app-1/mono
    zeropsSetup: apiprod
  - hostname: workerdev
    type: nodejs@22
    buildFromGit: https://hq.acme.example/git/app-1/mono
    zeropsSetup: workerdev
`,
			wantPairs: []pairWant{
				{repo: tierHQ + "/git/app-1/mono", repoName: "mono", dev: "apidev", devSetup: "apidev", devType: "nodejs@22", stage: "apistage", stageSetup: "apiprod", sType: "nodejs@22"},
				{repo: tierHQ + "/git/app-1/mono", repoName: "mono", dev: "workerdev", devSetup: "workerdev", devType: "nodejs@22", stage: "workerstage", stageSetup: "workerprod", sType: "nodejs@22"},
			},
		},
		{
			name: "a block-form build, and a half that names no setup builds the setup named after it",
			yaml: `services:
  - hostname: appdev
    type: php-nginx@8.4
    buildFromGit:
      url: https://hq.acme.example/git/app-1/app
      ref: main
  - hostname: appstage
    type: php-nginx@8.4
    buildFromGit:
      url: https://hq.acme.example/git/app-1/app
    zeropsSetup: prod
`,
			wantPairs: []pairWant{{repo: tierHQ + "/git/app-1/app", repoName: "app", dev: "appdev", devSetup: "appdev", devType: "php-nginx@8.4", stage: "appstage", stageSetup: "prod", sType: "php-nginx@8.4"}},
		},
		{
			name: "a pair's priority is its higher half's",
			yaml: `services:
  - hostname: webdev
    type: nodejs@22
    buildFromGit: https://hq.acme.example/git/app-1/web
  - hostname: webstage
    type: nodejs@22
    buildFromGit: https://hq.acme.example/git/app-1/web
    priority: 3
  - hostname: apidev
    type: nodejs@22
    buildFromGit: https://hq.acme.example/git/app-1/api
    priority: 7
  - hostname: apistage
    type: nodejs@22
    buildFromGit: https://hq.acme.example/git/app-1/api
`,
			wantPairs: []pairWant{
				{repo: tierHQ + "/git/app-1/api", repoName: "api", dev: "apidev", devSetup: "apidev", devType: "nodejs@22", stage: "apistage", stageSetup: "apistage", sType: "nodejs@22", priority: 7},
				{repo: tierHQ + "/git/app-1/web", repoName: "web", dev: "webdev", devSetup: "webdev", devType: "nodejs@22", stage: "webstage", stageSetup: "webstage", sType: "nodejs@22", priority: 3},
			},
		},
		{
			name: "what cannot be stood up is named, never guessed",
			yaml: `services:
  - hostname: appdev
    type: nodejs@22
    buildFromGit: https://hq.acme.example/git/app-1/app
  - hostname: appstage
    type: nodejs@22
    buildFromGit: https://hq.acme.example/git/app-1/app
  - hostname: lonedev
    type: nodejs@22
    buildFromGit: https://hq.acme.example/git/app-1/lone
  - hostname: otherdev
    type: nodejs@22
    buildFromGit: https://hq.acme.example/git/app-2/other
  - hostname: otherstage
    type: nodejs@22
    buildFromGit: https://hq.acme.example/git/app-2/other
  - hostname: bare
    type: nodejs@22
    startWithoutCode: true
  - hostname: aastage
    type: nodejs@22
    buildFromGit: https://hq.acme.example/git/app-1/twin
  - hostname: bbstage
    type: nodejs@22
    buildFromGit: https://hq.acme.example/git/app-1/twin
  - hostname: store
    type: object-storage
`,
			wantPairs: []pairWant{{repo: tierHQ + "/git/app-1/app", repoName: "app", dev: "appdev", devSetup: "appdev", devType: "nodejs@22", stage: "appstage", stageSetup: "appstage", sType: "nodejs@22"}},
			wantSkipped: []string{
				"lonedev:unpaired", "otherdev:foreign-repository", "otherstage:foreign-repository",
				"bare:no-repository", "aastage:unpaired", "bbstage:unpaired", "store:managed",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tier, err := ParseMateTier(tt.yaml, tierHQ, tierApp)
			if err != nil {
				t.Fatalf("ParseMateTier: %v", err)
			}
			if len(tier.Pairs) != len(tt.wantPairs) {
				t.Fatalf("pairs = %+v, want %d", tier.Pairs, len(tt.wantPairs))
			}
			for i, want := range tt.wantPairs {
				got := tier.Pairs[i]
				if got.Repository != want.repo || got.RepoName != want.repoName ||
					got.Dev.Hostname != want.dev || got.Dev.Setup != want.devSetup || got.Dev.Type != want.devType ||
					got.Stage.Hostname != want.stage || got.Stage.Setup != want.stageSetup || got.Stage.Type != want.sType ||
					got.Priority() != want.priority {
					t.Errorf("pair %d = %+v (priority %d), want %+v", i, got, got.Priority(), want)
				}
			}
			skipped := make([]string, 0, len(tier.Skipped))
			for _, s := range tier.Skipped {
				skipped = append(skipped, s.Hostname+":"+string(s.Reason))
			}
			if strings.Join(skipped, " ") != strings.Join(tt.wantSkipped, " ") {
				t.Errorf("skipped = %v, want %v", skipped, tt.wantSkipped)
			}
		})
	}
}

// TestParseMateTier_SkipsCarryWhereTheirCodeComesFrom: a public build names
// the repository the platform built it from, so the summary can say so.
func TestParseMateTier_SkipsCarryWhereTheirCodeComesFrom(t *testing.T) {
	t.Parallel()
	tier, err := ParseMateTier(beviroTier, tierHQ, tierApp)
	if err != nil {
		t.Fatalf("ParseMateTier: %v", err)
	}
	for _, s := range tier.Skipped {
		if s.Hostname == "mailpit" && s.Source != "https://github.com/zeropsio/recipe-mailpit" {
			t.Errorf("mailpit source = %q", s.Source)
		}
		if s.Hostname == "db" && s.Type != "postgresql@17" {
			t.Errorf("db type = %q", s.Type)
		}
	}
}

func TestParseMateTier_Refusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		yaml    string
		wantErr error
		want    string
	}{
		{name: "not YAML", yaml: "services: [\n", wantErr: ErrMateTierUnreadable},
		{name: "no services", yaml: "project:\n  name: x\n", wantErr: ErrMateTierUnreadable},
		{name: "a service with no hostname", yaml: "services:\n  - type: nodejs@22\n", wantErr: ErrMateTierUnreadable},
		{
			name:    "services, but no pair",
			yaml:    "services:\n  - hostname: db\n    type: postgresql@17\n  - hostname: mailpit\n    type: alpine@3.20\n    buildFromGit: https://github.com/zeropsio/recipe-mailpit\n",
			wantErr: ErrMateTierNoPairs,
			want:    "db",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tier, err := ParseMateTier(tt.yaml, tierHQ, tierApp)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.want != "" && !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err %q does not name %q", err, tt.want)
			}
			if errors.Is(err, ErrMateTierNoPairs) && len(tier.Skipped) == 0 {
				t.Error("a tier with no pair still says what it skipped")
			}
		})
	}
}

// The tier's project variables are what a build reads through a `${NAME}`.
func TestParseMateTier_CarriesTheProjectVariables(t *testing.T) {
	t.Parallel()
	body := "project:\n  name: acme\n  envVariables:\n    API_URL: https://${apistage_zeropsSubdomain}\n    DEBUG: true\n" +
		"services:\n  - hostname: apidev\n    type: nodejs@22\n    buildFromGit: " + tierHQ + "/git/app-1/apidev\n  - hostname: apistage\n    type: nodejs@22\n    buildFromGit: " + tierHQ + "/git/app-1/apidev\n"
	tier, err := ParseMateTier(body, tierHQ, tierApp)
	if err != nil {
		t.Fatalf("ParseMateTier: %v", err)
	}
	if got := tier.ProjectEnvs["API_URL"]; got != "https://${apistage_zeropsSubdomain}" {
		t.Errorf("API_URL = %q", got)
	}
	if got := tier.ProjectEnvs["DEBUG"]; got != "true" {
		t.Errorf("DEBUG = %q, want the scalar as written", got)
	}
}

// A half carries its own service variables as the tier writes them — its
// envVariables and envSecrets — which a build lifts as ${RUNTIME_X}.
func TestParseMateTier_CarriesEachHalfsOwnVariables(t *testing.T) {
	t.Parallel()
	body := "services:\n" +
		"  - hostname: storedev\n    type: nodejs@22\n    buildFromGit: " + tierHQ + "/git/app-1/storedev\n" +
		"  - hostname: storestage\n    type: nodejs@22\n    buildFromGit: " + tierHQ + "/git/app-1/storedev\n" +
		"    envVariables:\n      API_URL: https://${apistage_zeropsSubdomain}\n    envSecrets:\n      PORT_NUMBER: 8000\n"
	tier, err := ParseMateTier(body, tierHQ, tierApp)
	if err != nil {
		t.Fatalf("ParseMateTier: %v", err)
	}
	stage := tier.Pairs[0].Stage
	if stage.Envs["API_URL"] != "https://${apistage_zeropsSubdomain}" || stage.Envs["PORT_NUMBER"] != "8000" {
		t.Errorf("stage envs = %v", stage.Envs)
	}
	if len(tier.Pairs[0].Dev.Envs) != 0 {
		t.Errorf("dev envs = %v, want none", tier.Pairs[0].Dev.Envs)
	}
}

// TestParseMateTier_PairsOnlyRepositoriesOfThisApplication: a runtime is a
// pair's half only when it builds from a repository of the Mate's
// application on its HQ — `<HQ>/git/<appId>/<repo>`. Another application's
// repository is foreign; anything else, the platform builds.
func TestParseMateTier_PairsOnlyRepositoriesOfThisApplication(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
		want   MateTierSkipReason // "" for a pair
	}{
		{"this application's repository", tierHQ + "/git/app-1/web.git", ""},
		{"another application's", tierHQ + "/git/app-2/web", MateTierSkipForeign},
		{"HQ's host, outside its git", tierHQ + "/app-1/web", MateTierSkipPlatformBuild},
		{"another host", "https://elsewhere.example/git/app-1/web", MateTierSkipPlatformBuild},
		{"HQ's host on another port", "https://hq.acme.example:8443/git/app-1/web", MateTierSkipPlatformBuild},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := "services:\n" +
				"  - hostname: webdev\n    type: nodejs@22\n    buildFromGit: " + tt.source + "\n" +
				"  - hostname: webstage\n    type: nodejs@22\n    buildFromGit: " + tt.source + "\n" +
				"  - hostname: apidev\n    type: nodejs@22\n    buildFromGit: " + tierHQ + "/git/app-1/api\n" +
				"  - hostname: apistage\n    type: nodejs@22\n    buildFromGit: " + tierHQ + "/git/app-1/api\n"
			tier, err := ParseMateTier(body, tierHQ, tierApp)
			if err != nil {
				t.Fatal(err)
			}
			paired := false
			for _, p := range tier.Pairs {
				if p.Dev.Hostname == "webdev" {
					paired = true
					if p.RepoName != "web" {
						t.Errorf("repository name = %q, want web", p.RepoName)
					}
				}
			}
			var reason MateTierSkipReason
			for _, s := range tier.Skipped {
				if s.Hostname == "webdev" {
					reason = s.Reason
				}
			}
			if paired != (tt.want == "") || reason != tt.want {
				t.Errorf("webdev paired=%v skipped as %q, want %q", paired, reason, tt.want)
			}
		})
	}
}
