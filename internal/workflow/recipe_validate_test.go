package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
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
			errs := ValidateRecipePlan(tt.plan, nil)
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
			p.Targets = []RecipeTarget{{Type: "php@8.4", Role: "app"}}
		}, "hostname is required"},
		{"target missing type", func(p *RecipePlan) {
			p.Targets = []RecipeTarget{{Hostname: "app", Role: "app"}}
		}, "type is required"},
		{"target missing role", func(p *RecipePlan) {
			p.Targets = []RecipeTarget{{Hostname: "app", Type: "php@8.4"}}
		}, "role is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan := validMinimalPlan()
			tt.modify(&plan)
			errs := ValidateRecipePlan(plan, nil)
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

			errs := ValidateRecipePlan(plan, nil)
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
			Category: "runtime",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "php-nginx@8.4", Status: "active"},
			},
		},
		{
			Name:     "php",
			Category: "runtime",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "php@8.4", Status: "active"},
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

			errs := ValidateRecipePlan(plan, liveTypes)
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
			Category: "runtime",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "php-nginx@8.4", Status: "active"},
			},
		},
		{
			Name:     "php",
			Category: "runtime",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "php@8.4", Status: "active"},
			},
		},
	}

	plan := validMinimalPlan()
	errs := ValidateRecipePlan(plan, liveTypes)
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
		{"laravel-hello-world", true},
		{"bun-hello-world", true},
		{"nestjs-showcase", true},
		{"django-rest-hello-world", true},
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
			errs := ValidateRecipePlan(plan, nil)

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
