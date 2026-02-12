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
	mounted  map[string]bool
	writable map[string]bool
	mountErr error
}

func newStubMounter() *stubMounter {
	return &stubMounter{
		mounted:  make(map[string]bool),
		writable: make(map[string]bool),
	}
}

func (s *stubMounter) IsMounted(_ context.Context, path string) (bool, error) {
	return s.mounted[path], nil
}

func (s *stubMounter) Mount(_ context.Context, _, localPath string) error {
	if s.mountErr != nil {
		return s.mountErr
	}
	s.mounted[localPath] = true
	return nil
}

func (s *stubMounter) Unmount(_ context.Context, _, path string) error {
	delete(s.mounted, path)
	return nil
}

func (s *stubMounter) IsWritable(_ context.Context, path string) (bool, error) {
	return s.writable[path], nil
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
	mounter.mounted["/var/www/app"] = true
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
	mounter.mounted["/var/www/app"] = true
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
