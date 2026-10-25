// Tests for: ops/lease_group.go — an acquire records only a container whose
// alias and hostname the alias file can carry (docs/spec-workflows.md §8 DP-1).
package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestLeaseAcquire_ContainerHostnameOutsideTheShape_HandedBack: the alias file
// is root-read ssh config, so its render refuses the whole set when one
// record's hostname is not node-id-<n>.runtime.<pool>.zerops — a record of
// such a container would cost every lease its alias stanza until released.
// The acquire checks the hostname the orchestrator answered before it writes
// the record, hands the container straight back (clean, so no reset) and
// refuses, never echoing the hostname.
func TestLeaseAcquire_ContainerHostnameOutsideTheShape_HandedBack(t *testing.T) {
	t.Parallel()
	for name, hostname := range map[string]string{
		"empty":            "",
		"another shape":    "devpool-3.example.com",
		"config injection": "node-id-3.runtime.devpool.zerops\n    ProxyCommand evil",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			fx.swarm.acquired = acquiredAs(3)
			fx.swarm.acquired.Container.Hostname = hostname

			_, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature")

			var pe *platform.PlatformError
			if err == nil {
				t.Fatal("acquire succeeded with a container hostname outside node-id-<n>.runtime.<pool>.zerops")
			}
			if hostname != "" && strings.Contains(err.Error(), hostname) {
				t.Errorf("error %q echoes the hostname", err)
			}
			if errors.As(err, &pe) && hostname != "" && strings.Contains(pe.Suggestion, hostname) {
				t.Errorf("suggestion %q echoes the hostname", pe.Suggestion)
			}
			if len(fx.swarm.releases) != 1 || fx.swarm.releases[0].reset {
				t.Errorf("releases = %+v, want the container handed straight back, no reset", fx.swarm.releases)
			}
			if _, statErr := os.Stat(filepath.Join(fx.stateDir, "leases", "devpool-3.json")); !errors.Is(statErr, os.ErrNotExist) {
				t.Errorf("a record of the container was written (%v)", statErr)
			}
		})
	}
}
