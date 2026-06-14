package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
)

// TestDetectExistingProjectConflicts_NoOverlap pins the no-conflict
// happy path: target services have hostnames disjoint from what the
// launch bundle would create → empty result.
func TestDetectExistingProjectConflicts_NoOverlap(t *testing.T) {
	t.Parallel()
	conflicts := detectExistingProjectConflicts(
		[]string{"app", "worker"},
		[]string{"db", "redis"},
		[]platform.ServiceStack{
			{Name: "different", Status: "ACTIVE"},
		},
	)
	if len(conflicts) != 0 {
		t.Errorf("expected no conflicts, got %+v", conflicts)
	}
}

// TestDetectExistingProjectConflicts_PromotedHostnameMatch fires
// when a promoted runtime collides with an existing target service.
func TestDetectExistingProjectConflicts_PromotedHostnameMatch(t *testing.T) {
	t.Parallel()
	conflicts := detectExistingProjectConflicts(
		[]string{"app", "worker"},
		[]string{"db"},
		[]platform.ServiceStack{
			{
				Name:   "app",
				Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName: "nodejs@22",
				},
			},
		},
	)
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %+v", conflicts)
	}
	if conflicts[0].Hostname != "app" {
		t.Errorf("hostname: got %q want app", conflicts[0].Hostname)
	}
	if conflicts[0].ExistingType != "nodejs@22" {
		t.Errorf("existingType: got %q want nodejs@22", conflicts[0].ExistingType)
	}
}

// TestResolveExistingProjectStrategies_SkipAndReplaceMixed verifies
// that the resolver applies the per-hostname agent ack correctly,
// and surfaces unack'd conflicts in the unresolved list.
func TestResolveExistingProjectStrategies_SkipAndReplaceMixed(t *testing.T) {
	t.Parallel()
	conflicts := []existingProjectConflict{
		{Hostname: "app", Kind: conflictKindRuntime},
		{Hostname: "web", Kind: conflictKindRuntime},
		{Hostname: "redis", Kind: conflictKindRuntime},
	}
	mergeStrategy := map[string]string{
		"app": "skip",
		"web": "replace",
		// redis intentionally absent — unack'd.
	}
	resolved, unresolved := resolveExistingProjectStrategies(conflicts, mergeStrategy)
	if len(resolved) != 2 {
		t.Errorf("resolved: got %d want 2", len(resolved))
	}
	if len(unresolved) != 1 || unresolved[0].Hostname != "redis" {
		t.Errorf("unresolved: got %+v want [redis]", unresolved)
	}
	wantStrategy := map[string]existingProjectMergeStrategy{
		"app": mergeStrategySkip,
		"web": mergeStrategyReplace,
	}
	for _, c := range resolved {
		if c.Strategy != wantStrategy[c.Hostname] {
			t.Errorf("strategy for %q: got %q want %q", c.Hostname, c.Strategy, wantStrategy[c.Hostname])
		}
	}
}

// TestResolveExistingProjectStrategies_ManagedReplaceRejected pins P0-3:
// a managed-kind conflict flagged `replace` is invalid (the platform
// overrides only runtimes), so it must re-surface as unresolved rather
// than resolve to a replace that the import would reject.
func TestResolveExistingProjectStrategies_ManagedReplaceRejected(t *testing.T) {
	t.Parallel()
	conflicts := []existingProjectConflict{
		{Hostname: "db", Kind: conflictKindManaged},
		{Hostname: "app", Kind: conflictKindRuntime},
	}
	mergeStrategy := map[string]string{
		"db":  "replace", // invalid for managed
		"app": "replace", // valid for runtime
	}
	resolved, unresolved := resolveExistingProjectStrategies(conflicts, mergeStrategy)
	if len(unresolved) != 1 || unresolved[0].Hostname != "db" {
		t.Errorf("unresolved: got %+v want [db] (managed replace rejected)", unresolved)
	}
	if len(resolved) != 1 || resolved[0].Hostname != "app" || resolved[0].Strategy != mergeStrategyReplace {
		t.Errorf("resolved: got %+v want [app=replace]", resolved)
	}
}

// TestDetectExistingProjectConflicts_TagsKind pins that the conflict's
// Kind is derived from the desired (bundle) source: a promoted runtime is
// runtime-kind, a managed dep is managed-kind.
func TestDetectExistingProjectConflicts_TagsKind(t *testing.T) {
	t.Parallel()
	conflicts := detectExistingProjectConflicts(
		[]string{"app"},
		[]string{"db"},
		[]platform.ServiceStack{
			{Name: "app", Status: "ACTIVE"},
			{Name: "db", Status: "ACTIVE"},
		},
	)
	kind := map[string]conflictKind{}
	for _, c := range conflicts {
		kind[c.Hostname] = c.Kind
	}
	if kind["app"] != conflictKindRuntime {
		t.Errorf("app kind = %q want runtime", kind["app"])
	}
	if kind["db"] != conflictKindManaged {
		t.Errorf("db kind = %q want managed", kind["db"])
	}
}

