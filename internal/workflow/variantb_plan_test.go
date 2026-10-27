package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// planB is F2's pair under variant B: webdev (Bun) edits, apidev (Go) runs
// the api part, both on the local-storage service vol.
func planB(mountPath string, edit func(*RuntimeTarget)) []BootstrapTarget {
	rt := RuntimeTarget{
		DevHostname:   "webdev",
		Type:          "bun@1.3",
		BootstrapMode: topology.PlanModeStandard,
		Stages:        []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}},
		CodeVolume: &topology.CodeVolume{
			Hostname:    "vol",
			MountPath:   mountPath,
			DevServices: []topology.DevServiceRef{{Hostname: "apidev", SetupName: "dev-api"}},
		},
	}
	if edit != nil {
		edit(&rt)
	}
	return []BootstrapTarget{{Runtime: rt}}
}

// TestValidateBootstrapTargets_AdoptedCodeVolume_NamesEveryServiceOnce: the
// code volume adopt records again for a variant-B pair (docs/spec-mate.md
// §2.8a.4a) names each of its services once — no further dev service or
// volume named like another service of the plan — and every one of them
// live.
func TestValidateBootstrapTargets_AdoptedCodeVolume_NamesEveryServiceOnce(t *testing.T) {
	t.Parallel()
	live := []platform.ServiceStack{{Name: "webdev"}, {Name: "apidev"}, {Name: "webstage"}, {Name: "apistage"}, {Name: "vol"}}
	adopted := func(edit func(*RuntimeTarget)) []BootstrapTarget {
		return planB("/var/www/ws", func(rt *RuntimeTarget) {
			rt.IsExisting = true
			if edit != nil {
				edit(rt)
			}
		})
	}
	tests := []struct {
		name    string
		targets []BootstrapTarget
		live    []platform.ServiceStack
		wantErr string
	}{
		{name: "the adopted pair validates", targets: adopted(nil), live: live},
		{
			name: "a dev service named like the pair's stage is refused",
			targets: adopted(func(rt *RuntimeTarget) {
				rt.CodeVolume.DevServices = []topology.DevServiceRef{{Hostname: "apistage"}}
			}),
			live:    live,
			wantErr: `"apistage" is planned twice`,
		},
		{
			name:    "the volume named like the dev half is refused",
			targets: adopted(func(rt *RuntimeTarget) { rt.CodeVolume.Hostname = "webdev" }),
			live:    live,
			wantErr: `"webdev" is planned twice`,
		},
		{
			name:    "a further dev service the project does not hold is refused",
			targets: adopted(nil),
			live:    []platform.ServiceStack{{Name: "webdev"}, {Name: "webstage"}, {Name: "apistage"}, {Name: "vol"}},
			wantErr: "apidev",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ValidateBootstrapTargets(tt.targets, nil, tt.live)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("err = %v, want a valid plan", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to say %q", err, tt.wantErr)
			}
		})
	}
}

// TestBootstrapCompletePlan_PlanCarryingCodeVolume_RefusedNamingPools
// (docs/spec-mate.md §2.8a.4a): zcp sets up no new code volume, so a plan an
// agent submits never carries one — on a new pair or one it adopts. The
// refusal names the field, what replaces it and how an existing variant-B
// pair moves, and nothing is planned.
func TestBootstrapCompletePlan_PlanCarryingCodeVolume_RefusedNamingPools(t *testing.T) {
	t.Parallel()
	for name, existing := range map[string]bool{"a new pair": false, "an adopted pair": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			eng := NewEngine(t.TempDir(), EnvContainer, nil)
			if _, err := eng.BootstrapStart("proj-1", "web and api in one repository"); err != nil {
				t.Fatalf("BootstrapStart: %v", err)
			}
			targets := planB("/var/www/ws", func(rt *RuntimeTarget) { rt.IsExisting = existing })
			var live []platform.ServiceStack
			if existing {
				live = []platform.ServiceStack{{Name: "webdev"}, {Name: "apidev"}, {Name: "webstage"}, {Name: "apistage"}, {Name: "vol"}}
			}
			_, err := eng.BootstrapCompletePlan(targets, nil, live)
			if err == nil {
				t.Fatal("a plan carrying codeVolume was accepted")
			}
			for _, want := range []string{"codeVolume", "dev pool", `workflow="pool-migrate"`, "with a stage per part", "a dev pool of its own"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("err = %v, want it to say %q", err, want)
				}
			}
			// Every part ends on a dev pool of its own: one dev service
			// holding the whole repository is no longer the plan it names.
			if strings.Contains(err.Error(), "one dev service holds the whole repository") {
				t.Errorf("err = %v, still names one dev service for the whole repository", err)
			}
			state, err := eng.GetState()
			if err != nil {
				t.Fatalf("GetState: %v", err)
			}
			if state.Bootstrap.Plan != nil {
				t.Errorf("a refused plan was stored: %+v", state.Bootstrap.Plan)
			}
		})
	}
}

