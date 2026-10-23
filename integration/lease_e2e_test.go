// Tests for: integration — what the live end-to-end run of dev-pool leases in a
// Mate found, through the full MCP server against a mock platform and a fake
// pool orchestrator (docs/spec-workflows.md §8 DP-2, DP-17…).

package integration_test

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// leaseE2EPoolMock is a project holding the pool devpool, its API_TOKEN set.
func leaseE2EPoolMock() *platform.Mock {
	return defaultMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: "devpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{ID: "env-t", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}})
}

// TestLease_Integration_RepoAsGiven_ClonedVerbatim: an acquire of `…/repo.git`
// clones that URL, never its canonical `…/repo`, which a plain git server
// answers with "repository not found" (DP-17).
func TestLease_Integration_RepoAsGiven_ClonedVerbatim(t *testing.T) {
	// Non-parallel: t.Chdir (the state dir) and a swap of
	// http.DefaultTransport (the orchestrator).
	t.Chdir(t.TempDir())
	routeLeaseOrchestrator(t)
	ssh := &stageLeaseSSH{}
	session, cleanup := setupTestServerWithDeploy(t, leaseE2EPoolMock(), defaultLogFetcher(), ssh)
	defer cleanup()

	const given = "https://git.example.com/team/repo.git"
	res := callAndGetResult(t, session, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": given, "branch": "feat"})
	if text := getTextContent(t, res); res.IsError {
		t.Fatalf("acquire: %s", text)
	}
	at, checkout := ssh.first("fetch -q --prune origin")
	if at < 0 {
		t.Fatal("the checkout did not run")
	}
	if !strings.Contains(checkout.command, "git remote add origin '"+given+"'") {
		t.Errorf("the checkout does not clone %s as given:\n%s", given, checkout.command)
	}
	if strings.Contains(checkout.command, "'https://git.example.com/team/repo'") {
		t.Errorf("the checkout clones the canonical form:\n%s", checkout.command)
	}
}
