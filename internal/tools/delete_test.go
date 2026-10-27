// Tests for: delete.go — zerops_delete MCP tool handler.

package tools

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
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
		"consent":         "api",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	// Verify delete now polls to completion (status=FINISHED).
	text := getTextContent(t, result)
	var resp deleteResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("parse delete result: %v", err)
	}
	if resp.Process == nil || resp.Process.Status != "FINISHED" {
		t.Errorf("process status = %v, want FINISHED", resp.Process)
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
		"consent":         "api",
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
		"consent":         "api",
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
		"consent":         "api",
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
		"consent":         "api",
	})

	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}

	// Verify delete succeeded (orphan unit cleanup is best-effort).
	text := getTextContent(t, result)
	var resp deleteResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if resp.Process == nil || resp.Process.Status != "FINISHED" {
		t.Errorf("process status = %v, want FINISHED", resp.Process)
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
		"consent":         "api",
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
		"consent":         "api",
	})

	// Delete should still succeed even if unmount has issues.
	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	var resp deleteResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if resp.Process == nil || resp.Process.Status != "FINISHED" {
		t.Errorf("process status = %v, want FINISHED", resp.Process)
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
		"consent":         "zcp",
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
		"consent":         "zcp",
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
		"consent":         "api",
	})

	if result.IsError {
		t.Errorf("deleting other service should succeed, got: %s", getTextContent(t, result))
	}
}

// Ensure ops.Mounter is satisfied by stubMounter (compile check).
var _ ops.Mounter = (*stubMounter)(nil)

// Deleting one preview of a pair of several leaves the pair: the stage
// leaves its Stages, the pair's meta and its other stages stay, and the
// answer names the call that proposes the part's removal from the group
// recipe's tiers. A pair's only stage keeps today's answer.
func TestDeleteTool_StageOfSeveral_LeavesThePairAndNamesTheRecipeCall(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		stages     []topology.StageRef
		delete     string
		wantStages []string
		wantSteer  bool
	}{
		{
			name:       "one stage of three",
			stages:     []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}, {Hostname: "workerstage", SetupName: "worker"}},
			delete:     "workerstage",
			wantStages: []string{"webstage", "apistage"},
			wantSteer:  true,
		},
		{
			name:       "the only stage",
			stages:     []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}},
			delete:     "appstage",
			wantStages: []string{"appstage"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			meta := &workflow.ServiceMeta{
				Hostname: "appdev", Mode: topology.PlanModeStandard, BootstrapSession: "s1", BootstrappedAt: "2026-10-14T12:00:00Z",
				Stages: tt.stages, HQ: &workflow.HQRepoRef{AppID: "app-1", Repo: "appdev"},
			}
			if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
				t.Fatalf("WriteServiceMeta: %v", err)
			}
			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{{ID: "svc-1", Name: tt.delete}}).
				WithProcess(&platform.Process{ID: "proc-delete-svc-1", ActionName: "delete", Status: "FINISHED"})
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterDelete(srv, mock, "proj-1", stateDir, nil, runtime.Info{})

			result := callTool(t, srv, "zerops_delete", map[string]any{"serviceHostname": tt.delete, "consent": tt.delete})
			text := getTextContent(t, result)
			if result.IsError {
				t.Fatalf("unexpected IsError: %s", text)
			}
			got, err := workflow.ReadServiceMeta(stateDir, "appdev")
			if err != nil || got == nil {
				t.Fatalf("the pair's meta is gone (%v)", err)
			}
			if hosts := got.StageHostnames(); !slices.Equal(hosts, tt.wantStages) {
				t.Errorf("stages = %v, want %v", hosts, tt.wantStages)
			}
			steer := `action=\"group-recipe\" service=\"` + tt.delete + `\"`
			if strings.Contains(text, steer) != tt.wantSteer {
				t.Errorf("answer %s: carries the recipe call %v, want %v", text, !tt.wantSteer, tt.wantSteer)
			}
			if tt.wantSteer && (!strings.Contains(text, "as the Mate's recipe change") || strings.Contains(text, "the person reviews")) {
				t.Errorf("answer %s: the recipe call does not name the Mate's recipe change", text)
			}
		})
	}
}

