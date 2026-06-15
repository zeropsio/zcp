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
		ImportYAML:     "project:\n  corePackage: SERIOUS\n  location: eu-central\nservices:\n  - hostname: app\n    startWithoutCode: true\n    minContainers: 2\n",
		SourceSnapshot: ops.SourceSnapshot{ZeropsYAMLSHA256: "abc123"},
	}
	inputs := ops.LaunchBundleInputs{
		Runtimes: []ops.LaunchRuntimeInput{{ProdHostname: "app", ServiceType: "nodejs@22", RepoURL: "https://example/r.git", ZeropsYAMLBody: "zerops:\n  - setup: prod\n"}},
		ManagedServices: []ops.ManagedServiceEntry{
			{Hostname: "db", Type: "postgresql@16"},
		},
	}
	checks := runReadinessRubric(bundle, inputs)
	if len(checks) != 7 {
		t.Fatalf("expected 7 checks, got %d: %+v", len(checks), checks)
	}
	if hasBlockingFailures(checks) {
		t.Errorf("expected no blocking failures, got: %+v", checks)
	}
}

// TestReadinessRubric_SchemaFailsBlocks pins that bundle.Errors → fail.
func TestReadinessRubric_SchemaFailsBlocks(t *testing.T) {
	t.Parallel()
	bundle := &ops.LaunchBundle{
		ImportYAML:     "project:\n  corePackage: SERIOUS\n",
		Errors:         []schema.ValidationError{{Path: "/foo", Message: "broken"}},
		SourceSnapshot: ops.SourceSnapshot{ZeropsYAMLSHA256: "x"},
	}
	checks := runReadinessRubric(bundle, ops.LaunchBundleInputs{Runtimes: []ops.LaunchRuntimeInput{{ProdHostname: "app", MinContainers: 2}}})
	if !hasBlockingFailures(checks) {
		t.Fatal("expected blocking failure for schema errors")
	}
}

// TestReadinessRubric_RawLowMinContainers_PassesAfterComposerFloor is the
// B22 merge regression guard: a source that set minContainers=1 (raw input)
// must NOT false-block — the composer floors it to 2 with a warning, so the
// rubric (now reading the COMPOSED bundle, not the raw input) sees 2 and
// passes. The bug the bright-oak audit found (and proposed deleting the
// whole rubric over) was the rubric reading the raw input here.
func TestReadinessRubric_RawLowMinContainers_PassesAfterComposerFloor(t *testing.T) {
	t.Parallel()
	bundle := &ops.LaunchBundle{
		// The composer's floored output — minContainers:2 in the emitted yaml.
		ImportYAML:     "project:\n  corePackage: SERIOUS\nservices:\n  - hostname: app\n    startWithoutCode: true\n    minContainers: 2\n",
		SourceSnapshot: ops.SourceSnapshot{ZeropsYAMLSHA256: "x"},
	}
	// Raw input still carries 1 — must be ignored by the check.
	checks := runReadinessRubric(bundle, ops.LaunchBundleInputs{Runtimes: []ops.LaunchRuntimeInput{{ProdHostname: "app", MinContainers: 1}}})
	if hasBlockingFailures(checks) {
		t.Fatalf("raw minContainers=1 must NOT block once the composer floored to 2: %+v", checks)
	}
	for _, c := range checks {
		if c.ID == readinessCheckRuntimeMinContainers && c.Status != readinessStatusPass {
			t.Errorf("min-containers check must pass (composed=2); got %q", c.Status)
		}
	}
}

