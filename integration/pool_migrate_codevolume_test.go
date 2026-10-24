// Tests for: integration — the pool migration of a variant-B pair (a repository
// on a code volume several dev services mount) through the full MCP server:
// the census on the volume's checkout, every dev service retired, then the
// code volume last (docs/spec-workflows.md §13 PM-13).

package integration_test

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/tools"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// poolMigrateIntVolumeSSH is the variant-B pair over SSH: every dev service
// mounts the volume at /var/www/ws and the volume's own container holds it at
// /data — both the one real repository; the census answers canned, the
// checkout's zerops.yaml is read from the repository.
type poolMigrateIntVolumeSSH struct {
	poolMigrateIntPartsSSH
}

func (s *poolMigrateIntVolumeSSH) rooted(command string) string {
	command = strings.ReplaceAll(command, "'/var/www/ws'", "'/var/www'")
	return strings.ReplaceAll(command, "'"+ops.CodeVolumeDataRoot+"'", "'/var/www'")
}

func (s *poolMigrateIntVolumeSSH) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	s.record("raw " + hostname + ": " + command)
	command = s.rooted(command)
	if strings.Contains(command, "zcp-code-volume-config 1") {
		s.record(hostname + ": " + command)
		out, err := s.local(ctx, command).CombinedOutput()
		if err != nil {
			return out, &platform.SSHExecError{Hostname: hostname, Output: string(out), Err: err}
		}
		return out, nil
	}
	return s.poolMigrateIntPartsSSH.ExecSSH(ctx, hostname, command)
}

// ExecSSHStdin answers one command as ExecSSH does; the input is not read.
func (s *poolMigrateIntVolumeSSH) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s *poolMigrateIntVolumeSSH) StreamSSH(ctx context.Context, hostname, command string, stdout io.Writer) error {
	return s.poolRetireIntSSH.StreamSSH(ctx, hostname, s.rooted(command), stdout)
}

// poolMigrateIntVolumeClient deletes for real, and a deleted service reads as
// the platform answers it by id: HTTP 400 serviceStackNotFound.
type poolMigrateIntVolumeClient struct {
	*poolRetireIntClient
}

func (c *poolMigrateIntVolumeClient) GetService(ctx context.Context, id string) (*platform.ServiceStack, error) {
	c.mu.Lock()
	gone := slices.Contains(c.deleted, id)
	c.mu.Unlock()
	if gone {
		pe := platform.NewPlatformError(platform.ErrAPIError, "Service stack not found.", "")
		pe.APICode = "serviceStackNotFound"
		return nil, pe
	}
	return c.Mock.GetService(ctx, id)
}

