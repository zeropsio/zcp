package ops

import (
	"context"
	"errors"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestLookupService_NotFound(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s-app", Name: "appdev"},
	})

	_, err := LookupService(context.Background(), mock, "p1", "missing")
	if err == nil {
		t.Fatal("expected error for missing hostname")
	}
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *platform.PlatformError, got %T (%v)", err, err)
	}
	if pe.Code != platform.ErrServiceNotFound {
		t.Errorf("code = %q, want %q", pe.Code, platform.ErrServiceNotFound)
	}
	// Suggestion text must list the actual project hostnames — that's
	// the wording every caller relies on for parity with FindService.
	if pe.Suggestion == "" {
		t.Errorf("suggestion is empty; expected 'Available services: ...'")
	}
}

func TestLookupService_Found(t *testing.T) {
	t.Parallel()
	want := platform.ServiceStack{ID: "s-app", Name: "appdev"}
	mock := platform.NewMock().WithServices([]platform.ServiceStack{want})

	got, err := LookupService(context.Background(), mock, "p1", "appdev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.ID != want.ID {
		t.Errorf("got %+v, want service ID %q", got, want.ID)
	}
}

func TestListProjectServices_Passthrough(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s-app", Name: "appdev"},
		{ID: "s-db", Name: "db"},
	})

	services, err := ListProjectServices(context.Background(), mock, "p1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(services) != 2 {
		t.Errorf("expected 2 services, got %d", len(services))
	}
}

// TestFetchServiceUserEnvs_ExcludesSystemAndYamlBaked pins the 2026-08 service-env model (spec-zerops-env-lifecycle.md §1)
// (spec docs/spec-zerops-env-lifecycle.md §1/§6): the user-set layer is the
// slim /env USER set MINUS the keys the active app-version's yaml-baked
// USER mirror also carries — Type alone can no longer tell a user-set var
// apart from the yaml-baked mirror (both USER since 2026-08). SYSTEM
// intrinsics are excluded outright; an empty-typed legacy fixture is
// treated as user-set.
func TestFetchServiceUserEnvs_ExcludesSystemAndYamlBaked(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithService(&platform.ServiceStack{
			ID: "svc-api", Name: "api", Status: statusActive,
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
			ActiveAppVersion:     &platform.ActiveAppVersionDigest{ID: "av-api"},
		}).
		WithServiceEnv("svc-api", []platform.ServiceEnvVar{
			{Key: "hostname", Content: "api", Type: platform.ServiceEnvSystem},
			{Key: "FOO", Content: "fromyaml", Type: platform.ServiceEnvUser}, // yaml-baked mirror
			{Key: "API_KEY", Content: "secretvalue", Type: platform.ServiceEnvUser},
			{Key: "BAR", Content: "userset", Type: ""},
		}).
		WithAppVersionUserData("av-api", []platform.ServiceEnvVar{
			{Key: "FOO", Content: "fromyaml", Type: platform.ServiceEnvUser},
		})

	got, err := FetchServiceUserEnvs(context.Background(), mock, "svc-api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	keys := map[string]bool{}
	for _, e := range got {
		keys[e.Key] = true
	}
	if len(got) != 2 {
		t.Fatalf("got %d vars, want 2 (API_KEY, BAR): %+v", len(got), got)
	}
	if !keys["API_KEY"] || !keys["BAR"] {
		t.Errorf("expected API_KEY + BAR present, got %+v", got)
	}
	if keys["hostname"] {
		t.Error("SYSTEM intrinsic hostname must be excluded")
	}
	if keys["FOO"] {
		t.Error("yaml-baked mirror FOO must be excluded (present in the app-version userData)")
	}
}

// TestFetchServiceUserEnvs_NeverDeployed_NoSubtraction pins the lifecycle
// gate (spec §1): a runtime service with no active app version yet has no
// yaml-baked layer to subtract — AppVersionEnvVars returns nil, so every
// USER/empty-typed slim entry passes through untouched. SYSTEM stays
// excluded.
func TestFetchServiceUserEnvs_NeverDeployed_NoSubtraction(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithService(&platform.ServiceStack{
			ID: "svc-api", Name: "api", Status: statusActive,
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
			// ActiveAppVersion intentionally nil: never deployed.
		}).
		WithServiceEnv("svc-api", []platform.ServiceEnvVar{
			{Key: "hostname", Content: "api", Type: platform.ServiceEnvSystem},
			{Key: "API_KEY", Content: "secretvalue", Type: platform.ServiceEnvUser},
		})

	got, err := FetchServiceUserEnvs(context.Background(), mock, "svc-api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Key != "API_KEY" {
		t.Fatalf("got %+v, want exactly [API_KEY]", got)
	}
}

// TestFetchServiceUserEnvs_YamlFetchError_ReturnsError pins that a failed
// yaml-baked read on a live runtime propagates as a real error — never an
// empty slice with nil error, which would leak the yaml-baked mirror's
// keys into export/launch's envSecrets (GAP0-1 regression class).
func TestFetchServiceUserEnvs_YamlFetchError_ReturnsError(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithService(&platform.ServiceStack{
			ID: "svc-api", Name: "api", Status: statusActive,
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
			ActiveAppVersion:     &platform.ActiveAppVersionDigest{ID: "av-api"},
		}).
		WithServiceEnv("svc-api", []platform.ServiceEnvVar{
			{Key: "API_KEY", Content: "secretvalue", Type: platform.ServiceEnvUser},
		}).
		WithError("GetAppVersionUserData", platform.NewPlatformError(platform.ErrAPIError, "boom", ""))

	got, err := FetchServiceUserEnvs(context.Background(), mock, "svc-api")
	if err == nil {
		t.Fatalf("expected error, got nil (got %+v) — a failed yaml read must never surface as an empty slice", got)
	}
	if got != nil {
		t.Errorf("expected nil result alongside the error, got %+v", got)
	}
}

// TestAppVersionEnvVars_NewModel_YamlLayerPresent pins that the classifier
// fix propagates through ServiceHigherLayers: a live runtime's yaml-baked
// layer reads Present (not silently empty) and carries the USER-typed
// run.envVariables key, with SYSTEM intrinsics excluded. Before the S2
// classifier fix this layer silently vanished (the old classifier didn't
// recognize the live "USER" wire value), which is exactly the regression
// class this test guards.
func TestAppVersionEnvVars_NewModel_YamlLayerPresent(t *testing.T) {
	t.Parallel()
	svc := platform.ServiceStack{
		ID: "svc-api", Name: "api", Status: statusActive,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
		ActiveAppVersion:     &platform.ActiveAppVersionDigest{ID: "av-api"},
	}
	mock := platform.NewMock().
		WithServiceEnv("svc-api", []platform.ServiceEnvVar{{Key: "PORT", Content: "3000", Type: platform.ServiceEnvSystem}}).
		WithAppVersionUserData("av-api", []platform.ServiceEnvVar{
			{Key: "FOO", Content: "fromyaml", Type: platform.ServiceEnvUser},
			{Key: "hostname", Content: "api", Type: platform.ServiceEnvSystem},
		})

	higher, err := ServiceHigherLayers(context.Background(), mock, svc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if higher.YamlBakedState.Availability != LayerPresent {
		t.Fatalf("YamlBakedState.Availability = %v, want LayerPresent", higher.YamlBakedState.Availability)
	}
	if len(higher.YamlBaked) != 1 || higher.YamlBaked[0].Key != "FOO" {
		t.Fatalf("YamlBaked = %+v, want exactly one entry (FOO)", higher.YamlBaked)
	}
}
