package bundle

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestComposeServiceVault_Buckets pins the four-category emission for the
// runtime's user-set service env layer (the slim /env USER layer minus the
// yaml-baked mirror) into its vault (GAP0-1): a generated value and a
// placeholder sensitive, the SECRET-safe default for unclassified entries.
func TestComposeServiceVault_Buckets(t *testing.T) {
	envs := []ProjectEnvVar{
		{Key: "PLAIN_CFG", Value: "info"},
		{Key: "API_KEY", Value: "sk-live-realsecret"},
		{Key: "SESSION_KEY", Value: "x"},
		{Key: "DB_REF", Value: "${db_connectionString}"},
		{Key: "UNCLASSIFIED_SECRET", Value: "should-never-leak"},
		{Key: "STALE_KEY", Value: "leftover-after-refactor"},
	}
	cls := map[string]topology.SecretClassification{
		"PLAIN_CFG":   topology.SecretClassPlainConfig,
		"API_KEY":     topology.SecretClassExternalSecret,
		"SESSION_KEY": topology.SecretClassAutoSecret,
		"DB_REF":      topology.SecretClassInfrastructure,
		"STALE_KEY":   topology.SecretClassExclude,
		// UNCLASSIFIED_SECRET intentionally absent → secret-safe default.
	}
	out, warnings := composeServiceVault(envs, cls)

	if _, present := out["STALE_KEY"]; present {
		t.Errorf("exclude-classified service env must be dropped entirely; got %+v", out["STALE_KEY"])
	}
	want := map[string]vaultValue{
		"PLAIN_CFG":   {value: "info"},
		"API_KEY":     {value: ExternalSecretPlaceholder, sensitive: true},
		"SESSION_KEY": {value: autoSecretPreprocessor, sensitive: true},
		// SECRET-safe: an unclassified entry never carries its source value.
		"UNCLASSIFIED_SECRET": {value: ExternalSecretPlaceholder, sensitive: true},
	}
	for key, w := range want {
		if out[key] != w {
			t.Errorf("%s = %+v, want %+v", key, out[key], w)
		}
	}
	if _, present := out["DB_REF"]; present {
		t.Errorf("infrastructure should be dropped; got %+v", out["DB_REF"])
	}
	// Both external-secret and the unclassified one must surface a warning.
	joined := strings.Join(warnings, "\n")
	for _, k := range []string{"API_KEY", "UNCLASSIFIED_SECRET"} {
		if !strings.Contains(joined, k) {
			t.Errorf("expected a review warning naming %q; warnings:\n%s", k, joined)
		}
	}
}

// TestBuildExport_EmitsServiceVault pins that a runtime's service values land
// in the runtime entry's vault in the export import.yaml (GAP0-1), never in
// the deprecated envSecrets.
func TestBuildExport_EmitsServiceVault(t *testing.T) {
	inputs := BundleInputs{
		ProjectName:    "myproj",
		TargetHostname: "app",
		ServiceType:    "nodejs@22",
		SetupName:      "app",
		RepoURL:        "https://github.com/example/app.git",
		ZeropsYAMLBody: launchYAMLNoDBRef, // setup: app
		ServiceEnvs:    []ProjectEnvVar{{Key: "FEATURE_FLAG", Value: "on"}},
	}
	cls := map[string]topology.SecretClassification{"FEATURE_FLAG": topology.SecretClassPlainConfig}
	b, err := BuildExport(inputs, cls)
	if err != nil {
		t.Fatalf("BuildExport: %v", err)
	}
	if len(b.Errors) > 0 {
		t.Fatalf("schema validation errors (the vault must be schema-valid): %v", b.Errors)
	}
	app := runtimeEntryFromYAML(t, b.ImportYAML, "app")
	if _, legacy := app["envSecrets"]; legacy {
		t.Errorf("runtime entry carries the deprecated envSecrets: %+v", app)
	}
	vault, ok := app["vault"].(map[string]any)
	if !ok {
		t.Fatalf("runtime entry missing vault; entry: %+v", app)
	}
	if vault["FEATURE_FLAG"] != "on" {
		t.Errorf("vault FEATURE_FLAG: got %v want on", vault["FEATURE_FLAG"])
	}
}

// TestBuildLaunch_EmitsPerRuntimeServiceVault pins each runtime's vault in
// the launch bundle (GAP0-1) + the secret-safe default for an unclassified
// SECRET, written sensitive.
func TestBuildLaunch_EmitsPerRuntimeServiceVault(t *testing.T) {
	inputs := launchInputsWith(launchYAMLNoDBRef, nil)
	inputs.Runtimes[0].ServiceEnvs = []ProjectEnvVar{
		{Key: "FEATURE_FLAG", Value: "on"},
		{Key: "STRIPE_KEY", Value: "sk-live-leakme"},
	}
	cls := map[string]topology.SecretClassification{
		"FEATURE_FLAG": topology.SecretClassPlainConfig,
		// STRIPE_KEY unclassified → secret-safe REPLACE_ME.
	}
	b, err := BuildLaunch(inputs, cls)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if len(b.Errors) > 0 {
		t.Fatalf("schema errors: %v", b.Errors)
	}
	if strings.Contains(b.ImportYAML, "sk-live-leakme") {
		t.Fatal("LEAK: unclassified SECRET value reached the launch bundle")
	}
	app := runtimeEntryFromYAML(t, b.ImportYAML, "app")
	vault, ok := app["vault"].(map[string]any)
	if !ok {
		t.Fatalf("runtime entry missing vault; entry: %+v", app)
	}
	if vault["FEATURE_FLAG"] != "on" {
		t.Errorf("FEATURE_FLAG: got %v want on", vault["FEATURE_FLAG"])
	}
	stripe, _ := vault["STRIPE_KEY"].(map[string]any)
	if stripe["value"] != ExternalSecretPlaceholder || stripe["sensitive"] != true {
		t.Errorf("unclassified SECRET STRIPE_KEY: got %v want {value: %s, sensitive: true}", vault["STRIPE_KEY"], ExternalSecretPlaceholder)
	}
}

// runtimeEntryFromYAML parses import.yaml and returns the services[] entry
// with the given hostname.
func runtimeEntryFromYAML(t *testing.T, importYAML, hostname string) map[string]any {
	t.Helper()
	var doc struct {
		Services []map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(importYAML), &doc); err != nil {
		t.Fatalf("parse import yaml: %v\n%s", err, importYAML)
	}
	for _, s := range doc.Services {
		if s["hostname"] == hostname {
			return s
		}
	}
	t.Fatalf("service %q not found in import yaml:\n%s", hostname, importYAML)
	return nil
}