// TestMissingDestructiveAckForReplaces_RefusesWithoutAck pins the
// invariant: replace-flagged conflict without confirmDestructive ack
// surfaces the hostname as needing ack.
func TestMissingDestructiveAckForReplaces_RefusesWithoutAck(t *testing.T) {
	t.Parallel()
	resolved := []existingProjectConflict{
		{Hostname: "app", Strategy: mergeStrategyReplace},
		{Hostname: "db", Strategy: mergeStrategySkip},
	}
	missing := missingDestructiveAckForReplaces(resolved, nil)
	if len(missing) != 1 || missing[0] != "app" {
		t.Errorf("missing: got %+v want [app]", missing)
	}
}

// TestMissingDestructiveAckForReplaces_AcceptsMatchingAck pins the
// happy path: ack covers every replace-flagged hostname.
func TestMissingDestructiveAckForReplaces_AcceptsMatchingAck(t *testing.T) {
	t.Parallel()
	resolved := []existingProjectConflict{
		{Hostname: "app", Strategy: mergeStrategyReplace},
	}
	ack := &DestructiveAck{
		Operation:           "launch-production-replace",
		AcknowledgedTargets: []string{"app"},
	}
	missing := missingDestructiveAckForReplaces(resolved, ack)
	if len(missing) != 0 {
		t.Errorf("expected no missing, got %+v", missing)
	}
}

// TestApplyMergeResolutionsToBundle_DropsSkipsAndOverridesReplaces pins
// that the resolver trims skip-flagged entries AND sets Override=true on
// replace-flagged runtimes (so the composer emits `override: true`).
func TestApplyMergeResolutionsToBundle_DropsSkipsAndOverridesReplaces(t *testing.T) {
	t.Parallel()
	inputs := ops.LaunchBundleInputs{
		Runtimes: []ops.LaunchRuntimeInput{
			{ProdHostname: "app", ServiceType: "nodejs@22"},
			{ProdHostname: "web", ServiceType: "nodejs@22"},
			{ProdHostname: "worker", ServiceType: "nodejs@22"},
		},
		ManagedServices: []ops.ManagedServiceEntry{
			{Hostname: "db", Type: "postgresql@16"},
			{Hostname: "redis", Type: "valkey@7.2"},
		},
	}
	resolved := []existingProjectConflict{
		{Hostname: "app", Kind: conflictKindRuntime, Strategy: mergeStrategySkip},
		{Hostname: "web", Kind: conflictKindRuntime, Strategy: mergeStrategyReplace},
		{Hostname: "db", Kind: conflictKindManaged, Strategy: mergeStrategySkip},
	}
	got, changed := applyMergeResolutionsToBundle(inputs, resolved)
	if !changed {
		t.Fatal("changed = false; want true (entries were skipped/overridden)")
	}
	// app dropped (skip), web kept with Override, worker kept untouched.
	overrideByHost := map[string]bool{}
	for _, r := range got.Runtimes {
		overrideByHost[r.ProdHostname] = r.Override
	}
	if _, present := overrideByHost["app"]; present {
		t.Error("app should have been dropped (skip)")
	}
	if !overrideByHost["web"] {
		t.Error("web should carry Override=true (replace)")
	}
	if overrideByHost["worker"] {
		t.Error("worker (no conflict) must not carry Override")
	}
	if len(got.ManagedServices) != 1 || got.ManagedServices[0].Hostname != "redis" {
		t.Errorf("managed: got %+v want [redis]", got.ManagedServices)
	}
}

// TestRuntimeEntry_ReplaceEmitsOverride pins the emitted-YAML contract:
// a replace-acked runtime (Override=true) must carry `override: true` in
// its services[] entry so the platform overwrites the existing service.
func TestRuntimeEntry_ReplaceEmitsOverride(t *testing.T) {
	t.Parallel()
	inputs := ops.LaunchBundleInputs{
		SourceProjectID:   "src",
		TargetProjectName: "prod",
		Variant:           bundle.VariantLaunchExisting,
		Runtimes: []ops.LaunchRuntimeInput{
			{ProdHostname: "web", ServiceType: "nodejs@22", RepoURL: "https://github.com/o/r", SetupName: "prod", Override: true, ZeropsYAMLBody: "zerops:\n  - setup: prod\n    run:\n      start: ./app\n"},
		},
	}
	b, err := ops.BuildLaunchBundle(inputs, nil)
	if err != nil {
		t.Fatalf("BuildLaunchBundle: %v", err)
	}
	if !strings.Contains(b.ImportYAML, "override: true") {
		t.Errorf("replace-acked runtime YAML missing `override: true`:\n%s", b.ImportYAML)
	}
}
