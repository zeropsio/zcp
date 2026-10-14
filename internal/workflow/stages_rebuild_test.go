package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// An old zcp writes a pair as one stageHostname/stageSetupName and drops
// stages (spec-mate §2.8a.4). These metas are what it leaves on disk.
const (
	oldPairMeta        = `{"hostname":"appdev","mode":"standard","stageHostname":"appstage","primarySetupName":"dev","stageSetupName":"prod","bootstrapSession":"s","bootstrappedAt":"t"}`
	newPairMeta        = `{"hostname":"appdev","mode":"standard","stages":[{"hostname":"appstage","setupName":"prod"}],"stageHostname":"appstage","primarySetupName":"dev","stageSetupName":"prod","bootstrapSession":"s","bootstrappedAt":"t"}`
	oldLocalStageMeta  = `{"hostname":"myproj","mode":"local-stage","stageHostname":"appstage","stageSetupName":"prod","bootstrapSession":"s","bootstrappedAt":"t"}`
	oldSimpleMeta      = `{"hostname":"app","mode":"simple","bootstrapSession":"s","bootstrappedAt":"t"}`
	oldAPIDevPairMeta  = `{"hostname":"apidev","mode":"standard","stageHostname":"apistage","primarySetupName":"dev-api","stageSetupName":"api","bootstrapSession":"s","bootstrappedAt":"t"}`
	singlePartYml      = "zerops:\n  - setup: dev\n    run: {base: nodejs@22}\n  - setup: prod\n    run: {base: nodejs@22}\n"
	singlePartBasedYml = "zerops:\n  - setup: base\n    run: {base: nodejs@22}\n  - setup: dev\n    extends: base\n  - setup: prod\n    extends: [base]\n"
	threePartYml       = "zerops:\n  - setup: dev\n    run: {base: bun@1.3}\n  - setup: prod\n    run: {base: bun@1.3}\n  - setup: api\n    run: {base: go@1}\n  - setup: worker\n    run: {base: bun@1.3}\n"
)

const rebuildTier = `services:
  - hostname: db
    type: postgresql@16
  - hostname: appdev
    type: bun@1.3
    zeropsSetup: dev
    buildFromGit: https://hq.acme.example/git/app-1/appdev
  - hostname: appstage
    type: bun@1.3
    zeropsSetup: prod
    buildFromGit: https://hq.acme.example/git/app-1/appdev
  - hostname: api
    type: go@1
    zeropsSetup: api
    buildFromGit: https://hq.acme.example/git/app-1/appdev
  - hostname: worker
    type: bun@1.3
    zeropsSetup: worker
    buildFromGit: https://hq.acme.example/git/app-1/appdev
  - hostname: otherapi
    type: go@1
    zeropsSetup: api
    buildFromGit: https://hq.acme.example/git/app-1/otherdev
`

func readRebuildMeta(t *testing.T, raw string) *ServiceMeta {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "services"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "services", "m.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	metas, err := ListServiceMetas(dir)
	if err != nil || len(metas) != 1 {
		t.Fatalf("read meta: %v (%d metas)", err, len(metas))
	}
	return metas[0]
}

