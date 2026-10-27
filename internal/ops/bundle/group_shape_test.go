package bundle

import (
	"maps"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A Mate's project as it really is (owner's decision, 2026-10-27): every part
// of a monorepo is its own dev pool and its own stage, nothing shares a
// container, one repository in HQ. The recipe describes that project: the AI
// Agent tier — the one a second Mate is born from — gives every part its dev
// pool and its stage, and each runtime of every tier is typed and named from
// its own part, never from a dev half that serves another.

const appsRepo = "https://hq.example.com/git/org-1/app-1/apps"

const poolMonorepoYAML = `zerops:
  - setup: webpool
    build:
      base: nodejs@24
    run:
      base: nodejs@24
      start: zcp-devsup run
  - setup: web
    build:
      base: nodejs@24
      buildCommands:
        - pnpm --filter web build
      deployFiles: apps/web/.output
    run:
      base: nodejs@24
      start: node apps/web/.output/server/index.mjs
  - setup: hqpool
    build:
      base: nodejs@24
    run:
      base: nodejs@24
      start: zcp-devsup run
  - setup: hq
    build:
      base: nodejs@24
      buildCommands:
        - pnpm --filter hq build
      deployFiles: apps/hq/dist/~
    run:
      base: static
  - setup: boxpool
    build:
      base: nodejs@24
    run:
      base: nodejs@24
      start: zcp-devsup run
  - setup: box
    build:
      base: nodejs@24
      buildCommands:
        - pnpm --filter box build
      deployFiles: apps/box/dist/~
    run:
      base: static
`

// poolPart is one part of a pool-keyed monorepo: its dev pool, keying the
// pair, and its stage as the live project runs it.
func poolPart(part, repo, body, stageType, stageSetup string, stageScaling *Scaling) GroupRuntime {
	return GroupRuntime{
		DevHostname: part + "pool", DevPool: true, ServiceType: "swarm@1",
		RepoURL: repo, SetupName: part + "pool", ZeropsYAMLBody: body,
		Scaling: &Scaling{MinContainers: 0, MaxContainers: 3, CPUMode: "SHARED", MinCPU: 1, MaxCPU: 8, MinRAM: 4, MaxRAM: 16, MinDisk: 1, MaxDisk: 100},
		ServiceEnvs: []ProjectEnvVar{
			{Key: "sshIsolation", Value: "vpn service@zcp"},
			{Key: "NODE_ENV", Value: "development"},
		},
		Stages: []GroupStage{{
			Hostname: part + "stage", SetupName: stageSetup,
			ServiceType: stageType, Scaling: stageScaling, SubdomainEnabled: true,
		}},
	}
}

// poolMonorepoInputs is Zerops Apps as it runs (2026-10-27): web, hq and box,
// each its dev pool and its stage on one repository — the web a node server,
// hq and box static sites.
func poolMonorepoInputs() GroupRecipeInputs {
	nodeStage := &Scaling{MinContainers: 1, MaxContainers: 1, CPUMode: "SHARED", MinCPU: 1, MaxCPU: 4, MinRAM: 0.5, MaxRAM: 4}
	staticStage := &Scaling{MinContainers: 1, MaxContainers: 1, CPUMode: "SHARED", MinCPU: 1, MaxCPU: 2, MinRAM: 0.25, MaxRAM: 1}
	return GroupRecipeInputs{
		Name:            "zerops-apps",
		Title:           "Zerops Apps",
		MateProjectName: "Zerops Apps - Jago",
		MateProjectID:   "p-jago",
		CorePackage:     "LIGHT",
		Runtimes: []GroupRuntime{
			poolPart("web", appsRepo, poolMonorepoYAML, "alpine/nodejs@24", "web", nodeStage),
			poolPart("hq", appsRepo, poolMonorepoYAML, "alpine/static@1.0", "hq", staticStage),
			poolPart("box", appsRepo, poolMonorepoYAML, "alpine/static@1.0", "box", staticStage),
		},
	}
}

