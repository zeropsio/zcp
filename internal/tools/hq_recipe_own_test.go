// Tests for: tools/hq_recipe.go — a tier zcp wrote from this Mate, and
// nobody changed since, is proposed anew when the Mate's project changes
// shape; a tier a person or another Mate wrote or edited never is (D30,
// spec-mate §2.8a.5).
package tools

import (
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/recipe"
	"github.com/zeropsio/zcp/internal/workflow"
)

const (
	recipeAgentTier = "0 — AI Agent/import.yaml"
	recipeStageTier = "3 — Stage/import.yaml"
	recipeProdTier  = "4 — Small Production/import.yaml"
)

// recipeBeforePools is the recipe an older zcp proposed from the lab's
// Mate before its pair moved onto a dev pool — written before tiers carried
// zcp's mark (mateID "") or with it: the dev service appdev beside its stage.
func recipeBeforePools(t *testing.T, lab *hqLab, mateID string) map[string]string {
	t.Helper()
	repoURL, err := hq.RepoURLAt(lab.rt.HQ.Origin, labOrg, labApp, "appdev")
	if err != nil {
		t.Fatal(err)
	}
	layout, _, err := bundle.BuildGroupRecipe(bundle.GroupRecipeInputs{
		Name: labApp, Title: labApp, MateProjectName: "acme-mate-1", MateProjectID: mateID,
		Runtimes: []bundle.GroupRuntime{{
			DevHostname: "appdev", ServiceType: "nodejs@22", RepoURL: repoURL, SetupName: "api",
			Stages: []bundle.GroupStage{{Hostname: "appstage", SetupName: "prod"}},
		}},
		ManagedServices: []bundle.ManagedServiceEntry{{Hostname: "db", Type: "postgresql:single@18"}},
	})
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	built, err := recipe.Build(layout)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, file := range built {
		files[file.Path] = file.Body
	}
	return files
}

