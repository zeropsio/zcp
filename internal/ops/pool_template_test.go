package ops

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/hq"
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

// PT-SEED-UNBORN — A repository with no main is never seeded and a main
// holding a file is never written: only HQ's own empty first commit takes
// the seed.
// witness: internal/ops/pool_template.go | case !found: → case found:
func TestPoolTemplate_PTSeedUnborn_OnlyHQsEmptyFirstCommitIsSeeded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		found bool
		paths []string
		want  SeedMain
	}{
		{"no main", false, nil, SeedMainUnborn},
		{"HQ's empty first commit", true, nil, SeedMainEmpty},
		{"main holding code", true, []string{"index.js"}, SeedMainWritten},
	}
	for _, tt := range tests {
		if got := SeedMainOf(tt.found, tt.paths); got != tt.want {
			t.Errorf("%s: SeedMainOf = %d, want %d", tt.name, got, tt.want)
		}
	}
}

// seedFileSet is a seed's files by path: the paths are what SeedOnto reads.
var seedFileSet = map[string]string{"zerops.yaml": "zerops: []\n", ".zcp/dev-servers.json": "{}\n"}

// PT-SEED-FOREIGN — The seed is written only onto a change opened for it:
// another open change HQ hands back, or a seed change whose head holds other
// files, is refused and written nothing.
// witness: internal/ops/pool_template.go | case !c.Created && c.Title != SeedTitle: → case !c.Created && c.Title == SeedTitle:
func TestPoolTemplate_PTSeedForeign_AnotherChangeIsWrittenNothing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change SeedChange
	}{
		{"the Mate's default change, nothing pushed yet", SeedChange{Title: "Mate change"}},
		{"the Mate's change of code", SeedChange{Title: "Add the API", HeadFound: true, HeadTree: "t-code", HeadPaths: []string{"index.js", "zerops.yaml"}}},
		{"a seed change a push added code to", SeedChange{Title: SeedTitle, HeadFound: true, HeadTree: "t-more", HeadPaths: []string{".zcp/dev-servers.json", "index.js", "zerops.yaml"}}},
	}
	for _, tt := range tests {
		if got := SeedOnto(tt.change, "t-seed", seedFileSet); got != SeedWriteRefused {
			t.Errorf("%s: SeedOnto = %d, want SeedWriteRefused (%d)", tt.name, got, SeedWriteRefused)
		}
	}
}

// PT-SEED-RESENT — A plan sent again after a stop reuses its open seed
// change: a head that is the seed already is written nothing, a head holding
// the seed's files with other content gets one commit on it.
// witness: internal/ops/pool_template.go | case c.HeadTree == seedTree: → case c.HeadTree != seedTree:
func TestPoolTemplate_PTSeedResent_OwnOpenChangeIsReused(t *testing.T) {
	t.Parallel()
	seedPaths := []string{".zcp/dev-servers.json", "zerops.yaml"}
	tests := []struct {
		name   string
		change SeedChange
		want   SeedWrite
	}{
		{"opened now", SeedChange{Created: true, Title: SeedTitle}, SeedWriteRoot},
		{"own change, nothing pushed before the stop", SeedChange{Title: SeedTitle}, SeedWriteRoot},
		{"own change, the seed pushed before the stop", SeedChange{Title: SeedTitle, HeadFound: true, HeadTree: "t-seed", HeadPaths: seedPaths}, SeedWriteNothing},
		{"own change, an older template's seed", SeedChange{Title: SeedTitle, HeadFound: true, HeadTree: "t-older", HeadPaths: seedPaths}, SeedWriteOnHead},
	}
	for _, tt := range tests {
		if got := SeedOnto(tt.change, "t-seed", seedFileSet); got != tt.want {
			t.Errorf("%s: SeedOnto = %d, want %d", tt.name, got, tt.want)
		}
	}
}

// PT-SEED-ORPHAN — A seed change HQ did not land is closed once main was
// written by another, and left as it is when main holds the seed's landing.
// witness: internal/ops/pool_template.go | return mainWritten && !seedOnMain → return mainWritten
func TestPoolTemplate_PTSeedOrphan_ClosedOnlyWhenAnotherWroteMain(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                    string
		mainWritten, seedOnMain bool
		want                    bool
	}{
		{"another wrote main", true, false, true},
		{"Core landed the seed, its answer lost", true, true, false},
		{"main still empty", false, false, false},
	}
	for _, tt := range tests {
		if got := SeedOrphaned(tt.mainWritten, tt.seedOnMain); got != tt.want {
			t.Errorf("%s: SeedOrphaned = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// PT-SEED-LEFT-OPEN — A plan sent again onto a main another wrote closes the
// seed change an earlier run left open, and closes no other change.
// witness: internal/ops/pool_template.go | return state == hq.ChangeOpen && title == SeedTitle → return state == hq.ChangeOpen || title == SeedTitle
func TestPoolTemplate_PTSeedLeftOpen_ClosedOnAMainAnotherWrote(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		title, state string
		onMain       bool
		want         bool
	}{
		{"the seed change left open, main written by a person's change", SeedTitle, hq.ChangeOpen, false, true},
		{"the Mate's open change of code", "Add the API", hq.ChangeOpen, false, false},
		{"the seed change merged", SeedTitle, hq.ChangeMerged, true, false},
		{"the seed change closed", SeedTitle, hq.ChangeClosed, false, false},
	}
	for _, tt := range tests {
		if got := SeedLeftOpen(tt.title, tt.state) && SeedOrphaned(true, tt.onMain); got != tt.want {
			t.Errorf("%s: closed = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// PT-SEED-LANDED-BENEATH — Closing a seed change HQ answers no longer open
// (landed beneath a change on top of it, or closed meanwhile) settles the
// seed; any other answer fails the plan.
// witness: internal/ops/pool_template.go | refused.Reason == "change_not_open" → refused.Reason == "change_not_found"
func TestPoolTemplate_PTSeedLandedBeneath_NotOpenIsSettled(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"closed now", nil, true},
		{"merged beneath a later change", fmt.Errorf("close: %w", &hq.RefusedError{Status: http.StatusConflict, Code: "conflict", Reason: "change_not_open"}), true},
		{"not the Mate's own", &hq.RefusedError{Status: http.StatusForbidden, Code: "forbidden"}, false},
		{"HQ did not answer", errors.New("connection reset"), false},
	}
	for _, tt := range tests {
		if got := SeedCloseDone(tt.err); got != tt.want {
			t.Errorf("%s: SeedCloseDone = %v, want %v", tt.name, got, tt.want)
		}
	}
}
