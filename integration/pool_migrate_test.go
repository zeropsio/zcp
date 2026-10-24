// Tests for: integration — zerops_workflow workflow="pool-migrate" through the
// full MCP server, against a mock platform and a fake dev service over SSH.

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
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

// poolRetireIntReadyYAML is a zerops.yaml whose pool setup passes every image
// check for a pool of three containers and a previewPortBase of 4000.
const poolRetireIntReadyYAML = `zerops:
  - setup: apppool
    build:
      base: nodejs@22
      addToRunPrepare:
        - package.json
    run:
      base: nodejs@22
      ports:
        - port: 4001
          httpSupport: true
        - port: 4002
          httpSupport: true
        - port: 4003
          httpSupport: true
      prepareCommands:
        - sudo sh -c 'u=https://github.com/zeropsio/zcp/releases/download/v1.2.3/zcp-devsup-linux-amd64; f=/usr/local/bin/zcp-devsup; curl -fsSL "$u" -o "$f" && chmod 0755 "$f"'
        - cp package.json /var/www/ && cd /var/www && npm ci
      start: zcp-devsup run
`

const poolRetireIntDeclaration = `{"version": 1, "previewPortBase": 4000, "servers": [{"name": "web", "port": 3000, "command": "npm run dev"}]}`

// poolRetireIntSSH is the dev service and the pool's lease: the census and
// the image read answer canned; the retire's own scripts run in a local shell
// against repo, a real repository standing for /var/www.
type poolRetireIntSSH struct {
	mu       sync.Mutex
	repo     string
	commands []string
}

func (s *poolRetireIntSSH) record(c string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands = append(s.commands, c)
}

func (s *poolRetireIntSSH) local(ctx context.Context, command string) *exec.Cmd {
	command = strings.ReplaceAll(command, "'/var/www'", "'"+s.repo+"'")
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	return cmd
}

func (s *poolRetireIntSSH) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	s.record(hostname + ": " + command)
	switch {
	case hostname == "apppool-1" && command == "true":
		return nil, nil
	case hostname == "apppool-1":
		return nil, errors.New("no supervisor answer here")
	case strings.Contains(command, "'zcp-image 1'"):
		var b strings.Builder
		fmt.Fprintf(&b, "zcp-image 1\nF %d zerops.yaml\n%s\nF %d .zcp/dev-servers.json\n%s\nC 2026-10-24T10:00:00+02:00\nzcp-image-end\n",
			len(poolRetireIntReadyYAML), poolRetireIntReadyYAML, len(poolRetireIntDeclaration), poolRetireIntDeclaration)
		return []byte(b.String()), nil
	case strings.Contains(command, "'zcp-bundle 1'"), strings.Contains(command, "'zcp-fingerprint 1'"), strings.HasPrefix(command, "rm -rf "):
		out, err := s.local(ctx, command).CombinedOutput()
		if err != nil {
			return out, &platform.SSHExecError{Hostname: hostname, Output: string(out), Err: err}
		}
		return out, nil
	}
	return []byte(poolMigrateIntCensus()), nil
}

func (s *poolRetireIntSSH) ExecSSHBackground(_ context.Context, hostname, command string, _ time.Duration) ([]byte, error) {
	s.record("background " + hostname + ": " + command)
	return nil, nil
}

func (s *poolRetireIntSSH) StreamSSH(ctx context.Context, hostname, command string, stdout io.Writer) error {
	s.record("stream " + hostname + ": " + command)
	cmd := s.local(ctx, command)
	cmd.Stdout = stdout
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return &platform.SSHExecError{Hostname: hostname, Output: stderr.String(), Err: err}
	}
	return nil
}

// poolRetireIntClient deletes the dev service for real: once deleted it is
// gone by id.
type poolRetireIntClient struct {
	*platform.Mock
	mu      sync.Mutex
	deleted []string
}

func (c *poolRetireIntClient) GetService(ctx context.Context, id string) (*platform.ServiceStack, error) {
	c.mu.Lock()
	gone := slices.Contains(c.deleted, id)
	c.mu.Unlock()
	if gone {
		return nil, platform.NewPlatformError(platform.ErrServiceNotFound, "service not found", "")
	}
	return c.Mock.GetService(ctx, id)
}

