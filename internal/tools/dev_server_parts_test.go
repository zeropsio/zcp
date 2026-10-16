// Tests for: dev_server.go — the zerops_dev_server answer on a dev service
// that runs several dev servers: the subdomain address of the started
// server's own port, and a status of every server that reports none alone.

package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// webAndDocs declares web (:3000) and docs (:5173) on appdev.
const webAndDocs = `{"version":1,"servers":[` +
	`{"name":"web","port":3000,"command":"pnpm dev","cwd":"apps/web"},` +
	`{"name":"docs","port":5173,"command":"pnpm docs","cwd":"apps/docs"}]}`

func TestDevServer_AStatusOfSeveralServers_SaysNothingOfRunning(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "appdev", ProjectID: "proj-1", Status: serviceStatusRunning}})
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDevServer(srv, DevServerDeps{Client: mock, ProjectID: "proj-1", StateDir: t.TempDir(), SSH: devservertest.NewSupervised(webAndDocs)})

	result := callTool(t, srv, "zerops_dev_server", map[string]any{"action": "status", "hostname": "appdev"})
	text := getTextContent(t, result)
	if result.IsError {
		t.Fatalf("status of several: %s", text)
	}
	var answer map[string]any
	if err := json.Unmarshal([]byte(text), &answer); err != nil {
		t.Fatalf("parse %s: %v", text, err)
	}
	if running, said := answer["running"]; said {
		t.Errorf("running = %v on a status that reports two servers, want it left out", running)
	}
	if msg, _ := answer["message"].(string); !strings.Contains(msg, "web on appdev is running") || !strings.Contains(msg, "docs on appdev is running") {
		t.Errorf("message = %q, want both servers", msg)
	}
}

func TestDevServer_AStartedServer_ReportsTheSubdomainAddressOfItsOwnPort(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeDev, BootstrapSession: "sess1", BootstrappedAt: "2026-10-16",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	svc := platform.ServiceStack{ID: "svc-1", Name: "appdev", ProjectID: "proj-1", Status: "ACTIVE", SubdomainAccess: true,
		Ports:                []platform.Port{{Port: 3000, Protocol: "tcp", Scheme: "http"}, {Port: 5173, Protocol: "tcp", Scheme: "http"}},
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "USER", ServiceStackTypeVersionName: "nodejs@22"}}
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{svc}).
		WithService(&svc).
		WithProject(&platform.Project{ID: "proj-1", Name: "test", Status: statusActive, SubdomainHost: "abc1.prg1.zerops.app"})
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDevServer(srv, DevServerDeps{Client: mock, HTTPClient: okHTTP, ProjectID: "proj-1", SSH: devservertest.NewSupervised(webAndDocs), StateDir: dir})

	result := callTool(t, srv, "zerops_dev_server", map[string]any{"action": "restart", "hostname": "appdev", "name": "docs"})
	text := getTextContent(t, result)
	if result.IsError {
		t.Fatalf("restart docs: %s", text)
	}
	var answer struct {
		SubdomainURL string `json:"subdomainUrl"`
		PublicAccess struct {
			URL string `json:"url"`
		} `json:"publicAccess"`
	}
	if err := json.Unmarshal([]byte(text), &answer); err != nil {
		t.Fatalf("parse %s: %v", text, err)
	}
	if !strings.HasPrefix(answer.SubdomainURL, "https://appdev-") || !strings.Contains(answer.SubdomainURL, "-5173.") {
		t.Errorf("subdomainUrl = %q, want docs' own port 5173", answer.SubdomainURL)
	}
	if answer.PublicAccess.URL != answer.SubdomainURL {
		t.Errorf("publicAccess.url = %q beside subdomainUrl %q, want the same port's address", answer.PublicAccess.URL, answer.SubdomainURL)
	}
}
