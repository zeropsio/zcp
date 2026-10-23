// Tests for: dev_server.go, deploy_subdomain.go — a dev server in a dev-pool
// lease (docs/spec-workflows.md §8 "Dev pools (DP)", O3 PA-1/PA-2): the alias
// is the SSH host, the pool is the service whose subdomain, intent and stamp
// public access reads and writes, and the lease's address is the pool's
// subdomain on the lease's preview port.
package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// writeDevpoolLeaseFile records lease devpool-3 the way zerops_lease does (the
// record's file is <state>/leases/<alias>.json).
func writeDevpoolLeaseFile(t *testing.T, stateDir string) {
	t.Helper()
	dir := filepath.Join(stateDir, "leases")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	rec := `{"poolHostname":"devpool","poolServiceId":"svc-pool","containerId":"c-3","containerNumber":3,` +
		`"containerHostname":"node-id-3.runtime.devpool.zerops","lease":"lease-secret","branch":"feat-x","acquiredAt":"2026-10-23T12:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "devpool-3.json"), []byte(rec), 0o600); err != nil {
		t.Fatal(err)
	}
}

// leasePoolMock is a project whose only service is the pool devpool, its
// subdomain off, with four preview ports declared httpSupport.
func leasePoolMock(subdomainOn bool) *platform.Mock {
	pool := platform.ServiceStack{
		ID: "svc-pool", Name: "devpool", ProjectID: "proj-1", Status: "ACTIVE", SubdomainAccess: subdomainOn,
		Ports: []platform.Port{
			{Port: 9001, Protocol: "tcp", HTTPSupport: true}, {Port: 9002, Protocol: "tcp", HTTPSupport: true},
			{Port: 9003, Protocol: "tcp", HTTPSupport: true}, {Port: 9004, Protocol: "tcp", HTTPSupport: true},
		},
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "USER", ServiceStackTypeVersionName: "nodejs@22"},
	}
	return platform.NewMock().
		WithServices([]platform.ServiceStack{pool}).
		WithService(&pool).
		WithProject(&platform.Project{ID: "proj-1", Name: "test", Status: statusActive, SubdomainHost: "abc1.prg1.zerops.app"}).
		WithProcess(&platform.Process{ID: "proc-subdomain-enable-svc-pool", Status: statusFinished})
}

func writePoolMeta(t *testing.T, dir string) {
	t.Helper()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname: "devpool", Mode: topology.PlanModeDev, BootstrapSession: "sess1", BootstrappedAt: "2026-10-23",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
}

func TestEnsurePublicAccess_LeaseAlias_ActsOnPool(t *testing.T) {
	restore := ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 50*time.Millisecond)
	defer restore()

	dir := t.TempDir()
	writeDevpoolLeaseFile(t, dir)
	writePoolMeta(t, dir)
	mock := leasePoolMock(false)
	result := &ops.DeployResult{}

	ensurePublicAccess(context.Background(), mock, okHTTP, "proj-1", dir, "devpool-3", result, true)

	if mock.CallCounts["EnableSubdomainAccess"] != 1 {
		t.Fatalf("EnableSubdomainAccess calls = %d, want 1 (on the pool): warnings %v", mock.CallCounts["EnableSubdomainAccess"], result.Warnings)
	}
	meta, err := workflow.FindServiceMeta(dir, "devpool")
	if err != nil || meta == nil {
		t.Fatalf("pool meta: %v %v", meta, err)
	}
	if meta.PublicAccessFor("devpool").SubdomainEnabledByZcpAt == "" {
		t.Error("the stamp belongs on the pool, and is missing")
	}
	if alias, _ := workflow.FindServiceMeta(dir, "devpool-3"); alias != nil {
		t.Errorf("the alias must never get a meta of its own: %+v", alias)
	}
	if result.PublicAccess == nil || result.PublicAccess.Intent == "" {
		t.Errorf("publicAccess = %+v, want the pool's summary", result.PublicAccess)
	}

	// PA-2: the stamp is the pool's, so a second lease of the pool never
	// enables again.
	ensurePublicAccess(context.Background(), mock, okHTTP, "proj-1", dir, "devpool-3", &ops.DeployResult{}, true)
	if mock.CallCounts["EnableSubdomainAccess"] != 1 {
		t.Errorf("EnableSubdomainAccess calls after a second start = %d, want still 1", mock.CallCounts["EnableSubdomainAccess"])
	}
}

func TestEnsurePublicAccess_LeaseAlias_NoRecordLooksUpTheAliasAsAService(t *testing.T) {
	dir := t.TempDir() // no lease record
	mock := leasePoolMock(false)

	ensurePublicAccess(context.Background(), mock, okHTTP, "proj-1", dir, "devpool-3", &ops.DeployResult{}, true)

	if mock.CallCounts["EnableSubdomainAccess"] != 0 {
		t.Errorf("an alias with no record is no service and no pool: EnableSubdomainAccess calls = %d", mock.CallCounts["EnableSubdomainAccess"])
	}
}

func TestDevServer_LeaseAlias_PreviewURLFromPoolSubdomain(t *testing.T) {
	restore := ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 50*time.Millisecond)
	defer restore()

	dir := t.TempDir()
	writeDevpoolLeaseFile(t, dir)
	writePoolMeta(t, dir)
	mock := leasePoolMock(false)
	c := devservertest.NewSupervised(`{"version":1,"previewPortBase":9000,"servers":[{"name":"web","port":3000,"command":"pnpm dev"}]}`)
	c.Servers["web"].PreviewPort = 9003
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDevServer(srv, DevServerDeps{Client: mock, HTTPClient: okHTTP, ProjectID: "proj-1", SSH: c, StateDir: dir})

	result := callTool(t, srv, "zerops_dev_server", map[string]any{"action": "start", "hostname": "devpool-3"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	var got struct {
		Hostname     string `json:"hostname"`
		Running      bool   `json:"running"`
		Port         int    `json:"port"`
		Pool         string `json:"pool"`
		PreviewPort  int    `json:"previewPort"`
		PreviewURL   string `json:"previewUrl"`
		SubdomainURL string `json:"subdomainUrl"`
		PublicAccess struct {
			Subdomain string `json:"subdomain"`
			URL       string `json:"url"`
		} `json:"publicAccess"`
	}
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &got); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	const want = "https://devpool-abc1-9003.prg1.zerops.app"
	if got.Hostname != "devpool-3" || got.Pool != "devpool" || !got.Running {
		t.Errorf("hostname/pool/running = %q/%q/%v", got.Hostname, got.Pool, got.Running)
	}
	if got.Port != 3000 || got.PreviewPort != 9003 {
		t.Errorf("port/previewPort = %d/%d, want the server's 3000 and the forward's 9003", got.Port, got.PreviewPort)
	}
	if got.PreviewURL != want || got.SubdomainURL != want || got.PublicAccess.URL != want {
		t.Errorf("urls = %q / %q / %q, all want the pool's subdomain on the preview port %q", got.PreviewURL, got.SubdomainURL, got.PublicAccess.URL, want)
	}
	if mock.CallCounts["EnableSubdomainAccess"] != 1 {
		t.Errorf("EnableSubdomainAccess calls = %d, want once, on the pool", mock.CallCounts["EnableSubdomainAccess"])
	}
}

func TestDevServer_LeaseAlias_StatusReportsThePreviewURLOnlyWhileThePoolSubdomainIsOn(t *testing.T) {
	tests := []struct {
		name        string
		subdomainOn bool
		want        string
	}{
		{name: "on", subdomainOn: true, want: "https://devpool-abc1-9003.prg1.zerops.app"},
		{name: "off", subdomainOn: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeDevpoolLeaseFile(t, dir)
			mock := leasePoolMock(tt.subdomainOn)
			c := devservertest.NewSupervised(`{"version":1,"previewPortBase":9000,"servers":[{"name":"web","port":3000,"command":"pnpm dev"}]}`)
			c.Servers["web"].PreviewPort = 9003
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterDevServer(srv, DevServerDeps{Client: mock, HTTPClient: okHTTP, ProjectID: "proj-1", SSH: c, StateDir: dir})

			result := callTool(t, srv, "zerops_dev_server", map[string]any{"action": "status", "hostname": "devpool-3"})
			if result.IsError {
				t.Fatalf("unexpected error: %s", getTextContent(t, result))
			}
			var got struct {
				PreviewURL string `json:"previewUrl"`
			}
			if err := json.Unmarshal([]byte(getTextContent(t, result)), &got); err != nil {
				t.Fatal(err)
			}
			if got.PreviewURL != tt.want {
				t.Errorf("previewUrl = %q, want %q", got.PreviewURL, tt.want)
			}
			if mock.CallCounts["EnableSubdomainAccess"] != 0 {
				t.Errorf("a status never enables a subdomain")
			}
		})
	}
}
