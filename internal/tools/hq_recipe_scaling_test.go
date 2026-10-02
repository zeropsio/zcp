// Tests for: tools/hq_recipe_scaling.go — one host's scale proposed into the
// tiers on main of the application's recipe repository, as the Mate's change
// there, against a fake HQ that serves real git (hq_lab_test.go).
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
)

// scalingRecipeClient is the recipe lab's project with a Meilisearch running
// at searchRAM GB and a free-memory buffer of searchFree GB.
func scalingRecipeClient(searchRAM, searchFree float64) *platform.Mock {
	return recipeReconcileClient(platform.ServiceStack{ID: "svc-search", Name: "search", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "meilisearch:single@1.44", ServiceStackTypeCategoryName: "USER"},
		CustomAutoscaling:    &platform.CustomAutoscaling{CPUMode: "SHARED", MinCPU: 1, MaxCPU: 8, MinRAM: searchRAM, MaxRAM: 48, MinFreeRAMGB: searchFree}})
}

// unreadService is a project whose one service's detail cannot be read.
type unreadService struct {
	*platform.Mock
	id string
}

func (u unreadService) GetService(ctx context.Context, id string) (*platform.ServiceStack, error) {
	if id == u.id {
		return nil, errors.New("service detail read failed")
	}
	return u.Mock.GetService(ctx, id)
}

// scalingTitle is the title of the Mate's scaling proposal for search.
const scalingTitle = "Mate: search's scale in the group recipe"

// newScalingLab is the recipe lab whose group's first recipe, written from
// a search at 1 GB, landed on main: the floor wrote it at 2 GB with a 0.5 GB
// buffer.
func newScalingLab(t *testing.T) *hqLab {
	t.Helper()
	lab := newRecipeLab(t)
	lab.hq.seed(hq.RecipeRepo)
	if outcome := lab.proposeRecipe(scalingRecipeClient(1, 0.25)); outcome.Change != 1 {
		t.Fatalf("the first recipe was not proposed: %+v", outcome)
	}
	lab.hq.mergeChange(hq.RecipeRepo, 1)
	return lab
}

// proposeScaling is the agent's group-recipe scaling=host over the lab.
func (l *hqLab) proposeScaling(client platform.Client, host string) (*hqScalingAnswer, string) {
	l.t.Helper()
	result, _, _ := handleGroupRecipeScaling(l.t.Context(), client, l.hq.srv.Client(), l.rt, l.stateDir, host)
	text := getTextContent(l.t, result)
	if result.IsError {
		return nil, text
	}
	var answer hqScalingAnswer
	if err := json.Unmarshal([]byte(text), &answer); err != nil {
		l.t.Fatalf("parse: %v\n%s", err, text)
	}
	return &answer, text
}

type hqScalingAnswer struct {
	Change    int      `json:"change"`
	ChangeURL string   `json:"changeUrl"`
	Branch    string   `json:"branch"`
	Tiers     []string `json:"tiers"`
	Message   string   `json:"message"`
}

