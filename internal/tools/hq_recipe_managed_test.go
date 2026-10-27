// Tests for: tools/hq_recipe_managed.go — a managed service the Mate's
// project gained reaching the tiers on main as a targeted "add service X"
// proposal, run against a fake HQ that serves real git (hq_lab_test.go).
package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
)

// cacheService is a Valkey the Mate adds to its project after the group's
// recipe landed on main.
var cacheService = platform.ServiceStack{ID: "svc-cache", Name: "cache", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "valkey:single@7.2", ServiceStackTypeCategoryName: "USER"}}

// tierEntryType is the type a tier's entry for host writes, "" when the
// tier names no host.
func tierEntryType(body, host string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "- hostname: "+host {
			continue
		}
		for _, field := range lines[i+1:] {
			if strings.HasPrefix(strings.TrimSpace(field), "- ") || strings.TrimSpace(field) == "" {
				break
			}
			if value, ok := strings.CutPrefix(strings.TrimSpace(field), "type: "); ok {
				return value
			}
		}
	}
	return ""
}

// A managed service added to a one-part app's Mate project is proposed into
// every tier that lacks it, shaped by each tier's policy, as one recipe
// change of the Mate's: the reconcile names the call and says merging it
// imports the service into every stage or production environment made from
// those tiers, which is why it waits for a person; the proposal adds that
// service alone, ahead of the runtimes, every other line — a person's edits
// included — as main has it, and says the same of its merge.
func TestGroupRecipeService_ManagedAdded_ProposedIntoEveryTierLackingIt(t *testing.T) {
	t.Parallel()
	lab, main := newPartsLab(t)
	client := recipeReconcileClient(cacheService)

	outcome := lab.proposeRecipe(client)
	if outcome.Change != 0 || !strings.Contains(outcome.Line, `action="group-recipe" service="cache"`) {
		t.Fatalf("reconcile after adding a cache = %+v, want the steer to service=cache", outcome)
	}
	if !strings.Contains(outcome.Line, "Merging the change imports it into every stage or production environment made from those tiers, so it waits for a person to merge it unless none is made from them") ||
		strings.Contains(outcome.Line, "added themselves") || strings.Contains(outcome.Line, "by a person or the Chief") {
		t.Errorf("the steer does not say merging the change imports the cache into the stage and production environments made from those tiers: %s", outcome.Line)
	}
	if strings.Contains(outcome.Line, `service="db"`) {
		t.Errorf("the steer names the database every tier already lists: %s", outcome.Line)
	}
	if !strings.Contains(outcome.Line, "to propose each as the Mate's recipe change") || strings.Contains(outcome.Line, "the person reviews") {
		t.Errorf("the steer does not name the Mate's recipe change: %s", outcome.Line)
	}

	answer, text := lab.proposeService(client, "cache")
	if answer == nil || answer.Change != 2 || len(answer.Tiers) != 3 {
		t.Fatalf("service=cache = %s, want change #2 adding it to three tiers", text)
	}
	if change := lab.hq.changeIn(hq.RecipeRepo, 2); change == nil || change.Title != "Mate: cache in the group recipe" {
		t.Fatalf("change #2 = %+v", change)
	}
	if !strings.Contains(answer.Message, "Merging it imports cache into every stage or production environment made from the tiers it changes.") ||
		strings.Contains(answer.Message, "added themselves") || strings.Contains(answer.Message, "by a person or the Chief") {
		t.Errorf("the proposal does not say merging it imports the cache into the stage and production environments made from its tiers: %s", answer.Message)
	}
	proposed := lab.hq.recipeFiles(recipeBranch(2))
	wantType := map[string]string{
		hq.RecipeTierPaths[hq.RecipeTierMate]:       "valkey:single@7.2",
		hq.RecipeTierPaths[hq.RecipeTierStage]:      "valkey:single@7.2",
		hq.RecipeTierPaths[hq.RecipeTierProduction]: "valkey:ha@7.2",
	}
	for path, body := range proposed {
		want, targeted := wantType[path]
		if !targeted {
			if body != main[path] {
				t.Errorf("%s changed, which no tier edit names", path)
			}
			continue
		}
		if got := tierEntryType(body, "cache"); got != want {
			t.Errorf("%s writes the cache as %q, want %q:\n%s", path, got, want, body)
		}
		if back, err := bundle.RemoveTierService(body, "cache"); err != nil || back != main[path] {
			t.Errorf("%s: the proposal changes more than the cache (%v):\n%s", path, err, body)
		}
		for _, held := range bundle.HeldRuntimes(body) {
			if strings.Index(body, "- hostname: cache\n") > strings.Index(body, "- hostname: "+held.Hostname+"\n") {
				t.Errorf("%s lists the cache after the runtime %s, which reads it:\n%s", path, held.Hostname, body)
			}
		}
	}
	if stage := proposed[hq.RecipeTierPaths[hq.RecipeTierStage]]; !strings.Contains(stage, "hostname: shop") || !strings.Contains(stage, "# edited by the group") {
		t.Errorf("a person's edit in the Stage tier was not kept:\n%s", stage)
	}

	// Merged, the recipe carries the cache and nothing is said again.
	lab.hq.mergeChange(hq.RecipeRepo, 2)
	if outcome := lab.proposeRecipe(client); strings.Contains(outcome.Line, `service="cache"`) {
		t.Errorf("reconcile after the cache merged = %+v, want no steer to it", outcome)
	}
}

