// Tests for: tools/hq_recipe_parts.go — a Mate's parts reaching the tiers on
// main of its group recipe (recipe_parts_SENTENCES.md), planned from literal
// tier files.
package tools

import (
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/recipe"
)

const (
	medusaRepo    = "https://hq.example.com/org-1/medusa.git"
	nextstoreRepo = "https://hq.example.com/org-1/nextstore.git"
	firmaRepo     = "https://hq.example.com/org-1/firma.git"
	dakarRepo     = "https://hq.example.com/org-1/dakar.git"
	shopRepo      = "https://hq.example.com/org-1/shop.git"
)

// tierEntry is a tier's service as the composer writes it.
func tierEntry(host, typ, repo, setup string) string {
	entry := "  - hostname: " + host + "\n    type: " + typ + "\n"
	if repo != "" {
		entry += "    buildFromGit: " + repo + "\n    zeropsSetup: " + setup + "\n"
	}
	return entry + "    priority: 0\n"
}

// poolEntry is a part's dev pool as the AI Agent tier writes it.
func poolEntry(host string) string {
	return "  # A dev pool: one container leased per branch of the repository its stage builds from.\n" +
		"  - hostname: " + host + "\n    type: swarm@1\n    priority: 0\n    minContainers: 0\n    maxContainers: 3\n"
}

var (
	dbEntry      = "  - hostname: db\n    type: postgresql@16\n    mode: NON_HA\n    priority: 10\n"
	storageEntry = "  - hostname: storage\n    type: object-storage\n    objectStorageSize: 2\n    priority: 10\n"
)

// tierBody is a tier file of project name carrying entries, a blank line
// between each.
func tierBody(name string, entries ...string) string {
	return "project:\n  name: " + name + "\nservices:\n" + strings.Join(entries, "\n")
}

// bigShock is the Big Shock application's tiers: main carrying two classic
// pairs (medusa, nextstore) beside a utility and managed services — Stage
// and Small Production with the storage written after the runtimes by hand
// — and the Mate's composition adding a part on the new repository firma,
// keyed by its dev pool, with dakar, a classic pair on a new repository,
// when dakar is set.
func bigShock(dakar bool) recipeTiers {
	medusa := []string{tierEntry("medusadev", "nodejs@22", medusaRepo, "dev"), tierEntry("medusastage", "nodejs@22", medusaRepo, "prod")}
	nextstore := []string{tierEntry("nextstoredev", "nodejs@22", nextstoreRepo, "dev"), tierEntry("nextstorestage", "nodejs@22", nextstoreRepo, "prod")}
	mailpit := tierEntry("mailpit", "go@1", "", "")
	agent := append(append([]string{dbEntry, storageEntry, mailpit}, medusa...), nextstore...)
	group := func(name string) string {
		return tierBody(name, dbEntry, tierEntry("medusa", "nodejs@22", medusaRepo, "prod"), tierEntry("nextstore", "nodejs@22", nextstoreRepo, "prod"), storageEntry)
	}
	runtimes := []bundle.GroupRuntime{
		{DevHostname: "firmapool", DevPool: true, RepoURL: firmaRepo, SetupName: "firmapool", Stages: []bundle.GroupStage{{Hostname: "firmastage", SetupName: "prod"}}},
		{DevHostname: "medusadev", RepoURL: medusaRepo, SetupName: "dev", Stages: []bundle.GroupStage{{Hostname: "medusastage", SetupName: "prod"}}},
		{DevHostname: "nextstoredev", RepoURL: nextstoreRepo, SetupName: "dev", Stages: []bundle.GroupStage{{Hostname: "nextstorestage", SetupName: "prod"}}},
	}
	newAgent := []string{poolEntry("firmapool"), tierEntry("firmastage", "nodejs@22", firmaRepo, "prod")}
	newGroup := []string{tierEntry("firma", "nodejs@22", firmaRepo, "prod")}
	if dakar {
		runtimes = append(runtimes, bundle.GroupRuntime{DevHostname: "dakardev", RepoURL: dakarRepo, SetupName: "dev", Stages: []bundle.GroupStage{{Hostname: "dakarstage", SetupName: "prod"}}})
		newAgent = append([]string{tierEntry("dakardev", "nodejs@22", dakarRepo, "dev"), tierEntry("dakarstage", "nodejs@22", dakarRepo, "prod")}, newAgent...)
		newGroup = append([]string{tierEntry("dakar", "nodejs@22", dakarRepo, "prod")}, newGroup...)
	}
	composedGroup := func(name string) string {
		return tierBody(name, append(append([]string{dbEntry, storageEntry}, newGroup...),
			tierEntry("medusa", "nodejs@22", medusaRepo, "prod"), tierEntry("nextstore", "nodejs@22", nextstoreRepo, "prod"))...)
	}
	paths := hq.RecipeTierPaths
	return recipeTiers{
		composed: []recipe.File{
			{Path: paths[hq.RecipeTierMate], Body: tierBody("bob-shock", append(append([]string{dbEntry, storageEntry, mailpit}, newAgent...), append(medusa, nextstore...)...)...)},
			{Path: paths[hq.RecipeTierStage], Body: composedGroup("big-shock-stage")},
			{Path: paths[hq.RecipeTierProduction], Body: composedGroup("big-shock-production")},
		},
		main: map[string]string{
			paths[hq.RecipeTierMate]:       tierBody("bob-shock", agent...),
			paths[hq.RecipeTierStage]:      group("big-shock-stage"),
			paths[hq.RecipeTierProduction]: group("big-shock-production"),
		},
		runtimes: runtimes,
	}
}

