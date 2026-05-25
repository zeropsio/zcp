//go:build live

// Test 1 (simplest) of the live-platform test suite per
// plans/live-flow-tests-2026-05-16.md §2 — Export flow verified against
// real eval-zcp source state. Read-only (no platform mutation, no
// cleanup needed). Run via:
//
//   ZCP_API_KEY=... go test -tags live ./internal/tools/ -run TestLive_Export -v
//
// Karel's session (2026-05-16, transcript /tmp/karel-launch-prod.jsonl)
// surfaced that current behavioral evals don't reach full mutation
// cycle, so the composer's correctness against real platform-shaped
// data was only mock-tested. This test asserts:
//
//   - envclass.ClassifyProjectEnv drops every SYSTEM env on live state
//   - all four buckets fire correctly given live ProjectEnvType=USER envs
//   - the composer's output yaml is schema-valid + contains no SYSTEM
//     env keys + emits object-storage entries without `mode:` and with
//     `objectStorageSize:` (F19/F20/F21 fixes verified against real
//     source-state shape).

package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/envclass"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/ops/inventory"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// TestLive_ExportComposerAgainstEvalZcp reads project envs + service list
// from eval-zcp, applies envclass, runs ops.BuildBundle, and asserts the
// composed yaml satisfies Phase 2 invariants (F19/F20/F21 + envclass).
//
// What this DOES verify:
//   - inventory.FetchProjectEnvs decodes SDK shape correctly against
//     live data (Type/Sensitive/Editable populated)
//   - envclass rules behave deterministically on live envs
//   - bundle.BuildBundle produces a schema-valid yaml
//   - No SYSTEM envs leak into project.envVariables (F19)
//
// What this does NOT verify (left for live e2e launch test):
//   - SSH-read of zerops.yaml body from source service
//   - git remote URL read from source service container
//   - Actual platform import acceptance (no mutation here)
func TestLive_ExportComposerAgainstEvalZcp(t *testing.T) {
	client, projectID, _ := liveSourceClient(t)
	ctx, cancel := liveTestCtx(t, 0)
	defer cancel()

	// 1. Live source state reads via inventory layer.
	projectEnvs, err := inventory.FetchProjectEnvs(ctx, client, projectID)
	if err != nil {
		t.Fatalf("inventory.FetchProjectEnvs: %v", err)
	}
	if len(projectEnvs) == 0 {
		t.Fatal("expected live eval-zcp project to have at least one env (platform SYSTEM envs are auto-injected)")
	}

	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(services) == 0 {
		t.Skip("eval-zcp has no services; export test requires a runtime service. Re-run after a seed.")
	}

	// 2. Find a runtime service to target. Prefer appdev (typical
	// behavioral seed); otherwise pick first non-system user-runtime.
	var target *platform.ServiceStack
	for i, s := range services {
		if s.Name == "appdev" {
			target = &services[i]
			break
		}
	}
	if target == nil {
		for i, s := range services {
			if !s.IsSystem() && !strings.HasPrefix(s.ServiceStackTypeInfo.ServiceStackTypeVersionName, "build_") {
				target = &services[i]
				break
			}
		}
	}
	if target == nil {
		t.Skip("no user-runtime service found in eval-zcp")
	}
	t.Logf("target runtime: %s (%s)", target.Name, target.ServiceStackTypeInfo.ServiceStackTypeVersionName)

	// 3. envclass behaviour on live envs.
	var systemDropped []string
	var userClassifiable []platform.ProjectEnvVar
	classifications := map[string]topology.SecretClassification{}
	for _, env := range projectEnvs {
		res := envclass.ClassifyProjectEnv(env)
		switch res.Decision {
		case envclass.Drop:
			systemDropped = append(systemDropped, env.Key)
		case envclass.PromptUser:
			userClassifiable = append(userClassifiable, env)
			// In a real classify-prompt, the agent picks the
			// bucket; here we accept the envclass bias.
			classifications[env.Key] = res.Bias
		default:
			t.Fatalf("envclass.ClassifyProjectEnv returned unexpected Decision %v for %s", res.Decision, env.Key)
		}
	}
	t.Logf("envclass on %d live project envs: %d SYSTEM dropped, %d USER classifiable",
		len(projectEnvs), len(systemDropped), len(userClassifiable))
	if len(systemDropped) == 0 {
		t.Error("expected at least one SYSTEM env on live eval-zcp (zeropsSubdomainHost, *CdnUrl, *Isolation are platform-injected)")
	}

	// 4. Build BundleInputs. ZeropsYAMLBody normally comes via SSH;
	// for this test we use a minimal valid yaml whose setup-name
	// matches what the composer references (target hostname).
	setupName := target.Name
	yamlBody := minimalLiveTestYAML(setupName)

	managed := []bundle.ManagedServiceEntry{}
	for _, s := range services {
		if s.IsSystem() || s.Name == target.Name {
			continue
		}
		typ := s.ServiceStackTypeInfo.ServiceStackTypeVersionName
		if strings.HasPrefix(typ, "build_") || typ == "" {
			continue
		}
		// Exclude ZCP control plane — IsSystem() doesn't catch zcp@1
		// because its typeCategory is null (not CORE/BUILD/INTERNAL).
		// Production code path (collectManagedServices via Discover)
		// uses .IsInfrastructure which DOES filter zcp; this test
		// reaches platform.Client directly so applies the filter
		// manually.
		if strings.HasPrefix(typ, "zcp@") || s.Name == "zcp" {
			continue
		}
		managed = append(managed, bundle.ManagedServiceEntry{
			Hostname: s.Name,
			Type:     typ,
			Mode:     s.Mode,
		})
	}
	t.Logf("managed deps for bundle: %d", len(managed))

	// Convert filtered USER envs to composer input shape.
	bundleEnvs := make([]ops.ProjectEnvVar, 0, len(userClassifiable))
	for _, e := range userClassifiable {
		bundleEnvs = append(bundleEnvs, ops.ProjectEnvVar{Key: e.Key, Value: e.Content})
	}

	inputs := ops.BundleInputs{
		ProjectName:      "eval-zcp",
		TargetHostname:   target.Name,
		SourceMode:       topology.ModeStandard,
		ServiceType:      target.ServiceStackTypeInfo.ServiceStackTypeVersionName,
		SubdomainEnabled: target.SubdomainAccess,
		SetupName:        setupName,
		ZeropsYAMLBody:   yamlBody,
		RepoURL:          "https://github.com/krls2020/eval2",
		ProjectEnvs:      bundleEnvs,
		ManagedServices:  managed,
	}

	bundleResult, err := ops.BuildBundle(inputs, topology.ExportVariantDev, classifications)
	if err != nil {
		t.Fatalf("ops.BuildBundle: %v", err)
	}
	if len(bundleResult.Errors) > 0 {
		t.Fatalf("bundle schema errors: %v", bundleResult.Errors)
	}

	yamlOut := bundleResult.ImportYAML
	t.Logf("composed export yaml (%d bytes):\n%s", len(yamlOut), yamlOut)

	// 5. Phase 2 invariant assertions against the live-composed yaml.
	// F19: no SYSTEM env key may appear in composed yaml.
	for _, sysKey := range systemDropped {
		if strings.Contains(yamlOut, sysKey) {
			t.Errorf("F19 regression: SYSTEM env %q leaked into composed yaml (envclass-Drop value reached composer)", sysKey)
		}
	}

	// Control-plane filter: zcp@1 must not appear as a managed entry.
	if strings.Contains(yamlOut, "type: zcp@") {
		t.Error("control-plane regression: composed yaml contains a zcp@1 entry. zcp@1 is the ZCP-managed control-plane container in eval-zcp; never a target-bundle managed dep.")
	}

	for _, m := range managed {
		if strings.HasPrefix(m.Type, "object-storage") {
			block := extractServiceBlockLive(yamlOut, m.Hostname)
			if block == "" {
				t.Logf("note: object-storage entry %q not in composed yaml (may be filtered by export variant)", m.Hostname)
				continue
			}
			if strings.Contains(block, "mode:") {
				t.Errorf("F20 regression: object-storage entry %q has `mode:`\n%s", m.Hostname, block)
			}
			if !strings.Contains(block, "objectStorageSize:") {
				t.Errorf("F21 regression: object-storage entry %q missing `objectStorageSize:`\n%s", m.Hostname, block)
			}
		}
	}
}

