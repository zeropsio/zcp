package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// TestRenderStatus_ADevHalfSaysHowItsRepositorysCodeIsKept: status names, on
// a wired pair's dev half, its repository in HQ and how HQ last said its code
// is kept — on GitHub, the checkout's origin; followed, HQ the origin; or in
// HQ alone — so the agent works on it the right way; unknown while zcp could
// not read it. A stage half says nothing of it.
func TestRenderStatus_ADevHalfSaysHowItsRepositorysCodeIsKept(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		origin *HQOrigin
		want   []string
		never  []string
	}{
		{name: "kept on GitHub", origin: &HQOrigin{Mode: "github", URL: "https://github.com/acme/app"},
			want: []string{"hqRepo=app", "origin.mode=github", "origin=https://github.com/acme/app"}},
		{name: "followed", origin: &HQOrigin{Mode: "follow", URL: "https://github.com/acme/app"},
			want: []string{"hqRepo=app", "origin.mode=follow", "origin=https://github.com/acme/app"}},
		{name: "in HQ alone", origin: &HQOrigin{Mode: HQOriginNone},
			want: []string{"hqRepo=app", "origin.mode=none"}, never: []string{"origin="}},
		{name: "not read yet", want: []string{"hqRepo=app", "origin.mode=unknown"}, never: []string{"origin="}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			meta := &ServiceMeta{
				Hostname: "appdev", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "appstage"}},
				BootstrappedAt: "2026-10-22", GitPushState: topology.GitPushConfigured,
				HQ: &HQRepoRef{AppID: "app-1", Repo: "app", Origin: tc.origin},
			}
			runtime := platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}
			dev := renderServiceLine(buildOneSnapshot(platform.ServiceStack{Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: runtime}, meta, nil))
			stage := renderServiceLine(buildOneSnapshot(platform.ServiceStack{Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: runtime}, meta, nil))
			for _, want := range tc.want {
				if !strings.Contains(dev, want) {
					t.Errorf("the dev half's line lacks %q: %s", want, dev)
				}
			}
			for _, never := range tc.never {
				if strings.Contains(dev, never) {
					t.Errorf("the dev half's line says %q: %s", never, dev)
				}
			}
			if strings.Contains(stage, "hqRepo=") || strings.Contains(stage, "origin") {
				t.Errorf("the stage half's line names the repository's origin: %s", stage)
			}
		})
	}
}

// TestRenderStatus_NoPairNamesAChange: a change is its repository's, so no
// pair's line names one — not the dev half's of a pair whose record an older
// zcp wrote with a change, nor its stage's.
func TestRenderStatus_NoPairNamesAChange(t *testing.T) {
	t.Parallel()
	meta := &ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "appstage"}},
		BootstrappedAt: "2026-10-22", GitPushState: topology.GitPushConfigured,
		HQ: &HQRepoRef{AppID: "app-1", Repo: "app", Change: 7, Branch: "fix-login"},
	}
	runtime := platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}
	for _, host := range []string{"appdev", "appstage"} {
		line := renderServiceLine(buildOneSnapshot(platform.ServiceStack{Name: host, Status: "ACTIVE", ServiceStackTypeInfo: runtime}, meta, nil))
		if strings.Contains(line, "change") {
			t.Errorf("%s's line names a change: %s", host, line)
		}
	}
}

// TestRenderStatus_ChangesBlockPerRepository: status names each repository's
// changes in one line — by number and branch, with their standing and the
// leases on their branch — and the branches leases hold with no change yet.
func TestRenderStatus_ChangesBlockPerRepository(t *testing.T) {
	t.Parallel()
	env := StateEnvelope{Phase: PhaseIdle, HQChanges: []HQRepoChanges{{
		Repo: "appsdev",
		Changes: []HQChangeLine{
			{Number: 2, Branch: "pool-previews", Labels: []string{"ready", "behind main"}, Leases: []string{"hqpool-3", "boxpool-5"}},
			{Number: 6, Branch: "badge", Labels: []string{"draft"}, Leases: []string{"hqpool-4"}},
			{Number: 7, Branch: "mate/p-mate/7"},
		},
		Branches: []HQBranchLine{{Branch: "build-stamp", Leases: []string{"webpool-2"}}},
	}}}

	out := RenderStatus(Response{Envelope: env})

	want := "Changes:\n  - HQ appsdev: #2 pool-previews (ready · behind main) — leases hqpool-3, boxpool-5; #6 badge (draft) — leases hqpool-4; #7 mate/p-mate/7; branch build-stamp — leases webpool-2: no change yet\n"
	if !strings.Contains(out, want) {
		t.Errorf("status lacks\n%s\ngot\n%s", want, out)
	}
	if out := RenderStatus(Response{Envelope: StateEnvelope{Phase: PhaseIdle}}); strings.Contains(out, "Changes:") {
		t.Errorf("status with no change names a Changes section:\n%s", out)
	}
}

// TestRenderStatus_ACheckoutWhoseTargetCannotBeNamedSaysWhy: a dev half whose
// checkout origin could not be matched to HQ's repository, for want of the
// organisation its address names, says that rather than nothing.
func TestRenderStatus_ACheckoutWhoseTargetCannotBeNamedSaysWhy(t *testing.T) {
	t.Parallel()
	meta := &ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "appstage"}},
		BootstrappedAt: "2026-10-22", GitPushState: topology.GitPushConfigured,
		HQ: &HQRepoRef{AppID: "app-1", Repo: "app"},
	}
	svc := buildOneSnapshot(platform.ServiceStack{Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}}, meta, nil)
	svc.HQCheckoutOrigin, svc.HQOriginBlocked = HQOriginNone, "no organisation"
	if line := renderServiceLine(svc); !strings.Contains(line, "cannot point it at HQ: no organisation") {
		t.Errorf("the dev half's line does not say why: %s", line)
	}
}
