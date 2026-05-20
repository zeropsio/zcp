package tools

import (
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/topology"
)

// TestReadinessRubric_NilBundle returns nil.
func TestReadinessRubric_NilBundle(t *testing.T) {
	t.Parallel()
	if got := runReadinessRubric(nil, ops.LaunchBundleInputs{}); got != nil {
		t.Fatalf("expected nil for nil bundle, got %v", got)
	}
}

// TestReadinessRubric_AllPassOnCleanBundle covers the happy path —
// every check passes.
func TestReadinessRubric_AllPassOnCleanBundle(t *testing.T) {
	t.Parallel()
	bundle := &ops.LaunchBundle{
		SourceSnapshot: ops.SourceSnapshot{ZeropsYAMLSHA256: "abc123"},
	}
	inputs := ops.LaunchBundleInputs{
		Runtimes: []ops.LaunchRuntimeInput{{ProdHostname: "app", ServiceType: "nodejs@22", RepoURL: "https://example/r.git", ZeropsYAMLBody: "zerops:\n  - setup: prod\n"}},
		ManagedServices: []ops.ManagedServiceEntry{
			{Hostname: "db", Type: "postgresql@16"},
		},
	}
	checks := runReadinessRubric(bundle, inputs)
	if len(checks) != 5 {
		t.Fatalf("expected 5 checks, got %d: %+v", len(checks), checks)
	}
	if hasBlockingFailures(checks) {
		t.Errorf("expected no blocking failures, got: %+v", checks)
	}
}

// TestReadinessRubric_SchemaFailsBlocks pins that bundle.Errors → fail.
func TestReadinessRubric_SchemaFailsBlocks(t *testing.T) {
	t.Parallel()
	bundle := &ops.LaunchBundle{
		Errors:         []schema.ValidationError{{Path: "/foo", Message: "broken"}},
		SourceSnapshot: ops.SourceSnapshot{ZeropsYAMLSHA256: "x"},
	}
	checks := runReadinessRubric(bundle, ops.LaunchBundleInputs{Runtimes: []ops.LaunchRuntimeInput{{ProdHostname: "app", MinContainers: 2}}})
	if !hasBlockingFailures(checks) {
		t.Fatal("expected blocking failure for schema errors")
	}
}

// TestReadinessRubric_LowMinContainersBlocks pins the >= 2 invariant.
func TestReadinessRubric_LowMinContainersBlocks(t *testing.T) {
	t.Parallel()
	bundle := &ops.LaunchBundle{
		SourceSnapshot: ops.SourceSnapshot{ZeropsYAMLSHA256: "x"},
	}
	checks := runReadinessRubric(bundle, ops.LaunchBundleInputs{Runtimes: []ops.LaunchRuntimeInput{{ProdHostname: "app", MinContainers: 1}}})
	if !hasBlockingFailures(checks) {
		t.Fatal("expected blocking failure for minContainers=1")
	}
}

// TestReadinessRubric_MissingSnapshotBlocks pins source-immutability
// substrate.
func TestReadinessRubric_MissingSnapshotBlocks(t *testing.T) {
	t.Parallel()
	bundle := &ops.LaunchBundle{} // SourceSnapshot empty
	checks := runReadinessRubric(bundle, ops.LaunchBundleInputs{Runtimes: []ops.LaunchRuntimeInput{{ProdHostname: "app", MinContainers: 2}}})
	if !hasBlockingFailures(checks) {
		t.Fatal("expected blocking failure for missing source snapshot")
	}
}

// TestReadinessRubric_KeepNonHAWarnsButPasses pins opt-out behavior:
// HA-by-default but KeepNonHA passes-with-warning, not block.
func TestReadinessRubric_KeepNonHAWarnsButPasses(t *testing.T) {
	t.Parallel()
	bundle := &ops.LaunchBundle{
		SourceSnapshot: ops.SourceSnapshot{ZeropsYAMLSHA256: "x"},
	}
	inputs := ops.LaunchBundleInputs{
		Runtimes: []ops.LaunchRuntimeInput{{ProdHostname: "app", ServiceType: "nodejs@22", RepoURL: "https://example/r.git", ZeropsYAMLBody: "zerops:\n  - setup: prod\n"}},
		ManagedServices: []ops.ManagedServiceEntry{
			{Hostname: "valkey", Type: "valkey@7"},
		},
		KeepNonHA: []string{"valkey"},
	}
	checks := runReadinessRubric(bundle, inputs)
	if hasBlockingFailures(checks) {
		t.Errorf("expected no block; KeepNonHA is a warn, got: %+v", checks)
	}
	// Verify the HA check has the warn severity.
	var haCheck *readinessCheck
	for i := range checks {
		if checks[i].ID == readinessCheckHAManagedDeps {
			haCheck = &checks[i]
			break
		}
	}
	if haCheck == nil {
		t.Fatal("expected HA check in rubric")
	}
	if haCheck.Severity != readinessSeverityWarn {
		t.Errorf("HA check with KeepNonHA: severity got %q want warn", haCheck.Severity)
	}
}

// TestReadinessRubric_NoManagedServicesSkipsHACheck pins the no-deps case.
func TestReadinessRubric_NoManagedServicesSkipsHACheck(t *testing.T) {
	t.Parallel()
	bundle := &ops.LaunchBundle{
		SourceSnapshot: ops.SourceSnapshot{ZeropsYAMLSHA256: "x"},
	}
	checks := runReadinessRubric(bundle, ops.LaunchBundleInputs{Runtimes: []ops.LaunchRuntimeInput{{ProdHostname: "app", MinContainers: 2}}})
	for _, c := range checks {
		if c.ID == readinessCheckHAManagedDeps && c.Status != readinessStatusSkip {
			t.Errorf("expected HA check skip when no managed deps, got %q", c.Status)
		}
	}
	// _ = topology to keep import live
	_ = topology.SecretClassPlainConfig
}
