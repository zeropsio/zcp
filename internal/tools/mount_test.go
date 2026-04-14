// Tests for: tools/mount.go — zerops_mount MCP tool handler.
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// stubMounter is a minimal Mounter for tool-layer tests.
type stubMounter struct {
	states   map[string]platform.MountState
	writable map[string]bool
	mountErr error
	units    map[string]bool
}

func newStubMounter() *stubMounter {
	return &stubMounter{
		states:   make(map[string]platform.MountState),
		writable: make(map[string]bool),
		units:    make(map[string]bool),
	}
}

func (s *stubMounter) CheckMount(_ context.Context, path string) (platform.MountState, error) {
	state, ok := s.states[path]
	if !ok {
		return platform.MountStateNotMounted, nil
	}
	return state, nil
}

func (s *stubMounter) Mount(_ context.Context, _, localPath string) error {
	if s.mountErr != nil {
		return s.mountErr
	}
	s.states[localPath] = platform.MountStateActive
	return nil
}

func (s *stubMounter) Unmount(_ context.Context, _, path string) error {
	delete(s.states, path)
	return nil
}

func (s *stubMounter) ForceUnmount(_ context.Context, _, path string) error {
	delete(s.states, path)
	return nil
}

func (s *stubMounter) IsWritable(_ context.Context, path string) (bool, error) {
	return s.writable[path], nil
}

func (s *stubMounter) ListMountDirs(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (s *stubMounter) HasUnit(_ context.Context, hostname string) (bool, error) {
	return s.units[hostname], nil
}

func (s *stubMounter) CleanupUnit(_ context.Context, _ string) error {
	return nil
}

func mountServer(mock platform.Client, mounter ops.Mounter) *mcp.Server {
	return mountServerWithRT(mock, mounter, runtime.Info{}, nil)
}

func mountServerWithRT(mock platform.Client, mounter ops.Mounter, rtInfo runtime.Info, engine *workflow.Engine) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterMount(srv, mock, "proj-1", mounter, rtInfo, "", engine)
	return srv
}

// mountServerWithDevelop creates a server with a develop marker for workflow context tests.
func mountServerWithDevelop(t *testing.T, mock platform.Client, mounter ops.Mounter) *mcp.Server {
	t.Helper()
	stateDir := t.TempDir()
	if err := workflow.WriteDevelopMarker(stateDir, "proj-1", "test"); err != nil {
		t.Fatalf("write develop marker: %v", err)
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterMount(srv, mock, "proj-1", mounter, runtime.Info{}, stateDir, nil)
	return srv
}

// mountServerWithSession creates a server with an active bootstrap session for workflow context tests.
func mountServerWithSession(t *testing.T, mock platform.Client, mounter ops.Mounter) *mcp.Server {
	t.Helper()
	stateDir := t.TempDir()
	engine := workflow.NewEngine(stateDir, workflow.EnvContainer, nil)
	if _, err := engine.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("bootstrap start: %v", err)
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterMount(srv, mock, "proj-1", mounter, runtime.Info{}, stateDir, engine)
	return srv
}

func TestMountTool_Mount(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "app"},
	})
	mounter := newStubMounter()
	srv := mountServerWithDevelop(t, mock, mounter)

	result := callTool(t, srv, "zerops_mount", map[string]any{
		"action":          "mount",
		"serviceHostname": "app",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}

	var parsed ops.MountResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if parsed.Status != mountStatusMounted {
		t.Errorf("status = %s, want MOUNTED", parsed.Status)
	}
}

func TestMountTool_Unmount(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "app"},
	})
	mounter := newStubMounter()
	mounter.states["/var/www/app"] = platform.MountStateActive
	srv := mountServer(mock, mounter)

	result := callTool(t, srv, "zerops_mount", map[string]any{
		"action":          "unmount",
		"serviceHostname": "app",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}

	var parsed ops.MountResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if parsed.Status != "UNMOUNTED" {
		t.Errorf("status = %s, want UNMOUNTED", parsed.Status)
	}
}