func (c *poolRetireIntClient) DeleteService(ctx context.Context, id string) (*platform.Process, error) {
	c.mu.Lock()
	c.deleted = append(c.deleted, id)
	c.mu.Unlock()
	return c.Mock.DeleteService(ctx, id)
}

type poolRetireIntSwarm struct{}

var errPoolRetireIntSwarm = errors.New("only listed here")

func (poolRetireIntSwarm) Pool(context.Context) (platform.SwarmPool, error) {
	return platform.SwarmPool{}, errPoolRetireIntSwarm
}
func (poolRetireIntSwarm) Containers(context.Context) ([]platform.SwarmContainer, error) {
	return []platform.SwarmContainer{{ID: "c-1", Number: 1, Hostname: "node-id-1.runtime.apppool.zerops", LockState: "reserved"}}, nil
}
func (poolRetireIntSwarm) Acquire(context.Context, platform.SwarmAcquireRequest) (platform.SwarmAcquired, error) {
	return platform.SwarmAcquired{}, errPoolRetireIntSwarm
}
func (poolRetireIntSwarm) Release(context.Context, string, string, bool) (platform.SwarmContainer, error) {
	return platform.SwarmContainer{}, errPoolRetireIntSwarm
}
func (poolRetireIntSwarm) Renew(context.Context, string, string) (platform.SwarmContainer, error) {
	return platform.SwarmContainer{}, errPoolRetireIntSwarm
}
func (poolRetireIntSwarm) Delete(context.Context, string, string) error { return errPoolRetireIntSwarm }
func (poolRetireIntSwarm) Rollout(context.Context) (platform.SwarmRollout, error) {
	return platform.SwarmRollout{}, errPoolRetireIntSwarm
}

func poolRetireIntGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// poolRetireIntRig is a pair ready to retire behind the full MCP server: the
// pool provisioned, holding the dev's env and running the commit's pool
// setup, zcp holding a lease of it on main.
func poolRetireIntRig(t *testing.T, withOrchestrator bool) (*mcp.ClientSession, *poolRetireIntClient, *poolRetireIntSSH, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	stateDir := t.TempDir()
	const origin = "https://github.com/example/app.git"
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeStandard,
		Stages:       []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}},
		GitPushState: topology.GitPushConfigured, RemoteURL: origin, BootstrappedAt: "2026-10-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if got := ops.SeedPoolDefaultRepo(stateDir, "apppool", origin); got.State != ops.PoolDefaultRepoSeeded {
		t.Fatalf("seed: %+v", got)
	}
	leases := filepath.Join(stateDir, "leases")
	if err := os.MkdirAll(leases, 0o700); err != nil {
		t.Fatal(err)
	}
	record := `{"poolHostname":"apppool","poolServiceId":"svc-pool","containerId":"c-1","containerNumber":1,"containerHostname":"node-id-1.runtime.apppool.zerops","lease":"l","branch":"main","repo":"` + origin + `","acquiredAt":"2026-10-24T09:30:00Z"}`
	if err := os.WriteFile(filepath.Join(leases, "apppool-1.json"), []byte(record), 0o600); err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join(t.TempDir(), "www")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	poolRetireIntGit(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	poolRetireIntGit(t, repo, "add", "-A")
	poolRetireIntGit(t, repo, "commit", "-q", "-m", "c1")

	pool := poolMigrateIntService("apppool", "svc-pool", "swarm@1")
	pool.ActiveAppVersion = &platform.ActiveAppVersionDigest{ID: "av-pool", Created: "2026-10-24T09:00:00Z", Source: "CLI", Built: true}
	services := []platform.ServiceStack{
		poolMigrateIntService("appdev", "svc-dev", "nodejs@22"),
		poolMigrateIntService("appstage", "svc-stage", "nodejs@22"),
		pool,
	}
	mock := platform.NewMock().WithServices(services).WithServicesDirect(services).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{
			{ID: "p-api", Key: "API_TOKEN", Content: "pool-api-token", Type: platform.ServiceEnvUser},
			{ID: "p-ssh", Key: "sshIsolation", Content: "vpn service@zcp", Type: platform.ServiceEnvUser},
		}).
		WithProcess(&platform.Process{ID: "proc-delete-svc-dev", Status: platform.ProcessStatusFinished})
	client := &poolRetireIntClient{Mock: mock}
	ssh := &poolRetireIntSSH{repo: repo}

	srv := mcp.NewServer(&mcp.Implementation{Name: "zcp-test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(stateDir, workflow.EnvContainer, nil)
	var opts []tools.WorkflowOption
	if withOrchestrator {
		opts = append(opts, tools.WithPoolOrchestratorsForWorkflow(func(string, string) ops.SwarmAPI { return poolRetireIntSwarm{} }))
	}
	tools.RegisterWorkflow(srv, client, nil, "proj-1", nil, engine, nil, stateDir, "zcp", nil, ssh, runtime.Info{InContainer: true, ServiceName: "zcp"}, "", opts...)
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "integration-test", Version: "0.1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session, client, ssh, stateDir
}

func poolRetireIntCall(t *testing.T, session *mcp.ClientSession, args map[string]any) (map[string]any, bool) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "zerops_workflow", Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if result.StructuredContent != nil {
		t.Fatalf("structuredContent must stay empty: %v", result.StructuredContent)
	}
	var out map[string]any
	text := result.Content[0].(*mcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("decode: %v\n%s", err, text)
	}
	return out, result.IsError
}

