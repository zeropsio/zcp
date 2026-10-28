package workflow

import (
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
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
					len(got.Stages) != 1 || got.FirstStage().Hostname != want.stage || got.FirstStage().Setup != want.stageSetup || got.FirstStage().Type != want.sType ||
					got.CodeVolume != "" || len(got.DevServices) != 0 ||
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

// TestParseMateTier_OneDevHalfTakesEveryStageOfItsRepository (docs/spec-mate.md
// §2.8a.4): a repository the tier builds into one dev half and a stage per
// part is one pair holding every stage — the one named like the dev half
// first, then the tier's order — never a pair and stages left unpaired.
func TestParseMateTier_OneDevHalfTakesEveryStageOfItsRepository(t *testing.T) {
	t.Parallel()
	tier, err := ParseMateTier(`services:
  - hostname: webstage
    type: bun@1.3
    buildFromGit: https://hq.acme.example/git/app-1/app
    zeropsSetup: web
  - hostname: appdev
    type: bun@1.3
    buildFromGit: https://hq.acme.example/git/app-1/app
    zeropsSetup: dev
  - hostname: apistage
    type: go@1
    buildFromGit: https://hq.acme.example/git/app-1/app
    zeropsSetup: api
    priority: 4
  - hostname: appstage
    type: bun@1.3
    buildFromGit: https://hq.acme.example/git/app-1/app
    zeropsSetup: prod
`, tierHQ, tierApp)
	if err != nil {
		t.Fatalf("ParseMateTier: %v", err)
	}
	if len(tier.Pairs) != 1 || len(tier.Skipped) != 0 {
		t.Fatalf("pairs = %+v, skipped = %+v; want one pair and nothing skipped", tier.Pairs, tier.Skipped)
	}
	p := tier.Pairs[0]
	stages := make([]string, 0, len(p.Stages))
	for _, s := range p.Stages {
		stages = append(stages, s.Hostname+"/"+s.Setup+"/"+s.Type)
	}
	if p.Dev.Hostname != "appdev" || strings.Join(stages, " ") != "appstage/prod/bun@1.3 webstage/web/bun@1.3 apistage/api/go@1" {
		t.Errorf("pair = %s with stages %v", p.Dev.Hostname, stages)
	}
	if p.CodeVolume != "" || len(p.DevServices) != 0 || p.Priority() != 4 {
		t.Errorf("a one-dev-half pair is variant A with its highest priority: codeVolume %q, devServices %+v, priority %d", p.CodeVolume, p.DevServices, p.Priority())
	}
}

// variantBTierRuntimes is F2's tier — webdev and apidev from one
// repository, a stage per part — whose dev setups share a code volume when
// the tier also holds the local-storage service vol.
const variantBTierRuntimes = `  - hostname: webdev
    type: bun@1.3
    buildFromGit: https://hq.acme.example/git/app-1/mono
    zeropsSetup: dev
  - hostname: webstage
    type: bun@1.3
    buildFromGit: https://hq.acme.example/git/app-1/mono
    zeropsSetup: web
  - hostname: apidev
    type: go@1
    buildFromGit: https://hq.acme.example/git/app-1/mono
    zeropsSetup: dev-api
  - hostname: apistage
    type: go@1
    buildFromGit: https://hq.acme.example/git/app-1/mono
    zeropsSetup: api
  - hostname: workerstage
    type: bun@1.3
    buildFromGit: https://hq.acme.example/git/app-1/mono
    zeropsSetup: worker
`

// TestParseMateTier_SeveralDevServicesBesideALocalStorageService_PairByName
// (docs/spec-mate.md §2.8a.4a): zcp sets up no new code volume, so a tier
// whose repository builds several dev services beside a local-storage
// service is read like any other — each stage pairs with the runtime named
// like it, no pair holds a code volume, and the volume is a managed service
// the stand-up leaves as it is.
func TestParseMateTier_SeveralDevServicesBesideALocalStorageService_PairByName(t *testing.T) {
	t.Parallel()
	for name, rest := range map[string]string{
		"one local-storage service":  "  - hostname: vol\n    type: local-storage:single@1\n",
		"no local-storage service":   "",
		"two local-storage services": "  - hostname: vol\n    type: local-storage:single@1\n  - hostname: data\n    type: local-storage:single@1\n",
	} {
		tier, err := ParseMateTier("services:\n"+variantBTierRuntimes+rest, tierHQ, tierApp)
		if err != nil {
			t.Fatalf("%s: ParseMateTier: %v", name, err)
		}
		pairs := make([]string, 0, len(tier.Pairs))
		for _, p := range tier.Pairs {
			if p.CodeVolume != "" || len(p.DevServices) != 0 || len(p.Stages) != 1 {
				t.Errorf("%s: pair %+v holds a code volume", name, p)
			}
			pairs = append(pairs, p.Dev.Hostname+"→"+p.FirstStage().Hostname)
		}
		if strings.Join(pairs, " ") != "apidev→apistage webdev→webstage" {
			t.Errorf("%s: pairs = %v, want each stage with the runtime named like it", name, pairs)
		}
	}
}

// TestParseMateTier_ARecordedVariantBPair_IsReadAsThatPair (docs/spec-mate.md
// §2.8a.4a): a variant-B pair zcp already records keeps working, so a tier
// read beside its record pairs the repository's runtimes as that pair — its
// recorded dev half, wherever the tier lists it, the others further dev
// services, every stage of the repository its stage — and never a second
// pair for one of its services. A record naming another volume, or a pair
// already keyed by its dev pool, keeps nothing.
func TestParseMateTier_ARecordedVariantBPair_IsReadAsThatPair(t *testing.T) {
	t.Parallel()
	const vol = "  - hostname: vol\n    type: local-storage:single@1\n"
	recorded := func(volume string, pool bool) *ServiceMeta {
		m := NewServiceMeta("apidev", topology.PlanModeStandard)
		m.CodeVolume = &topology.CodeVolume{Hostname: volume, MountPath: "/var/www/ws",
			DevServices: []topology.DevServiceRef{{Hostname: "webdev", SetupName: "dev"}}}
		if pool {
			m.DevPool = &DevPoolRef{ServiceID: "svc-apipool"}
		}
		return m
	}

	tier, err := ParseMateTier("services:\n"+variantBTierRuntimes+vol, tierHQ, tierApp, NewServiceMeta("other", topology.PlanModeDev), recorded("vol", false))
	if err != nil {
		t.Fatalf("ParseMateTier: %v", err)
	}
	if len(tier.Pairs) != 1 {
		t.Fatalf("pairs = %+v, want the recorded variant-B pair", tier.Pairs)
	}
	p := tier.Pairs[0]
	stages := make([]string, 0, len(p.Stages))
	for _, s := range p.Stages {
		stages = append(stages, s.Hostname+"/"+s.Setup)
	}
	devs := make([]string, 0, len(p.DevServices))
	for _, d := range p.DevServices {
		devs = append(devs, d.Hostname+"/"+d.Setup)
	}
	if p.Dev.Hostname != "apidev" || p.CodeVolume != "vol" || strings.Join(devs, " ") != "webdev/dev" ||
		strings.Join(stages, " ") != "apistage/api webstage/web workerstage/worker" {
		t.Errorf("pair = %s on %q, dev services %v, stages %v", p.Dev.Hostname, p.CodeVolume, devs, stages)
	}

	for name, rec := range map[string]*ServiceMeta{
		"a record naming another volume":        recorded("data", false),
		"a pair already keyed by its dev pool":  recorded("vol", true),
		"a record of a dev half the tier lacks": func() *ServiceMeta { m := recorded("vol", false); m.Hostname = "gonedev"; return m }(),
	} {
		tier, err := ParseMateTier("services:\n"+variantBTierRuntimes+vol, tierHQ, tierApp, rec)
		if err != nil {
			t.Fatalf("%s: ParseMateTier: %v", name, err)
		}
		for _, p := range tier.Pairs {
			if p.CodeVolume != "" || len(p.DevServices) != 0 {
				t.Errorf("%s: pair %+v keeps a code volume", name, p)
			}
		}
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
	stage := tier.Pairs[0].FirstStage()
	if stage.Envs["API_URL"] != "https://${apistage_zeropsSubdomain}" || stage.Envs["PORT_NUMBER"] != "8000" {
		t.Errorf("stage envs = %v", stage.Envs)
	}
	if len(tier.Pairs[0].Dev.Envs) != 0 {
		t.Errorf("dev envs = %v, want none", tier.Pairs[0].Dev.Envs)
	}
}

// TestParseMateTier_PairsOnlyRepositoriesOfThisApplication: a runtime is a
// pair's half only when it builds from a repository of the Mate's
// application on its HQ — `<HQ>/git/<appId>/<repo>`, including an own-app
// legacy path from a former host or port. Another application's repository
// on current HQ is foreign; other unmatched sources the platform builds.
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
		{"another host", "https://elsewhere.example/git/app-1/web", ""},
		{"HQ's host on another port", "https://hq.acme.example:8443/git/app-1/web", ""},
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

func TestParseMateTier_HQMigrationDecisions(t *testing.T) {
	t.Parallel()
	const app = "424dcb59-29a9-4a57-ad71-0c3c6ca06553"
	const formerHQ = "https://53h3i3i53j9t767n7kibd1tj0t0.prg1-zerops.zone"
	const source = formerHQ + "/git/" + app + "/flipdev"
	t.Run("A Mate added after HQ migration stands up the migrated application's existing dev/stage pair.", func(t *testing.T) {
		t.Parallel()
		body := "services:\n" +
			"  - hostname: flipdev\n    type: nodejs@22\n    buildFromGit: " + source + "\n" +
			"  - hostname: flipstage\n    type: nodejs@22\n    buildFromGit: " + source + "\n"
		tier, err := ParseMateTier(body, tierHQ, app)
		if err != nil {
			t.Fatalf("ASSERTION: migrated application's existing pair must parse: %v", err)
		}
		if len(tier.Pairs) != 1 || len(tier.Skipped) != 0 {
			t.Fatalf("pairs=%+v skipped=%+v, want one pair and no skips", tier.Pairs, tier.Skipped)
		}
		pair := tier.Pairs[0]
		if pair.RepoName != "flipdev" || pair.Repository != source || pair.Dev.Hostname != "flipdev" || len(pair.Stages) != 1 || pair.FirstStage().Hostname != "flipstage" {
			t.Errorf("migrated pair = %+v, want flipdev/flipstage from %s", pair, source)
		}
	})
	t.Run("A recipe reference to another application's repository is never treated as this application's repository.", func(t *testing.T) {
		t.Parallel()
		for _, host := range []string{tierHQ, formerHQ, "https://elsewhere.example"} {
			t.Run(host, func(t *testing.T) {
				t.Parallel()
				body := "services:\n" +
					"  - hostname: flipdev\n    type: nodejs@22\n    buildFromGit: " + host + "/git/another-app/flipdev\n" +
					"  - hostname: flipstage\n    type: nodejs@22\n    buildFromGit: " + host + "/git/another-app/flipdev\n"
				tier, err := ParseMateTier(body, tierHQ, app)
				if !errors.Is(err, ErrMateTierNoPairs) || len(tier.Pairs) != 0 || len(tier.Skipped) != 2 {
					t.Fatalf("foreign application: pairs=%+v skipped=%+v err=%v", tier.Pairs, tier.Skipped, err)
				}
				want := MateTierSkipPlatformBuild
				if host == tierHQ {
					want = MateTierSkipForeign
				}
				for _, skip := range tier.Skipped {
					if skip.Reason != want {
						t.Errorf("%s reason=%q, want %q", skip.Hostname, skip.Reason, want)
					}
				}
			})
		}
	})
}

func TestParseMateTier_MigratedHostTrustsOnlyTheOwnApplicationLegacyPath(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"https://elsewhere.example/git/org-1/app-1/web",
		"https://elsewhere.example/prefix/git/app-1/web",
		"https://elsewhere.example/git/app-1/web/more",
		"https://elsewhere.example/git//app-1/web",
		"http://elsewhere.example/git/app-1/web",
		"https:///git/app-1/web",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			body := "services:\n" +
				"  - hostname: webdev\n    type: nodejs@22\n    buildFromGit: " + source + "\n" +
				"  - hostname: webstage\n    type: nodejs@22\n    buildFromGit: " + source + "\n"
			tier, err := ParseMateTier(body, tierHQ, tierApp)
			if !errors.Is(err, ErrMateTierNoPairs) || len(tier.Pairs) != 0 || len(tier.Skipped) != 2 {
				t.Fatalf("unsupported reference: pairs=%+v skipped=%+v err=%v", tier.Pairs, tier.Skipped, err)
			}
			for _, skip := range tier.Skipped {
				if skip.Reason != MateTierSkipPlatformBuild {
					t.Errorf("skip=%+v, want platform-build", skip)
				}
			}
		})
	}
}

