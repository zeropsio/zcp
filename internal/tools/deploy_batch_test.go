// Tests for: tools/deploy_batch.go — zerops_deploy_batch MCP tool handler.
package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestDeployBatch_ThreeTargetsAllSucceed(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-api-src", Name: "apidev"},
			{ID: "svc-app-src", Name: "appdev"},
			{ID: "svc-worker-src", Name: "workerdev"},
			{ID: "svc-api-tgt", Name: "apistage"},
			{ID: "svc-app-tgt", Name: "appstage"},
			{ID: "svc-worker-tgt", Name: "workerstage"},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-api", ServiceStackID: "svc-api-tgt", Status: statusActive, Sequence: 1},
			{ID: "av-app", ServiceStackID: "svc-app-tgt", Status: statusActive, Sequence: 1},
			{ID: "av-worker", ServiceStackID: "svc-worker-tgt", Status: statusActive, Sequence: 1},
		})
	ssh := &stubSSH{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeployBatch(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, "", testDeployEngine(t))

	result := callTool(t, srv, "zerops_deploy_batch", map[string]any{
		"targets": []map[string]any{
			{"sourceService": "apidev", "targetService": "apistage", "setup": "prod"},
			{"sourceService": "appdev", "targetService": "appstage", "setup": "prod"},
			{"sourceService": "workerdev", "targetService": "workerstage", "setup": "prod"},
		},
	})
	if result.IsError {
		t.Fatalf("tool returned error: %s", getTextContent(t, result))
	}

	var batch ops.DeployBatchResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &batch); err != nil {
		t.Fatalf("failed to parse batch result: %v", err)
	}
	if len(batch.Entries) != 3 {
		t.Fatalf("entries len = %d, want 3", len(batch.Entries))
	}
	if batch.Succeeded != 3 {
		t.Errorf("succeeded = %d, want 3; summary=%q", batch.Succeeded, batch.Summary)
		for _, e := range batch.Entries {
			t.Logf("  %s → status=%v err=%q", e.Target.TargetService, statusOrNone(e.Result), e.Error)
		}
	}
	if batch.Failed != 0 {
		t.Errorf("failed = %d, want 0", batch.Failed)
	}
	// Each target's result must be present and DEPLOYED.
	for _, e := range batch.Entries {
		if e.Result == nil {
			t.Errorf("target %s: nil Result", e.Target.TargetService)
			continue
		}
		if e.Result.Status != statusDeployed {
			t.Errorf("target %s: status = %s, want DEPLOYED", e.Target.TargetService, e.Result.Status)
		}
	}
}

func TestDeployBatch_EmptyTargetsFails(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock()
	ssh := &stubSSH{}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeployBatch(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, "", testDeployEngine(t))

	result := callTool(t, srv, "zerops_deploy_batch", map[string]any{
		"targets": []map[string]any{},
	})
	text := getTextContent(t, result)
	if text == "" {
		t.Fatal("empty response")
	}
	// Either IsError=true or an error text in the body.
	if !result.IsError && !contains(text, "at least one target") {
		t.Errorf("expected at-least-one-target error, got: %s", text)
	}
}

