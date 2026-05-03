package recipe

import (
	"reflect"
	"strings"
	"testing"
)

// TestParseConsumedServicesFromYaml — Run-21 R2-3.
//
// Parser walks run.envVariables, extracts `${X}` and `${X_*}`
// references, returns sorted unique hostnames that match a managed
// service in the plan.
func TestParseConsumedServicesFromYaml(t *testing.T) {
	t.Parallel()

	plan := &Plan{
		Services: []Service{
			{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18"},
			{Kind: ServiceKindManaged, Hostname: "cache", Type: "valkey@8"},
			{Kind: ServiceKindManaged, Hostname: "broker", Type: "nats@2.10"},
			{Kind: ServiceKindManaged, Hostname: "search", Type: "meilisearch@1"},
		},
	}

	tests := []struct {
		name string
		yaml string
		want []string
	}{
		{
			name: "two_services_referenced",
			yaml: `zerops:
  - setup: api
    run:
      envVariables:
        DATABASE_URL: postgresql://${db_user}:${db_password}@${db_hostname}:${db_port}/${db_dbName}
        REDIS_URL: redis://${cache_hostname}:${cache_port}
`,
			want: []string{"cache", "db"},
		},
		{
			name: "no_envvars",
			yaml: `zerops:
  - setup: api
    run:
      start: node index.js
`,
			want: nil,
		},
		{
			name: "ignores_unknown_token",
			yaml: `zerops:
  - setup: spa
    run:
      envVariables:
        VITE_API_URL: ${api_zeropsSubdomain}
        APP_ENV: production
`,
			// `api` is not in plan.Services (it's a runtime codebase).
			// Only managed-service hits count.
			want: nil,
		},
		{
			name: "deduplicates",
			yaml: `zerops:
  - setup: api
    run:
      envVariables:
        DB1: ${db_hostname}
        DB2: ${db_port}
        DB3: ${db_user}
`,
			want: []string{"db"},
		},
		{
			name: "bare_reference_no_underscore",
			yaml: `zerops:
  - setup: api
    run:
      envVariables:
        SEARCH: ${search}
`,
			want: []string{"search"},
		},
		{
			name: "unparseable_yaml_returns_nil",
			yaml: ":::not yaml:::",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseConsumedServicesFromYaml(tt.yaml, plan)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseConsumedServicesFromYaml = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRecipeContext_FiltersServicesByCodebaseConsumption — Run-21 R2-3.
//
// SPA codebase with empty (non-nil) ConsumesServices gets the Managed
// services block omitted from its dispatched prompt context.
func TestRecipeContext_FiltersServicesByCodebaseConsumption(t *testing.T) {
	t.Parallel()

	plan := &Plan{
		Slug:      "showcase",
		Framework: "node-svelte",
		Tier:      "showcase",
		Codebases: []Codebase{
			{
				Hostname:         "spa",
				Role:             RoleFrontend,
				BaseRuntime:      "nodejs@22",
				SourceRoot:       "/var/www/spadev",
				ConsumesServices: []string{}, // explicit empty: SPA consumes nothing managed
			},
			{
				Hostname:         "api",
				Role:             RoleAPI,
				BaseRuntime:      "nodejs@22",
				SourceRoot:       "/var/www/apidev",
				ConsumesServices: []string{"db", "cache"},
			},
		},
		Services: []Service{
			{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18"},
			{Kind: ServiceKindManaged, Hostname: "cache", Type: "valkey@8"},
			{Kind: ServiceKindManaged, Hostname: "broker", Type: "nats@2.10"},
		},
	}

	// SPA brief — Managed services block should be absent.
	in := RecipeInput{BriefKind: "scaffold", Codebase: "spa"}
	prompt, err := buildSubagentPrompt(plan, nil, in)
	if err != nil {
		t.Fatalf("buildSubagentPrompt spa: %v", err)
	}
	if strings.Contains(prompt, "### Managed services") {
		t.Errorf("spa prompt should omit Managed services block when ConsumesServices is empty;\n%s", prompt)
	}

	// API brief — Managed services block lists db + cache, excludes broker.
	in.Codebase = "api"
	prompt, err = buildSubagentPrompt(plan, nil, in)
	if err != nil {
		t.Fatalf("buildSubagentPrompt api: %v", err)
	}
	if !strings.Contains(prompt, "### Managed services") {
		t.Errorf("api prompt missing Managed services block")
	}
	if !strings.Contains(prompt, "`db`") || !strings.Contains(prompt, "`cache`") {
		t.Errorf("api prompt missing db/cache listings")
	}
	if strings.Contains(prompt, "`broker`") {
		t.Errorf("api prompt leaks broker (not in ConsumesServices)")
	}
}

// TestRecipeContext_NilConsumesServices_FallsBackToAll — back-compat.
//
// Codebase with nil (not zero) ConsumesServices preserves pre-R2-3
// behavior of dumping the full Services block — applies to existing
// plans loaded before the field existed and to sim paths that skip
// the parse step.
func TestRecipeContext_NilConsumesServices_FallsBackToAll(t *testing.T) {
	t.Parallel()

	plan := &Plan{
		Slug: "x",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22", SourceRoot: "/var/www/apidev"},
		},
		Services: []Service{
			{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18"},
			{Kind: ServiceKindManaged, Hostname: "cache", Type: "valkey@8"},
		},
	}
	in := RecipeInput{BriefKind: "scaffold", Codebase: "api"}
	prompt, err := buildSubagentPrompt(plan, nil, in)
	if err != nil {
		t.Fatalf("buildSubagentPrompt: %v", err)
	}
	if !strings.Contains(prompt, "`db`") || !strings.Contains(prompt, "`cache`") {
		t.Errorf("nil-ConsumesServices fallback should list all managed services; prompt:\n%s", prompt)
	}
}