// checkOnlyAdded fails unless each planned tier lists want, in its order,
// and taking the added services out gives main's body back.
func checkOnlyAdded(t *testing.T, tiers recipeTiers, planned []recipeServiceTier, want map[string][]string) {
	t.Helper()
	for _, tier := range planned {
		if got := bundle.TierHostnames(tier.body); !slices.Equal(got, want[tier.path]) {
			t.Errorf("%s lists %q, want %q", tier.path, got, want[tier.path])
		}
		back := tier.body
		for _, host := range tier.added {
			var err error
			if back, err = bundle.RemoveTierService(back, host); err != nil {
				t.Fatalf("%s: %v", tier.path, err)
			}
		}
		if back != tiers.main[tier.path] {
			t.Errorf("%s: the proposal changes more than the services it adds:\n%s", tier.path, tier.body)
		}
	}
}

// witness: internal/tools/hq_recipe_parts.go | if !t.carries(part.repo) { → if !t.carries(part.repo) && part.setup == "" {
func TestPlanRecipeService_NPEveryTier_ANewPartGoesIntoEveryTier(t *testing.T) {
	t.Parallel()
	tiers := bigShock(false)
	parts := tiers.newParts()
	if len(parts) != 1 || parts[0].host != "firmastage" {
		t.Fatalf("new parts = %+v, want firma's, named by firmastage", parts)
	}
	planned := tiers.plan([]recipePart{parts[0].part})
	want := []string{"0 — AI Agent: adds firmapool, firmastage", "3 — Stage: adds firma", "4 — Small Production: adds firma"}
	if got := serviceTierEdits(planned); !slices.Equal(got, want) {
		t.Errorf("edits = %q, want %q", got, want)
	}
	paths := hq.RecipeTierPaths
	group := []string{"db", "medusa", "nextstore", "firma", "storage"}
	checkOnlyAdded(t, tiers, planned, map[string][]string{
		paths[hq.RecipeTierMate]:       {"db", "storage", "mailpit", "medusadev", "medusastage", "nextstoredev", "nextstorestage", "firmapool", "firmastage"},
		paths[hq.RecipeTierStage]:      group,
		paths[hq.RecipeTierProduction]: group,
	})
}

