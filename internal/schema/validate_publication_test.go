package schema

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

const flipImportYAML = `services:
  - hostname: agenthome
    type: local-storage:single@1
    verticalAutoscaling:
      cpuMode: SHARED
      minCpu: 1
      maxCpu: 8
      minRam: 0.125
      maxRam: 48
      minDisk: 5
      maxDisk: 5
      minFreeRamGB: 0.125
  - hostname: storage
    type: object-storage
    objectStorageSize: 1
`

func TestImportValidator_CachedPublication_UsesPublishedConstraints(t *testing.T) {
	t.Parallel()
	cache := NewCache(time.Hour, "https://instance.example", nil)
	publication := copySchemas(Embedded())
	// A publication constraint absent from the embedded floor proves which
	// document owns the verdict, without adding another schema artifact.
	publication.ImportYml.Raw = cloneSchema(t, publication.ImportYml.Raw)
	navigatePath(publication.ImportYml.Raw, "properties", "services", "items", "properties", "objectStorageSize")["minimum"] = float64(3)
	cache.fetchSchemas = func(context.Context, string) (*Schemas, error) { return publication, nil }
	validator, err := cache.ImportValidator(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if validator.SourceURL != "https://instance.example"+importYmlSchemaPath || len(validator.Digest) != 64 {
		t.Fatalf("missing publication identity: %q %q", validator.SourceURL, validator.Digest)
	}
	errs := validator.Validate(flipImportYAML)
	if len(errs) == 0 {
		t.Fatal("cached publication's objectStorageSize minimum must reject embedded-valid input")
	}
	found := false
	for _, failure := range errs {
		if failure.Service == "storage" && failure.Path == "/services/1/objectStorageSize" && strings.HasSuffix(failure.SchemaPath, "/objectStorageSize/minimum") && failure.Correction != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing service/schema-path/correction diagnostic: %+v", errs)
	}
}

func TestImportValidator_FlipShape_AcceptsNestedScalingAndUnversionedStorage(t *testing.T) {
	t.Parallel()
	cache := NewCache(time.Hour, "", nil)
	cache.fetchSchemas = func(context.Context, string) (*Schemas, error) { return Embedded(), nil }
	validator, err := cache.ImportValidator(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if errs := validator.Validate(flipImportYAML); len(errs) != 0 {
		t.Fatalf("published nested shape rejected: %+v", errs)
	}
}

func TestImportValidator_FlatMinCPU_NamesSchemaLocationAndCorrection(t *testing.T) {
	t.Parallel()
	cache := NewCache(time.Hour, "", nil)
	cache.fetchSchemas = func(context.Context, string) (*Schemas, error) { return Embedded(), nil }
	validator, err := cache.ImportValidator(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	errs := validator.Validate("services:\n  - hostname: flip\n    type: ubuntu/nodejs@24\n    minCpu: 1\n")
	if len(errs) == 0 {
		t.Fatal("flat service minCpu must be refused")
	}
	for _, failure := range errs {
		if failure.Service == "flip" && failure.Path == "/services/0" && strings.HasSuffix(failure.SchemaPath, "/additionalProperties") && strings.Contains(failure.Correction, "verticalAutoscaling.minCpu") {
			return
		}
	}
	t.Fatalf("missing flat-field correction from schema: %+v", errs)
}

func TestImportValidator_PublicationUnavailable_NeverUsesEmbeddedFloor(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"missing", "uncompilable"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			cache := NewCache(time.Hour, "", nil)
			cache.fetchSchemas = func(context.Context, string) (*Schemas, error) {
				if scenario == "missing" {
					return nil, errors.New("publication offline")
				}
				publication := copySchemas(Embedded())
				publication.ImportYml.Raw = map[string]any{"type": "not-a-json-schema-type"}
				return publication, nil
			}
			validator, err := cache.ImportValidator(context.Background())
			if err == nil || !strings.Contains(err.Error(), "validation unavailable") || validator != nil {
				t.Fatalf("want unavailable without validator, got %v, %v", validator, err)
			}
			if strings.Contains(err.Error(), "invalid recipe") {
				t.Fatalf("unavailable publication classified as recipe invalid: %v", err)
			}
		})
	}
}

func TestImportValidator_RefreshFailure_KeepsLastPublishedValidator(t *testing.T) {
	t.Parallel()
	cache := NewCache(-time.Second, "", nil)
	fetches := 0
	cache.fetchSchemas = func(context.Context, string) (*Schemas, error) {
		fetches++
		if fetches > 1 {
			return nil, errors.New("publication offline")
		}
		return Embedded(), nil
	}
	first, err := cache.ImportValidator(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := cache.ImportValidator(context.Background())
	if err != nil || first != second {
		t.Fatalf("last published validator lost: %v", err)
	}
}

func cloneSchema(t *testing.T, raw map[string]any) map[string]any {
	t.Helper()
	body, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
