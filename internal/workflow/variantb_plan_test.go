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

func TestValidateBootstrapTargets_PlanB_MountsTheCodeVolumeUnderVarWwwNeverAtIt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		targets []BootstrapTarget
		live    []platform.ServiceStack
		wantErr string
	}{
		{name: "a folder under /var/www validates", targets: planB("/var/www/ws", nil)},
		{name: "a deeper folder under /var/www validates", targets: planB("/var/www/code/repo", nil)},
		{name: "/var/www itself is refused", targets: planB("/var/www", nil), wantErr: "under /var/www"},
		{name: "/var/www/ is /var/www itself", targets: planB("/var/www/", nil), wantErr: "under /var/www"},
		{name: "a path outside /var/www is refused", targets: planB("/mnt/vol", nil), wantErr: "under /var/www"},
		{name: "a path climbing out of /var/www is refused", targets: planB("/var/www/../vol", nil), wantErr: "under /var/www"},
		{name: "a relative path is refused", targets: planB("ws", nil), wantErr: "under /var/www"},
		{name: "a missing mount path is refused", targets: planB("", nil), wantErr: "under /var/www"},
		{
			name:    "a code volume without a further dev service is no variant B",
			targets: planB("/var/www/ws", func(rt *RuntimeTarget) { rt.CodeVolume.DevServices = nil }),
			wantErr: "further dev service",
		},
		{
			name:    "a simple runtime has no dev service to share code with",
			targets: planB("/var/www/ws", func(rt *RuntimeTarget) { rt.BootstrapMode = topology.PlanModeSimple; rt.Stages = nil }),
			wantErr: "variant B",
		},
		{
			name: "a dev service named like the pair's stage is refused",
			targets: planB("/var/www/ws", func(rt *RuntimeTarget) {
				rt.CodeVolume.DevServices = []topology.DevServiceRef{{Hostname: "apistage"}}
			}),
			wantErr: `"apistage" is planned twice`,
		},
		{
			name:    "the volume named like the dev half is refused",
			targets: planB("/var/www/ws", func(rt *RuntimeTarget) { rt.CodeVolume.Hostname = "webdev" }),
			wantErr: `"webdev" is planned twice`,
		},
		{
			name:    "an invalid volume hostname is refused",
			targets: planB("/var/www/ws", func(rt *RuntimeTarget) { rt.CodeVolume.Hostname = "code_vol" }),
			wantErr: `"code_vol"`,
		},
		{
			name:    "a new pair's dev service colliding with a live service is refused",
			targets: planB("/var/www/ws", nil),
			live:    []platform.ServiceStack{{Name: "apidev"}},
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
