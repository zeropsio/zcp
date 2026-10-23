// Tests for: integration — zerops_workflow workflow="pool-migrate" through the
// full MCP server, against a mock platform and a fake dev service over SSH.

package integration_test

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/tools"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// poolMigrateDevSSH stands in for the dev service: it answers the census
// with census and records every command.
type poolMigrateDevSSH struct {
	mu       sync.Mutex
	census   string
	commands []string
}

func (s *poolMigrateDevSSH) ExecSSH(_ context.Context, hostname, command string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands = append(s.commands, hostname+": "+command)
	return []byte(s.census), nil
}

// poolMigrateIntCensus is what the census prints for a dev service whose main
// checkout is on main, pushed and clean, beside the linked worktrees given as
// (path, branch) pairs.
func poolMigrateIntCensus(worktrees ...string) string {
	var b strings.Builder
	b.WriteString("zcp-census 1\n" +
		"W worktree /var/www\nW HEAD 1111111111111111111111111111111111111111\nW branch refs/heads/main\n")
	for i := 0; i+1 < len(worktrees); i += 2 {
		for _, line := range []string{"", "worktree " + worktrees[i], "HEAD 2222222222222222222222222222222222222222", "branch refs/heads/" + worktrees[i+1]} {
			b.WriteString("W " + line + "\n")
		}
	}
	b.WriteString("B refs/heads/main\torigin\trefs/heads/main\nU refs/heads/main\t0\nRN 0\n" +
		"T /var/www\nTS # branch.oid 1111111111111111111111111111111111111111\nTS # branch.head main\n" +
		"TS # branch.upstream origin/main\nTS # branch.ab +0 -0\n")
	for i := 0; i+1 < len(worktrees); i += 2 {
		b.WriteString("T " + worktrees[i] + "\nTS # branch.head " + worktrees[i+1] + "\n")
	}
	b.WriteString("zcp-census-end\n")
	return b.String()
}

func (s *poolMigrateDevSSH) ExecSSHBackground(_ context.Context, hostname, command string, _ time.Duration) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands = append(s.commands, "background "+hostname+": "+command)
	return nil, nil
}

func poolMigrateIntService(hostname, id, typ string) platform.ServiceStack {
	return platform.ServiceStack{
		ID:   id,
		Name: hostname,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  typ,
			ServiceStackTypeCategoryName: "USER",
		},
		Status: "ACTIVE",
		CustomAutoscaling: &platform.CustomAutoscaling{
			CPUMode: "SHARED", MinCPU: 1, MaxCPU: 2, MinRAM: 0.25, MaxRAM: 4, MinDisk: 1, MaxDisk: 10,
		},
	}
}

func poolMigrateIntTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			out[rel] = "dir"
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = string(b)
		return nil
	}); err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

