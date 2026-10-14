package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

func threeStagePair() *ServiceMeta {
	m := NewServiceMeta("appdev", topology.PlanModeStandard)
	m.PrimarySetupName = "dev"
	m.Stages = []topology.StageRef{
		{Hostname: "webstage", SetupName: "web"},
		{Hostname: "apistage", SetupName: "api"},
		{Hostname: "workerstage"},
	}
	m.BootstrappedAt = "2026-10-14T00:00:00Z"
	return m
}

func TestReadServiceMeta_LegacyStageFields_ReadAsOneStage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		json string
		want []topology.StageRef
	}{
		{
			name: "an old pair meta reads as one stage with the same hostname and setup",
			json: `{"hostname":"appdev","mode":"standard","stageHostname":"appstage","primarySetupName":"dev","stageSetupName":"prod","bootstrapSession":"s","bootstrappedAt":"t"}`,
			want: []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}},
		},
		{
			name: "an old pair meta without a recorded stage setup reads as one stage with no setup",
			json: `{"hostname":"appdev","mode":"standard","stageHostname":"appstage","bootstrapSession":"s","bootstrappedAt":"t"}`,
			want: []topology.StageRef{{Hostname: "appstage"}},
		},
		{
			name: "an old single-service meta reads with no stage",
			json: `{"hostname":"app","mode":"simple","bootstrapSession":"s","bootstrappedAt":"t"}`,
			want: nil,
		},
		{
			name: "stages win over the mirrored old keys",
			json: `{"hostname":"appdev","mode":"standard","stages":[{"hostname":"webstage","setupName":"web"},{"hostname":"apistage"}],"stageHostname":"oldstage","stageSetupName":"old","bootstrapSession":"s","bootstrappedAt":"t"}`,
			want: []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage"}},
		},
		{
			name: "a meta that records stages reads them as written",
			json: `{"hostname":"appdev","mode":"standard","stages":[{"hostname":"webstage","setupName":"web"},{"hostname":"apistage","setupName":"api"}],"bootstrapSession":"s","bootstrappedAt":"t"}`,
			want: []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			host := "appdev"
			if strings.Contains(tt.json, `"hostname":"app",`) {
				host = "app"
			}
			if err := os.MkdirAll(filepath.Join(dir, "services"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "services", host+".json"), []byte(tt.json), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := ReadServiceMeta(dir, host)
			if err != nil {
				t.Fatalf("ReadServiceMeta: %v", err)
			}
			if !slices.Equal(got.Stages, tt.want) {
				t.Errorf("Stages = %+v, want %+v", got.Stages, tt.want)
			}
		})
	}
}

// An older zcp (the release a container restart pulls back) decodes the meta
// into its fixed struct and knows only stageHostname/stageSetupName; every
// write mirrors the first stage there so it still sees the pair.
func TestWriteServiceMeta_MirrorsTheFirstStageIntoTheOldKeys(t *testing.T) {
	t.Parallel()
	type olderZCP struct {
		StageHostname  string `json:"stageHostname"`
		StageSetupName string `json:"stageSetupName"`
	}
	tests := []struct {
		name   string
		stages []topology.StageRef
		want   olderZCP
		// wantStages is the stages key as written, re-read through ReadServiceMeta.
		wantStages []topology.StageRef
	}{
		{
			name:       "a one-stage pair keeps today's keys beside stages",
			stages:     []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}},
			want:       olderZCP{StageHostname: "appstage", StageSetupName: "prod"},
			wantStages: []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}},
		},
		{
			name:       "a three-stage pair mirrors its first stage",
			stages:     threeStagePair().Stages,
			want:       olderZCP{StageHostname: "webstage", StageSetupName: "web"},
			wantStages: threeStagePair().Stages,
		},
		{
			name: "a single service writes no stage key",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			m := NewServiceMeta("appdev", topology.PlanModeStandard)
			m.Stages = tt.stages
			if err := WriteServiceMeta(dir, m); err != nil {
				t.Fatalf("WriteServiceMeta: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "services", "appdev.json"))
			if err != nil {
				t.Fatal(err)
			}
			var older olderZCP
			if err := json.Unmarshal(data, &older); err != nil {
				t.Fatal(err)
			}
			if older != tt.want {
				t.Errorf("older zcp reads %+v, want %+v:\n%s", older, tt.want, data)
			}
			if tt.stages == nil && strings.Contains(string(data), "stage") {
				t.Errorf("a stageless meta carries a stage key:\n%s", data)
			}
			got, err := ReadServiceMeta(dir, "appdev")
			if err != nil {
				t.Fatalf("ReadServiceMeta: %v", err)
			}
			if !slices.Equal(got.Stages, tt.wantStages) {
				t.Errorf("Stages = %+v, want %+v", got.Stages, tt.wantStages)
			}
		})
	}
}

