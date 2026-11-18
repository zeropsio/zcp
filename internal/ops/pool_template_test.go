package ops

import (
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/devsup"
)

// seedSetupsOf decodes the seed's zerops.yaml into its setups by name.
func seedSetupsOf(t *testing.T, files map[string]string) map[string]seedSetup {
	t.Helper()
	var doc seedDoc
	if err := yaml.Unmarshal([]byte(files["zerops.yaml"]), &doc); err != nil {
		t.Fatalf("seed zerops.yaml does not decode: %v", err)
	}
	setups := map[string]seedSetup{}
	for _, s := range doc.Zerops {
		setups[s.Setup] = s
	}
	return setups
}

// PT-SEED-POOL — A seed gives each part a pool setup named after its pool
// that builds and runs on the part's own runtime type, installs zcp-devsup
// and starts `zcp-devsup run`.
// witness: internal/ops/pool_template.go | Base:            p.Type, → Base:            bundle.PoolType,
func TestPoolTemplate_PTSeedPool_PoolSetupOnPartTypeUnderSupervisor(t *testing.T) {
	t.Parallel()
	files, err := SeedFiles([]PoolPart{{Part: "app", Pool: "apppool", Stage: "appstage", Type: "nodejs@22"}})
	if err != nil {
		t.Fatalf("SeedFiles: %v", err)
	}
	pool, ok := seedSetupsOf(t, files)["apppool"]
	if !ok {
		t.Fatalf("seed has no setup %q:\n%s", "apppool", files["zerops.yaml"])
	}
	if pool.Build.Base != "nodejs@22" || pool.Run.Base != "nodejs@22" {
		t.Errorf("pool setup bases = build %q, run %q, want nodejs@22 for both", pool.Build.Base, pool.Run.Base)
	}
	if pool.Run.Start != "zcp-devsup run" {
		t.Errorf("pool setup run.start = %q, want %q", pool.Run.Start, "zcp-devsup run")
	}
	if !slices.Contains(pool.Run.PrepareCommands, DevsupPrepareCommand(DevsupLatestURL)) {
		t.Errorf("pool setup prepareCommands = %q, want the zcp-devsup install line", pool.Run.PrepareCommands)
	}
}

// PT-SEED-PORTS — A seed's pool setup declares exactly the ports
// previewPortBase + 1 … previewPortBase + maxContainers, each with
// httpSupport.
// witness: internal/ops/pool_template.go | for port := SeedPreviewPortBase + 1; → for port := SeedPreviewPortBase;
func TestPoolTemplate_PTSeedPorts_ExactlyOnePreviewPortPerContainer(t *testing.T) {
	t.Parallel()
	files, err := SeedFiles([]PoolPart{{Part: "app", Pool: "apppool", Stage: "appstage", Type: "nodejs@22", MaxContainers: 5}})
	if err != nil {
		t.Fatalf("SeedFiles: %v", err)
	}
	pool := seedSetupsOf(t, files)["apppool"]
	want := []seedPort{
		{Port: 4001, HTTPSupport: true}, {Port: 4002, HTTPSupport: true}, {Port: 4003, HTTPSupport: true},
		{Port: 4004, HTTPSupport: true}, {Port: 4005, HTTPSupport: true},
	}
	if !slices.Equal(pool.Run.Ports, want) {
		t.Errorf("pool setup run.ports = %v, want %v", pool.Run.Ports, want)
	}
}

// PT-SEED-STAGE — A seed gives each part a stage setup named after the part
// that deploys ./, with the idle start for a runtime that takes a start
// command and none for a static one.
// witness: internal/ops/pool_template.go | if topology.RuntimeClassFor(p.Type) == topology.RuntimeDynamic { → if topology.RuntimeClassFor(p.Type) != topology.RuntimeDynamic {
func TestPoolTemplate_PTSeedStage_StageSetupIdleStartOnlyWhereTaken(t *testing.T) {
	t.Parallel()
	files, err := SeedFiles([]PoolPart{
		{Part: "api", Pool: "apipool", Stage: "apistage", Type: "nodejs@22"},
		{Part: "web", Pool: "webpool", Stage: "webstage", Type: "static"},
	})
	if err != nil {
		t.Fatalf("SeedFiles: %v", err)
	}
	setups := seedSetupsOf(t, files)
	for _, tc := range []struct{ part, base, start string }{
		{part: "api", base: "nodejs@22", start: "zsc noop --silent"},
		{part: "web", base: "static", start: ""},
	} {
		stage, ok := setups[tc.part]
		if !ok {
			t.Fatalf("seed has no stage setup %q:\n%s", tc.part, files["zerops.yaml"])
		}
		if stage.Build.Base != tc.base || stage.Run.Base != tc.base || stage.Build.DeployFiles != "./" {
			t.Errorf("stage %q = build %q deployFiles %q, run %q; want %q, ./, %q", tc.part, stage.Build.Base, stage.Build.DeployFiles, stage.Run.Base, tc.base, tc.base)
		}
		if stage.Run.Start != tc.start {
			t.Errorf("stage %q run.start = %q, want %q", tc.part, stage.Run.Start, tc.start)
		}
	}
}

// PT-CHECK-MISSING — A main holding code without the parts' setups is
// refused naming each part's missing pool setup, each missing stage setup and
// the missing dev-server declaration.
// witness: internal/ops/pool_template.go | if doc.FindEntry(p.Part) == nil { → if doc.FindEntry(p.Part) != nil {
func TestPoolTemplate_PTCheckMissing_CodeWithoutSetups_NamesEachGap(t *testing.T) {
	t.Parallel()
	const code = "zerops:\n  - setup: prod\n    build:\n      base: nodejs@22\n    run:\n      base: nodejs@22\n      start: node server.js\n"
	lacks := CheckPoolSetups(code, "", []PoolPart{
		{Part: "api", Pool: "apipool", Stage: "apistage"},
		{Part: "web", Pool: "webpool", Stage: "webstage"},
	})
	for _, name := range []string{`"apipool"`, `"api"`, `"webpool"`, `"web"`, devsup.DeclarationPath} {
		if !slices.ContainsFunc(lacks, func(line string) bool { return strings.Contains(line, name) }) {
			t.Errorf("no gap names %s; gaps:\n%s", name, strings.Join(lacks, "\n"))
		}
	}
}

// PT-CHECK-SEED — The seed's own two files pass the setup check, so a plan
// sent again on a seeded main writes nothing and is never refused.
// witness: internal/ops/pool_template.go | PreviewPortBase: SeedPreviewPortBase, → PreviewPortBase: 0,
func TestPoolTemplate_PTCheckSeed_SeedPassesTheCheck(t *testing.T) {
	t.Parallel()
	parts := []PoolPart{
		{Part: "api", Pool: "apipool", Stage: "apistage", Type: "nodejs@22", MaxContainers: 4},
		{Part: "web", Pool: "webpool", Stage: "webstage", Type: "static"},
	}
	files, err := SeedFiles(parts)
	if err != nil {
		t.Fatalf("SeedFiles: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("seed files = %d, want 2 (zerops.yaml and %s)", len(files), devsup.DeclarationPath)
	}
	if lacks := CheckPoolSetups(files["zerops.yaml"], files[devsup.DeclarationPath], parts); len(lacks) != 0 {
		t.Errorf("the seed lacks:\n%s", strings.Join(lacks, "\n"))
	}
}
