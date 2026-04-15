package workflow

import (
	"os"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/schema"
)

// validMinimalPlan returns a structurally valid minimal recipe plan for testing.
func validMinimalPlan() RecipePlan {
	return RecipePlan{
		Framework:   "laravel",
		Tier:        RecipeTierMinimal,
		Slug:        "laravel-hello-world",
		RuntimeType: "php-nginx@8.4",
		BuildBases:  []string{"php@8.4"},
		Research: ResearchData{
			ServiceType:    "php-nginx",
			PackageManager: "composer",
			HTTPPort:       80,
			BuildCommands:  []string{"composer install"},
			DeployFiles:    []string{"."},
			StartCommand:   "php artisan serve",
			DBDriver:       "mysql",
			MigrationCmd:   "php artisan migrate",
			LoggingDriver:  "stderr",
		},
		Targets: []RecipeTarget{
			{Hostname: "app", Type: "php-nginx@8.4", Environments: []string{"0", "1", "2", "3", "4", "5"}},
		},
		Features: []RecipeFeature{validGreetingFeature()},
	}
}

// validShowcasePlan returns a structurally valid showcase recipe plan for testing.
// Includes all 7 required showcase service kinds: app, worker, database, cache,
// storage, mail catcher, search engine.
func validShowcasePlan() RecipePlan {
	p := validMinimalPlan()
	p.Tier = RecipeTierShowcase
	p.Slug = "laravel-showcase"
	p.Research.CacheLib = "predis"
	p.Research.SessionDriver = "redis"
	p.Research.QueueDriver = "redis"
	p.Research.StorageDriver = "s3"
	p.Research.SearchLib = "meilisearch"
	p.Research.MailLib = "smtp"
	p.Targets = []RecipeTarget{
		{Hostname: "app", Type: "php-nginx@8.4"},
		// Laravel default: shared-codebase worker (Horizon-style). Tests that
		// need the separate-codebase 3-repo case should clone this fixture and
		// clear SharesCodebaseWith (or build their own plan).
		{Hostname: "worker", Type: "php-nginx@8.4", IsWorker: true, SharesCodebaseWith: "app"},
		{Hostname: "db", Type: "mariadb@10.11"},
		{Hostname: "cache", Type: "keydb@6"},
		// NATS messaging broker — dedicated queue layer, not an overload of cache.
		// Required for every showcase plan (validateShowcaseServices).
		{Hostname: "queue", Type: "nats@2.12"},
		{Hostname: "storage", Type: "object-storage"},
		{Hostname: "search", Type: "meilisearch@1"},
	}
	p.Features = validShowcaseFeatures()
	return p
}

func TestValidateRecipePlan_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		plan RecipePlan
	}{
		{"minimal plan", validMinimalPlan()},
		{"showcase plan", validShowcasePlan()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			errs := ValidateRecipePlan(tt.plan, nil, nil)
			if len(errs) > 0 {
				t.Errorf("expected valid plan, got errors: %v", errs)
			}
		})
	}
}

