//go:build e2e

package conformance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider/document"
	"github.com/zeropsio/zcp/internal/dataconsole/console/seed"
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

// TestDocument_WriteRoundtrip is the write-path conformance case for the
// document family's full-tier engines (elasticsearch, meilisearch,
// typesense). It seeds the namespaced fixture (console/seed — guarantees
// the container exists with the right shape, notably typesense's
// schema-defined "products" collection) and, within it, creates its own
// dedicated probe document rather than mutating the seeded ones, proving
// five proofs:
//
//   - ProofWriteRoundtrip: CreateDoc, then ReadBlob confirms it is applied.
//   - ProofTaskConfirmedWrite (meilisearch only): CreateDoc's write reuses
//     putDoc, which polls meilisearch's async task queue to a terminal
//     status before returning (I-1, DOC-AUD-01) — the immediate,
//     no-retry ReadBlob right after is what proves the write wasn't merely
//     enqueued.
//   - ProofCreateCollision: creating the SAME id again is refused with
//     ErrConflict (I-4), never a clobber.
//   - ProofSearch: a bounded read-only search over the seeded fixture
//     content (not the probe doc, so this proof does not depend on the
//     search engine's write-visibility latency for a just-created id).
//   - ProofDelete: Delete the probe doc, ReadBlob confirms ErrNotFound.
func TestDocument_WriteRoundtrip(t *testing.T) {
	requireHarness(t)
	entries := activeConfig.ByFamily(provider.FamilyDocument)
	for _, entry := range entries {
		if provider.SupportFor(entry.Type) != provider.SupportFull {
			continue // qdrant (view-only) is proven by TestDocument_ViewOnlyQdrant
		}
		t.Run(entry.Hostname, func(t *testing.T) {
			prov := setupService(t, entry, false) // ReadOnly=false — this case proves the write path
			if prov == nil {
				return
			}
			defer func() { _ = prov.Close() }()
			op, ok := prov.(*document.Provider)
			if !ok {
				t.Fatalf("%s: provider %T is not *document.Provider (Search/CreateDoc are concrete-only, not on provider.ObjectProvider)", entry.Hostname, prov)
			}

			desc, err := entry.Descriptor()
			if err != nil {
				t.Fatalf("%s: descriptor: %v", entry.Hostname, err)
			}
			cleanupFixture := seedNamespacedFixture(t, entry, desc)
			if cleanupFixture == nil {
				return
			}
			defer cleanupFixture()

			ctx, cancel := context.WithTimeout(context.Background(), assertTimeout)
			defer cancel()

			container := seed.DocumentName(activeNamespace, "products")
			id := fmt.Sprintf("conf-probe-%d", time.Now().UnixNano())
			body := []byte(fmt.Sprintf(`{"id":%q,"title":"conformance probe","price":1.5}`, id))
			docPath := provider.Path{Service: entry.Hostname, Segments: []string{container, id}}
			defer func() { _ = op.Delete(context.Background(), docPath) }()

			gotID, err := op.CreateDoc(ctx, docPath, body)
			if err != nil {
				t.Fatalf("CreateDoc: %v", err)
			}
			if gotID != id {
				t.Errorf("CreateDoc returned id %q, want %q", gotID, id)
			}

			// ---- ProofWriteRoundtrip (+ ProofTaskConfirmedWrite for meilisearch) ----
			raw, meta, err := op.ReadBlob(ctx, docPath)
			if err != nil {
				t.Fatalf("ReadBlob: %v", err)
			}
			if meta.ContentType != "application/json" {
				t.Errorf("ReadBlob.ContentType = %q, want application/json", meta.ContentType)
			}
			if !bytes.Contains(raw, []byte("conformance probe")) {
				t.Errorf("ReadBlob after CreateDoc = %s, want it to contain the written title", raw)
			}

			// ---- ProofCreateCollision ----
			if _, err := op.CreateDoc(ctx, docPath, body); !errors.Is(err, provider.ErrConflict) {
				t.Errorf("CreateDoc(collision) = %v, want ErrConflict (I-4: create never clobbers)", err)
			}

			// ---- ProofSearch: bounded read-only search over the seeded fixture ----
			nodes, _, err := op.Search(ctx, provider.Path{Service: entry.Hostname, Segments: []string{container}}, "Widget", provider.Page{Limit: 10})
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if len(nodes) == 0 {
				t.Errorf("Search(%q) in %q returned 0 results, want the seeded \"Widget\" product to match", "Widget", container)
			}

			// ---- ProofDelete ----
			if err := op.Delete(ctx, docPath); err != nil {
				t.Fatalf("Delete: %v", err)
			}
			if _, _, err := op.ReadBlob(ctx, docPath); !errors.Is(err, provider.ErrNotFound) {
				t.Errorf("ReadBlob after Delete = %v, want ErrNotFound", err)
			}

			globalSummary.Record(entry.Hostname, string(provider.FamilyDocument), outcomePass, "")
		})
	}
}

// TestDocument_ViewOnlyQdrant proves the document family's one view-only
// engine's two distinguishing facts: a point read carries BlobMeta.Vector
// (ProofVectorMeta, UI-AUD-03) and every mutation refuses with ErrReadOnly
// even under armed writes (ProofMutationRefusal) — document.New forces
// qdrant read-only unconditionally (vectors are not human-editable in v1),
// so the PROVIDER's own constructed posture wins regardless of the caller's
// requested write flag.
func TestDocument_ViewOnlyQdrant(t *testing.T) {
	requireHarness(t)
	entries := activeConfig.ByFamily(provider.FamilyDocument)
	for _, entry := range entries {
		if provider.SupportFor(entry.Type) != provider.SupportViewOnly {
			continue
		}
		t.Run(entry.Hostname, func(t *testing.T) {
			prov := setupService(t, entry, false) // armed writes requested — qdrant's own posture must still win
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
				t.Fatalf("%s: descriptor: %v", entry.Hostname, err)
			}
			cleanupFixture := seedNamespacedFixture(t, entry, desc)
			if cleanupFixture == nil {
				return
			}
			defer cleanupFixture()

			ctx, cancel := context.WithTimeout(context.Background(), assertTimeout)
			defer cancel()

			container := seed.DocumentName(activeNamespace, "items")
			points, _, err := op.List(ctx, provider.Path{Service: entry.Hostname, Segments: []string{container}}, provider.Page{Limit: 10})
			if err != nil {
				t.Fatalf("List(points): %v", err)
			}
			if len(points) == 0 {
				skipOrFail(t, entry, "no points present in the seeded qdrant collection even after seeding")
				return
			}
			pointPath := points[0].Path

			// ---- ProofVectorMeta ----
			_, meta, err := op.ReadBlob(ctx, pointPath)
			if err != nil {
				t.Fatalf("ReadBlob(%v): %v", pointPath.Segments, err)
			}
			if !meta.Vector {
				t.Errorf("ReadBlob(%v).Vector = false, want true (a qdrant point must be flagged so the SPA collapses the embedding — UI-AUD-03)", pointPath.Segments)
			}

			// ---- ProofMutationRefusal ----
			if err := op.WriteBlob(ctx, pointPath, []byte(`{}`), ""); !errors.Is(err, provider.ErrReadOnly) {
				t.Errorf("WriteBlob on qdrant (view-only) = %v, want ErrReadOnly (the provider's constructed posture, not the caller's requested write flag)", err)
			}

			globalSummary.Record(entry.Hostname, string(provider.FamilyDocument), outcomePass, "")
		})
	}
}
