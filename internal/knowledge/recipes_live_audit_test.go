//go:build live

// Live-network audit: run with `go test -tags live -run TestLiveAllRecipesValidate
// ./internal/knowledge/`. Skipped in the default (offline) suite. Fetches the real
// platform schema and validates EVERY shipped recipe import.yml against it — the
// durable check that a recipe never pins a service type/version the live platform
// dropped, and that every declared type classifies cleanly through topology. The
// embedded coverage pins validate against a frozen version list; this validates
// the committed recipe corpus against the LIVE catalog the import will actually
// hit.
package knowledge

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/topology"
	"gopkg.in/yaml.v3"
)

// TestLiveAllRecipesValidate walks every embedded recipe import.yml.
//
// HARD gate (the schema-refactor surface):
//   - every services[].type EXISTS in the live catalog (s.HasServiceType) — catches
//     a recipe pinning a type/version the platform no longer exposes;
//   - every type classifies as exactly one of managed/runtime/storage/utility — the
//     same topology contract the adopt route relies on when it derives a plan from
//     live-discovered recipe services (verified live: discover returns mode-composite
//     managed `postgresql:single@18` and OS-prefixed runtime `ubuntu/php-nginx@8.4`;
//     both canonicalize + classify correctly).
//
// ADVISORY (logged, non-fatal): ValidateImportYAMLStructure findings. The structure
// schema is STRICTER than the live import endpoint — e.g. it rejects
// `services[].envVariables` (additionalProperties:false), but the platform import
// empirically ACCEPTS that field (verified via zcli service-import 2026-06-02; the
// only error was an unrelated buildFromGit build failure). Several static-SSR recipes
// carry `services[].envVariables`; the field is tolerated by the platform but likely
// silently dropped. Surfaced here for recipe-content review, not failed.
func TestLiveAllRecipesValidate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := schema.FetchSchemas(ctx, "") // "" → CanonicalAPIHost (api.app-prg1.zerops.io)
	if err != nil {
		t.Fatalf("live fetch failed: %v", err)
	}

	files, err := fs.Glob(contentFS, "recipes/*.import.yml")
	if err != nil {
		t.Fatalf("glob recipes: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no recipe import.yml files found in embedded FS")
	}

	type importDoc struct {
		Services []struct {
			Hostname string `yaml:"hostname"`
			Type     string `yaml:"type"`
		} `yaml:"services"`
	}

	var (
		structErrs    []string
		rejectedTypes []string
		unclassified  []string
		typeCount     = map[string]int{}
		recipeCount   int
	)

	for _, f := range files {
		slug := strings.TrimSuffix(strings.TrimPrefix(f, "recipes/"), ".import.yml")
		raw, err := contentFS.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		content := string(raw)
		recipeCount++

		for _, ve := range schema.ValidateImportYAMLStructure(content) {
			structErrs = append(structErrs, fmt.Sprintf("%s: %s", slug, ve.Message))
		}

		var doc importDoc
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("yaml %s: %v", slug, err)
		}
		for _, svc := range doc.Services {
			ty := strings.TrimSpace(svc.Type)
			if ty == "" {
				continue
			}
			typeCount[ty]++
			if !s.HasServiceType(ty) {
				rejectedTypes = append(rejectedTypes, fmt.Sprintf("%s/%s: %q", slug, svc.Hostname, ty))
			}
			classified := topology.IsObjectStorageType(ty) ||
				topology.IsSharedStorageType(ty) ||
				topology.IsManagedService(ty) ||
				topology.IsUtilityType(ty) ||
				topology.IsRuntimeType(ty)
			if !classified {
				unclassified = append(unclassified, fmt.Sprintf("%s/%s: %q", slug, svc.Hostname, ty))
			}
		}
	}

	distinct := make([]string, 0, len(typeCount))
	for ty := range typeCount {
		distinct = append(distinct, ty)
	}
	sort.Strings(distinct)
	fmt.Printf("\n===== LIVE ALL-RECIPES VALIDATE =====\n")
	fmt.Printf("recipes: %d   distinct service types: %d\n", recipeCount, len(distinct))
	for _, ty := range distinct {
		fmt.Printf("   %-22s ×%d  managed=%v storage=%v runtime=%v\n",
			ty, typeCount[ty], topology.IsManagedService(ty) && !topology.IsObjectStorageType(ty) && !topology.IsSharedStorageType(ty),
			topology.IsObjectStorageType(ty) || topology.IsSharedStorageType(ty), topology.IsRuntimeType(ty))
	}

	dump := func(title string, items []string) {
		sort.Strings(items)
		if len(items) > 0 {
			fmt.Printf("%s: %d\n", title, len(items))
			for _, it := range items {
				fmt.Println("   " + it)
			}
		}
	}
	hard := func(title string, items []string) {
		dump(title, items)
		if len(items) > 0 {
			t.Errorf("%s: %d (see above)", title, len(items))
		}
	}
	// ADVISORY: structure schema is stricter than the live import endpoint (it
	// rejects services[].envVariables, which the platform tolerates) — log, don't fail.
	if len(structErrs) > 0 {
		dump("ADVISORY — structure-stricter-than-platform (recipe-content review)", structErrs)
		t.Logf("%d structure-validator advisories (platform tolerates; see above)", len(structErrs))
	}
	// HARD: the schema-refactor surface — type existence + classification.
	hard("REJECTED recipe service types (not in live catalog)", rejectedTypes)
	hard("UNCLASSIFIED recipe types", unclassified)
}
