// Tests for: where the agent runs git for a repository of this Mate's HQ —
// the dev service of its pair, a lease of its dev pool, or the checkout the
// agent fetches it into (tools/hq_repo_host.go).
package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestRepoDevHost_RecipeNamedRepository_FindsItsPair: a recipe can name a
// pair's repository other than its dev hostname, so the repository's dev
// service is found by the record's repository, never by the name — and its
// git runs there through `zcp git <host>`, never on the mount or in the
// agent's own shell.
func TestRepoDevHost_RecipeNamedRepository_FindsItsPair(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeWiredPair(t, stateDir, "shopdev", "storefront")
	writeWiredPair(t, stateDir, "apidev", "api")

	route := repoDevHost(stateDir, labApp, "storefront")

	if got, want := route.git("fetch origin", "checkout main"), "`zcp git shopdev fetch origin && zcp git shopdev checkout main`"; got != want {
		t.Errorf("git = %s, want %s", got, want)
	}
	if got := route.where(); got != "in its dev service shopdev" {
		t.Errorf("where = %q, want its dev service shopdev", got)
	}
	if got, want := route.take("fix-login"), "`zcp git shopdev fetch origin fix-login && zcp git shopdev checkout fix-login`"; got != want {
		t.Errorf("take = %s, want %s", got, want)
	}
	if other := repoDevHost(stateDir, "another-app", "storefront"); strings.Contains(other.git("status"), "shopdev") {
		t.Errorf("a repository of another application found shopdev: %s", other.git("status"))
	}
}

// TestRepoDevHost_PoolPair_NamesLeaseRoute: a repository only pairs keyed by
// dev pools are wired to has no checkout of its own — its pool's hostname
// reaches a random container — so its git runs in a lease, over ssh to the
// lease's alias, and a branch is taken by a lease acquired on it; a lease the
// words know is named.
func TestRepoDevHost_PoolPair_NamesLeaseRoute(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "webpool", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "webstage"}},
		BootstrapSession: "test", BootstrappedAt: "2026-10-02", GitPushState: topology.GitPushConfigured,
		HQ: &workflow.HQRepoRef{AppID: labApp, Repo: "web"}, DevPool: &workflow.DevPoolRef{ServiceID: "svc-webpool"},
	}); err != nil {
		t.Fatal(err)
	}

	route := repoDevHost(stateDir, labApp, "web")

	if got, want := route.git("push origin fix-login"), "`ssh <alias> \"cd /var/www && git push origin fix-login\"`"; got != want {
		t.Errorf("git = %s, want %s", got, want)
	}
	if got := route.where(); got != "in a lease of the dev pool webpool" {
		t.Errorf("where = %q, want a lease of webpool", got)
	}
	if got, want := route.take("fix-login"), "`zerops_lease action=\"acquire\" pool=\"webpool\" branch=\"fix-login\"`"; got != want {
		t.Errorf("take = %s, want %s", got, want)
	}
	if strings.Contains(route.git("status"), "zcp git webpool") {
		t.Errorf("a pool's hostname is no checkout, yet git runs there: %s", route.git("status"))
	}
	leased := route.inLease("webpool-3")
	if got, want := leased.git("push origin fix-login"), "`ssh webpool-3 \"cd /var/www && git push origin fix-login\"`"; got != want {
		t.Errorf("in its lease, git = %s, want %s", got, want)
	}
	if got := leased.where(); got != "in its lease webpool-3" {
		t.Errorf("in its lease, where = %q", got)
	}
}

// TestRepoDevHost_NoPairHere_NamesTheFetchCheckout: a repository no pair of
// this project is wired to — a sibling Mate's — has no dev service zcp can
// name, so its git runs in the dev service whose checkout the agent fetches
// it into, still through `zcp git`.
func TestRepoDevHost_NoPairHere_NamesTheFetchCheckout(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeWiredPair(t, stateDir, "apidev", "api")

	route := repoDevHost(stateDir, labApp, "storefront")

	if got, want := route.git("fetch origin"), "`zcp git <host> fetch origin`"; got != want {
		t.Errorf("git = %s, want %s", got, want)
	}
	if got := route.where(); got != "in the dev service whose checkout you fetch it into" {
		t.Errorf("where = %q, want the fetch checkout", got)
	}
}
