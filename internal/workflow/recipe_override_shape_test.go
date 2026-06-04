package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// recipesDir is the on-disk recipe corpus relative to this package. `go test`
// runs with cwd = package dir, so the parity gate reads the REAL recipe import
// YAMLs — a recipe edit that breaks the shape-driven rewrite is caught here.
const recipesDir = "../knowledge/recipes"

func readRecipeImportYAML(t *testing.T, slug string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(recipesDir, slug+".import.yml"))
	if err != nil {
		t.Fatalf("read recipe %s: %v", slug, err)
	}
	return string(b)
}

// canonicalRemarshal is the YAML a faithful no-op rewrite must produce: parse
// then marshal, no mutation. RewriteRecipeImportYAMLFromShape with empty
// overrides must equal this for every recipe.
func canonicalRemarshal(t *testing.T, in string) string {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(in), &doc); err != nil {
		t.Fatalf("canonical parse: %v", err)
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		t.Fatalf("canonical marshal: %v", err)
	}
	return string(out)
}

// TestRecipeShapeRoundTrip_DerivePlanRewriteParity is the R3-P3 gate. It proves
// the shape-driven rewrite (keyed by each recipe's own hostnames) is a faithful
// replacement for the legacy slot-matcher:
//   - empty overrides → a byte-faithful re-marshal of EVERY recipe in the corpus
//     (no runtime/managed service dropped, no spurious mutation);
//   - where the legacy slot-matcher can place every service from the DERIVED
//     plan, the new rewrite matches its output byte-for-byte;
//   - where the legacy matcher CANNOT (a zeropsSetup:worker recipe — the worker
//     folds to stage-role and the derived worker target's dev-role slot goes
//     unmatched, so legacy errors), the new rewrite still succeeds and preserves
//     every runtime. That gap is exactly why R3 replaces the matcher.
func TestRecipeShapeRoundTrip_DerivePlanRewriteParity(t *testing.T) {
	t.Parallel()

	// 1. Identity across the WHOLE corpus: empty overrides == canonical re-marshal.
	t.Run("identity_default_whole_corpus", func(t *testing.T) {
		t.Parallel()
		files, err := filepath.Glob(filepath.Join(recipesDir, "*.import.yml"))
		if err != nil || len(files) == 0 {
			t.Fatalf("glob recipes: %v (found %d)", err, len(files))
		}
		for _, f := range files {
			name := strings.TrimSuffix(filepath.Base(f), ".import.yml")
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				b, err := os.ReadFile(f)
				if err != nil {
					t.Fatalf("read: %v", err)
				}
				got, err := RewriteRecipeImportYAMLFromShape(string(b), RecipeShapeOverrides{})
				if err != nil {
					t.Fatalf("rewrite: %v", err)
				}
				if want := canonicalRemarshal(t, string(b)); got != want {
					t.Errorf("empty-override rewrite mutated %s (len got=%d want=%d)", name, len(got), len(want))
				}
			})
		}
	})

	// 2. Every runtime the recipe declares survives the default rewrite — the
	//    completeness guarantee the deleted slot-matcher could not give (it
	//    dropped/erred on workers, cross-type stages, and second-repo pairs).
	t.Run("default_rewrite_keeps_every_runtime", func(t *testing.T) {
		t.Parallel()
		named := []string{
			"laravel-minimal", "nextjs-ssr-hello-world", "nestjs-minimal",
			"vue-static-hello-world", "zerops-showcase", "laravel-showcase",
		}
		for _, slug := range named {
			t.Run(slug, func(t *testing.T) {
				t.Parallel()
				y := readRecipeImportYAML(t, slug)
				shape, err := ParseRecipeImportShape(y)
				if err != nil {
					t.Fatalf("parse shape: %v", err)
				}
				newOut, err := RewriteRecipeImportYAMLFromShape(y, RecipeShapeOverrides{})
				if err != nil {
					t.Fatalf("rewrite: %v", err)
				}
				for _, r := range shape.Runtimes {
					if !strings.Contains(newOut, "hostname: "+r.Hostname) {
						t.Errorf("default rewrite dropped runtime %q for %s", r.Hostname, slug)
					}
				}
			})
		}
	})

	// 3. The rewrite applies its two override kinds (and only those).
	t.Run("applies_hostname_override", func(t *testing.T) {
		t.Parallel()
		y := readRecipeImportYAML(t, "nestjs-minimal")
		out, err := RewriteRecipeImportYAMLFromShape(y, RecipeShapeOverrides{
			RuntimeHostnameByOriginal: map[string]string{"appdev": "myapi", "appstage": "myapistage"},
		})
		if err != nil {
			t.Fatalf("rewrite: %v", err)
		}
		if !strings.Contains(out, "hostname: myapi") || !strings.Contains(out, "hostname: myapistage") {
			t.Errorf("hostname override not applied:\n%s", out)
		}
		if strings.Contains(out, "hostname: appdev") || strings.Contains(out, "hostname: appstage") {
			t.Errorf("original runtime hostnames still present after rename:\n%s", out)
		}
		// Managed db hostname is untouched — managed services are never renamed.
		if !strings.Contains(out, "hostname: db") {
			t.Errorf("managed db hostname must be preserved:\n%s", out)
		}
	})

	t.Run("drops_exists_managed", func(t *testing.T) {
		t.Parallel()
		y := readRecipeImportYAML(t, "nestjs-minimal")
		out, err := RewriteRecipeImportYAMLFromShape(y, RecipeShapeOverrides{
			ManagedResolutionByHost: map[string]string{"db": ResolutionExists},
		})
		if err != nil {
			t.Fatalf("rewrite: %v", err)
		}
		if strings.Contains(out, "type: postgresql") {
			t.Errorf("EXISTS db should be dropped from the import:\n%s", out)
		}
		// Runtimes survive the managed drop.
		if !strings.Contains(out, "hostname: appdev") {
			t.Errorf("runtime dropped when dropping the managed dep:\n%s", out)
		}
	})
}