const noolaRepo = "https://hq.example.com/git/org-1/app-2/noola"

const polyglotYAML = `zerops:
  - setup: apipool
    run:
      base: nodejs@24
      start: zcp-devsup run
  - setup: apiprod
    build:
      base: nodejs@24
      deployFiles: apps/api
    run:
      base: nodejs@24
      start: node apps/api/index.js
  - setup: edgepool
    run:
      base: elixir@1.16
      start: zcp-devsup run
  - setup: edgeprod
    build:
      base: elixir@1.16
      deployFiles: apps/edge/_build/prod/rel/edge
    run:
      base: elixir@1.16
      os: alpine
      start: bin/edge start
  - setup: geopool
    run:
      base: nodejs@24
      start: zcp-devsup run
  - setup: geoprod
    build:
      base: nodejs@24
      deployFiles: apps/geo
    run:
      base: nodejs@24
      start: node apps/geo/index.js
  - setup: sitepool
    run:
      base: nodejs@24
      start: zcp-devsup run
  - setup: siteprod
    build:
      base: nodejs@24
      deployFiles: apps/site/dist/~
    run:
      base: static
  - setup: webpool
    run:
      base: nodejs@24
      start: zcp-devsup run
  - setup: webprod
    build:
      base: nodejs@24
      deployFiles: apps/web/dist/~
    run:
      base: static
`

// polyglotInputs is Noola as it runs (2026-10-27): an api in node, an edge
// in elixir, a site and a web served static — each its dev pool and its stage
// — a geo part whose stage is not running, the data they share, and a mail
// catcher only the Mate's own project needs.
func polyglotInputs() GroupRecipeInputs {
	scale := &Scaling{MinContainers: 1, MaxContainers: 1, CPUMode: "SHARED", MinCPU: 1, MaxCPU: 8, MinRAM: 1, MaxRAM: 48, MinDisk: 1, MaxDisk: 250}
	geo := poolPart("geo", noolaRepo, polyglotYAML, "", "geoprod", nil)
	return GroupRecipeInputs{
		Name:            "noola",
		Title:           "noola",
		MateProjectName: "Noola - Odin",
		MateProjectID:   "p-odin",
		CorePackage:     "LIGHT",
		ProjectEnvs: []ProjectEnvVar{
			{Key: "API_INTERNAL_URL", Value: "http://apistage:3000"},
			{Key: "SMTP_HOST", Value: "mailpit"},
			{Key: "SMTP_PORT", Value: "1025"},
		},
		Runtimes: []GroupRuntime{
			poolPart("api", noolaRepo, polyglotYAML, "ubuntu/nodejs@24", "apiprod", scale),
			poolPart("edge", noolaRepo, polyglotYAML, "alpine/elixir@1.16.3", "edgeprod", scale),
			geo,
			poolPart("site", noolaRepo, polyglotYAML, "alpine/static@1.0", "siteprod", scale),
			poolPart("web", noolaRepo, polyglotYAML, "alpine/static@1.0", "webprod", scale),
		},
		ManagedServices: []ManagedServiceEntry{
			{Hostname: "cache", Type: "valkey:single@7.2", Profile: "hobby"},
			{Hostname: "db", Type: "postgresql:single@18", Profile: "oltp-hobby"},
		},
		Utilities: []GroupUtility{{
			Hostname: "mailpit", ServiceType: "alpine@3.24",
			BuildFromGit:     "https://github.com/zeropsio/recipe-mailpit",
			SubdomainEnabled: true,
			Scaling:          &Scaling{MinContainers: 1, MaxContainers: 10, CPUMode: "SHARED", MinCPU: 1, MaxCPU: 8, MinRAM: 0.125, MaxRAM: 48},
		}},
	}
}

// tierServiceTypes reads a tier's services as hostname → type.
func tierServiceTypes(t *testing.T, body string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for host, entry := range tierEntries(t, body) {
		out[host], _ = entry["type"].(string)
	}
	return out
}

