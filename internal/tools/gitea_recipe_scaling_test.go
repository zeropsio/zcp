package tools

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// scalingRecipeClient is recipeReconcileClient with a Meilisearch running at
// searchRAM GB and a free-memory buffer of searchFree GB.
func scalingRecipeClient(searchRAM, searchFree float64) *platform.Mock {
	runtimeType := func(v string) platform.ServiceTypeInfo {
		return platform.ServiceTypeInfo{ServiceStackTypeVersionName: v, ServiceStackTypeCategoryName: "USER"}
	}
	services := []platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: runtimeType("nodejs@22")},
		{ID: "svc-appstage", Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: runtimeType("nodejs@22")},
		{ID: "svc-db", Name: "db", Status: "ACTIVE", ServiceStackTypeInfo: runtimeType("postgresql:single@18")},
		{ID: "svc-search", Name: "search", Status: "ACTIVE", ServiceStackTypeInfo: runtimeType("meilisearch:single@1.44"),
			CustomAutoscaling: &platform.CustomAutoscaling{CPUMode: "SHARED", MinCPU: 1, MaxCPU: 8, MinRAM: searchRAM, MaxRAM: 48, MinFreeRAMGB: searchFree}},
	}
	return platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "acme-mate-1", Status: "ACTIVE"}).
		WithServicesDirect(services).
		WithServices(services)
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

