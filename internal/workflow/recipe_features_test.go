package workflow

import (
	"strings"
	"testing"
)

// validGreetingFeature is the canonical hello-world feature — one
// feature covering api + ui + db. Reused by tests that don't care
// about shape details.
func validGreetingFeature() RecipeFeature {
	return RecipeFeature{
		ID:          "greeting",
		Description: "Fetch a greeting row from the database and render it.",
		Surface:     []string{FeatureSurfaceAPI, FeatureSurfaceUI, FeatureSurfaceDB},
		HealthCheck: "/api/greeting",
		UITestID:    "greeting",
		Interaction: "Open the page; observe the [data-feature=\"greeting\"] section populate with the greeting text within 2s.",
		MustObserve: "[data-feature=\"greeting\"] [data-value] text is non-empty and matches the seeded greeting.",
	}
}

// validShowcaseFeatures returns a full showcase feature set covering
// every managed-service surface: db, cache, storage, search, queue,
// plus worker + api + ui. Mirrors the structure a showcase agent
// should declare at research time.
func validShowcaseFeatures() []RecipeFeature {
	return []RecipeFeature{
		{
			ID:          "items-crud",
			Description: "List and create items from the database via a typed API.",
			Surface:     []string{FeatureSurfaceAPI, FeatureSurfaceUI, FeatureSurfaceDB},
			HealthCheck: "/api/items",
			UITestID:    "items-crud",
			Interaction: "Fill the items form title field, click Submit, observe the table row count increase by 1.",
			MustObserve: "[data-feature=\"items-crud\"] [data-row] count increases after submit.",
		},
		{
			ID:          "cache-demo",
			Description: "Write a cache entry with TTL and read it back.",
			Surface:     []string{FeatureSurfaceAPI, FeatureSurfaceUI, FeatureSurfaceCache},
			HealthCheck: "/api/cache",
			UITestID:    "cache-demo",
			Interaction: "Click Write, then Read; observe the written value render in the Read result panel.",
			MustObserve: "[data-feature=\"cache-demo\"] [data-result] text equals the written value.",
		},
		{
			ID:          "storage-upload",
			Description: "Upload a file to object storage and list uploaded files.",
			Surface:     []string{FeatureSurfaceAPI, FeatureSurfaceUI, FeatureSurfaceStorage},
			HealthCheck: "/api/files",
			UITestID:    "storage-upload",
			Interaction: "Fill the upload input with a sample file, click Upload, observe the new filename in the file list.",
			MustObserve: "[data-feature=\"storage-upload\"] [data-file] count increases after upload.",
		},
		{
			ID:          "search-items",
			Description: "Live full-text search across indexed items.",
			Surface:     []string{FeatureSurfaceAPI, FeatureSurfaceUI, FeatureSurfaceSearch},
			HealthCheck: "/api/search",
			UITestID:    "search-items",
			Interaction: "Type a query matching a seeded row into the search input, wait 400ms for debounce, observe hit rows render.",
			MustObserve: "[data-feature=\"search-items\"] [data-hit] count > 0 for a query known to match a seeded row.",
		},
		{
			ID:          "jobs-dispatch",
			Description: "Dispatch a background job via the messaging broker and render the processed result.",
			Surface:     []string{FeatureSurfaceAPI, FeatureSurfaceUI, FeatureSurfaceQueue, FeatureSurfaceWorker},
			HealthCheck: "/api/jobs",
			UITestID:    "jobs-dispatch",
			Interaction: "Click Dispatch Job; poll the result endpoint; observe the processedAt timestamp populate within 5s.",
			MustObserve: "[data-feature=\"jobs-dispatch\"] [data-processed-at] text is non-empty within the poll window.",
		},
	}
}