// A tier that already lists the service is left as it is: a person who
// wrote the cache into one tier by hand gets it proposed into the others
// alone.
func TestGroupRecipeService_ManagedAlreadyInATier_LeavesThatTier(t *testing.T) {
	t.Parallel()
	lab, _ := newPartsLab(t)
	mateTier := hq.RecipeTierPaths[hq.RecipeTierMate]
	withCache, err := bundle.AddTierServiceInRank(lab.hq.recipeFiles("main")[mateTier],
		"services:\n  - hostname: cache\n    type: valkey@7.2\n    mode: NON_HA\n    priority: 10\n    # written by Eva\n", "cache")
	if err != nil {
		t.Fatal(err)
	}
	lab.hq.landOnMain(hq.RecipeRepo, map[string]string{mateTier: withCache})
	main := lab.hq.recipeFiles("main")

	answer, text := lab.proposeService(recipeReconcileClient(cacheService), "cache")
	if answer == nil || answer.Change != 2 || len(answer.Tiers) != 2 {
		t.Fatalf("service=cache = %s, want change #2 adding it to the two tiers that lack it", text)
	}
	if body := lab.hq.recipeFiles(recipeBranch(2))[mateTier]; body != main[mateTier] {
		t.Errorf("the AI Agent tier, which lists the cache, changed:\n%s", body)
	}
}

// A database and a cache added to a one-part app's Mate project — the
// tiers on main listing only the runtime — are each proposed into every
// tier, one change at a time, and the small production tier gets the
// database highly available.
func TestGroupRecipeService_DatabaseAndCache_EachProposedInTurn(t *testing.T) {
	t.Parallel()
	lab, _ := newPartsLab(t)
	runtimeOnly := map[string]string{}
	for _, tier := range []string{hq.RecipeTierMate, hq.RecipeTierStage, hq.RecipeTierProduction} {
		path := hq.RecipeTierPaths[tier]
		body, err := bundle.RemoveTierService(lab.hq.recipeFiles("main")[path], "db")
		if err != nil {
			t.Fatal(err)
		}
		runtimeOnly[path] = body
	}
	lab.hq.landOnMain(hq.RecipeRepo, runtimeOnly)
	client := recipeReconcileClient(cacheService)

	outcome := lab.proposeRecipe(client)
	if !strings.Contains(outcome.Line, `service="db"`) || !strings.Contains(outcome.Line, `service="cache"`) {
		t.Fatalf("reconcile over tiers lacking both = %+v, want a steer to each", outcome)
	}
	if answer, text := lab.proposeService(client, "db"); answer == nil || answer.Change != 2 || len(answer.Tiers) != 3 {
		t.Fatalf("service=db = %s", text)
	}
	if _, text := lab.proposeService(client, "cache"); !strings.Contains(text, "one recipe proposal open at a time") {
		t.Errorf("service=cache while the database's change is open = %s, want it to wait", text)
	}
	production := lab.hq.recipeFiles(recipeBranch(2))[hq.RecipeTierPaths[hq.RecipeTierProduction]]
	if got := tierEntryType(production, "db"); got != "postgresql:ha@18" {
		t.Errorf("the Small Production tier writes the database as %q, want postgresql:ha@18:\n%s", got, production)
	}
	lab.hq.mergeChange(hq.RecipeRepo, 2)
	if answer, text := lab.proposeService(client, "cache"); answer == nil || answer.Change != 3 || len(answer.Tiers) != 3 {
		t.Fatalf("service=cache once the database merged = %s", text)
	}
	for path, body := range lab.hq.recipeFiles(recipeBranch(3)) {
		if _, tier := runtimeOnly[path]; tier && (!bundle.TierNamesHost(body, "db") || !bundle.TierNamesHost(body, "cache")) {
			t.Errorf("%s does not list both the database and the cache:\n%s", path, body)
		}
	}
}