func TestValidateRecipePlan_MissingFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modify  func(*RecipePlan)
		wantErr string
	}{
		{"missing framework", func(p *RecipePlan) { p.Framework = "" }, "framework is required"},
		{"invalid tier", func(p *RecipePlan) { p.Tier = "invalid" }, "tier must be"},
		{"missing slug", func(p *RecipePlan) { p.Slug = "" }, "slug is required"},
		{"invalid slug format", func(p *RecipePlan) { p.Slug = "BadSlug" }, "slug"},
		{"missing runtimeType", func(p *RecipePlan) { p.RuntimeType = "" }, "runtimeType is required"},
		{"missing serviceType", func(p *RecipePlan) { p.Research.ServiceType = "" }, "research.serviceType is required"},
		{"missing packageManager", func(p *RecipePlan) { p.Research.PackageManager = "" }, "research.packageManager is required"},
		{"missing httpPort", func(p *RecipePlan) { p.Research.HTTPPort = 0 }, "research.httpPort is required"},
		{"missing buildCommands", func(p *RecipePlan) { p.Research.BuildCommands = nil }, "research.buildCommands is required"},
		{"missing deployFiles", func(p *RecipePlan) { p.Research.DeployFiles = nil }, "research.deployFiles is required"},
		{"missing startCommand (non-implicit)", func(p *RecipePlan) {
			p.RuntimeType = "nodejs@22"
			p.Research.StartCommand = ""
		}, "research.startCommand is required"},
		{"missing targets", func(p *RecipePlan) { p.Targets = nil }, "at least one target is required"},
		{"target missing hostname", func(p *RecipePlan) {
			p.Targets = []RecipeTarget{{Type: "php@8.4"}}
		}, "hostname is required"},
		{"target missing type", func(p *RecipePlan) {
			p.Targets = []RecipeTarget{{Hostname: "app"}}
		}, "type is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan := validMinimalPlan()
			tt.modify(&plan)
			errs := ValidateRecipePlan(plan, nil, nil)
			if len(errs) == 0 {
				t.Fatal("expected validation errors")
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

func TestValidateRecipePlan_ShowcaseMissingFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modify  func(*RecipePlan)
		wantErr string
	}{
		{"missing cacheLib", func(p *RecipePlan) { p.Research.CacheLib = "" }, "cacheLib"},
		{"missing sessionDriver", func(p *RecipePlan) { p.Research.SessionDriver = "" }, "sessionDriver"},
		{"missing queueDriver", func(p *RecipePlan) { p.Research.QueueDriver = "" }, "queueDriver"},
		{"missing storageDriver", func(p *RecipePlan) { p.Research.StorageDriver = "" }, "storageDriver"},
		{"missing searchLib", func(p *RecipePlan) { p.Research.SearchLib = "" }, "searchLib"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan := validShowcasePlan()
			tt.modify(&plan)

			errs := ValidateRecipePlan(plan, nil, nil)
			if len(errs) == 0 {
				t.Fatal("expected validation errors for showcase missing field")
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

func TestValidateRecipePlan_ShowcaseMissingServices(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modify  func(*RecipePlan)
		wantErr string
	}{
		{"missing worker", func(p *RecipePlan) {
			// Remove worker target.
			var filtered []RecipeTarget
			for _, t := range p.Targets {
				if !t.IsWorker {
					filtered = append(filtered, t)
				}
			}
			p.Targets = filtered
		}, "worker"},
		{"missing database", func(p *RecipePlan) {
			var filtered []RecipeTarget
			for _, t := range p.Targets {
				if serviceTypeKind(t.Type) != "database" {
					filtered = append(filtered, t)
				}
			}
			p.Targets = filtered
		}, "database"},
		{"missing cache", func(p *RecipePlan) {
			var filtered []RecipeTarget
			for _, t := range p.Targets {
				if serviceTypeKind(t.Type) != "cache" {
					filtered = append(filtered, t)
				}
			}
			p.Targets = filtered
		}, "cache"},
		{"missing storage", func(p *RecipePlan) {
			var filtered []RecipeTarget
			for _, t := range p.Targets {
				if serviceTypeKind(t.Type) != "storage" {
					filtered = append(filtered, t)
				}
			}
			p.Targets = filtered
		}, "storage"},
		{"missing search engine", func(p *RecipePlan) {
			var filtered []RecipeTarget
			for _, t := range p.Targets {
				if serviceTypeKind(t.Type) != "search engine" {
					filtered = append(filtered, t)
				}
			}
			p.Targets = filtered
		}, "search engine"},
		{"missing messaging broker", func(p *RecipePlan) {
			// Remove the NATS target — validation must reject a showcase that
			// overloads cache with queue responsibility.
			var filtered []RecipeTarget
			for _, t := range p.Targets {
				if serviceTypeKind(t.Type) != "messaging" {
					filtered = append(filtered, t)
				}
			}
			p.Targets = filtered
		}, "messaging"},
		{"missing app (no non-worker runtime)", func(p *RecipePlan) {
			var filtered []RecipeTarget
			for _, t := range p.Targets {
				if !IsRuntimeType(t.Type) || t.IsWorker {
					filtered = append(filtered, t)
				}
			}
			p.Targets = filtered
		}, "app"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan := validShowcasePlan()
			tt.modify(&plan)

			errs := ValidateRecipePlan(plan, nil, nil)
			if len(errs) == 0 {
				t.Fatal("expected validation errors for missing showcase service")
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

// TestValidateRecipePlan_WorkerCodebaseRefs locks the semantics of the new
// explicit SharesCodebaseWith field on RecipeTarget. The happy path is
// covered by TestValidateRecipePlan_Valid + the 3-repo test; this table
// enumerates every rejection rule.
func TestValidateRecipePlan_WorkerCodebaseRefs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modify  func(*RecipePlan)
		wantErr string // substring that must appear in at least one error
	}{
		{
			name: "sharesCodebaseWith on non-worker is rejected",
			modify: func(p *RecipePlan) {
				for i := range p.Targets {
					if p.Targets[i].Hostname == "app" {
						p.Targets[i].SharesCodebaseWith = "db" // nonsense but illustrative
					}
				}
			},
			wantErr: "only valid on worker targets",
		},
		{
			name: "sharesCodebaseWith referencing unknown target is rejected",
			modify: func(p *RecipePlan) {
				for i := range p.Targets {
					if p.Targets[i].IsWorker {
						p.Targets[i].SharesCodebaseWith = "nonexistent"
					}
				}
			},
			wantErr: "unknown target",
		},
		{
			name: "sharesCodebaseWith pointing at another worker is rejected",
			modify: func(p *RecipePlan) {
				// Add a second worker and make the first point at it.
				p.Targets = append(p.Targets, RecipeTarget{
					Hostname: "worker2", Type: "php-nginx@8.4", IsWorker: true,
				})
				for i := range p.Targets {
					if p.Targets[i].Hostname == "worker" {
						p.Targets[i].SharesCodebaseWith = "worker2"
					}
				}
			},
			wantErr: "workers cannot host workers",
		},
		{
			name: "sharesCodebaseWith pointing at a managed service is rejected",
			modify: func(p *RecipePlan) {
				for i := range p.Targets {
					if p.Targets[i].IsWorker {
						p.Targets[i].SharesCodebaseWith = "db"
					}
				}
			},
			wantErr: "non-runtime target",
		},
		{
			name: "sharesCodebaseWith with base-runtime mismatch is rejected",
			modify: func(p *RecipePlan) {
				// Add an API target with a different runtime, then point the
				// (PHP) worker at it — base runtimes don't match.
				p.Targets = append(p.Targets, RecipeTarget{
					Hostname: "api", Type: "nodejs@22", Role: "api",
				})
				for i := range p.Targets {
					if p.Targets[i].IsWorker {
						p.Targets[i].SharesCodebaseWith = "api"
					}
				}
			},
			wantErr: "same base runtime",
		},
		{
			// Guards Rule 5: worker's OWN type must be a runtime. Without
			// this check, the base-runtime comparison produces a misleading
			// error when the agent accidentally types a worker as a managed
			// service.
			name: "worker typed as managed service is rejected with precise error",
			modify: func(p *RecipePlan) {
				for i := range p.Targets {
					if p.Targets[i].IsWorker {
						p.Targets[i].Type = "postgresql@17"
						p.Targets[i].SharesCodebaseWith = "app"
					}
				}
			},
			wantErr: "worker type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan := validShowcasePlan()
			tt.modify(&plan)

			errs := ValidateRecipePlan(plan, nil, nil)
			if len(errs) == 0 {
				t.Fatalf("expected validation errors, got none")
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

// TestValidateRecipePlan_MessagingHostnameEnforced locks the convention that
// every showcase messaging broker must use hostname "queue". The hostname is
// load-bearing for docs (themes/services.md), rendering tests
// (NATSQueueRendering), and cross-service env refs in recipe.md — if an agent
// names it "broker" or "nats" the whole downstream chain drifts.
func TestValidateRecipePlan_MessagingHostnameEnforced(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hostname string
		wantErr  bool
	}{
		{"canonical queue hostname", "queue", false},
		{"broker is rejected", "broker", true},
		{"nats is rejected (even though type matches)", "nats", true},
		{"msg is rejected", "msg", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan := validShowcasePlan()
			for i := range plan.Targets {
				if serviceTypeKind(plan.Targets[i].Type) == kindMessaging {
					plan.Targets[i].Hostname = tt.hostname
					break
				}
			}
			errs := ValidateRecipePlan(plan, nil, nil)
			hasHostnameErr := false
			for _, e := range errs {
				if strings.Contains(e, "messaging broker must use hostname") {
					hasHostnameErr = true
					break
				}
			}
			if tt.wantErr && !hasHostnameErr {
				t.Errorf("expected messaging hostname error for %q, got: %v", tt.hostname, errs)
			}
			if !tt.wantErr && hasHostnameErr {
				t.Errorf("unexpected messaging hostname error for %q: %v", tt.hostname, errs)
			}
		})
	}
}

// TestValidateRecipePlan_SeparateCodebaseWorker locks the default behaviour:
// a worker with empty SharesCodebaseWith is a SEPARATE codebase, and the
// plan validates cleanly regardless of whether its base runtime matches any
// other target. This is the inverse guard for the old runtime-match heuristic.
func TestValidateRecipePlan_SeparateCodebaseWorker(t *testing.T) {
	t.Parallel()

	plan := validShowcasePlan()
	// Default fixture has SharesCodebaseWith="app"; flip to separate.
	for i := range plan.Targets {
		if plan.Targets[i].IsWorker {
			plan.Targets[i].SharesCodebaseWith = ""
		}
	}
	errs := ValidateRecipePlan(plan, nil, nil)
	if len(errs) > 0 {
		t.Errorf("separate-codebase worker (same runtime as app) should validate cleanly, got: %v", errs)
	}
}

func TestValidateRecipePlan_InvalidTypes(t *testing.T) {
	t.Parallel()

	liveTypes := []platform.ServiceStackType{
		{
			Name:     "php-nginx",
			Category: "USER",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "php-nginx@8.4", Status: "ACTIVE"},
			},
		},
		{
			Name:     "zbuild php",
			Category: "BUILD",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "php@8.4", Status: "ACTIVE"},
			},
		},
	}

	tests := []struct {
		name    string
		modify  func(*RecipePlan)
		wantErr string
	}{
		{"invalid runtimeType", func(p *RecipePlan) {
			p.RuntimeType = "nonexistent@1.0"
		}, "runtimeType"},
		{"invalid buildBase", func(p *RecipePlan) {
			p.BuildBases = []string{"nonexistent@1.0"}
		}, "buildBase"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan := validMinimalPlan()
			tt.modify(&plan)

			errs := ValidateRecipePlan(plan, liveTypes, nil)
			if len(errs) == 0 {
				t.Fatal("expected validation errors for invalid types")
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

func TestValidateRecipePlan_ValidWithLiveTypes(t *testing.T) {
	t.Parallel()

	liveTypes := []platform.ServiceStackType{
		{
			Name:     "php-nginx",
			Category: "USER",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "php-nginx@8.4", Status: "ACTIVE"},
			},
		},
		{
			Name:     "zbuild php",
			Category: "BUILD",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "php@8.4", Status: "ACTIVE"},
			},
		},
	}

	plan := validMinimalPlan()
	errs := ValidateRecipePlan(plan, liveTypes, nil)
	if len(errs) > 0 {
		t.Errorf("expected valid plan with live types, got errors: %v", errs)
	}
}

func TestValidateRecipePlan_SlugPatterns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		slug  string
		valid bool
	}{
		{"php-hello-world", true},
		{"bun-hello-world", true},
		{"python-hello-world", true},
		{"laravel-minimal", true},
		{"nestjs-minimal", true},
		{"django-minimal", true},
		{"nestjs-showcase", true},
		{"django-showcase", true},
		{"BadSlug", false},
		{"laravel", false},
		{"laravel-", false},
		{"-hello-world", false},
		{"laravel hello world", false},
		{"LARAVEL-hello-world", false},
	}

	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			t.Parallel()
			plan := validMinimalPlan()
			plan.Slug = tt.slug
			errs := ValidateRecipePlan(plan, nil, nil)

			hasSlugErr := false
			for _, e := range errs {
				if strings.Contains(e, "slug") {
					hasSlugErr = true
					break
				}
			}

			if tt.valid && hasSlugErr {
				t.Errorf("slug %q should be valid, got slug error: %v", tt.slug, errs)
			}
			if !tt.valid && !hasSlugErr {
				t.Errorf("slug %q should be invalid, got no slug error", tt.slug)
			}
		})
	}
}

