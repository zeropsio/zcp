package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// recordingSSH answers like stubSSH and records every command per host.
type recordingSSH struct {
	mu    sync.Mutex
	calls map[string][]string
}

func (s *recordingSSH) ExecSSH(_ context.Context, host, command string) ([]byte, error) {
	s.mu.Lock()
	if s.calls == nil {
		s.calls = map[string][]string{}
	}
	s.calls[host] = append(s.calls[host], command)
	s.mu.Unlock()
	if strings.Contains(command, "rev-parse --verify HEAD") && strings.Contains(command, "git status --porcelain") {
		return nil, errStubSSHNoRepo
	}
	return []byte("ok"), nil
}

func (s *recordingSSH) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

// push is the zcli push command run on host, "" when none ran.
func (s *recordingSSH) push(host string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.calls[host] {
		if strings.Contains(c, "zcli push") {
			return c
		}
	}
	return ""
}

// writeVariantBPair records F2's pair under variant B: webdev edits, apidev
// runs the api part, both mount vol at /var/www/ws.
func writeVariantBPair(t *testing.T, stateDir string) {
	t.Helper()
	meta := &workflow.ServiceMeta{
		Hostname:         "webdev",
		Mode:             topology.PlanModeStandard,
		PrimarySetupName: "dev",
		Stages:           []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}},
		CodeVolume: &topology.CodeVolume{Hostname: "vol", MountPath: "/var/www/ws",
			DevServices: []topology.DevServiceRef{{Hostname: "apidev", SetupName: "dev-api"}}},
		BootstrappedAt: "2026-10-15T00:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}
}

func variantBServices() []platform.ServiceStack {
	stack := func(id, name, typ string) platform.ServiceStack {
		return platform.ServiceStack{ID: id, Name: name, Status: statusActive,
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: typ}}
	}
	return []platform.ServiceStack{
		stack("svc-web", "webdev", "bun@1.3"),
		stack("svc-api", "apidev", "go@1"),
		stack("svc-vol", "vol", "local-storage:single@1"),
		stack("svc-webstage", "webstage", "bun@1.3"),
		stack("svc-apistage", "apistage", "go@1"),
	}
}

func TestDeployTool_FromVariantBDevService_RunsWithWorkingDirOnTheVolume(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		args   map[string]any
		source string
	}{
		{name: "a further dev service deploys itself from the volume", args: map[string]any{"targetService": "apidev"}, source: "apidev"},
		{name: "the dev half deploys itself from the volume", args: map[string]any{"targetService": "webdev"}, source: "webdev"},
		{name: "a stage is built from the volume", args: map[string]any{"sourceService": "webdev", "targetService": "apistage", "setup": "api"}, source: "webdev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			writeVariantBPair(t, stateDir)
			services := variantBServices()
			events := make([]platform.AppVersionEvent, 0, len(services))
			for _, svc := range services {
				events = append(events, platform.AppVersionEvent{ID: "av-" + svc.ID, ProjectID: "proj-1", ServiceStackID: svc.ID, Status: statusActive, Sequence: 1})
			}
			mock := platform.NewMock().WithServices(services).WithAppVersionEvents(events)
			ssh := &recordingSSH{}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"},
				nil, runtime.Info{}, stateDir, testDeployEngine(t), nil, DevsupSource{})

			result := callTool(t, srv, "zerops_deploy", tt.args)

			push := ssh.push(tt.source)
			if push == "" {
				t.Fatalf("no push ran on %s: %s", tt.source, getTextContent(t, result))
			}
			if !strings.HasPrefix(push, "cd '/var/www/ws' &&") {
				t.Errorf("push on %s = %q, want it to run in /var/www/ws", tt.source, push)
			}
		})
	}
}

