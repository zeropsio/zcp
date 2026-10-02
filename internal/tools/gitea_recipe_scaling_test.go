package tools

import (
	"context"
	"encoding/json"
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
