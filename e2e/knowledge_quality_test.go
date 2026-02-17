//go:build e2e

// Tests for: e2e — knowledge quality verification against real Zerops API.
//
// Verifies that documented claims in services.md (ports, env vars, versions)
// match the real Zerops platform, catching documentation drift before it
// reaches LLMs via GetBriefing().
//
// Run: ZCP_API_KEY=... go test ./e2e/ -tags e2e -run TestE2E_KnowledgeQuality -v -timeout 120s

package e2e_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
)

// serviceClaim captures what services.md documents about a managed service type.
// Hardcoded to detect drift between docs and reality — parsing docs to generate
// claims would create a tautology (only verifies parsing, not accuracy).
type serviceClaim struct {
	typePattern        string   // base type (e.g. "postgresql")
	normalizedName     string   // H2 section name in services.md (e.g. "PostgreSQL")
	documentedVersions []string // versions docs mention (e.g. ["18","17","16","14"])
	expectedPorts      []int    // ports that MUST exist on any running instance
	haOnlyPorts        []int    // ports only present in HA mode
	expectedEnvKeys    []string // auto-injected env var KEY names
	forbiddenEnvKeys   []string // keys that must NOT exist
}

// serviceClaims is the hardcoded claims table derived from services.md.
// Covers all 14 entries from serviceNormalizer (sections.go:109-124).
var serviceClaims = []serviceClaim{
	{
		typePattern:        "postgresql",
		normalizedName:     "PostgreSQL",
		documentedVersions: []string{"18", "17", "16", "14"},
		expectedPorts:      []int{5432},
		haOnlyPorts:        []int{5433},
		expectedEnvKeys:    []string{"hostname", "port", "portTls", "user", "password", "connectionString", "connectionTlsString", "dbName", "superUser", "superUserPassword"},
	},
	{
		typePattern:        "mariadb",
		normalizedName:     "MariaDB",
		documentedVersions: []string{"10.6"},
		expectedPorts:      []int{3306},
		expectedEnvKeys:    []string{"hostname", "port", "projectId", "serviceId", "user", "password", "connectionString", "dbName"},
	},
	{
		typePattern:        "valkey",
		normalizedName:     "Valkey",
		documentedVersions: []string{"7.2"},
		expectedPorts:      []int{6379},
		haOnlyPorts:        []int{7000},
		expectedEnvKeys:    []string{"hostname", "port", "connectionString"},
		forbiddenEnvKeys:   []string{"user", "password"},
	},
	{
		typePattern:        "keydb",
		normalizedName:     "KeyDB",
		documentedVersions: []string{"6"},
		expectedPorts:      []int{6379},
		expectedEnvKeys:    []string{"hostname", "port", "connectionString"},
		forbiddenEnvKeys:   []string{"user", "password"},
	},
	{
		typePattern:        "elasticsearch",
		normalizedName:     "Elasticsearch",
		documentedVersions: []string{"9.2", "8.16"},
		expectedPorts:      []int{9200},
		expectedEnvKeys:    []string{"hostname", "port", "password"},
	},
	{
		typePattern:    "object-storage",
		normalizedName: "Object Storage",
		// No version, independent infra — ports not exposed on ServiceStack.
		expectedEnvKeys: []string{"apiUrl", "accessKeyId", "secretAccessKey", "bucketName"},
	},
	{
		typePattern:    "shared-storage",
		normalizedName: "Shared Storage",
		// No version, mount-based — no ports or env vars.
	},
	{
		typePattern:        "kafka",
		normalizedName:     "Kafka",
		documentedVersions: []string{"3.8"},
		expectedPorts:      []int{9092},
		expectedEnvKeys:    []string{"hostname", "port", "user", "password"},
	},
	{
		typePattern:        "nats",
		normalizedName:     "NATS",
		documentedVersions: []string{"2.12", "2.10"},
		expectedPorts:      []int{4222, 8222},
		expectedEnvKeys:    []string{"hostname", "user", "password", "connectionString"},
	},
	{
		typePattern:        "meilisearch",
		normalizedName:     "Meilisearch",
		documentedVersions: []string{"1.20", "1.10"},
		expectedPorts:      []int{7700},
		expectedEnvKeys:    []string{"hostname", "masterKey", "defaultSearchKey", "defaultAdminKey"},
	},
	{
		typePattern:        "clickhouse",
		normalizedName:     "ClickHouse",
		documentedVersions: []string{"25.3"},
		expectedPorts:      []int{9000, 8123, 9004, 9005},
		expectedEnvKeys:    []string{"hostname", "port", "portHttp", "portMysql", "portPostgresql", "portNative", "password", "superUserPassword", "dbName"},
	},
	{
		typePattern:        "qdrant",
		normalizedName:     "Qdrant",
		documentedVersions: []string{"1.12", "1.10"},
		expectedPorts:      []int{6333, 6334},
		expectedEnvKeys:    []string{"hostname", "port", "grpcPort", "apiKey", "readOnlyApiKey", "connectionString", "grpcConnectionString"},
	},
	{
		typePattern:        "typesense",
		normalizedName:     "Typesense",
		documentedVersions: []string{"27.1"},
		expectedPorts:      []int{8108},
		expectedEnvKeys:    []string{"hostname", "port", "apiKey"},
	},
	{
		typePattern:    "rabbitmq",
		normalizedName: "RabbitMQ",
		// rabbitmq@3.9 is DISABLED on the platform — no active versions.
		// Service card retained for migration guidance (deprecated → NATS).
		expectedPorts:   []int{5672, 15672},
		expectedEnvKeys: []string{"hostname", "port", "user", "password", "connectionString"},
	},
}

