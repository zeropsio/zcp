// Tests for: BuildBootstrapRouteOptions — the discovery-phase entry point
// that produces the ranked list of routes the LLM chooses from.
package workflow

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
)

// fakeRecipeCorpus is a stub for BuildBootstrapRouteOptions tests that don't
// want to drag the knowledge engine in. Behaviour is fully deterministic
// by field.
type fakeRecipeCorpus struct {
	matches []RecipeMatch
	err     error
	calls   int
}

func (f *fakeRecipeCorpus) FindRankedMatches(_ string, limit int) ([]RecipeMatch, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if limit <= 0 || limit >= len(f.matches) {
		return append([]RecipeMatch(nil), f.matches...), nil
	}
	return append([]RecipeMatch(nil), f.matches[:limit]...), nil
}

func userSvc(name, typeVersion string) platform.ServiceStack {
	return platform.ServiceStack{
		Name: name,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  typeVersion,
			ServiceStackTypeCategoryName: "USER",
		},
	}
}

func systemSvc(name string) platform.ServiceStack {
	return platform.ServiceStack{
		Name: name,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  "l7-balancer",
			ServiceStackTypeCategoryName: "HTTP_L7_BALANCER",
		},
	}
}

// routesOf extracts the Route field from each option for quick equality
// checks — full struct comparison matters less than the ordering + inclusion
// contract the discovery API exposes.
func routesOf(opts []BootstrapRouteOption) []BootstrapRoute {
	out := make([]BootstrapRoute, len(opts))
	for i, o := range opts {
		out[i] = o.Route
	}
	return out
}

func findOption(opts []BootstrapRouteOption, r BootstrapRoute) *BootstrapRouteOption {
	for i := range opts {
		if opts[i].Route == r {
			return &opts[i]
		}
	}
	return nil
}

func TestBuildBootstrapRouteOptions_EmptyProject_GoodIntent_RanksRecipesThenClassic(t *testing.T) {
	t.Parallel()
	corpus := &fakeRecipeCorpus{matches: []RecipeMatch{
		{Slug: "laravel-minimal", Confidence: 0.95, ImportYAML: "services:\n  - hostname: app\n    type: php-nginx@8.4\n"},
		{Slug: "laravel-octane", Confidence: 0.67, ImportYAML: "services:\n  - hostname: octane\n    type: php-nginx@8.4\n"},
	}}

	opts, err := BuildBootstrapRouteOptions(context.Background(), "laravel weather", nil, nil, corpus, runtime.Info{})
	if err != nil {
		t.Fatalf("BuildBootstrapRouteOptions: %v", err)
	}
	want := []BootstrapRoute{BootstrapRouteRecipe, BootstrapRouteRecipe, BootstrapRouteClassic}
	if !slices.Equal(routesOf(opts), want) {
		t.Fatalf("routes = %v, want %v", routesOf(opts), want)
	}
	if opts[0].RecipeSlug != "laravel-minimal" {
		t.Errorf("first recipe: want laravel-minimal, got %q", opts[0].RecipeSlug)
	}
	if opts[0].Confidence <= opts[1].Confidence {
		t.Errorf("confidence sort broken: %.2f <= %.2f", opts[0].Confidence, opts[1].Confidence)
	}
}

func TestBuildBootstrapRouteOptions_EmptyProject_EmptyIntent_ClassicOnly(t *testing.T) {
	t.Parallel()
	corpus := &fakeRecipeCorpus{matches: []RecipeMatch{
		{Slug: "laravel", Confidence: 0.99, ImportYAML: "services:\n  - hostname: app\n    type: php-nginx@8.4\n"},
	}}
	opts, err := BuildBootstrapRouteOptions(context.Background(), "", nil, nil, corpus, runtime.Info{})
	if err != nil {
		t.Fatalf("BuildBootstrapRouteOptions: %v", err)
	}
	if got := routesOf(opts); !slices.Equal(got, []BootstrapRoute{BootstrapRouteClassic}) {
		t.Errorf("routes = %v, want [classic]", got)
	}
	if corpus.calls != 0 {
		t.Errorf("corpus queried %d times with empty intent, want 0", corpus.calls)
	}
}

