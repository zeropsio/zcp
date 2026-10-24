package tools

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// variantBTierFixture is the stand-up fixture whose tier is variant B
// (docs/spec-mate.md §2.8a.4a), the shape of F2: medusadev's repository builds
// a further dev service apidev (setup dev-api) beside the dev half, both dev
// setups mounting the code volume vol at /var/www/ws, and a second stage
// apistage (setup api). Nothing is recorded yet.
func variantBTierFixture(t *testing.T) *standupFixture {
	t.Helper()
	f := newStandupFixture(t)
	f.hq.tier += `  - hostname: apidev
    type: go@1
    buildFromGit: ` + f.srv.URL + `/git/app-1/medusadev.git
    zeropsSetup: dev-api
  - hostname: apistage
    type: go@1
    buildFromGit: ` + f.srv.URL + `/git/app-1/medusadev.git
    zeropsSetup: api
  - hostname: vol
    type: local-storage:single@1
`
	devAPI := `  - setup: dev-api
    build:
      base: go@1
      deployFiles: [.]
    run:
      base: go@1
      volume:
        hostname: vol
        mountPath: /var/www/ws
      start: zsc noop --silent
  - setup: api
    build:
      base: go@1
      deployFiles: [.]
    run:
      base: go@1
      start: ./app
`
	devHalf := strings.Replace(standupZeropsYAML("medusa"), "    run:\n      base: nodejs@22\n",
		"    run:\n      base: nodejs@22\n      volume:\n        hostname: vol\n        mountPath: /var/www/ws\n", 1)
	body := devHalf + devAPI
	if err := os.WriteFile(filepath.Join(f.root, "medusadev", "zerops.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	f.ssh.zeropsYAML = map[string]string{"medusadev": body}
	empty := &platform.ActiveAppVersionDigest{ID: "av-placeholder", Source: platform.AppVersionSourceNone}
	f.services = append(f.services,
		platform.ServiceStack{ID: "svc-apidev", Name: "apidev", Status: statusActive, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "go@1", ServiceStackTypeCategoryName: "USER"}, ActiveAppVersion: empty},
		platform.ServiceStack{ID: "svc-apistage", Name: "apistage", Status: "READY_TO_DEPLOY", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "go@1", ServiceStackTypeCategoryName: "USER"}},
		platform.ServiceStack{ID: "svc-vol", Name: "vol", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "local-storage:single@1", ServiceStackTypeCategoryName: "STANDARD"}},
	)
	f.events = append(f.events,
		platform.AppVersionEvent{ID: "av-apidev", ServiceStackID: "svc-apidev", Status: statusActive, Sequence: 2},
		platform.AppVersionEvent{ID: "av-apistage", ServiceStackID: "svc-apistage", Status: statusActive, Sequence: 2})
	f.mock.WithServices(f.services).WithAppVersionEvents(f.events)
	return f
}

