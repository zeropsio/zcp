package knowledge

import (
	"strings"
	"testing"
)

func TestParseServiceDefinitions_ValidYAML(t *testing.T) {
	t.Parallel()
	content := `# Bun Hello World on Zerops

## zerops.yml

` + "```yaml" + `
zerops:
  - setup: prod
    build:
      base: bun@1.2
` + "```" + `

## Service Definitions

> Per-service blocks extracted from battle-tested recipe imports.

### Dev/Stage (from AI Agent environment)

` + "```yaml" + `
project:
  name: bun-hello-world-agent

services:
  - hostname: appdev
    type: bun@1.2
    zeropsSetup: dev
    buildFromGit: https://github.com/zerops-recipe-apps/bun-hello-world-app
    enableSubdomainAccess: true
    verticalAutoscaling:
      minRam: 0.5

  - hostname: appstage
    type: bun@1.2
    zeropsSetup: prod
    buildFromGit: https://github.com/zerops-recipe-apps/bun-hello-world-app
    enableSubdomainAccess: true
    verticalAutoscaling:
      minRam: 0.5

  - hostname: db
    type: postgresql@18
    mode: NON_HA
    priority: 10
    verticalAutoscaling:
      minRam: 0.25
` + "```" + `

### Small Production

` + "```yaml" + `
project:
  name: bun-hello-world-small-prod

services:
  - hostname: app
    type: bun@1.2
    zeropsSetup: prod
    buildFromGit: https://github.com/zerops-recipe-apps/bun-hello-world-app
    enableSubdomainAccess: true
    minContainers: 2
    verticalAutoscaling:
      minRam: 0.25
      minFreeRamGB: 0.125
` + "```" + `
`

	defs := parseServiceDefinitions(content)

	if defs == nil {
		t.Fatal("expected non-nil ServiceDefinitions")
	}

	if defs.DevStageImport == "" {
		t.Error("DevStageImport should not be empty")
	}
	if defs.SmallProdImport == "" {
		t.Error("SmallProdImport should not be empty")
	}

	// Dev/stage import should contain runtime service entries
	if !strings.Contains(defs.DevStageImport, "bun@1.2") {
		t.Error("DevStageImport should contain runtime type")
	}
	if !strings.Contains(defs.DevStageImport, "minRam: 0.5") {
		t.Error("DevStageImport should contain scaling values")
	}

	// Small prod import should contain production patterns
	if !strings.Contains(defs.SmallProdImport, "minContainers: 2") {
		t.Error("SmallProdImport should contain production scaling")
	}
	if !strings.Contains(defs.SmallProdImport, "minFreeRamGB") {
		t.Error("SmallProdImport should contain free RAM reserve")
	}
}

func TestParseServiceDefinitions_NoSection(t *testing.T) {
	t.Parallel()
	content := `# Laravel on Zerops

## zerops.yml

` + "```yaml" + `
zerops:
  - setup: prod
` + "```" + `
`

	defs := parseServiceDefinitions(content)
	if defs != nil {
		t.Error("expected nil when no Service Definitions section exists")
	}
}

func TestTransformForBootstrap_RemovesBuildFromGit(t *testing.T) {
	t.Parallel()
	input := `project:
  name: bun-hello-world-agent

services:
  - hostname: appdev
    type: bun@1.2
    zeropsSetup: dev
    buildFromGit: https://github.com/zerops-recipe-apps/bun-hello-world-app
    enableSubdomainAccess: true
    verticalAutoscaling:
      minRam: 0.5

  - hostname: db
    type: postgresql@18
    mode: NON_HA
    verticalAutoscaling:
      minRam: 0.25`

	result := TransformForBootstrap(input)

	if strings.Contains(result, "buildFromGit") {
		t.Error("bootstrap transform should remove buildFromGit")
	}
	if strings.Contains(result, "zeropsSetup") {
		t.Error("bootstrap transform should remove zeropsSetup")
	}
	if strings.Contains(result, "enableSubdomainAccess") {
		t.Error("bootstrap transform should remove enableSubdomainAccess (no app listening with startWithoutCode)")
	}
	if !strings.Contains(result, "verticalAutoscaling") {
		t.Error("bootstrap transform should keep verticalAutoscaling")
	}
	if !strings.Contains(result, "minRam: 0.5") {
		t.Error("bootstrap transform should keep scaling values")
	}
	// Dev services (hostname ending in "dev") should get startWithoutCode: true
	// since bootstrap uses SSHFS — the developer drives the server manually.
	if !strings.Contains(result, "startWithoutCode: true") {
		t.Error("bootstrap transform should add startWithoutCode: true for dev services")
	}
}

func TestTransformForBootstrap_NoProdStartWithoutCode(t *testing.T) {
	t.Parallel()
	input := `services:
  - hostname: app
    type: bun@1.2
    zeropsSetup: prod
    buildFromGit: https://github.com/zerops-recipe-apps/bun-hello-world-app
    minContainers: 2
    verticalAutoscaling:
      minRam: 0.25`

	result := TransformForBootstrap(input)

	// Prod-only imports should NOT get startWithoutCode
	if strings.Contains(result, "startWithoutCode") {
		t.Error("bootstrap transform should NOT add startWithoutCode to prod services")
	}
	if !strings.Contains(result, "minContainers: 2") {
		t.Error("bootstrap transform should keep minContainers")
	}
}

func TestStore_GetServiceDefinitions(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("load store: %v", err)
	}

	// This test verifies the Store method works — actual content depends on synced recipes.
	recipes := store.ListRecipes()
	if len(recipes) == 0 {
		t.Skip("no recipes available")
	}

	for _, name := range recipes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			defs := store.GetServiceDefinitions(name)
			// Not all recipes have service definitions (API must be synced).
			// Just verify the method doesn't panic or return inconsistent data.
			if defs != nil {
				if defs.DevStageImport == "" && defs.SmallProdImport == "" {
					t.Error("ServiceDefinitions struct exists but both imports are empty")
				}
			}
		})
	}
}

func TestExtractRuntimeServices_FiltersCorrectly(t *testing.T) {
	t.Parallel()
	input := `project:
  name: test

services:
  - hostname: app
    type: bun@1.2
    verticalAutoscaling:
      minRam: 0.5

  - hostname: db
    type: postgresql@18
    mode: NON_HA

  - hostname: cache
    type: valkey@7.2
    mode: NON_HA`

	runtime, managed := extractServiceEntries(input)

	if len(runtime) == 0 {
		t.Fatal("expected at least one runtime service")
	}
	if !strings.Contains(runtime[0], "bun@1.2") {
		t.Error("runtime service should contain bun type")
	}

	if len(managed) != 2 {
		t.Errorf("expected 2 managed services, got %d", len(managed))
	}
}