// searchService is a Meilisearch the Mate adds beside the cache.
var searchService = platform.ServiceStack{ID: "svc-search", Name: "search", Status: "ACTIVE",
	ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "meilisearch:single@1.44", ServiceStackTypeCategoryName: "USER"},
	CustomAutoscaling:    &platform.CustomAutoscaling{CPUMode: "SHARED", MinCPU: 1, MaxCPU: 8, MinRAM: 2, MaxRAM: 48, MinFreeRAMGB: 0.5}}

// A person closing a managed service's change unmerged silences its
// reminder: the reconcile stops naming the call until a later recipe change
// of the Mate's merges, and then asks again.
func TestGroupRecipeService_ManagedDeclined_ReminderStopsUntilALaterMerge(t *testing.T) {
	t.Parallel()
	lab, _ := newPartsLab(t)
	client := recipeReconcileClient(cacheService)
	if answer, text := lab.proposeService(client, "cache"); answer == nil || answer.Change != 2 {
		t.Fatalf("service=cache = %s", text)
	}
	lab.hq.closeChange(hq.RecipeRepo, 2)

	if outcome := lab.proposeRecipe(client); strings.Contains(outcome.Line, `service="cache"`) {
		t.Errorf("reconcile after the person closed the cache's change = %q, want no steer to it", outcome.Line)
	}

	client = recipeReconcileClient(cacheService, searchService)
	outcome := lab.proposeRecipe(client)
	if !strings.Contains(outcome.Line, `service="search"`) || strings.Contains(outcome.Line, `service="cache"`) {
		t.Fatalf("reconcile with a search added = %q, want the steer to search alone", outcome.Line)
	}
	if answer, text := lab.proposeService(client, "search"); answer == nil || answer.Change != 3 {
		t.Fatalf("service=search = %s", text)
	}
	lab.hq.mergeChange(hq.RecipeRepo, 3)
	if outcome := lab.proposeRecipe(client); !strings.Contains(outcome.Line, `service="cache"`) {
		t.Errorf("reconcile after a later recipe change merged = %q, want the steer to the cache again", outcome.Line)
	}
}

// A managed service no tier on main can take by zcp's edit is said once
// per pass as one to add by hand, with no call to make.
func TestGroupRecipeService_ManagedNoTierCanTakeIt_SaysToAddByHand(t *testing.T) {
	t.Parallel()
	lab, _ := newPartsLab(t)
	flow := map[string]string{}
	for _, tier := range []string{hq.RecipeTierMate, hq.RecipeTierStage, hq.RecipeTierProduction} {
		flow[hq.RecipeTierPaths[tier]] = "project:\n  name: acme\nservices: [{hostname: db, type: postgresql@18, priority: 10}, {hostname: app, type: nodejs@22, priority: 1}]\n"
	}
	lab.hq.landOnMain(hq.RecipeRepo, flow)
	client := recipeReconcileClient(cacheService)

	outcome := lab.proposeRecipe(client)
	if strings.Contains(outcome.Line, `service="cache"`) || strings.Count(outcome.Line, "by hand") != 1 || !strings.Contains(outcome.Line, "cache") {
		t.Errorf("reconcile over tiers no edit can take = %q, want the cache named once as one to add by hand", outcome.Line)
	}
	if answer, text := lab.proposeService(client, "cache"); answer == nil || answer.Change != 0 || !strings.Contains(answer.Message, "Left as main has it") {
		t.Errorf("service=cache = %s, want nothing proposed and each tier named", text)
	}
}

// A proposal for a managed service the Mate has since deleted is withdrawn
// by the same call: its open change is brought to main's tree and adds
// nothing.
func TestGroupRecipeService_ManagedDeleted_WithdrawsItsOpenChange(t *testing.T) {
	t.Parallel()
	lab, _ := newPartsLab(t)
	if answer, text := lab.proposeService(recipeReconcileClient(cacheService), "cache"); answer == nil || answer.Change != 2 {
		t.Fatalf("service=cache = %s", text)
	}

	answer, text := lab.proposeService(lab.mock, "cache")
	if answer == nil || answer.Change != 2 || !strings.Contains(answer.Message, "withdrawn") {
		t.Fatalf("service=cache once the cache is deleted = %s, want change #2 withdrawn", text)
	}
	if main, branch := lab.hq.recipeFiles("main"), lab.hq.recipeFiles(recipeBranch(2)); !equalFiles(main, branch) {
		t.Errorf("the withdrawn change still differs from main: %v", diffPaths(main, branch))
	}
	if _, text := lab.proposeService(lab.mock, "nothing"); !strings.Contains(text, "runs no managed service") {
		t.Errorf("service= for a host nothing names = %s, want it refused", text)
	}
}