// variantBStandupFixture is variantBTierFixture with medusadev's pair already
// recorded as variant B — a project set up before zcp stopped setting up code
// volumes, stood up again: the record is kept and still works.
func variantBStandupFixture(t *testing.T) *standupFixture {
	t.Helper()
	f := variantBTierFixture(t)

	meta := workflow.NewServiceMeta("medusadev", topology.PlanModeStandard)
	meta.PrimarySetupName = "medusadev"
	meta.Stages = []topology.StageRef{{Hostname: "medusastage", SetupName: "medusaprod"}, {Hostname: "apistage", SetupName: "api"}}
	meta.CodeVolume = &topology.CodeVolume{Hostname: "vol", MountPath: "/var/www/ws",
		DevServices: []topology.DevServiceRef{{Hostname: "apidev", SetupName: "dev-api"}}}
	meta.BootstrapSession = "s1"
	meta.BootstrappedAt = "2026-10-15T00:00:00Z"
	if err := workflow.WriteServiceMeta(f.stateDir, meta); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestStandup_VariantB_FillsTheVolumeOnceAndEveryDevServiceDeploysItsOwnDevSetup(t *testing.T) {
	t.Parallel()
	f := variantBStandupFixture(t)

	result, body := f.run(t)
	if result.IsError || body.StandUp != standupDevelopment {
		t.Fatalf("first call: %s", getTextContent(t, result))
	}

	// Development: the dev half and the further dev service each deploy their
	// own dev setup from the checkout in /var/www — the volume is still empty.
	pushes := f.ssh.pushes()
	for _, want := range []string{"medusadev → svc-medusadev (medusadev)", "medusadev → svc-apidev (dev-api)"} {
		if !slices.Contains(pushes, want) {
			t.Errorf("deploys = %v, want %q", pushes, want)
		}
	}
	for _, cmd := range f.ssh.commandsFor("zcli push") {
		if (strings.Contains(cmd, "svc-medusadev") || strings.Contains(cmd, "svc-apidev")) && !strings.HasPrefix(cmd, "cd '/var/www' &&") {
			t.Errorf("a dev setup deploys from the checkout in /var/www: %s", cmd)
		}
	}

	// Then the volume is filled once, from the dev half, and every dev
	// service's supervisor reloads the declaration it now holds.
	order := f.ssh.order(t)
	fill := order.index("medusadev", "@@zcp code-volume")
	if fill < 0 || fill < order.index("medusadev", "svc-medusadev") || fill < order.index("medusadev", "svc-apidev") {
		t.Errorf("the volume is filled after both dev setups deploy:\n%s", order)
	}
	if n := len(f.ssh.commandsFor("@@zcp code-volume")); n != 1 {
		t.Errorf("the volume was filled %d times, want once", n)
	}
	for _, host := range []string{"medusadev", "apidev"} {
		if reload := order.index(host, "zcp-devsup ctl 'reload'"); reload < fill {
			t.Errorf("%s's supervisor reloads after the fill:\n%s", host, order)
		}
	}

	dev := body.service(t, "medusadev")
	if dev.CodeVolume == nil || dev.CodeVolume.Filled != standupNow || len(dev.CodeVolume.Reloads) != 0 {
		t.Errorf("medusadev's code volume = %+v", dev.CodeVolume)
	}
	api := body.service(t, "apidev")
	if api.Role != standupRoleDev || api.Pair != "medusadev" || api.Setup != "dev-api" || api.Deploy == nil || api.Deploy.Status != standupDeployed {
		t.Errorf("apidev = %+v", api)
	}

	// apidev stands up with medusadev's pair, never as a pair of its own.
	if own, _ := workflow.ReadServiceMeta(f.stateDir, "apidev"); own != nil {
		t.Errorf("apidev recorded as a pair of its own: %+v", own)
	}
	if meta, _ := workflow.FindServiceMeta(f.stateDir, "apidev"); meta == nil || meta.Hostname != "medusadev" || meta.CodeVolume == nil {
		t.Errorf("apidev resolves to %+v, want medusadev's variant-B pair", meta)
	}
	if stage := body.service(t, "apistage"); stage.Pair != "medusadev" || stage.Deploy == nil || stage.Deploy.Status != standupQueued ||
		!strings.Contains(stage.Next, `targetService="apistage"`) {
		t.Errorf("apistage = %+v", stage)
	}

	// The second call builds the stage from the volume; the volume, already
	// filled, is left as it is.
	if err := os.MkdirAll(filepath.Join(f.root, "medusadev", "ws"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "medusadev", "ws", "zerops.yaml"), []byte(f.ssh.zeropsYAML["medusadev"]), 0o600); err != nil {
		t.Fatal(err)
	}
	f.deployed("medusadev", "nextstoredev", "apidev")
	result, body = f.run(t)
	if result.IsError {
		t.Fatalf("second call: %s", getTextContent(t, result))
	}
	if dev := body.service(t, "medusadev"); dev.CodeVolume == nil || dev.CodeVolume.Filled != standupAlready {
		t.Errorf("second call: medusadev's code volume = %+v", dev.CodeVolume)
	}
	for _, host := range []string{"medusastage", "apistage"} {
		if stage := body.service(t, host); stage.Deploy == nil || stage.Deploy.Status != standupDeployed {
			t.Errorf("second call: %s = %+v", host, stage)
		}
	}
	if body.StandUp != standupReady {
		t.Errorf("second call: standUp = %q", body.StandUp)
	}
}

// TestStandup_FreshTierWithALocalStorageService_SetsUpNoCodeVolume
// (docs/spec-mate.md §2.8a.4a): zcp sets up no new code volume. A fresh tier
// whose repository builds two dev services and two stages beside a
// local-storage service stands up as two ordinary pairs, each stage with the
// dev service named like it and each dev service holding its own checkout:
// no pair records a code volume, nothing is filled, and the volume stays a
// managed service of the project.
func TestStandup_FreshTierWithALocalStorageService_SetsUpNoCodeVolume(t *testing.T) {
	t.Parallel()
	f := variantBTierFixture(t)
	// apidev is a dev half of its own now: the branch cut puts main's files
	// in it too, and its mount shows them.
	body := f.ssh.zeropsYAML["medusadev"]
	if err := os.MkdirAll(filepath.Join(f.root, "apidev"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "apidev", "zerops.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	f.ssh.zeropsYAML["apidev"] = body

	result, report := f.run(t)
	if result.IsError || report.StandUp != standupDevelopment {
		t.Fatalf("first call: %s", getTextContent(t, result))
	}
	metas, err := workflow.ListServiceMetas(f.stateDir)
	if err != nil {
		t.Fatalf("ListServiceMetas: %v", err)
	}
	for _, m := range metas {
		if m.CodeVolume != nil {
			t.Errorf("%s records a code volume: %+v", m.Hostname, m.CodeVolume)
		}
	}
	for dev, stage := range map[string]string{"medusadev": "medusastage", "apidev": "apistage"} {
		meta, _ := workflow.ReadServiceMeta(f.stateDir, dev)
		if meta == nil || !slices.Equal(meta.StageHostnames(), []string{stage}) {
			t.Errorf("%s = %+v, want a pair of its own with the stage %s", dev, meta, stage)
		}
	}
	if n := len(f.ssh.commandsFor("@@zcp code-volume")); n != 0 {
		t.Errorf("the volume was filled %d times, want never", n)
	}
	pushes := f.ssh.pushes()
	for _, wantPush := range []string{"medusadev → svc-medusadev (medusadev)", "apidev → svc-apidev (dev-api)"} {
		if !slices.Contains(pushes, wantPush) {
			t.Errorf("deploys = %v, want %q", pushes, wantPush)
		}
	}
	if s := report.service(t, "vol"); s.Role != standupRoleManaged {
		t.Errorf("vol = %+v, want a managed service the stand-up leaves as it is", s)
	}
}

// sshOrder is every SSH command in order, by host.
type sshOrder []standupSSHCall

func (s *standupSSH) order(t *testing.T) sshOrder {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(sshOrder(s.commands))
}

// index is the place of the first command on host containing substr, -1 for none.
func (o sshOrder) index(host, substr string) int {
	for i, c := range o {
		if c.host == host && strings.Contains(c.cmd, substr) {
			return i
		}
	}
	return -1
}

func (o sshOrder) String() string {
	lines := make([]string, 0, len(o))
	for _, c := range o {
		lines = append(lines, c.host+": "+c.cmd[:min(len(c.cmd), 120)])
	}
	return strings.Join(lines, "\n")
}
