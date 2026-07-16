//go:build e2e

package conformance

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// TestDocument_Smoke is the read-only smoke case for the document family
// (elasticsearch, meilisearch, typesense — full; qdrant — view-only but
// still readable): Health (via setupService), seeds its own namespaced
// fixture (console/seed — S10b), then List indices/collections + ReadBlob
// the first document in the first non-empty index. Seeding first means an
// index with documents is guaranteed by this test itself rather than
// depending on cmd/dcseed having run out-of-band — nothing to read means
// the read path is not actually proven. The fixture is torn down
// (deferred) after the assertions.
func TestDocument_Smoke(t *testing.T) {
	requireHarness(t)
	entries := activeConfig.ByFamily(provider.FamilyDocument)
	if len(entries) == 0 {
		t.Skip("no document services in DC_LIVE_CONFIG")
	}
	for _, entry := range entries {
		t.Run(entry.Hostname, func(t *testing.T) {
			prov := setupService(t, entry, true)
			if prov == nil {
				return
			}
			defer func() { _ = prov.Close() }()
			op, ok := prov.(provider.ObjectProvider)
			if !ok {
				t.Fatalf("%s: provider %T does not implement ObjectProvider", entry.Hostname, prov)
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

			containers, _, err := op.List(ctx, provider.Path{Service: entry.Hostname}, provider.Page{Limit: 100})
			if err != nil {
				t.Fatalf("List(indices): %v", err)
			}
			if len(containers) == 0 {
				skipOrFail(t, entry, "no indices/collections present even after seeding")
				return
			}

			var docPath provider.Path
			found := false
			for _, c := range containers {
				docs, _, err := op.List(ctx, c.Path, provider.Page{Limit: 20})
				if err != nil {
					t.Fatalf("List(docs in %q): %v", c.Name, err)
				}
				if len(docs) > 0 {
					docPath = docs[0].Path
					found = true
					break
				}
			}
			if !found {
				skipOrFail(t, entry, "no documents present in any index/collection even after seeding")
				return
			}

			_, meta, err := op.ReadBlob(ctx, docPath)
			if err != nil {
				t.Fatalf("ReadBlob(%v): %v", docPath.Segments, err)
			}
			if meta.ContentType != "application/json" {
				t.Errorf("ReadBlob(%v).ContentType = %q, want application/json", docPath.Segments, meta.ContentType)
			}
			version := ""
			if entry.Document != nil {
				version = esVersion(ctx, entry.Document) // best-effort; only elasticsearch's root JSON is attempted, see version_test.go
			}
			t.Logf("%s: doc %v — %d bytes, server version = %s", entry.Hostname, docPath.Segments, meta.Size, logVersion(version))

			globalSummary.Record(entry.Hostname, string(provider.FamilyDocument), outcomePass, "")
		})
	}
}