// tierEntries reads a tier's services by hostname.
func tierEntries(t *testing.T, body string) map[string]map[string]any {
	t.Helper()
	var doc struct {
		Services []map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("tier does not parse: %v\n%s", err, body)
	}
	out := map[string]map[string]any{}
	for _, svc := range doc.Services {
		host, _ := svc["hostname"].(string)
		out[host] = svc
	}
	return out
}

// TestBuildGroupRecipe_PoolKeyedMonorepo_PoolAndStagePerPart: three parts,
// each its dev pool and its stage on one repository. The AI Agent tier gives
// a Mate made from it the same shape — a dev pool per part, written as zcp
// imports a part's pool (no build, no containers until a lease asks, the
// sshIsolation that admits zcp, no credential) — and its stage; Stage and
// Small Production run one runtime per part, named after the part and typed
// as its stage runs. No dev service stands in for the pools, and nothing is
// typed after the pools or after the web.
func TestBuildGroupRecipe_PoolKeyedMonorepo_PoolAndStagePerPart(t *testing.T) {
	t.Parallel()
	assertGroupGolden(t, poolMonorepoInputs(), "pool-monorepo")

	layout, _, err := BuildGroupRecipe(poolMonorepoInputs())
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	want := map[string]map[string]string{
		"AI Agent": {
			"webpool": "swarm@1", "webstage": "alpine/nodejs@24",
			"hqpool": "swarm@1", "hqstage": "alpine/static@1.0",
			"boxpool": "swarm@1", "boxstage": "alpine/static@1.0",
		},
		"Stage":            {"web": "alpine/nodejs@24", "hq": "alpine/static@1.0", "box": "alpine/static@1.0"},
		"Small Production": {"web": "alpine/nodejs@24", "hq": "alpine/static@1.0", "box": "alpine/static@1.0"},
	}
	for _, tier := range layout.Tiers {
		if got := tierServiceTypes(t, tier.ImportYAML); !maps.Equal(got, want[tier.Title]) {
			t.Errorf("tier %q services = %v, want %v", tier.Title, got, want[tier.Title])
		}
		if strings.Contains(tier.ImportYAML, "GIT_TOKEN") {
			t.Errorf("tier %q names GIT_TOKEN: a pool of a repository in HQ takes the Mate's credential from zcp, never a person's token", tier.Title)
		}
	}
	agent := tierEntries(t, tierDocBody(t, layout, "AI Agent"))
	for _, pool := range []string{"webpool", "hqpool", "boxpool"} {
		entry := agent[pool]
		if entry["minContainers"] != 0 || entry["maxContainers"] != 3 {
			t.Errorf("%s containers = %v..%v, want 0..3 (a pool holds no container until a lease asks)", pool, entry["minContainers"], entry["maxContainers"])
		}
		for _, key := range []string{"buildFromGit", "zeropsSetup", "enableSubdomainAccess"} {
			if _, ok := entry[key]; ok {
				t.Errorf("%s carries %s: a pool's image is deployed from a lease, never built by the import", pool, key)
			}
		}
		vault, _ := entry["vault"].(map[string]any)
		if vault["sshIsolation"] != "vpn service@zcp" {
			t.Errorf("%s vault = %v, want the sshIsolation that admits zcp", pool, vault)
		}
	}
	for _, stage := range []string{"webstage", "hqstage", "boxstage"} {
		if agent[stage]["buildFromGit"] != appsRepo {
			t.Errorf("%s buildFromGit = %v, want the monorepo's one repository", stage, agent[stage]["buildFromGit"])
		}
	}
}