// normalizerKeys mirrors serviceNormalizer keys from sections.go:109-124.
// Used by ClaimsTableCoversNormalizers to detect if a new service type is
// added to the normalizer without a corresponding claims entry.
var normalizerKeys = []string{
	"postgresql", "mariadb", "valkey", "keydb", "elasticsearch",
	"object-storage", "shared-storage", "kafka", "nats", "meilisearch",
	"clickhouse", "qdrant", "typesense", "rabbitmq",
}

// knownBaseTypes contains all base type patterns recognized by the system.
// Merges serviceNormalizer + runtimeNormalizer keys.
var knownBaseTypes = map[string]bool{
	// Services (from serviceNormalizer)
	"postgresql": true, "mariadb": true, "valkey": true, "keydb": true,
	"elasticsearch": true, "object-storage": true, "objectstorage": true,
	"shared-storage": true, "kafka": true, "nats": true, "meilisearch": true,
	"clickhouse": true, "qdrant": true, "typesense": true, "rabbitmq": true,
	// Runtimes (from runtimeNormalizer)
	"php": true, "php-nginx": true, "php-apache": true, "nodejs": true,
	"bun": true, "deno": true, "python": true, "go": true, "java": true,
	"dotnet": true, "rust": true, "elixir": true, "gleam": true,
	"ruby": true, "static": true, "docker": true, "alpine": true, "ubuntu": true,
}

