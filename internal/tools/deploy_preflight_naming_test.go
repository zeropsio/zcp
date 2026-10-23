package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A preflight failure names the repo's own config file; a zerops.yaml repo
// reads as before.
func TestDeployPreFlight_NamesTheReposConfigFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		body     string
		recorded string
		want     string
	}{
		{"setup_not_found", "zerops:\n  - setup: web\n    run:\n      start: x\n", "", "no setup entry \"appdev\" found in %s"},
		{"invalid_yaml", "zerops: [\n", "", "%s invalid YAML"},
		{"recorded_setup_not_declared", "zerops:\n  - setup: web\n    run:\n      start: x\n  - setup: api\n    run:\n      start: y\n", "ghost", "which %s does not declare"},
	}
	for _, file := range []string{"zerops.yaml", "zerops.yml"} {
		for _, tt := range tests {
			t.Run(tt.name+"/"+file, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				stateDir := filepath.Join(dir, ".zcp", "state")
				if err := os.MkdirAll(stateDir, 0o755); err != nil {
					t.Fatal(err)
				}
				mount := filepath.Join(dir, "appdev")
				if err := os.MkdirAll(mount, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(mount, file), []byte(tt.body), 0o600); err != nil {
					t.Fatal(err)
				}
				meta := &workflow.ServiceMeta{
					Hostname: "appdev", Mode: topology.PlanModeDev, PrimarySetupName: tt.recorded,
					BootstrapSession: "s1", BootstrappedAt: "2026-04-01T00:00:00Z",
				}
				if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
					t.Fatal(err)
				}
				_, result, err := deployPreFlight(context.Background(), platform.NewMock(), "proj-1", stateDir, "appdev", "appdev", "", "", true)
				var text strings.Builder
				if err != nil {
					var blocker *workflow.ErrRequiresSetupInput
					if !errors.As(err, &blocker) {
						t.Fatalf("unexpected error: %v", err)
					}
					text.WriteString(blocker.Reason)
				}
				if result != nil {
					for _, c := range result.Checks {
						text.WriteString(c.Detail + "\n")
					}
					text.WriteString(result.Summary)
				}
				if want := strings.Replace(tt.want, "%s", file, 1); !strings.Contains(text.String(), want) {
					t.Errorf("missing %q in %q", want, text.String())
				}
				other := "zerops.yml"
				if file == other {
					other = "zerops.yaml"
				}
				if strings.Contains(text.String(), other) {
					t.Errorf("message names %s for a %s repo: %q", other, file, text.String())
				}
			})
		}
	}
}
