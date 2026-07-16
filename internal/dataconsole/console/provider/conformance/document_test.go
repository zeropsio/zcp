//go:build e2e

package conformance

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// TestDocument_Smoke is the read-only smoke case for the document family
// (elasticsearch, meilisearch, typesense — full; qdrant — view-only but still
// readable): Health (via setupService) + List indices/collections + ReadBlob
// the first document in the first non-empty index. An index with zero
// documents skips-or-fails via the same profile/manifest gate as an
// unreachable engine, for the same reason as the kv case: nothing to read
// means the read path is not actually proven.
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

			ctx, cancel := context.WithTimeout(context.Background(), assertTimeout)
			defer cancel()

			containers, _, err := op.List(ctx, provider.Path{Service: entry.Hostname}, provider.Page{Limit: 100})
			if err != nil {
				t.Fatalf("List(indices): %v", err)
			}
			if len(containers) == 0 {
				skipOrFail(t, entry, "no indices/collections present")
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
				skipOrFail(t, entry, "no documents present in any index/collection — seed via cmd/dcseed")
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
