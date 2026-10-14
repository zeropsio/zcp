// Tests for: committed recipe import.yml files vs the embedded platform catalog.
package knowledge

import (
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/schema"
	"gopkg.in/yaml.v3"
)

// TestRecipeImports_ServiceTypesInEmbeddedCatalog fails when a shipped recipe
// names a service type the platform no longer offers (an import with it is
// rejected). The embedded catalog is kept equal to the live one by the daily
// schema-drift workflow; the live variant is TestLiveAllRecipesValidate (api tag).
func TestRecipeImports_ServiceTypesInEmbeddedCatalog(t *testing.T) {
	t.Parallel()
	catalog := schema.Embedded()

	files, err := fs.Glob(contentFS, "recipes/*.import.yml")
	if err != nil {
		t.Fatalf("glob recipes: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no recipe import.yml files found in embedded FS")
	}

	var rejected []string
	for _, f := range files {
		raw, err := contentFS.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		var doc struct {
			Services []struct {
				Hostname string `yaml:"hostname"`
				Type     string `yaml:"type"`
			} `yaml:"services"`
		}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("yaml %s: %v", f, err)
		}
		for _, svc := range doc.Services {
			ty := strings.TrimSpace(svc.Type)
			if ty != "" && !catalog.HasServiceType(ty) {
				rejected = append(rejected, fmt.Sprintf("%s/%s: %q", f, svc.Hostname, ty))
			}
		}
	}
	if len(rejected) > 0 {
		t.Errorf("recipes name service types missing from the platform catalog (run `make sync`; if still present the recipe source must change):\n  %s",
			strings.Join(rejected, "\n  "))
	}
}
