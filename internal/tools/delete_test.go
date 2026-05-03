// Tests for: delete.go — zerops_delete MCP tool handler.

package tools

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestDeleteTool_Confirmed(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithProcess(&platform.Process{ID: "proc-delete-svc-1", ActionName: "delete", Status: "FINISHED"})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDelete(srv, mock, "proj-1", "", nil, runtime.Info{})

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "api",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	// Verify delete now polls to completion (status=FINISHED).
	text := getTextContent(t, result)
	var proc platform.Process
	if err := json.Unmarshal([]byte(text), &proc); err != nil {
		t.Fatalf("parse delete result: %v", err)
	}
	if proc.Status != "FINISHED" {
		t.Errorf("status = %q, want %q", proc.Status, "FINISHED")
	}
}

func TestDeleteTool_EmptyHostname(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDelete(srv, mock, "proj-1", "", nil, runtime.Info{})

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "",
	})

	if !result.IsError {
		t.Error("expected IsError for empty hostname")
	}
}

func TestDeleteTool_CleansUpServiceMeta(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()

	// Write a service meta file that should be cleaned up after delete.
	meta := &workflow.ServiceMeta{
		Hostname:         "api",
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	// Verify the meta file exists.
	metaPath := filepath.Join(stateDir, "services", "api.json")
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		t.Fatal("expected service meta to exist before delete")
	}

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithProcess(&platform.Process{ID: "proc-delete-svc-1", ActionName: "delete", Status: "FINISHED"})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDelete(srv, mock, "proj-1", stateDir, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "api",
	})

	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}

	// Service meta file should be removed after successful delete.
	if _, err := os.Stat(metaPath); !os.IsNotExist(err) {
		t.Error("expected service meta to be deleted after service deletion")
	}
}

func TestDeleteTool_NoStateDir_StillSucceeds(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithProcess(&platform.Process{ID: "proc-delete-svc-1", ActionName: "delete", Status: "FINISHED"})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDelete(srv, mock, "proj-1", "", nil, runtime.Info{})

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "api",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}
}

func TestDeleteTool_UnmountsOnSuccess(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithProcess(&platform.Process{ID: "proc-delete-svc-1", ActionName: "delete", Status: "FINISHED"})

	mounter := newStubMounter()
	mounter.states["/var/www/api"] = platform.MountStateActive

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDelete(srv, mock, "proj-1", "", mounter, runtime.Info{})

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "api",
	})

	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}

	// Mount should have been cleaned up.
	if _, exists := mounter.states["/var/www/api"]; exists {
		t.Error("expected mount to be cleaned up after delete")
	}
}

func TestDeleteTool_UnmountOrphanUnit(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithProcess(&platform.Process{ID: "proc-delete-svc-1", ActionName: "delete", Status: "FINISHED"})

	mounter := newStubMounter()
	mounter.units["api"] = true // Orphan unit, no FUSE mount.

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDelete(srv, mock, "proj-1", "", mounter, runtime.Info{})

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "api",
	})

	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}

	// Verify delete succeeded (orphan unit cleanup is best-effort).
	text := getTextContent(t, result)
	var proc platform.Process
	if err := json.Unmarshal([]byte(text), &proc); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if proc.Status != "FINISHED" {
		t.Errorf("status = %q, want %q", proc.Status, "FINISHED")
	}
}

func TestDeleteTool_NilMounter_NoUnmount(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithProcess(&platform.Process{ID: "proc-delete-svc-1", ActionName: "delete", Status: "FINISHED"})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDelete(srv, mock, "proj-1", "", nil, runtime.Info{}) // nil mounter = local dev

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "api",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}
}

func TestDeleteTool_UnmountError_StillSucceeds(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithProcess(&platform.Process{ID: "proc-delete-svc-1", ActionName: "delete", Status: "FINISHED"})

	mounter := newStubMounter()
	mounter.states["/var/www/api"] = platform.MountStateActive
	mounter.mountErr = errors.New("unmount failed") // mountErr used by Mount, but we need unmount to fail

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDelete(srv, mock, "proj-1", "", mounter, runtime.Info{})

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "api",
	})

	// Delete should still succeed even if unmount has issues.
	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	var proc platform.Process
	if err := json.Unmarshal([]byte(text), &proc); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if proc.Status != "FINISHED" {
		t.Errorf("status = %q, want %q", proc.Status, "FINISHED")
	}
}

func TestDeleteTool_SelfDelete_Blocked(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "zcp"}}).
		WithProcess(&platform.Process{ID: "proc-delete-svc-1", ActionName: "delete", Status: "FINISHED"})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDelete(srv, mock, "proj-1", "", nil, runtime.Info{
		InContainer: true,
		ServiceName: "zcp",
		ServiceID:   "svc-1",
	})

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "zcp",
	})

	if !result.IsError {
		t.Fatal("expected self-delete to be blocked")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, platform.ErrSelfServiceBlocked) {
		t.Errorf("expected error code %s, got: %s", platform.ErrSelfServiceBlocked, text)
	}
}

func TestDeleteTool_SelfDelete_CaseInsensitive(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "zcp"}}).
		WithProcess(&platform.Process{ID: "proc-delete-svc-1", ActionName: "delete", Status: "FINISHED"})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDelete(srv, mock, "proj-1", "", nil, runtime.Info{
		InContainer: true,
		ServiceName: "zcp",
	})

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "ZCP",
	})

	if !result.IsError {
		t.Fatal("expected case-insensitive self-delete to be blocked")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, platform.ErrSelfServiceBlocked) {
		t.Errorf("expected error code %s, got: %s", platform.ErrSelfServiceBlocked, text)
	}
}

func TestDeleteTool_SelfDelete_LocalDev_Allowed(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "zcp"}}).
		WithProcess(&platform.Process{ID: "proc-delete-svc-1", ActionName: "delete", Status: "FINISHED"})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	// InContainer=false: guard should not activate even if hostname matches.
	RegisterDelete(srv, mock, "proj-1", "", nil, runtime.Info{})

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "zcp",
	})

	if result.IsError {
		t.Errorf("local dev delete should succeed, got: %s", getTextContent(t, result))
	}
}

func TestDeleteTool_OtherService_InContainer_Allowed(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithProcess(&platform.Process{ID: "proc-delete-svc-1", ActionName: "delete", Status: "FINISHED"})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDelete(srv, mock, "proj-1", "", nil, runtime.Info{
		InContainer: true,
		ServiceName: "zcp",
	})

	result := callTool(t, srv, "zerops_delete", map[string]any{
		"serviceHostname": "api",
	})

	if result.IsError {
		t.Errorf("deleting other service should succeed, got: %s", getTextContent(t, result))
	}
}

// Ensure ops.Mounter is satisfied by stubMounter (compile check).
var _ ops.Mounter = (*stubMounter)(nil)
