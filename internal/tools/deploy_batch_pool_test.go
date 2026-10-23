// Tests for: tools/deploy_batch.go — zerops_deploy_batch whose target is a pool
// or whose source is a lease alias, held to what zerops_deploy does for each
// (docs/spec-workflows.md §8 DP-1, DP-7, PS-3, PS-5).
package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// poolBatchMock is a project with a dev service appdev, its stage appstage and
// the dev pool webpool, each with an appVersion the build poll finishes on.
func poolBatchMock() *platform.Mock {
	return platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-dev", Name: "appdev", Status: serviceStatusRunning, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
			{ID: "svc-stage", Name: "appstage", Status: serviceStatusRunning, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
			{ID: "svc-pool", Name: "webpool", Status: serviceStatusRunning, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1"}},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-stage", ProjectID: "proj-1", ServiceStackID: "svc-stage", Status: statusActive, Sequence: 1},
			{ID: "av-pool", ProjectID: "proj-1", ServiceStackID: "svc-pool", Status: statusActive, Sequence: 1},
		})
}

// runPoolBatch calls zerops_deploy_batch with targets over mock and stateDir.
// The tool result is returned whole, and the batch parsed when it is no error.
func runPoolBatch(t *testing.T, mock *platform.Mock, ssh ops.SSHDeployer, stateDir string, targets []map[string]any) (*mcp.CallToolResult, ops.DeployBatchResult, string) {
	t.Helper()
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeployBatch(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{InContainer: true, ServiceName: "zcp"}, stateDir, testDeployEngine(t), nil, DevsupSource{})
	result := callTool(t, srv, "zerops_deploy_batch", map[string]any{"targets": targets})
	text := getTextContent(t, result)
	var batch ops.DeployBatchResult
	if !result.IsError {
		if err := json.Unmarshal([]byte(text), &batch); err != nil {
			t.Fatalf("parse batch result: %v", err)
		}
	}
	return result, batch, text
}

// batchEntryResult is the finished result of the batch entry deploying source
// to target; the test fails when that entry did not produce one.
func batchEntryResult(t *testing.T, batch ops.DeployBatchResult, source, target string) ops.DeployResult {
	t.Helper()
	for _, e := range batch.Entries {
		if e.Target.SourceService != source || e.Target.TargetService != target {
			continue
		}
		if e.Error != "" || e.Result == nil {
			t.Fatalf("entry %s → %s: error %q, result %v", source, target, e.Error, e.Result)
		}
		return *e.Result
	}
	t.Fatalf("no entry %s → %s in %+v", source, target, batch.Entries)
	return ops.DeployResult{}
}

