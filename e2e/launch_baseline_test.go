//go:build e2e

// launch_baseline_test.go — narrow live protocol-conformance canary for
// the env-type taxonomy.
//
// What this still pins (the ONLY reason the file survives): the ZCP
// platform wrappers decode the server's env Type fields with an
// UNCHECKED string cast — GetProjectEnv does `ProjectEnvType(e.Type)`
// and GetServiceEnv does `ServiceEnvType(e.Type)` (internal/platform/
// zerops_env.go). Both target types are plain `type … string`, so a
// server-side enum ADDITION decodes verbatim and is silently accepted
// everywhere else in ZCP. This test is the one place that reads the
// running platform's project + service envs and asserts every decoded
// EnvType / UserDataType is a member of the known CLOSED set — so a new
// server enum surfaces here instead of leaking unnoticed into envclass.
//
// What it deliberately no longer covers: DTO field reachability +
// closed-set decode against fixtures are pinned at the unit layer
// (internal/platform/env_types_test.go, internal/envclass/
// classify_test.go). Re-asserting them here was dead duplication.
//
// Run:
//   ZCP_API_KEY=<token> go test -tags e2e ./e2e/ -run TestLaunchBaseline -v
//
// SKIPS when ZCP_API_KEY is unset (same convention as the rest of the
// e2e suite — see helpers_test.go::newHarness).

package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// knownProjectEnvTypes is the closed set the ZCP wrapper recognizes for
// ProjectEnvVar.Type (mirrors the ProjectEnvType constants in
// internal/platform/types.go). A live value outside this set means the
// server grew the enum and the unchecked cast swallowed it.
var knownProjectEnvTypes = map[platform.ProjectEnvType]struct{}{
	platform.ProjectEnvUser:   {},
	platform.ProjectEnvSystem: {},
}

// knownServiceEnvTypes is the closed set for ServiceEnvVar.Type (mirrors
// the ServiceEnvType constants in internal/platform/types.go).
var knownServiceEnvTypes = map[platform.ServiceEnvType]struct{}{
	platform.ServiceEnvUser:   {},
	platform.ServiceEnvSystem: {},
}

// TestLaunchBaseline_EnvTypeEnumsStayClosed reads the live project + a
// live service's envs through the platform client and asserts every
// decoded Type is a member of the known closed set. This is the only
// guard against a server-side enum addition that the wrappers' unchecked
// string cast would otherwise accept silently.
func TestLaunchBaseline_EnvTypeEnumsStayClosed(t *testing.T) {
	h := newHarness(t) // skips when ZCP_API_KEY unset

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// --- Project envs ---
	// Every Zerops project carries platform SYSTEM envs (zeropsSubdomainHost,
	// CDN URLs, …), so EnvList is never empty even on a fresh project.
	projEnvs, err := h.client.GetProjectEnv(ctx, h.projectID)
	if err != nil {
		t.Fatalf("GetProjectEnv: %v", err)
	}
	if len(projEnvs) == 0 {
		t.Fatal("project envs empty — even a fresh project carries platform SYSTEM envs; " +
			"server-side change or wrong project resolved?")
	}
	t.Logf("project %s — %d envs", h.projectID, len(projEnvs))
	hasSystem := false
	for _, e := range projEnvs {
		if e.Type == platform.ProjectEnvSystem {
			hasSystem = true
		}
		if _, ok := knownProjectEnvTypes[e.Type]; !ok {
			t.Errorf("project env %q: Type=%q outside the known closed set (USER|SYSTEM) — "+
				"the server grew EnvTypeEnum and the unchecked cast swallowed it", e.Key, e.Type)
		}
	}
	if !hasSystem {
		t.Error("no Type=SYSTEM project envs — expected zeropsSubdomainHost / CDN URLs; " +
			"server stopped injecting them or wrong project resolved")
	}

	// --- Service envs ---
	// Pick the first non-system service; any service type works (managed
	// deps expose READ_ONLY envs, runtime services expose ENV/EDITABLE).
	services, err := h.client.ListServices(ctx, h.projectID)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	var target *platform.ServiceStack
	for i := range services {
		if !services[i].IsSystem() {
			target = &services[i]
			break
		}
	}
	if target == nil {
		t.Log("no non-system service in project — service-env enum check skipped")
		return
	}

	svcEnvs, err := h.client.GetServiceEnv(ctx, target.ID)
	if err != nil {
		t.Logf("GetServiceEnv on %s (%s): %v — service-env enum check skipped", target.Name, target.ID, err)
		return
	}
	t.Logf("service %s (%s) — %d envs", target.Name, target.ID, len(svcEnvs))
	for _, e := range svcEnvs {
		if _, ok := knownServiceEnvTypes[e.Type]; !ok {
			t.Errorf("service env %q: Type=%q outside the known closed set "+
				"(READ_ONLY|EDITABLE|SECRET|INTERNAL|ENV) — server grew UserDataTypeEnum and "+
				"the unchecked cast swallowed it", e.Key, e.Type)
		}
	}
}
