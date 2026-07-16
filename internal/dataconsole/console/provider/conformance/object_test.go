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
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider/object"
)

// TestObject_Conversions supersedes the 5 skips left in
// internal/dataconsole/console/provider/object/object_test.go pointing at
// "see e2e" (now this file):
//
//   - TestHealth_NeedsLiveMinIO                              → setupService's Health() gate.
//   - TestList_NeedsLiveMinIO                                 → List() finding the fixture below.
//   - TestStat_NeedsLiveMinIO                                 → Stat() on the fixture.
//   - TestReadBlob_NeedsLiveMinIO                             → ReadBlob() round-tripping the written bytes.
//   - TestWriteBlob_Delete_Rename_SuccessPaths_NeedLiveMinIO  → the Write→Rename→Delete chain below.
//
// Unlike the other families, this case is SELF-SEEDING: it writes its own
// fixture object, round-trips it through every read/mutate operation, and
// deletes it — so it proves the full read+write surface without depending on
// cmd/dcseed timing (object storage, unlike tabular/kv/document, has no
// always-present system data to fall back on).
func TestObject_Conversions(t *testing.T) {
	requireHarness(t)
	entries := activeConfig.ByFamily(provider.FamilyObject)
	if len(entries) == 0 {
		t.Skip("no object services in DC_LIVE_CONFIG")
	}
	for _, entry := range entries {
		t.Run(entry.Hostname, func(t *testing.T) {
			prov := setupService(t, entry, false) // ReadOnly=false — this case proves the write path too
			if prov == nil {
				return
			}
			defer func() { _ = prov.Close() }()
			op, ok := prov.(*object.Provider)
			if !ok {
				t.Fatalf("%s: provider %T is not *object.Provider (Rename is concrete-only, not on provider.ObjectProvider)", entry.Hostname, prov)
			}

			ctx, cancel := context.WithTimeout(context.Background(), assertTimeout)
			defer cancel()

			dir := provider.Path{Service: entry.Hostname, Segments: []string{"_conformance"}}
			name := fmt.Sprintf("probe-%d.txt", time.Now().UnixNano())
			src := provider.Path{Service: entry.Hostname, Segments: []string{"_conformance", name}}
			dst := provider.Path{Service: entry.Hostname, Segments: []string{"_conformance", name + ".renamed"}}
			payload := []byte("zcp data console conformance probe\n")

			// Best-effort cleanup even on a mid-test failure — never leave the
			// fixture behind in the shared testbed.
			defer func() {
				_ = op.Delete(context.Background(), src)
				_ = op.Delete(context.Background(), dst)
			}()

			// supersedes TestWriteBlob_Delete_Rename_SuccessPaths_NeedLiveMinIO (write half)
			if err := op.WriteBlob(ctx, src, payload, "text/plain"); err != nil {
				t.Fatalf("WriteBlob(%v): %v", src.Segments, err)
			}

			// supersedes TestList_NeedsLiveMinIO
			nodes, _, err := op.List(ctx, dir, provider.Page{Limit: 100})
			if err != nil {
				t.Fatalf("List(%v): %v", dir.Segments, err)
			}
			if !containsBlobNamed(nodes, name) {
				t.Fatalf("List(%v) did not include the fixture %q written above: %+v", dir.Segments, name, nodes)
			}

			// supersedes TestStat_NeedsLiveMinIO
			stat, err := op.Stat(ctx, src)
			if err != nil {
				t.Fatalf("Stat(%v): %v", src.Segments, err)
			}
			if stat.Meta == nil || stat.Meta.Size == nil || *stat.Meta.Size != int64(len(payload)) {
				t.Errorf("Stat(%v).Meta.Size = %v, want %d", src.Segments, stat.Meta, len(payload))
			}

			// supersedes TestReadBlob_NeedsLiveMinIO
			got, meta, err := op.ReadBlob(ctx, src)
			if err != nil {
				t.Fatalf("ReadBlob(%v): %v", src.Segments, err)
			}
			if !bytes.Equal(got, payload) {
				t.Errorf("ReadBlob(%v) = %q, want %q", src.Segments, got, payload)
			}
			if meta.Truncated {
				t.Errorf("ReadBlob(%v).Truncated = true for a %d-byte fixture", src.Segments, len(payload))
			}

			// supersedes TestWriteBlob_Delete_Rename_SuccessPaths_NeedLiveMinIO (rename half)
			if err := op.Rename(ctx, src, dst); err != nil {
				t.Fatalf("Rename(%v -> %v): %v", src.Segments, dst.Segments, err)
			}
			if _, err := op.Stat(ctx, src); !errors.Is(err, provider.ErrNotFound) {
				t.Errorf("Stat(%v) after Rename = %v, want ErrNotFound (old path must be gone)", src.Segments, err)
			}
			renamed, _, err := op.ReadBlob(ctx, dst)
			if err != nil {
				t.Fatalf("ReadBlob(%v) after Rename: %v", dst.Segments, err)
			}
			if !bytes.Equal(renamed, payload) {
				t.Errorf("ReadBlob(%v) after Rename = %q, want %q", dst.Segments, renamed, payload)
			}

			// supersedes TestWriteBlob_Delete_Rename_SuccessPaths_NeedLiveMinIO (delete half)
			if err := op.Delete(ctx, dst); err != nil {
				t.Fatalf("Delete(%v): %v", dst.Segments, err)
			}
			if _, err := op.Stat(ctx, dst); !errors.Is(err, provider.ErrNotFound) {
				t.Errorf("Stat(%v) after Delete = %v, want ErrNotFound", dst.Segments, err)
			}

			globalSummary.Record(entry.Hostname, string(provider.FamilyObject), outcomePass, "")
		})
	}
}

func containsBlobNamed(nodes []provider.Node, name string) bool {
	for _, n := range nodes {
		if n.Kind == provider.KindBlob && n.Name == name {
			return true
		}
	}
	return false
}
