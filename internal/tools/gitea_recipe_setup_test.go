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

	"gopkg.in/yaml.v3"

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
		// wantErr is a substring of the composer's refusal, "" when the
		// recipe composes.
		wantErr string
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
			// The dev half still names its conventional setup, but nothing
			// says what the stage half builds — and a guess would land on
			// the group repo's main for good — so nothing composes yet.
			name:      "no setup recorded and no file on the mount",
			wantSetup: "appdev",
			wantErr:   "no zerops.yaml was read",
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
				// A pair that has never deployed has neither half's setup.
				m.PrimarySetupName = tt.recordedSetup
				m.StageSetupName = ""
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
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
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

// A Mate that joined the group's existing repositories deploys its dev half
// and never its stage half, so its metas record no stage setup (the medusa
// group's second Mate, 2026-09-26). The group environments it proposes build
// the setup the pair's zerops.yaml declares beside the dev one — never the dev
// setup, whose `start: zsc noop` served production a 502.
func TestComposeGroupRecipeInputs_JoinerWithoutStageSetup_BuildsTheYAMLsOtherSetup(t *testing.T) {
	const yamlBody = "zerops:\n  - setup: appdev\n    run:\n      start: zsc noop\n  - setup: appprod\n    run:\n      start: npm start\n"
	tests := []struct {
		name string
		// recordedDev is PrimarySetupName: what the joiner's own dev deploys
		// recorded, "" before its first one.
		recordedDev string
	}{
		{name: "its dev deploys recorded the dev setup", recordedDev: "appdev"},
		{name: "nothing deployed yet — the hostname convention names the dev setup"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDir := t.TempDir()
			writeGiteaWiredPairMeta(t, stateDir)
			if err := workflow.UpdateServiceMeta(stateDir, "appdev", func(m *workflow.ServiceMeta) error {
				m.PrimarySetupName = tt.recordedDev
				m.StageSetupName = ""
				return nil
			}); err != nil {
				t.Fatalf("UpdateServiceMeta: %v", err)
			}
			meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
			mountRoot := mountWithZeropsYAML(t, "appdev", "zerops.yaml", yamlBody)

			inputs, err := composeGroupRecipeInputs(
				context.Background(), recipeReconcileClient(), "p1", mountRoot,
				[]*workflow.ServiceMeta{meta},
			)
			if err != nil {
				t.Fatalf("composeGroupRecipeInputs: %v", err)
			}
			layout, _, err := bundle.BuildGroupRecipe(inputs, nil)
			if err != nil {
				t.Fatalf("BuildGroupRecipe: %v", err)
			}
			for _, tier := range layout.Tiers {
				doc := map[string]any{}
				if err := yaml.Unmarshal([]byte(tier.ImportYAML), &doc); err != nil {
					t.Fatalf("tier %q: %v", tier.Title, err)
				}
				services, _ := doc["services"].([]any)
				for _, raw := range services {
					entry, _ := raw.(map[string]any)
					host, _ := entry["hostname"].(string)
					setup, _ := entry["zeropsSetup"].(string)
					want := "appprod"
					switch host {
					case "appdev":
						want = "appdev"
					case "db":
						continue
					}
					if setup != want {
						t.Errorf("tier %q: %s zeropsSetup = %q, want %q", tier.Title, host, setup, want)
					}
				}
			}
		})
	}
}