func TestBuildBootstrapRouteOptions_EmptyProject_NoiseFloor_DropsWeakMatches(t *testing.T) {
	t.Parallel()
	corpus := &fakeRecipeCorpus{matches: []RecipeMatch{
		{Slug: "weak", Confidence: 0.3, ImportYAML: "services:\n  - hostname: app\n    type: php@8.4\n"},
	}}
	opts, err := BuildBootstrapRouteOptions(context.Background(), "anything", nil, nil, corpus, runtime.Info{})
	if err != nil {
		t.Fatalf("BuildBootstrapRouteOptions: %v", err)
	}
	if got := routesOf(opts); !slices.Equal(got, []BootstrapRoute{BootstrapRouteClassic}) {
		t.Errorf("routes = %v, want [classic] — weak match below noise floor should be dropped", got)
	}
}

func TestBuildBootstrapRouteOptions_NilCorpus_ClassicOnly(t *testing.T) {
	t.Parallel()
	opts, err := BuildBootstrapRouteOptions(context.Background(), "laravel", nil, nil, nil, runtime.Info{})
	if err != nil {
		t.Fatalf("BuildBootstrapRouteOptions: %v", err)
	}
	if got := routesOf(opts); !slices.Equal(got, []BootstrapRoute{BootstrapRouteClassic}) {
		t.Errorf("routes = %v, want [classic] when corpus is nil", got)
	}
}

func TestBuildBootstrapRouteOptions_AdoptableRuntime_IncludesAdoptBeforeRecipe(t *testing.T) {
	t.Parallel()
	corpus := &fakeRecipeCorpus{matches: []RecipeMatch{
		{Slug: "node-todo", Confidence: 0.95, ImportYAML: "services:\n  - hostname: api\n    type: nodejs@22\n"},
	}}
	existing := []platform.ServiceStack{userSvc("appdev", "nodejs@22")}

	opts, err := BuildBootstrapRouteOptions(context.Background(), "node todo app", existing, nil, corpus, runtime.Info{})
	if err != nil {
		t.Fatalf("BuildBootstrapRouteOptions: %v", err)
	}
	want := []BootstrapRoute{BootstrapRouteAdopt, BootstrapRouteRecipe, BootstrapRouteClassic}
	if !slices.Equal(routesOf(opts), want) {
		t.Fatalf("routes = %v, want %v — adopt must precede recipe", routesOf(opts), want)
	}
	adopt := findOption(opts, BootstrapRouteAdopt)
	if !slices.Equal(adopt.AdoptServices, []string{"appdev"}) {
		t.Errorf("adopt services = %v, want [appdev]", adopt.AdoptServices)
	}
}

func TestBuildBootstrapRouteOptions_ManagedOnly_NoAdopt(t *testing.T) {
	t.Parallel()
	// A project with only managed services is not adoptable — managed
	// services carry no mode/strategy, so there's nothing for adopt to decide.
	corpus := &fakeRecipeCorpus{matches: []RecipeMatch{
		{Slug: "laravel", Confidence: 0.95, ImportYAML: "services:\n  - hostname: appdev\n    type: php-nginx@8.4\n"},
	}}
	existing := []platform.ServiceStack{userSvc("db", "postgresql@16")}

	opts, err := BuildBootstrapRouteOptions(context.Background(), "laravel", existing, nil, corpus, runtime.Info{})
	if err != nil {
		t.Fatalf("BuildBootstrapRouteOptions: %v", err)
	}
	if got := routesOf(opts); !slices.Equal(got, []BootstrapRoute{BootstrapRouteRecipe, BootstrapRouteClassic}) {
		t.Errorf("routes = %v, want [recipe, classic] — managed-only project is not adoptable", got)
	}
}

