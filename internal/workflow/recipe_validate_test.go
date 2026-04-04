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
			{Hostname: "app", Type: "php-nginx@8.4", Role: "app", Environments: []string{"0", "1", "2", "3", "4", "5"}},
		},
	}
}

func TestValidateRecipePlan_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		plan RecipePlan
	}{
		{"minimal plan", validMinimalPlan()},
		{"showcase plan", func() RecipePlan {
			p := validMinimalPlan()
			p.Tier = RecipeTierShowcase
			p.Slug = "laravel-showcase"
			p.Research.CacheLib = "predis"
			p.Research.SessionDriver = "redis"
			p.Research.QueueDriver = "redis"
			p.Targets = append(p.Targets, RecipeTarget{
				Hostname: "redis", Type: "keydb@6", Role: "cache",
				Environments: []string{"0", "1", "3", "4", "5"},
			})
			return p
		}()},
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
			p.Targets = []RecipeTarget{{Type: "php@8.4", Role: "app", Environments: []string{"0"}}}
		}, "hostname is required"},
		{"target missing type", func(p *RecipePlan) {
			p.Targets = []RecipeTarget{{Hostname: "app", Role: "app", Environments: []string{"0"}}}
		}, "type is required"},
		{"target missing role", func(p *RecipePlan) {
			p.Targets = []RecipeTarget{{Hostname: "app", Type: "php@8.4", Environments: []string{"0"}}}
		}, "role is required"},
		{"target missing environments", func(p *RecipePlan) {
			p.Targets = []RecipeTarget{{Hostname: "app", Type: "php@8.4", Role: "app"}}
		}, "environments is required"},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan := validMinimalPlan()
			plan.Tier = RecipeTierShowcase
			plan.Slug = "laravel-showcase"
			plan.Research.CacheLib = "predis"
			plan.Research.SessionDriver = "redis"
			plan.Research.QueueDriver = "redis"
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
		t.Fatalf("read zerops.yml schema: %v", err)
	}
	importData, err := os.ReadFile("../schema/testdata/import_yml_schema.json")
	if err != nil {
		t.Fatalf("read import.yaml schema: %v", err)
	}
	zy, err := schema.ParseZeropsYmlSchema(zeropsData)
	if err != nil {
		t.Fatalf("parse zerops.yml schema: %v", err)
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
			{Hostname: "app", Type: "php-nginx@8.4", Role: "app"},
			{Hostname: "db", Type: "postgresql@16", Role: "db"},
		}, false},
		{"invalid target type", []RecipeTarget{
			{Hostname: "app", Type: "foobar@1.0", Role: "app"},
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