func poolMigrateIntCall(t *testing.T, mode topology.Mode, census string) (map[string]any, *platform.Mock, *poolMigrateDevSSH, bool) {
	t.Helper()
	stateDir := t.TempDir()
	meta := &workflow.ServiceMeta{
		Hostname:       "appdev",
		Mode:           mode,
		GitPushState:   topology.GitPushConfigured,
		RemoteURL:      "https://github.com/example/app.git",
		BootstrappedAt: "2026-10-01T00:00:00Z",
	}
	if mode == topology.PlanModeStandard {
		meta.Stages = []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}}
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("seed meta: %v", err)
	}
	services := []platform.ServiceStack{
		poolMigrateIntService("appdev", "svc-dev", "nodejs@22"),
		poolMigrateIntService("appstage", "svc-stage", "nodejs@22"),
	}
	mock := platform.NewMock().WithServices(services).WithServicesDirect(services)
	ssh := &poolMigrateDevSSH{census: census}

	srv := mcp.NewServer(&mcp.Implementation{Name: "zcp-test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(stateDir, workflow.EnvContainer, nil)
	tools.RegisterWorkflow(srv, mock, nil, "proj-1", nil, engine, nil, stateDir, "zcp", nil, ssh, runtime.Info{InContainer: true, ServiceName: "zcp"}, "")

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "integration-test", Version: "0.1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	before := poolMigrateIntTree(t, stateDir)
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "zerops_workflow",
		Arguments: map[string]any{"workflow": "pool-migrate", "service": "appdev"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if result.IsError {
		t.Fatalf("error result: %s", text)
	}
	if result.StructuredContent != nil {
		t.Fatalf("structuredContent must stay empty: %v", result.StructuredContent)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("decode: %v\n%s", err, text)
	}
	unchanged := reflect.DeepEqual(before, poolMigrateIntTree(t, stateDir))
	return out, mock, ssh, unchanged
}

func TestPoolMigrate_Integration_PairNamesProvisionWithAStanza(t *testing.T) {
	t.Parallel()
	out, mock, ssh, unchanged := poolMigrateIntCall(t, topology.PlanModeStandard, poolMigrateIntCensus())

	if out["status"] != "step" || out["step"] != "provision" || out["pool"] != "apppool" {
		t.Fatalf("status/step/pool = %v/%v/%v", out["status"], out["step"], out["pool"])
	}
	next, _ := out["next"].(map[string]any)
	args, _ := next["args"].(map[string]any)
	content, _ := args["content"].(string)
	if next["tool"] != "zerops_import" || !strings.Contains(content, "type: swarm@1") {
		t.Fatalf("next = %v", next)
	}
	if errs := schema.ValidateImportYAML(content); len(errs) > 0 {
		t.Fatalf("stanza fails the import schema: %v\n%s", errs, content)
	}
	if !unchanged {
		t.Fatal("the read side changed the state dir")
	}
	// Every call has returned, so CallCounts is safe to range over.
	reads := map[string]bool{"ListServicesDirect": true, "GetProjectProcessesDirect": true, "GetService": true}
	for method, n := range mock.CallCounts {
		if n > 0 && !reads[method] {
			t.Errorf("the read side called %s %d times", method, n)
		}
	}
	if len(ssh.commands) != 1 || !strings.Contains(ssh.commands[0], "worktree list --porcelain") {
		t.Fatalf("ssh commands = %v, want the one census", ssh.commands)
	}
	census, _ := out["census"].(map[string]any)
	if census["clean"] != true {
		t.Fatalf("census = %v, want the clean dev service's census", out["census"])
	}
	if wb, _ := out["workBranch"].(map[string]any); wb["branch"] != "main" || wb["source"] != "upstream" {
		t.Fatalf("workBranch = %v, want main from the upstream", out["workBranch"])
	}
}

func TestPoolMigrate_Integration_SimpleServiceRefusedByName(t *testing.T) {
	t.Parallel()
	out, _, ssh, unchanged := poolMigrateIntCall(t, topology.PlanModeSimple, "")

	if out["status"] != "not-a-candidate" {
		t.Fatalf("status = %v, want not-a-candidate", out["status"])
	}
	refusals, _ := out["refusals"].([]any)
	first, _ := refusals[0].(map[string]any)
	if first["reason"] != "simple-mode" || !strings.Contains(first["message"].(string), "appdev") {
		t.Fatalf("refusal = %v", first)
	}
	if len(ssh.commands) != 0 || !unchanged {
		t.Fatalf("a refused service was touched: ssh %v, state unchanged %v", ssh.commands, unchanged)
	}
}

func TestPoolMigrate_Integration_CrewWorktreesRefused(t *testing.T) {
	t.Parallel()
	out, _, _, unchanged := poolMigrateIntCall(t, topology.PlanModeStandard, poolMigrateIntCensus("/var/www/.crew/ada", "crew/ada"))

	if out["status"] != "not-a-candidate" {
		t.Fatalf("status = %v, want not-a-candidate", out["status"])
	}
	refusals, _ := out["refusals"].([]any)
	first, _ := refusals[0].(map[string]any)
	if first["reason"] != "crew-worktrees" || !strings.Contains(first["message"].(string), "ada") {
		t.Fatalf("refusal = %v", first)
	}
	if !unchanged {
		t.Fatal("the read side changed the state dir")
	}
}
