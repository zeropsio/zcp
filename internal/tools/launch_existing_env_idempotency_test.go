// Tests for: tools/launch_existing.go::mutateProjectEnvs idempotency
// (FIX 1 PR 3, eval root-cause review 2026-05-19).
//
// Eval evidence (launch-to-existing-prod-project 20260517-185653):
// first launch call created SESSION_SECRET etc. on the existing project;
// retry hit projectEnvDuplicateKey on the SAME env. Agent deleted
// services thinking they were stale, but project-level envs persist
// independently of service deletion, so the retry loop continued
// failing.
//
// Pre-FIX 1 PR 3: CreateProjectEnv called unconditionally → dup-key.
// After: GetProjectEnv preflight → skip keys already present →
// warnings list what was skipped.
package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

func TestMutateProjectEnvs_AllNew_CreatesAll(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock() // no pre-existing envs

	composer := []bundle.ProjectEnvVar{
		{Key: "API_KEY", Value: "k1"},
		{Key: "SESSION_SECRET", Value: "s1"},
	}
	classifications := map[string]topology.SecretClassification{
		"API_KEY":        topology.SecretClassPlainConfig,
		"SESSION_SECRET": topology.SecretClassPlainConfig,
	}

	warnings, errResult := mutateProjectEnvs(
		context.Background(), mock, t.TempDir(), "lid", "src-pid",
		WorkflowInput{ExistingProjectID: "tgt-pid", ProductionProjectName: "myapp-prod"},
		composer, classifications,
	)
	if errResult != nil {
		t.Fatalf("unexpected error result: %+v", errResult)
	}
	if len(mock.CapturedProjectEnvCreations) != 2 {
		t.Errorf("CreateProjectEnv called %d times, want 2 (all new)", len(mock.CapturedProjectEnvCreations))
	}
	for _, w := range warnings {
		if strings.Contains(w, "already exists") {
			t.Errorf("no env should be skipped on empty target; got warning: %q", w)
		}
	}
}

func TestMutateProjectEnvs_SomeExisting_SkipsDuplicates(t *testing.T) {
	t.Parallel()
	// Target already has SESSION_SECRET; API_KEY is new.
	mock := platform.NewMock().WithProjectEnv([]platform.ProjectEnvVar{
		{Key: "SESSION_SECRET"},
	})

	composer := []bundle.ProjectEnvVar{
		{Key: "API_KEY", Value: "k1"},
		{Key: "SESSION_SECRET", Value: "s1"},
	}
	classifications := map[string]topology.SecretClassification{
		"API_KEY":        topology.SecretClassPlainConfig,
		"SESSION_SECRET": topology.SecretClassPlainConfig,
	}

	warnings, errResult := mutateProjectEnvs(
		context.Background(), mock, t.TempDir(), "lid", "src-pid",
		WorkflowInput{ExistingProjectID: "tgt-pid", ProductionProjectName: "myapp-prod"},
		composer, classifications,
	)
	if errResult != nil {
		t.Fatalf("unexpected error: %+v", errResult)
	}
	// Only API_KEY should be created — SESSION_SECRET skipped.
	if len(mock.CapturedProjectEnvCreations) != 1 {
		t.Errorf("CreateProjectEnv called %d times, want 1 (SESSION_SECRET skipped)", len(mock.CapturedProjectEnvCreations))
	}
	if len(mock.CapturedProjectEnvCreations) > 0 && mock.CapturedProjectEnvCreations[0].Key != "API_KEY" {
		t.Errorf("expected API_KEY created, got %q", mock.CapturedProjectEnvCreations[0].Key)
	}

	// Warning must surface the skip.
	foundSkipWarning := false
	for _, w := range warnings {
		if strings.Contains(w, "SESSION_SECRET") && strings.Contains(w, "already exists") {
			foundSkipWarning = true
			break
		}
	}
	if !foundSkipWarning {
		t.Errorf("warning for SESSION_SECRET skip not found; warnings: %v", warnings)
	}
}

func TestMutateProjectEnvs_AllExisting_SkipsEverything(t *testing.T) {
	t.Parallel()
	// Target already has everything (full retry scenario).
	mock := platform.NewMock().WithProjectEnv([]platform.ProjectEnvVar{
		{Key: "API_KEY"},
		{Key: "SESSION_SECRET"},
	})

	composer := []bundle.ProjectEnvVar{
		{Key: "API_KEY", Value: "k1"},
		{Key: "SESSION_SECRET", Value: "s1"},
	}
	classifications := map[string]topology.SecretClassification{
		"API_KEY":        topology.SecretClassPlainConfig,
		"SESSION_SECRET": topology.SecretClassPlainConfig,
	}

	warnings, errResult := mutateProjectEnvs(
		context.Background(), mock, t.TempDir(), "lid", "src-pid",
		WorkflowInput{ExistingProjectID: "tgt-pid", ProductionProjectName: "myapp-prod"},
		composer, classifications,
	)
	if errResult != nil {
		t.Fatalf("unexpected error: %+v", errResult)
	}
	if len(mock.CapturedProjectEnvCreations) != 0 {
		t.Errorf("CreateProjectEnv called %d times, want 0 (full retry idempotent)", len(mock.CapturedProjectEnvCreations))
	}
	if len(warnings) < 2 {
		t.Errorf("expected 2 skip warnings, got %d: %v", len(warnings), warnings)
	}
}

func TestMutateProjectEnvs_GetProjectEnvFails_FallsThrough(t *testing.T) {
	t.Parallel()
	// Preflight fails; mutation proceeds without idempotency check, but
	// surfaces a warning so the operator knows. Mimics platform outage
	// or token-scope quirks.
	mock := platform.NewMock().WithError("GetProjectEnv", platform.NewPlatformError(platform.ErrAPIError, "preflight down", ""))

	composer := []bundle.ProjectEnvVar{
		{Key: "API_KEY", Value: "k1"},
	}
	classifications := map[string]topology.SecretClassification{
		"API_KEY": topology.SecretClassPlainConfig,
	}

	warnings, errResult := mutateProjectEnvs(
		context.Background(), mock, t.TempDir(), "lid", "src-pid",
		WorkflowInput{ExistingProjectID: "tgt-pid", ProductionProjectName: "myapp-prod"},
		composer, classifications,
	)
	if errResult != nil {
		t.Fatalf("preflight failure must NOT abort mutation: %+v", errResult)
	}
	if len(mock.CapturedProjectEnvCreations) != 1 {
		t.Errorf("CreateProjectEnv should still be called once on preflight failure")
	}

	foundPreflightWarning := false
	for _, w := range warnings {
		if strings.Contains(w, "preflight") {
			foundPreflightWarning = true
			break
		}
	}
	if !foundPreflightWarning {
		t.Errorf("preflight failure must surface a warning; got: %v", warnings)
	}
}