func TestValidateFeatures_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		features []RecipeFeature
		tier     string
		targets  []RecipeTarget
	}{
		{
			name:     "hello-world single feature",
			features: []RecipeFeature{validGreetingFeature()},
			tier:     RecipeTierMinimal,
			targets: []RecipeTarget{
				{Hostname: "app", Type: "nodejs@22"},
				{Hostname: "db", Type: "postgresql@18"},
			},
		},
		{
			name:     "minimal with two features",
			features: []RecipeFeature{validGreetingFeature(), {ID: "health", Description: "Liveness probe endpoint.", Surface: []string{FeatureSurfaceAPI}, HealthCheck: "/api/health"}},
			tier:     RecipeTierMinimal,
			targets: []RecipeTarget{
				{Hostname: "app", Type: "nodejs@22"},
				{Hostname: "db", Type: "postgresql@18"},
			},
		},
		{
			name:     "showcase full coverage",
			features: validShowcaseFeatures(),
			tier:     RecipeTierShowcase,
			targets: []RecipeTarget{
				{Hostname: "api", Type: "nodejs@22"},
				{Hostname: "app", Type: "nodejs@22", Role: "app"},
				{Hostname: "worker", Type: "nodejs@22", IsWorker: true},
				{Hostname: "db", Type: "postgresql@18"},
				{Hostname: "cache", Type: "valkey@8"},
				{Hostname: "queue", Type: "nats@2.12"},
				{Hostname: "storage", Type: "object-storage"},
				{Hostname: "search", Type: "meilisearch@1"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			errs := validateFeatures(tt.features, tt.tier, tt.targets)
			if len(errs) > 0 {
				t.Errorf("expected valid features, got errors: %v", errs)
			}
		})
	}
}

func TestValidateFeatures_Empty(t *testing.T) {
	t.Parallel()

	errs := validateFeatures(nil, RecipeTierMinimal, nil)
	if len(errs) == 0 {
		t.Fatal("expected error for empty features, got none")
	}
	if !strings.Contains(errs[0], "features is required") {
		t.Errorf("expected 'features is required' error, got: %v", errs[0])
	}
}

func TestValidateFeatures_SingleFeatureRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modify  func(*RecipeFeature)
		wantErr string
	}{
		{
			name:    "missing id",
			modify:  func(f *RecipeFeature) { f.ID = "" },
			wantErr: "id is required",
		},
		{
			name:    "bad id capital letters",
			modify:  func(f *RecipeFeature) { f.ID = "GreetingFeature" },
			wantErr: "id must match",
		},
		{
			name:    "bad id underscore",
			modify:  func(f *RecipeFeature) { f.ID = "greeting_feature" },
			wantErr: "id must match",
		},
		{
			name:    "description too short",
			modify:  func(f *RecipeFeature) { f.Description = "short" },
			wantErr: "description too short",
		},
		{
			name:    "missing surface",
			modify:  func(f *RecipeFeature) { f.Surface = nil },
			wantErr: "surface is required",
		},
		{
			name:    "unknown surface",
			modify:  func(f *RecipeFeature) { f.Surface = []string{"telepathy"} },
			wantErr: "surface \"telepathy\" not in allowed set",
		},
		{
			name:    "api surface missing healthCheck",
			modify:  func(f *RecipeFeature) { f.HealthCheck = "" },
			wantErr: "healthCheck is required when surface includes 'api'",
		},
		{
			name:    "healthCheck without leading slash",
			modify:  func(f *RecipeFeature) { f.HealthCheck = "api/greeting" },
			wantErr: "healthCheck \"api/greeting\" must start with '/'",
		},
		{
			name:    "ui surface missing uiTestId",
			modify:  func(f *RecipeFeature) { f.UITestID = "" },
			wantErr: "uiTestId is required when surface includes 'ui'",
		},
		{
			name:    "ui surface bad uiTestId",
			modify:  func(f *RecipeFeature) { f.UITestID = "Greeting" },
			wantErr: "uiTestId \"Greeting\" must match",
		},
		{
			name:    "ui surface missing interaction",
			modify:  func(f *RecipeFeature) { f.Interaction = "" },
			wantErr: "interaction is required when surface includes 'ui'",
		},
		{
			name:    "ui surface missing mustObserve",
			modify:  func(f *RecipeFeature) { f.MustObserve = "" },
			wantErr: "mustObserve is required when surface includes 'ui'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := validGreetingFeature()
			tt.modify(&f)
			errs := validateFeatures([]RecipeFeature{f}, RecipeTierMinimal, nil)
			if len(errs) == 0 {
				t.Fatalf("expected error containing %q, got none", tt.wantErr)
			}
			found := false
			for _, e := range errs {
				if strings.Contains(e, tt.wantErr) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, errs)
			}
		})
	}
}

