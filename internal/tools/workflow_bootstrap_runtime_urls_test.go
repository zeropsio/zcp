// Tests for: workflow_bootstrap.go::populateRuntimeURLs — the L4 (tools)
// resolution step of RCO-7's structured runtime-URL collection. workflow
// must not import ops (hard layering rule), so this is where
// ops.ResolveSubdomainURL actually gets called and the resolved collection
// is attached to the bootstrap response.

package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// seedBootstrapPlanAtClose drives a fresh engine through start → plan
// submission (standard mode dev/stage pair) → provision completion
// (checker=nil bypasses live-service validation, same precedent as
// integration/bootstrap_conductor_test.go) so the session lands with
// "close" as the current, in_progress step — the shape RCO-7 names for
// post-provision / status-during-close.
func seedBootstrapPlanAtClose(t *testing.T, eng *workflow.Engine, targets []workflow.BootstrapTarget) {
	t.Helper()
	seedBootstrapPlan(t, eng, targets)
	if _, err := eng.BootstrapComplete(context.Background(), workflow.StepProvision, "provisioned", nil); err != nil {
		t.Fatalf("BootstrapComplete(provision): %v", err)
	}
}

// TestWorkflowBootstrap_PopulatesRuntimeURLs pins RCO-7's L4 resolution step:
// populateRuntimeURLs resolves the structured collection via
// ops.ResolveSubdomainURL for every subdomain-enabled service, classifies
// each hostname's role against the bootstrap plan (dev/stage), and marks
// the stage entry as the handoff. A resolution failure (GetProject error)
// is best-effort — the response keeps its Current step (close is never
// blocked) and the collection is simply empty, with the close guide noting
// the URL could not be resolved yet.
//
// Independent-oracle literal: hostname "tmponb3astage", project subdomain
// host "24cb", port 3000 → "https://tmponb3astage-24cb-3000.prg1.zerops.app"
// (live-verified observation cited in the slice brief) — hand-written here,
// not recomputed via ops.BuildSubdomainURL's own formula.
func TestWorkflowBootstrap_PopulatesRuntimeURLs(t *testing.T) {
	t.Parallel()

	newEngineAtClose := func(t *testing.T) *workflow.Engine {
		t.Helper()
		eng := workflow.NewEngine(t.TempDir(), workflow.EnvContainer, nil)
		seedBootstrapPlanAtClose(t, eng, []workflow.BootstrapTarget{
			{Runtime: workflow.RuntimeTarget{
				DevHostname:   "tmponb3adev",
				Type:          "nodejs@22",
				BootstrapMode: "standard",
				ExplicitStage: "tmponb3astage",
			}},
		})
		return eng
	}

	t.Run("resolves the stage URL, classifies roles, marks stage as handoff", func(t *testing.T) {
		t.Parallel()
		eng := newEngineAtClose(t)
		mock := platform.NewMock().
			WithProject(&platform.Project{ID: "proj-1", SubdomainHost: "24cb.prg1.zerops.app"}).
			WithServices([]platform.ServiceStack{
				{ID: "svc-dev", Name: "tmponb3adev", SubdomainAccess: false},
				{ID: "svc-stage", Name: "tmponb3astage", SubdomainAccess: true, Ports: []platform.Port{{Port: 3000}}},
			})

		resp, err := eng.BootstrapStatus()
		if err != nil {
			t.Fatalf("BootstrapStatus: %v", err)
		}
		if resp.Current == nil || resp.Current.Name != workflow.StepClose {
			t.Fatalf("expected close step in progress before populate, got %+v", resp.Current)
		}

		populateRuntimeURLs(context.Background(), mock, "proj-1", eng, resp)

		if len(resp.RuntimeURLs) != 1 {
			t.Fatalf("expected exactly 1 resolved runtime URL (dev has no subdomain access), got %d: %+v", len(resp.RuntimeURLs), resp.RuntimeURLs)
		}
		want := workflow.RuntimeURL{
			Hostname: "tmponb3astage",
			Role:     workflow.RuntimeURLRoleStage,
			URL:      "https://tmponb3astage-24cb-3000.prg1.zerops.app",
			Handoff:  true,
		}
		got := resp.RuntimeURLs[0]
		gotPublicAccess := got.PublicAccess
		got.PublicAccess = nil // compared separately below — carries a slice, not comparable via !=
		if got != want {
			t.Errorf("runtime URL = %+v, want %+v", got, want)
		}
		// PA-5 (docs/spec-workflows.md §8 O3): every URL-bearing surface,
		// RCO-7 included, renders publicAccess. This stage entry is already
		// subdomain-enabled (SubdomainAccess: true, no domains seeded) — no
		// persisted ServiceMeta at this call site, so intent is derived from
		// observed state alone (topology.DeriveAdoptedIntent): on ⇒ subdomain.
		if gotPublicAccess == nil {
			t.Fatal("RuntimeURL.PublicAccess: want non-nil")
		}
		if gotPublicAccess.Intent != "subdomain" {
			t.Errorf("PublicAccess.Intent = %q, want %q", gotPublicAccess.Intent, "subdomain")
		}
		if gotPublicAccess.Subdomain != "on" {
			t.Errorf("PublicAccess.Subdomain = %q, want %q", gotPublicAccess.Subdomain, "on")
		}
		if gotPublicAccess.URL != want.URL {
			t.Errorf("PublicAccess.URL = %q, want %q", gotPublicAccess.URL, want.URL)
		}
		if resp.Current == nil || !strings.Contains(resp.Current.DetailedGuide, want.URL) {
			t.Errorf("close guide should carry the resolved stage URL, got:\n%s", safeGuide(resp))
		}
	})

	t.Run("resolver failure omits the entry and does not block close", func(t *testing.T) {
		t.Parallel()
		eng := newEngineAtClose(t)
		mock := platform.NewMock().
			WithProject(&platform.Project{ID: "proj-1", SubdomainHost: "24cb.prg1.zerops.app"}).
			WithServices([]platform.ServiceStack{
				{ID: "svc-stage", Name: "tmponb3astage", SubdomainAccess: true, Ports: []platform.Port{{Port: 3000}}},
			}).
			WithError("GetProject", context.DeadlineExceeded)

		resp, err := eng.BootstrapStatus()
		if err != nil {
			t.Fatalf("BootstrapStatus: %v", err)
		}
		if resp.Current == nil || resp.Current.Name != workflow.StepClose {
			t.Fatalf("expected close step in progress before populate, got %+v", resp.Current)
		}

		populateRuntimeURLs(context.Background(), mock, "proj-1", eng, resp)

		if len(resp.RuntimeURLs) != 0 {
			t.Errorf("resolver failure must omit the entry, got %+v", resp.RuntimeURLs)
		}
		if resp.Current == nil {
			t.Fatal("resolver failure must not block close — Current must stay populated")
		}
		if !strings.Contains(resp.Current.DetailedGuide, "could be resolved yet") {
			t.Errorf("close guide should note the URL could not be resolved yet, got:\n%s", resp.Current.DetailedGuide)
		}
	})
}

