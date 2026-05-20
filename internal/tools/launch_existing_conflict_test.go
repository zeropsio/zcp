package tools

import (
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
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
		{Hostname: "app"},
		{Hostname: "db"},
		{Hostname: "redis"},
	}
	mergeStrategy := map[string]string{
		"app": "skip",
		"db":  "replace",
		// redis intentionally absent — unack'd.
	}
	resolved, unresolved := resolveExistingProjectStrategies(conflicts, mergeStrategy)
	if len(resolved) != 2 {
		t.Errorf("resolved: got %d want 2", len(resolved))
	}
	if len(unresolved) != 1 || unresolved[0].Hostname != "redis" {
		t.Errorf("unresolved: got %+v want [redis]", unresolved)
	}
	// Verify per-strategy mapping.
	wantStrategy := map[string]existingProjectMergeStrategy{
		"app": mergeStrategySkip,
		"db":  mergeStrategyReplace,
	}
	for _, c := range resolved {
		if c.Strategy != wantStrategy[c.Hostname] {
			t.Errorf("strategy for %q: got %q want %q", c.Hostname, c.Strategy, wantStrategy[c.Hostname])
		}
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

// TestApplyMergeSkipsToBundle_DropsSkipFlaggedEntries pins that
// composer trims runtimes + managed deps whose hostname is skip-flagged.
func TestApplyMergeSkipsToBundle_DropsSkipFlaggedEntries(t *testing.T) {
	t.Parallel()
	inputs := ops.LaunchBundleInputs{
		Runtimes: []ops.LaunchRuntimeInput{
			{ProdHostname: "app", ServiceType: "nodejs@22"},
			{ProdHostname: "worker", ServiceType: "nodejs@22"},
		},
		ManagedServices: []ops.ManagedServiceEntry{
			{Hostname: "db", Type: "postgresql@16"},
			{Hostname: "redis", Type: "valkey@7"},
		},
	}
	resolved := []existingProjectConflict{
		{Hostname: "app", Strategy: mergeStrategySkip},
		{Hostname: "db", Strategy: mergeStrategySkip},
	}
	got := applyMergeSkipsToBundle(inputs, resolved)
	if len(got.Runtimes) != 1 || got.Runtimes[0].ProdHostname != "worker" {
		t.Errorf("runtimes: got %+v want [worker]", got.Runtimes)
	}
	if len(got.ManagedServices) != 1 || got.ManagedServices[0].Hostname != "redis" {
		t.Errorf("managed: got %+v want [redis]", got.ManagedServices)
	}
}