// A meta an older zcp wrote back (mirror only, stages dropped) and the next
// write by this zcp: the pair survives the round trip.
func TestWriteServiceMeta_MigratedLegacyMeta_WritesStagesAndTheMirror(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "services"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"hostname":"appdev","mode":"standard","stageHostname":"appstage","stageSetupName":"prod","bootstrapSession":"s","bootstrappedAt":"t"}`
	if err := os.WriteFile(filepath.Join(dir, "services", "appdev.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := UpdateServiceMeta(dir, "appstage", func(m *ServiceMeta) error {
		m.FirstDeployedAt = "2026-10-14T00:00:00Z"
		return nil
	}); err != nil {
		t.Fatalf("UpdateServiceMeta: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "services", "appdev.json"))
	if err != nil {
		t.Fatal(err)
	}
	var written struct {
		Stages         []topology.StageRef `json:"stages"`
		StageHostname  string              `json:"stageHostname"`
		StageSetupName string              `json:"stageSetupName"`
	}
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatal(err)
	}
	if want := []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}}; !slices.Equal(written.Stages, want) || written.StageHostname != "appstage" || written.StageSetupName != "prod" {
		t.Errorf("rewritten meta = %+v, want stages %+v and the appstage/prod mirror:\n%s", written, want, data)
	}
}

func TestServiceMeta_ThreeStagePair_EveryStageIsAHalfOfThePair(t *testing.T) {
	t.Parallel()
	m := threeStagePair()

	if got, want := m.Hostnames(), []string{"appdev", "webstage", "apistage", "workerstage"}; !slices.Equal(got, want) {
		t.Errorf("Hostnames() = %v, want %v", got, want)
	}
	for _, tt := range []struct {
		host      string
		role      topology.Mode
		mode      topology.Mode
		setup     string
		pushCheck topology.PushSourceResult
	}{
		{"appdev", topology.DeployRoleDev, topology.ModeStandard, "dev", topology.PushSourceOK},
		{"webstage", topology.DeployRoleStage, topology.ModeStage, "web", topology.PushSourceIsStageHalf},
		{"apistage", topology.DeployRoleStage, topology.ModeStage, "api", topology.PushSourceIsStageHalf},
		{"workerstage", topology.DeployRoleStage, topology.ModeStage, "", topology.PushSourceIsStageHalf},
		{"other", "", "", "", topology.PushSourceUnknownHost},
	} {
		if got := m.RoleFor(tt.host); got != tt.role {
			t.Errorf("RoleFor(%q) = %q, want %q", tt.host, got, tt.role)
		}
		if got := m.ModeFor(tt.host); got != tt.mode {
			t.Errorf("ModeFor(%q) = %q, want %q", tt.host, got, tt.mode)
		}
		if got := m.SetupNameFor(tt.host); got != tt.setup {
			t.Errorf("SetupNameFor(%q) = %q, want %q", tt.host, got, tt.setup)
		}
		if got := m.PushSourceCheckFor(tt.host); got != tt.pushCheck {
			t.Errorf("PushSourceCheckFor(%q) = %v, want %v", tt.host, got, tt.pushCheck)
		}
	}

	idx := ManagedRuntimeIndex([]*ServiceMeta{m})
	for _, h := range []string{"appdev", "webstage", "apistage", "workerstage"} {
		if idx[h] != m {
			t.Errorf("ManagedRuntimeIndex[%q] does not resolve to the pair", h)
		}
	}

	dir := t.TempDir()
	if err := WriteServiceMeta(dir, m); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"webstage", "apistage", "workerstage"} {
		found, err := FindServiceMeta(dir, h)
		if err != nil || found == nil || found.Hostname != "appdev" {
			t.Errorf("FindServiceMeta(%q) = %v, %v; want the appdev pair", h, found, err)
		}
		if !IsKnownService(dir, h) {
			t.Errorf("IsKnownService(%q) = false, want true", h)
		}
	}
	if deleted := PruneServiceMetas(dir, map[string]bool{"apistage": true}); len(deleted) != 0 {
		t.Errorf("PruneServiceMetas removed %v while one of its stages is live", deleted)
	}
}

func TestResolveCanonicalSetup_StageOfThreeStagePair_RecordsThatStagesSetup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m := threeStagePair()
	if err := WriteServiceMeta(dir, m); err != nil {
		t.Fatal(err)
	}
	yaml := "zerops:\n  - setup: dev\n  - setup: web\n  - setup: api\n  - setup: worker\n  - setup: workerstage\n"
	got, err := ResolveCanonicalSetup(t.Context(), nil, ResolveCanonicalSetupInput{
		StateDir: dir, TargetHostname: "workerstage", Mode: topology.ModeStage, LocalYAMLBody: yaml,
	})
	if err != nil || got != "workerstage" {
		t.Fatalf("ResolveCanonicalSetup = %q, %v; want workerstage", got, err)
	}
	// The cache now answers for that stage, and only that stage changed.
	got, err = ResolveCanonicalSetup(t.Context(), nil, ResolveCanonicalSetupInput{StateDir: dir, TargetHostname: "workerstage"})
	if err != nil || got != "workerstage" {
		t.Fatalf("cached ResolveCanonicalSetup = %q, %v; want workerstage", got, err)
	}
	read, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatal(err)
	}
	want := []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}, {Hostname: "workerstage", SetupName: "workerstage"}}
	if !slices.Equal(read.Stages, want) {
		t.Errorf("Stages = %+v, want %+v", read.Stages, want)
	}
	if read.PrimarySetupName != "dev" {
		t.Errorf("PrimarySetupName = %q, want dev", read.PrimarySetupName)
	}
}
