// Tests for the StateEnvelope-on-the-wire contract (docs/spec-mate.md,
// "Envelope on the wire"). A workflow-aware tool result's text ends with a
// fenced `json zcp-envelope` block so the mate client's reducer can rebuild
// lifecycle state from the provider's tool-result stream — the envelope
// cannot ride in MCP `structuredContent` because Claude Code replaces the
// model-facing text with it (see structured_content_lint_test.go).
package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestHandleLifecycleStatus_TextCarriesEnvelope pins the primary
// carrier: `zerops_workflow action="status"` is the lifecycle recovery
// primitive, so its result must carry the envelope the reducer keys on.
func TestHandleLifecycleStatus_TextCarriesEnvelope(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "test"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "apidev", Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
		})

	result, structured, err := handleLifecycleStatus(
		context.Background(), eng, mock, "proj-1", runtime.Info{InContainer: true},
	)
	if err != nil {
		t.Fatalf("handleLifecycleStatus: %v", err)
	}
	if structured != nil {
		t.Fatalf("structured output must stay nil — Claude Code replaces the text with it")
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	if !strings.Contains(text, "## Status") {
		t.Errorf("markdown guidance lost:\n%s", text)
	}

	env, ok := workflow.ExtractEnvelope(text)
	if !ok {
		t.Fatalf("status result carries no envelope block:\n%s", text)
	}
	if env.Project.ID != "proj-1" {
		t.Errorf("envelope project: got %q want %q", env.Project.ID, "proj-1")
	}
	if env.Phase == "" {
		t.Errorf("envelope phase is empty")
	}
	if len(env.Services) != 1 || env.Services[0].Hostname != "apidev" {
		t.Errorf("envelope services: got %+v want one apidev", env.Services)
	}
}

// TestRenderDevelopBriefing_TextCarriesEnvelope — the develop `start`
// briefing is the second lifecycle-status carrier: a thread that opens a
// work session must be able to seed its strip without a second call.
func TestRenderDevelopBriefing_TextCarriesEnvelope(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-2", Name: "test"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "apidev", Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
		})

	result, structured, err := renderDevelopBriefing(
		context.Background(), eng, mock, "proj-2", runtime.Info{InContainer: true},
	)
	if err != nil {
		t.Fatalf("renderDevelopBriefing: %v", err)
	}
	if structured != nil {
		t.Fatalf("structured output must stay nil")
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	env, ok := workflow.ExtractEnvelope(text)
	if !ok {
		t.Fatalf("develop briefing carries no envelope block:\n%s", text)
	}
	if env.Project.ID != "proj-2" {
		t.Errorf("envelope project: got %q want %q", env.Project.ID, "proj-2")
	}
}

// TestConvertError_CarriesNoEnvelope — an error result is a leaf payload
// (spec-workflows P4). Appending an envelope there would let the reducer
// treat a failed call as fresh state.
func TestConvertError_CarriesNoEnvelope(t *testing.T) {
	t.Parallel()

	result := convertError(platform.NewPlatformError(
		platform.ErrNotImplemented, "boom", ""), WithRecoveryStatus())
	if _, ok := workflow.ExtractEnvelope(getTextContent(t, result)); ok {
		t.Errorf("error results must not carry an envelope block")
	}
}