// TestParseMateTier_ReadsHQsAddressWithTheOrganisation: HQ serves a
// repository at `/git/<orgId>/<appId>/<repo>` (docs/spec-mate.md §2.8a.4),
// the address zcp's group recipe writes, and a tier written before HQ named
// the organisation still carries `/git/<appId>/<repo>`: both name this
// application's repository. Another application's is foreign either way, and
// a deeper path is no repository of HQ's.
func TestParseMateTier_ReadsHQsAddressWithTheOrganisation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
		want   MateTierSkipReason // "" for a pair
	}{
		{"the organisation's address", tierHQ + "/git/org-1/app-1/web.git", ""},
		{"the organisation's address, canonical", tierHQ + "/git/org-1/app-1/web", ""},
		{"an address from before the organisation", tierHQ + "/git/app-1/web", ""},
		{"another application of the organisation", tierHQ + "/git/org-1/app-2/web", MateTierSkipForeign},
		{"a deeper path", tierHQ + "/git/org-1/app-1/web/more", MateTierSkipPlatformBuild},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// The api pair stands in every case, so a tier whose web pair is
			// skipped still parses.
			body := "services:\n" +
				"  - hostname: webdev\n    type: nodejs@22\n    buildFromGit: " + tt.source + "\n" +
				"  - hostname: webstage\n    type: nodejs@22\n    buildFromGit: " + tt.source + "\n" +
				"  - hostname: apidev\n    type: nodejs@22\n    buildFromGit: " + tierHQ + "/git/org-1/app-1/api\n" +
				"  - hostname: apistage\n    type: nodejs@22\n    buildFromGit: " + tierHQ + "/git/org-1/app-1/api\n"
			tier, err := ParseMateTier(body, tierHQ, tierApp)
			if err != nil {
				t.Fatal(err)
			}
			paired := false
			for _, p := range tier.Pairs {
				if p.Dev.Hostname == "webdev" {
					paired = p.RepoName == "web"
				}
			}
			var reason MateTierSkipReason
			for _, s := range tier.Skipped {
				if s.Hostname == "webdev" {
					reason = s.Reason
				}
			}
			if paired != (tt.want == "") || reason != tt.want {
				t.Errorf("webdev paired=%v (pairs %+v) skipped as %q, want %q", paired, tier.Pairs, reason, tt.want)
			}
		})
	}
}