func TestDeployTool_FromPairWithoutCodeVolume_RunsInVarWww(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	setupAdoptedService(t, stateDir, "appdev", "appstage")
	mock := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "appdev"}, {ID: "svc-2", Name: "appstage"}}).
		WithAppVersionEvents([]platform.AppVersionEvent{{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-1", Status: statusActive, Sequence: 1}})
	ssh := &recordingSSH{}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"},
		nil, runtime.Info{}, stateDir, testDeployEngine(t), nil, DevsupSource{})

	callTool(t, srv, "zerops_deploy", map[string]any{"targetService": "appdev"})

	if push := ssh.push("appdev"); !strings.HasPrefix(push, "cd '/var/www' &&") {
		t.Errorf("push = %q, want it to run in /var/www", push)
	}
}

func TestDeployPreFlight_FromVariantBDevService_ReadsTheYamlOnTheVolume(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(filepath.Join(dir, "apidev", "ws"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeVariantBPair(t, stateDir)
	yml := "zerops:\n  - setup: dev-api\n    build:\n      base: go@1\n      deployFiles: [.]\n    run:\n      start: go run ./api\n      ports:\n        - port: 8080\n"
	if err := os.WriteFile(filepath.Join(dir, "apidev", "ws", "zerops.yaml"), []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}

	resolved, result, err := deployPreFlight(context.Background(), platform.NewMock(), "proj-1", stateDir, "apidev", "apidev", "",
		sourceWorkingDir(stateDir, "apidev", ""), true)

	if err != nil || result == nil || !result.Passed {
		t.Fatalf("preflight = %+v, %v; want it to pass on the volume's yaml", result, err)
	}
	if resolved.setup != "dev-api" {
		t.Errorf("resolved setup = %q, want dev-api", resolved.setup)
	}
}

// fillingSSH answers the code-volume fill like a container whose volume is
// empty, then records everything like recordingSSH.
type fillingSSH struct{ recordingSSH }

func (s *fillingSSH) ExecSSH(ctx context.Context, host, command string) ([]byte, error) {
	out, err := s.recordingSSH.ExecSSH(ctx, host, command)
	if strings.Contains(command, "@@zcp code-volume") && !strings.Contains(command, "zcp-devsup") {
		return []byte("@@zcp code-volume filled\n"), nil
	}
	if strings.Contains(command, "ctl 'reload'") {
		return []byte("{}\n@@zcp-devsup ctl 0\n"), nil
	}
	return out, err
}

// TestDeployTool_OfAVariantBDevHalf_FillsTheCodeVolumeOnceAndReloadsTheSupervisors
// (Decision: B is recognised from topology on every route): the deploy that
// mounts the code volume on the dev half also puts the repository on it and
// reloads the supervisor of every dev service of the pair, so the adopt route
// never leaves the volume to a hand copy. A further dev service's deploy
// fills nothing.
func TestDeployTool_OfAVariantBDevHalf_FillsTheCodeVolumeOnceAndReloadsTheSupervisors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		args       map[string]any
		wantFill   bool
		wantReload []string
	}{
		{name: "the dev half fills and reloads", args: map[string]any{"targetService": "webdev", "workingDir": "/var/www"}, wantFill: true, wantReload: []string{"webdev", "apidev"}},
		{name: "a further dev service fills nothing", args: map[string]any{"sourceService": "webdev", "targetService": "apidev", "setup": "dev-api"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			writeVariantBPair(t, stateDir)
			services := variantBServices()
			events := make([]platform.AppVersionEvent, 0, len(services))
			for _, svc := range services {
				events = append(events, platform.AppVersionEvent{ID: "av-" + svc.ID, ProjectID: "proj-1", ServiceStackID: svc.ID, Status: statusActive, Sequence: 1})
			}
			mock := platform.NewMock().WithServices(services).WithAppVersionEvents(events)
			ssh := &fillingSSH{}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"},
				nil, runtime.Info{}, stateDir, testDeployEngine(t), nil, DevsupSource{})

			result := callTool(t, srv, "zerops_deploy", tt.args)

			var fills int
			reloaded := map[string]bool{}
			for host, cmds := range ssh.calls {
				for _, c := range cmds {
					if strings.Contains(c, "@@zcp code-volume filled") && strings.Contains(c, "'/var/www/ws'") {
						fills++
					}
					if strings.Contains(c, "ctl 'reload'") {
						reloaded[host] = true
					}
				}
			}
			if (fills == 1) != tt.wantFill || (!tt.wantFill && fills != 0) {
				t.Errorf("fills = %d, want fill %v: %s", fills, tt.wantFill, getTextContent(t, result))
			}
			for _, h := range tt.wantReload {
				if !reloaded[h] {
					t.Errorf("no supervisor reload on %s: %v", h, reloaded)
				}
			}
			if !tt.wantFill && len(reloaded) != 0 {
				t.Errorf("reloaded %v without a fill", reloaded)
			}
		})
	}
}