func safeGuide(resp *workflow.BootstrapResponse) string {
	if resp == nil || resp.Current == nil {
		return "<nil current>"
	}
	return resp.Current.DetailedGuide
}

// The response collection and its public-access payload must carry the selected
// endpoint, never the newline-separated platform URL list.
func TestBuildRuntimeURLs_TwoHTTPPorts_ProjectsSelectedURI(t *testing.T) {
	t.Parallel()
	const first = "https://portprobe-31e1-5733.prg1.zerops.app"
	const second = "https://portprobe-31e1-5734.prg1.zerops.app"
	for _, tc := range []struct {
		name  string
		ports []platform.Port
		want  string
	}{
		{"5733", []platform.Port{{Port: 5733, Scheme: "http"}, {Port: 5734, Scheme: "http"}}, first},
		{"5734", []platform.Port{{Port: 5734, Scheme: "http"}, {Port: 5733, Scheme: "http"}}, second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := platform.ServiceStack{ID: "svc-1", Name: "portprobe", SubdomainAccess: true, Ports: tc.ports}
			mock := platform.NewMock().WithProject(&platform.Project{ID: "proj-1", SubdomainHost: "31e1"}).
				WithServiceEnv(svc.ID, []platform.ServiceEnvVar{{Key: "zeropsSubdomain", Content: second + "\n" + first}})
			got := buildRuntimeURLs(context.Background(), mock, "proj-1", []platform.ServiceStack{svc}, &workflow.ServicePlan{})
			if len(got) != 1 {
				t.Fatalf("runtime URLs = %+v, want one service entry", got)
			}
			if got[0].URL != tc.want {
				t.Errorf("ASSERTION: runtime URI = %q, want %q", got[0].URL, tc.want)
			}
			if got[0].PublicAccess == nil || got[0].PublicAccess.URL != tc.want {
				t.Errorf("ASSERTION: runtime public access = %+v, want URI %q", got[0].PublicAccess, tc.want)
			}
		})
	}
}