// loadTestSchemas loads the live schema test data from internal/schema/testdata/.
func loadTestSchemas(t *testing.T) *schema.Schemas {
	t.Helper()
	zeropsData, err := os.ReadFile("../schema/testdata/zerops_yml_schema.json")
	if err != nil {
		t.Fatalf("read zerops.yaml schema: %v", err)
	}
	importData, err := os.ReadFile("../schema/testdata/import_yml_schema.json")
	if err != nil {
		t.Fatalf("read import.yaml schema: %v", err)
	}
	zy, err := schema.ParseZeropsYmlSchema(zeropsData)
	if err != nil {
		t.Fatalf("parse zerops.yaml schema: %v", err)
	}
	iy, err := schema.ParseImportYmlSchema(importData)
	if err != nil {
		t.Fatalf("parse import.yaml schema: %v", err)
	}
	return &schema.Schemas{ZeropsYml: zy, ImportYml: iy}
}

func TestValidateRecipePlan_WithSchemas(t *testing.T) {
	t.Parallel()
	schemas := loadTestSchemas(t)

	plan := validMinimalPlan()
	errs := ValidateRecipePlan(plan, nil, schemas)
	if len(errs) > 0 {
		t.Errorf("expected valid plan with schemas, got errors: %v", errs)
	}
}