// TestGroupRecipeScaling_SteersAndProposesOneHostsBlock is a Mate changing a
// service's scale after the group's recipe is on main: the steer speaks only
// when the recipe would change, naming each key's old and new value and the
// call that proposes it; the proposal is the Mate's change in the recipe
// repository, changing that host's verticalAutoscaling in each tier file and
// nothing else; once the recipe says what the Mate runs, the change is
// brought to main's tree and adds nothing.
func TestGroupRecipeScaling_SteersAndProposesOneHostsBlock(t *testing.T) {
	lab := newScalingLab(t)
	main := lab.hq.recipeFiles("main")

	tests := []struct {
		name   string
		client platform.Client
		host   string
		want   []string
	}{
		{"the live scale floors to what the recipe says", scalingRecipeClient(1, 0.25), "search", nil},
		{"a host whose block the recipe already has", scalingRecipeClient(1, 0.25), "db", nil},
		{"a host the recipe does not name", scalingRecipeClient(1, 0.25), "cache", nil},
		{"a scale that could not be read", unreadService{scalingRecipeClient(4, 1), "svc-search"}, "search", nil},
		{"raised past the recipe", scalingRecipeClient(4, 1), "search", []string{"minRam 2 → 4", "minFreeRamGB 0.5 → 1", `action="group-recipe" scaling="search"`, "as a change the person reviews"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := groupRecipeScalingSteer(t.Context(), tt.client, lab.hq.srv.Client(), lab.rt, lab.stateDir, tt.host)
			if len(tt.want) == 0 && got != "" {
				t.Errorf("steer = %q, want nothing: the recipe would not change", got)
			}
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("steer = %q, want it to carry %q", got, w)
				}
			}
		})
	}

	answer, text := lab.proposeScaling(scalingRecipeClient(4, 1), "search")
	if answer == nil || answer.Change != 2 || answer.Branch != recipeBranch(2) {
		t.Fatalf("scaling proposal = %s", text)
	}
	if change := lab.hq.changeIn(hq.RecipeRepo, 2); change == nil || change.Title != scalingTitle || change.State != hq.ChangeOpen {
		t.Fatalf("change #2 = %+v, want it open and titled %q", change, scalingTitle)
	}
	if !lab.hq.recipeHolds(lab.hq.recipeHead("main"), recipeBranch(2)) {
		t.Error("the proposal is not cut from main")
	}
	proposed := lab.hq.recipeFiles(recipeBranch(2))
	var changed []string
	for path, body := range proposed {
		if main[path] == body {
			continue
		}
		changed = append(changed, path)
		// Every line outside search's entry is main's.
		outside := func(body string) []string {
			lines := strings.Split(body, "\n")
			start, end := hostEntryLines(lines, "search")
			return append(slices.Clone(lines[:start]), lines[end:]...)
		}
		if !slices.Equal(outside(body), outside(main[path])) {
			t.Errorf("%s: the proposal changes lines outside search's entry", path)
		}
		if !strings.Contains(body, "      minRam: 4") || !strings.Contains(body, "      minFreeRamGB: 1") {
			t.Errorf("%s: search's block does not carry the new scale:\n%s", path, body)
		}
	}
	slices.Sort(changed)
	if want := []string{"0 — AI Agent/import.yaml", "3 — Stage/import.yaml", "4 — Small Production/import.yaml"}; !slices.Equal(changed, want) {
		t.Errorf("changed files = %v, want only the tier files %v", changed, want)
	}

	// Nothing to change for another host: nothing opened, nothing pushed.
	head := lab.hq.recipeHead(recipeBranch(2))
	if answer, text := lab.proposeScaling(scalingRecipeClient(1, 0.25), "db"); answer == nil || answer.Change != 0 {
		t.Errorf("a host the recipe already has as it runs proposed something: %s", text)
	}
	if lab.hq.changeIn(hq.RecipeRepo, 3) != nil || lab.hq.recipeHead(recipeBranch(2)) != head {
		t.Error("a host with nothing to propose touched the recipe repository")
	}

	// Scaled back to what the recipe says: the open proposal went stale and
	// is brought to main's tree, so it adds nothing.
	if answer, text := lab.proposeScaling(scalingRecipeClient(1, 0.25), "search"); answer == nil {
		t.Fatalf("nothing to propose was refused: %s", text)
	}
	if got := lab.hq.recipeFiles(recipeBranch(2)); !maps.Equal(got, main) {
		t.Errorf("the stale proposal still changes main: %v", added(main, got))
	}
}

// TestGroupRecipeScaling_ProposesFromMainsHeadEveryTierFresh is a second
// proposal for the same host: the change carries every tier as this
// proposal writes it — a tier the first proposal changed and this one does
// not is main's again — and a tier whose block the splice cannot rewrite is
// left as main has it, the answer saying why.
func TestGroupRecipeScaling_ProposesFromMainsHeadEveryTierFresh(t *testing.T) {
	lab := newScalingLab(t)
	// The group sized search by hand on Stage, and wrote a note on its Small
	// Production block.
	const stage, production = "3 — Stage/import.yaml", "4 — Small Production/import.yaml"
	files := lab.hq.recipeFiles("main")
	editSearch := func(path string, edit func(string) string) {
		lines := strings.Split(files[path], "\n")
		start, end := hostEntryLines(lines, "search")
		for i := start; i < end; i++ {
			lines[i] = edit(lines[i])
		}
		files[path] = strings.Join(lines, "\n")
	}
	editSearch(stage, func(line string) string {
		line = strings.Replace(line, "minRam: 2", "minRam: 8", 1)
		return strings.Replace(line, "minFreeRamGB: 0.5", "minFreeRamGB: 1", 1)
	})
	editSearch(production, func(line string) string {
		if strings.TrimSpace(line) == "verticalAutoscaling:" {
			return line + " # sized for launch"
		}
		return line
	})
	lab.hq.landOnMain(hq.RecipeRepo, map[string]string{stage: files[stage], production: files[production]})
	main := lab.hq.recipeFiles("main")

	if answer, text := lab.proposeScaling(scalingRecipeClient(4, 1), "search"); answer == nil || answer.Change != 2 {
		t.Fatalf("the first proposal = %s", text)
	}
	if first := lab.hq.recipeFiles(recipeBranch(2)); !strings.Contains(first[stage], "minRam: 4") {
		t.Fatalf("the first proposal does not lower Stage's search to 4:\n%s", first[stage])
	}
	answer, text := lab.proposeScaling(scalingRecipeClient(8, 1), "search")
	if answer == nil || answer.Change != 2 {
		t.Fatalf("the second proposal = %s, want it on the same change", text)
	}
	second := lab.hq.recipeFiles(recipeBranch(2))
	if second[stage] != main[stage] {
		t.Errorf("Stage still carries the first proposal's scale:\n%s", second[stage])
	}
	if !strings.Contains(second["0 — AI Agent/import.yaml"], "minRam: 8") {
		t.Error("the AI Agent tier does not carry the second proposal's scale")
	}
	if second[production] != main[production] {
		t.Errorf("Small Production's hand-written block was rewritten:\n%s", second[production])
	}
	if !strings.Contains(answer.Message, "Small Production") || !strings.Contains(answer.Message, "comment") {
		t.Errorf("answer = %s, want it to say why Small Production was left", answer.Message)
	}
}

