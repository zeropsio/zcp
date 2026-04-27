//go:build e2e

// Tests for: e2e — zerops_discover meta-field shape against the live
// project. Verifies isInfrastructure / managedByZcp / mountPath are
// populated correctly for the services in the active project.
//
// Earlier this file also covered server.BuildInstructions output (service
// listings, container vs local mode, adoption labels). That responsibility
// moved out of BuildInstructions into the workflow engine + atom corpus
// when BuildInstructions was reduced to AdoptionNote/StateHint
// composition (see internal/server/instructions_test.go for the current
// unit-level coverage). The obsolete tests were deleted instead of
// refactored — the strings they asserted on no longer come from
// BuildInstructions, so refactoring would have produced a hollow
// duplicate of the unit tests.
//
// Prerequisites:
//   - ZCP_API_KEY set (or zcli logged in)
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_Discover -v -timeout 120s

package e2e_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
)

func TestE2E_Discover_MetaFields(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	session := newSession(t, h.srv)

	// Call discover without filter.
	text := session.mustCallSuccess("zerops_discover", nil)

	var result ops.DiscoverResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("unmarshal discover: %v", err)
	}

	if len(result.Services) == 0 {
		t.Skip("no services in project")
	}

	// Every service should have isInfrastructure set correctly.
	for _, svc := range result.Services {
		expectInfra := isInfraServiceType(svc.Type)
		if svc.IsInfrastructure != expectInfra {
			t.Errorf("service %q: isInfrastructure=%v, want %v (type=%s)",
				svc.Hostname, svc.IsInfrastructure, expectInfra, svc.Type)
		}
	}

	// Without state dir, managedByZcp should be false for all.
	for _, svc := range result.Services {
		if svc.ManagedByZCP {
			t.Errorf("service %q: managedByZcp=true but no state dir provided",
				svc.Hostname)
		}
	}

	// Check mount paths — if running on container with mounts.
	for _, svc := range result.Services {
		mountPath := "/var/www/" + svc.Hostname
		if info, err := os.Stat(mountPath); err == nil && info.IsDir() {
			if svc.MountPath == "" {
				t.Errorf("service %q mounted at %s but mountPath is empty",
					svc.Hostname, mountPath)
			}
		}
	}
}

// isInfraServiceType checks if a type string is infrastructure (managed service).
// Duplicates the logic check against known prefixes for test independence.
func isInfraServiceType(typeName string) bool {
	lower := strings.ToLower(typeName)
	for _, prefix := range []string{
		"postgresql", "mariadb", "valkey", "keydb",
		"elasticsearch", "meilisearch", "rabbitmq", "kafka",
		"nats", "clickhouse", "qdrant", "typesense",
		"object-storage", "shared-storage",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}
