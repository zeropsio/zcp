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
// HQ alone — so the agent works on it the right way. A stage half says
// nothing of it, and neither does a pair zcp has not read it for.
func TestRenderStatus_ADevHalfSaysHowItsRepositorysCodeIsKept(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		origin *HQOrigin
		want   []string
		never  []string
	}{
		{name: "kept on GitHub", origin: &HQOrigin{Mode: HQOriginGitHub, URL: "https://github.com/acme/app"},
			want: []string{"hqRepo=app", "origin.mode=github", "origin=https://github.com/acme/app"}},
		{name: "followed", origin: &HQOrigin{Mode: HQOriginFollow, URL: "https://github.com/acme/app"},
			want: []string{"hqRepo=app", "origin.mode=follow", "origin=https://github.com/acme/app"}},
		{name: "in HQ alone", origin: &HQOrigin{Mode: HQOriginNone},
			want: []string{"hqRepo=app", "origin.mode=none"}, never: []string{"origin="}},
		{name: "not read yet", want: []string{"hqRepo=app"}, never: []string{"origin.mode=", "origin="}},
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
