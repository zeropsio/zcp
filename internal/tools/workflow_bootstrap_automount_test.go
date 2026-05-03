// Tests for: workflow_bootstrap.go::autoMountTargets — post-mount git init hook.
//
// Focus is on GLC-1: every successfully-mounted runtime service reaches
// InitServiceGit with the canonical command. Mount failures short-circuit
// the git init (wrong target state); nil sshDeployer (local env) short-
// circuits everything because the outer mounter guard fires first.
package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// mountRecorder satisfies ops.Mounter with configurable per-hostname Mount
// failures. Distinct from stubMounter/newStubMounter elsewhere in the tools
// package because those don't surface the hostname on Mount errors — we need
// per-host failure control to assert the skip-on-mount-failure branch.
type mountRecorder struct {
	mountErrByHost map[string]error
}

func (m *mountRecorder) CheckMount(_ context.Context, _ string) (platform.MountState, error) {
	return platform.MountStateNotMounted, nil
}

func (m *mountRecorder) Mount(_ context.Context, hostname, _ string) error {
	if m.mountErrByHost != nil {
		if err, ok := m.mountErrByHost[hostname]; ok {
			return err
		}
	}
	return nil
}

func (m *mountRecorder) Unmount(_ context.Context, _, _ string) error      { return nil }
func (m *mountRecorder) ForceUnmount(_ context.Context, _, _ string) error { return nil }
func (m *mountRecorder) IsWritable(_ context.Context, _ string) (bool, error) {
	return true, nil
}
func (m *mountRecorder) ListMountDirs(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (m *mountRecorder) HasUnit(_ context.Context, _ string) (bool, error) { return false, nil }
func (m *mountRecorder) CleanupUnit(_ context.Context, _ string) error     { return nil }

// sshRecorder records every ExecSSH command so tests can verify exactly
// which hostnames got InitServiceGit and what shell payload landed on each.
// Distinct from stubSSH elsewhere in the package (that one doesn't record).
type sshRecorder struct {
	calls     []struct{ Host, Cmd string }
	errByHost map[string]error
}

func (s *sshRecorder) ExecSSH(_ context.Context, hostname, command string) ([]byte, error) {
	s.calls = append(s.calls, struct{ Host, Cmd string }{hostname, command})
	if s.errByHost != nil {
		if err, ok := s.errByHost[hostname]; ok {
			return nil, err
		}
	}
	return nil, nil
}

func (s *sshRecorder) ExecSSHBackground(_ context.Context, _, _ string, _ time.Duration) ([]byte, error) {
	return nil, nil
}

// seedBootstrapPlan drives the engine through start + plan submission so
// state.Bootstrap.Plan.Targets is populated for autoMountTargets to iterate.
func seedBootstrapPlan(t *testing.T, eng *workflow.Engine, targets []workflow.BootstrapTarget) {
	t.Helper()
	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	if _, err := eng.BootstrapCompletePlan(targets, nil, nil); err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}
}

func TestAutoMountTargets_CallsInitServiceGit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	seedBootstrapPlan(t, eng, []workflow.BootstrapTarget{
		{Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "standard", ExplicitStage: "appstage"}},
		{Runtime: workflow.RuntimeTarget{DevHostname: "workerdev", Type: "nodejs@22", BootstrapMode: "standard", ExplicitStage: "workerstage"}},
	})

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-app", Name: "appdev"},
		{ID: "svc-worker", Name: "workerdev"},
	})
	mounter := &mountRecorder{}
	ssh := &sshRecorder{}

	results := autoMountTargets(context.Background(), mock, "proj-1", mounter, ssh, eng)

	if len(results) != 2 {
		t.Fatalf("expected 2 mount infos, got %d", len(results))
	}
	for _, r := range results {
		if r.Status == "FAILED" {
			t.Errorf("%s unexpectedly failed: %s", r.Hostname, r.Error)
		}
	}

	if len(ssh.calls) != 2 {
		t.Fatalf("expected 2 InitServiceGit calls, got %d", len(ssh.calls))
	}
	wantHosts := map[string]bool{"appdev": false, "workerdev": false}
	for _, c := range ssh.calls {
		if _, ok := wantHosts[c.Host]; !ok {
			t.Errorf("unexpected host %q in ssh calls", c.Host)
			continue
		}
		wantHosts[c.Host] = true
		for _, want := range []string{
			"cd /var/www",
			"test -d .git || git init -q -b main",
			"git config user.email 'agent@zerops.io'",
			"git config user.name 'Zerops Agent'",
		} {
			if !strings.Contains(c.Cmd, want) {
				t.Errorf("%s command missing %q\nfull: %s", c.Host, want, c.Cmd)
			}
		}
	}
	for host, seen := range wantHosts {
		if !seen {
			t.Errorf("expected InitServiceGit call for %q, none recorded", host)
		}
	}
}

// Failed mounts never reach InitServiceGit — the continue guard after
// mountErr keeps us from trying to SSH into an unmounted service.
func TestAutoMountTargets_SkipsInitOnMountFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	seedBootstrapPlan(t, eng, []workflow.BootstrapTarget{
		{Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "standard", ExplicitStage: "appstage"}},
		{Runtime: workflow.RuntimeTarget{DevHostname: "workerdev", Type: "nodejs@22", BootstrapMode: "standard", ExplicitStage: "workerstage"}},
	})

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-app", Name: "appdev"},
		{ID: "svc-worker", Name: "workerdev"},
	})
	mounter := &mountRecorder{mountErrByHost: map[string]error{
		"workerdev": errors.New("mount: connection refused"),
	}}
	ssh := &sshRecorder{}

	results := autoMountTargets(context.Background(), mock, "proj-1", mounter, ssh, eng)
	if len(results) != 2 {
		t.Fatalf("expected 2 mount infos, got %d", len(results))
	}
	if len(ssh.calls) != 1 {
		t.Fatalf("expected 1 InitServiceGit call (appdev only), got %d", len(ssh.calls))
	}
	if ssh.calls[0].Host != "appdev" {
		t.Errorf("SSH call should target appdev, got %q", ssh.calls[0].Host)
	}
}

// Nil sshDeployer must not panic — explicit guard blocks the init attempt.
// Mount results still come back normally.
func TestAutoMountTargets_NilSSHDeployer(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	seedBootstrapPlan(t, eng, []workflow.BootstrapTarget{
		{Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "standard", ExplicitStage: "appstage"}},
	})

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-app", Name: "appdev"},
	})
	mounter := &mountRecorder{}

	results := autoMountTargets(context.Background(), mock, "proj-1", mounter, nil, eng)
	if len(results) != 1 {
		t.Fatalf("expected 1 mount info, got %d", len(results))
	}
	if results[0].Status == "FAILED" {
		t.Errorf("mount should succeed without ssh deployer: %s", results[0].Error)
	}
}

// Compile-time guard: ensure stubs keep satisfying the target interfaces.
var (
	_ ops.Mounter     = (*mountRecorder)(nil)
	_ ops.SSHDeployer = (*sshRecorder)(nil)
)