func TestRebuildStages_MetaWithoutStages_StagesFromZeropsYmlPartsAndTier(t *testing.T) {
	t.Parallel()
	appstage := topology.StageRef{Hostname: "appstage", SetupName: "prod"}
	tests := []struct {
		name    string
		meta    string
		others  []string
		yml     string
		tier    string
		wantDue bool
		want    []topology.StageRef
	}{
		{
			name: "an old meta of a single-part app stays as it is",
			meta: oldPairMeta, yml: singlePartYml, tier: rebuildTier,
			want: []topology.StageRef{appstage},
		},
		{
			name: "a base only extends names is no part, so the app is single-part",
			meta: oldPairMeta, yml: singlePartBasedYml, tier: rebuildTier,
			want: []topology.StageRef{appstage},
		},
		{
			name: "an old meta of a three-part app gets a stage per part, the mirror first, each hostname from the tier",
			meta: oldPairMeta, yml: threePartYml, tier: rebuildTier, wantDue: true,
			want: []topology.StageRef{appstage, {Hostname: "api", SetupName: "api"}, {Hostname: "worker", SetupName: "worker"}},
		},
		{
			name: "a part no tier entry of the pair's repository names stays out",
			meta: oldPairMeta, yml: threePartYml, wantDue: true,
			tier: `services:
  - hostname: appdev
    zeropsSetup: dev
    buildFromGit: https://hq.acme.example/git/app-1/appdev
  - hostname: appstage
    zeropsSetup: prod
    buildFromGit: https://hq.acme.example/git/app-1/appdev
  - hostname: worker
    zeropsSetup: worker
    buildFromGit: https://hq.acme.example/git/app-1/appdev
  - hostname: otherapi
    zeropsSetup: api
    buildFromGit: https://hq.acme.example/git/app-1/otherdev
`,
			want: []topology.StageRef{appstage, {Hostname: "worker", SetupName: "worker"}},
		},
		{
			name: "a part two tier entries name stays out, never guessed",
			meta: oldPairMeta, yml: threePartYml, wantDue: true,
			tier: rebuildTier + `  - hostname: api2
    zeropsSetup: api
    buildFromGit: https://hq.acme.example/git/app-1/appdev
`,
			want: []topology.StageRef{appstage, {Hostname: "worker", SetupName: "worker"}},
		},
		{
			name: "a tier that does not name the dev half adds no stage",
			meta: oldPairMeta, yml: threePartYml, wantDue: true,
			tier: `services:
  - hostname: api
    zeropsSetup: api
    buildFromGit: https://hq.acme.example/git/app-1/appdev
`,
			want: []topology.StageRef{appstage},
		},
		{
			name: "a tier hostname another pair records stays with that pair",
			meta: oldPairMeta, yml: threePartYml, tier: rebuildTier, wantDue: true,
			others: []string{`{"hostname":"apidev","mode":"standard","stages":[{"hostname":"api","setupName":"api"}],"primarySetupName":"dev-api","bootstrapSession":"s","bootstrappedAt":"t"}`},
			want:   []topology.StageRef{appstage, {Hostname: "worker", SetupName: "worker"}},
		},
		{
			name: "parts of a repository two dev services hold stay out: which pair owns them is not known",
			meta: oldPairMeta, yml: threePartYml + "  - setup: dev-api\n    run: {base: go@1}\n", wantDue: true,
			others: []string{oldAPIDevPairMeta},
			tier: rebuildTier + `  - hostname: apidev
    zeropsSetup: dev-api
    buildFromGit: https://hq.acme.example/git/app-1/appdev
`,
			want: []topology.StageRef{appstage},
		},
		{
			name: "a meta that records stages is never rebuilt",
			meta: newPairMeta, yml: threePartYml, tier: rebuildTier,
			want: []topology.StageRef{appstage},
		},
		{
			name: "a local-stage meta keeps its one stage",
			meta: oldLocalStageMeta, yml: threePartYml, tier: rebuildTier,
			want: []topology.StageRef{appstage},
		},
		{
			name: "a single-service meta gets no stage",
			meta: oldSimpleMeta, yml: threePartYml, tier: rebuildTier,
			want: nil,
		},
		{
			name: "an unreadable zerops.yml rebuilds nothing",
			meta: oldPairMeta, yml: "zerops: [", tier: rebuildTier,
			want: []topology.StageRef{appstage},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			meta := readRebuildMeta(t, tt.meta)
			others := make([]*ServiceMeta, 0, len(tt.others))
			for _, raw := range tt.others {
				others = append(others, readRebuildMeta(t, raw))
			}
			due := StagesRebuildDue(meta, others, tt.yml)
			if due != tt.wantDue {
				t.Fatalf("StagesRebuildDue = %v, want %v", due, tt.wantDue)
			}
			got := meta.Stages
			if due {
				got = RebuildStages(meta, others, tt.yml, tt.tier)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("stages = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestRebuildStages_WrittenMeta_NoLongerDue pins "zcp then writes the meta":
// a rebuilt meta records stages, so the next load does not rebuild it, and
// the mirror still names the first stage for an older zcp.
func TestRebuildStages_WrittenMeta_NoLongerDue(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "services"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "services", "appdev.json"), []byte(oldPairMeta), 0o600); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatal(err)
	}
	meta.Stages = RebuildStages(meta, nil, threePartYml, rebuildTier)
	if err := WriteServiceMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	again, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatal(err)
	}
	if StagesRebuildDue(again, nil, threePartYml) {
		t.Error("a written meta is due for a rebuild again")
	}
	if got := again.StageHostnames(); !slices.Equal(got, []string{"appstage", "api", "worker"}) {
		t.Errorf("stages after write = %v, want [appstage api worker]", got)
	}
	var legacy legacyStageKeys
	raw, _ := os.ReadFile(filepath.Join(dir, "services", "appdev.json"))
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.StageHostname != "appstage" || legacy.StageSetupName != "prod" {
		t.Errorf("mirror = %+v, want appstage/prod", legacy)
	}
}