func TestMountTool_UnmountStale(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "app"},
	})
	mounter := newStubMounter()
	mounter.states["/var/www/app"] = platform.MountStateStale
	srv := mountServer(mock, mounter)

	result := callTool(t, srv, "zerops_mount", map[string]any{
		"action":          "unmount",
		"serviceHostname": "app",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}

	var parsed ops.MountResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if parsed.Status != "UNMOUNTED" {
		t.Errorf("status = %s, want UNMOUNTED", parsed.Status)
	}
}

func TestMountTool_Status(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "app"},
		{ID: "svc-2", Name: "worker"},
	})
	mounter := newStubMounter()
	mounter.states["/var/www/app"] = platform.MountStateActive
	srv := mountServer(mock, mounter)

	result := callTool(t, srv, "zerops_mount", map[string]any{
		"action": "status",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}

	var parsed ops.MountStatusResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if len(parsed.Mounts) != 2 {
		t.Fatalf("mounts count = %d, want 2", len(parsed.Mounts))
	}
}

func TestMountTool_StatusStale(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "app"},
	})
	mounter := newStubMounter()
	mounter.states["/var/www/app"] = platform.MountStateStale
	srv := mountServer(mock, mounter)

	result := callTool(t, srv, "zerops_mount", map[string]any{
		"action": "status",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}

	var parsed ops.MountStatusResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if len(parsed.Mounts) != 1 {
		t.Fatalf("mounts count = %d, want 1", len(parsed.Mounts))
	}
	m := parsed.Mounts[0]
	if m.Mounted {
		t.Error("stale mount should report mounted=false")
	}
	if !m.Stale {
		t.Error("stale mount should report stale=true")
	}
}

func TestMountTool_NoAction(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock()
	mounter := newStubMounter()
	srv := mountServer(mock, mounter)

	err := callToolMayError(t, srv, "zerops_mount", map[string]any{})
	if err == nil {
		t.Error("expected error for missing action")
	}
}

func TestMountTool_InvalidAction(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock()
	mounter := newStubMounter()
	srv := mountServer(mock, mounter)

	result := callTool(t, srv, "zerops_mount", map[string]any{
		"action": "invalid",
	})

	if !result.IsError {
		t.Error("expected IsError for invalid action")
	}
}

func TestMountTool_ServiceNotFound(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "app"},
	})
	mounter := newStubMounter()
	srv := mountServerWithSession(t, mock, mounter)

	result := callTool(t, srv, "zerops_mount", map[string]any{
		"action":          "mount",
		"serviceHostname": "nonexistent",
	})

	if !result.IsError {
		t.Error("expected IsError for nonexistent service")
	}
}

func TestMountTool_UnmountOrphanUnit(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "app"},
	})
	mounter := newStubMounter()
	mounter.units["app"] = true
	// No FUSE mount — state is NotMounted by default.
	srv := mountServer(mock, mounter)

	result := callTool(t, srv, "zerops_mount", map[string]any{
		"action":          "unmount",
		"serviceHostname": "app",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}

	var parsed ops.MountResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if parsed.Status != "UNIT_CLEANED" {
		t.Errorf("status = %s, want UNIT_CLEANED", parsed.Status)
	}
}

func TestMountTool_MountAllowedWithDevelopMarker(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "app"},
	})
	mounter := newStubMounter()
	srv := mountServerWithDevelop(t, mock, mounter)

	result := callTool(t, srv, "zerops_mount", map[string]any{
		"action":          "mount",
		"serviceHostname": "app",
	})

	if result.IsError {
		t.Errorf("mount should succeed with develop marker, got: %s", getTextContent(t, result))
	}
}

func TestMountTool_MountAllowedWithBootstrapSession(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "app"},
	})
	mounter := newStubMounter()
	srv := mountServerWithSession(t, mock, mounter)

	result := callTool(t, srv, "zerops_mount", map[string]any{
		"action":          "mount",
		"serviceHostname": "app",
	})

	if result.IsError {
		t.Errorf("mount should succeed with bootstrap session, got: %s", getTextContent(t, result))
	}
}