// TestGroupRecipeScaling_OneOpenChangeInTheRecipeRepository: HQ keeps one
// open change per Mate per repository, so a scaling proposal and the
// additive one never write over each other — the one that finds the other
// open leaves it as it is and says which.
func TestGroupRecipeScaling_OneOpenChangeInTheRecipeRepository(t *testing.T) {
	t.Run("the additive reconcile leaves a scaling proposal alone", func(t *testing.T) {
		lab := newScalingLab(t)
		if answer, text := lab.proposeScaling(scalingRecipeClient(4, 1), "search"); answer == nil || answer.Change != 2 {
			t.Fatalf("scaling proposal = %s", text)
		}
		head := lab.hq.recipeHead(recipeBranch(2))

		outcome := lab.proposeRecipe(scalingRecipeClient(4, 1))

		if lab.hq.recipeHead(recipeBranch(2)) != head || outcome.Committed {
			t.Errorf("the additive reconcile wrote over the scaling proposal: %+v", outcome)
		}
		if change := lab.hq.changeIn(hq.RecipeRepo, 2); change.Title != scalingTitle {
			t.Errorf("change #2 retitled %q", change.Title)
		}
	})
	t.Run("a scaling proposal waits for the open additive one", func(t *testing.T) {
		// main carries only the Stage tier, which names search, so the
		// additive proposal of the other tiers is open.
		const stage = "3 — Stage/import.yaml"
		lab := newRecipeLab(t)
		lab.hq.seed(hq.RecipeRepo)
		if outcome := lab.proposeRecipe(scalingRecipeClient(1, 0.25)); outcome.Change != 1 {
			t.Fatalf("the first recipe was not proposed: %+v", outcome)
		}
		stageBody := lab.hq.recipeFiles(recipeBranch(1))[stage]
		lab.hq.closeChange(hq.RecipeRepo, 1)
		lab.hq.landOnMain(hq.RecipeRepo, map[string]string{stage: stageBody})
		if outcome := lab.proposeRecipe(scalingRecipeClient(1, 0.25)); outcome.Change != 2 || !outcome.Created {
			t.Fatalf("the missing tiers were not proposed: %+v", outcome)
		}
		head := lab.hq.recipeHead(recipeBranch(2))

		answer, text := lab.proposeScaling(scalingRecipeClient(4, 1), "search")

		if answer != nil || !strings.Contains(text, "#2") || !strings.Contains(text, hq.RecipeProposalTitle) {
			t.Errorf("answer = %s, want a refusal naming the open change #2", text)
		}
		if lab.hq.recipeHead(recipeBranch(2)) != head {
			t.Error("the scaling proposal wrote over the additive one")
		}
		if change := lab.hq.changeIn(hq.RecipeRepo, 2); change.Title != hq.RecipeProposalTitle {
			t.Errorf("change #2 retitled %q", change.Title)
		}
	})
}

// hostEntryLines finds host's services[] item in a tier file, by the same
// layout the splice reads: [start, end) of its lines.
func hostEntryLines(lines []string, host string) (int, int) {
	for i, line := range lines {
		if strings.TrimSpace(line) != "- hostname: "+host {
			continue
		}
		dash := len(line) - len(strings.TrimLeft(line, " "))
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) != "" && len(lines[j])-len(strings.TrimLeft(lines[j], " ")) <= dash {
				return i, j
			}
		}
		return i, len(lines)
	}
	return 0, 0
}