// TestDeployBatch_CrossDeploy_PreflightReadsSourceMounts pins that the batch
// handler plumbs `sourceService` into pre-flight (was: only TargetService
// reached preflight, masking cross-deploy YAML lookup). With a non-empty
// stateDir + per-source-mount yaml, three dev→stage cross-deploys must pass
// preflight and complete — the same shape `ops.deploySSH` runs at deploy
// time. Closes the Codex-flagged batch coverage gap (the existing
// _ThreeTargetsAllSucceed test passes stateDir="" so preflight is skipped).
func TestDeployBatch_CrossDeploy_PreflightReadsSourceMounts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Standard pair metas — one ServiceMeta per pair (spec-workflows.md §8 E8).
	for _, m := range []*workflow.ServiceMeta{
		{Hostname: "apidev", StageHostname: "apistage", Mode: "standard", BootstrapSession: "s1", BootstrappedAt: "2026-04-01T00:00:00Z"},
		{Hostname: "appdev", StageHostname: "appstage", Mode: "standard", BootstrapSession: "s1", BootstrappedAt: "2026-04-01T00:00:00Z"},
		{Hostname: "workerdev", StageHostname: "workerstage", Mode: "standard", BootstrapSession: "s1", BootstrappedAt: "2026-04-01T00:00:00Z"},
	} {
		if err := workflow.WriteServiceMeta(stateDir, m); err != nil {
			t.Fatalf("write meta %s: %v", m.Hostname, err)
		}
	}

	// One yaml per source mount — recipe layout. Pre-fix the batch
	// preflight searched <projectRoot>/<targetHostname>/zerops.yaml first;
	// none of these would resolve.
	yamlBody := `zerops:
  - setup: dev
    build:
      base: nodejs@22
      deployFiles: [.]
    run:
      start: node dist/main.js
      ports:
        - port: 3000
      envVariables:
        NODE_ENV: development
  - setup: prod
    build:
      base: nodejs@22
      deployFiles: [./dist]
    run:
      start: node dist/main.js
      ports:
        - port: 3000
      envVariables:
        NODE_ENV: production
`
	for _, src := range []string{"apidev", "appdev", "workerdev"} {
		mountDir := filepath.Join(dir, src)
		if err := os.MkdirAll(mountDir, 0o755); err != nil {
			t.Fatalf("mkdir mount %s: %v", src, err)
		}
		if err := os.WriteFile(filepath.Join(mountDir, "zerops.yaml"), []byte(yamlBody), 0o600); err != nil {
			t.Fatalf("write yaml %s: %v", src, err)
		}
	}

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-api-src", Name: "apidev"},
			{ID: "svc-app-src", Name: "appdev"},
			{ID: "svc-worker-src", Name: "workerdev"},
			{ID: "svc-api-tgt", Name: "apistage"},
			{ID: "svc-app-tgt", Name: "appstage"},
			{ID: "svc-worker-tgt", Name: "workerstage"},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-api", ServiceStackID: "svc-api-tgt", Status: statusActive, Sequence: 1},
			{ID: "av-app", ServiceStackID: "svc-app-tgt", Status: statusActive, Sequence: 1},
			{ID: "av-worker", ServiceStackID: "svc-worker-tgt", Status: statusActive, Sequence: 1},
		})
	ssh := &stubSSH{output: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeployBatch(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, stateDir, testDeployEngine(t))

	result := callTool(t, srv, "zerops_deploy_batch", map[string]any{
		"targets": []map[string]any{
			{"sourceService": "apidev", "targetService": "apistage", "setup": "prod"},
			{"sourceService": "appdev", "targetService": "appstage", "setup": "prod"},
			{"sourceService": "workerdev", "targetService": "workerstage", "setup": "prod"},
		},
	})
	if result.IsError {
		t.Fatalf("batch tool returned error (preflight likely failed): %s", getTextContent(t, result))
	}

	var batch ops.DeployBatchResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &batch); err != nil {
		t.Fatalf("parse batch result: %v", err)
	}
	if batch.Succeeded != 3 {
		t.Errorf("succeeded = %d, want 3 — preflight may have failed off the wrong yaml location", batch.Succeeded)
		for _, e := range batch.Entries {
			t.Logf("  %s → status=%v err=%q", e.Target.TargetService, statusOrNone(e.Result), e.Error)
		}
	}
}

// contains — small utility to keep the test readable without importing strings
// at every test file top. Trivial subset of strings.Contains.
func contains(haystack, needle string) bool {
	return len(needle) == 0 || indexOf(haystack, needle) >= 0
}

func indexOf(h, n string) int {
	if len(n) > len(h) {
		return -1
	}
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

// statusOrNone is a small helper for diagnostic logging — avoids panicking
// when a batch entry carries nil Result.
func statusOrNone(r *ops.DeployResult) string {
	if r == nil {
		return "<nil>"
	}
	return r.Status
}
