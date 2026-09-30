// Tests for: tools/standup_deploy.go — the stand-up's deploy order: which
// halves start together and which waits for what.
package tools

import (
	"maps"
	"slices"
	"strings"
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
		// reads is what each half's build reads; none falls back to priority.
		reads map[string][]string
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
		{
			name:  "the storefront's stage reads the API's stage: it waits for that and nothing else",
			pairs: []workflow.MateTierPair{standupTestPair("medusa", 5), standupTestPair("nextstore", 0)},
			reads: map[string][]string{"nextstorestage": {"medusastage"}},
			want: map[string][]string{
				"medusadev": nil, "nextstoredev": nil,
				"medusastage": {"medusadev"}, "nextstorestage": {"medusastage", "nextstoredev"},
			},
		},
		{
			name:  "reads, not priority: a stage above that nothing reads holds nobody",
			pairs: []workflow.MateTierPair{standupTestPair("api", 10), standupTestPair("worker", 5), standupTestPair("web", 0)},
			reads: map[string][]string{"webstage": {"apistage"}},
			want: map[string][]string{
				"apidev": nil, "workerdev": nil, "webdev": nil,
				"apistage": {"apidev"}, "workerstage": {"workerdev"}, "webstage": {"apistage", "webdev"},
			},
		},
		{
			name:  "a dev build that reads another dev half waits for it",
			pairs: []workflow.MateTierPair{standupTestPair("api", 0), standupTestPair("web", 0)},
			reads: map[string][]string{"webdev": {"apidev"}},
			want: map[string][]string{
				"apidev": nil, "webdev": {"apidev"},
				"apistage": {"apidev"}, "webstage": {"webdev"},
			},
		},
		{
			name:  "a dev build that reads a stage is not held for it — the stages come after development",
			pairs: []workflow.MateTierPair{standupTestPair("api", 5), standupTestPair("web", 0)},
			reads: map[string][]string{"webdev": {"apistage"}, "webstage": {"apistage"}},
			want: map[string][]string{
				"apidev": nil, "webdev": nil,
				"apistage": {"apidev"}, "webstage": {"apistage", "webdev"},
			},
		},
		{
			name:  "reads that go round in a circle fall back to priority",
			pairs: []workflow.MateTierPair{standupTestPair("api", 5), standupTestPair("web", 0)},
			reads: map[string][]string{"webstage": {"apistage"}, "apistage": {"webstage"}},
			want: map[string][]string{
				"apidev": nil, "webdev": nil,
				"apistage": {"apidev"}, "webstage": {"apistage", "webdev"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := standupAfter(tt.pairs, tt.reads, nil)
			if !maps.EqualFunc(got, tt.want, slices.Equal[[]string]) {
				t.Errorf("standupAfter =\n  %v\nwant\n  %v", got, tt.want)
			}
		})
	}
}

// TestStandupReads_FromTheRecipe: recipe → edges → schedule. What each half's
// build reads comes from its own setup in the pair's zerops.yaml — directly,
// through a lifted runtime variable, or through the tier's project variables
// — and orders the halves; what a half reads only once it runs orders nothing.
func TestStandupReads_FromTheRecipe(t *testing.T) {
	t.Parallel()
	pairs := []workflow.MateTierPair{standupTestPair("medusa", 5), standupTestPair("nextstore", 0)}
	pairs[0].Dev.Setup, pairs[0].Stage.Setup = "medusadev", "medusaprod"
	pairs[1].Dev.Setup, pairs[1].Stage.Setup = "nextstoredev", "nextstoreprod"
	const medusa = "zerops:\n  - setup: medusadev\n    run:\n      start: zsc noop\n  - setup: medusaprod\n    run:\n      envVariables:\n        STORE_CORS: https://${nextstorestage_zeropsSubdomain}\n"
	storefront := func(build string) string {
		return "zerops:\n  - setup: nextstoredev\n    run:\n      envVariables:\n        API: ${medusadev_zeropsSubdomain}\n      start: zsc noop\n  - setup: nextstoreprod\n" + build +
			"    run:\n      envVariables:\n        BACKEND_URL: https://${medusastage_zeropsSubdomain}\n      start: yarn start\n"
	}
	tests := []struct {
		name       string
		storefront string
		project    map[string]string
		wantReads  map[string][]string
		wantAfter  []string // what nextstorestage waits for
	}{
		{
			name:       "the storefront's build pre-renders from the API's stage",
			storefront: storefront("    build:\n      envVariables:\n        NEXT_PUBLIC_MEDUSA_BACKEND_URL: https://${medusastage_zeropsSubdomain}\n"),
			wantReads:  map[string][]string{"nextstorestage": {"medusastage"}},
			wantAfter:  []string{"medusastage", "nextstoredev"},
		},
		{
			name:       "through a lifted runtime variable",
			storefront: storefront("    build:\n      envVariables:\n        NEXT_PUBLIC_MEDUSA_BACKEND_URL: ${RUNTIME_BACKEND_URL}\n"),
			wantReads:  map[string][]string{"nextstorestage": {"medusastage"}},
			wantAfter:  []string{"medusastage", "nextstoredev"},
		},
		{
			name:       "through a project variable of the tier",
			storefront: storefront("    build:\n      envVariables:\n        NEXT_PUBLIC_MEDUSA_BACKEND_URL: ${MEDUSA_URL}\n"),
			project:    map[string]string{"MEDUSA_URL": "https://${medusastage_zeropsSubdomain}"},
			wantReads:  map[string][]string{"nextstorestage": {"medusastage"}},
			wantAfter:  []string{"medusastage", "nextstoredev"},
		},
		{
			name:       "reads only once they run: no edge, and the priority orders the stages",
			storefront: storefront(""),
			wantReads:  map[string][]string{},
			wantAfter:  []string{"medusastage", "nextstoredev"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reads, _ := standupReads(pairs, map[string]string{"medusadev": medusa, "nextstoredev": tt.storefront}, tt.project)
			if !maps.EqualFunc(reads, tt.wantReads, slices.Equal[[]string]) {
				t.Errorf("reads = %v, want %v", reads, tt.wantReads)
			}
			if got := standupAfter(pairs, reads, nil)["nextstorestage"]; !slices.Equal(got, tt.wantAfter) {
				t.Errorf("nextstorestage waits for %v, want %v", got, tt.wantAfter)
			}
		})
	}
}

