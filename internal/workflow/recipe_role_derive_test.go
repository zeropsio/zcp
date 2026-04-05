package workflow

import (
	"strings"
	"testing"
)

// TestDeriveRole covers every service type the agent can reasonably submit.
// This test is the safety net for the v14 class of bug: if a type slips past
// DeriveRole into the runtime fallback when it should map to a managed role,
// or if a managed type has no entry in managedRolePrefixes, the template layer
// silently drops zeropsSetup/buildFromGit and the finalize check fails with a
// cryptic "missing zeropsSetup and buildFromGit" error.
func TestDeriveRole(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		svcType  string
		isWorker bool
		want     string
		wantErr  bool
	}{
		// Runtime → app (default)
		{"php-nginx → app", "php-nginx@8.4", false, RecipeRoleApp, false},
		{"php-apache → app", "php-apache@8.5", false, RecipeRoleApp, false},
		{"nodejs → app", "nodejs@22", false, RecipeRoleApp, false},
		{"bun → app", "bun@1.2", false, RecipeRoleApp, false},
		{"deno → app", "deno@2", false, RecipeRoleApp, false},
		{"python → app", "python@3.12", false, RecipeRoleApp, false},
		{"go → app", "go@1", false, RecipeRoleApp, false},
		{"rust → app", "rust@stable", false, RecipeRoleApp, false},
		{"ruby → app", "ruby@3.3", false, RecipeRoleApp, false},
		{"java → app", "java@21", false, RecipeRoleApp, false},
		{"dotnet → app", "dotnet@9", false, RecipeRoleApp, false},
		{"elixir → app", "elixir@1.16", false, RecipeRoleApp, false},
		{"gleam → app", "gleam@1.5", false, RecipeRoleApp, false},
		{"nginx → app", "nginx@1.22", false, RecipeRoleApp, false},
		{"static → app", "static", false, RecipeRoleApp, false},
		{"docker → app", "docker@26.1", false, RecipeRoleApp, false},
		{"ubuntu → app", "ubuntu@24.04", false, RecipeRoleApp, false},
		{"alpine → app", "alpine@3.22", false, RecipeRoleApp, false},

		// Runtime with isWorker=true → worker
		{"php-nginx worker", "php-nginx@8.4", true, RecipeRoleWorker, false},
		{"nodejs worker", "nodejs@22", true, RecipeRoleWorker, false},
		{"go worker", "go@1", true, RecipeRoleWorker, false},
		{"python worker", "python@3.12", true, RecipeRoleWorker, false},

		// Managed DB
		{"postgresql → db", "postgresql@17", false, RecipeRoleDB, false},
		{"postgresql v18 → db", "postgresql@18", false, RecipeRoleDB, false},
		{"mariadb → db", "mariadb@10.6", false, RecipeRoleDB, false},
		{"clickhouse → db", "clickhouse@25.3", false, RecipeRoleDB, false},

		// Managed cache
		{"valkey → cache", "valkey@7.2", false, RecipeRoleCache, false},
		{"keydb → cache", "keydb@6", false, RecipeRoleCache, false},

		// Managed search
		{"elasticsearch → search", "elasticsearch@9.2", false, RecipeRoleSearch, false},
		{"meilisearch → search", "meilisearch@1.20", false, RecipeRoleSearch, false},
		{"qdrant → search", "qdrant@1.12", false, RecipeRoleSearch, false},
		{"typesense → search", "typesense@27.1", false, RecipeRoleSearch, false},

		// Managed storage
		{"object-storage → storage", "object-storage", false, RecipeRoleStorage, false},
		{"shared-storage → storage", "shared-storage", false, RecipeRoleStorage, false},

		// Messaging → mail
		{"nats → mail", "nats@2.12", false, RecipeRoleMail, false},
		{"kafka → mail", "kafka@3.9", false, RecipeRoleMail, false},
		{"rabbitmq → mail", "rabbitmq@3", false, RecipeRoleMail, false},

		// Utility (ubuntu-based, looks like runtime, classified by IsUtilityType)
		{"mailpit → mail", "mailpit", false, RecipeRoleMail, false},
		{"mailpit versioned → mail", "mailpit@1", false, RecipeRoleMail, false},

		// isWorker is ignored for non-runtime types
		{"db with isWorker=true → still db", "postgresql@17", true, RecipeRoleDB, false},
		{"cache with isWorker=true → still cache", "valkey@7.2", true, RecipeRoleCache, false},
		{"mail with isWorker=true → still mail", "nats@2.12", true, RecipeRoleMail, false},
		{"storage with isWorker=true → still storage", "object-storage", true, RecipeRoleStorage, false},

		// Case-insensitivity — agents may submit weird casings
		{"uppercase type", "POSTGRESQL@17", false, RecipeRoleDB, false},
		{"mixed case", "Valkey@7.2", false, RecipeRoleCache, false},

		// Error cases
		{"empty type", "", false, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := DeriveRole(tt.svcType, tt.isWorker)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got role=%q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("DeriveRole(%q, %v) = %q, want %q", tt.svcType, tt.isWorker, got, tt.want)
			}
		})
	}
}

// TestDeriveRole_ManagedTypesCoveredByPrefixes ensures every prefix in
// managedServicePrefixes has a role mapping in managedRolePrefixes. If a new
// managed type is added to the platform without updating managedRolePrefixes,
// DeriveRole returns an error — this test surfaces that mismatch immediately
// instead of letting it break recipe generation at runtime.
func TestDeriveRole_ManagedTypesCoveredByPrefixes(t *testing.T) {
	t.Parallel()
	// Flatten the role map into a prefix set for lookup.
	covered := map[string]bool{}
	for _, prefixes := range managedRolePrefixes {
		for p := range prefixes {
			covered[p] = true
		}
	}
	for _, prefix := range managedServicePrefixes {
		if !covered[prefix] {
			t.Errorf("managed service prefix %q has no role mapping in managedRolePrefixes — add it to db/cache/search/storage/mail", prefix)
		}
	}
}

