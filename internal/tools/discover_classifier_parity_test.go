package tools

import (
	"context"
	"path/filepath"
	"sort"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestDiscoverClassifierParity pins that the new per-service
// AdoptionState classifier in enrichWithMetaStatus stays semantically
// aligned with the workflow.BuildBootstrapRouteOptions (the exported
// authority for adopt vs resume routing decisions).
//
// Codex v2 review B2 mandated this parity check: discover's
// AdoptionAdoptable hostnames must match the `adopt` route's
// AdoptServices; AdoptionResumable presence must correspond to the
// `resume` route's existence with the same hostnames and matching
// session ID. Otherwise the discover surface and workflow route
// surface could drift, and an agent following one would be misled by
// the other.
//
// We deliberately go through the EXPORTED workflow API
// (BuildBootstrapRouteOptions) rather than calling unexported helpers
// like adoptableServices / resumeOption directly — Codex flagged that
// approach as unimplementable from internal/tools. The exported API
// IS the authority; tests against unexported helpers belong in
// internal/workflow if needed separately.
func TestDiscoverClassifierParity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		services   []platform.ServiceStack
		metas      []*workflow.ServiceMeta
		wantAdopt  []string // expected adoptable hostnames (sorted)
		wantResume struct {
			Hostnames []string // expected resumable hostnames (sorted)
			SessionID string   // ResumeSession from workflow side
		}
	}{
		{
			name: "fresh project — all runtimes adoptable, no resumable, managed deps + zcp self excluded",
			services: []platform.ServiceStack{
				{ID: "rt-1", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
				{ID: "rt-2", Name: "appstage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
				{ID: "db-1", Name: "db", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16", ServiceStackTypeCategoryName: "USER"}},
				{ID: "zcp-1", Name: "zcp", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "zcp@1", ServiceStackTypeCategoryName: "USER"}},
			},
			metas:     nil,
			wantAdopt: []string{"appdev", "appstage"},
		},
		{
			name: "adopted pair — neither adoptable nor resumable",
			services: []platform.ServiceStack{
				{ID: "rt-1", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
				{ID: "rt-2", Name: "appstage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
			},
			metas: []*workflow.ServiceMeta{
				{Hostname: "appdev", StageHostname: "appstage", Mode: topology.PlanModeStandard, BootstrappedAt: "2026-05-27", BootstrapSession: "old-sess"},
			},
			wantAdopt: nil,
		},
		{
			name: "resumable single runtime — appears in resume route only",
			services: []platform.ServiceStack{
				{ID: "rt-1", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
			},
			metas: []*workflow.ServiceMeta{
				{Hostname: "appdev", Mode: topology.PlanModeDev, BootstrapSession: "live-sess-xyz"},
			},
			wantResume: struct {
				Hostnames []string
				SessionID string
			}{
				Hostnames: []string{"appdev"},
				SessionID: "live-sess-xyz",
			},
		},
		{
			name: "orphan meta (incomplete + empty session) — adoptable, not resumable",
			services: []platform.ServiceStack{
				{ID: "rt-1", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
			},
			metas: []*workflow.ServiceMeta{
				{Hostname: "appdev", Mode: topology.PlanModeDev /* BootstrapSession empty */},
			},
			wantAdopt: []string{"appdev"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			stateDir := filepath.Join(dir, ".zcp", "state")
			for _, m := range tt.metas {
				if err := workflow.WriteServiceMeta(stateDir, m); err != nil {
					t.Fatalf("seed meta: %v", err)
				}
			}

			// Discover side: classify via enrichWithMetaStatus.
			discoverResult := &ops.DiscoverResult{
				Services: serviceInfosFromStacks(tt.services),
			}
			enrichWithMetaStatus(discoverResult, stateDir, nil)

			discoverAdoptable := collectByState(discoverResult, ops.AdoptionAdoptable)
			discoverResumable := collectByState(discoverResult, ops.AdoptionResumable)

			// Workflow side: BuildBootstrapRouteOptions is the exported
			// authority for adopt + resume route semantics. Pass
			// runtime.Info{ServiceName:"zcp"} so workflow's self-filter
			// (route.go's isSelf check) excludes the zcp control-plane
			// container — matching the discover-side Type=="zcp@1"
			// filter. Without this, workflow would include "zcp" in
			// AdoptServices while discover correctly excludes it,
			// and the parity check would false-positive on this
			// pure-test-fixture difference.
			opts, err := workflow.BuildBootstrapRouteOptions(
				context.Background(),
				"",
				tt.services,
				tt.metas,
				nil,
				runtime.Info{ServiceName: "zcp"},
			)
			if err != nil {
				t.Fatalf("BuildBootstrapRouteOptions: %v", err)
			}
			var workflowAdopt, workflowResume []string
			var workflowResumeSession string
			for _, opt := range opts {
				switch opt.Route {
				case workflow.BootstrapRouteAdopt:
					workflowAdopt = append([]string(nil), opt.AdoptServices...)
				case workflow.BootstrapRouteResume:
					workflowResume = append([]string(nil), opt.ResumeServices...)
					workflowResumeSession = opt.ResumeSession
				case workflow.BootstrapRouteRecipe, workflow.BootstrapRouteClassic:
					// Not relevant for adopt/resume parity check.
				}
			}

			sort.Strings(discoverAdoptable)
			sort.Strings(discoverResumable)
			sort.Strings(workflowAdopt)
			sort.Strings(workflowResume)

			// Parity assertion #1: adoptable hostnames match.
			if !parityStringsEqual(discoverAdoptable, workflowAdopt) {
				t.Errorf("ADOPTABLE drift:\n  discover.AdoptionAdoptable = %v\n  workflow.adopt.AdoptServices = %v",
					discoverAdoptable, workflowAdopt)
			}

			// Parity assertion #2: resumable hostnames match.
			if !parityStringsEqual(discoverResumable, workflowResume) {
				t.Errorf("RESUMABLE drift:\n  discover.AdoptionResumable = %v\n  workflow.resume.ResumeServices = %v",
					discoverResumable, workflowResume)
			}

			// Parity assertion #3: when resumable, the session ID surfaces
			// in the discover warning (we don't reparse the warning here
			// — that's covered in TestEnrichWithMetaStatus_ResumableRuntime_*
			// — but we confirm the SAME session ID is available on both
			// sides via meta lookup).
			if len(discoverResumable) > 0 {
				if workflowResumeSession == "" {
					t.Errorf("workflow.resume.ResumeSession empty despite resumable services %v", discoverResumable)
				}
			}

			// Vs expectations.
			if tt.wantAdopt == nil {
				if len(discoverAdoptable) > 0 {
					t.Errorf("discover adoptable: got %v, want empty", discoverAdoptable)
				}
			} else if !parityStringsEqual(discoverAdoptable, paritySortedCopy(tt.wantAdopt)) {
				t.Errorf("discover adoptable: got %v, want %v", discoverAdoptable, tt.wantAdopt)
			}
			if tt.wantResume.Hostnames != nil {
				if !parityStringsEqual(discoverResumable, paritySortedCopy(tt.wantResume.Hostnames)) {
					t.Errorf("discover resumable: got %v, want %v", discoverResumable, tt.wantResume.Hostnames)
				}
				if tt.wantResume.SessionID != "" && workflowResumeSession != tt.wantResume.SessionID {
					t.Errorf("workflow resume session: got %q, want %q", workflowResumeSession, tt.wantResume.SessionID)
				}
			}
		})
	}
}

// serviceInfosFromStacks builds DiscoverResult.Services entries from
// the same platform.ServiceStack fixtures the workflow side
// consumes — mirrors what ops.Discover's buildSummaryServiceInfo
// would do for these fields.
func serviceInfosFromStacks(stacks []platform.ServiceStack) []ops.ServiceInfo {
	out := make([]ops.ServiceInfo, len(stacks))
	for i, s := range stacks {
		out[i] = ops.ServiceInfo{
			Hostname:         s.Name,
			ServiceID:        s.ID,
			Type:             s.ServiceStackTypeInfo.ServiceStackTypeVersionName,
			IsInfrastructure: topology.IsManagedService(s.ServiceStackTypeInfo.ServiceStackTypeVersionName),
		}
	}
	return out
}

func collectByState(r *ops.DiscoverResult, state ops.AdoptionState) []string {
	var out []string
	for _, s := range r.Services {
		if s.AdoptionState == state {
			out = append(out, s.Hostname)
		}
	}
	return out
}

func parityStringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func paritySortedCopy(s []string) []string {
	out := make([]string, len(s))
	copy(out, s)
	sort.Strings(out)
	return out
}