func TestValidateRecipePlan_SchemaBuildBaseValidation(t *testing.T) {
	t.Parallel()
	schemas := loadTestSchemas(t)

	tests := []struct {
		name    string
		bases   []string
		wantErr bool
	}{
		{"php valid", []string{"php@8.4"}, false},
		{"nodejs valid", []string{"nodejs@22"}, false},
		{"multi valid", []string{"php@8.4", "nodejs@22"}, false},
		{"invalid base", []string{"foobar@1.0"}, true},
		{"php-nginx not a build base", []string{"php-nginx@8.4"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan := validMinimalPlan()
			plan.BuildBases = tt.bases
			errs := ValidateRecipePlan(plan, nil, schemas)
			hasBuildErr := false
			for _, e := range errs {
				if strings.Contains(e, "buildBase") {
					hasBuildErr = true
					break
				}
			}
			if tt.wantErr && !hasBuildErr {
				t.Errorf("expected buildBase error for %v, got none", tt.bases)
			}
			if !tt.wantErr && hasBuildErr {
				t.Errorf("unexpected buildBase error for %v: %v", tt.bases, errs)
			}
		})
	}
}

func TestValidateRecipePlan_SchemaRuntimeTypeValidation(t *testing.T) {
	t.Parallel()
	schemas := loadTestSchemas(t)

	tests := []struct {
		name    string
		rt      string
		wantErr bool
	}{
		{"php-nginx valid", "php-nginx@8.4", false},
		{"nodejs valid", "nodejs@22", false},
		{"static valid", "static", false},
		{"bare php invalid", "php@8.4", true},
		{"nonexistent invalid", "foobar@1.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan := validMinimalPlan()
			plan.RuntimeType = tt.rt
			errs := ValidateRecipePlan(plan, nil, schemas)
			hasRTErr := false
			for _, e := range errs {
				if strings.Contains(e, "runtimeType") {
					hasRTErr = true
					break
				}
			}
			if tt.wantErr && !hasRTErr {
				t.Errorf("expected runtimeType error for %q, got none", tt.rt)
			}
			if !tt.wantErr && hasRTErr {
				t.Errorf("unexpected runtimeType error for %q: %v", tt.rt, errs)
			}
		})
	}
}