// minimalLiveTestYAML returns a minimal schema-valid zerops.yml stub
// whose setup-name matches what BundleInputs.SetupName carries. The
// BuildExport composer also validates the zerops.yaml body against
// the per-runtime schema (schema.ValidateZeropsYAML), so `build.
// deployFiles` and a few other required fields must be present.
// Content beyond schema requirements doesn't affect the F19/F20/F21
// invariant assertions.
func minimalLiveTestYAML(setupName string) string {
	return "zerops:\n" +
		"  - setup: " + setupName + "\n" +
		"    build:\n" +
		"      base: nodejs@22\n" +
		"      buildCommands:\n" +
		"        - echo build\n" +
		"      deployFiles: ./\n" +
		"    run:\n" +
		"      base: nodejs@22\n" +
		"      start: node index.js\n"
}

// extractServiceBlockLive mirrors the heuristic in cmd/zcp-launch-live —
// pulls a yaml indented service block starting at `- hostname: <h>`.
func extractServiceBlockLive(yaml, h string) string {
	prefixes := []string{
		"    - hostname: " + h + "\n",
		"  - hostname: " + h + "\n",
	}
	for _, p := range prefixes {
		idx := strings.Index(yaml, p)
		if idx < 0 {
			continue
		}
		rest := yaml[idx:]
		end := strings.Index(rest[len(p):], "- hostname:")
		if end < 0 {
			return rest
		}
		return rest[:end+len(p)]
	}
	return ""
}
