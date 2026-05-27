// Tests for: adopt_local.go — Gate A cascade integration in
// LocalAutoAdopt. Lives in the same package (workflow) because the
// cascade's writeBackCache mutates the meta on disk, which we verify
// via ReadServiceMeta.
package workflow

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// Gate A hit on LocalAutoAdopt's single-runtime path: the cascade reads
// the seeded GH integration setup name and writes back to
// StageSetupName on the freshly-adopted meta.
func TestLocalAutoAdopt_Case1_CascadeHit_PopulatesStageSetupName(t *testing.T) {
	dir := t.TempDir()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "myproject"}).
		WithServices([]platform.ServiceStack{
			{
				ID: "rt-1", Name: "apistage", Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
			},
		}).
		WithIntegrationStatus("rt-1", platform.IntegrationStatus{
			State:           platform.IntegrationConfigured,
			Provider:        platform.IntegrationProviderGitHub,
			ZeropsYamlSetup: "prod",
		})

	if _, err := LocalAutoAdopt(context.Background(), mock, "p1", dir); err != nil {
		t.Fatalf("LocalAutoAdopt: %v", err)
	}

	meta, _ := ReadServiceMeta(dir, "myproject")
	if meta == nil {
		t.Fatal("expected meta after adoption")
	}
	if got := meta.SetupNameFor("apistage"); got != "prod" {
		t.Errorf("SetupNameFor(apistage): got %q, want prod (primary=%q stage=%q)",
			got, meta.PrimarySetupName, meta.StageSetupName)
	}
	if meta.StageSetupName != "prod" {
		t.Errorf("StageSetupName: got %q, want prod", meta.StageSetupName)
	}
}

// Cascade miss at server start has no agent to receive a blocker — the
// meta lands with empty setup names, and the next agent-driven
// setup-sensitive call reruns cascade. The KEY invariant: adoption
// must NOT fail / return an error on cascade miss.
func TestLocalAutoAdopt_Case1_CascadeMiss_SilentlySwallows(t *testing.T) {
	dir := t.TempDir()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "myproject"}).
		WithServices([]platform.ServiceStack{
			{
				ID: "rt-1", Name: "apistage", Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
			},
		})
	// No integration status, no app version → cascade exhausts at
	// steps 2-4 and returns ErrRequiresSetupInput.

	result, err := LocalAutoAdopt(context.Background(), mock, "p1", dir)
	if err != nil {
		t.Fatalf("LocalAutoAdopt: %v (cascade miss must NOT fail adoption)", err)
	}
	if result == nil || result.Meta == nil {
		t.Fatal("expected meta despite cascade miss")
	}
	// Meta itself written + stage linked, setup name stays empty.
	if result.Meta.Mode != topology.PlanModeLocalStage {
		t.Errorf("Mode: got %q, want local-stage", result.Meta.Mode)
	}
	if result.Meta.StageSetupName != "" {
		t.Errorf("StageSetupName: got %q, want empty (cascade miss leaves field for later discovery)",
			result.Meta.StageSetupName)
	}
}
