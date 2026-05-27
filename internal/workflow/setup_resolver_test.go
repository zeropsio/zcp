package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// writeMetaForResolver seeds a minimal ServiceMeta for cascade tests.
// Hostname is hardcoded "appdev" — every existing cascade test uses
// that hostname; if a future test needs a different name, promote the
// helper to take the hostname back.
func writeMetaForResolver(t *testing.T, dir, stageHost string) {
	t.Helper()
	meta := &ServiceMeta{
		Hostname:         "appdev",
		StageHostname:    stageHost,
		BootstrappedAt:   "2026-05-27T10:00:00Z",
		BootstrapSession: "sess-1",
	}
	if err := WriteServiceMeta(dir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
}

// Step 1 — local cache hit.
// Cache populated; client is nil to prove no platform call needed.
func TestResolveCanonicalSetup_Step1_CacheHit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	meta := &ServiceMeta{
		Hostname:         "appdev",
		PrimarySetupName: "dev",
		BootstrappedAt:   "2026-05-27T10:00:00Z",
		BootstrapSession: "s1",
	}
	if err := WriteServiceMeta(dir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	got, err := ResolveCanonicalSetup(context.Background(), nil, ResolveCanonicalSetupInput{
		StateDir: dir, TargetHostname: "appdev",
	})
	if err != nil {
		t.Fatalf("ResolveCanonicalSetup: %v", err)
	}
	if got != "dev" {
		t.Errorf("want dev, got %q", got)
	}
}

// Step 2 — GH integration hit + cache write-back.
func TestResolveCanonicalSetup_Step2_GHIntegration_WritesBackCache(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMetaForResolver(t, dir, "")

	mock := platform.NewMock().WithIntegrationStatus("svc-1", platform.IntegrationStatus{
		State:           platform.IntegrationConfigured,
		Provider:        platform.IntegrationProviderGitHub,
		ZeropsYamlSetup: "appdev",
	})

	got, err := ResolveCanonicalSetup(context.Background(), mock, ResolveCanonicalSetupInput{
		StateDir: dir, ServiceID: "svc-1", TargetHostname: "appdev",
	})
	if err != nil {
		t.Fatalf("ResolveCanonicalSetup: %v", err)
	}
	if got != "appdev" {
		t.Errorf("want appdev, got %q", got)
	}

	// Cache write-back verification.
	reloaded, _ := ReadServiceMeta(dir, "appdev")
	if reloaded.PrimarySetupName != "appdev" {
		t.Errorf("cache write-back: want appdev, got %q", reloaded.PrimarySetupName)
	}
}

// Step 3 — ActiveAppVersion.GithubIntegration.ZeropsYamlSetup hit.
func TestResolveCanonicalSetup_Step3_ActiveAppVersionGH_WritesBackCache(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMetaForResolver(t, dir, "")

	mock := platform.NewMock().WithService(&platform.ServiceStack{
		ID:   "svc-1",
		Name: "appdev",
		ActiveAppVersion: &platform.ActiveAppVersionDigest{
			ID:                     "ver-1",
			GithubIntegrationSetup: "appdev",
		},
	})
	// Step 2 unseeded → returns NotConfigured; cascade falls through to step 3.

	got, err := ResolveCanonicalSetup(context.Background(), mock, ResolveCanonicalSetupInput{
		StateDir: dir, ServiceID: "svc-1", TargetHostname: "appdev",
	})
	if err != nil {
		t.Fatalf("ResolveCanonicalSetup: %v", err)
	}
	if got != "appdev" {
		t.Errorf("want appdev, got %q", got)
	}

	reloaded, _ := ReadServiceMeta(dir, "appdev")
	if reloaded.PrimarySetupName != "appdev" {
		t.Errorf("cache write-back: want appdev, got %q", reloaded.PrimarySetupName)
	}
}

// Step 4 — GetAppVersionAppCode → fetch archive → extract zerops.yaml → parse.
// Uses ArchiveFetcher stub to bypass HTTP+zip in the test.
func TestResolveCanonicalSetup_Step4_ArchiveFetch_WritesBackCache(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMetaForResolver(t, dir, "")

	mock := platform.NewMock().
		WithService(&platform.ServiceStack{
			ID:   "svc-1",
			Name: "appdev",
			ActiveAppVersion: &platform.ActiveAppVersionDigest{
				ID: "ver-1",
				// No GithubIntegrationSetup → cascade falls past step 3.
			},
		}).
		WithAppVersionAppCode("ver-1", "https://stub/archive.zip")

	stubBody := "zerops:\n  - setup: dev\n  - setup: prod\n"
	fetcher := func(_ context.Context, url string) (string, error) {
		if url != "https://stub/archive.zip" {
			t.Errorf("fetcher got unexpected URL: %s", url)
		}
		return stubBody, nil
	}

	got, err := ResolveCanonicalSetup(context.Background(), mock, ResolveCanonicalSetupInput{
		StateDir: dir, ServiceID: "svc-1", TargetHostname: "appdev",
		Mode: topology.ModeStandard, ArchiveFetcher: fetcher,
	})
	if err != nil {
		t.Fatalf("ResolveCanonicalSetup: %v", err)
	}
	if got != "dev" {
		t.Errorf("want dev (well-known suffix match for appdev/Standard), got %q", got)
	}

	reloaded, _ := ReadServiceMeta(dir, "appdev")
	if reloaded.PrimarySetupName != "dev" {
		t.Errorf("cache write-back: want dev, got %q", reloaded.PrimarySetupName)
	}
}

// Step 5 — LocalYAMLBody fallback.
func TestResolveCanonicalSetup_Step5_LocalYAML_SingleBlockAutoPick(t *testing.T) {
	t.Parallel()
	body := "zerops:\n  - setup: app\n    build: {base: nodejs@22}\n"
	got, err := ResolveCanonicalSetup(context.Background(), platform.NewMock(), ResolveCanonicalSetupInput{
		TargetHostname: "myservice", Mode: topology.ModeStandard, LocalYAMLBody: body,
	})
	if err != nil {
		t.Fatalf("ResolveCanonicalSetup: %v", err)
	}
	if got != "app" {
		t.Errorf("want app (single-block auto-pick), got %q", got)
	}
}

func TestResolveCanonicalSetup_Step5_LocalYAML_HostnameMatch(t *testing.T) {
	t.Parallel()
	body := "zerops:\n  - setup: appdev\n  - setup: prod\n"
	got, err := ResolveCanonicalSetup(context.Background(), platform.NewMock(), ResolveCanonicalSetupInput{
		TargetHostname: "appdev", Mode: topology.ModeStandard, LocalYAMLBody: body,
	})
	if err != nil {
		t.Fatalf("ResolveCanonicalSetup: %v", err)
	}
	if got != "appdev" {
		t.Errorf("want appdev (hostname exact match), got %q", got)
	}
}

// Step 6 — total miss returns *ErrRequiresSetupInput.
func TestResolveCanonicalSetup_Step6_TotalMiss_RequiresSetupInput(t *testing.T) {
	t.Parallel()
	_, err := ResolveCanonicalSetup(context.Background(), platform.NewMock(), ResolveCanonicalSetupInput{
		ServiceID: "svc-1", TargetHostname: "appdev", Mode: topology.ModeStandard,
		// No LocalYAMLBody; mock has no integration/appVersion seeded → orphan.
	})
	if err == nil {
		t.Fatal("expected ErrRequiresSetupInput, got nil")
	}
	var blocker *ErrRequiresSetupInput
	if !errors.As(err, &blocker) {
		t.Fatalf("error type: want *ErrRequiresSetupInput, got %T (%v)", err, err)
	}
	if blocker.TargetHostname != "appdev" {
		t.Errorf("blocker hostname: want appdev, got %q", blocker.TargetHostname)
	}
}

// Step 6 — multi-setup ambiguity returns blocker with AvailableSetups.
func TestResolveCanonicalSetup_Step6_MultiSetupAmbiguity_CarriesAvailable(t *testing.T) {
	t.Parallel()
	body := "zerops:\n  - setup: web\n  - setup: api\n"
	_, err := ResolveCanonicalSetup(context.Background(), platform.NewMock(), ResolveCanonicalSetupInput{
		TargetHostname: "frontend", Mode: topology.ModeStandard, LocalYAMLBody: body,
	})
	var blocker *ErrRequiresSetupInput
	if !errors.As(err, &blocker) {
		t.Fatalf("error type: want *ErrRequiresSetupInput, got %T", err)
	}
	if len(blocker.AvailableSetups) != 2 {
		t.Errorf("AvailableSetups: want [web api], got %v", blocker.AvailableSetups)
	}
}

// Stage-half target writes to StageSetupName (not PrimarySetupName).
func TestResolveCanonicalSetup_StageHalfTarget_WritesStageSetupName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMetaForResolver(t, dir, "appstage")

	mock := platform.NewMock().WithIntegrationStatus("svc-stage", platform.IntegrationStatus{
		State:           platform.IntegrationConfigured,
		Provider:        platform.IntegrationProviderGitHub,
		ZeropsYamlSetup: "prod",
	})

	got, err := ResolveCanonicalSetup(context.Background(), mock, ResolveCanonicalSetupInput{
		StateDir: dir, ServiceID: "svc-stage", TargetHostname: "appstage",
	})
	if err != nil {
		t.Fatalf("ResolveCanonicalSetup: %v", err)
	}
	if got != "prod" {
		t.Errorf("want prod, got %q", got)
	}

	reloaded, _ := FindServiceMeta(dir, "appstage")
	if reloaded.StageSetupName != "prod" {
		t.Errorf("StageSetupName: want prod, got %q", reloaded.StageSetupName)
	}
	if reloaded.PrimarySetupName != "" {
		t.Errorf("PrimarySetupName must stay empty for stage-half resolve, got %q", reloaded.PrimarySetupName)
	}
}

func TestResolveCanonicalSetup_TargetHostnameRequired(t *testing.T) {
	t.Parallel()
	_, err := ResolveCanonicalSetup(context.Background(), nil, ResolveCanonicalSetupInput{})
	if err == nil || err.Error() == "" {
		t.Fatal("expected non-nil error for empty TargetHostname")
	}
}
