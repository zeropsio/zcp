//go:build e2e

// Live verifier for the prod-ops subdomain URL (root-caused from p4.txt: prod-ops
// status never surfaced subdomainUrl). It proves the URL prodOpsStatus composes
// — from GetProject's subdomain-host PREFIX + the apiHost-derived region domain
// (NO env values, P-LP-5) — equals the REAL zeropsSubdomain the platform serves.
//
// Why it exists: v9.116.1 shipped a prod-ops subdomain fix verified only against
// a mock whose GetProject returned a full dotted host; live, GetProject returns a
// PREFIX ("21c9"), so the URL came out empty. This test pins the reconstruction
// against live ground truth so that class of mock-vs-reality gap can't recur.
//
// Run (needs a live launched prod project with the window still open):
//   ZCP_E2E_LAUNCH_KEY=<launch/integration token with ADMIN on the prod project> \
//   ZCP_E2E_PROD_PROJECT=<prod projectID> \
//   ZCP_E2E_PROD_SERVICE=<prod runtime serviceID with subdomain enabled> \
//   go test ./e2e/ -tags e2e -run TestE2E_ProdSubdomainURL -v -timeout 120s

package e2e_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestE2E_ProdSubdomainURL(t *testing.T) {
	launchKey := os.Getenv("ZCP_E2E_LAUNCH_KEY")
	prodProject := os.Getenv("ZCP_E2E_PROD_PROJECT")
	prodService := os.Getenv("ZCP_E2E_PROD_SERVICE")
	if launchKey == "" || prodProject == "" || prodService == "" {
		t.Skip("ZCP_E2E_LAUNCH_KEY / ZCP_E2E_PROD_PROJECT / ZCP_E2E_PROD_SERVICE not set — skipping live prod-subdomain verification")
	}
	apiHost := os.Getenv("ZCP_API_HOST")
	if apiHost == "" {
		apiHost = "api.app-prg1.zerops.io"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	admin, err := platform.NewProjectAdminClient(launchKey, apiHost)
	if err != nil {
		t.Fatalf("NewProjectAdminClient: %v", err)
	}
	defer admin.Close()

	// Reconstruct the URL the way prodOpsStatus does — GetProject prefix + Ports
	// + apiHost-derived domain, NO env values.
	proj, err := admin.GetProject(ctx, prodProject)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	services, err := admin.ListServices(ctx, prodProject)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	var svc *platform.ServiceStack
	for i := range services {
		if services[i].ID == prodService {
			svc = &services[i]
		}
	}
	if svc == nil {
		t.Fatalf("service %s not found in prod project %s", prodService, prodProject)
	}
	if !svc.SubdomainAccess {
		t.Fatalf("service %s has SubdomainAccess=false — enable it + wait for the balancer before running", svc.Name)
	}
	port, ok := ops.PreferredHTTPPort(svc.Ports)
	if !ok {
		t.Fatalf("no HTTP port on %s: %+v", svc.Name, svc.Ports)
	}
	domain := ops.SubdomainDomainFromAPIHost(apiHost)
	reconstructed := ops.BuildSubdomainURL(svc.Name, proj.SubdomainHost+"."+domain, port.Port)
	t.Logf("reconstructed = %q (prefix=%q domain=%q port=%d)", reconstructed, proj.SubdomainHost, domain, port.Port)

	// Ground truth: the real zeropsSubdomain env value.
	full, err := platform.NewZeropsClient(launchKey, apiHost)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}
	envs, err := full.GetServiceEnv(ctx, prodService)
	if err != nil {
		t.Fatalf("GetServiceEnv: %v", err)
	}
	var real string
	for _, e := range envs {
		if e.Key == "zeropsSubdomain" {
			real = e.Content
		}
	}
	if real == "" {
		t.Fatalf("no zeropsSubdomain env on %s — subdomain not yet propagated?", svc.Name)
	}
	if reconstructed != real {
		t.Errorf("reconstructed URL != real zeropsSubdomain:\n  reconstructed: %q\n  real:          %q", reconstructed, real)
	}
}