func TestBuildBootstrapRouteOptions_SystemServicesIgnored(t *testing.T) {
	t.Parallel()
	existing := []platform.ServiceStack{systemSvc("l7-balancer")}
	opts, err := BuildBootstrapRouteOptions(context.Background(), "", existing, nil, nil, runtime.Info{})
	if err != nil {
		t.Fatalf("BuildBootstrapRouteOptions: %v", err)
	}
	if got := routesOf(opts); !slices.Equal(got, []BootstrapRoute{BootstrapRouteClassic}) {
		t.Errorf("routes = %v, want [classic] — system service should not trigger adopt", got)
	}
}

func TestBuildBootstrapRouteOptions_BootstrappedMeta_NoAdopt(t *testing.T) {
	t.Parallel()
	// Runtime with a complete ServiceMeta is already adopted/bootstrapped —
	// no adopt option surfaces.
	existing := []platform.ServiceStack{userSvc("appdev", "nodejs@22")}
	metas := []*ServiceMeta{{
		Hostname:       "appdev",
		Mode:           topology.PlanModeDev,
		BootstrappedAt: "2026-04-18T10:00:00Z",
	}}
	opts, err := BuildBootstrapRouteOptions(context.Background(), "", existing, metas, nil, runtime.Info{})
	if err != nil {
		t.Fatalf("BuildBootstrapRouteOptions: %v", err)
	}
	if got := routesOf(opts); !slices.Equal(got, []BootstrapRoute{BootstrapRouteClassic}) {
		t.Errorf("routes = %v, want [classic] — bootstrapped service is not adoptable", got)
	}
}

func TestBuildBootstrapRouteOptions_IncompleteMeta_PrefersResumeOverAdopt(t *testing.T) {
	t.Parallel()
	// Incomplete meta WITH session ID → resume; adopt is suppressed because
	// the slot is already claimed by a previous session.
	existing := []platform.ServiceStack{userSvc("appdev", "nodejs@22")}
	metas := []*ServiceMeta{{
		Hostname:         "appdev",
		Mode:             topology.PlanModeDev,
		BootstrapSession: "sess-abc",
		// BootstrappedAt intentionally empty — incomplete.
	}}
	opts, err := BuildBootstrapRouteOptions(context.Background(), "", existing, metas, nil, runtime.Info{})
	if err != nil {
		t.Fatalf("BuildBootstrapRouteOptions: %v", err)
	}
	want := []BootstrapRoute{BootstrapRouteResume, BootstrapRouteClassic}
	if !slices.Equal(routesOf(opts), want) {
		t.Fatalf("routes = %v, want %v", routesOf(opts), want)
	}
	res := findOption(opts, BootstrapRouteResume)
	if res.ResumeSession != "sess-abc" {
		t.Errorf("resumeSession = %q, want sess-abc", res.ResumeSession)
	}
	if !slices.Equal(res.ResumeServices, []string{"appdev"}) {
		t.Errorf("resumeServices = %v, want [appdev]", res.ResumeServices)
	}
}

func TestBuildBootstrapRouteOptions_IncompleteMetaOrphan_AdoptNotResume(t *testing.T) {
	t.Parallel()
	// Incomplete meta with NO session ID is an orphan — no session to
	// resume, so it falls under adopt.
	existing := []platform.ServiceStack{userSvc("appdev", "nodejs@22")}
	metas := []*ServiceMeta{{
		Hostname: "appdev",
		Mode:     topology.PlanModeDev,
		// Neither BootstrappedAt nor BootstrapSession set.
	}}
	opts, err := BuildBootstrapRouteOptions(context.Background(), "", existing, metas, nil, runtime.Info{})
	if err != nil {
		t.Fatalf("BuildBootstrapRouteOptions: %v", err)
	}
	want := []BootstrapRoute{BootstrapRouteAdopt, BootstrapRouteClassic}
	if !slices.Equal(routesOf(opts), want) {
		t.Errorf("routes = %v, want %v — orphan incomplete meta falls under adopt", routesOf(opts), want)
	}
}

