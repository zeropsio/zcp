// Tests for: tools/mount.go — zerops_mount MCP tool handler.
package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// stubMounter is a minimal Mounter for tool-layer tests.
type stubMounter struct {
	states   map[string]platform.MountState
	writable map[string]bool
	mountErr error
}

func newStubMounter() *stubMounter {
	return &stubMounter{
		states:   make(map[string]platform.MountState),
		writable: make(map[string]bool),
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

func mountServer(mock platform.Client, mounter ops.Mounter) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterMount(srv, mock, "proj-1", mounter)
	return srv
}

func TestMountTool_Mount(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "app"},
	})
	mounter := newStubMounter()
	srv := mountServer(mock, mounter)

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
	if parsed.Status != "MOUNTED" {
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
	srv := mountServer(mock, mounter)

	result := callTool(t, srv, "zerops_mount", map[string]any{
		"action":          "mount",
		"serviceHostname": "nonexistent",
	})

	if !result.IsError {
		t.Error("expected IsError for nonexistent service")
	}
}
