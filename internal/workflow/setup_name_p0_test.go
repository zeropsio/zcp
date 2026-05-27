package workflow

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
)

// TestServiceMeta_PreP0Format_LoadsCleanly pins the backward-compat
// invariant: ServiceMeta JSON written before setup-name fields existed
// unmarshals cleanly with PrimarySetupName / StageSetupName zero-valued.
// SetupNameFor on the result returns "" — cache-miss signal. No silent
// reconstruction; first setup-sensitive op runs cascade.
func TestServiceMeta_PreP0Format_LoadsCleanly(t *testing.T) {
	t.Parallel()

	legacy := []byte(`{
		"hostname": "appdev",
		"mode": "dev",
		"stageHostname": "appstage",
		"closeDeployMode": "auto",
		"closeDeployModeConfirmed": true,
		"gitPushState": "configured",
		"remoteUrl": "git@github.com:example/demo.git",
		"buildIntegration": "actions",
		"bootstrapSession": "sess-legacy",
		"bootstrappedAt": "2026-04-01T12:00:00Z",
		"firstDeployedAt": "2026-04-02T10:00:00Z"
	}`)

	var meta ServiceMeta
	if err := json.Unmarshal(legacy, &meta); err != nil {
		t.Fatalf("legacy JSON failed to unmarshal: %v", err)
	}
	if meta.Hostname != "appdev" {
		t.Errorf("Hostname: want appdev, got %q", meta.Hostname)
	}
	if meta.PrimarySetupName != "" {
		t.Errorf("PrimarySetupName: want empty (cache miss), got %q", meta.PrimarySetupName)
	}
	if meta.StageSetupName != "" {
		t.Errorf("StageSetupName: want empty (cache miss), got %q", meta.StageSetupName)
	}
	if got := meta.SetupNameFor("appdev"); got != "" {
		t.Errorf("SetupNameFor on pre-P0 meta: want empty cache miss, got %q", got)
	}
	if got := meta.SetupNameFor("appstage"); got != "" {
		t.Errorf("SetupNameFor on pre-P0 stage half: want empty cache miss, got %q", got)
	}
}

// TestSetupNameFor_PairKeyed pins the pair-keyed lookup contract:
// targetHostname == StageHostname returns StageSetupName; targetHostname
// == Hostname returns PrimarySetupName; any other hostname returns ""
// (caller must load that hostname's own meta).
func TestSetupNameFor_PairKeyed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		meta        *ServiceMeta
		hostname    string
		wantSetup   string
		description string
	}{
		{"nil meta returns empty", nil, "appdev", "", "nil receiver"},
		{
			"primary hostname returns primary setup",
			&ServiceMeta{Hostname: "appdev", PrimarySetupName: "dev"},
			"appdev", "dev",
			"hostname matches primary; returns PrimarySetupName",
		},
		{
			"stage hostname returns stage setup",
			&ServiceMeta{Hostname: "appdev", StageHostname: "appstage", PrimarySetupName: "dev", StageSetupName: "prod"},
			"appstage", "prod",
			"hostname matches stage half; returns StageSetupName",
		},
		{
			"primary half of pair returns primary",
			&ServiceMeta{Hostname: "appdev", StageHostname: "appstage", PrimarySetupName: "dev", StageSetupName: "prod"},
			"appdev", "dev",
			"hostname is primary in pair; not stage",
		},
		{
			"out-of-scope hostname returns empty",
			&ServiceMeta{Hostname: "appdev", StageHostname: "appstage", PrimarySetupName: "dev", StageSetupName: "prod"},
			"unrelated", "",
			"caller must load that hostname's own meta",
		},
		{
			"empty primary cache misses on primary",
			&ServiceMeta{Hostname: "appdev"},
			"appdev", "",
			"unset PrimarySetupName returns empty — signals cache miss",
		},
		{
			"empty stage cache misses on stage half",
			&ServiceMeta{Hostname: "appdev", StageHostname: "appstage", PrimarySetupName: "dev"},
			"appstage", "",
			"unset StageSetupName returns empty even when primary is set",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.meta.SetupNameFor(tc.hostname)
			if got != tc.wantSetup {
				t.Errorf("%s: SetupNameFor(%q) = %q, want %q", tc.description, tc.hostname, got, tc.wantSetup)
			}
		})
	}
}

// TestComputeEnvelope_PopulatesSetupNameFromMeta verifies P0 item 3:
// ServiceMeta.PrimarySetupName + StageSetupName project into
// ServiceSnapshot at envelope build time. Dev half carries primary +
// stage paired-half value; stage half carries stage as own SetupName.
func TestComputeEnvelope_PopulatesSetupNameFromMeta(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	meta := &ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrappedAt:   "2026-05-27T10:00:00Z",
		BootstrapSession: "sess-1",
		PrimarySetupName: "dev",
		StageSetupName:   "prod",
	}
	if err := WriteServiceMeta(dir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	svcDev := platform.ServiceStack{
		ID: "s1", Name: "appdev", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  "nodejs@22",
			ServiceStackTypeCategoryName: "USER",
		},
	}
	svcStage := platform.ServiceStack{
		ID: "s2", Name: "appstage", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  "nodejs@22",
			ServiceStackTypeCategoryName: "USER",
		},
	}

	mock := platform.NewMock().WithServices([]platform.ServiceStack{svcDev, svcStage})
	env, err := ComputeEnvelope(context.Background(), mock, dir, "proj-1", runtime.Info{}, fixedTime)
	if err != nil {
		t.Fatalf("ComputeEnvelope: %v", err)
	}

	var dev, stage *ServiceSnapshot
	for i := range env.Services {
		s := &env.Services[i]
		switch s.Hostname {
		case "appdev":
			dev = s
		case "appstage":
			stage = s
		}
	}
	if dev == nil || stage == nil {
		t.Fatalf("expected both halves; got services=%v", env.Services)
	}
	if dev.SetupName != "dev" {
		t.Errorf("dev half SetupName: want %q, got %q", "dev", dev.SetupName)
	}
	if dev.StageSetupName != "prod" {
		t.Errorf("dev half StageSetupName: want %q, got %q", "prod", dev.StageSetupName)
	}
	if stage.SetupName != "prod" {
		t.Errorf("stage half SetupName: want %q (via SetupNameFor projection), got %q", "prod", stage.SetupName)
	}
}
