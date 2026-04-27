//go:build e2e

// Tests for: e2e — orphan-meta auto-cleanup primitive against eval-zcp (E3).
//
// Engine plan 2026-04-27 ticket E3 made orphan ServiceMeta cleanup a
// transparent side-effect of bootstrap-start. The cleanup primitive is
// `workflow.PruneServiceMetas(stateDir, liveHostnames)`, fed by
// `ops.ListProjectServices`. This test exercises that path against the
// real Zerops API: live services from eval-zcp are converted to a
// liveHostnames map; orphan metas seeded in a temp state dir whose
// hostnames don't match any live service are pruned, while one seeded
// against a real hostname survives.

package e2e_test

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestE2E_PruneServiceMetas_DeletesOrphansAgainstLiveEvalZcp(t *testing.T) {
	token := os.Getenv("ZCP_API_KEY")
	if token == "" {
		t.Skip("ZCP_API_KEY not set — skipping E2E test")
	}

	apiHost := os.Getenv("ZCP_API_HOST")
	if apiHost == "" {
		apiHost = "api.app-prg1.zerops.io"
	}

	client, err := platform.NewZeropsClient(token, apiHost)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	authInfo, err := auth.Resolve(ctx, client)
	if err != nil {
		t.Fatalf("auth resolve: %v", err)
	}

	live, err := ops.ListProjectServices(ctx, client, authInfo.ProjectID)
	if err != nil {
		t.Fatalf("ListProjectServices: %v", err)
	}
	if len(live) == 0 {
		t.Fatal("eval-zcp returned zero live services — cannot anchor the survivor case")
	}

	stateDir := t.TempDir()

	// Seed one meta whose hostname matches a real live service. It must
	// survive the prune. Pick the first live service deterministically.
	survivor := live[0].Name
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:       survivor,
		Mode:           topology.PlanModeDev,
		BootstrappedAt: "2026-04-25",
	}); err != nil {
		t.Fatalf("seed survivor meta: %v", err)
	}

	// Seed two metas whose hostnames cannot exist on eval-zcp. They must
	// be pruned. The "zcpe3" prefix is well outside any production naming
	// scheme and the e2e cleanup allowlist.
	orphans := []string{"zcpe3orphan1", "zcpe3orphan2"}
	for _, host := range orphans {
		if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
			Hostname:       host,
			Mode:           topology.PlanModeDev,
			BootstrappedAt: "2026-04-25",
		}); err != nil {
			t.Fatalf("seed orphan meta %s: %v", host, err)
		}
	}

	// Build the liveHostnames map exactly the way the bootstrap-start
	// helper does (`liveHostnamesMap` in internal/tools/workflow.go).
	liveByName := make(map[string]bool, len(live))
	for _, svc := range live {
		liveByName[svc.Name] = true
	}

	deleted := workflow.PruneServiceMetas(stateDir, liveByName)

	if !slices.Equal(deleted, orphans) {
		t.Errorf("deleted = %v, want %v (sorted)", deleted, orphans)
	}

	// Survivor must still exist on disk.
	got, err := workflow.ReadServiceMeta(stateDir, survivor)
	if err != nil {
		t.Fatalf("ReadServiceMeta(%s): %v", survivor, err)
	}
	if got == nil {
		t.Errorf("survivor meta %q was pruned; expected to remain (matches live service)", survivor)
	}

	// Orphan metas must be gone.
	for _, host := range orphans {
		got, err := workflow.ReadServiceMeta(stateDir, host)
		if err != nil {
			t.Fatalf("ReadServiceMeta(%s): %v", host, err)
		}
		if got != nil {
			t.Errorf("orphan meta %q still on disk after prune", host)
		}
	}
}
