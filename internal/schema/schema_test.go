package schema

import (
	"os"
	"slices"
	"testing"
)

func TestEmbeddedSchemas_LocalStorageSingle_Present(t *testing.T) {
	t.Parallel()
	if !Embedded().HasServiceType("local-storage:single@1") {
		t.Error("embedded import schema is missing active local-storage:single@1")
	}
}

func TestEmbeddedSchemas_LocalStorageVolumeFields_Present(t *testing.T) {
	t.Parallel()
	const body = `zerops:
  - setup: app
    run:
      base: alpine/nodejs@22
      volume:
        hostname: data
        mountPath: /var/lib/app-data
        readOnly: true
`
	if errs := ValidateZeropsYAMLStructure(body, "app"); len(errs) > 0 {
		t.Fatalf("embedded zerops.yaml schema rejected Local Storage volume fields: %v", errs)
	}
}

// TestParseSchemas_PopulatesEnumSlices pins that parse extracts the raw
// enum slices the catalog consumes (ServiceTypes / BuildBases / RunBases).
// Members are pinned in their LIVE composite/decorated spelling (the
// 2026-06 schema is composite-only; bare forms left the raw enum) —
// bare-input acceptance is the catalog's equivalence concern, pinned by
// TestCatalogHasBase_CompositeAndBare, never raw membership.
func TestParseSchemas_PopulatesEnumSlices(t *testing.T) {
	t.Parallel()
	zyData, err := os.ReadFile("testdata/zerops_yml_schema.json")
	if err != nil {
		t.Fatalf("read zerops yml schema: %v", err)
	}
	zy, err := ParseZeropsYmlSchema(zyData)
	if err != nil {
		t.Fatalf("parse zerops yml schema: %v", err)
	}
	for _, want := range []string{"alpine/nodejs@22", "ubuntu/rust@stable"} {
		if !slices.Contains(zy.BuildBases, want) {
			t.Errorf("BuildBases missing %q", want)
		}
	}
	for _, want := range []string{"alpine/nodejs@22", "static", "zcp@1"} {
		if !slices.Contains(zy.RunBases, want) {
			t.Errorf("RunBases missing %q", want)
		}
	}
	if slices.Contains(zy.BuildBases, "nodejs@22") {
		t.Error("bare spelling unexpectedly present in raw BuildBases — if the platform re-adds it, revisit the catalog equivalence comments")
	}

	iyData, err := os.ReadFile("testdata/import_yml_schema.json")
	if err != nil {
		t.Fatalf("read import yml schema: %v", err)
	}
	iy, err := ParseImportYmlSchema(iyData)
	if err != nil {
		t.Fatalf("parse import yml schema: %v", err)
	}
	for _, want := range []string{"postgresql:ha@16", "mariadb:single@10.6", "shared-storage:ha", "object-storage", "alpine/nodejs@22"} {
		if !slices.Contains(iy.ServiceTypes, want) {
			t.Errorf("ServiceTypes missing %q", want)
		}
	}
}

func TestParseImportYmlSchema_Enums(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/import_yml_schema.json")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}

	s, err := ParseImportYmlSchema(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// services[].mode was deprecated as an enum field in the May 2026
	// schema refresh — operation mode is now derived from the
	// service-type version name (`postgresql:ha@18` vs
	// `postgresql:single@18`). Modes is allowed to be empty; legacy
	// values (HA, NON_HA) are still recognized if the schema retains them.
	for _, m := range s.Modes {
		if m != "HA" && m != "NON_HA" {
			t.Errorf("unexpected mode %q in Modes; legacy enum should yield HA/NON_HA only", m)
		}
	}
	if !slices.Contains(s.CorePackages, "LIGHT") || !slices.Contains(s.CorePackages, "SERIOUS") {
		t.Errorf("expected core packages [LIGHT, SERIOUS], got %v", s.CorePackages)
	}
	if len(s.StoragePolicies) == 0 {
		t.Error("expected storage policies, got none")
	}
}

func TestParseInvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := ParseZeropsYmlSchema([]byte("{invalid"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}

	_, err = ParseImportYmlSchema([]byte("{invalid"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
