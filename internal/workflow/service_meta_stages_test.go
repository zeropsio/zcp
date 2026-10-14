package workflow

import (
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
	m.Stages = []topology.Stage{
		{Hostname: "webstage", Setup: "web"},
		{Hostname: "apistage", Setup: "api"},
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
		want []topology.Stage
	}{
		{
			name: "an old pair meta reads as one stage with the same hostname and setup",
			json: `{"hostname":"appdev","mode":"standard","stageHostname":"appstage","primarySetupName":"dev","stageSetupName":"prod","bootstrapSession":"s","bootstrappedAt":"t"}`,
			want: []topology.Stage{{Hostname: "appstage", Setup: "prod"}},
		},
		{
			name: "an old pair meta without a recorded stage setup reads as one stage with no setup",
			json: `{"hostname":"appdev","mode":"standard","stageHostname":"appstage","bootstrapSession":"s","bootstrappedAt":"t"}`,
			want: []topology.Stage{{Hostname: "appstage"}},
		},
		{
			name: "an old single-service meta reads with no stage",
			json: `{"hostname":"app","mode":"simple","bootstrapSession":"s","bootstrappedAt":"t"}`,
			want: nil,
		},
		{
			name: "a meta that records stages reads them as written",
			json: `{"hostname":"appdev","mode":"standard","stages":[{"hostname":"webstage","setup":"web"},{"hostname":"apistage","setup":"api"}],"bootstrapSession":"s","bootstrappedAt":"t"}`,
			want: []topology.Stage{{Hostname: "webstage", Setup: "web"}, {Hostname: "apistage", Setup: "api"}},
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

func TestWriteServiceMeta_MigratedLegacyMeta_WritesStagesOnly(t *testing.T) {
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
	body := string(data)
	if strings.Contains(body, "stageHostname") || strings.Contains(body, "stageSetupName") {
		t.Errorf("rewritten meta still carries a legacy stage field:\n%s", body)
	}
	if !strings.Contains(body, `"stages"`) || !strings.Contains(body, `"hostname": "appstage"`) || !strings.Contains(body, `"setup": "prod"`) {
		t.Errorf("rewritten meta lost its stage:\n%s", body)
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
	want := []topology.Stage{{Hostname: "webstage", Setup: "web"}, {Hostname: "apistage", Setup: "api"}, {Hostname: "workerstage", Setup: "workerstage"}}
	if !slices.Equal(read.Stages, want) {
		t.Errorf("Stages = %+v, want %+v", read.Stages, want)
	}
	if read.PrimarySetupName != "dev" {
		t.Errorf("PrimarySetupName = %q, want dev", read.PrimarySetupName)
	}
}
