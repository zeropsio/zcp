package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// TestBuildExportEnvelope_KnownTargetSinglEntryServices pins step 0a.5
// case (a) of plans/atom-corpus-verification-2026-05-02.md. With a
// non-empty target hostname, BuildExportEnvelope returns an envelope
// with Phase=PhaseExportActive, the supplied ExportStatus, and a
// Services slice carrying a single snapshot for the target service —
// no other project services contribute, per the
// single-entry-Services audit decision (synthesize_export_audit.md).
func TestBuildExportEnvelope_KnownTargetSingleEntryServices(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{
			ID:     "svc-app",
			Name:   "appdev",
			Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName: "nodejs@22",
			},
		},
		{
			ID:     "svc-db",
			Name:   "db",
			Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName: "postgresql@16",
			},
		},
	})
	stateDir := t.TempDir()

	env, err := BuildExportEnvelope(
		context.Background(),
		"appdev",
		topology.ExportStatusPublishReady,
		ExportEnvelopeOpts{Client: mock, ProjectID: "proj-1", StateDir: stateDir},
	)
	if err != nil {
		t.Fatalf("BuildExportEnvelope: %v", err)
	}
	if env.Phase != PhaseExportActive {
		t.Errorf("Phase = %q, want %q", env.Phase, PhaseExportActive)
	}
	if env.ExportStatus != topology.ExportStatusPublishReady {
		t.Errorf("ExportStatus = %q, want %q", env.ExportStatus, topology.ExportStatusPublishReady)
	}
	if len(env.Services) != 1 {
		t.Fatalf("Services length = %d, want 1 (single-entry semantics; managed db must not appear)", len(env.Services))
	}
	if env.Services[0].Hostname != "appdev" {
		t.Errorf("Services[0].Hostname = %q, want %q", env.Services[0].Hostname, "appdev")
	}
	if env.Services[0].TypeVersion != "nodejs@22" {
		t.Errorf("Services[0].TypeVersion = %q, want %q", env.Services[0].TypeVersion, "nodejs@22")
	}
}

// TestBuildExportEnvelope_EmptyTargetEmptyServices pins step 0a.5 case
// (b): when target is empty (scope-prompt state), BuildExportEnvelope
// returns an envelope with an empty Services slice — atoms for
// scope-prompt MUST NOT use service-scoped axes, so no anchor service
// exists. Skips the ListServices call entirely (no client lookup
// needed for an unselected target).
func TestBuildExportEnvelope_EmptyTargetEmptyServices(t *testing.T) {
	t.Parallel()

	env, err := BuildExportEnvelope(
		context.Background(),
		"",
		topology.ExportStatusScopePrompt,
		ExportEnvelopeOpts{Client: nil, ProjectID: "proj-1", StateDir: t.TempDir()},
	)
	if err != nil {
		t.Fatalf("BuildExportEnvelope empty-target: %v", err)
	}
	if env.Phase != PhaseExportActive {
		t.Errorf("Phase = %q, want %q", env.Phase, PhaseExportActive)
	}
	if env.ExportStatus != topology.ExportStatusScopePrompt {
		t.Errorf("ExportStatus = %q, want %q", env.ExportStatus, topology.ExportStatusScopePrompt)
	}
	if len(env.Services) != 0 {
		t.Errorf("Services length = %d, want 0 (target unknown)", len(env.Services))
	}
}

// TestBuildExportEnvelope_ServiceNotFound pins the not-found error
// path: a target that doesn't appear in the project's ListServices
// response surfaces a clear error rather than silently emitting an
// envelope with empty Services (which would mislead the synthesizer
// into rendering scope-prompt atoms).
func TestBuildExportEnvelope_ServiceNotFound(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-other", Name: "other", Status: "ACTIVE"},
	})

	_, err := BuildExportEnvelope(
		context.Background(),
		"appdev",
		topology.ExportStatusPublishReady,
		ExportEnvelopeOpts{Client: mock, ProjectID: "proj-1", StateDir: t.TempDir()},
	)
	if err == nil {
		t.Fatal("expected error for unknown target service, got nil")
	}
	if !strings.Contains(err.Error(), "appdev") {
		t.Errorf("error %q must name the missing service", err.Error())
	}
}

