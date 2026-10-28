// Tests for: tools/pool_part_origin.go — the origin of a pool-keyed part
// whose repository is in this container's HQ is read there, on main,
// through the Mate credential (docs/spec-workflows.md §10 P-LP-19).
package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestReadPoolPartOrigin_HQRepository_ReadOnMainThroughHQ: a pool-keyed pair
// wired to its repository in this container's HQ is read through HQ — on
// main, whatever the pair tracks — and never as a plain origin with the
// pool's GIT_TOKEN. A container whose HQ addresses need an organisation it
// does not have says so, naming HQ and main, before any fetch.
func TestReadPoolPartOrigin_HQRepository_ReadOnMainThroughHQ(t *testing.T) {
	t.Parallel()
	const hqAddr = "https://hq.invalid"
	stateDir := poolPartPair(t, hqAddr+"/git/org-1/app-1/app.git", "")
	if err := workflow.UpdateServiceMeta(stateDir, testPool, func(m *workflow.ServiceMeta) error {
		m.HQ = &workflow.HQRepoRef{AppID: "app-1", Repo: "app"}
		return nil
	}); err != nil {
		t.Fatalf("wire the pair to HQ: %v", err)
	}
	meta := poolDeliveryMeta(t, stateDir)
	rt := runtime.Info{InContainer: true, HQ: &runtime.HQLink{Origin: hqAddr, Key: "mate-key", ProjectID: "mate-1"}}

	origin, err := readPoolPartOrigin(context.Background(), poolPartOriginDeps{client: poolPartClient(), rt: rt, projectID: "proj-1"}, meta)

	if origin != nil {
		t.Fatalf("origin = %+v, want none from an HQ this container cannot address", origin)
	}
	var e *poolPartOriginError
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want a poolPartOriginError", err)
	}
	if e.ref != hqBase {
		t.Errorf("ref = %q, want HQ's %q", e.ref, hqBase)
	}
	for _, want := range []string{"HQ", "organisation", hqBase} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), poolPartGitToken) {
		t.Errorf("the error carries the pool's GIT_TOKEN: %v", err)
	}
}
