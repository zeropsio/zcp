// Tests for: tools/gitea_recipe_reconcile.go — the setup block A2 names for
// each runtime.
//
// A2 composes unattended, on a pair that may never have deployed: the classic
// bootstrap route leaves PrimarySetupName empty, and the composer's
// "SetupName required" turned that ordinary state into a group recipe that
// could never be proposed (measured on a live Mate 2026-09-16). The
// conventional name is the pair's own hostname — the same one the git-push
// deploy falls back to — and the pair's zerops.yaml is on the mount, so the
// name can be VERIFIED rather than merely warned about.
package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/workflow"
)

// mountWithZeropsYAML lays out a mount root the way ops.MountService does —
// /var/www/{hostname} per pair — and writes body as the pair's zerops.yaml.
func mountWithZeropsYAML(t *testing.T, hostname, filename, body string) string {
	t.Helper()
	root := t.TempDir()
	if filename == "" {
		return root
	}
	dir := filepath.Join(root, hostname)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
	return root
}

func TestComposeGroupRecipeInputs_SetupName(t *testing.T) {
	tests := []struct {
		name string
		// recordedSetup is ServiceMeta.PrimarySetupName — empty for a pair
		// that has never deployed.
		recordedSetup string
		// yamlFile is the file on the mount, "" for a pair that has none.
		yamlFile  string
		yamlBody  string
		wantSetup string
		// wantWarning is a substring the composer's warnings must carry, ""
		// when the setup must come out verified.
		wantWarning string
	}{
		{
			name:      "no setup recorded, the yaml declares the hostname block",
			yamlFile:  "zerops.yaml",
			yamlBody:  "zerops:\n  - setup: appdev\n    run:\n      start: node index.js\n",
			wantSetup: "appdev",
		},
		{
			name:      "a zerops.yml is read just the same",
			yamlFile:  "zerops.yml",
			yamlBody:  "zerops:\n  - setup: appdev\n    run:\n      start: node index.js\n",
			wantSetup: "appdev",
		},
		{
			name:        "no setup recorded and no file on the mount",
			wantSetup:   "appdev",
			wantWarning: "no zerops.yaml was read",
		},
		{
			name:          "a recorded setup wins over the convention",
			recordedSetup: "api",
			yamlFile:      "zerops.yaml",
			yamlBody:      "zerops:\n  - setup: api\n    run:\n      start: node index.js\n",
			wantSetup:     "api",
		},
		{
			name:          "a recorded setup the yaml does not declare is still warned about",
			recordedSetup: "api",
			yamlFile:      "zerops.yaml",
			yamlBody:      "zerops:\n  - setup: appdev\n    run:\n      start: node index.js\n",
			wantSetup:     "api",
			wantWarning:   "is not declared in its zerops.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDir := t.TempDir()
			writeGiteaWiredPairMeta(t, stateDir)
			if err := workflow.UpdateServiceMeta(stateDir, "appdev", func(m *workflow.ServiceMeta) error {
				m.PrimarySetupName = tt.recordedSetup
				return nil
			}); err != nil {
				t.Fatalf("UpdateServiceMeta: %v", err)
			}
			meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
			mountRoot := mountWithZeropsYAML(t, "appdev", tt.yamlFile, tt.yamlBody)

			inputs, err := composeGroupRecipeInputs(
				context.Background(), recipeReconcileClient(), "p1", mountRoot,
				[]*workflow.ServiceMeta{meta},
			)
			if err != nil {
				t.Fatalf("a pair that has never deployed must still compose: %v", err)
			}
			if len(inputs.Runtimes) != 1 {
				t.Fatalf("runtimes = %d, want 1", len(inputs.Runtimes))
			}
			if got := inputs.Runtimes[0].SetupName; got != tt.wantSetup {
				t.Errorf("SetupName = %q, want %q", got, tt.wantSetup)
			}

			_, warnings, err := bundle.BuildGroupRecipe(inputs, nil)
			if err != nil {
				t.Fatalf("BuildGroupRecipe: %v", err)
			}
			joined := strings.Join(warnings, " | ")
			if tt.wantWarning == "" {
				if strings.Contains(joined, "unverified") || strings.Contains(joined, "not declared") {
					t.Errorf("the setup should be verified, not warned about: %s", joined)
				}
				return
			}
			if !strings.Contains(joined, tt.wantWarning) {
				t.Errorf("warnings %q are missing %q", joined, tt.wantWarning)
			}
		})
	}
}