// TestRenderExportGuidance_NonEmptyForMatchingAtoms pins step 0a.5 case
// (c): RenderExportGuidance returns a non-empty body when at least one
// atom in the corpus matches the envelope. Uses a synthetic two-atom
// corpus (one universal export-active atom + one filtered to publish-
// ready) so the test doesn't couple to the live atom corpus.
func TestRenderExportGuidance_NonEmptyForMatchingAtoms(t *testing.T) {
	t.Parallel()

	corpus := []KnowledgeAtom{
		{
			ID: "universal-intro", Priority: 1,
			Axes: AxisVector{Phases: []Phase{PhaseExportActive}},
			Body: "Universal export framing line.",
		},
		{
			ID: "publish-only", Priority: 2,
			Axes: AxisVector{
				Phases:         []Phase{PhaseExportActive},
				ExportStatuses: []topology.ExportStatus{topology.ExportStatusPublishReady},
			},
			Body: "Publish-ready specific guidance.",
		},
	}

	env := StateEnvelope{Phase: PhaseExportActive, ExportStatus: topology.ExportStatusPublishReady}
	body, err := RenderExportGuidance(env, corpus)
	if err != nil {
		t.Fatalf("RenderExportGuidance: %v", err)
	}
	if body == "" {
		t.Fatal("body is empty; want both atoms rendered")
	}
	if !strings.Contains(body, "Universal export framing line.") {
		t.Errorf("body missing universal atom content: %q", body)
	}
	if !strings.Contains(body, "Publish-ready specific guidance.") {
		t.Errorf("body missing publish-only atom content: %q", body)
	}
	universalIdx := strings.Index(body, "Universal export framing line.")
	publishIdx := strings.Index(body, "Publish-ready specific guidance.")
	if universalIdx > publishIdx {
		t.Errorf("priority order broken: universal (priority 1) must precede publish-only (priority 2); got universal@%d publish@%d", universalIdx, publishIdx)
	}

	// Different status — only the universal atom matches.
	envScaffold := StateEnvelope{Phase: PhaseExportActive, ExportStatus: topology.ExportStatusScaffoldRequired}
	bodyScaffold, err := RenderExportGuidance(envScaffold, corpus)
	if err != nil {
		t.Fatalf("RenderExportGuidance scaffold: %v", err)
	}
	if !strings.Contains(bodyScaffold, "Universal export framing line.") {
		t.Errorf("scaffold body missing universal atom: %q", bodyScaffold)
	}
	if strings.Contains(bodyScaffold, "Publish-ready specific guidance.") {
		t.Errorf("scaffold body must not include publish-only atom: %q", bodyScaffold)
	}
}

// TestSynthesize_ServiceContextFiresOnTargetService pins step 0a.7 of
// the plan: an atom with BOTH a service-scoped axis (runtimes:
// implicit-webserver) AND the new envelope-scoped axis (exportStatus:
// scaffold-required) matches when the target snapshot satisfies both;
// doesn't match for a different runtime; doesn't match when target is
// empty (scope-prompt with empty Services slice — service-scoped axes
// can't fire because no service is in the envelope).
//
// This explicitly verifies the service-context decision works: routing
// handleExport through atom synthesis with a single-entry Services
// slice for the target makes service-scoped axes fire on the actual
// service being exported, eliminating the SynthesizeImmediatePhase
// silent-non-firing problem.
func TestSynthesize_ServiceContextFiresOnTargetService(t *testing.T) {
	t.Parallel()

	corpus := []KnowledgeAtom{
		{
			ID: "scaffold-implicit-web", Priority: 5,
			Axes: AxisVector{
				Phases:         []Phase{PhaseExportActive},
				ExportStatuses: []topology.ExportStatus{topology.ExportStatusScaffoldRequired},
				Runtimes:       []topology.RuntimeClass{topology.RuntimeImplicitWeb},
			},
			Body: "scaffold guidance for php-nginx implicit-webserver target.",
		},
	}

	cases := []struct {
		name        string
		env         StateEnvelope
		wantMatched bool
	}{
		{
			name: "implicit_webserver_target_in_scaffold_required",
			env: StateEnvelope{
				Phase:        PhaseExportActive,
				ExportStatus: topology.ExportStatusScaffoldRequired,
				Services: []ServiceSnapshot{
					{Hostname: "appdev", RuntimeClass: topology.RuntimeImplicitWeb, Status: "ACTIVE"},
				},
			},
			wantMatched: true,
		},
		{
			name: "dynamic_runtime_target_does_not_match",
			env: StateEnvelope{
				Phase:        PhaseExportActive,
				ExportStatus: topology.ExportStatusScaffoldRequired,
				Services: []ServiceSnapshot{
					{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic, Status: "ACTIVE"},
				},
			},
			wantMatched: false,
		},
		{
			name: "empty_services_scope_prompt_does_not_match",
			env: StateEnvelope{
				Phase:        PhaseExportActive,
				ExportStatus: topology.ExportStatusScopePrompt,
				Services:     nil,
			},
			wantMatched: false,
		},
		{
			name: "implicit_webserver_target_but_publish_ready_status_does_not_match",
			env: StateEnvelope{
				Phase:        PhaseExportActive,
				ExportStatus: topology.ExportStatusPublishReady,
				Services: []ServiceSnapshot{
					{Hostname: "appdev", RuntimeClass: topology.RuntimeImplicitWeb, Status: "ACTIVE"},
				},
			},
			wantMatched: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			matches, err := Synthesize(tc.env, corpus)
			if err != nil {
				t.Fatalf("Synthesize: %v", err)
			}
			matched := false
			for _, m := range matches {
				if m.AtomID == "scaffold-implicit-web" {
					matched = true
					break
				}
			}
			if matched != tc.wantMatched {
				t.Errorf("matched=%v, want %v (matches=%+v)", matched, tc.wantMatched, matches)
			}
		})
	}
}