func TestValidateRecipePlan_SchemaTargetTypeValidation(t *testing.T) {
	t.Parallel()
	schemas := loadTestSchemas(t)

	tests := []struct {
		name    string
		targets []RecipeTarget
		wantErr bool
	}{
		{"valid targets", []RecipeTarget{
			{Hostname: "app", Type: "php-nginx@8.4"},
			{Hostname: "db", Type: "postgresql@18"},
		}, false},
		{"invalid target type", []RecipeTarget{
			{Hostname: "app", Type: "foobar@1.0"},
		}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan := validMinimalPlan()
			plan.Targets = tt.targets
			errs := ValidateRecipePlan(plan, nil, schemas)
			hasTypeErr := false
			for _, e := range errs {
				if strings.Contains(e, "import.yaml schema") {
					hasTypeErr = true
					break
				}
			}
			if tt.wantErr && !hasTypeErr {
				t.Errorf("expected target type error, got none")
			}
			if !tt.wantErr && hasTypeErr {
				t.Errorf("unexpected target type error: %v", errs)
			}
		})
	}
}

// TestValidateRecipePlan_LatestManagedVersion locks the rule that landed
// after the v14 nestjs-showcase run shipped postgresql@17 in all six
// generated environment imports while @18 was available in the catalog.
// The model picked second-newest at research-step recipePlan submission
// with no rationale, and finalize froze the wrong type into the deliverable.
// The fix: every managed service target whose version is older than the
// catalog latest must explain itself via TypePinReason — silent drift to
// second-newest is the failure mode being prevented.
func TestValidateRecipePlan_LatestManagedVersion(t *testing.T) {
	t.Parallel()
	schemas := loadTestSchemas(t)

	tests := []struct {
		name      string
		target    RecipeTarget
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "latest postgresql passes",
			target:  RecipeTarget{Hostname: "db", Type: "postgresql@18"},
			wantErr: false,
		},
		{
			// The exact v14 failure mode — agent picked @17 with no
			// rationale from a {14,16,17,18} catalog.
			name:      "older postgresql without reason rejected",
			target:    RecipeTarget{Hostname: "db", Type: "postgresql@17"},
			wantErr:   true,
			errSubstr: "pins an older version",
		},
		{
			// Escape hatch: deliberate pin documented in TypePinReason
			// must be accepted (otherwise the rule blocks legitimate
			// compat constraints like "library X doesn't yet support 18").
			name: "older postgresql with pin reason accepted",
			target: RecipeTarget{
				Hostname:      "db",
				Type:          "postgresql@17",
				TypePinReason: "node-postgres 9.x driver still throws on pg18 array_agg result shape",
			},
			wantErr: false,
		},
		{
			// "latest" alias is itself the latest by definition — must
			// never trigger the rule, regardless of catalog state.
			name:    "postgresql@latest alias passes (when in schema)",
			target:  RecipeTarget{Hostname: "db", Type: "postgresql@18"},
			wantErr: false,
		},
		{
			// meilisearch in test schema is {1.10, 1.20} — pinning 1.10
			// without a reason is the same class of bug as postgresql@17.
			name:      "older meilisearch without reason rejected",
			target:    RecipeTarget{Hostname: "search", Type: "meilisearch@1.10"},
			wantErr:   true,
			errSubstr: "pins an older version",
		},
		{
			name:    "latest meilisearch passes",
			target:  RecipeTarget{Hostname: "search", Type: "meilisearch@1.20"},
			wantErr: false,
		},
		{
			// nats latest in test schema is 2.12. Older 2.10 must be
			// rejected unless explained.
			name:      "older nats without reason rejected",
			target:    RecipeTarget{Hostname: "queue", Type: "nats@2.10"},
			wantErr:   true,
			errSubstr: "nats@2.12",
		},
		{
			// Single-version base — keydb test schema has only @6, so
			// "latest" and the only option are the same. No rule trip.
			name:    "single-version base accepted",
			target:  RecipeTarget{Hostname: "cache", Type: "keydb@6"},
			wantErr: false,
		},
		{
			// Utility services have no version suffix and no version
			// catalog — the rule must not produce a false positive.
			name:    "object-storage no version skipped",
			target:  RecipeTarget{Hostname: "storage", Type: "object-storage"},
			wantErr: false,
		},
		{
			// Runtimes are exempt from the rule even when older — their
			// version comes from framework compat negotiated at research
			// time. nodejs test schema is {18,20,22,24}, agent picks 22
			// for NestJS 10 compat, must not be flagged.
			name:    "nodejs@22 runtime exempt even though @24 exists",
			target:  RecipeTarget{Hostname: "api", Type: "nodejs@22"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan := validMinimalPlan()
			// Need a runtime target to keep the plan otherwise valid;
			// add the test target alongside it.
			plan.Targets = []RecipeTarget{
				{Hostname: "app", Type: "php-nginx@8.4"},
				tt.target,
			}
			errs := ValidateRecipePlan(plan, nil, schemas)
			hasErr := false
			for _, e := range errs {
				if strings.Contains(e, "pins an older version") || strings.Contains(e, tt.errSubstr) {
					hasErr = true
					break
				}
			}
			if tt.wantErr && !hasErr {
				t.Errorf("expected latest-version error containing %q for %+v, got: %v", tt.errSubstr, tt.target, errs)
			}
			if !tt.wantErr && hasErr {
				t.Errorf("unexpected latest-version error for %+v: %v", tt.target, errs)
			}
		})
	}
}