func TestE2E_KnowledgeQuality(t *testing.T) {
	h := newHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// --- Setup: load knowledge store ---
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("load embedded store: %v", err)
	}

	// --- Setup: fetch live platform data ---
	liveTypes, err := h.client.ListServiceStackTypes(ctx)
	if err != nil {
		t.Fatalf("list service stack types: %v", err)
	}
	if len(liveTypes) == 0 {
		t.Fatal("no service stack types returned from platform")
	}

	services, err := h.client.ListServices(ctx, h.projectID)
	if err != nil {
		t.Fatalf("list services: %v", err)
	}

	// Filter to user-visible services.
	var userServices []platform.ServiceStack
	for _, svc := range services {
		if !svc.IsSystem() {
			userServices = append(userServices, svc)
		}
	}

	// Build claims lookup.
	claimsByType := make(map[string]serviceClaim)
	for _, c := range serviceClaims {
		claimsByType[c.typePattern] = c
	}

	// Fetch env vars for managed services (services with a claim).
	type serviceWithEnv struct {
		svc  platform.ServiceStack
		envs []platform.EnvVar
	}
	byType := groupByBaseType(userServices)
	managedWithEnvs := make(map[string][]serviceWithEnv)

	for bt, svcs := range byType {
		if _, hasClaim := claimsByType[bt]; !hasClaim {
			continue
		}
		for _, svc := range svcs {
			envs, envErr := h.client.GetServiceEnv(ctx, svc.ID)
			if envErr != nil {
				t.Fatalf("get env for %s: %v", svc.Name, envErr)
			}
			managedWithEnvs[bt] = append(managedWithEnvs[bt], serviceWithEnv{svc: svc, envs: envs})
		}
	}

	activeVersions := activeVersionSet(liveTypes)

	// --- Phase 1: Knowledge Self-Consistency ---

	t.Run("Phase1", func(t *testing.T) {
		// ClaimsTableCoversNormalizers: every serviceNormalizer key has a claim.
		t.Run("ClaimsTableCoversNormalizers", func(t *testing.T) {
			for _, key := range normalizerKeys {
				if _, ok := claimsByType[key]; !ok {
					t.Errorf("serviceNormalizer key %q has no claims table entry", key)
				}
			}
			// Reverse: every claim has a normalizer key.
			normSet := make(map[string]bool, len(normalizerKeys))
			for _, k := range normalizerKeys {
				normSet[k] = true
			}
			for _, c := range serviceClaims {
				if !normSet[c.typePattern] {
					t.Errorf("claims entry %q not in normalizerKeys list", c.typePattern)
				}
			}
		})

		for _, claim := range serviceClaims {
			claim := claim

			// BriefingLoads: GetBriefing returns non-empty content containing the normalized name.
			t.Run("BriefingLoads/"+claim.typePattern, func(t *testing.T) {
				briefing, briefErr := store.GetBriefing("", []string{claim.typePattern}, liveTypes)
				if briefErr != nil {
					t.Fatalf("GetBriefing: %v", briefErr)
				}
				if briefing == "" {
					t.Error("GetBriefing returned empty content")
				}
				if !strings.Contains(briefing, claim.normalizedName) {
					t.Errorf("briefing does not contain normalized name %q", claim.normalizedName)
				}
			})

			// ServiceCardExists: H2 section exists in services.md.
			t.Run("ServiceCardExists/"+claim.typePattern, func(t *testing.T) {
				doc, docErr := store.Get("zerops://themes/services")
				if docErr != nil {
					t.Fatalf("get services.md: %v", docErr)
				}
				sections := doc.H2Sections()
				if _, ok := sections[claim.normalizedName]; !ok {
					var available []string
					for name := range sections {
						available = append(available, name)
					}
					t.Errorf("no H2 section %q in services.md (available: %v)", claim.normalizedName, available)
				}
			})

			// DocumentedVersionsActive: each documented version is ACTIVE in the platform catalog.
			for _, ver := range claim.documentedVersions {
				fullName := claim.typePattern + "@" + ver
				t.Run("DocumentedVersionsActive/"+fullName, func(t *testing.T) {
					if !activeVersions[fullName] {
						t.Errorf("documented version %s not ACTIVE in platform catalog", fullName)
					}
				})
			}
		}
	})

	// --- Phase 2: API Verification (against running services) ---

	t.Run("Phase2", func(t *testing.T) {
		// TypeFormat: every user-visible service has recognized base type.
		for _, svc := range userServices {
			svc := svc
			t.Run("TypeFormat/"+svc.Name, func(t *testing.T) {
				versionName := svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
				if versionName == "" {
					t.Error("empty ServiceStackTypeVersionName")
					return
				}
				base := kqBaseType(versionName)
				if !knownBaseTypes[base] {
					t.Logf("unrecognized base type %q (full: %s) — not in serviceNormalizer or runtimeNormalizer", base, versionName)
				}
			})
		}

		// Ports and EnvVars: check against claims for running managed services.
		for bt, svcsWithEnv := range managedWithEnvs {
			claim := claimsByType[bt]
			for _, swe := range svcsWithEnv {
				swe := swe

				if len(claim.expectedPorts) > 0 {
					t.Run("Ports/"+swe.svc.Name, func(t *testing.T) {
						portSet := make(map[int]bool)
						for _, p := range swe.svc.Ports {
							portSet[p.Port] = true
						}
						for _, ep := range claim.expectedPorts {
							if !portSet[ep] {
								t.Errorf("expected port %d not found (have: %s)", ep, formatPorts(swe.svc.Ports))
							}
						}
						if swe.svc.Mode == "HA" {
							for _, hp := range claim.haOnlyPorts {
								if !portSet[hp] {
									t.Errorf("expected HA-only port %d not found (have: %s)", hp, formatPorts(swe.svc.Ports))
								}
							}
						}
					})
				}

				if len(claim.expectedEnvKeys) > 0 || len(claim.forbiddenEnvKeys) > 0 {
					t.Run("EnvVars/"+swe.svc.Name, func(t *testing.T) {
						keys := kqEnvKeySet(swe.envs)
						for _, ek := range claim.expectedEnvKeys {
							if !keys[ek] {
								// Only log key names, never values.
								t.Errorf("expected env key %q not found (have %d keys)", ek, len(keys))
							}
						}
						for _, fk := range claim.forbiddenEnvKeys {
							if keys[fk] {
								t.Errorf("forbidden env key %q exists but should not", fk)
							}
						}
					})
				}
			}
		}

		// Report skipped service types (in claims but not running).
		for _, claim := range serviceClaims {
			if len(claim.expectedPorts) == 0 && len(claim.expectedEnvKeys) == 0 {
				continue
			}
			if _, running := managedWithEnvs[claim.typePattern]; !running {
				t.Run("Ports/"+claim.typePattern+"(not_running)", func(t *testing.T) {
					t.Skipf("no running %s service in project", claim.typePattern)
				})
				t.Run("EnvVars/"+claim.typePattern+"(not_running)", func(t *testing.T) {
					t.Skipf("no running %s service in project", claim.typePattern)
				})
			}
		}
	})

	// --- Phase 3: Recipe Version Drift ---

	t.Run("Phase3", func(t *testing.T) {
		recipes := store.ListRecipes()
		if len(recipes) == 0 {
			t.Fatal("no recipes found in store")
		}

		for _, name := range recipes {
			content, recipeErr := store.GetRecipe(name)
			if recipeErr != nil {
				t.Errorf("get recipe %s: %v", name, recipeErr)
				continue
			}

			versions := recipeVersionRefs(content)
			for _, v := range versions {
				t.Run("RecipeVersion/"+name+"/"+v, func(t *testing.T) {
					if !activeVersions[v] {
						t.Errorf("recipe %q references version %s which is not ACTIVE in platform catalog", name, v)
					}
				})
			}
		}
	})
}