// TestReadinessRubric_ComposedMinContainersBelowFloorBlocks pins that the
// check still catches a genuinely-broken bundle — a sub-2 value in the
// EMITTED yaml WITHOUT a consent input means a composer regression and
// must block. With consent (gap plan P2.1) the same emitted value is a
// reported trade-off: warn-pass, never a block, never silently raised.
func TestReadinessRubric_ComposedMinContainersBelowFloorBlocks(t *testing.T) {
	t.Parallel()
	bundle := &ops.LaunchBundle{
		ImportYAML:     "project:\n  corePackage: SERIOUS\nservices:\n  - hostname: app\n    startWithoutCode: true\n    minContainers: 1\n",
		SourceSnapshot: ops.SourceSnapshot{ZeropsYAMLSHA256: "x"},
	}
	// No consent input → regression → block.
	checks := runReadinessRubric(bundle, ops.LaunchBundleInputs{Runtimes: []ops.LaunchRuntimeInput{{ProdHostname: "app"}}})
	if !hasBlockingFailures(checks) {
		t.Fatal("composed minContainers=1 without consent (composer regression) must block")
	}
	// Consented 1 → warn-pass.
	consented := runReadinessRubric(bundle, ops.LaunchBundleInputs{Runtimes: []ops.LaunchRuntimeInput{{ProdHostname: "app", MinContainers: 1}}})
	if hasBlockingFailures(consented) {
		t.Fatal("consented minContainers=1 must not block")
	}
	foundWarn := false
	for _, c := range consented {
		if c.ID == readinessCheckRuntimeMinContainers && c.Severity == readinessSeverityWarn && c.Status == readinessStatusPass {
			foundWarn = true
		}
	}
	if !foundWarn {
		t.Fatalf("consented sub-floor must surface as warn-pass; got %+v", consented)
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
		ImportYAML:     "project:\n  corePackage: SERIOUS\nservices:\n  - hostname: app\n    startWithoutCode: true\n    minContainers: 2\n",
		SourceSnapshot: ops.SourceSnapshot{ZeropsYAMLSHA256: "x"},
	}
	inputs := ops.LaunchBundleInputs{
		Runtimes: []ops.LaunchRuntimeInput{{ProdHostname: "app", ServiceType: "nodejs@22", RepoURL: "https://example/r.git", ZeropsYAMLBody: "zerops:\n  - setup: prod\n"}},
		ManagedServices: []ops.ManagedServiceEntry{
			{Hostname: "valkey", Type: "valkey@7.2"},
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
		ImportYAML:     "project:\n  corePackage: SERIOUS\n",
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

// TestReadinessRubric_PipelineFirst pins the pipeline-first composition
// invariant as a rubric row (plans/launch-pipeline-first-2026-06-11.md
// P2): the composed import YAML must carry NO buildFromGit and every
// promoted runtime must start via startWithoutCode — a violation means a
// composer regression (block severity).
func TestReadinessRubric_PipelineFirst(t *testing.T) {
	t.Parallel()
	inputs := ops.LaunchBundleInputs{
		Runtimes: []ops.LaunchRuntimeInput{{ProdHostname: "app", ServiceType: "nodejs@22", RepoURL: "https://example/r", ZeropsYAMLBody: "zerops:\n  - setup: prod\n"}},
	}
	pass := &ops.LaunchBundle{
		ImportYAML:     "project:\n  corePackage: SERIOUS\nservices:\n  - hostname: app\n    startWithoutCode: true\n    minContainers: 2\n",
		SourceSnapshot: ops.SourceSnapshot{ZeropsYAMLSHA256: "abc"},
	}
	checks := runReadinessRubric(pass, inputs)
	if !readinessCheckPassed(checks, readinessCheckPipelineFirst) {
		t.Errorf("clean pipeline-first bundle must pass %s: %+v", readinessCheckPipelineFirst, checks)
	}

	regress := &ops.LaunchBundle{
		ImportYAML:     "project:\n  corePackage: SERIOUS\nservices:\n  - hostname: app\n    buildFromGit: https://example/r\n    minContainers: 2\n",
		SourceSnapshot: ops.SourceSnapshot{ZeropsYAMLSHA256: "abc"},
	}
	checks = runReadinessRubric(regress, inputs)
	if readinessCheckPassed(checks, readinessCheckPipelineFirst) {
		t.Errorf("buildFromGit in the composed launch YAML is a composer regression and must fail %s", readinessCheckPipelineFirst)
	}
	if !hasBlockingFailures(checks) {
		t.Errorf("pipeline-first regression must block ready-to-launch")
	}
}

// readinessCheckPassed reports whether the named check is present with
// status=pass.
func readinessCheckPassed(checks []readinessCheck, id string) bool {
	for _, c := range checks {
		if c.ID == id && c.Status == readinessStatusPass {
			return true
		}
	}
	return false
}