// witness: internal/tools/hq_recipe_parts.go | if (r.DevPool || devs) && → if devs &&
func TestPlanRecipeService_NPPool_APoolKeyedPartBringsItsPool(t *testing.T) {
	t.Parallel()
	group := func(name string, worker bool) string {
		entries := []string{dbEntry, tierEntry("api", "nodejs@22", shopRepo, "api")}
		if worker {
			entries = append(entries, tierEntry("worker", "nodejs@22", shopRepo, "worker"))
		}
		return tierBody(name, entries...)
	}
	paths := hq.RecipeTierPaths
	tiers := recipeTiers{
		composed: []recipe.File{
			{Path: paths[hq.RecipeTierMate], Body: tierBody("shop-mate", dbEntry, poolEntry("apipool"), tierEntry("apistage", "nodejs@22", shopRepo, "api"),
				poolEntry("workerpool"), tierEntry("workerstage", "nodejs@22", shopRepo, "worker"))},
			{Path: paths[hq.RecipeTierStage], Body: group("shop-stage", true)},
			{Path: paths[hq.RecipeTierProduction], Body: group("shop-production", true)},
		},
		main: map[string]string{
			paths[hq.RecipeTierMate]:       tierBody("shop-mate", dbEntry, poolEntry("apipool"), tierEntry("apistage", "nodejs@22", shopRepo, "api")),
			paths[hq.RecipeTierStage]:      group("shop-stage", false),
			paths[hq.RecipeTierProduction]: group("shop-production", false),
		},
		runtimes: []bundle.GroupRuntime{
			{DevHostname: "apipool", DevPool: true, RepoURL: shopRepo, SetupName: "apipool", Stages: []bundle.GroupStage{{Hostname: "apistage", SetupName: "api"}}},
			{DevHostname: "workerpool", DevPool: true, RepoURL: shopRepo, SetupName: "workerpool", Stages: []bundle.GroupStage{{Hostname: "workerstage", SetupName: "worker"}}},
		},
	}
	part, found := tiers.partOf("workerstage")
	if !found {
		t.Fatal("workerstage names no part")
	}
	planned := tiers.plan([]recipePart{part})
	want := []string{"0 — AI Agent: adds workerpool, workerstage", "3 — Stage: adds worker", "4 — Small Production: adds worker"}
	if got := serviceTierEdits(planned); !slices.Equal(got, want) {
		t.Errorf("edits = %q, want %q", got, want)
	}
	checkOnlyAdded(t, tiers, planned, map[string][]string{
		paths[hq.RecipeTierMate]:       {"db", "apipool", "apistage", "workerpool", "workerstage"},
		paths[hq.RecipeTierStage]:      {"db", "api", "worker"},
		paths[hq.RecipeTierProduction]: {"db", "api", "worker"},
	})
}

// witness: internal/tools/hq_recipe_parts.go | case !carried: → case !carried && part.setup == "":
func TestPlanRecipeService_NPHandWritten_ATierPeopleWroteStaysAsWritten(t *testing.T) {
	t.Parallel()
	tiers := bigShock(false)
	production := hq.RecipeTierPaths[hq.RecipeTierProduction]
	tiers.main[production] = tierBody("big-shock-production", dbEntry, tierEntry("medusa", "nodejs@22", medusaRepo, "prod"), storageEntry)
	part, found := tiers.partOf("nextstorestage")
	if !found {
		t.Fatal("nextstorestage names no part")
	}
	planned := tiers.plan([]recipePart{part})
	if edits := serviceTierEdits(planned); len(edits) != 0 {
		t.Errorf("edits = %q, want none: Small Production was written without nextstore, which the other tiers carry", edits)
	}
	for _, tier := range planned {
		if tier.body != tiers.main[tier.path] {
			t.Errorf("%s changed:\n%s", tier.path, tier.body)
		}
	}
	if parts := tiers.newParts(); len(parts) != 1 || parts[0].host != "firmastage" {
		t.Errorf("new parts = %+v, want firma's alone", parts)
	}
}

