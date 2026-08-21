// Tests for: workflow_export.go + launch_bundle_compose.go — the S2
// read-model migration of the service-scope env carried into a bundle's
// envSecrets. Since 2026-08 a yaml-baked run.envVariables key is ALSO
// mirrored read-only on the slim /env as Type USER (spec
// docs/spec-zerops-env-lifecycle.md §1/§6), so Type alone can no longer
// tell a user-set var apart from the yaml-baked mirror there —
// ops.FetchServiceUserEnvs derives the user-set layer by subtraction
// against the app-version userDataList. These two tests prove that
// subtraction reaches the composed bundle end to end: the user-set key
// survives, the yaml-baked mirror and SYSTEM intrinsics never do.
package tools

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
)

// TestExport_ServiceUserEnv_CarriedNotYamlBaked pins the export path: the
// composed import.yaml's envSecrets carries the runtime's user-set service
// env key (placeholder per classification) and never the yaml-baked
// mirror or a SYSTEM intrinsic.
func TestExport_ServiceUserEnv_CarriedNotYamlBaked(t *testing.T) {
	t.Parallel()

	rt := runtimeService("appdev", "php-apache@8.4", true)
	rt.ActiveAppVersion = &platform.ActiveAppVersionDigest{ID: "av-appdev"}

	mock := newExportMock(
		[]platform.ServiceStack{rt, managedService()},
		[]platform.ProjectEnvVar{
			{Key: "APP_KEY", Content: "old-key"},
		},
	).
		WithServiceEnv("svc-appdev", []platform.ServiceEnvVar{
			{Key: "zeropsSubdomain", Content: "https://appdev.zerops.app", Type: platform.ServiceEnvSystem},
			{Key: "API_TOKEN", Content: "user-set-secret", Type: platform.ServiceEnvUser},
			{Key: "DB_HOST", Content: "${db_hostname}", Type: platform.ServiceEnvUser}, // yaml-baked mirror
		}).
		WithAppVersionUserData("av-appdev", []platform.ServiceEnvVar{
			{Key: "DB_HOST", Content: "${db_hostname}", Type: "USER"},
		})

	dir := t.TempDir()
	writeBootstrappedMeta(t, dir, topology.ModeStandard, topology.GitPushConfigured)

	ssh := &routedSSH{responses: map[string]string{
		"cat /var/www/zerops.yaml": exportTestZeropsYAML,
		"git remote get-url":       "https://github.com/example/demo.git",
	}}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, dir, "", nil, ssh, runtime.Info{InContainer: true}, "")

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"workflow":      "export",
		"targetService": "appdev",
		"variant":       "dev",
		"envClassifications": map[string]any{
			"APP_KEY": "auto-secret",
		},
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	body := decodeExportJSON(t, result)
	bundleOut, _ := body["bundle"].(map[string]any)
	if bundleOut == nil {
		t.Fatalf("bundle should be present, got body: %v", body)
	}
	importYaml, _ := bundleOut["importYaml"].(string)

	var doc struct {
		Services []struct {
			Hostname   string            `yaml:"hostname"`
			EnvSecrets map[string]string `yaml:"envSecrets"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(importYaml), &doc); err != nil {
		t.Fatalf("parse importYaml: %v\nyaml:\n%s", err, importYaml)
	}
	var runtimeEntry *struct {
		Hostname   string            `yaml:"hostname"`
		EnvSecrets map[string]string `yaml:"envSecrets"`
	}
	for i := range doc.Services {
		if doc.Services[i].Hostname == "appdev" {
			runtimeEntry = &doc.Services[i]
			break
		}
	}
	if runtimeEntry == nil {
		t.Fatalf("appdev runtime entry not found in importYaml:\n%s", importYaml)
	}
	if _, ok := runtimeEntry.EnvSecrets["API_TOKEN"]; !ok {
		t.Errorf("envSecrets must carry the user-set API_TOKEN, got: %+v", runtimeEntry.EnvSecrets)
	}
	if _, ok := runtimeEntry.EnvSecrets["DB_HOST"]; ok {
		t.Errorf("envSecrets must NOT carry the yaml-baked mirror DB_HOST, got: %+v", runtimeEntry.EnvSecrets)
	}
	if _, ok := runtimeEntry.EnvSecrets["zeropsSubdomain"]; ok {
		t.Errorf("envSecrets must NOT carry the SYSTEM intrinsic zeropsSubdomain, got: %+v", runtimeEntry.EnvSecrets)
	}
}

// TestLaunchBundle_ServiceUserEnv_CarriedNotYamlBaked pins the launch path
// at the composeLaunchBundleInputs seam: the runtime's ServiceEnvs (which
// the composer emits as envSecrets) carries the user-set key and never the
// yaml-baked mirror or a SYSTEM intrinsic.
func TestLaunchBundle_ServiceUserEnv_CarriedNotYamlBaked(t *testing.T) {
	t.Parallel()

	svc := platform.ServiceStack{
		ID:   "svc-appdev",
		Name: "appdev",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  "php-apache@8.4",
			ServiceStackTypeCategoryName: "USER",
		},
		Status:           "ACTIVE",
		Mode:             "NON_HA",
		ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-appdev"},
	}
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj1", Name: "demo", Status: "ACTIVE"}).
		WithServices([]platform.ServiceStack{svc}).
		WithServiceEnv("svc-appdev", []platform.ServiceEnvVar{
			{Key: "zeropsSubdomain", Content: "https://appdev.zerops.app", Type: platform.ServiceEnvSystem},
			{Key: "API_TOKEN", Content: "user-set-secret", Type: platform.ServiceEnvUser},
			{Key: "DB_HOST", Content: "${db_hostname}", Type: platform.ServiceEnvUser}, // yaml-baked mirror
		}).
		WithAppVersionUserData("av-appdev", []platform.ServiceEnvVar{
			{Key: "DB_HOST", Content: "${db_hostname}", Type: "USER"},
		})

	ssh := &routedSSH{responses: map[string]string{
		"cat /var/www/zerops.yaml": exportTestZeropsYAML,
		"git rev-parse HEAD":       "deadbeef",
	}}

	runtimes := []resolvedLaunchRuntime{{
		ChoiceHostname:  "appdev",
		PushHostname:    "appdev",
		ProdHostname:    "app",
		SetupName:       "appdev",
		SetupProvenance: "dev-setup-promoted",
	}}
	gateChecks := []*LaunchSourceControlCheck{{
		ChoiceHostname: "appdev",
		PushHostname:   "appdev",
		MetaRemoteURL:  "https://github.com/example/demo.git",
	}}

	inputs, warnings, err := composeLaunchBundleInputs(
		context.Background(), mock, ssh, runtime.Info{InContainer: true},
		"proj1", "myapp-prod", runtimes, gateChecks,
		nil, nil, nil, nil, bundle.VariantLaunchNew,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v (warnings: %v)", err, warnings)
	}
	if len(inputs.Runtimes) != 1 {
		t.Fatalf("expected 1 runtime, got %d", len(inputs.Runtimes))
	}

	envs := inputs.Runtimes[0].ServiceEnvs
	keys := map[string]bool{}
	for _, e := range envs {
		keys[e.Key] = true
	}
	if !keys["API_TOKEN"] {
		t.Errorf("ServiceEnvs must carry the user-set API_TOKEN, got: %+v", envs)
	}
	if keys["DB_HOST"] {
		t.Errorf("ServiceEnvs must NOT carry the yaml-baked mirror DB_HOST, got: %+v", envs)
	}
	if keys["zeropsSubdomain"] {
		t.Errorf("ServiceEnvs must NOT carry the SYSTEM intrinsic zeropsSubdomain, got: %+v", envs)
	}
}
