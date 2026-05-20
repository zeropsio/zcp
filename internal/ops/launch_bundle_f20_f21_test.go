// F20/F21 permanent regression pins. Phase 2b (plans/workflow-family-
// architecture-2026-05-14.md §11) landed the bundle.ServiceTypeRules.
// AcceptsMode + ManagedServiceEntry.QuotaGBytes composer fixes;
// these tests now run unconditionally to guard against re-introduction.
//
// F19 (CDN keys leaking into target yaml) closed in Phase 2a at the
// handler-envclass boundary — composer never sees Type=SYSTEM envs
// anymore. Pin migrated to:
//   - internal/envclass/classify_test.go::TestClassifyProjectEnv_CDNKeys_SystemDrops
//   - internal/tools/workflow_launch_production_test.go::
//       TestHandleLaunchProduction_ClassifyPrompt_HidesSystemEnvs
//
// Run: go test ./internal/ops/ -run TestF2

package ops_test

import (
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
)

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
		Runtimes: []ops.LaunchRuntimeInput{
			{
				ProdHostname: "app",
				ServiceType:  "php-nginx@8.3+8.0",
				SetupName:    "prod",
				RepoURL:      "https://github.com/example/laravel",
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
			},
		},
		ProjectEnvs: []ops.ProjectEnvVar{
			{Key: "APP_ENV", Value: "production"},
		},
		ManagedServices: []ops.ManagedServiceEntry{
			{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA"},
			{Hostname: "storage", Type: "object-storage", Mode: ""},
		},
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
