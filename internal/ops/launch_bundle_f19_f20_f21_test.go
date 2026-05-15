//go:build regression_red

// F19/F20/F21 reproducers — pin the bugs identified in
// plans/launch-production-feedback-fixes-2026-05-13-round3.md. These
// tests MUST FAIL with the current composer (Phase 0). When Phase 2
// of the workflow-family redesign (plans/workflow-family-architecture-
// 2026-05-14.md §11) lands the SDK-driven classifier + server
// baseline composer, these tests flip to PASS and the build tag is
// removed — they become permanent regression pins.
//
// Run: go test -tags regression_red ./internal/ops/ -run TestF1
//
// Why a build tag: default `go test ./...` should stay green during
// the phased v9.91 → v9.92 → v9.93 → v9.95 release sequence. The
// reproducers prove the bugs are observable, but the actual fix
// lands in Phase 2 (v9.92). Gate G2 evidence per plan §13 confirms
// these tests PASS after the fix; at that point the build tag is
// stripped + tests run unconditionally.

package ops_test

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
)

// cdnSourceEnvs returns a source-project env list that includes the
// three platform-managed CDN URL keys verified live against eval-zcp
// (see plans/research/env-types-investigation-2026-05-14.md). The
// current composer (Phase 0) does NOT auto-drop these — they flow
// through user classification and end up in the rendered target yaml.
func cdnSourceEnvs() []ops.ProjectEnvVar {
	return []ops.ProjectEnvVar{
		{Key: "LOG_LEVEL", Value: "info"},
		{Key: "storageCdnUrl", Value: "https://source-storage.example.cdn"},
		{Key: "staticCdnUrl", Value: "https://source-static.example.cdn"},
		{Key: "apiCdnUrl", Value: "https://source-api.example.cdn"},
	}
}

// classifyAllPlainSimple is a local copy of classifyAllPlain so this
// file remains self-contained relative to launch_bundle_test.go's
// helpers (each test file is independently compilable under build tag).
func classifyAllPlainSimple(envs []ops.ProjectEnvVar) map[string]topology.SecretClassification {
	cls := make(map[string]topology.SecretClassification, len(envs))
	for _, e := range envs {
		cls[e.Key] = topology.SecretClassPlainConfig
	}
	return cls
}