// --- Helpers ---

// kqBaseType extracts the base type from a versioned string: "postgresql@16" → "postgresql".
func kqBaseType(versionName string) string {
	base, _, _ := strings.Cut(versionName, "@")
	return base
}

// groupByBaseType groups services by their base type pattern.
func groupByBaseType(svcs []platform.ServiceStack) map[string][]platform.ServiceStack {
	groups := make(map[string][]platform.ServiceStack)
	for _, svc := range svcs {
		bt := kqBaseType(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName)
		groups[bt] = append(groups[bt], svc)
	}
	return groups
}

// kqEnvKeySet builds a set of env var key names.
func kqEnvKeySet(envs []platform.EnvVar) map[string]bool {
	set := make(map[string]bool, len(envs))
	for _, e := range envs {
		set[e.Key] = true
	}
	return set
}

// activeVersionSet builds a set of all ACTIVE version names from the platform catalog.
func activeVersionSet(types []platform.ServiceStackType) map[string]bool {
	set := make(map[string]bool)
	for _, st := range types {
		for _, v := range st.Versions {
			if v.Status == "ACTIVE" {
				set[v.Name] = true
			}
		}
	}
	return set
}

// formatPorts returns a human-readable list of port numbers from a Port slice.
func formatPorts(ports []platform.Port) string {
	nums := make([]string, len(ports))
	for i, p := range ports {
		nums[i] = fmt.Sprintf("%d", p.Port)
	}
	return strings.Join(nums, ", ")
}

// recipeVersionRefs extracts type@version and base@version references from recipe content.
// Skips "latest" versions (no catalog entry) and special types without versions.
var recipeVersionRefRe = regexp.MustCompile(`(?:type|base):\s*(\S+@\S+)`)
var recipeVersionArrayRe = regexp.MustCompile(`(?:type|base):\s*\[([^\]]+)\]`)

// skipVersionPatterns lists version suffixes that aren't in the catalog.
var skipVersionPatterns = []string{"@latest"}

func recipeVersionRefs(content string) []string {
	seen := make(map[string]bool)
	var refs []string

	addRef := func(v string) {
		v = strings.TrimSpace(v)
		v = strings.TrimRight(v, ",]\"'")
		if v == "" || seen[v] || !strings.Contains(v, "@") {
			return
		}
		for _, skip := range skipVersionPatterns {
			if strings.HasSuffix(v, skip) {
				return
			}
		}
		// Skip special types without platform catalog entries.
		base, _, _ := strings.Cut(v, "@")
		if base == "static" || base == "object-storage" || base == "shared-storage" {
			return
		}
		seen[v] = true
		refs = append(refs, v)
	}

	// Array values: base: [php@8.3, nodejs@18]
	for _, match := range recipeVersionArrayRe.FindAllStringSubmatch(content, -1) {
		for _, part := range strings.Split(match[1], ",") {
			addRef(part)
		}
	}

	// Single values: type: nodejs@20, base: php@8.3
	for _, match := range recipeVersionRefRe.FindAllStringSubmatch(content, -1) {
		v := match[1]
		if strings.HasPrefix(v, "[") {
			continue // Array values handled above
		}
		addRef(v)
	}

	return refs
}
