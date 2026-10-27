// Tests for: tools/hq_recipe.go — a tier zcp wrote from this Mate, and
// nobody changed since, is proposed anew when the Mate's project changes
// shape; a tier a person or another Mate wrote or edited never is (D30,
// spec-mate §2.8a.5).
package tools

import (
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
// changed, as the Mate's recipe change a person reviews: the AI Agent tier
// gets the dev pool in the dev service's place, marked; a tier whose shape
// did not change (Stage, Small Production: the same runtime, type, setup and
// repository) is left as main has it, and so is every README. A person who
// closes the proposal unmerged is not asked again.
func TestGroupRecipe_ItsOwnTiersFromBeforeAPoolMigration_ProposedAnew(t *testing.T) {
	t.Parallel()
	lab := newRecipeLab(t)
	lab.hq.landAsSquash(hq.RecipeRepo, labMate, hq.RecipeProposalTitle, 1, recipeBeforePools(t, lab, ""))
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
	for _, want := range []string{"0 — AI Agent", "a person reviews"} {
		if !strings.Contains(outcome.Line, want) {
			t.Errorf("line %q does not say %q", outcome.Line, want)
		}
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
				lab.hq.landAsSquash(hq.RecipeRepo, labMate, hq.RecipeProposalTitle, 1, recipeBeforePools(t, lab, ""))
				lab.hq.landAsSquash(hq.RecipeRepo, labMate, "Give the dev more memory", 2, edited(recipeBeforePools(t, lab, "")))
			},
		},
		{
			name: "another Mate's zcp wrote it",
			land: func(lab *hqLab) {
				lab.hq.landAsSquash(hq.RecipeRepo, "p-other", hq.RecipeProposalTitle, 1, recipeBeforePools(t, lab, ""))
			},
		},
		{
			name: "its mark is another Mate's",
			land: func(lab *hqLab) {
				lab.hq.landAsSquash(hq.RecipeRepo, labMate, hq.RecipeProposalTitle, 1, recipeBeforePools(t, lab, "p-other"))
			},
		},
		{
			name: "its file no longer matches its mark",
			land: func(lab *hqLab) {
				lab.hq.landAsSquash(hq.RecipeRepo, labMate, hq.RecipeProposalTitle, 1, edited(recipeBeforePools(t, lab, labMate)))
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
