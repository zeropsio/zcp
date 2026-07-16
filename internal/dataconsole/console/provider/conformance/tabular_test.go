//go:build e2e

package conformance

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// TestTabular_Smoke is the read-only smoke case for the tabular family
// (postgresql, mariadb, mysql, clickhouse): Health (via setupService) + List
// schemas + ReadTable the first table found. It walks schemas until it finds
// one with at least one table — a fresh, unseeded database still has
// information_schema/pg_catalog (or their dialect equivalents), so this case
// does not depend on cmd/dcseed having run.
func TestTabular_Smoke(t *testing.T) {
	requireHarness(t)
	entries := activeConfig.ByFamily(provider.FamilyTabular)
	if len(entries) == 0 {
		t.Skip("no tabular services in DC_LIVE_CONFIG")
	}
	for _, entry := range entries {
		t.Run(entry.Hostname, func(t *testing.T) {
			prov := setupService(t, entry, true)
			if prov == nil {
				return // already skipped/failed by setupService
			}
			defer func() { _ = prov.Close() }()
			tp, ok := prov.(provider.TabularProvider)
			if !ok {
				t.Fatalf("%s: provider %T does not implement TabularProvider", entry.Hostname, prov)
			}

			ctx, cancel := context.WithTimeout(context.Background(), assertTimeout)
			defer cancel()

			schemas, _, err := tp.List(ctx, provider.Path{Service: entry.Hostname}, provider.Page{Limit: 100})
			if err != nil {
				t.Fatalf("List(schemas): %v", err)
			}
			if len(schemas) == 0 {
				t.Fatalf("List(schemas): 0 schemas returned")
			}

			var tablePath provider.Path
			found := false
			for _, s := range schemas {
				tables, _, err := tp.List(ctx, s.Path, provider.Page{Limit: 50})
				if err != nil {
					t.Fatalf("List(tables in %q): %v", s.Name, err)
				}
				if len(tables) > 0 {
					tablePath = tables[0].Path
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("no tables found in any of %d schemas", len(schemas))
			}

			page, err := tp.ReadTable(ctx, tablePath, provider.Page{Limit: 10})
			if err != nil {
				t.Fatalf("ReadTable(%v): %v", tablePath.Segments, err)
			}
			t.Logf("%s: table %v — %d columns, %d rows in this page, server version = %s",
				entry.Hostname, tablePath.Segments, len(page.Columns), len(page.Rows), logVersion(sqlVersion(ctx, tp)))

			globalSummary.Record(entry.Hostname, string(provider.FamilyTabular), outcomePass, "")
		})
	}
}