// TestPoolMigrate_Integration_VariantBRetireDeletesEveryDevServiceThenTheVolume:
// through the full MCP server a variant-B pair is a candidate — the census
// read on the volume's checkout through the dev's mount — and once both
// parts' pools are leased the retire diagnoses every dev service and the
// volume, then deletes appdev, apidev and, last, the volume by their ids,
// leaving one pool-keyed pair per part that names no code volume.
//
// non-parallel: the retire builds its bundle in the default scratch
// directory, which the fake dev resolves on this machine — as
// TestPoolMigrate_Integration_MultiPartRetireSplitsThePair's does.
func TestPoolMigrate_Integration_VariantBRetireDeletesEveryDevServiceThenTheVolume(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	stateDir := t.TempDir()
	const origin = "https://github.com/example/app.git"
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeStandard, PrimarySetupName: "dev",
		Stages:       []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}},
		CodeVolume:   &topology.CodeVolume{Hostname: "vol", MountPath: "/var/www/ws", DevServices: []topology.DevServiceRef{{Hostname: "apidev", SetupName: "dev-api"}}},
		GitPushState: topology.GitPushConfigured, RemoteURL: origin, BootstrappedAt: "2026-10-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	services := make([]platform.ServiceStack, 0, 7)
	services = append(services,
		poolMigrateIntService("appdev", "svc-dev", "nodejs@22"),
		poolMigrateIntService("apidev", "svc-api", "go@1"),
		poolMigrateIntService("vol", "svc-vol", "local-storage:single@1"),
		poolMigrateIntService("webstage", "svc-webstage", "nodejs@22"),
		poolMigrateIntService("apistage", "svc-apistage", "go@1"),
	)
	mock := platform.NewMock()
	for _, id := range []string{"svc-dev", "svc-api", "svc-vol"} {
		mock.WithProcess(&platform.Process{ID: "proc-delete-" + id, Status: platform.ProcessStatusFinished})
	}
	leases := filepath.Join(stateDir, "leases")
	if err := os.MkdirAll(leases, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, pool := range []string{"webpool", "apipool"} {
		if got := ops.SeedPoolDefaultRepo(stateDir, pool, origin); got.State != ops.PoolDefaultRepoSeeded {
			t.Fatalf("seed %s: %+v", pool, got)
		}
		record := `{"poolHostname":"` + pool + `","poolServiceId":"svc-` + pool + `","containerId":"c-` + pool + `","containerNumber":1,"containerHostname":"node-id-1.runtime.` + pool + `.zerops","lease":"l","branch":"main","repo":"` + origin + `","acquiredAt":"2026-10-24T09:30:00Z"}`
		if err := os.WriteFile(filepath.Join(leases, pool+"-1.json"), []byte(record), 0o600); err != nil {
			t.Fatal(err)
		}
		svc := poolMigrateIntService(pool, "svc-"+pool, "swarm@1")
		svc.ActiveAppVersion = &platform.ActiveAppVersionDigest{ID: "av-" + pool, Created: "2026-10-24T09:00:00Z", Source: "CLI", Built: true}
		services = append(services, svc)
		mock.WithServiceEnv("svc-"+pool, []platform.ServiceEnvVar{
			{ID: "p-api", Key: "API_TOKEN", Content: "pool-api-token", Type: platform.ServiceEnvUser},
			{ID: "p-ssh", Key: "sshIsolation", Content: "vpn service@zcp", Type: platform.ServiceEnvUser},
		})
	}
	mock.WithServices(services).WithServicesDirect(services)

	repo := filepath.Join(t.TempDir(), "www")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	poolRetireIntGit(t, repo, "init", "-q", "-b", "main")
	config := "zerops:\n  - setup: dev\n    run:\n      volume:\n        hostname: vol\n        mountPath: /var/www/ws\n  - setup: web\n    run:\n      base: nodejs@22\n"
	if err := os.WriteFile(filepath.Join(repo, "zerops.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	poolRetireIntGit(t, repo, "add", "-A")
	poolRetireIntGit(t, repo, "commit", "-q", "-m", "c1")

	client := &poolMigrateIntVolumeClient{&poolRetireIntClient{Mock: mock}}
	ssh := &poolMigrateIntVolumeSSH{poolMigrateIntPartsSSH{poolRetireIntSSH{repo: repo, work: filepath.Join(t.TempDir(), "zcp-pool-migrate")}}}
	srv := mcp.NewServer(&mcp.Implementation{Name: "zcp-test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(stateDir, workflow.EnvContainer, nil)
	swarm := tools.WithPoolOrchestratorsForWorkflow(func(baseURL, _ string) ops.SwarmAPI {
		for _, pool := range []string{"webpool", "apipool"} {
			if strings.Contains(baseURL, "."+pool+".") {
				return poolRetireIntPartsSwarm{pool: pool}
			}
		}
		return poolRetireIntSwarm{}
	})
	tools.RegisterWorkflow(srv, client, nil, "proj-1", nil, engine, nil, stateDir, "zcp", nil, ssh, runtime.Info{InContainer: true, ServiceName: "zcp"}, "", swarm)
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

	out, _ := poolRetireIntCall(t, session, map[string]any{"workflow": "pool-migrate", "service": "appdev"})
	if out["status"] != "step" || out["step"] != "ready-to-retire" {
		t.Fatalf("read side = %v, want ready-to-retire", out)
	}
	ssh.mu.Lock()
	census := ssh.commands[0]
	ssh.mu.Unlock()
	if !strings.HasPrefix(census, "raw appdev: cd '/var/www/ws'") {
		t.Errorf("first ssh = %q, want the census on appdev's mount of the volume", census)
	}

	retire := map[string]any{"workflow": "pool-migrate", "action": "retire", "service": "appdev"}
	out, isErr := poolRetireIntCall(t, session, retire)
	if !isErr || out["code"] != platform.ErrDiagnosisRequired {
		t.Fatalf("diagnose = %v", out)
	}
	wd, _ := out["wouldDestroy"].(map[string]any)
	targets, _ := wd["targets"].([]any)
	if len(targets) != 3 || targets[0] != "appdev" || targets[1] != "apidev" || targets[2] != "vol" {
		t.Fatalf("targets = %v, want appdev, apidev and vol", targets)
	}
	retire["confirmDestructive"] = map[string]any{"operation": "pool-retire", "acknowledgedTargets": []string{"appdev", "apidev", "vol"}}
	out, isErr = poolRetireIntCall(t, session, retire)
	if isErr || out["step"] != "retired" {
		t.Fatalf("confirmed = %v, want retired", out)
	}
	if !slices.Equal(client.deleted, []string{"svc-dev", "svc-api", "svc-vol"}) {
		t.Fatalf("deleted %q, want appdev, apidev, then the volume, by their ids", client.deleted)
	}
	for stage, pool := range map[string]string{"webstage": "webpool", "apistage": "apipool"} {
		meta, err := workflow.FindServiceMeta(stateDir, stage)
		if err != nil || meta == nil || meta.Hostname != pool || !meta.DevIsPool() || meta.CodeVolume != nil {
			t.Fatalf("%s's pair = %+v, want it keyed by %s with no code volume", stage, meta, pool)
		}
	}
}