// TestDeriveRole_RoleToSetupNameMapping ensures every derived role maps to a
// valid recipeSetupName output. This catches the inverse bug: a role is added
// but recipeSetupName doesn't know about it and returns the wrong setup.
func TestDeriveRole_RoleToSetupNameMapping(t *testing.T) {
	t.Parallel()
	// All known roles should produce a setup name; only app/worker distinguish.
	cases := []struct {
		role     string
		wantProd string
	}{
		{RecipeRoleApp, "prod"},
		{RecipeRoleWorker, "worker"},
		{RecipeRoleDB, "prod"},
		{RecipeRoleCache, "prod"},
		{RecipeRoleSearch, "prod"},
		{RecipeRoleStorage, "prod"},
		{RecipeRoleMail, "prod"},
	}
	for _, c := range cases {
		if got := recipeSetupName(c.role, false); got != c.wantProd {
			t.Errorf("recipeSetupName(%q, false) = %q, want %q", c.role, got, c.wantProd)
		}
		if got := recipeSetupName(c.role, true); got != "dev" {
			t.Errorf("recipeSetupName(%q, true) = %q, want dev", c.role, got)
		}
	}
}

// TestGenerateEnvImportYAML_EveryTargetHasRequiredFields is the integration
// test that would have caught the v14 bug: for every combination of target +
// environment tier, the generated import.yaml MUST contain buildFromGit and
// the appropriate zeropsSetup (or buildFromGit for utility services). If the
// role derivation falls through to an unexpected branch, one of these asserts
// trips and tells us exactly which (type, envIndex) combo is broken.
func TestGenerateEnvImportYAML_EveryTargetHasRequiredFields(t *testing.T) {
	t.Parallel()

	plan := &RecipePlan{
		Framework: "test",
		Tier:      RecipeTierShowcase,
		Slug:      "test-recipe",
		Research:  ResearchData{NeedsAppSecret: true, AppSecretKey: "APP_KEY"},
		Targets: []RecipeTarget{
			{Hostname: "app", Type: "php-nginx@8.4"},
			{Hostname: "worker", Type: "php-nginx@8.4", IsWorker: true},
			{Hostname: "db", Type: "postgresql@17"},
			{Hostname: "cache", Type: "valkey@7.2"},
			{Hostname: "search", Type: "meilisearch@1.20"},
			{Hostname: "storage", Type: "object-storage"},
			{Hostname: "mail", Type: "mailpit"},
			{Hostname: "queue", Type: "nats@2.12"},
		},
	}

	for envIndex := 0; envIndex < EnvTierCount(); envIndex++ {
		out := GenerateEnvImportYAML(plan, envIndex)
		if out == "" {
			t.Fatalf("env %d: empty output", envIndex)
		}
		for _, target := range plan.Targets {
			role := target.Role()
			// The finalize check requires zeropsSetup+buildFromGit ONLY on
			// runtime services (app/worker) and utility services (mailpit).
			// Managed services (db, cache, search, storage, nats/kafka) are
			// platform-owned — buildFromGit is inert on them.
			needsGit := !IsDataService(role) || IsUtilityType(target.Type)

			hostnames := []string{target.Hostname}
			if IsRuntimeService(role) && !IsUtilityType(target.Type) && envIndex <= 1 {
				hostnames = []string{target.Hostname + "dev", target.Hostname + "stage"}
			}
			for _, h := range hostnames {
				block := extractServiceBlock(out, h)
				if block == "" {
					t.Errorf("env %d: service block %q not found in output (type=%s, role=%s)", envIndex, h, target.Type, role)
					continue
				}
				hasSetup := strings.Contains(block, "zeropsSetup:")
				hasGit := strings.Contains(block, "buildFromGit:")
				if needsGit {
					if !hasSetup {
						t.Errorf("env %d, service %q (type=%s, role=%s, needsGit=true): MISSING zeropsSetup in block:\n%s", envIndex, h, target.Type, role, block)
					}
					if !hasGit {
						t.Errorf("env %d, service %q (type=%s, role=%s, needsGit=true): MISSING buildFromGit in block:\n%s", envIndex, h, target.Type, role, block)
					}
				} else {
					// Managed services should NOT have zeropsSetup/buildFromGit —
					// they are inert and just add noise.
					if hasSetup {
						t.Errorf("env %d, service %q (type=%s, role=%s, needsGit=false): UNEXPECTED zeropsSetup in block:\n%s", envIndex, h, target.Type, role, block)
					}
					if hasGit {
						t.Errorf("env %d, service %q (type=%s, role=%s, needsGit=false): UNEXPECTED buildFromGit in block:\n%s", envIndex, h, target.Type, role, block)
					}
				}
			}
		}
	}
}

// extractServiceBlock returns the YAML block for a given hostname from an
// import.yaml output (up to but not including the next service or top-level
// key). Best-effort for test assertions only.
func extractServiceBlock(yaml, hostname string) string {
	marker := "  - hostname: " + hostname + "\n"
	start := strings.Index(yaml, marker)
	if start == -1 {
		return ""
	}
	rest := yaml[start+len(marker):]
	// Find the next "  - hostname:" or end of file.
	nextSvc := strings.Index(rest, "  - hostname:")
	if nextSvc == -1 {
		return yaml[start:]
	}
	return yaml[start : start+len(marker)+nextSvc]
}
