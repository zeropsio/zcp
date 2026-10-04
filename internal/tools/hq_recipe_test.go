// Tests for: tools/hq_recipe.go — the recipe in the application's recipe
// repository: the tiers its main lacks, proposed as the Mate's own change
// there (SPEC §3.2c), run against a fake HQ that serves real git
// (hq_lab_test.go).
package tools

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// recipeReconcileClient is a Mate's live project the recipe composes from:
// the lab's pair and the database it uses.
func recipeReconcileClient(extra ...platform.ServiceStack) *platform.Mock {
	services := append([]platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
		{ID: "svc-appstage", Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
		{ID: "svc-db", Name: "db", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql:single@18", ServiceStackTypeCategoryName: "USER"}},
	}, extra...)
	return platform.NewMock().
		WithProject(&platform.Project{ID: labMate, Name: "acme-mate-1", Status: "ACTIVE"}).
		WithServicesDirect(services).
		WithServices(services)
}

// newRecipeLab is the lab with a project the recipe composes from, its pair
// wired, both halves deployed: the group tiers build the stage half's setup,
// and a stage setup nothing records is withheld rather than guessed.
func newRecipeLab(t *testing.T) *hqLab {
	t.Helper()
	lab := newHQLab(t)
	lab.mock = recipeReconcileClient()
	lab.ssh.mock = lab.mock
	lab.wire()
	if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
		m.PrimarySetupName, m.StageSetupName = "api", "prod"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return lab
}

// proposeRecipe is one pass of the recipe reconcile over the lab.
func (l *hqLab) proposeRecipe(client platform.Client) recipeOutcome {
	l.t.Helper()
	return groupRecipeOutcome(l.t.Context(), client, l.hq.srv.Client(), l.rt, l.stateDir)
}

// recipeBranch is the branch of the Mate's change number in the recipe
// repository.
func recipeBranch(number int) string { return fmt.Sprintf("mate/%s/%d", labMate, number) }

// recipeFiles is what ref holds in the recipe repository of the application
// the Mate was first in, path to body.
func (f *fakeHQ) recipeFiles(ref string) map[string]string {
	f.t.Helper()
	dir := f.repoDir(labApp, hq.RecipeRepo)
	files := map[string]string{}
	for path := range strings.SplitSeq(f.git(f.t.Context(), dir, "ls-tree", "-r", "-z", "--name-only", ref), "\x00") {
		if path != "" {
			files[path] = f.git(f.t.Context(), dir, "show", ref+":"+path) + "\n"
		}
	}
	return files
}

// recipeHead is the commit ref is at in the recipe repository.
func (f *fakeHQ) recipeHead(ref string) string {
	f.t.Helper()
	return f.git(f.t.Context(), f.repoDir(labApp, hq.RecipeRepo), "rev-parse", ref)
}

// recipeHolds reports whether ref holds ancestor in the recipe repository.
func (f *fakeHQ) recipeHolds(ancestor, ref string) bool {
	f.t.Helper()
	return f.git(f.t.Context(), f.repoDir(labApp, hq.RecipeRepo), "merge-base", ancestor, ref) == ancestor
}

// handWrittenTiers is a group whose people wrote every tier themselves —
// project env, secrets, the production setups — the state the medusa group
// was in when its second Mate's proposal replaced it (2026-09-26).
func handWrittenTiers() map[string]string {
	return map[string]string{
		"README.md":                                   "# acme\n",
		"0 — AI Agent/import.yaml":                    "project:\n  name: acme-mate\nservices:\n  - hostname: appdev\n    zeropsSetup: appdev\n",
		"3 — Stage/import.yaml":                       "project:\n  name: acme stage\n  envVariables:\n    APP_ENV: stage\nservices:\n  - hostname: app\n    zeropsSetup: appprod\n",
		"4 — Small Production/import.yaml":            "project:\n  name: acme production\n  envVariables:\n    APP_ENV: production\nservices:\n  - hostname: app\n    zeropsSetup: appprod\n    envSecrets:\n      APP_KEY: <@generateRandomString(<32>)>\n",
		"5 — Highly-available Production/import.yaml": "project:\n  name: acme ha\nservices:\n  - hostname: app\n    zeropsSetup: appprod\n",
	}
}

func without(files map[string]string, dir string) map[string]string {
	out := maps.Clone(files)
	for path := range out {
		if strings.HasPrefix(path, dir+"/") {
			delete(out, path)
		}
	}
	return out
}

// assertOnlyAdds fails unless branch is main plus files main does not have:
// the only kind of change Core lands by itself, and the only kind that
// cannot rewrite a tier the group already has.
func assertOnlyAdds(t *testing.T, main, branch map[string]string) {
	t.Helper()
	for path, body := range main {
		got, ok := branch[path]
		switch {
		case !ok:
			t.Errorf("the proposal removes %q, which main carries", path)
		case got != body:
			t.Errorf("the proposal modifies %q, which main carries", path)
		}
	}
}

// added is what branch holds that main does not, sorted.
func added(main, branch map[string]string) []string {
	var paths []string
	for path := range branch {
		if _, onMain := main[path]; !onMain {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	return paths
}

// TestGroupRecipe_ProposedAsTheMatesChange: a group's first recipe is
// proposed whole as the Mate's change in the recipe repository, titled
// exactly and with no description, its branch main plus the files main
// lacks; every tier builds each pair from its repository in HQ, and the
// credential is nowhere on disk.
func TestGroupRecipe_ProposedAsTheMatesChange(t *testing.T) {
	lab := newRecipeLab(t)
	lab.hq.seed(hq.RecipeRepo)
	mainBefore := lab.hq.recipeHead("main")

	outcome := lab.proposeRecipe(lab.mock)

	if outcome.Blocked != "" || !outcome.Created || !outcome.Committed || outcome.Change != 1 {
		t.Fatalf("outcome = %+v", outcome)
	}
	if want := lab.hq.srv.URL + "/changes/" + labApp + "/group/1"; outcome.ChangeURL != want || !strings.Contains(outcome.Line, want) {
		t.Errorf("change address = %q in %q, want %q", outcome.ChangeURL, outcome.Line, want)
	}
	if change := lab.hq.changeIn(hq.RecipeRepo, 1); change == nil || change.Title != hq.RecipeProposalTitle || change.Body != "" {
		t.Fatalf("change #1 in the recipe repository = %+v, want it titled %q with no description", change, hq.RecipeProposalTitle)
	}
	if lab.hq.recipeHead("main") != mainBefore {
		t.Error("zcp moved the recipe repository's main")
	}
	main, branch := lab.hq.recipeFiles("main"), lab.hq.recipeFiles(recipeBranch(1))
	assertOnlyAdds(t, main, branch)
	// Every tier at the path HQ reads it from, its README beside it, and the
	// root README: HQ's recipe repository is born with an empty tree.
	want := make([]string, 0, 1+2*len(hq.RecipeTierPaths))
	want = append(want, "README.md")
	for _, path := range hq.RecipeTierPaths {
		want = append(want, path, strings.TrimSuffix(path, "import.yaml")+"README.md")
	}
	slices.Sort(want)
	if got := added(main, branch); !slices.Equal(got, want) {
		t.Errorf("the proposal adds %v, want %v", got, want)
	}
	for _, path := range hq.RecipeTierPaths {
		if repo := lab.hq.srv.URL + "/git/" + labApp + "/appdev"; !strings.Contains(branch[path], "buildFromGit: "+repo) {
			t.Errorf("%s builds the pair from somewhere else than %s:\n%s", path, repo, branch[path])
		}
	}
	assertNoSecretOnDisk(t, lab.stateDir, labCredential)
}

// TestGroupRecipe_ProposesOnlyWhatMainLacks: a tier on main is the group's,
// hand-written or landed earlier, and is never proposed over; a group whose
// main has every tier gets nothing — no change opened.
func TestGroupRecipe_ProposesOnlyWhatMainLacks(t *testing.T) {
	tests := []struct {
		name         string
		main         map[string]string
		wantProposed []string // nil: nothing proposed, no change opened
	}{
		{name: "every tier hand-written on main", main: handWrittenTiers()},
		{
			name:         "main lacks only Stage",
			main:         without(handWrittenTiers(), "3 — Stage"),
			wantProposed: []string{"3 — Stage/README.md", "3 — Stage/import.yaml"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := newRecipeLab(t)
			lab.hq.landOnMain(hq.RecipeRepo, tt.main)

			outcome := lab.proposeRecipe(lab.mock)

			if tt.wantProposed == nil {
				if !outcome.OnMain || outcome.Line != "" || lab.hq.changeIn(hq.RecipeRepo, 1) != nil {
					t.Errorf("outcome = %+v, change %+v; want nothing proposed and nothing said", outcome, lab.hq.changeIn(hq.RecipeRepo, 1))
				}
				return
			}
			if !outcome.Created || !strings.Contains(outcome.Line, "3 — Stage") {
				t.Fatalf("outcome = %+v", outcome)
			}
			main, branch := lab.hq.recipeFiles("main"), lab.hq.recipeFiles(recipeBranch(1))
			assertOnlyAdds(t, main, branch)
			if got := added(main, branch); !slices.Equal(got, tt.wantProposed) {
				t.Errorf("the proposal adds %v, want %v", got, tt.wantProposed)
			}
		})
	}
}

// TestGroupRecipe_FollowsTheProjectOnTheSameChange: an open proposal follows
// the project until it lands — an identical recipe pushes nothing, and a
// changed one moves the same change's branch forward, still only adding.
func TestGroupRecipe_FollowsTheProjectOnTheSameChange(t *testing.T) {
	lab := newRecipeLab(t)
	if first := lab.proposeRecipe(lab.mock); !first.Created {
		t.Fatalf("first pass: %+v", first)
	}
	firstHead := lab.hq.recipeHead(recipeBranch(1))

	if again := lab.proposeRecipe(lab.mock); again.Committed || again.Line != "" || again.Change != 1 {
		t.Errorf("an identical recipe: %+v, want nothing pushed, nothing said, change #1 still its change", again)
	}
	if lab.hq.recipeHead(recipeBranch(1)) != firstHead {
		t.Error("an identical recipe moved the change's branch")
	}

	cache := platform.ServiceStack{ID: "svc-cache", Name: "cache", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "valkey:single@7", ServiceStackTypeCategoryName: "USER"}}
	changed := lab.proposeRecipe(recipeReconcileClient(cache))
	if !changed.Committed || changed.Created || !strings.Contains(changed.Line, "change #1") {
		t.Fatalf("a changed recipe: %+v, want change #1 updated", changed)
	}
	if lab.hq.changeIn(hq.RecipeRepo, 2) != nil {
		t.Error("a changed recipe opened a second change")
	}
	if !lab.hq.recipeHolds(firstHead, recipeBranch(1)) {
		t.Error("the change's branch did not move forward from its last push")
	}
	main, branch := lab.hq.recipeFiles("main"), lab.hq.recipeFiles(recipeBranch(1))
	assertOnlyAdds(t, main, branch)
	if !strings.Contains(branch["0 — AI Agent/import.yaml"], "hostname: cache") {
		t.Error("the update does not carry the new service")
	}
}

// TestGroupRecipe_MainMovedUnderAnOpenProposal: a person lands a tier on
// main while the proposal is open. The same change takes main in — its
// branch moves forward with main as a parent — and stops proposing the tier
// main now has, so it still only adds.
func TestGroupRecipe_MainMovedUnderAnOpenProposal(t *testing.T) {
	lab := newRecipeLab(t)
	if first := lab.proposeRecipe(lab.mock); !first.Created {
		t.Fatalf("first pass: %+v", first)
	}
	firstHead := lab.hq.recipeHead(recipeBranch(1))
	production := handWrittenTiers()["4 — Small Production/import.yaml"]
	lab.hq.landOnMain(hq.RecipeRepo, map[string]string{"4 — Small Production/import.yaml": production})

	second := lab.proposeRecipe(lab.mock)

	if !second.Committed || second.Created || second.Change != 1 || lab.hq.changeIn(hq.RecipeRepo, 2) != nil {
		t.Fatalf("second pass: %+v, want change #1 moved forward", second)
	}
	if !lab.hq.recipeHolds(firstHead, recipeBranch(1)) || !lab.hq.recipeHolds(lab.hq.recipeHead("main"), recipeBranch(1)) {
		t.Error("the change's branch must hold both its last push and the new main")
	}
	main, branch := lab.hq.recipeFiles("main"), lab.hq.recipeFiles(recipeBranch(1))
	assertOnlyAdds(t, main, branch)
	if _, ok := branch["4 — Small Production/README.md"]; ok {
		t.Error("the proposal adds a README to the production tier the person wrote")
	}
}

// TestGroupRecipe_AProposalMainOvertookAddsNothing: main comes to carry every
// tier while this Mate's proposal is open — another Mate's landed first. The
// change is brought to main's tree, so it adds nothing and Core closes it.
func TestGroupRecipe_AProposalMainOvertookAddsNothing(t *testing.T) {
	lab := newRecipeLab(t)
	if first := lab.proposeRecipe(lab.mock); !first.Created {
		t.Fatalf("first pass: %+v", first)
	}
	firstHead := lab.hq.recipeHead(recipeBranch(1))
	lab.hq.landOnMain(hq.RecipeRepo, handWrittenTiers())

	second := lab.proposeRecipe(lab.mock)

	if !second.OnMain || !second.Committed || !strings.Contains(second.Line, "now adds nothing") {
		t.Fatalf("second pass: %+v", second)
	}
	if !lab.hq.recipeHolds(firstHead, recipeBranch(1)) {
		t.Error("the change's branch did not move forward")
	}
	if main, branch := lab.hq.recipeFiles("main"), lab.hq.recipeFiles(recipeBranch(1)); !maps.Equal(main, branch) {
		t.Errorf("the change still differs from main: adds %v", added(main, branch))
	}
	if third := lab.proposeRecipe(lab.mock); third.Committed || third.Line != "" {
		t.Errorf("the pass after: %+v, want nothing pushed and nothing said", third)
	}
}

// TestGroupRecipe_OpensNothingMainAlreadyHas is the owner's run of
// 2026-09-17 in HQ's terms: once Core lands the proposal main carries every
// tier, and the next pass opens nothing; a proposal a person closed unmerged
// leaves main without them, so the next pass proposes them again.
func TestGroupRecipe_OpensNothingMainAlreadyHas(t *testing.T) {
	tests := []struct {
		name       string
		settle     func(*fakeHQ)
		wantChange bool
	}{
		{name: "Core landed it", settle: func(f *fakeHQ) { f.mergeChange(hq.RecipeRepo, 1) }},
		{name: "a person closed it unmerged", settle: func(f *fakeHQ) { f.closeChange(hq.RecipeRepo, 1) }, wantChange: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := newRecipeLab(t)
			if first := lab.proposeRecipe(lab.mock); !first.Created {
				t.Fatalf("first pass: %+v", first)
			}
			tt.settle(lab.hq)

			next := lab.proposeRecipe(lab.mock)

			if opened := lab.hq.changeIn(hq.RecipeRepo, 2) != nil; opened != tt.wantChange || next.Created != tt.wantChange {
				t.Errorf("the next pass opened a change: %v, want %v (%+v)", opened, tt.wantChange, next)
			}
			if !tt.wantChange && next.Line != "" {
				t.Errorf("a landed recipe is nothing to report on a pass, got %q", next.Line)
			}
		})
	}
}

// TestHandleGroupRecipe_Table: the agent-facing surface answers on every
// pass, including the one that changed nothing — being asked is itself a
// reason to say where the recipe is, or that main already has it.
func TestHandleGroupRecipe_Table(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*hqLab)
		twice    bool
		wantErr  bool
		wantText []string
		notText  []string
	}{
		{
			name:     "proposed",
			wantText: []string{`"groupRepo":"group"`, `"change":1`, "/changes/" + labApp + "/group/1", recipeBranch(1)},
		},
		{
			name:     "asked again, nothing changed — still answers with the change",
			twice:    true,
			wantText: []string{"already proposed", "change #1", "/changes/" + labApp + "/group/1"},
		},
		// E2E F15: told that zcp "never proposes over" a tier on main, the agent
		// reported a scale it was asked to carry into the recipe as blocked. A
		// tier on main takes a service's scale through the scaling proposal.
		{
			name:  "main already carries every tier",
			setup: func(l *hqLab) { l.hq.landOnMain(hq.RecipeRepo, handWrittenTiers()) },
			wantText: []string{
				`"onMain":true`, "already carries every tier",
				`call zerops_workflow action=\"group-recipe\" with scaling set to the service's hostname`,
				"as a change the person reviews",
				"anything else in a tier on main",
			},
			notText: []string{"never proposes over"},
		},
		{
			name: "not enrolled with HQ",
			setup: func(l *hqLab) {
				if err := os.Remove(hq.EnrollmentPath()); err != nil {
					l.t.Fatal(err)
				}
			},
			wantErr:  true,
			wantText: []string{"was not proposed", "not enrolled"},
		},
		{
			name: "no pair has its repository yet",
			setup: func(l *hqLab) {
				if err := workflow.UpdateServiceMeta(l.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
					m.HQ = nil
					return nil
				}); err != nil {
					l.t.Fatal(err)
				}
			},
			wantErr:  true,
			wantText: []string{"was not proposed", "no pair has its repository in HQ yet"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := newRecipeLab(t)
			if tt.setup != nil {
				tt.setup(lab)
			}
			handle := func() *mcp.CallToolResult {
				res, typed, err := handleGroupRecipe(t.Context(), lab.mock, lab.hq.srv.Client(), lab.rt, lab.stateDir)
				if err != nil || typed != nil {
					t.Fatalf("handleGroupRecipe: %v, typed %v", err, typed)
				}
				return res
			}
			if tt.twice {
				handle()
			}
			res := handle()
			if res.IsError != tt.wantErr {
				t.Errorf("IsError = %v, want %v", res.IsError, tt.wantErr)
			}
			text := strings.ReplaceAll(resultText(t, res), `": `, `":`)
			for _, want := range tt.wantText {
				if !strings.Contains(text, want) {
					t.Errorf("result is missing %q:\n%s", want, text)
				}
			}
			for _, absent := range tt.notText {
				if strings.Contains(text, absent) {
					t.Errorf("result says %q:\n%s", absent, text)
				}
			}
			if lab.hq.changeIn(hq.RecipeRepo, 2) != nil {
				t.Error("asking twice opened a second change")
			}
		})
	}
}