// TestGroupRecipeScaling_SteersAndProposesOneHostsBlock is a Mate changing a
// service's scale after the group's recipe is on main: the steer line speaks
// only when the recipe would change, naming each key's old and new value and
// the call that proposes it; the proposal changes that host's
// verticalAutoscaling in each tier file and nothing else.
func TestGroupRecipeScaling_SteersAndProposesOneHostsBlock(t *testing.T) {
	stateDir := t.TempDir()
	writeGiteaWiredPairMeta(t, stateDir)
	fake := newFakeGroupGitea()
	srv := fake.start(t)
	envPath := writeLiveEnvFile(t, map[string]string{"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken})
	rt := runtime.Info{InContainer: true, ProjectID: "p1"}
	ctx := context.Background()

	// The group's first recipe lands, written from a search at 1 GB — the
	// floor writes it at 2 GB with a 0.5 GB buffer.
	if line := reconcileGiteaGroupRecipe(ctx, scalingRecipeClient(1, 0.25), srv.Client(), rt, stateDir, envPath); !strings.Contains(line, "#11") {
		t.Fatalf("the first recipe was not proposed: %q", line)
	}
	fake.merge(11)
	main := maps.Clone(fake.branches[fakeGroupRepo]["main"])

	steer := func(client platform.Client, host string) string {
		return groupRecipeScalingSteer(ctx, client, srv.Client(), rt, stateDir, envPath, host)
	}
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
		{"raised past the recipe", scalingRecipeClient(4, 1), "search", []string{"minRam 2 → 4", "minFreeRamGB 0.5 → 1", `action="group-recipe" scaling="search"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := steer(tt.client, tt.host)
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

	result, _, _ := handleGroupRecipeScaling(ctx, scalingRecipeClient(4, 1), srv.Client(), rt, stateDir, envPath, "search")
	if result.IsError {
		t.Fatalf("scaling proposal: %s", getTextContent(t, result))
	}
	var body struct {
		PullRequest int      `json:"pullRequest"`
		Tiers       []string `json:"tiers"`
	}
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &body); err != nil {
		t.Fatalf("parse: %v", err)
	}
	pr := fake.pulls[body.PullRequest]
	if pr == nil || !pr.open {
		t.Fatalf("no open pull request #%d: %s", body.PullRequest, getTextContent(t, result))
	}
	proposed := fake.branches[pr.headRepo][pr.branch]
	var changed []string
	for path, text := range proposed {
		if main[path] == text {
			continue
		}
		changed = append(changed, path)
		// Every line outside search's entry is main's.
		outside := func(body string) []string {
			lines := strings.Split(body, "\n")
			start, end := hostEntryLines(lines, "search")
			return append(slices.Clone(lines[:start]), lines[end:]...)
		}
		if !slices.Equal(outside(text), outside(main[path])) {
			t.Errorf("%s: the proposal changes lines outside search's entry", path)
		}
		if !strings.Contains(text, "      minRam: 4") || !strings.Contains(text, "      minFreeRamGB: 1") {
			t.Errorf("%s: search's block does not carry the new scale:\n%s", path, text)
		}
	}
	slices.Sort(changed)
	if want := []string{"0 — AI Agent/import.yaml", "3 — Stage/import.yaml", "4 — Small Production/import.yaml"}; !slices.Equal(changed, want) {
		t.Errorf("changed files = %v, want only the tier files %v", changed, want)
	}

	// Nothing to change: no pull request.
	before := fake.pullPosts
	result, _, _ = handleGroupRecipeScaling(ctx, scalingRecipeClient(1, 0.25), srv.Client(), rt, stateDir, envPath, "db")
	if result.IsError || fake.pullPosts != before {
		t.Errorf("a host the recipe already has as it runs opened a pull request: %s", getTextContent(t, result))
	}

	// Scaled back to what the recipe says: the open proposal went stale and
	// is closed.
	result, _, _ = handleGroupRecipeScaling(ctx, scalingRecipeClient(1, 0.25), srv.Client(), rt, stateDir, envPath, "search")
	if result.IsError || fake.pulls[body.PullRequest].open {
		t.Errorf("nothing to propose left proposal #%d open: %s", body.PullRequest, getTextContent(t, result))
	}
}

// TestGroupRecipeScaling_ProposesFromMainsHeadEveryTierFresh is a second
// proposal for the same host on the same main: the branch carries every
// tier as this proposal writes it — a tier the first proposal changed and
// this one does not is main's again — read at the head the branch is cut
// from; a tier whose block the splice cannot rewrite is left as main has it,
// and the answer says why.
func TestGroupRecipeScaling_ProposesFromMainsHeadEveryTierFresh(t *testing.T) {
	stateDir := t.TempDir()
	writeGiteaWiredPairMeta(t, stateDir)
	fake := newFakeGroupGitea()
	srv := fake.start(t)
	envPath := writeLiveEnvFile(t, map[string]string{"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken})
	rt := runtime.Info{InContainer: true, ProjectID: "p1"}
	ctx := context.Background()
	if line := reconcileGiteaGroupRecipe(ctx, scalingRecipeClient(1, 0.25), srv.Client(), rt, stateDir, envPath); !strings.Contains(line, "#11") {
		t.Fatalf("the first recipe was not proposed: %q", line)
	}
	fake.merge(11)

	// The group sized search by hand on Stage, and wrote a note on its
	// Small Production block.
	const stage, production = "3 — Stage/import.yaml", "4 — Small Production/import.yaml"
	editSearch := func(path string, edit func(string) string) {
		lines := strings.Split(fake.branches[fakeGroupRepo]["main"][path], "\n")
		start, end := hostEntryLines(lines, "search")
		for i := start; i < end; i++ {
			lines[i] = edit(lines[i])
		}
		fake.branches[fakeGroupRepo]["main"][path] = strings.Join(lines, "\n")
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
	main := maps.Clone(fake.branches[fakeGroupRepo]["main"])

	propose := func(ram float64) (map[string]string, string) {
		t.Helper()
		result, _, _ := handleGroupRecipeScaling(ctx, scalingRecipeClient(ram, 1), srv.Client(), rt, stateDir, envPath, "search")
		if result.IsError {
			t.Fatalf("scaling proposal: %s", getTextContent(t, result))
		}
		var body struct {
			PullRequest int `json:"pullRequest"`
		}
		_ = json.Unmarshal([]byte(getTextContent(t, result)), &body)
		pr := fake.pulls[body.PullRequest]
		return fake.branches[pr.headRepo][pr.branch], getTextContent(t, result)
	}
	first, _ := propose(4)
	if !strings.Contains(first[stage], "minRam: 4") {
		t.Fatalf("the first proposal does not lower Stage's search to 4:\n%s", first[stage])
	}
	fake.fileReadRefs = nil
	second, answer := propose(8)
	if second[stage] != main[stage] {
		t.Errorf("Stage still carries the first proposal's scale:\n%s", second[stage])
	}
	if !strings.Contains(second["0 — AI Agent/import.yaml"], "minRam: 8") {
		t.Errorf("the AI Agent tier does not carry the second proposal's scale")
	}
	if second[production] != main[production] {
		t.Errorf("Small Production's hand-written block was rewritten:\n%s", second[production])
	}
	if !strings.Contains(answer, "Small Production") || !strings.Contains(answer, "comment") {
		t.Errorf("answer = %s, want it to say why Small Production was left", answer)
	}
	for _, ref := range fake.fileReadRefs {
		if ref != fakeGroupHead(main) {
			t.Errorf("a tier was read at %q, not at main's head %q", ref, fakeGroupHead(main))
		}
	}
	if len(fake.fileReadRefs) == 0 {
		t.Error("no tier was read")
	}
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

// TestGroupRecipeScaling_AnEmptyDiffProposesNothing: a scale the compare
// answers with an empty diff is nothing to propose — not a failure, and never
// "pull request #0".
func TestGroupRecipeScaling_AnEmptyDiffProposesNothing(t *testing.T) {
	stateDir := t.TempDir()
	writeGiteaWiredPairMeta(t, stateDir)
	fake := newFakeGroupGitea()
	srv := fake.start(t)
	envPath := writeLiveEnvFile(t, map[string]string{"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken})
	rt := runtime.Info{InContainer: true, ProjectID: "p1"}
	ctx := context.Background()
	first := giteaGroupRecipeOutcome(ctx, scalingRecipeClient(1, 0.25), srv.Client(), rt, stateDir, envPath)
	if first.PullNumber == 0 {
		t.Fatalf("the first recipe was not proposed: %q", first.Line)
	}
	fake.merge(first.PullNumber)
	posts := fake.pullPosts
	fake.compareEmpty = true

	result, _, _ := handleGroupRecipeScaling(ctx, scalingRecipeClient(4, 1), srv.Client(), rt, stateDir, envPath, "search")
	answer := getTextContent(t, result)
	if result.IsError {
		t.Fatalf("an empty diff is not a failure: %s", answer)
	}
	if fake.pullPosts != posts {
		t.Errorf("pull requests opened = %d, want none", fake.pullPosts-posts)
	}
	if !strings.Contains(answer, "nothing to propose") || strings.Contains(answer, "#0") {
		t.Errorf("answer = %s, want nothing to propose", answer)
	}
}
