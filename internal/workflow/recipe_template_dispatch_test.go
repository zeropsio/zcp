package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestGenerateEnvImportYAML_DispatchCorrectness is the safety net for template
// dispatch. For every combination of (service type × envIndex × isWorker), the
// generated import.yaml must satisfy:
//
//   - runtime services: have zeropsSetup+buildFromGit, no priority, no mode
//   - utility services: have zeropsSetup+buildFromGit (utility URL), priority:10
//   - managed services: priority:10, mode (except object-storage), no buildFromGit
//   - env 0-1 runtime targets: rendered as dev+stage pair (appdev, appstage)
//   - env 2-5 runtime targets: rendered as bare hostname (app)
//   - workers: no enableSubdomainAccess
//   - apps + utilities: enableSubdomainAccess
//
// This test would have caught the v14 class of bug on day one.
func TestGenerateEnvImportYAML_DispatchCorrectness(t *testing.T) {
	t.Parallel()

	plan := &RecipePlan{
		Framework: "test",
		Slug:      "test-recipe",
		Research:  ResearchData{NeedsAppSecret: true, AppSecretKey: "APP_KEY"},
		Targets: []RecipeTarget{
			{Hostname: "app", Type: "php-nginx@8.4"},
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true},
			{Hostname: "db", Type: "postgresql@17"},
			{Hostname: "cache", Type: "valkey@7.2"},
			{Hostname: "search", Type: "meilisearch@1.20"},
			{Hostname: "store", Type: "object-storage"},
			{Hostname: "shared", Type: "shared-storage"},
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
			// Determine expected hostnames (env 0-1 runtime → dev+stage pair).
			hostnames := []string{target.Hostname}
			if topology.IsRuntimeType(target.Type) && envIndex <= 1 {
				hostnames = []string{target.Hostname + "dev", target.Hostname + "stage"}
			}

			for _, h := range hostnames {
				block := extractServiceBlock(out, h)
				if block == "" {
					t.Errorf("env %d, type=%s: service block %q not found in output", envIndex, target.Type, h)
					continue
				}
				assertBlockInvariants(t, envIndex, target, h, block)
			}
		}
	}
}

// assertBlockInvariants verifies every field-presence contract for one service
// block based on the target's type and IsWorker flag.
func assertBlockInvariants(t *testing.T, envIndex int, target RecipeTarget, hostname, block string) {
	t.Helper()

	isRuntime := topology.IsRuntimeType(target.Type)
	isUtil := topology.IsUtilityType(target.Type)
	isObjStorage := topology.IsObjectStorageType(target.Type)
	supportsMode := topology.ServiceSupportsMode(target.Type)
	supportsAutoscale := topology.ServiceSupportsAutoscaling(target.Type)

	hasSetup := strings.Contains(block, "zeropsSetup:")
	hasGit := strings.Contains(block, "buildFromGit:")
	hasPriority := strings.Contains(block, "priority: 10")
	hasMode := strings.Contains(block, "mode:")
	hasSubdomain := strings.Contains(block, "enableSubdomainAccess: true")
	hasAutoscaling := strings.Contains(block, "verticalAutoscaling:")
	hasObjStoreSize := strings.Contains(block, "objectStorageSize:")
	hasMinContainers := strings.Contains(block, "minContainers: 2")

	ctx := func(msg string) string {
		return formatContext(envIndex, target, hostname, msg, block)
	}

	// zeropsSetup+buildFromGit: runtime + utility only
	needsGit := isRuntime || isUtil
	if needsGit {
		if !hasSetup {
			t.Errorf("%s", ctx("MISSING zeropsSetup"))
		}
		if !hasGit {
			t.Errorf("%s", ctx("MISSING buildFromGit"))
		}
	} else {
		if hasSetup {
			t.Errorf("%s", ctx("UNEXPECTED zeropsSetup on managed service"))
		}
		if hasGit {
			t.Errorf("%s", ctx("UNEXPECTED buildFromGit on managed service"))
		}
	}

	// priority: 10: non-runtime (managed + utility)
	if isRuntime {
		if hasPriority {
			t.Errorf("%s", ctx("UNEXPECTED priority:10 on runtime service"))
		}
	} else {
		if !hasPriority {
			t.Errorf("%s", ctx("MISSING priority:10 on non-runtime service"))
		}
	}

	// mode: services that support it
	switch {
	case supportsMode:
		if !hasMode {
			t.Errorf("%s", ctx("MISSING mode on service that supports it"))
		}
		// env 5 = HA, others = NON_HA
		if envIndex == 5 && !strings.Contains(block, "mode: HA") {
			t.Errorf("%s", ctx("env 5 should have mode: HA"))
		}
		if envIndex < 5 && !strings.Contains(block, "mode: NON_HA") {
			t.Errorf("%s", ctx("envs 0-4 should have mode: NON_HA"))
		}
	case hasMode:
		t.Errorf("%s", ctx("UNEXPECTED mode on service that does not support it"))
	}

	// enableSubdomainAccess: runtime apps (non-workers) + utility services
	shouldHaveSubdomain := (isRuntime && !target.IsWorker) || isUtil
	if shouldHaveSubdomain && !hasSubdomain {
		t.Errorf("%s", ctx("MISSING enableSubdomainAccess"))
	}
	if !shouldHaveSubdomain && hasSubdomain {
		t.Errorf("%s", ctx("UNEXPECTED enableSubdomainAccess"))
	}

	// verticalAutoscaling: services that support it
	if supportsAutoscale && !hasAutoscaling {
		t.Errorf("%s", ctx("MISSING verticalAutoscaling"))
	}
	if !supportsAutoscale && hasAutoscaling {
		t.Errorf("%s", ctx("UNEXPECTED verticalAutoscaling"))
	}

	// objectStorageSize: only object-storage services
	if isObjStorage && !hasObjStoreSize {
		t.Errorf("%s", ctx("MISSING objectStorageSize"))
	}
	if !isObjStorage && hasObjStoreSize {
		t.Errorf("%s", ctx("UNEXPECTED objectStorageSize"))
	}

	// minContainers: 2 on runtime services in env 4-5 only
	if isRuntime && envIndex >= 4 && !hasMinContainers {
		t.Errorf("%s", ctx("MISSING minContainers: 2 for prod runtime"))
	}
	if (!isRuntime || envIndex < 4) && hasMinContainers {
		t.Errorf("%s", ctx("UNEXPECTED minContainers on non-prod-runtime service"))
	}
}

