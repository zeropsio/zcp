// Tests for: tools/standup_deploy.go — the stand-up's deploy order: which
// halves start together and which waits for what.
package tools

import (
	"maps"
	"slices"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

func standupTestPair(name string, priority int) workflow.MateTierPair {
	return workflow.MateTierPair{
		RepoName: name + "dev",
		Dev:      workflow.MateTierRuntime{Hostname: name + "dev", Priority: priority},
		Stage:    workflow.MateTierRuntime{Hostname: name + "stage", Priority: priority},
	}
}

// TestStandupAfter_EveryDevHalfStartsAtOnce: a dev half's build installs
// dependencies and reads no API, so every dev half starts at once; a stage
// waits for its own dev half (it is cross-deployed from its checkout) and for
// the stage of every pair above it by priority, whose API its build may read.
func TestStandupAfter_EveryDevHalfStartsAtOnce(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		pairs []workflow.MateTierPair
		want  map[string][]string
	}{
		{
			name:  "one pair",
			pairs: []workflow.MateTierPair{standupTestPair("app", 0)},
			want:  map[string][]string{"appdev": nil, "appstage": {"appdev"}},
		},
		{
			name:  "an API above a storefront: the storefront's stage waits for the API's stage, never its dev half",
			pairs: []workflow.MateTierPair{standupTestPair("medusa", 5), standupTestPair("nextstore", 0)},
			want: map[string][]string{
				"medusadev":      nil,
				"nextstoredev":   nil,
				"medusastage":    {"medusadev"},
				"nextstorestage": {"medusastage", "nextstoredev"},
			},
		},
		{
			name:  "pairs of one priority wait for nothing of each other",
			pairs: []workflow.MateTierPair{standupTestPair("api", 0), standupTestPair("web", 0)},
			want: map[string][]string{
				"apidev": nil, "webdev": nil,
				"apistage": {"apidev"}, "webstage": {"webdev"},
			},
		},
		{
			name:  "three priorities: a stage waits for every stage above it",
			pairs: []workflow.MateTierPair{standupTestPair("api", 10), standupTestPair("worker", 5), standupTestPair("web", 0)},
			want: map[string][]string{
				"apidev": nil, "workerdev": nil, "webdev": nil,
				"apistage":    {"apidev"},
				"workerstage": {"apistage", "workerdev"},
				"webstage":    {"apistage", "webdev", "workerstage"},
			},
		},
		{
			name: "a pair's priority is its higher half's",
			pairs: []workflow.MateTierPair{
				{RepoName: "apidev", Dev: workflow.MateTierRuntime{Hostname: "apidev"}, Stage: workflow.MateTierRuntime{Hostname: "apistage", Priority: 3}},
				standupTestPair("web", 2),
			},
			want: map[string][]string{
				"apidev": nil, "webdev": nil,
				"apistage": {"apidev"}, "webstage": {"apistage", "webdev"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := standupAfter(tt.pairs)
			if !maps.EqualFunc(got, tt.want, slices.Equal[[]string]) {
				t.Errorf("standupAfter =\n  %v\nwant\n  %v", got, tt.want)
			}
		})
	}
}
