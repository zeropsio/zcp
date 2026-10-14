package tools

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestStandup_DeploysEveryStageOfAPair (docs/spec-mate.md §2.8a.4): a
// repository the tier builds into one dev half and a stage per part is one
// pair, and the stand-up builds every stage of it — each cross-deployed from
// the dev half with its own setup on the second call, all recorded on the
// pair, the first stage still the one named like the dev half.
func TestStandup_DeploysEveryStageOfAPair(t *testing.T) {
	t.Parallel()
	f := newStandupFixture(t)
	f.hq.tier += `  - hostname: adminstage
    type: nodejs@22
    buildFromGit: ` + f.srv.URL + `/git/app-1/medusadev.git
    zeropsSetup: medusaadmin
`
	body := standupZeropsYAML("medusa") + `  - setup: medusaadmin
    build:
      base: nodejs@22
      buildCommands: [npm ci, npm run build:admin]
      deployFiles: [.]
    run:
      base: nodejs@22
      ports:
        - port: 9001
          httpSupport: true
      start: npm run start:admin
`
	if err := os.WriteFile(filepath.Join(f.root, "medusadev", "zerops.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	f.ssh.zeropsYAML = map[string]string{"medusadev": body}
	f.services = append(f.services, platform.ServiceStack{ID: "svc-adminstage", Name: "adminstage", Status: "READY_TO_DEPLOY",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}})
	f.events = append(f.events, platform.AppVersionEvent{ID: "av-adminstage", ServiceStackID: "svc-adminstage", Status: statusActive, Sequence: 2})
	f.mock.WithServices(f.services).WithAppVersionEvents(f.events)

	result, first := f.run(t)
	if result.IsError || first.StandUp != standupDevelopment {
		t.Fatalf("first call: %s", getTextContent(t, result))
	}
	for _, host := range []string{"medusastage", "adminstage"} {
		if s := first.service(t, host); s.Role != standupRoleStage || s.Pair != "medusadev" || s.Deploy == nil || s.Deploy.Status != standupQueued {
			t.Errorf("first call: %s = %+v, want a queued stage of medusadev", host, s)
		}
	}

	f.devsDeployed()
	result, second := f.run(t)
	if result.IsError || second.StandUp != standupReady {
		t.Fatalf("second call: %s", getTextContent(t, result))
	}
	pushes := f.ssh.pushes()
	for _, want := range []string{"medusadev → svc-medusastage (medusaprod)", "medusadev → svc-adminstage (medusaadmin)"} {
		if !slices.Contains(pushes, want) {
			t.Errorf("deploys = %v, want %q", pushes, want)
		}
	}
	for _, host := range []string{"medusastage", "adminstage"} {
		if s := second.service(t, host); s.Deploy == nil || s.Deploy.Status != standupDeployed {
			t.Errorf("second call: %s = %+v", host, s)
		}
	}
	meta, _ := workflow.ReadServiceMeta(f.stateDir, "medusadev")
	want := []topology.StageRef{{Hostname: "medusastage", SetupName: "medusaprod"}, {Hostname: "adminstage", SetupName: "medusaadmin"}}
	if meta == nil || !slices.Equal(meta.Stages, want) {
		t.Errorf("medusadev's stages = %+v, want %+v", meta, want)
	}
	if dev := second.service(t, "medusadev"); dev.Pair != "medusastage" {
		t.Errorf("medusadev's pair = %q, want its first stage", dev.Pair)
	}
}