// witness: internal/tools/hq_recipe_parts.go | if r.DevPool && r.DevHostname == host && len(r.Stages) > 0 { → if r.DevPool && r.DevHostname == host && len(r.Stages) > 1 {
func TestPlanRecipeService_NPService_APoolNamesItsStagesPart(t *testing.T) {
	t.Parallel()
	tiers := bigShock(false)
	stage, found := tiers.partOf("firmastage")
	if !found {
		t.Fatal("firmastage names no part")
	}
	for _, host := range []string{"firmastage", "firmapool"} {
		part, found := tiers.partOf(host)
		if !found || part != stage {
			t.Errorf("service=%s names %+v (%v), want firmastage's part %+v", host, part, found, stage)
			continue
		}
		if edits := serviceTierEdits(tiers.plan([]recipePart{part})); len(edits) != 3 {
			t.Errorf("service=%s proposes %q, want firma's part in every tier", host, edits)
		}
	}
}

// witness: internal/tools/hq_recipe_parts.go | if !declined(p.host) { → if declined(p.host) {
func TestRecipeNewPartsProposal_NPPass_EveryNewPartInOneProposal(t *testing.T) {
	t.Parallel()
	tiers := bigShock(true)
	hosts, parts := recipeNewPartsProposal(tiers, func(string) bool { return false })
	if !slices.Equal(hosts, []string{"dakarstage", "firmastage"}) {
		t.Fatalf("proposed %q, want dakarstage and firmastage, by their stages", hosts)
	}
	want := []string{"0 — AI Agent: adds dakardev, dakarstage, firmapool, firmastage", "3 — Stage: adds dakar, firma", "4 — Small Production: adds dakar, firma"}
	if got := serviceTierEdits(tiers.plan(parts)); !slices.Equal(got, want) {
		t.Errorf("edits = %q, want %q", got, want)
	}
	if hosts, _ := recipeNewPartsProposal(tiers, func(host string) bool { return host == "firmastage" }); !slices.Equal(hosts, []string{"dakarstage"}) {
		t.Errorf("with firmastage declined, proposed %q, want dakarstage alone", hosts)
	}
}

// witness: internal/tools/hq_recipe_parts.go | if parts.Change == 0 { → if parts.Change != 0 {
func TestRecipeNewPartsSteps_NPStatus_CallThenChangeThenNothing(t *testing.T) {
	t.Parallel()
	const url = "https://hq.example.com/changes/app-1/group/7"
	open := hq.MateState{Changes: []hq.MateChange{{Repo: hq.RecipeRepo, Number: 7, State: hq.ChangeOpen}}}
	merged := hq.MateState{Changes: []hq.MateChange{{Repo: hq.RecipeRepo, Number: 7, State: hq.ChangeMerged}}}
	unproposed := &recipeNewParts{Hosts: []string{"dakarstage", "firmastage"}}
	proposed := &recipeNewParts{Hosts: []string{"dakarstage", "firmastage"}, Change: 7, ChangeURL: url}
	tests := []struct {
		name  string
		parts *recipeNewParts
		state *hq.MateState
		want  []string
	}{
		{"no change proposes them", unproposed, &open, []string{`zerops_workflow action="group-recipe"`, "dakarstage, firmastage"}},
		{"their change is open", proposed, &open, []string{"#7", url, "dakarstage, firmastage"}},
		{"their change is open, state unheard", proposed, nil, []string{"#7", url}},
		{"their change merged", proposed, &merged, nil},
		{"no new parts", nil, &open, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			steps := recipeNewPartsSteps(tt.parts, tt.state)
			if tt.want == nil {
				if len(steps) != 0 {
					t.Errorf("steps = %q, want none", steps)
				}
				return
			}
			if len(steps) != 1 {
				t.Fatalf("steps = %q, want one", steps)
			}
			for _, part := range tt.want {
				if !strings.Contains(steps[0], part) {
					t.Errorf("step %q does not name %q", steps[0], part)
				}
			}
			if tt.parts.Change != 0 && strings.Contains(steps[0], `action="group-recipe"`) {
				t.Errorf("step %q asks for the call while change #7 proposes the parts", steps[0])
			}
		})
	}
}