// TestStandupReads_EveryRouteTheWriterCounts: the stand-up reads a build the
// way the recipe writer does, so a stage never builds before the stage it
// reads. Three pairs — an API above a storefront and a worker; the worker's
// stage reads the API's stage directly, so the order comes from reads — and
// the storefront's stage reads the API's stage through each route a plain
// read of its setup would miss.
func TestStandupReads_EveryRouteTheWriterCounts(t *testing.T) {
	t.Parallel()
	const worker = "zerops:\n  - setup: workerdev\n    run:\n      start: zsc noop\n  - setup: workerprod\n    build:\n      envVariables:\n        API: ${apistage_hostname}\n"
	const api = "zerops:\n  - setup: apidev\n    run:\n      start: zsc noop\n  - setup: apiprod\n    run:\n      start: node api.js\n"
	tests := []struct {
		name       string
		storefront string // "" = the pair's zerops.yaml could not be read
		stageEnvs  map[string]string
	}{
		{
			name:       "a service variable the build lifts",
			storefront: "zerops:\n  - setup: storefrontdev\n    run:\n      start: zsc noop\n  - setup: storefrontprod\n    build:\n      envVariables:\n        API: ${RUNTIME_API_URL}\n",
			stageEnvs:  map[string]string{"API_URL": "https://${apistage_zeropsSubdomain}"},
		},
		{
			name:       "a setup inherited through extends",
			storefront: "zerops:\n  - setup: base\n    build:\n      envVariables:\n        API: https://${apistage_zeropsSubdomain}\n  - setup: storefrontdev\n    run:\n      start: zsc noop\n  - setup: storefrontprod\n    extends: base\n",
		},
		{
			name: "a zerops.yaml that could not be read keeps its priority",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pairs := []workflow.MateTierPair{standupTestPair("api", 5), standupTestPair("storefront", 0), standupTestPair("worker", 0)}
			for i := range pairs {
				name := strings.TrimSuffix(pairs[i].Dev.Hostname, "dev")
				pairs[i].Dev.Setup, pairs[i].Stage.Setup = name+"dev", name+"prod"
			}
			pairs[1].Stage.Envs = tt.stageEnvs
			bodies := map[string]string{"apidev": api, "workerdev": worker}
			if tt.storefront != "" {
				bodies["storefrontdev"] = tt.storefront
			}
			reads, unread := standupReads(pairs, bodies, nil)
			after := standupAfter(pairs, reads, unread)
			if got := after["storefrontstage"]; !slices.Contains(got, "apistage") {
				t.Errorf("storefrontstage waits for %v, want apistage among them (reads %v)", got, reads)
			}
			if got := after["workerstage"]; !slices.Equal(got, []string{"apistage", "workerdev"}) {
				t.Errorf("workerstage waits for %v, want its read and its dev half", got)
			}
		})
	}
}