// TestBuildGroupRecipe_Polyglot_EachRuntimeFromItsOwnPart: parts in node,
// elixir and static, each typed as its own stage runs — a part whose stage is
// not running by its setup's run.base, never as its pool — named after the
// part in Stage and Small Production, a value naming a stage naming the
// part's runtime there; the data on every tier; and the mail catcher, a
// dev-only service, on the AI Agent tier alone.
func TestBuildGroupRecipe_Polyglot_EachRuntimeFromItsOwnPart(t *testing.T) {
	t.Parallel()
	assertGroupGolden(t, polyglotInputs(), "polyglot")

	layout, warnings, err := BuildGroupRecipe(polyglotInputs())
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	parts := map[string]string{
		"api": "ubuntu/nodejs@24", "edge": "alpine/elixir@1.16.3", "geo": "nodejs@24",
		"site": "alpine/static@1.0", "web": "alpine/static@1.0",
		"cache": "", "db": "",
	}
	for _, title := range []string{"Stage", "Small Production"} {
		got := tierServiceTypes(t, tierDocBody(t, layout, title))
		if _, ok := got["mailpit"]; ok {
			t.Errorf("tier %q carries mailpit: a dev-only service stays in the Mate's own project", title)
		}
		for host, typ := range parts {
			gotType, ok := got[host]
			switch {
			case !ok:
				t.Errorf("tier %q lacks %s (has %v)", title, host, got)
			case typ != "" && gotType != typ:
				t.Errorf("tier %q %s type = %q, want %q", title, host, gotType, typ)
			}
		}
	}
	agent := tierServiceTypes(t, tierDocBody(t, layout, "AI Agent"))
	if agent["mailpit"] != "alpine@3.24" || agent["geostage"] != "nodejs@24" || agent["geopool"] != "swarm@1" {
		t.Errorf("AI Agent services = %v, want mailpit kept, geostage typed by geoprod's run.base, geopool a pool", agent)
	}
	if stage := tierDocBody(t, layout, "Stage"); !strings.Contains(stage, "API_INTERNAL_URL: http://api:3000") {
		t.Errorf("Stage tier does not name the api part's runtime:\n%s", stage)
	}
	if !warningsContain(warnings, `"mailpit"`) {
		t.Errorf("warnings %v do not say mailpit stays in the AI Agent tier", warnings)
	}
}

// TestBuildGroupRecipe_DevOnlyUtilities_OnlyInTheAIAgentTier: a mail catcher
// or a database admin UI is the Mate's own tooling — production once came up
// with the medusa group's mailpit (D30) — so the group environments leave it
// out; any other public-build utility runs in every tier.
func TestBuildGroupRecipe_DevOnlyUtilities_OnlyInTheAIAgentTier(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		utility GroupUtility
		devOnly bool
	}{
		{"mailpit by its recipe", GroupUtility{Hostname: "mail", ServiceType: "alpine@3.24", BuildFromGit: "https://github.com/zeropsio/recipe-mailpit"}, true},
		{"mailpit by its hostname", GroupUtility{Hostname: "mailpit", ServiceType: "alpine@3.24", BuildFromGit: "https://github.com/acme/mail-catcher"}, true},
		{"adminer", GroupUtility{Hostname: "adminer", ServiceType: "php-apache@8.3", BuildFromGit: "https://github.com/zeropsio/recipe-adminer"}, true},
		{"an image proxy", GroupUtility{Hostname: "imgproxy", ServiceType: "ubuntu@24.04", BuildFromGit: "https://github.com/acme/imgproxy"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inputs := groupInputsFixture()
			inputs.Utilities = []GroupUtility{tt.utility}
			layout, _, err := BuildGroupRecipe(inputs)
			if err != nil {
				t.Fatalf("BuildGroupRecipe: %v", err)
			}
			for _, tier := range layout.Tiers {
				_, carried := tierServiceTypes(t, tier.ImportYAML)[tt.utility.Hostname]
				want := tier.Title == "AI Agent" || !tt.devOnly
				if carried != want {
					t.Errorf("tier %q carries %s = %v, want %v", tier.Title, tt.utility.Hostname, carried, want)
				}
			}
		})
	}
}

