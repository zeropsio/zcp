package knowledge

import (
	"strings"
	"testing"
)

func TestRuntimeResources_GuidesDocumentRAM(t *testing.T) {
	t.Parallel()

	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("load store: %v", err)
	}

	for slug, res := range runtimeResourceMap {
		t.Run(slug, func(t *testing.T) {
			t.Parallel()

			// Look in recipes/{slug}-hello-world (new format) or runtimes/{slug} (legacy)
			doc, err := store.Get("zerops://recipes/" + slug + "-hello-world")
			if err != nil {
				doc, err = store.Get("zerops://runtimes/" + slug)
				if err != nil {
					t.Fatalf("runtime knowledge for %s not found in recipes/ or runtimes/: %v", slug, err)
				}
			}

			content := doc.Content

			// Guide must have a "Resource Requirements" section (H2 or H3) with minRam values.
			if !strings.Contains(content, "## Resource Requirements") && !strings.Contains(content, "### Resource Requirements") {
				t.Errorf("runtime guide %s missing 'Resource Requirements' section (dev minRam %.2g GB, stage minRam %.2g GB)",
					slug, res.DevMinRAM, res.StageMinRAM)
			}
		})
	}
}

func TestRuntimeResources_AllRuntimesHaveConfig(t *testing.T) {
	t.Parallel()

	// Runtimes that compile code on the container should have resource config.
	// Static/nginx/alpine/ubuntu/docker don't compile — excluded.
	compilingRuntimes := []string{
		"go", "java", "nodejs", "php", "python", "rust",
		"elixir", "gleam", "bun", "deno", "ruby", "dotnet",
	}

	for _, slug := range compilingRuntimes {
		t.Run(slug, func(t *testing.T) {
			t.Parallel()
			res := GetRuntimeResources(slug)
			if res.DevMinRAM == 0 {
				t.Errorf("runtime %s has no DevMinRAM in runtimeResourceMap", slug)
			}
			if res.StageMinRAM == 0 {
				t.Errorf("runtime %s has no StageMinRAM in runtimeResourceMap", slug)
			}
		})
	}
}

func TestRuntimeResources_DevAlwaysGEStage(t *testing.T) {
	t.Parallel()

	for slug, res := range runtimeResourceMap {
		t.Run(slug, func(t *testing.T) {
			t.Parallel()
			if res.DevMinRAM < res.StageMinRAM {
				t.Errorf("%s: DevMinRAM (%.2f) < StageMinRAM (%.2f) — dev compiles, should need more",
					slug, res.DevMinRAM, res.StageMinRAM)
			}
		})
	}
}