// laravelStyleInputs models a Laravel-shaped source with an
// object-storage managed dep. Reproduces the F20 + F21 surface; the
// runtime entry stays minimal so each test's assertion focuses on the
// storage entry exclusively.
func laravelStyleInputs() ops.LaunchBundleInputs {
	return ops.LaunchBundleInputs{
		SourceProjectID:   "f20-f21-repro",
		TargetProjectName: "laravel-prod",
		TargetHostname:    "app",
		ServiceType:       "php-nginx@8.3+8.0",
		SetupName:         "prod",
		RepoURL:           "https://github.com/example/laravel",
		ZeropsYAMLBody: `zerops:
  - setup: prod
    build:
      base:
        - php@8.3
        - nginx@1.22
    run:
      base:
        - php@8.3
        - nginx@1.22
      start: php artisan octane:start
`,
		GitCommitSHA: "f20f21reprosha",
		ProjectEnvs: []ops.ProjectEnvVar{
			{Key: "APP_ENV", Value: "production"},
		},
		ManagedServices: []ops.ManagedServiceEntry{
			{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA"},
			{Hostname: "storage", Type: "object-storage", Mode: ""},
		},
	}
}

// TestF19_CDNKeysLeakIntoTargetImportYaml pins F19: platform-managed
// CDN URL keys (storageCdnUrl / staticCdnUrl / apiCdnUrl) are
// Type=SYSTEM on the server (verified live, env-types-investigation-
// 2026-05-14.md). The composer must NOT emit them into the target
// project's import yaml — the target generates its own CDN URLs
// from its own services.
//
// Current Phase 0 behavior: classifier table (launch_platform_envs.go)
// is missing the 3 CDN keys; agent classifies them as plain-config;
// composer emits them verbatim under project.envVariables; platform
// rejects with `projectEnvUseOfSystemKey`.
//
// Expected post-Phase-2 behavior: the SDK-driven classifier treats
// project envs with `Type=SYSTEM` as universal drop. CDN keys never
// reach the agent's classify prompt; never appear in target yaml.
func TestF19_CDNKeysLeakIntoTargetImportYaml(t *testing.T) {
	inputs := launchStandardPairInputs()
	inputs.ProjectEnvs = cdnSourceEnvs()

	bundle, err := ops.BuildLaunchBundle(inputs, classifyAllPlainSimple(inputs.ProjectEnvs))
	if err != nil {
		t.Fatalf("BuildLaunchBundle: %v", err)
	}

	yamlBody := bundle.ImportYAML
	for _, leaked := range []string{"storageCdnUrl", "staticCdnUrl", "apiCdnUrl"} {
		if strings.Contains(yamlBody, leaked) {
			t.Errorf("F19: target import yaml leaks platform-managed CDN key %q; "+
				"expected SDK Type=SYSTEM classifier to drop before emit.\nyaml:\n%s",
				leaked, yamlBody)
		}
	}
}

// TestF20_ObjectStorageEmitsModeField pins F20 (CRITICAL): the
// platform import API rejects ANY `mode:` value on object-storage
// service entries with `projectImportInvalidParameter:
// {"hostname":["storage"],"storage.mode":["mode not supported"]}`.
// Composer must omit the field for object-storage type.
//
// Current Phase 0 behavior: BuildLaunchBundle's managed-service
// loop (launch_bundle.go:207-225) emits `mode: HA` (or NON_HA when
// KeepNonHA opts in) unconditionally for every managed service,
// including object-storage.
//
// Expected post-Phase-2 behavior: the bundle composer consults
// `serviceTypeAcceptsMode(type)` (or equivalent rule) and omits the
// field for object-storage / shared-storage. Same logic already
// exists in export composer for symmetry.
func TestF20_ObjectStorageEmitsModeField(t *testing.T) {
	inputs := laravelStyleInputs()
	bundle, err := ops.BuildLaunchBundle(inputs, classifyAllPlainSimple(inputs.ProjectEnvs))
	if err != nil {
		t.Fatalf("BuildLaunchBundle: %v", err)
	}

	doc := parseImportYAML(t, bundle.ImportYAML)
	storage := findService(t, doc, "storage")
	if mode, present := storage["mode"]; present {
		t.Errorf("F20: object-storage entry emits `mode: %v` — platform import "+
			"endpoint rejects ANY mode value on this type. Composer must omit "+
			"the field for object-storage (see export composer for the symmetric "+
			"rule).\nstorage entry:\n%+v", mode, storage)
	}
}

// TestF21_ObjectStorageMissingObjectStorageSize pins F21: the
// platform import schema REQUIRES `objectStorageSize` (GB, range
// 1-100) on object-storage entries. Composer must emit the field
// derived from the source service's `quotaGBytes`. After F20 is
// fixed (mode omission), F21 surfaces as the next blocker:
// `projectImportMissingParameter:
// {"hostname":["storage"],"parameter":["storage.objectStorageSize"]}`.
//
// Current Phase 0 behavior: neither launch nor export composer
// maps source quotaGBytes to bundle objectStorageSize. The field is
// always absent.
//
// Expected post-Phase-2 behavior: ManagedServiceEntry carries
// QuotaGBytes (or equivalent); composer emits
// `objectStorageSize: <quotaGBytes>` on object-storage entries.
// Default to 1 when source value is unset.
func TestF21_ObjectStorageMissingObjectStorageSize(t *testing.T) {
	inputs := laravelStyleInputs()
	bundle, err := ops.BuildLaunchBundle(inputs, classifyAllPlainSimple(inputs.ProjectEnvs))
	if err != nil {
		t.Fatalf("BuildLaunchBundle: %v", err)
	}

	doc := parseImportYAML(t, bundle.ImportYAML)
	storage := findService(t, doc, "storage")
	if _, present := storage["objectStorageSize"]; !present {
		t.Errorf("F21: object-storage entry missing required `objectStorageSize` "+
			"field — platform import endpoint rejects without it. Composer must "+
			"propagate source `quotaGBytes` into the bundle (or default to 1 "+
			"when source value is unset).\nstorage entry:\n%+v", storage)
	}
}