func TestPoolMigrate_Integration_RetireDiagnoseConfirmDeletesByRecordedID(t *testing.T) {
	t.Parallel()
	session, client, ssh, stateDir := poolRetireIntRig(t, true)

	out, _ := poolRetireIntCall(t, session, map[string]any{"workflow": "pool-migrate", "service": "appdev"})
	if out["step"] != "ready-to-retire" {
		t.Fatalf("read side = %v, want ready-to-retire", out["step"])
	}
	retire := map[string]any{"workflow": "pool-migrate", "action": "retire", "service": "appdev"}
	out, isErr := poolRetireIntCall(t, session, retire)
	if !isErr || out["code"] != platform.ErrDiagnosisRequired {
		t.Fatalf("diagnose = %v", out)
	}
	if len(client.deleted) > 0 {
		t.Fatal("the diagnosis deleted")
	}
	retire["confirmDestructive"] = map[string]any{"operation": "pool-retire", "acknowledgedTargets": []string{"appdev"}}
	out, isErr = poolRetireIntCall(t, session, retire)
	if isErr || out["step"] != "retired" {
		t.Fatalf("confirmed = %v, want retired", out)
	}
	if !slices.Equal(client.deleted, []string{"svc-dev"}) {
		t.Fatalf("deleted %q, want svc-dev by its id", client.deleted)
	}
	block, _ := out["retire"].(map[string]any)
	bundle, _ := block["bundle"].(map[string]any)
	path, _ := bundle["path"].(string)
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("bundle %q: %v", path, err)
	}
	meta, err := workflow.FindServiceMeta(stateDir, "appstage")
	if err != nil || meta == nil || meta.Hostname != "apppool" || !meta.DevIsPool() {
		t.Fatalf("the stage's pair = %+v, want it keyed by the pool", meta)
	}
	var streamed bool
	for _, c := range ssh.commands {
		streamed = streamed || strings.HasPrefix(c, "stream appdev: ")
	}
	if !streamed {
		t.Errorf("the bundle was not streamed off the dev: %q", ssh.commands)
	}
	// The read side now answers from the record.
	if out, _ := poolRetireIntCall(t, session, map[string]any{"workflow": "pool-migrate", "service": "appstage"}); out["step"] != "retired" {
		t.Fatalf("read side after the retire = %v, want retired", out["step"])
	}
}

func TestPoolMigrate_Integration_RetireWithoutOrchestratorRefused(t *testing.T) {
	t.Parallel()
	session, client, _, stateDir := poolRetireIntRig(t, false)
	out, isErr := poolRetireIntCall(t, session, map[string]any{
		"workflow": "pool-migrate", "action": "retire", "service": "appdev",
		"confirmDestructive": map[string]any{"operation": "pool-retire", "acknowledgedTargets": []string{"appdev"}},
	})
	if isErr || out["status"] != "retire-refused" {
		t.Fatalf("retire = %v, want retire-refused", out)
	}
	refusals, _ := out["refusals"].([]any)
	first, _ := refusals[0].(map[string]any)
	if first["reason"] != "lease-unreadable" {
		t.Fatalf("refusal = %v", first)
	}
	if len(client.deleted) > 0 {
		t.Fatal("deleted without a lease the retire could check")
	}
	if _, err := os.Stat(filepath.Join(stateDir, "migrate", "appdev.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused retire wrote its record: %v", err)
	}
}
