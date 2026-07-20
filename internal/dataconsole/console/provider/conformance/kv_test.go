//go:build e2e

package conformance

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

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
			prov := setupService(t, entry)
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

			recordSummary(t, entry.Hostname, string(provider.FamilyKV))
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

// TestKV_WriteRoundtrip is the write-path conformance case for the kv
// family's one full-tier engine (valkey). It self-seeds two dedicated probe
// keys under a "_conformance_<ts>" prefix (independent of the seed
// package's namespaced fixture — no existing fixture carries an unset-TTL
// string value or a collection this test can safely collide-test against),
// proving five proofs:
//
//   - ProofWriteRoundtrip: WriteBlob a string value, ReadBlob confirms it.
//   - ProofValueFidelity: the no-TTL sentinel — a key with no TTL set
//     reports meta.TTLSeconds == nil, never the literal 0 (spec-
//     dataconsole.md §7.3.4).
//   - ProofTTL: SetTTL, re-read confirms ttlSeconds reflects it.
//   - ProofDelete: Delete the string key, ReadBlob confirms ErrNotFound.
//   - ProofCreateCollision: CreateKey twice at the same name — the second
//     call is refused with ErrConflict (I-4), never a clobber.
//   - ProofWrongTypeGuard: WriteBlob (string) over the just-created hash
//     collection is refused with ErrWrongType (KV-AUD-01), never a silent
//     overwrite.
func TestKV_WriteRoundtrip(t *testing.T) {
	requireHarness(t)
	entries := activeConfig.ByFamily(provider.FamilyKV)
	for _, entry := range entries {
		if provider.SupportFor(entry.Type) != provider.SupportFull {
			continue
		}
		t.Run(entry.Hostname, func(t *testing.T) {
			prov := setupService(t, entry) // ReadOnly=false — this case proves the write path
			if prov == nil {
				return
			}
			defer func() { _ = prov.Close() }()
			kvp, ok := prov.(provider.KVProvider)
			if !ok {
				t.Fatalf("%s: provider %T does not implement KVProvider", entry.Hostname, prov)
			}

			ctx, cancel := context.WithTimeout(context.Background(), assertTimeout)
			defer cancel()

			base := fmt.Sprintf("_conformance_%d", time.Now().UnixNano())
			strKey := provider.Path{Service: entry.Hostname, Segments: []string{base, "str"}}
			collKey := provider.Path{Service: entry.Hostname, Segments: []string{base, "coll"}}
			defer func() {
				_ = kvp.Delete(context.Background(), strKey)
				_ = kvp.Delete(context.Background(), collKey)
			}()

			// ---- ProofWriteRoundtrip ----
			if err := kvp.WriteBlob(ctx, strKey, []byte("hello-conformance"), ""); err != nil {
				t.Fatalf("WriteBlob: %v", err)
			}
			got, meta, err := kvp.ReadBlob(ctx, strKey)
			if err != nil {
				t.Fatalf("ReadBlob: %v", err)
			}
			if string(got) != "hello-conformance" {
				t.Errorf("ReadBlob = %q, want %q", got, "hello-conformance")
			}

			// ---- ProofDownloadContent: full exact string bytes ----
			dl, ok := prov.(provider.BlobDownloader)
			if !ok {
				t.Fatalf("%s: provider %T does not implement BlobDownloader", entry.Hostname, prov)
			}
			download, downloadMeta, err := dl.DownloadBlob(ctx, strKey)
			if err != nil {
				t.Fatalf("DownloadBlob: %v", err)
			}
			downloaded, readErr := io.ReadAll(download)
			closeErr := download.Close()
			if readErr != nil {
				t.Fatalf("read DownloadBlob: %v", readErr)
			}
			if closeErr != nil {
				t.Fatalf("close DownloadBlob: %v", closeErr)
			}
			if string(downloaded) != "hello-conformance" {
				t.Errorf("DownloadBlob = %q, want hello-conformance", downloaded)
			}
			if downloadMeta.Size != int64(len(downloaded)) || downloadMeta.ContentType != "text/plain" || downloadMeta.Filename != "str" {
				t.Errorf("DownloadBlob metadata = %+v, want exact text/plain str metadata", downloadMeta)
			}

			// ---- ProofValueFidelity: no-TTL sentinel is nil, never 0 ----
			if meta.TTLSeconds != nil {
				t.Errorf("ReadBlob.TTLSeconds = %d for a never-TTL'd key, want nil sentinel (spec-dataconsole.md §7.3.4)", *meta.TTLSeconds)
			}

			// ---- ProofTTL ----
			var secs int64 = 120
			if err := kvp.SetTTL(ctx, strKey, &secs); err != nil {
				t.Fatalf("SetTTL: %v", err)
			}
			_, meta2, err := kvp.ReadBlob(ctx, strKey)
			if err != nil {
				t.Fatalf("ReadBlob after SetTTL: %v", err)
			}
			if meta2.TTLSeconds == nil || *meta2.TTLSeconds <= 0 || *meta2.TTLSeconds > secs {
				t.Errorf("ReadBlob.TTLSeconds after SetTTL(%d) = %v, want a positive value <= %d", secs, meta2.TTLSeconds, secs)
			}

			// ---- ProofDelete ----
			if err := kvp.Delete(ctx, strKey); err != nil {
				t.Fatalf("Delete: %v", err)
			}
			if _, _, err := kvp.ReadBlob(ctx, strKey); !errors.Is(err, provider.ErrNotFound) {
				t.Errorf("ReadBlob after Delete = %v, want ErrNotFound", err)
			}

			// ---- ProofCreateCollision ----
			if _, err := kvp.CreateKey(ctx, provider.KVCreate{Path: collKey, Type: "hash", Field: "f1", Value: []byte("v1")}); err != nil {
				t.Fatalf("CreateKey(first): %v", err)
			}
			if _, err := kvp.CreateKey(ctx, provider.KVCreate{Path: collKey, Type: "hash", Field: "f2", Value: []byte("v2")}); !errors.Is(err, provider.ErrConflict) {
				t.Errorf("CreateKey(collision) = %v, want ErrConflict (I-4: create never clobbers)", err)
			}

			// ---- ProofWrongTypeGuard ----
			if err := kvp.WriteBlob(ctx, collKey, []byte("oops"), ""); !errors.Is(err, provider.ErrWrongType) {
				t.Errorf("WriteBlob(string) over a hash collection = %v, want ErrWrongType (KV-AUD-01)", err)
			}

			recordSummary(t, entry.Hostname, string(provider.FamilyKV))
		})
	}
}