func formatContext(envIndex int, target RecipeTarget, hostname, msg, block string) string {
	return "env " + envTiers[envIndex].Folder +
		", hostname=" + hostname +
		", type=" + target.Type +
		", isWorker=" + boolStr(target.IsWorker) +
		": " + msg + "\nBlock:\n" + block
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// extractServiceBlock returns the YAML block for a given hostname.
func extractServiceBlock(yaml, hostname string) string {
	marker := "  - hostname: " + hostname + "\n"
	start := strings.Index(yaml, marker)
	if start == -1 {
		return ""
	}
	rest := yaml[start+len(marker):]
	nextSvc := strings.Index(rest, "  - hostname:")
	if nextSvc == -1 {
		return yaml[start:]
	}
	return yaml[start : start+len(marker)+nextSvc]
}

// TestServiceTypeKind_CoversAllManagedPrefixes guards against managed types
// being added without a matching serviceTypeKind entry. If someone adds a new
// type to managedServicePrefixes but forgets to update serviceTypeKind,
// comment generation would produce "unlabeled service" garbage.
func TestServiceTypeKind_CoversAllManagedPrefixes(t *testing.T) {
	t.Parallel()
	for _, prefix := range topology.ManagedServicePrefixes() {
		// Use the prefix directly as the test type (serviceTypeKind uses
		// the prefix portion only, so no version suffix needed).
		if kind := serviceTypeKind(prefix); kind == "" {
			t.Errorf("managed service prefix %q has no serviceTypeKind mapping — add it to recipe_service_types.go", prefix)
		}
	}
}

// TestServiceTypeKind_ExpectedValues pins the exact category labels used in
// human-readable comments. Changing these affects generated recipe comments.
func TestServiceTypeKind_ExpectedValues(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"postgresql@17":     "database",
		"mariadb@10.6":      "database",
		"clickhouse@25.3":   "database",
		"valkey@7.2":        "cache",
		"keydb@6":           "cache",
		"elasticsearch@9.2": "search engine",
		"meilisearch@1.20":  "search engine",
		"qdrant@1.12":       "search engine",
		"typesense@27.1":    "search engine",
		"object-storage":    "storage",
		"shared-storage":    "storage",
		"nats@2.12":         "messaging",
		"kafka@3.9":         "messaging",
		"rabbitmq@3":        "messaging",
		"mailpit":           "mail catcher",
		// runtime types have no kind label
		"php-nginx@8.4": "",
		"nodejs@22":     "",
		"go@1":          "",
	}
	for svcType, want := range cases {
		if got := serviceTypeKind(svcType); got != want {
			t.Errorf("serviceTypeKind(%q) = %q, want %q", svcType, got, want)
		}
	}
}
