//go:build e2e

// Tests for: e2e — instruction delivery, mount detection, service classification.
//
// Verifies that BuildInstructions produces correct output for the current project:
// - Services are listed with correct labels (adoption, mount paths)
// - Discovery returns managedByZcp and mountPath fields
// - Router offerings reflect actual service state
//
// When run on a container (zcpx), also verifies SSHFS mount detection.
//
// Prerequisites:
//   - ZCP_API_KEY set (or zcli logged in)
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_Instructions -v -timeout 120s

package e2e_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
)

func TestE2E_Instructions_ServiceListing(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get instructions in local mode (default harness).
	inst := server.BuildInstructions(ctx, h.client, h.projectID, runtime.Info{}, "")

	// Must contain base + routing.
	if !strings.Contains(inst, "ZCP manages") {
		t.Error("missing base instructions")
	}
	if !strings.Contains(inst, "workflow sessions") {
		t.Error("missing routing instructions")
	}

	// Must list at least one service from the project.
	services, err := h.client.ListServices(ctx, h.projectID)
	if err != nil {
		t.Fatalf("list services: %v", err)
	}
	var runtimeCount int
	for _, svc := range services {
		if svc.IsSystem() {
			continue
		}
		if !strings.Contains(inst, svc.Name) {
			t.Errorf("instructions should list service %q", svc.Name)
		}
		// Check if it's a runtime service without meta → should have adoption label.
		typeName := svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
		if !isInfraServiceType(typeName) {
			runtimeCount++
		}
	}

	// If there are runtime services and no state dir, all should need adoption.
	if runtimeCount > 0 {
		if !strings.Contains(inst, "needs ZCP adoption") {
			t.Error("runtime services without state dir should show adoption label")
		}
		if !strings.Contains(inst, "Runtime services needing adoption") {
			t.Error("should have adoption section in orientation")
		}
	}

	// Router should always produce offerings.
	if !strings.Contains(inst, "Available workflows") {
		t.Error("should have router offerings")
	}
}

func TestE2E_Instructions_ContainerMode(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Simulate container mode.
	rtInfo := runtime.Info{InContainer: true, ServiceName: "zcpx"}
	inst := server.BuildInstructions(ctx, h.client, h.projectID, rtInfo, "")

	// Must have container environment section.
	if !strings.Contains(inst, "Live service filesystems") {
		t.Error("container mode should mention live service filesystems")
	}
	if !strings.Contains(inst, "workflow session first") {
		t.Error("container mode should have workflow directive near mount description")
	}

	// Must NOT have local environment section.
	if strings.Contains(inst, "zcli push") {
		t.Error("container mode should not mention zcli push (that's local mode)")
	}

	// Check mount detection — if /var/www/{hostname} exists, mount path should appear.
	services, err := h.client.ListServices(ctx, h.projectID)
	if err != nil {
		t.Fatalf("list services: %v", err)
	}
	for _, svc := range services {
		if svc.IsSystem() {
			continue
		}
		mountPath := "/var/www/" + svc.Name
		if info, statErr := os.Stat(mountPath); statErr == nil && info.IsDir() {
			if !strings.Contains(inst, "mounted at "+mountPath) {
				t.Errorf("service %q is mounted at %s but instructions don't show mount path", svc.Name, mountPath)
			}
		}
	}
}

func TestE2E_Instructions_LocalMode(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	inst := server.BuildInstructions(ctx, h.client, h.projectID, runtime.Info{}, "")

	// Must have local environment section.
	if !strings.Contains(inst, "zcli push") {
		t.Error("local mode should mention zcli push")
	}
	if !strings.Contains(inst, "workflow session first") {
		t.Error("local mode should have workflow directive")
	}

	// Must NOT have container environment.
	if strings.Contains(inst, "Live service filesystems") {
		t.Error("local mode should not mention SSHFS mounts")
	}
}

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

func TestE2E_Instructions_AdoptionWording(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	inst := server.BuildInstructions(ctx, h.client, h.projectID, runtime.Info{}, "")

	// Routing instructions should mention adoption and deploy (which covers fix/investigate).
	if !strings.Contains(inst, "deploy") {
		t.Error("routing should mention deploy workflow")
	}
	if !strings.Contains(inst, "Adopt existing") {
		t.Error("routing should mention adopting existing services")
	}
	if !strings.Contains(inst, "platform knowledge") {
		t.Error("routing should mention platform knowledge injection")
	}

	// Should NOT contain old ProjectState references.
	for _, old := range []string{"FRESH", "CONFORMANT", "NON_CONFORMANT", "Project state:"} {
		if strings.Contains(inst, old) {
			t.Errorf("instructions should not contain old ProjectState reference %q", old)
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

// newInstructionHarness creates a harness with custom runtime info.
// This allows testing container vs local mode instructions.
func newInstructionHarness(t *testing.T, rtInfo runtime.Info) (*e2eHarness, *auth.Info, platform.Client) {
	t.Helper()

	token := os.Getenv("ZCP_API_KEY")
	if token == "" {
		t.Skip("ZCP_API_KEY not set — skipping E2E test")
	}

	apiHost := os.Getenv("ZCP_API_HOST")
	if apiHost == "" {
		apiHost = "api.app-prg1.zerops.io"
	}

	region := os.Getenv("ZCP_REGION")
	if region == "" {
		region = "prg1"
	}

	client, err := platform.NewZeropsClient(token, apiHost)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	authInfo, err := auth.Resolve(ctx, client)
	if err != nil {
		t.Fatalf("auth resolve: %v", err)
	}
	authInfo.Region = region

	store, _ := knowledge.GetEmbeddedStore()
	logFetcher := platform.NewLogFetcher()
	srv := server.New(context.Background(), client, authInfo, store, logFetcher, nil, nil, rtInfo)

	return &e2eHarness{
		t:         t,
		client:    client,
		projectID: authInfo.ProjectID,
		authInfo:  authInfo,
		srv:       srv,
	}, authInfo, client
}