// moveLabPairOntoPool moves the lab's pair onto its dev pool, as a pool
// migration's retire does, and its project to what runs after: the pool, the
// stage and the database.
func (l *hqLab) moveLabPairOntoPool() {
	l.t.Helper()
	keyOnPool(l.t, l.stateDir)
	pool := liveRuntime(testPoolID, testPool, "swarm@1")
	pool.SubdomainAccess = false
	pool.CurrentAutoscaling = &platform.CustomAutoscaling{HorizontalMinCount: 0, HorizontalMaxCount: 3, CPUMode: "SHARED", MinRAM: 2}
	services := []platform.ServiceStack{
		pool,
		liveRuntime("svc-appstage", "appstage", "nodejs@22"),
		{ID: "svc-db", Name: "db", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql:single@18", ServiceStackTypeCategoryName: "USER"}},
	}
	l.mock = platform.NewMock().
		WithProject(&platform.Project{ID: labMate, Name: "acme-mate-1", Status: "ACTIVE"}).
		WithServicesDirect(services).
		WithServices(services).
		WithServiceEnv(testPoolID, []platform.ServiceEnvVar{
			{Key: "sshIsolation", Content: "vpn service@zcp", Type: platform.ServiceEnvUser},
			{Key: "API_TOKEN", Content: "pool-api-token", Type: platform.ServiceEnvUser, Sensitive: true},
		})
	l.ssh.mock = l.mock
}

// TestGroupRecipe_ItsOwnTiersFromBeforeAPoolMigration_ProposedAnew: tiers
// zcp wrote from this Mate before its pair moved onto a dev pool — landed by
// Core from this Mate's proposals, before tiers carried zcp's mark, and
// nobody's change touched since — are proposed anew once the project's shape
// changed, as the Mate's recipe change, which HQ lands by itself unless its
// merge would change an environment made from a tier: the AI Agent tier
// gets the dev pool in the dev service's place, marked; a tier whose shape
// did not change (Stage, Small Production: the same runtime, type, setup and
// repository) is left as main has it, and so is every README. A person who
// closes the proposal unmerged is not asked again.
func TestGroupRecipe_ItsOwnTiersFromBeforeAPoolMigration_ProposedAnew(t *testing.T) {
	t.Parallel()
	lab := newRecipeLab(t)
	lab.hq.landRecipeAsSquash(labMate, hq.RecipeProposalTitle, 1, recipeBeforePools(t, lab, ""))
	main := lab.hq.recipeFiles("main")
	lab.moveLabPairOntoPool()

	outcome := lab.proposeRecipe(lab.mock)

	if outcome.Change == 0 || !outcome.Created {
		t.Fatalf("nothing proposed: %+v", outcome)
	}
	change := lab.hq.changeIn(hq.RecipeRepo, outcome.Change)
	if change.Title != hq.RecipeProposalTitle {
		t.Errorf("change title = %q, want %q", change.Title, hq.RecipeProposalTitle)
	}
	branch := lab.hq.recipeFiles(recipeBranch(outcome.Change))
	agent := branch[recipeAgentTier]
	for _, want := range []string{"hostname: " + testPool, "type: swarm@1", "minContainers: 0", "sshIsolation: vpn service@zcp", "hostname: appstage"} {
		if !strings.Contains(agent, want) {
			t.Errorf("the AI Agent tier proposed lacks %q:\n%s", want, agent)
		}
	}
	for _, unwanted := range []string{"hostname: appdev", "pool-api-token", "API_TOKEN"} {
		if strings.Contains(agent, unwanted) {
			t.Errorf("the AI Agent tier proposed carries %q:\n%s", unwanted, agent)
		}
	}
	if mate, intact, stamped := bundle.TierStamp(agent); !stamped || !intact || mate != labMate {
		t.Errorf("the proposed AI Agent tier's mark = %q intact %v stamped %v, want this Mate's", mate, intact, stamped)
	}
	for path, body := range main {
		if path == recipeAgentTier {
			continue
		}
		if branch[path] != body {
			t.Errorf("the proposal changes %q, whose shape did not change:\n%s", path, branch[path])
		}
	}
	if len(outcome.Proposed) != 1 || outcome.Proposed[0] != "0 — AI Agent" {
		t.Errorf("proposed = %v, want the AI Agent tier alone", outcome.Proposed)
	}
	for _, want := range []string{"0 — AI Agent", "HQ lands the change on main by itself", "import, stop or start a service in a stage or production environment made from a tier", "a person with production rights to accept it on the project's page", "Changes how"} {
		if !strings.Contains(outcome.Line, want) {
			t.Errorf("line %q does not say %q", outcome.Line, want)
		}
	}
	for _, not := range []string{"in Review", "Review"} {
		if strings.Contains(outcome.Line, not) {
			t.Errorf("line %q names %q, a place the person cannot find", outcome.Line, not)
		}
	}
	if strings.Contains(outcome.Line, "a person reviews and merges it") {
		t.Errorf("line %q says a person merges a change HQ may land by itself", outcome.Line)
	}

	lab.hq.closeChange(hq.RecipeRepo, outcome.Change)
	again := lab.proposeRecipe(lab.mock)
	if again.Change != 0 || again.Committed {
		t.Errorf("a proposal the person closed unmerged was made again: %+v", again)
	}
}

// TestGroupRecipe_ATierAPersonOrAnotherMateWrote_IsNeverProposedOver: a
// tier zcp did not write from this Mate, or one changed since it did — a
// person's change landed over it, another Mate's zcp wrote it, its mark is
// another Mate's, or its file no longer matches its own mark — stays as main
// has it however the project's shape changed. Nothing is proposed.
func TestGroupRecipe_ATierAPersonOrAnotherMateWrote_IsNeverProposedOver(t *testing.T) {
	t.Parallel()
	edited := func(files map[string]string) map[string]string {
		out := map[string]string{}
		for path, body := range files {
			out[path] = strings.Replace(body, "project:\n", "# Our own words.\nproject:\n", 1)
		}
		return out
	}
	tests := []struct {
		name string
		land func(lab *hqLab)
	}{
		{
			name: "a person's change landed over zcp's",
			land: func(lab *hqLab) {
				lab.hq.landRecipeAsSquash(labMate, hq.RecipeProposalTitle, 1, recipeBeforePools(t, lab, ""))
				lab.hq.landRecipeAsSquash(labMate, "Give the dev more memory", 2, edited(recipeBeforePools(t, lab, "")))
			},
		},
		{
			name: "another Mate's zcp wrote it",
			land: func(lab *hqLab) {
				lab.hq.landRecipeAsSquash("p-other", hq.RecipeProposalTitle, 1, recipeBeforePools(t, lab, ""))
			},
		},
		{
			name: "its mark is another Mate's",
			land: func(lab *hqLab) {
				lab.hq.landRecipeAsSquash(labMate, hq.RecipeProposalTitle, 1, recipeBeforePools(t, lab, "p-other"))
			},
		},
		{
			name: "its file no longer matches its mark",
			land: func(lab *hqLab) {
				lab.hq.landRecipeAsSquash(labMate, hq.RecipeProposalTitle, 1, edited(recipeBeforePools(t, lab, labMate)))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lab := newRecipeLab(t)
			tt.land(lab)
			main := lab.hq.recipeFiles("main")
			lab.moveLabPairOntoPool()

			outcome := lab.proposeRecipe(lab.mock)

			if outcome.Change != 0 || outcome.Committed || !outcome.OnMain {
				t.Errorf("a tier zcp may not write over was proposed over: %+v", outcome)
			}
			if got := lab.hq.recipeFiles("main"); got[recipeAgentTier] != main[recipeAgentTier] {
				t.Error("main's AI Agent tier changed")
			}
			// The dev service a pool took over is the tier's dev half: no
			// steer proposes taking it out of a tier the group keeps.
			if strings.Contains(outcome.Line, `service="appdev"`) {
				t.Errorf("the reconcile steers to remove the dev service the pool took over: %s", outcome.Line)
			}
		})
	}
}

// TestGroupRecipe_ItsOwnTiersWithTheSameShape_StayAsTheyAre: a tier zcp
// wrote from this Mate is not proposed anew for a scale, a variable or a
// comment — only for its shape: the recipe a pass proposed and Core landed,
// composed again from the same project, proposes nothing.
func TestGroupRecipe_ItsOwnTiersWithTheSameShape_StayAsTheyAre(t *testing.T) {
	t.Parallel()
	lab := newRecipeLab(t)
	first := lab.proposeRecipe(lab.mock)
	if first.Change == 0 {
		t.Fatalf("nothing proposed: %+v", first)
	}
	lab.hq.mergeChange(hq.RecipeRepo, first.Change)
	if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
		m.SetStageSetup("appstage", "prod")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	lab.mock.WithServiceEnv("svc-appstage", []platform.ServiceEnvVar{{Key: "LOG_LEVEL", Content: "debug", Type: platform.ServiceEnvUser}})

	outcome := lab.proposeRecipe(lab.mock)

	if outcome.Change != 0 || outcome.Committed || !outcome.OnMain {
		t.Errorf("tiers of the same shape were proposed anew: %+v", outcome)
	}
}

// TestGroupRecipe_ItsOwnTiersUnderAnOlderHeader_StayItsOwnAndAsTheyAre: the
// header zcp writes into every tier is no part of what zcp compares, so a zcp
// that words its header otherwise proposes nothing over the tiers an older
// zcp wrote from this Mate, and still counts them its own: the mark is the
// digest of the file as the older zcp wrote it (bundle.TierStamp), and a
// tier's shape is the services it creates (bundle.TierShape). Once the
// project's shape changes, its own tier is proposed anew as before.
func TestGroupRecipe_ItsOwnTiersUnderAnOlderHeader_StayItsOwnAndAsTheyAre(t *testing.T) {
	t.Parallel()
	lab := newRecipeLab(t)
	older := map[string]string{}
	for path, body := range recipeBeforePools(t, lab, labMate) {
		if _, _, stamped := bundle.TierStamp(body); stamped {
			worded := strings.Replace(body, "\n#\n", "\n#\n# An older zcp worded this header otherwise.\n", 1)
			if worded == body {
				t.Fatalf("%s: no header paragraph to reword:\n%s", path, body)
			}
			body = bundle.StampTier(worded, labMate)
		}
		older[path] = body
	}
	lab.hq.landRecipeAsSquash(labMate, hq.RecipeProposalTitle, 1, older)

	if same := lab.proposeRecipe(lab.mock); same.Change != 0 || same.Committed || !same.OnMain {
		t.Errorf("tiers an older zcp worded otherwise were proposed anew: %+v", same)
	}

	lab.moveLabPairOntoPool()
	moved := lab.proposeRecipe(lab.mock)
	if moved.Change == 0 || !moved.Created || len(moved.Proposed) != 1 || moved.Proposed[0] != "0 — AI Agent" {
		t.Errorf("after the pool migration = %+v, want the AI Agent tier, still zcp's own, proposed anew", moved)
	}
}

// recipeShapedClient is the lab's project once it gained a cache — a shape
// change for every tier zcp wrote before it — its stage running at a minimum
// of minContainers and holding mode as APP_MODE, with extra beside.
func recipeShapedClient(minContainers int32, mode string, extra ...platform.ServiceStack) *platform.Mock {
	stage := liveRuntime("svc-appstage", "appstage", "nodejs@22")
	stage.CurrentAutoscaling.HorizontalMinCount, stage.CurrentAutoscaling.HorizontalMaxCount = minContainers, 4
	services := append([]platform.ServiceStack{
		liveRuntime("svc-appdev", "appdev", "nodejs@22"),
		stage,
		{ID: "svc-db", Name: "db", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql:single@18", ServiceStackTypeCategoryName: "USER"}},
		{ID: "svc-cache", Name: "cache", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "valkey:single@7", ServiceStackTypeCategoryName: "USER"}},
	}, extra...)
	return platform.NewMock().
		WithProject(&platform.Project{ID: labMate, Name: "acme-mate-1", Status: "ACTIVE"}).
		WithServicesDirect(services).
		WithServices(services).
		WithServiceEnv("svc-appstage", []platform.ServiceEnvVar{{Key: "APP_MODE", Content: mode, Type: platform.ServiceEnvUser}})
}

// composedTier is path as this Mate composes it now from client, marked as
// a tier of zcp's own.
func (l *hqLab) composedTier(client platform.Client, path string) string {
	l.t.Helper()
	comp, outcome, ok := composeGroupRecipe(l.t.Context(), client, l.hq.srv.Client(), l.rt, l.stateDir, l.schemas)
	if !ok {
		l.t.Fatalf("the recipe does not compose: %+v", outcome)
	}
	defer comp.close()
	for _, file := range comp.files {
		if file.Path == path {
			return bundle.StampTier(file.Body, labMate)
		}
	}
	l.t.Fatalf("no %s composed", path)
	return ""
}

// TestGroupRecipe_ATierProposedAnew_IsRewrittenOnlyForItsShape: a tier zcp
// proposes anew over main's is one an environment may be made from, so its
// change can wait for a person (recipe_changes_environments), and every push
// has Core judge it again and moves it in the person's list. Its shape is
// why it is proposed; while it waits, a pass pushes it again only for a new
// shape — a service added — never for a value that moved since: a scale the
// agent tried, a variable set again. What the change carries of those is
// what the pass that found the shape composed.
func TestGroupRecipe_ATierProposedAnew_IsRewrittenOnlyForItsShape(t *testing.T) {
	t.Parallel()
	search := liveRuntime("svc-search", "search", "nodejs@22")
	search.ActiveAppVersion = &platform.ActiveAppVersionDigest{ID: "av-search", Source: "GIT", Built: true,
		PublicGitSource: &platform.AppVersionGitSource{GitURL: "https://github.com/acme/search", BranchName: "main"}}
	tests := []struct {
		name     string
		next     *platform.Mock
		wantPush bool
		want     string // what the change's Stage tier holds after the pass
	}{
		{name: "a scale moved", next: recipeShapedClient(2, "blue")},
		{name: "a variable's value moved", next: recipeShapedClient(1, "green")},
		{name: "a scale and a value moved", next: recipeShapedClient(2, "green")},
		{name: "a service added", next: recipeShapedClient(2, "green", search), wantPush: true, want: "hostname: search"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lab := newRecipeLab(t)
			lab.hq.landRecipeAsSquash(labMate, hq.RecipeProposalTitle, 1, recipeBeforePools(t, lab, labMate))
			first := lab.proposeRecipe(recipeShapedClient(1, "blue"))
			if !first.Created || !slices.Contains(first.Replaced, "3 — Stage") {
				t.Fatalf("the cache did not propose the Stage tier anew: %+v", first)
			}
			lab.hq.judge(first.Change, hq.MergeClean, "recipe_changes_environments")
			held := lab.hq.recipeHead(recipeBranch(first.Change))
			if lab.composedTier(tt.next, recipeStageTier) == lab.hq.recipeFiles(recipeBranch(first.Change))[recipeStageTier] {
				t.Fatal("the next project composes the Stage tier as the change holds it: nothing moved")
			}

			outcome := lab.proposeRecipe(tt.next)

			if pushed := lab.hq.recipeHead(recipeBranch(first.Change)) != held; pushed != tt.wantPush || outcome.Committed != tt.wantPush {
				t.Errorf("pushed %v (%+v), want %v", pushed, outcome, tt.wantPush)
			}
			if outcome.Created || lab.hq.changeIn(hq.RecipeRepo, first.Change+1) != nil {
				t.Errorf("the pass opened a second recipe change: %+v", outcome)
			}
			if stage := lab.hq.recipeFiles(recipeBranch(first.Change))[recipeStageTier]; tt.want != "" && !strings.Contains(stage, tt.want) {
				t.Errorf("the change's Stage tier lacks %q:\n%s", tt.want, stage)
			}
		})
	}
}