func TestBuildBootstrapRouteOptions_RecipeCollisions_AnnotatedNotSuppressed(t *testing.T) {
	t.Parallel()
	// Recipe wants `db`; existing project already has `db` (managed). The
	// recipe is still surfaced (LLM may choose to force it, or suggest rename),
	// but the option carries the collision annotation so the LLM can see it.
	existing := []platform.ServiceStack{userSvc("db", "postgresql@16")}
	corpus := &fakeRecipeCorpus{matches: []RecipeMatch{
		{Slug: "laravel", Confidence: 0.95, ImportYAML: "services:\n  - hostname: appdev\n    type: php-nginx@8.4\n  - hostname: db\n    type: postgresql@16\n"},
	}}
	opts, err := BuildBootstrapRouteOptions(context.Background(), "laravel", existing, nil, corpus, runtime.Info{})
	if err != nil {
		t.Fatalf("BuildBootstrapRouteOptions: %v", err)
	}
	recipe := findOption(opts, BootstrapRouteRecipe)
	if recipe == nil {
		t.Fatal("recipe option missing despite matching intent")
	}
	if !slices.Equal(recipe.Collisions, []string{"db"}) {
		t.Errorf("collisions = %v, want [db]", recipe.Collisions)
	}
}

func TestBuildBootstrapRouteOptions_ClassicAlwaysLast(t *testing.T) {
	t.Parallel()
	// Regardless of input, classic is always the final entry — it's the
	// explicit override for "none of the above."
	tests := []struct {
		name     string
		intent   string
		existing []platform.ServiceStack
		metas    []*ServiceMeta
		matches  []RecipeMatch
	}{
		{"empty everything", "", nil, nil, nil},
		{"only classic possible", "laravel", nil, nil, nil},
		{"adopt + recipe + classic", "laravel",
			[]platform.ServiceStack{userSvc("appdev", "nodejs@22")},
			nil,
			[]RecipeMatch{{Slug: "laravel", Confidence: 0.95, ImportYAML: "services:\n  - hostname: app\n    type: php@8.4\n"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			corpus := &fakeRecipeCorpus{matches: tt.matches}
			opts, err := BuildBootstrapRouteOptions(context.Background(), tt.intent, tt.existing, tt.metas, corpus, runtime.Info{})
			if err != nil {
				t.Fatalf("BuildBootstrapRouteOptions: %v", err)
			}
			if len(opts) == 0 {
				t.Fatal("options should never be empty — classic is always included")
			}
			if opts[len(opts)-1].Route != BootstrapRouteClassic {
				t.Errorf("last option route = %q, want classic", opts[len(opts)-1].Route)
			}
		})
	}
}

func TestBuildBootstrapRouteOptions_CorpusError_Propagates(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("corpus unreachable")
	corpus := &fakeRecipeCorpus{err: wantErr}
	_, err := BuildBootstrapRouteOptions(context.Background(), "laravel", nil, nil, corpus, runtime.Info{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want wrapping %v", err, wantErr)
	}
}

func TestBuildBootstrapRouteOptions_CapsRecipeCountAtMaxRecipeOptions(t *testing.T) {
	t.Parallel()
	// Five strong candidates — only MaxRecipeOptions should surface.
	matches := []RecipeMatch{
		{Slug: "a", Confidence: 0.95, ImportYAML: "services:\n  - hostname: a\n    type: php@8\n"},
		{Slug: "b", Confidence: 0.90, ImportYAML: "services:\n  - hostname: b\n    type: php@8\n"},
		{Slug: "c", Confidence: 0.85, ImportYAML: "services:\n  - hostname: c\n    type: php@8\n"},
		{Slug: "d", Confidence: 0.80, ImportYAML: "services:\n  - hostname: d\n    type: php@8\n"},
		{Slug: "e", Confidence: 0.75, ImportYAML: "services:\n  - hostname: e\n    type: php@8\n"},
	}
	corpus := &fakeRecipeCorpus{matches: matches}
	opts, err := BuildBootstrapRouteOptions(context.Background(), "anything", nil, nil, corpus, runtime.Info{})
	if err != nil {
		t.Fatalf("BuildBootstrapRouteOptions: %v", err)
	}
	recipes := 0
	for _, o := range opts {
		if o.Route == BootstrapRouteRecipe {
			recipes++
		}
	}
	if recipes > MaxRecipeOptions {
		t.Errorf("recipe count = %d, want ≤ %d", recipes, MaxRecipeOptions)
	}
}

func TestRecipeCollisions_MalformedYAML_ReturnsNil(t *testing.T) {
	t.Parallel()
	existing := []platform.ServiceStack{userSvc("db", "postgresql@16")}
	got := recipeCollisions("this is not yaml", existing)
	if got != nil {
		t.Errorf("malformed yaml should produce nil collisions, got %v", got)
	}
}

func TestRecipeCollisions_EmptyInputs(t *testing.T) {
	t.Parallel()
	if got := recipeCollisions("", []platform.ServiceStack{userSvc("db", "postgresql@16")}); got != nil {
		t.Errorf("empty yaml should return nil, got %v", got)
	}
	if got := recipeCollisions("services:\n  - hostname: db\n", nil); got != nil {
		t.Errorf("empty existing should return nil, got %v", got)
	}
}

// userSvcWithID builds a service stack with explicit Zerops service ID so
// self-host filter tests can match by ServiceID (the canonical invariant).
func userSvcWithID(name, typeVersion, serviceID string) platform.ServiceStack {
	svc := userSvc(name, typeVersion)
	svc.ID = serviceID
	return svc
}

// Phase 2.1 — adoptableServices filters the agent's own host (ZCP control-plane
// container) so it never appears as an "adoptable" candidate. Live eval
// observed `adopt zcp` ranked #1 because the filter was missing.

func TestAdoptableServices_ExcludesSelfByServiceID(t *testing.T) {
	t.Parallel()
	existing := []platform.ServiceStack{
		userSvcWithID("zcp", "go@1", "service-zcp-id"),
		userSvcWithID("appdev", "nodejs@22", "service-app-id"),
	}
	self := runtime.Info{InContainer: true, ServiceID: "service-zcp-id", ServiceName: "zcp"}
	got := adoptableServices(existing, nil, self)
	if slices.Contains(got, "zcp") {
		t.Errorf("self host should be excluded; got %v", got)
	}
	if !slices.Contains(got, "appdev") {
		t.Errorf("non-self host should remain adoptable; got %v", got)
	}
}

func TestAdoptableServices_FallsBackToHostnameWhenNoServiceID(t *testing.T) {
	t.Parallel()
	// Local mode: runtime.Info has no ServiceID but DOES have ServiceName.
	// Filter should still exclude by hostname match.
	existing := []platform.ServiceStack{
		userSvc("zcp", "go@1"), // no platform ID at all
		userSvc("appdev", "nodejs@22"),
	}
	self := runtime.Info{InContainer: false, ServiceName: "zcp"}
	got := adoptableServices(existing, nil, self)
	if slices.Contains(got, "zcp") {
		t.Errorf("hostname-fallback filter should exclude zcp; got %v", got)
	}
}

// Phase 2.2 — stack-mismatch filter wired into BuildBootstrapRouteOptions.
// Live eval observed laravel-minimal (Postgres) ranked at confidence 0.95
// even when the user explicitly said "MySQL + Valkey". Filter must drop or
// demote.

func TestBuildBootstrapRouteOptions_DropsRecipeWithContradictedDB(t *testing.T) {
	t.Parallel()
	corpus := &fakeRecipeCorpus{matches: []RecipeMatch{
		{Slug: "laravel-minimal", Confidence: 0.95, ImportYAML: "services:\n  - hostname: appdev\n    type: php-nginx@8.4\n  - hostname: db\n    type: postgresql@18\n"},
	}}
	opts, err := BuildBootstrapRouteOptions(context.Background(), "Laravel app with MySQL", nil, nil, corpus, runtime.Info{})
	if err != nil {
		t.Fatalf("BuildBootstrapRouteOptions: %v", err)
	}
	for _, o := range opts {
		if o.Route == BootstrapRouteRecipe && o.RecipeSlug == "laravel-minimal" {
			t.Errorf("recipe with contradicted DB family must be dropped, got %v", o)
		}
	}
}

func TestBuildBootstrapRouteOptions_DemotesRecipeWithMissingDep(t *testing.T) {
	t.Parallel()
	// User wants Valkey cache; recipe has Postgres but no cache. Recipe
	// should still appear but ranked AFTER classic.
	corpus := &fakeRecipeCorpus{matches: []RecipeMatch{
		{Slug: "no-cache", Confidence: 0.95, ImportYAML: "services:\n  - hostname: appdev\n    type: nodejs@22\n  - hostname: db\n    type: postgresql@18\n"},
	}}
	opts, err := BuildBootstrapRouteOptions(context.Background(), "node app with valkey caching", nil, nil, corpus, runtime.Info{})
	if err != nil {
		t.Fatalf("BuildBootstrapRouteOptions: %v", err)
	}
	classicIdx := -1
	recipeIdx := -1
	for i, o := range opts {
		switch o.Route {
		case BootstrapRouteClassic:
			classicIdx = i
		case BootstrapRouteRecipe:
			recipeIdx = i
		case BootstrapRouteAdopt, BootstrapRouteResume:
			// not relevant to ordering assertion below
		}
	}
	if classicIdx == -1 || recipeIdx == -1 {
		t.Fatalf("missing routes: classicIdx=%d recipeIdx=%d, opts=%v", classicIdx, recipeIdx, routesOf(opts))
	}
	if recipeIdx < classicIdx {
		t.Errorf("recipe with missing dep must rank BELOW classic; classic=%d recipe=%d", classicIdx, recipeIdx)
	}
	if !strings.Contains(opts[recipeIdx].Why, "INCOMPLETE STACK") {
		t.Errorf("Why must flag missing dep: got %q", opts[recipeIdx].Why)
	}
}

func TestBuildBootstrapRouteOptions_KeepsCompatibleRecipe(t *testing.T) {
	t.Parallel()
	// User wants Postgres + Valkey; recipe matches exactly.
	corpus := &fakeRecipeCorpus{matches: []RecipeMatch{
		{Slug: "good", Confidence: 0.95, ImportYAML: "services:\n  - hostname: appdev\n    type: nodejs@22\n  - hostname: db\n    type: postgresql@18\n  - hostname: cache\n    type: valkey@7.2\n"},
	}}
	opts, err := BuildBootstrapRouteOptions(context.Background(), "node app with postgres + valkey", nil, nil, corpus, runtime.Info{})
	if err != nil {
		t.Fatalf("BuildBootstrapRouteOptions: %v", err)
	}
	classicIdx := -1
	recipeIdx := -1
	for i, o := range opts {
		switch o.Route {
		case BootstrapRouteClassic:
			classicIdx = i
		case BootstrapRouteRecipe:
			recipeIdx = i
		case BootstrapRouteAdopt, BootstrapRouteResume:
			// not relevant to ordering assertion below
		}
	}
	if recipeIdx == -1 || classicIdx == -1 {
		t.Fatalf("missing routes: classicIdx=%d recipeIdx=%d", classicIdx, recipeIdx)
	}
	if recipeIdx > classicIdx {
		t.Errorf("compatible recipe must rank ABOVE classic; classic=%d recipe=%d", classicIdx, recipeIdx)
	}
}

func TestAdoptableServices_LocalModeNoSelf_KeepsAllNonSystem(t *testing.T) {
	t.Parallel()
	existing := []platform.ServiceStack{
		userSvc("appdev", "nodejs@22"),
		userSvc("apidev", "go@1"),
	}
	self := runtime.Info{} // local dev — no self info at all
	got := adoptableServices(existing, nil, self)
	if len(got) != 2 {
		t.Errorf("local mode with no self info: want 2 adoptable, got %d (%v)", len(got), got)
	}
}