// resultText is a tool result's model-facing text — the only surface machine
// state rides on (the typed second return stays nil).
func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatal("the result carries no content")
	}
	var b strings.Builder
	for _, content := range res.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}

// TestRecipeSlug is main's group-slug rule (client-runtime groupRegistry.ts
// groupSlugBase) for the application's name: lowercase letters, digits and
// dashes, starting with a letter, 2 to 30 long, never ending on a dash.
func TestRecipeSlug(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, want string }{
		{"Acme", "acme"},
		{"Acme Corp", "acme-corp"},
		{"  Acme   Corp  ", "acme-corp"},
		{"Acme / Corp!", "acme-corp"},
		{"Ácme Čorp", "acme-corp"},
		{"2024 Launch", "group-2024-launch"},
		{"42", "group-42"},
		{"", "group"},
		{"!!!", "group"},
		{"A", "group"},
		{"ab", "ab"},
		{strings.Repeat("a", 40), strings.Repeat("a", 30)},
		{strings.Repeat("ab ", 12), "ab-ab-ab-ab-ab-ab-ab-ab-ab-ab"},
	}
	for _, tt := range tests {
		if got := recipeSlug(tt.name); got != tt.want {
			t.Errorf("recipeSlug(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestGroupRecipe_NamedAfterTheApplication: the stage and production tiers
// name their projects after the application HQ holds the Mate in, by its
// name's slug — by its id while HQ names it nothing.
func TestGroupRecipe_NamedAfterTheApplication(t *testing.T) {
	tests := []struct {
		name    string
		appName string
		want    string
	}{
		{"named", "Acme Corp", "name: acme-corp stage"},
		{"no name yet", "", "name: " + labApp + " stage"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := newRecipeLab(t)
			lab.hq.appName = tt.appName

			outcome := lab.proposeRecipe(lab.mock)

			stage := lab.hq.recipeFiles(recipeBranch(outcome.Change))[hq.RecipeTierPaths[hq.RecipeTierStage]]
			if !strings.Contains(stage, tt.want) {
				t.Errorf("the Stage tier does not say %q:\n%s", tt.want, stage)
			}
		})
	}
}