func TestValidateFeatures_DuplicateID(t *testing.T) {
	t.Parallel()

	a := validGreetingFeature()
	b := validGreetingFeature() // same ID
	errs := validateFeatures([]RecipeFeature{a, b}, RecipeTierMinimal, nil)
	if len(errs) == 0 {
		t.Fatal("expected duplicate id error, got none")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e, "duplicate id") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'duplicate id' error, got: %v", errs)
	}
}

func TestValidateFeatures_ShowcaseCoverage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		features []RecipeFeature
		targets  []RecipeTarget
		wantErr  string
	}{
		{
			name: "missing search coverage — this is the v18 bug",
			features: func() []RecipeFeature {
				full := validShowcaseFeatures()
				// Drop the search-items feature; keep everything else.
				return append(full[:3], full[4:]...)
			}(),
			targets: []RecipeTarget{
				{Hostname: "api", Type: "nodejs@22"},
				{Hostname: "worker", Type: "nodejs@22", IsWorker: true},
				{Hostname: "db", Type: "postgresql@18"},
				{Hostname: "cache", Type: "valkey@8"},
				{Hostname: "queue", Type: "nats@2.12"},
				{Hostname: "storage", Type: "object-storage"},
				{Hostname: "search", Type: "meilisearch@1"},
			},
			wantErr: "\"search\" surface",
		},
		{
			name: "missing worker coverage",
			features: []RecipeFeature{
				{
					ID: "items", Description: "DB-backed items list.",
					Surface:     []string{FeatureSurfaceAPI, FeatureSurfaceUI, FeatureSurfaceDB},
					HealthCheck: "/api/items", UITestID: "items",
					Interaction: "Open page; observe rows.",
					MustObserve: "[data-feature=\"items\"] [data-row] count > 0.",
				},
			},
			targets: []RecipeTarget{
				{Hostname: "app", Type: "nodejs@22"},
				{Hostname: "worker", Type: "nodejs@22", IsWorker: true},
				{Hostname: "db", Type: "postgresql@18"},
			},
			wantErr: "\"worker\" surface",
		},
		{
			name: "missing ui entirely",
			features: []RecipeFeature{
				{
					ID: "api-only", Description: "API-only feature with no UI.",
					Surface:     []string{FeatureSurfaceAPI, FeatureSurfaceDB},
					HealthCheck: "/api/only",
				},
			},
			targets: []RecipeTarget{
				{Hostname: "app", Type: "nodejs@22"},
				{Hostname: "db", Type: "postgresql@18"},
			},
			wantErr: "\"ui\" surface",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			errs := validateFeatures(tt.features, RecipeTierShowcase, tt.targets)
			if len(errs) == 0 {
				t.Fatalf("expected error containing %q, got none", tt.wantErr)
			}
			found := false
			for _, e := range errs {
				if strings.Contains(e, tt.wantErr) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, errs)
			}
		})
	}
}

func TestRecipeFeature_HasSurface(t *testing.T) {
	t.Parallel()

	f := RecipeFeature{Surface: []string{FeatureSurfaceAPI, FeatureSurfaceUI, FeatureSurfaceDB}}
	cases := []struct {
		in   string
		want bool
	}{
		{FeatureSurfaceAPI, true},
		{FeatureSurfaceUI, true},
		{FeatureSurfaceDB, true},
		{FeatureSurfaceCache, false},
		{"", false},
	}
	for _, c := range cases {
		if got := f.hasSurface(c.in); got != c.want {
			t.Errorf("hasSurface(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
