// Tests for: server/instructions.go — BuildInstructions, buildProjectSummary.
package server

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

func TestBuildInstructions_ConformantState_HasBootstrapHint(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{
				ID:     "svc-1",
				Name:   "nodedev",
				Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName: "nodejs@22",
				},
			},
			{
				ID:     "svc-2",
				Name:   "nodestage",
				Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName: "nodejs@22",
				},
			},
		})

	result := BuildInstructions(context.Background(), mock, "proj-1", runtime.Info{}, "")

	// Must contain the existing deploy routing hint.
	if !strings.Contains(result, `zerops_workflow action="start" workflow="deploy"`) {
		t.Error("expected deploy workflow hint in CONFORMANT state")
	}

	// Must contain the new bootstrap routing hint for adding new services in the project summary section.
	// The routing instructions always contain bootstrap, but the CONFORMANT section must also mention it.
	summary := buildProjectSummary(context.Background(), mock, "proj-1")
	if !strings.Contains(summary, "ADD new services") {
		t.Error("expected 'ADD new services' hint in CONFORMANT project summary")
	}
	if !strings.Contains(summary, `zerops_workflow action="start" workflow="bootstrap"`) {
		t.Error("expected bootstrap workflow hint in CONFORMANT project summary")
	}
}

func TestBuildInstructions_EmptyProject_HasBootstrapOnly(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{})

	result := BuildInstructions(context.Background(), mock, "proj-1", runtime.Info{}, "")

	if !strings.Contains(result, `zerops_workflow action="start" workflow="bootstrap"`) {
		t.Error("expected bootstrap workflow hint for empty project")
	}
}
