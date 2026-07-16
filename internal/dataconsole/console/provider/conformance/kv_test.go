//go:build e2e

package conformance

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// TestKV_Smoke is the read-only smoke case for the kv family (valkey):
// Health (via setupService), seeds its own namespaced fixture (console/seed
// — S10b), then List the keyspace + ReadBlob (string value) or ReadTable
// (hash/list/set/zset) on the first leaf found, up to two keyspace levels
// deep. Seeding first means the keyspace is guaranteed non-empty by this
// test itself rather than depending on cmd/dcseed having run out-of-band —
// a "smoke" case with no data to read has not actually proven the read
// path. The fixture is torn down (deferred) after the assertions.
func TestKV_Smoke(t *testing.T) {
	requireHarness(t)
	entries := activeConfig.ByFamily(provider.FamilyKV)
	if len(entries) == 0 {
		t.Skip("no kv services in DC_LIVE_CONFIG")
	}
	for _, entry := range entries {
		t.Run(entry.Hostname, func(t *testing.T) {
			prov := setupService(t, entry, true)
			if prov == nil {
				return
			}
			defer func() { _ = prov.Close() }()
			kvp, ok := prov.(provider.KVProvider)
			if !ok {
				t.Fatalf("%s: provider %T does not implement KVProvider", entry.Hostname, prov)
			}

			desc, err := entry.Descriptor()
			if err != nil {
				t.Fatalf("%s: descriptor: %v", entry.Hostname, err) // already validated at config load
			}
			cleanupFixture := seedNamespacedFixture(t, entry, desc)
			if cleanupFixture == nil {
				return // seed failed — already skipped/failed by seedNamespacedFixture
			}
			defer cleanupFixture()

			ctx, cancel := context.WithTimeout(context.Background(), assertTimeout)
			defer cancel()

			leaf, err := firstLeaf(ctx, kvp, provider.Path{Service: entry.Hostname})
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if leaf == nil {
				skipOrFail(t, entry, "no readable key found within 2 keyspace levels even after seeding")
				return
			}

			if leaf.Kind == provider.KindBlob {
				_, meta, err := kvp.ReadBlob(ctx, leaf.Path)
				if err != nil {
					t.Fatalf("ReadBlob(%v): %v", leaf.Path.Segments, err)
				}
				t.Logf("%s: string key %v — %d bytes", entry.Hostname, leaf.Path.Segments, meta.Size)
			} else {
				page, err := kvp.ReadTable(ctx, leaf.Path, provider.Page{Limit: 10})
				if err != nil {
					t.Fatalf("ReadTable(%v): %v", leaf.Path.Segments, err)
				}
				t.Logf("%s: collection key %v — %d columns, %d rows in this page", entry.Hostname, leaf.Path.Segments, len(page.Columns), len(page.Rows))
			}
			t.Logf("%s: server version = %s", entry.Hostname, logVersion(kvVersion(ctx, entry.KV)))

			globalSummary.Record(entry.Hostname, string(provider.FamilyKV), outcomePass, "")
		})
	}
}

// firstLeaf finds the first non-container node under root, descending into
// the first container one level if the top level holds only containers
// (valkey's ':'-grouped virtual namespacing).
func firstLeaf(ctx context.Context, kvp provider.KVProvider, root provider.Path) (*provider.Node, error) {
	nodes, _, err := kvp.List(ctx, root, provider.Page{Limit: 50})
	if err != nil {
		return nil, err
	}
	if leaf := firstNonContainer(nodes); leaf != nil {
		return leaf, nil
	}
	for _, n := range nodes {
		if n.Kind != provider.KindContainer {
			continue
		}
		deeper, _, err := kvp.List(ctx, n.Path, provider.Page{Limit: 50})
		if err != nil {
			return nil, err
		}
		if leaf := firstNonContainer(deeper); leaf != nil {
			return leaf, nil
		}
	}
	return nil, nil
}

func firstNonContainer(nodes []provider.Node) *provider.Node {
	for i := range nodes {
		if nodes[i].Kind != provider.KindContainer {
			return &nodes[i]
		}
	}
	return nil
}