// A tier zcp writes carries its values in vaults — the project's and each
// half's own, plain or `{value, sensitive}` — and those are what a build
// reads, like the deprecated blocks before them.
func TestParseMateTier_ReadsTheVaults(t *testing.T) {
	t.Parallel()
	body := "project:\n  name: acme\n  vault:\n    API_URL: https://${apistage_zeropsSubdomain}\n    JWT_SECRET: {value: <@generateRandomString(<48>)>, sensitive: true}\n" +
		"services:\n" +
		"  - hostname: storedev\n    type: nodejs@22\n    buildFromGit: " + tierHQ + "/git/app-1/storedev\n" +
		"  - hostname: storestage\n    type: nodejs@22\n    buildFromGit: " + tierHQ + "/git/app-1/storedev\n" +
		"    vault:\n      STORE_URL: https://${storestage_zeropsSubdomain}\n      STORE_TOKEN: {value: \"${api_TOKEN}\", sensitive: true}\n"
	tier, err := ParseMateTier(body, tierHQ, tierApp)
	if err != nil {
		t.Fatalf("ParseMateTier: %v", err)
	}
	wantProject := map[string]string{"API_URL": "https://${apistage_zeropsSubdomain}", "JWT_SECRET": "<@generateRandomString(<48>)>"}
	if !maps.Equal(tier.ProjectEnvs, wantProject) {
		t.Errorf("project = %v, want %v", tier.ProjectEnvs, wantProject)
	}
	wantStage := map[string]string{"STORE_URL": "https://${storestage_zeropsSubdomain}", "STORE_TOKEN": "${api_TOKEN}"}
	if got := tier.Pairs[0].FirstStage().Envs; !maps.Equal(got, wantStage) {
		t.Errorf("stage = %v, want %v", got, wantStage)
	}
}
