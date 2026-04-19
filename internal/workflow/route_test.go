package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// fakeRecipeCorpus is a stub for SelectBootstrapRoute tests that don't want to drag
// the knowledge engine in. Behaviour is fully deterministic by field.
type fakeRecipeCorpus struct {
	match *RecipeMatch
	err   error
	calls int
}

func (f *fakeRecipeCorpus) FindViableMatch(_ string) (*RecipeMatch, error) {
	f.calls++
	return f.match, f.err
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

func TestSelectBootstrapRoute_AdoptBeatsRecipe(t *testing.T) {
	t.Parallel()

	corpus := &fakeRecipeCorpus{match: &RecipeMatch{Slug: "laravel-minimal", Confidence: 0.99}}
	existing := []platform.ServiceStack{userSvc("appdev", "nodejs@22")}
	route, match, err := SelectBootstrapRoute(context.Background(), "laravel", existing, corpus)
	if err != nil {
		t.Fatalf("SelectBootstrapRoute: %v", err)
	}
	if route != BootstrapRouteAdopt {
		t.Errorf("route = %q, want adopt", route)
	}
	if match != nil {
		t.Errorf("match = %+v, want nil on adopt", match)
	}
	// Adopt short-circuits — recipes corpus must NOT be consulted.
	if corpus.calls != 0 {
		t.Errorf("recipe corpus queried %d times, want 0", corpus.calls)
	}
}

func TestSelectBootstrapRoute_RecipeWhenHighConfidenceViable(t *testing.T) {
	t.Parallel()

	corpus := &fakeRecipeCorpus{match: &RecipeMatch{Slug: "laravel-jetstream", Confidence: 0.92}}
	route, match, err := SelectBootstrapRoute(context.Background(), "laravel api", nil, corpus)
	if err != nil {
		t.Fatalf("SelectBootstrapRoute: %v", err)
	}
	if route != BootstrapRouteRecipe {
		t.Errorf("route = %q, want recipe", route)
	}
	if match == nil || match.Slug != "laravel-jetstream" {
		t.Errorf("match = %+v, want laravel-jetstream", match)
	}
}

func TestSelectBootstrapRoute_ClassicWhenConfidenceBelowThreshold(t *testing.T) {
	t.Parallel()

	corpus := &fakeRecipeCorpus{match: &RecipeMatch{Slug: "weak-match", Confidence: 0.5}}
	route, match, err := SelectBootstrapRoute(context.Background(), "some unrelated intent", nil, corpus)
	if err != nil {
		t.Fatalf("SelectBootstrapRoute: %v", err)
	}
	if route != BootstrapRouteClassic {
		t.Errorf("route = %q, want classic", route)
	}
	if match != nil {
		t.Errorf("match = %+v, want nil (confidence below threshold)", match)
	}
}

func TestSelectBootstrapRoute_ClassicWhenNoMatch(t *testing.T) {
	t.Parallel()

	corpus := &fakeRecipeCorpus{match: nil}
	route, _, err := SelectBootstrapRoute(context.Background(), "bespoke app", nil, corpus)
	if err != nil {
		t.Fatalf("SelectBootstrapRoute: %v", err)
	}
	if route != BootstrapRouteClassic {
		t.Errorf("route = %q, want classic", route)
	}
}

func TestSelectBootstrapRoute_ClassicWhenIntentEmpty(t *testing.T) {
	t.Parallel()

	corpus := &fakeRecipeCorpus{match: &RecipeMatch{Slug: "laravel", Confidence: 0.99}}
	route, _, err := SelectBootstrapRoute(context.Background(), "", nil, corpus)
	if err != nil {
		t.Fatalf("SelectBootstrapRoute: %v", err)
	}
	if route != BootstrapRouteClassic {
		t.Errorf("route = %q, want classic (empty intent should not trigger recipe search)", route)
	}
	if corpus.calls != 0 {
		t.Errorf("recipe corpus queried %d times, want 0 on empty intent", corpus.calls)
	}
}

func TestSelectBootstrapRoute_ClassicWhenCorpusNil(t *testing.T) {
	t.Parallel()

	route, _, err := SelectBootstrapRoute(context.Background(), "laravel", nil, nil)
	if err != nil {
		t.Fatalf("SelectBootstrapRoute: %v", err)
	}
	if route != BootstrapRouteClassic {
		t.Errorf("route = %q, want classic when no corpus available", route)
	}
}

func TestSelectBootstrapRoute_IgnoresSystemAndManagedForAdopt(t *testing.T) {
	t.Parallel()

	existing := []platform.ServiceStack{
		systemSvc("l7-balancer"),
		userSvc("db", "postgresql@16"),
	}
	corpus := &fakeRecipeCorpus{match: &RecipeMatch{Slug: "laravel", Confidence: 0.99}}
	route, match, err := SelectBootstrapRoute(context.Background(), "laravel", existing, corpus)
	if err != nil {
		t.Fatalf("SelectBootstrapRoute: %v", err)
	}
	// Only managed + system present — no adoption required, recipe wins.
	if route != BootstrapRouteRecipe {
		t.Errorf("route = %q, want recipe (no adoptable runtime)", route)
	}
	if match == nil {
		t.Error("expected recipe match, got nil")
	}
}

func TestSelectBootstrapRoute_CorpusErrorSurfaces(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("corpus unreachable")
	corpus := &fakeRecipeCorpus{err: wantErr}
	_, _, err := SelectBootstrapRoute(context.Background(), "laravel", nil, corpus)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want wrapping %v", err, wantErr)
	}
}