// TestLatestManagedVersion exercises the version comparison helper directly
// — covers the parsing edge cases the validator depends on (alias filtering,
// "+suffix" stripping, single-version bases, mixed-format catalogs).
func TestLatestManagedVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		types []string
		base  string
		want  string
	}{
		{
			name:  "postgresql 14-18 picks 18",
			types: []string{"postgresql@14", "postgresql@16", "postgresql@17", "postgresql@18"},
			base:  "postgresql",
			want:  "18",
		},
		{
			name:  "meilisearch 1.10 vs 1.20 picks 1.20",
			types: []string{"meilisearch@1.10", "meilisearch@1.20"},
			base:  "meilisearch",
			want:  "1.20",
		},
		{
			name: "version aliases ignored",
			// 'latest' is not concrete, must not be returned as latest.
			types: []string{"postgresql@17", "postgresql@18", "postgresql@latest"},
			base:  "postgresql",
			want:  "18",
		},
		{
			name: "canary and nightly ignored",
			// Pre-release channels must never outrank concrete versions.
			types: []string{"bun@1.2", "bun@1.3", "bun@canary", "bun@nightly"},
			base:  "bun",
			want:  "1.3",
		},
		{
			name: "patch versions sort numerically not lexically",
			// "1.10" must rank above "1.2" — lexical sort would invert this.
			types: []string{"foo@1.2", "foo@1.10"},
			base:  "foo",
			want:  "1.10",
		},
		{
			name: "+suffix stripped before comparison",
			// php-nginx bundles a secondary nginx version after '+'. The
			// PHP version is what orders, the nginx tag is informational.
			types: []string{"php-nginx@8.3+1.22", "php-nginx@8.4+1.22", "php-nginx@8.5+1.28"},
			base:  "php-nginx",
			want:  "8.5+1.28",
		},
		{
			name:  "unknown base returns empty",
			types: []string{"postgresql@18"},
			base:  "mongodb",
			want:  "",
		},
		{
			name:  "single version base returns that version",
			types: []string{"keydb@6"},
			base:  "keydb",
			want:  "6",
		},
		{
			name: "non-numeric versions skipped",
			// 'edge' is non-numeric and not in the alias list — parser
			// returns nil components, helper skips silently.
			types: []string{"foo@edge", "foo@1.0"},
			base:  "foo",
			want:  "1.0",
		},
		{
			name:  "empty list returns empty",
			types: nil,
			base:  "postgresql",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := latestManagedVersion(tt.types, tt.base)
			if got != tt.want {
				t.Errorf("latestManagedVersion(%v, %q) = %q, want %q", tt.types, tt.base, got, tt.want)
			}
		})
	}
}

