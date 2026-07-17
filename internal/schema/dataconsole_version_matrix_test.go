//go:build api

// Live-network audit: run with `go test -tags api -run
// TestLiveVersionMatrixImportYAMLValidates ./internal/schema/`. Skipped in
// the default (offline) suite. Validates
// e2e/testdata/dataconsole/version-matrix.import.yaml — the Data Console's
// alternate-version throwaway compatibility fixture
// (docs/spec-dataconsole-testing.md §8) — against the LIVE platform schema,
// following the same pattern as TestLiveAllTypesAudit (this package) and
// TestLiveAllRecipesValidate (internal/knowledge): fetch the real catalog,
// then confirm every declared services[].type is one the live platform
// actually accepts. This is the load-bearing check for a version-matrix
// fixture specifically — pinning an alternate engine version that the
// platform later drops would otherwise only surface as a mysterious import
// failure during the (expensive, manual) provisioning rehearsal.
package schema

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

const versionMatrixImportYAMLPath = "../../e2e/testdata/dataconsole/version-matrix.import.yaml"

func TestLiveVersionMatrixImportYAMLValidates(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := FetchSchemas(ctx, "") // "" → CanonicalAPIHost (api.app-prg1.zerops.io)
	if err != nil {
		t.Fatalf("live fetch failed: %v", err)
	}

	raw, err := os.ReadFile(versionMatrixImportYAMLPath)
	if err != nil {
		t.Fatalf("read %s: %v", versionMatrixImportYAMLPath, err)
	}
	content := string(raw)

	for _, ve := range ValidateImportYAMLStructure(content) {
		t.Errorf("structure validation: %s", ve.Error())
	}

	var doc struct {
		Services []struct {
			Hostname string `yaml:"hostname"`
			Type     string `yaml:"type"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	if len(doc.Services) == 0 {
		t.Fatal("no services found in version-matrix.import.yaml")
	}
	for _, svc := range doc.Services {
		if !s.HasServiceType(svc.Type) {
			t.Errorf("%s: type %q is REJECTED by the live catalog (platform dropped this version?)", svc.Hostname, svc.Type)
		}
	}
	fmt.Printf("version-matrix.import.yaml: %d services validated against the live catalog\n", len(doc.Services))
}
