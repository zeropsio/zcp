package bundle

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestComposeServiceEnvSecrets_Buckets pins the four-category emission for
// the runtime's USER-set service env (Type=SECRET) layer (GAP0-1), with the
// SECRET-safe default for unclassified entries.
func TestComposeServiceEnvSecrets_Buckets(t *testing.T) {
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
	out, warnings := composeServiceEnvSecrets(envs, cls)

	if _, present := out["STALE_KEY"]; present {
		t.Errorf("exclude-classified service env must be dropped entirely; got %q", out["STALE_KEY"])
	}

	if out["PLAIN_CFG"] != "info" {
		t.Errorf("plain-config should carry verbatim; got %q", out["PLAIN_CFG"])
	}
	if out["API_KEY"] != ExternalSecretPlaceholder {
		t.Errorf("external-secret should be REPLACE_ME; got %q", out["API_KEY"])
	}
	if out["SESSION_KEY"] != autoSecretPreprocessor {
		t.Errorf("auto-secret should be generateRandomString; got %q", out["SESSION_KEY"])
	}
	if _, present := out["DB_REF"]; present {
		t.Errorf("infrastructure should be dropped; got %q", out["DB_REF"])
	}
	// SECRET-safe: an unclassified SECRET entry must NEVER carry its source
	// value — it collapses to the placeholder.
	if out["UNCLASSIFIED_SECRET"] != ExternalSecretPlaceholder {
		t.Errorf("unclassified SECRET must be REPLACE_ME (secret-safe), never the value; got %q", out["UNCLASSIFIED_SECRET"])
	}
	if strings.Contains(strings.Join([]string{out["UNCLASSIFIED_SECRET"]}, ""), "should-never-leak") {
		t.Error("LEAK: unclassified SECRET value reached the bundle")
	}
	// Both external-secret and the unclassified one must surface a warning.
	joined := strings.Join(warnings, "\n")
	for _, k := range []string{"API_KEY", "UNCLASSIFIED_SECRET"} {
		if !strings.Contains(joined, k) {
			t.Errorf("expected a review warning naming %q; warnings:\n%s", k, joined)
		}
	}
}

// TestBuildExport_EmitsServiceEnvSecrets pins that a runtime's service
// envSecrets land on the runtime entry in the export import.yaml (GAP0-1).
func TestBuildExport_EmitsServiceEnvSecrets(t *testing.T) {
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
		t.Fatalf("schema validation errors (envSecrets must be schema-valid): %v", b.Errors)
	}
	app := runtimeEntryFromYAML(t, b.ImportYAML, "app")
	es, ok := app["envSecrets"].(map[string]any)
	if !ok {
		t.Fatalf("runtime entry missing envSecrets; entry: %+v", app)
	}
	if es["FEATURE_FLAG"] != "on" {
		t.Errorf("envSecrets FEATURE_FLAG: got %v want on", es["FEATURE_FLAG"])
	}
}

// TestBuildLaunch_EmitsPerRuntimeServiceEnvSecrets pins per-runtime
// envSecrets emission in the launch bundle (GAP0-1) + the secret-safe
// default for an unclassified SECRET.
func TestBuildLaunch_EmitsPerRuntimeServiceEnvSecrets(t *testing.T) {
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
	es, ok := app["envSecrets"].(map[string]any)
	if !ok {
		t.Fatalf("runtime entry missing envSecrets; entry: %+v", app)
	}
	if es["FEATURE_FLAG"] != "on" {
		t.Errorf("FEATURE_FLAG: got %v want on", es["FEATURE_FLAG"])
	}
	if es["STRIPE_KEY"] != ExternalSecretPlaceholder {
		t.Errorf("unclassified SECRET STRIPE_KEY: got %v want %q", es["STRIPE_KEY"], ExternalSecretPlaceholder)
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