func TestValidateBootstrapTargets_PlanWithoutCodeVolume_ValidatesAsToday(t *testing.T) {
	t.Parallel()
	targets := planB("", func(rt *RuntimeTarget) { rt.CodeVolume = nil })
	if _, err := ValidateBootstrapTargets(targets, nil, nil); err != nil {
		t.Fatalf("variant A plan: err = %v", err)
	}
}

func TestAdoptPair_VariantB_RecordsTheCodeVolumeAndItsDevServicesResolveToThePair(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	target := planB("/var/www/ws", func(rt *RuntimeTarget) { rt.PrimarySetupName = "dev" })[0]
	if err := AdoptPair(stateDir, target, "2026-10-15"); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	meta, err := FindServiceMeta(stateDir, "apidev")
	if err != nil || meta == nil || meta.Hostname != "webdev" {
		t.Fatalf("apidev resolves to %+v (err %v), want the webdev pair", meta, err)
	}
	if got := meta.RoleFor("apidev"); got != topology.DeployRoleDev {
		t.Errorf("RoleFor(apidev) = %q, want dev", got)
	}
	if got := meta.SetupNameFor("apidev"); got != "dev-api" {
		t.Errorf("SetupNameFor(apidev) = %q, want dev-api", got)
	}
	for host, want := range map[string]string{"webdev": "/var/www/ws", "apidev": "/var/www/ws", "webstage": "", "vol": ""} {
		if got := meta.WorkingDirFor(host); got != want {
			t.Errorf("WorkingDirFor(%s) = %q, want %q", host, got, want)
		}
	}
	if idx := ManagedRuntimeIndex([]*ServiceMeta{meta}); idx["apidev"] != meta || idx["vol"] != nil {
		t.Errorf("index = %v, want apidev in the pair and the volume no runtime", idx)
	}
}

func TestAdoptPair_VariantB_DevServiceAlreadyItsOwnService_Refused(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := WriteServiceMeta(stateDir, NewServiceMeta("apidev", topology.PlanModeDev)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	err := AdoptPair(stateDir, planB("/var/www/ws", nil)[0], "2026-10-15")
	if err == nil || !strings.Contains(err.Error(), "apidev") {
		t.Fatalf("err = %v, want apidev named", err)
	}
}

// TestAdoptPair_AServiceAnotherPairAlreadyNames_Refused (docs/spec-workflows.md
// §8 E8): one meta per pair. A target whose dev half or stage another pair
// already names — a further dev service of a variant-B pair, or that pair's
// stage — is refused, and nothing is written under its hostname.
func TestAdoptPair_AServiceAnotherPairAlreadyNames_Refused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		target BootstrapTarget
		named  string
	}{
		{
			name: "a further dev service of a variant-B pair adopted as a pair of its own",
			target: BootstrapTarget{Runtime: RuntimeTarget{DevHostname: "apidev", Type: "go@1", BootstrapMode: topology.PlanModeStandard,
				Stages: []topology.StageRef{{Hostname: "apiprod", SetupName: "api"}}}},
			named: "apidev",
		},
		{
			name: "a stage of a variant-B pair adopted under another dev half",
			target: BootstrapTarget{Runtime: RuntimeTarget{DevHostname: "workerdev", Type: "go@1", BootstrapMode: topology.PlanModeStandard,
				Stages: []topology.StageRef{{Hostname: "apistage", SetupName: "api"}}}},
			named: "apistage",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			if err := AdoptPair(stateDir, planB("/var/www/ws", nil)[0], "2026-10-15"); err != nil {
				t.Fatalf("seed the variant-B pair: %v", err)
			}
			err := AdoptPair(stateDir, tt.target, "2026-10-24")
			if err == nil || !strings.Contains(err.Error(), tt.named) || !strings.Contains(err.Error(), "webdev") {
				t.Fatalf("err = %v, want %s named as webdev's", err, tt.named)
			}
			if own, _ := ReadServiceMeta(stateDir, tt.target.Runtime.DevHostname); own != nil {
				t.Errorf("a second meta was written: %+v", own)
			}
		})
	}
}

func TestServiceMeta_PairWithoutCodeVolume_DeploysFromTheDefaultWorkingDir(t *testing.T) {
	t.Parallel()
	meta := &ServiceMeta{Hostname: "appdev", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "appstage"}}}
	for _, host := range []string{"appdev", "appstage"} {
		if got := meta.WorkingDirFor(host); got != "" {
			t.Errorf("WorkingDirFor(%s) = %q, want the default", host, got)
		}
	}
	if got := meta.Hostnames(); len(got) != 2 {
		t.Errorf("Hostnames() = %v, want the pair's two", got)
	}
}