// TestValidateRecipePlan_DBDriverRejectsORMs exercises the
// research.dbDriver validation. v16's nestjs-showcase shipped
// `dbDriver: "typeorm"` which leaked into the published root README
// as "connected to typeorm" (TypeORM is an ORM library, not a
// database). The validator now rejects ORM/client-library names at
// research-complete time with a targeted error.
func TestValidateRecipePlan_DBDriverRejectsORMs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		driver string
		bad    bool
	}{
		// Database names — valid.
		{"postgresql accepted", "postgresql", false},
		{"mariadb accepted", "mariadb", false},
		{"mysql accepted", "mysql", false},
		{"mongodb accepted", "mongodb", false},
		{"none accepted", "none", false},
		{"empty accepted", "", false},
		// ORM library names — rejected.
		{"typeorm rejected (v16 bug)", "typeorm", true},
		{"prisma rejected", "prisma", true},
		{"sequelize rejected", "sequelize", true},
		{"mongoose rejected", "mongoose", true},
		{"eloquent rejected", "eloquent", true},
		{"sqlalchemy rejected", "sqlalchemy", true},
		{"gorm rejected", "gorm", true},
		// Query builders — rejected.
		{"knex rejected", "knex", true},
		{"kysely rejected", "kysely", true},
		{"drizzle rejected", "drizzle", true},
		// Unknown values — rejected with generic message.
		{"garbage rejected", "asdf", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := validMinimalPlan()
			plan.Research.DBDriver = tc.driver
			errs := ValidateRecipePlan(plan, nil, nil)
			var dbDriverErr string
			for _, e := range errs {
				if strings.Contains(e, "research.dbDriver") {
					dbDriverErr = e
					break
				}
			}
			if tc.bad && dbDriverErr == "" {
				t.Errorf("expected dbDriver=%q to be rejected, got errs=%v", tc.driver, errs)
			}
			if !tc.bad && dbDriverErr != "" {
				t.Errorf("expected dbDriver=%q to be accepted, got error: %s", tc.driver, dbDriverErr)
			}
			// v16-specific message contract: when rejecting typeorm,
			// the error must name the ORM concept so the agent's retry
			// knows what to fix.
			if tc.driver == "typeorm" && !strings.Contains(dbDriverErr, "ORM") {
				t.Errorf("typeorm error should mention 'ORM', got: %s", dbDriverErr)
			}
		})
	}
}
