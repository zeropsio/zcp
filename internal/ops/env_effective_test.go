package ops

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func runtimeSvc(id, appVersionID string) platform.ServiceStack {
	s := platform.ServiceStack{
		ID:                   id,
		Name:                 "app",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
	}
	if appVersionID != "" {
		s.ActiveAppVersion = &platform.ActiveAppVersionDigest{ID: appVersionID}
	}
	return s
}

// TestAppVersionEnvVars_LifecycleStates pins the lifecycle gate (spec §1):
// managed + never-deployed yield nil (no app-version yaml-baked); only a
// live runtime service returns yaml-baked vars.
func TestAppVersionEnvVars_LifecycleStates(t *testing.T) {
	t.Parallel()
	yaml := []platform.ServiceEnvVar{{Key: "FOO", Content: "fromyaml"}, {Key: "DBREF", Content: "${db_hostname}"}}

	tests := []struct {
		name string
		svc  platform.ServiceStack
		want int // expected number of yaml-baked vars
	}{
		{
			name: "managed dep — no app-version even if ID present",
			svc: platform.ServiceStack{
				ID:                   "db1",
				Name:                 "db",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"},
				ActiveAppVersion:     &platform.ActiveAppVersionDigest{ID: "av-managed"},
			},
			want: 0,
		},
		{
			name: "runtime never-deployed — no active app version",
			svc:  runtimeSvc("app1", ""),
			want: 0,
		},
		{
			name: "runtime live — yaml-baked returned",
			svc:  runtimeSvc("app2", "av-live"),
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().
				WithAppVersionUserData("av-live", yaml).
				WithAppVersionUserData("av-managed", yaml) // seeded but must NOT be read for managed
			got, err := AppVersionEnvVars(context.Background(), mock, tt.svc)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tt.want {
				t.Errorf("got %d yaml-baked vars, want %d (%v)", len(got), tt.want, got)
			}
		})
	}
}

// TestEffectiveServiceEnv_LayersAndKeys pins the layered assembly +
// de-duplicated Keys() universe across project / service / yaml-baked.
func TestEffectiveServiceEnv_LayersAndKeys(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProjectEnv([]platform.ProjectEnvVar{{Key: "PROJ_VAR", Content: "p"}, {Key: "SHARED", Content: "fromproject"}}).
		WithServiceEnv("app2", []platform.ServiceEnvVar{{Key: "hostname", Content: "app"}, {Key: "USER_SET", Content: "u"}}).
		WithAppVersionUserData("av-live", []platform.ServiceEnvVar{{Key: "FOO", Content: "fromyaml"}, {Key: "SHARED", Content: "fromyaml"}})

	eff, err := EffectiveServiceEnv(context.Background(), mock, "proj", runtimeSvc("app2", "av-live"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(eff.Project) != 2 || len(eff.Service) != 2 || len(eff.YamlBaked) != 2 {
		t.Fatalf("layers: project=%d service=%d yamlBaked=%d (want 2/2/2)", len(eff.Project), len(eff.Service), len(eff.YamlBaked))
	}
	// Keys() de-dupes SHARED (in project + yaml-baked) → 5 unique keys.
	keys := eff.Keys()
	want := map[string]bool{"PROJ_VAR": true, "SHARED": true, "hostname": true, "USER_SET": true, "FOO": true}
	if len(keys) != len(want) {
		t.Errorf("Keys() = %v (%d), want %d unique", keys, len(keys), len(want))
	}
	for _, k := range keys {
		if !want[k] {
			t.Errorf("unexpected key %q in Keys()", k)
		}
	}
}

// TestServiceHigherLayers pins the above-project layer assembly used by
// project-set shadow detection: slim service userData (always) + yaml-baked
// (live runtime only; spec §1 lifecycle gate). It is the extracted helper
// EffectiveServiceEnv now composes on top of the project layer.
func TestServiceHigherLayers(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServiceEnv("app2", []platform.ServiceEnvVar{{Key: "hostname", Content: "app"}, {Key: "USER_SET", Content: "u"}}).
		WithServiceEnv("db1", []platform.ServiceEnvVar{{Key: "db_hostname", Content: "db"}}).
		WithAppVersionUserData("av-live", []platform.ServiceEnvVar{{Key: "FOO", Content: "fromyaml"}})

	t.Run("runtime live — service + yaml-baked", func(t *testing.T) {
		t.Parallel()
		svc, yaml, err := ServiceHigherLayers(context.Background(), mock, runtimeSvc("app2", "av-live"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(svc) != 2 || len(yaml) != 1 {
			t.Fatalf("service=%d yamlBaked=%d (want 2/1)", len(svc), len(yaml))
		}
		if svc[0].Layer != EnvLayerService || yaml[0].Layer != EnvLayerYamlBaked {
			t.Errorf("layer labels wrong: svc=%v yaml=%v", svc[0].Layer, yaml[0].Layer)
		}
	})

	t.Run("managed dep — service only, no yaml-baked", func(t *testing.T) {
		t.Parallel()
		managed := platform.ServiceStack{
			ID:                   "db1",
			Name:                 "db",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"},
			ActiveAppVersion:     &platform.ActiveAppVersionDigest{ID: "av-live"}, // seeded but must NOT be read for managed
		}
		svc, yaml, err := ServiceHigherLayers(context.Background(), mock, managed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(svc) != 1 || len(yaml) != 0 {
			t.Fatalf("service=%d yamlBaked=%d (want 1/0)", len(svc), len(yaml))
		}
	})

	t.Run("never-deployed runtime — service only", func(t *testing.T) {
		t.Parallel()
		svc, yaml, err := ServiceHigherLayers(context.Background(), mock, runtimeSvc("app2", ""))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(svc) != 2 || len(yaml) != 0 {
			t.Fatalf("service=%d yamlBaked=%d (want 2/0)", len(svc), len(yaml))
		}
	})
}