// TestRemovingAPartDeletesItsPreviewOnlyAfterExplicitConsent: zcp never
// deletes a service without the person's explicit consent. The agent asks the
// person; zerops_delete refuses a call that does not carry that consent for
// the service by name, deletes nothing and leaves the pair's parts as they
// were. With it, the preview goes and the pair keeps its other parts.
func TestRemovingAPartDeletesItsPreviewOnlyAfterExplicitConsent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		consent    string
		wantStages []string
		deleted    bool
	}{
		{name: "without consent", wantStages: []string{"webstage", "apistage"}},
		{name: "with consent to another service", consent: "apistage", wantStages: []string{"webstage", "apistage"}},
		{name: "with consent to this preview", consent: "webstage", wantStages: []string{"apistage"}, deleted: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
				Hostname: "appdev", Mode: topology.PlanModeStandard, BootstrapSession: "s1", BootstrappedAt: "2026-10-15T00:00:00Z",
				Stages: []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}},
			}); err != nil {
				t.Fatalf("WriteServiceMeta: %v", err)
			}
			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "webstage"}, {ID: "svc-2", Name: "apistage"}}).
				WithProcess(&platform.Process{ID: "proc-delete-svc-1", ActionName: "delete", Status: "FINISHED"})
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterDelete(srv, mock, "proj-1", stateDir, nil, runtime.Info{})

			args := map[string]any{"serviceHostname": "webstage"}
			if tt.consent != "" {
				args["consent"] = tt.consent
			}
			result := callTool(t, srv, "zerops_delete", args)
			text := getTextContent(t, result)

			if got := mock.CallCounts["DeleteService"] == 1; got != tt.deleted {
				t.Errorf("webstage deleted = %v, want %v: %s", got, tt.deleted, text)
			}
			if result.IsError == tt.deleted {
				t.Errorf("IsError = %v, want %v: %s", result.IsError, !tt.deleted, text)
			}
			if !tt.deleted {
				for _, want := range []string{platform.ErrConsentRequired, "webstage", `consent=\"webstage\"`} {
					if !strings.Contains(text, want) {
						t.Errorf("the refusal misses %q: %s", want, text)
					}
				}
			}
			got, err := workflow.ReadServiceMeta(stateDir, "appdev")
			if err != nil || got == nil {
				t.Fatalf("the pair's meta is gone (%v)", err)
			}
			if hosts := got.StageHostnames(); !slices.Equal(hosts, tt.wantStages) {
				t.Errorf("stages = %v, want %v", hosts, tt.wantStages)
			}
		})
	}
}

// TestDeleteTool_RemovesTheServicesHQState: a deleted service leaves no HQ
// state behind (hq/<hostname>.json in the state directory, the pair's backoff
// record); another service's record stays.
func TestDeleteTool_RemovesTheServicesHQState(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{Hostname: "appdev", BootstrapSession: "s1", BootstrappedAt: "2026-10-14T12:00:00Z"}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	writeHQPairState(stateDir, "appdev", hqPairState{Attempts: 1, LastAttemptAt: "2026-10-14T12:00:00Z", LastOutcome: "wired"})
	writeHQPairState(stateDir, "webdev", hqPairState{Attempts: 1, LastAttemptAt: "2026-10-14T12:00:00Z", LastOutcome: "wired"})

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "appdev"}}).
		WithProcess(&platform.Process{ID: "proc-delete-svc-1", ActionName: "delete", Status: "FINISHED"})
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDelete(srv, mock, "proj-1", stateDir, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_delete", map[string]any{"serviceHostname": "appdev", "consent": "appdev"})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}
	if _, err := os.Stat(filepath.Join(stateDir, "hq", "appdev.json")); !os.IsNotExist(err) {
		t.Errorf("hq/appdev.json still exists after its service was deleted (stat err %v)", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "hq", "webdev.json")); err != nil {
		t.Errorf("another service's HQ state is gone: %v", err)
	}
}