// TestTierStamp_NamesTheMateAndHoldsOnlyWhileUntouched: every tier zcp
// composes carries its mark — the Mate project that composed it and a digest
// of the file — so zcp can tell a tier it wrote and nobody changed since from
// one a person or another Mate wrote or edited. Any edit, of a value or of a
// comment, breaks it; a file with no mark is no tier zcp marked.
func TestTierStamp_NamesTheMateAndHoldsOnlyWhileUntouched(t *testing.T) {
	t.Parallel()
	layout, _, err := BuildGroupRecipe(poolMonorepoInputs())
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	for _, tier := range layout.Tiers {
		mate, intact, stamped := TierStamp(tier.ImportYAML)
		if !stamped || !intact || mate != "p-jago" {
			t.Errorf("tier %q stamp = %q intact %v stamped %v, want p-jago, intact", tier.Title, mate, intact, stamped)
		}
		if !strings.HasPrefix(tier.ImportYAML, "# ") && !strings.HasPrefix(tier.ImportYAML, "#zeropsPreprocessor=on\n") {
			t.Errorf("tier %q does not open with its header", tier.Title)
		}
	}
	body := tierDocBody(t, layout, "Stage")
	edits := map[string]string{
		"a value":      strings.Replace(body, "minRam: 0.25", "minRam: 0.5", 1),
		"a comment":    strings.Replace(body, "The Stage tier.", "Our stage.", 1),
		"a blank line": body + "\n",
	}
	for name, edited := range edits {
		if edited == body {
			t.Fatalf("edit %q changed nothing", name)
		}
		if _, intact, stamped := TierStamp(edited); !stamped || intact {
			t.Errorf("edit of %s: intact %v stamped %v, want stamped and not intact", name, intact, stamped)
		}
	}
	if _, _, stamped := TierStamp("project:\n  name: acme\nservices: []\n"); stamped {
		t.Error("a file with no mark reads as stamped")
	}
	restamped := StampTier(edits["a value"], "p-other")
	if mate, intact, _ := TierStamp(restamped); mate != "p-other" || !intact {
		t.Errorf("restamped = %q intact %v, want p-other intact", mate, intact)
	}
	if strings.Count(restamped, "# zcp-recipe: ") != 1 {
		t.Errorf("restamping left more than one mark:\n%s", restamped)
	}
}

// TestTierShape_ReadsWhatTheTierCreates: a tier's shape is the set of
// services it creates — a part by its repository, setup and type, any other
// service by its hostname and type — so a scale, a variable, a comment or a
// part's hostname changes no shape, and a pool migration, a part added or
// removed or a type changed does.
func TestTierShape_ReadsWhatTheTierCreates(t *testing.T) {
	t.Parallel()
	layout, _, err := BuildGroupRecipe(poolMonorepoInputs())
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	body := tierDocBody(t, layout, "Stage")
	same := strings.Replace(strings.Replace(body, "minRam: 0.25", "minRam: 0.5", 1), "The Stage tier.", "Our stage.", 1)
	same = strings.Replace(same, "hostname: web\n", "hostname: storefront\n", 1)
	if same == body || !strings.Contains(same, "hostname: storefront") {
		t.Fatalf("the edits did not apply:\n%s", same)
	}
	if !slicesEqual(TierShape(body), TierShape(same)) {
		t.Errorf("a scale, a comment or a part's hostname changed the shape: %v vs %v", TierShape(body), TierShape(same))
	}
	retyped := strings.Replace(body, "type: alpine/nodejs@24", "type: ubuntu/nodejs@24", 1)
	if slicesEqual(TierShape(body), TierShape(retyped)) {
		t.Error("a type change left the shape as it was")
	}
	inputs := poolMonorepoInputs()
	inputs.Runtimes = inputs.Runtimes[:2]
	fewer, _, err := BuildGroupRecipe(inputs)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	if slicesEqual(TierShape(body), TierShape(tierDocBody(t, fewer, "Stage"))) {
		t.Error("a part removed left the shape as it was")
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