func TestMountTool_MountBlockedWithoutWorkflow(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "app"},
	})
	mounter := newStubMounter()
	// stateDir with no develop marker and nil engine = no workflow context.
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterMount(srv, mock, "proj-1", mounter, runtime.Info{}, t.TempDir(), nil)

	result := callTool(t, srv, "zerops_mount", map[string]any{
		"action":          "mount",
		"serviceHostname": "app",
	})

	if !result.IsError {
		t.Error("mount should be blocked without workflow context")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "workflow") {
		t.Errorf("error should mention workflow, got: %s", text)
	}
}

func TestMountTool_StatusAllowedWithoutWorkflow(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "app"},
	})
	mounter := newStubMounter()
	// No engine — status should still work.
	srv := mountServer(mock, mounter)

	result := callTool(t, srv, "zerops_mount", map[string]any{
		"action": "status",
	})

	if result.IsError {
		t.Errorf("status action should work without workflow, got error: %s", getTextContent(t, result))
	}
}

func TestMountTool_SelfMount_Blocked(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "zcpx"},
	})
	mounter := newStubMounter()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvContainer, nil)
	if _, err := engine.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("bootstrap start: %v", err)
	}

	srv := mountServerWithRT(mock, mounter, runtime.Info{
		InContainer: true,
		ServiceName: "zcpx",
	}, engine)

	result := callTool(t, srv, "zerops_mount", map[string]any{
		"action":          "mount",
		"serviceHostname": "zcpx",
	})

	if !result.IsError {
		t.Fatal("expected self-mount to be blocked")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, platform.ErrSelfServiceBlocked) {
		t.Errorf("expected error code %s, got: %s", platform.ErrSelfServiceBlocked, text)
	}
}

func TestMountTool_SelfMount_CaseInsensitive(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "zcpx"},
	})
	mounter := newStubMounter()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvContainer, nil)
	if _, err := engine.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("bootstrap start: %v", err)
	}

	srv := mountServerWithRT(mock, mounter, runtime.Info{
		InContainer: true,
		ServiceName: "zcpx",
	}, engine)

	result := callTool(t, srv, "zerops_mount", map[string]any{
		"action":          "mount",
		"serviceHostname": "ZCPX",
	})

	if !result.IsError {
		t.Fatal("expected case-insensitive self-mount to be blocked")
	}
}

func TestMountTool_SelfMount_LocalDev_Allowed(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "zcpx"},
	})
	mounter := newStubMounter()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvContainer, nil)
	if _, err := engine.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("bootstrap start: %v", err)
	}

	// InContainer=false: guard should not activate.
	srv := mountServerWithRT(mock, mounter, runtime.Info{}, engine)

	result := callTool(t, srv, "zerops_mount", map[string]any{
		"action":          "mount",
		"serviceHostname": "zcpx",
	})

	if result.IsError {
		t.Errorf("local dev mount should succeed, got: %s", getTextContent(t, result))
	}
}

func TestMountTool_SelfUnmount_Allowed(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "zcpx"},
	})
	mounter := newStubMounter()

	// Even in container, unmount of self should be allowed (cleanup).
	srv := mountServerWithRT(mock, mounter, runtime.Info{
		InContainer: true,
		ServiceName: "zcpx",
	}, nil)

	result := callTool(t, srv, "zerops_mount", map[string]any{
		"action":          "unmount",
		"serviceHostname": "zcpx",
	})

	if result.IsError {
		t.Errorf("self-unmount should be allowed, got: %s", getTextContent(t, result))
	}
}

func TestMountTool_SelfStatus_Allowed(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "zcpx"},
	})
	mounter := newStubMounter()

	// Even in container, status of self should be allowed.
	srv := mountServerWithRT(mock, mounter, runtime.Info{
		InContainer: true,
		ServiceName: "zcpx",
	}, nil)

	result := callTool(t, srv, "zerops_mount", map[string]any{
		"action":          "status",
		"serviceHostname": "zcpx",
	})

	if result.IsError {
		t.Errorf("self-status should be allowed, got: %s", getTextContent(t, result))
	}
}
