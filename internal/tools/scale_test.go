// Tests for: scale.go — zerops_scale MCP tool handler.

package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestScaleTool_Success(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterScale(srv, mock, "proj-1", nil)

	result := callTool(t, srv, "zerops_scale", map[string]any{
		"serviceHostname": "api",
		"cpuMode":         "SHARED",
		"minCpu":          1,
		"maxCpu":          4,
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if parsed["serviceHostname"] != "api" {
		t.Errorf("serviceHostname = %q, want %q", parsed["serviceHostname"], "api")
	}
}

func TestScaleTool_PollsToFinished(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api", Mode: "NON_HA"}}).
		WithAutoscalingProcess(&platform.Process{
			ID:         "proc-scale-1",
			ActionName: "scale",
			Status:     "PENDING",
		}).
		WithProcess(&platform.Process{
			ID:         "proc-scale-1",
			Status:     statusFinished,
			ActionName: "scale",
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterScale(srv, mock, "proj-1", nil)

	result := callTool(t, srv, "zerops_scale", map[string]any{
		"serviceHostname": "api",
		"minCpu":          1,
		"maxCpu":          4,
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	proc, ok := parsed["process"].(map[string]any)
	if !ok {
		t.Fatalf("expected process in result, got: %v", parsed)
	}
	if proc["status"] != statusFinished {
		t.Errorf("process status = %v, want FINISHED", proc["status"])
	}
}

func TestScaleTool_WithDisk(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "db"}})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterScale(srv, mock, "proj-1", nil)

	result := callTool(t, srv, "zerops_scale", map[string]any{
		"serviceHostname": "db",
		"minDisk":         5.0,
		"maxDisk":         20.0,
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}
}

func TestScaleTool_WithThresholds(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterScale(srv, mock, "proj-1", nil)

	result := callTool(t, srv, "zerops_scale", map[string]any{
		"serviceHostname":   "api",
		"minCpu":            1,
		"maxCpu":            4,
		"minFreeRamGB":      0.125,
		"minFreeRamPercent": 5.0,
		"minFreeCpuCores":   0.2,
		"minFreeCpuPercent": 10.0,
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if parsed["serviceHostname"] != "api" {
		t.Errorf("serviceHostname = %q, want %q", parsed["serviceHostname"], "api")
	}
}

func TestScaleTool_ThresholdOnly(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterScale(srv, mock, "proj-1", nil)

	result := callTool(t, srv, "zerops_scale", map[string]any{
		"serviceHostname": "api",
		"minFreeRamGB":    0.125,
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if parsed["serviceHostname"] != "api" {
		t.Errorf("serviceHostname = %q, want %q", parsed["serviceHostname"], "api")
	}
}

func TestScaleTool_MissingService(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterScale(srv, mock, "proj-1", nil)

	// SDK schema validation rejects missing required "serviceHostname" field.
	err := callToolMayError(t, srv, "zerops_scale", map[string]any{
		"minCpu": 1,
	})
	if err == nil {
		t.Error("expected error for missing service hostname")
	}
}

func TestScaleTool_EmptyServiceHostname(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterScale(srv, mock, "proj-1", nil)

	result := callTool(t, srv, "zerops_scale", map[string]any{
		"serviceHostname": "",
		"minCpu":          1,
	})

	if !result.IsError {
		t.Error("expected IsError for empty serviceHostname")
	}
}

// TestScaleTool_SteersTheGroupRecipe: once a Mate's scale change lands, the
// answer carries the group recipe's steer — what the recipe on main writes
// differently and the call that proposes it — and nothing when the recipe
// would not change or the scale did not land — refused, or its process
// failed or was canceled.
func TestScaleTool_SteersTheGroupRecipe(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		steer     string
		failScale bool
		process   string
		want      string
		wantAsked bool
	}{
		{"the recipe differs", "The group recipe on acme/group@main still writes api differently", false, statusFinished, "The group recipe on acme/group@main still writes api differently", true},
		{"the recipe already says it", "", false, statusFinished, "", true},
		{"the scale failed", "never asked", true, statusFinished, "", false},
		{"the scale's process failed", "never asked", false, platform.ProcessStatusFailed, "", false},
		{"the scale's process was canceled", "never asked", false, platform.ProcessStatusCanceled, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api", Mode: "NON_HA"}}).
				WithAutoscalingProcess(&platform.Process{ID: "proc-scale-1", ActionName: "scale", Status: tt.process}).
				WithProcess(&platform.Process{ID: "proc-scale-1", ActionName: "scale", Status: tt.process})
			if tt.failScale {
				mock.WithError("SetAutoscaling", platform.NewPlatformError(platform.ErrAPIError, "refused", ""))
			}
			asked := false
			steer := func(_ context.Context, host string) string {
				asked = host == "api"
				return tt.steer
			}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterScale(srv, mock, "proj-1", steer)
			result := callTool(t, srv, "zerops_scale", map[string]any{"serviceHostname": "api", "minRam": 2})
			var parsed map[string]any
			_ = json.Unmarshal([]byte(getTextContent(t, result)), &parsed)
			got, _ := parsed["groupRecipe"].(string)
			if got != tt.want || asked != tt.wantAsked {
				t.Errorf("groupRecipe = %q (asked=%v), want %q (asked=%v)", got, asked, tt.want, tt.wantAsked)
			}
		})
	}
}