// TestDeployBatch_PoolTarget_SaysOnlyNewContainers: a batch entry whose target
// is a pool says what a single deploy to it says (DP-7) — it reaches only the
// containers the pool creates later and points at the rollout — while its
// sibling to an ordinary stage keeps today's result.
func TestDeployBatch_PoolTarget_SaysOnlyNewContainers(t *testing.T) {
	t.Parallel()

	// Before the first bootstrap: the adoption gate is not active yet, so
	// this test sees only what the deploy says.
	stateDir := filepath.Join(t.TempDir(), ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ssh := &leaseSSH{}
	result, batch, text := runPoolBatch(t, poolBatchMock(), ssh, stateDir, []map[string]any{
		{"sourceService": "appdev", "targetService": "appstage", "setup": "prod"},
		{"sourceService": "appdev", "targetService": "webpool", "setup": "prod"},
	})
	if result.IsError {
		t.Fatalf("a batch with a pool target must run: %s", text)
	}
	if batch.Succeeded != 2 {
		t.Errorf("succeeded = %d, want 2; summary %q", batch.Succeeded, batch.Summary)
	}

	pool := batchEntryResult(t, batch, "appdev", "webpool")
	if pool.Status != statusDeployed {
		t.Errorf("pool entry status = %s, want %s", pool.Status, statusDeployed)
	}
	assertPoolDeployResult(t, pool)
	if !strings.Contains(pool.Message, "keep their version") {
		t.Errorf("message %q must say the containers webpool already runs keep their version", pool.Message)
	}

	stage := batchEntryResult(t, batch, "appdev", "appstage")
	if stage.ReachesNewContainersOnly {
		t.Error("the stage entry carries the pool-image flag")
	}
	if strings.Contains(stage.Message, "creates after it") || strings.Contains(stage.NextActions, "zerops_lease") {
		t.Errorf("the stage entry speaks of a pool: message %q, next actions %q", stage.Message, stage.NextActions)
	}
	if !stage.SSHReady {
		t.Error("the stage entry lost its ssh readiness probe")
	}
	if ssh.hosts()["webpool"] {
		t.Error("an ssh call reached the pool by service hostname — a random container (PS-3)")
	}
}

// TestDeployBatch_PoolTarget_NotGatedNoProbe: a pool is never adopted (PS-5),
// so in a project the adoption gate guards a batch entry targeting one is not
// refused, and nothing — readiness probe, dev-server read, verify, public
// access — reaches the pool by its service hostname (PS-3).
func TestDeployBatch_PoolTarget_NotGatedNoProbe(t *testing.T) {
	t.Parallel()

	stateDir := filepath.Join(t.TempDir(), ".zcp", "state")
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:       "appdev",
		Mode:           topology.ModeDev,
		BootstrappedAt: "2026-10-23T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed meta: %v", err)
	}
	ssh := &leaseSSH{}
	result, batch, text := runPoolBatch(t, poolBatchMock(), ssh, stateDir, []map[string]any{
		{"sourceService": "appdev", "targetService": "webpool", "setup": "prod"},
	})
	if result.IsError {
		t.Fatalf("a batch to a pool in an adopted project must run: %s", text)
	}
	pool := batchEntryResult(t, batch, "appdev", "webpool")
	assertPoolDeployResult(t, pool)
	if pool.DevServer != nil {
		t.Errorf("a dev server was read on the pool: %+v", pool.DevServer)
	}
	if !ssh.hosts()["appdev"] {
		t.Errorf("the push did not run on the source appdev; hosts = %v", ssh.hosts())
	}
	if ssh.hosts()["webpool"] {
		t.Error("an ssh call reached the pool by service hostname — a random container (PS-3)")
	}
}

// TestDeployBatch_LeaseSource_ResolvesAlias: a batch entry whose source is a
// recorded lease alias deploys the way zerops_deploy does from it (DP-1): the
// alias is no service to look up or adopt, the push runs inside the leased
// container, and a deploy to its own pool names the lease that keeps its
// version.
func TestDeployBatch_LeaseSource_ResolvesAlias(t *testing.T) {
	t.Parallel()

	rig := newLeaseDeployRig(t)
	ssh := &leaseSSH{}
	result, batch, text := runPoolBatch(t, rig.mock, ssh, rig.stateDir, []map[string]any{
		{"sourceService": rig.alias, "targetService": "appstage"},
		{"sourceService": rig.alias, "targetService": "webpool"},
	})
	if result.IsError {
		t.Fatalf("a batch from a recorded lease must not be refused: %s", text)
	}

	stage := batchEntryResult(t, batch, rig.alias, "appstage")
	if stage.Status != statusDeployed || stage.SourceService != rig.alias {
		t.Errorf("stage entry = source %q status %q, want %s deployed", stage.SourceService, stage.Status, rig.alias)
	}
	if stage.ReachesNewContainersOnly {
		t.Error("the stage entry carries the pool-image flag")
	}

	pool := batchEntryResult(t, batch, rig.alias, "webpool")
	assertPoolDeployResult(t, pool)
	if !strings.Contains(pool.Message, rig.alias+" keeps the version it runs") {
		t.Errorf("message %q does not name the lease %s as keeping its version", pool.Message, rig.alias)
	}
	if !strings.Contains(pool.NextActions, rig.alias+" keeps its version") {
		t.Errorf("next actions %q do not name the lease %s", pool.NextActions, rig.alias)
	}

	if !ssh.hosts()[rig.alias] {
		t.Errorf("no ssh call reached the lease alias; hosts = %v", ssh.hosts())
	}
	if ssh.hosts()["webpool"] {
		t.Error("an ssh call reached the pool by service hostname — a random container (PS-3)")
	}
	// A lease's supervisor is not migrated by the deploy: no dev-setup or
	// supervisor command runs in the leased container (the stage's own
	// supervisor read is today's post-deploy step).
	ssh.mu.Lock()
	defer ssh.mu.Unlock()
	for _, c := range ssh.calls {
		if c.host == rig.alias && strings.Contains(c.command, "zcp-devsup") {
			t.Errorf("a lease deploy touched the lease's dev setup: %q", c.command)
		}
	}
}
