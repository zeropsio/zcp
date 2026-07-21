//go:build e2e

package conformance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

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
			prov := setupService(t, entry)
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
			if !page.Numbered {
				t.Fatalf("ReadTable(%v).Numbered = false, want server-declared relation pagination", tablePath.Segments)
			}
			for _, col := range page.Columns {
				if !col.Sortable {
					continue
				}
				if _, err := tp.ReadTable(ctx, tablePath, provider.Page{
					Limit: 10,
					Sort:  &provider.Sort{Column: col.Name, Direction: provider.SortDesc},
				}); err != nil {
					t.Fatalf("ReadTable(%v sort=%s desc): %v", tablePath.Segments, col.Name, err)
				}
				break
			}
			counter, ok := prov.(provider.TableCounter)
			if !ok {
				t.Fatalf("%s: provider %T does not implement TableCounter", entry.Hostname, prov)
			}
			count, err := counter.CountTable(ctx, tablePath)
			if err != nil {
				t.Fatalf("CountTable(%v): %v", tablePath.Segments, err)
			}
			if count < int64(len(page.Rows)) {
				t.Fatalf("CountTable(%v) = %d, smaller than readable page length %d", tablePath.Segments, count, len(page.Rows))
			}
			t.Logf("%s: table %v — %d columns, %d rows in this page, server version = %s",
				entry.Hostname, tablePath.Segments, len(page.Columns), len(page.Rows), logVersion(sqlVersion(ctx, tp)))

			recordSummary(t, entry.Hostname, string(provider.FamilyTabular))
		})
	}
}

// ---- full-engine write + fidelity (postgresql, mariadb) ----

// probeDB opens a raw database/sql connection to entry's tabular engine,
// independent of the tabular.Provider under test — this file needs to issue
// DDL (CREATE/DROP TABLE) that no TabularProvider method exposes (Query is a
// read-only transaction by contract; ReadTable/EditCell/InsertRow/DeleteRow
// only ever address an EXISTING table). DSN construction mirrors
// console/seed/tabular.go's seedPostgres/seedMySQL exactly; the pgx/mysql
// drivers are already registered by this package's transitive import of
// provider/tabular, so no additional driver import is needed here. schema is
// the identifier a Path.Segments[0] must carry to address a table this
// connection creates: "public" for postgres (seed/tabular.go's convention);
// the connected database name for mysql/mariadb (matching
// seed/tabular.go's cleanupMySQL, which scans information_schema.tables
// WHERE table_schema = DATABASE()).
func probeDB(t *testing.T, entry ServiceEntry) (db *sql.DB, schema string) {
	t.Helper()
	desc, err := entry.Descriptor()
	if err != nil {
		t.Fatalf("%s: descriptor: %v", entry.Hostname, err)
	}
	conn, ok := desc.(provider.SQLConn)
	if !ok {
		t.Fatalf("%s: descriptor %T is not a SQLConn", entry.Hostname, desc)
	}
	hostPort := net.JoinHostPort(conn.Host, conn.Port)
	switch provider.BaseType(entry.Type) {
	case "postgresql":
		dsn := fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable",
			url.QueryEscape(conn.User), url.QueryEscape(conn.Password), hostPort, conn.Database)
		if db, err = sql.Open("pgx", dsn); err != nil {
			t.Fatalf("%s: open: %v", entry.Hostname, err)
		}
		return db, "public"
	case "mariadb", "mysql":
		dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s", conn.User, conn.Password, hostPort, conn.Database)
		if db, err = sql.Open("mysql", dsn); err != nil {
			t.Fatalf("%s: open: %v", entry.Hostname, err)
		}
		return db, conn.Database
	default:
		t.Fatalf("%s: probeDB: unsupported base type %q", entry.Hostname, entry.Type)
		return nil, ""
	}
}

// clickHouseProbeDB opens a writable connection used only to create the
// aggregate-state fixtures below. The production provider remains a separate
// readonly=1 connection built by setupService, so the browse assertion still
// exercises the exact factory/provider path shipped by Data Console.
func clickHouseProbeDB(t *testing.T, entry ServiceEntry) (*sql.DB, string) {
	t.Helper()
	desc, err := entry.Descriptor()
	if err != nil {
		t.Fatalf("%s: descriptor: %v", entry.Hostname, err)
	}
	conn, ok := desc.(provider.SQLConn)
	if !ok {
		t.Fatalf("%s: descriptor %T is not a SQLConn", entry.Hostname, desc)
	}
	dsn := fmt.Sprintf("clickhouse://%s:%s@%s/%s",
		url.QueryEscape(conn.User), url.QueryEscape(conn.Password),
		net.JoinHostPort(conn.Host, conn.Port), conn.Database)
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		t.Fatalf("%s: open writable fixture connection: %v", entry.Hostname, err)
	}
	return db, conn.Database
}

// TestTabular_ClickHouseAggregateStateBrowse creates both relation shapes
// seen in telemetry databases: a direct AggregatingMergeTree and a
// MaterializedView whose physical columns contain AggregateFunction states.
// The fixtures are written through an independent connection, then browsed
// through the production readonly provider. This catches driver-level state
// decoding regressions that a fake database/sql driver cannot reproduce.
func TestTabular_ClickHouseAggregateStateBrowse(t *testing.T) {
	requireHarness(t)
	entries := activeConfig.ByFamily(provider.FamilyTabular)
	found := false
	for _, entry := range entries {
		if provider.BaseType(entry.Type) != "clickhouse" {
			continue
		}
		found = true
		t.Run(entry.Hostname, func(t *testing.T) {
			prov := setupService(t, entry)
			if prov == nil {
				return
			}
			defer func() { _ = prov.Close() }()
			tp, ok := prov.(provider.TabularProvider)
			if !ok {
				t.Fatalf("%s: provider %T does not implement TabularProvider", entry.Hostname, prov)
			}

			db, schema := clickHouseProbeDB(t, entry)
			defer func() { _ = db.Close() }()
			baseName := fmt.Sprintf("zcp_dc_conf_aggregate_%d", time.Now().UnixNano())
			directName := baseName + "_direct"
			sourceName := baseName + "_source"
			viewName := baseName + "_view"
			directEngine := "AggregatingMergeTree"
			sourceEngine := "MergeTree"
			insertSettings := ""
			var databaseEngine string
			engineCtx, engineCancel := context.WithTimeout(context.Background(), assertTimeout)
			err := db.QueryRowContext(engineCtx,
				"SELECT engine FROM system.databases WHERE name = ?", schema).Scan(&databaseEngine)
			engineCancel()
			if err != nil {
				t.Fatalf("clickhouse fixture database engine: %v", err)
			}
			if strings.EqualFold(databaseEngine, "Replicated") {
				// A Replicated database propagates DDL to every HA node, but its
				// table data is replicated only when the relation engine is too.
				// Quorum makes fixture setup complete before the one-shot semantic
				// assertion, without retrying the assertion through the load balancer.
				directEngine = "ReplicatedAggregatingMergeTree"
				sourceEngine = "ReplicatedMergeTree"
				insertSettings = " SETTINGS insert_quorum = 'auto'"
			}

			execFixture := func(stmt string) {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), assertTimeout)
				defer cancel()
				if _, err := db.ExecContext(ctx, stmt); err != nil {
					t.Fatalf("clickhouse aggregate fixture: %v", err)
				}
			}

			execFixture("CREATE TABLE " + directName + " (" +
				"dimension String, " +
				"exact AggregateFunction(sum, UInt64), " +
				"samples AggregateFunction(groupArray, UInt64)" +
				") ENGINE = " + directEngine + " ORDER BY dimension")
			defer dropProbeTable(db, directName)
			execFixture("INSERT INTO " + directName + insertSettings + " SELECT " +
				"'direct' AS dimension, " +
				"sumState(toUInt64(9007199254740993)) AS exact, " +
				"groupArrayState(toUInt64(number + 1)) AS samples " +
				"FROM numbers(2)")

			execFixture("CREATE TABLE " + sourceName + " (" +
				"dimension String, exact_input UInt64, sample UInt64" +
				") ENGINE = " + sourceEngine + " ORDER BY (dimension, sample)")
			defer dropProbeTable(db, sourceName)
			execFixture("CREATE MATERIALIZED VIEW " + viewName + " " +
				"ENGINE = " + directEngine + " ORDER BY dimension AS " +
				"SELECT dimension, sumState(exact_input) AS exact, " +
				"groupArrayState(sample) AS samples FROM " + sourceName + " GROUP BY dimension")
			defer dropProbeTable(db, viewName)
			execFixture("INSERT INTO " + sourceName + " (dimension, exact_input, sample)" + insertSettings + " VALUES " +
				"('view', 9007199254740993, 1), ('view', 9007199254740993, 2)")

			ctx, cancel := context.WithTimeout(context.Background(), assertTimeout)
			defer cancel()
			assertBrowse := func(relation, wantDimension string) {
				t.Helper()
				path := provider.Path{Service: entry.Hostname, Segments: []string{schema, relation}}
				page, err := tp.ReadTable(ctx, path, provider.Page{
					Limit: 10,
					Sort:  &provider.Sort{Column: "dimension", Direction: provider.SortDesc},
				})
				if err != nil {
					t.Fatalf("ReadTable(%s): %v", relation, err)
				}
				if len(page.Rows) != 1 {
					t.Fatalf("ReadTable(%s) rows = %d, want 1", relation, len(page.Rows))
				}
				dimensionIdx := colIndex(page.Columns, "dimension")
				exactIdx := colIndex(page.Columns, "exact")
				samplesIdx := colIndex(page.Columns, "samples")
				if dimensionIdx < 0 || exactIdx < 0 || samplesIdx < 0 {
					t.Fatalf("ReadTable(%s) columns = %+v, want dimension/exact/samples", relation, page.Columns)
				}
				row := page.Rows[0]
				if got := fmt.Sprint(row[dimensionIdx]); got != wantDimension {
					t.Errorf("ReadTable(%s) dimension = %q, want %q", relation, got, wantDimension)
				}
				if got := fmt.Sprint(row[exactIdx]); got != "18014398509481986" {
					t.Errorf("ReadTable(%s) exact = %q, want lossless >2^53 scalar", relation, got)
				}
				if got := fmt.Sprint(row[samplesIdx]); got != "[1,2]" {
					t.Errorf("ReadTable(%s) samples = %q, want exact finalized array %q", relation, got, "[1,2]")
				}
				for _, idx := range []int{exactIdx, samplesIdx} {
					col := page.Columns[idx]
					if !strings.HasPrefix(col.DataType, "AggregateFunction(") {
						t.Errorf("ReadTable(%s) %s type = %q, want original AggregateFunction metadata", relation, col.Name, col.DataType)
					}
					if col.Sortable || col.SortReason != "aggregate state" {
						t.Errorf("ReadTable(%s) aggregate column = %+v, want non-sortable aggregate state", relation, col)
					}
				}
				if _, err := tp.ReadTable(ctx, path, provider.Page{
					Limit: 10,
					Sort:  &provider.Sort{Column: "exact", Direction: provider.SortAsc},
				}); !errors.Is(err, provider.ErrInvalid) {
					t.Errorf("ReadTable(%s sort=exact) = %v, want ErrInvalid", relation, err)
				}
			}

			assertBrowse(directName, "direct")
			assertBrowse(viewName, "view")
			recordSummary(t, entry.Hostname, string(provider.FamilyTabular))
		})
	}
	if !found {
		t.Skip("no clickhouse service in DC_LIVE_CONFIG")
	}
}

// createProbeTable issues the one CREATE TABLE this file needs, dialect-aware
// (the same 4-column shape across postgres/mysql/mariadb: a generated PK for
// ProofInsertKeyEcho, a text column for ProofWriteRoundtrip, a bigint column
// for the ProofValueFidelity bigint check, and a timestamp column for the
// postgres-only sub-second-precision bonus check — see the doc comment on
// TestTabular_FullEngineWriteAndFidelity).
func createProbeTable(t *testing.T, db *sql.DB, base, name string) {
	t.Helper()
	var stmt string
	switch base {
	case "postgresql":
		stmt = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (id serial PRIMARY KEY, label text, big bigint, ts timestamptz)`, name)
	case "mariadb", "mysql":
		stmt = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (id INT AUTO_INCREMENT PRIMARY KEY, label VARCHAR(190), big BIGINT, ts DATETIME(6))`, name)
	default:
		t.Fatalf("createProbeTable: unsupported base type %q", base)
	}
	ctx, cancel := context.WithTimeout(context.Background(), assertTimeout)
	defer cancel()
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		t.Fatalf("create probe table %s: %v", name, err)
	}
}

// dropProbeTable is the idempotent teardown counterpart — safe to call even
// when the table was already dropped earlier in the same test (the
// ProofErrorParity step drops it ahead of this deferred safety net).
func dropProbeTable(db *sql.DB, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), assertTimeout)
	defer cancel()
	_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS "+name)
}

// colIndex returns the position of the column named name, or -1 if absent.
func colIndex(cols []provider.Column, name string) int {
	for i, c := range cols {
		if c.Name == name {
			return i
		}
	}
	return -1
}

// findRowByID re-reads tablePath in full and returns the columns + row whose
// "id" cell stringifies to id, so every assertion below re-resolves column
// position from a fresh read rather than caching a schema that could go
// stale mid-test.
func findRowByID(ctx context.Context, tp provider.TabularProvider, tablePath provider.Path, id any) (cols []provider.Column, row []any, found bool, err error) {
	page, err := tp.ReadTable(ctx, tablePath, provider.Page{Limit: 100})
	if err != nil {
		return nil, nil, false, err
	}
	idIdx := colIndex(page.Columns, "id")
	for _, r := range page.Rows {
		if fmt.Sprint(r[idIdx]) == fmt.Sprint(id) {
			return page.Columns, r, true, nil
		}
	}
	return page.Columns, nil, false, nil
}

// TestTabular_FullEngineWriteAndFidelity is the write-path conformance case
// for the full-tier tabular engines (postgresql, mariadb; mysql is proven by
// the mariadb cell via ProvenBy — see proofs.go). It self-manages its own
// scratch table (raw DDL via probeDB — no TabularProvider method can create
// one), independent of cmd/dcseed's static dataset or the seed package's
// namespaced fixtures (neither ships a table with a bigint/timestamp column
// this test needs for §7.3 value-fidelity). One flow proves six proofs:
//
//   - ProofInsertKeyEcho: 3x InsertRow, each echoing a generated "id" key.
//   - ProofPagination: a Limit:1 walk across the 3 rows just inserted.
//   - ProofWriteRoundtrip: EditCell a text column, re-read to confirm applied.
//   - ProofValueFidelity: EditCell a bigint column with a json.Number beyond
//     2^53 (simulating exactly what server.go's UseNumber() decode produces),
//     confirming bindArg's decimal-text bind round-trips exactly
//     (spec-dataconsole.md §7.3.1); postgres additionally gets a bonus
//     sub-second timestamp check (§7.3.2 — RFC3339Nano, not RFC3339).
//   - ProofDelete: DeleteRow the probe row, confirm it is genuinely gone.
//   - ProofErrorParity: drop the scratch table, confirm ReadTable now answers
//     ErrNotFound (§7.5 tabular: "a missing table → ErrNotFound").
func TestTabular_FullEngineWriteAndFidelity(t *testing.T) {
	requireHarness(t)
	entries := activeConfig.ByFamily(provider.FamilyTabular)
	for _, entry := range entries {
		if provider.SupportFor(entry.Type) != provider.SupportFull {
			continue // clickhouse (view-only) is proven by the dedicated view-only cases
		}
		t.Run(entry.Hostname, func(t *testing.T) {
			base := provider.BaseType(entry.Type)
			// setupService FIRST: its health probe routes an unreachable
			// engine through skipOrFail (partial-profile skip, full-profile
			// required fail) and records the ledger entry — a raw probeDB
			// dial before it would hard-fail a merely-unreachable engine and
			// leave no case record at all.
			prov := setupService(t, entry) // ReadOnly=false — this case proves the write path
			if prov == nil {
				return
			}
			defer func() { _ = prov.Close() }()

			db, schema := probeDB(t, entry)
			defer func() { _ = db.Close() }()
			tableName := fmt.Sprintf("zcp_dc_conf_%d", time.Now().UnixNano())
			createProbeTable(t, db, base, tableName)
			defer dropProbeTable(db, tableName)
			tp, ok := prov.(provider.TabularProvider)
			if !ok {
				t.Fatalf("%s: provider %T does not implement TabularProvider", entry.Hostname, prov)
			}

			ctx, cancel := context.WithTimeout(context.Background(), assertTimeout)
			defer cancel()
			tablePath := provider.Path{Service: entry.Hostname, Segments: []string{schema, tableName}}

			// ---- InsertRow x3 — ProofInsertKeyEcho ----
			var ids []any
			for i := 1; i <= 3; i++ {
				applied, err := tp.InsertRow(ctx, tablePath, map[string]any{"label": fmt.Sprintf("probe-%d", i)})
				if err != nil {
					t.Fatalf("InsertRow #%d: %v", i, err)
				}
				if applied.Key == nil || applied.Key["id"] == nil {
					t.Fatalf("InsertRow #%d: Applied.Key = %v, want a non-nil echoed \"id\" (spec-dataconsole.md §7.3.5)", i, applied.Key)
				}
				ids = append(ids, applied.Key["id"])
			}

			// ---- ProofPagination: walk the 3 rows one page at a time ----
			seenLabels := map[string]bool{}
			cursor := ""
			for page := 0; page < 5; page++ {
				tp2, err := tp.ReadTable(ctx, tablePath, provider.Page{Limit: 1, Cursor: cursor})
				if err != nil {
					t.Fatalf("ReadTable page %d: %v", page, err)
				}
				if len(tp2.Rows) != 1 {
					t.Fatalf("ReadTable page %d: got %d rows, want 1", page, len(tp2.Rows))
				}
				labelIdx := colIndex(tp2.Columns, "label")
				seenLabels[fmt.Sprint(tp2.Rows[0][labelIdx])] = true
				cursor = tp2.NextCursor
				if cursor == "" {
					break
				}
			}
			for i := 1; i <= 3; i++ {
				want := fmt.Sprintf("probe-%d", i)
				if !seenLabels[want] {
					t.Errorf("pagination walk never surfaced %q (saw %v)", want, seenLabels)
				}
			}

			// ---- ProofWriteRoundtrip: EditCell a text column ----
			id1 := ids[0]
			if _, err := tp.EditCell(ctx, provider.CellEdit{
				Path: tablePath, RowKey: map[string]any{"id": id1},
				Column: "label", NewValue: "probe-1-edited", ExpectedOld: "probe-1",
			}); err != nil {
				t.Fatalf("EditCell(label): %v", err)
			}
			if cols, row, found, err := findRowByID(ctx, tp, tablePath, id1); err != nil || !found {
				t.Fatalf("findRowByID after EditCell(label): found=%v err=%v", found, err)
			} else if got := fmt.Sprint(row[colIndex(cols, "label")]); got != "probe-1-edited" {
				t.Errorf("label after EditCell = %q, want %q (write not applied)", got, "probe-1-edited")
			}

			// ---- ProofValueFidelity: bigint beyond 2^53 via json.Number ----
			bigVal := json.Number("9007199254740993") // 2^53 + 1
			if _, err := tp.EditCell(ctx, provider.CellEdit{
				Path: tablePath, RowKey: map[string]any{"id": id1},
				Column: "big", NewValue: bigVal, ExpectedOld: nil,
			}); err != nil {
				t.Fatalf("EditCell(big): %v", err)
			}
			if cols, row, found, err := findRowByID(ctx, tp, tablePath, id1); err != nil || !found {
				t.Fatalf("findRowByID after EditCell(big): found=%v err=%v", found, err)
			} else if got := fmt.Sprint(row[colIndex(cols, "big")]); got != string(bigVal) {
				t.Errorf("big column after EditCell = %q, want exact decimal text %q (spec-dataconsole.md §7.3.1: bindArg/json.Number fidelity)", got, string(bigVal))
			}

			// ---- ProofValueFidelity bonus: sub-second timestamp precision (postgres only —
			// mariadb/mysql's DSN carries no parseTime flag in production, so its
			// driver never reaches the time.Time branch this specifically proves) ----
			if base == "postgresql" {
				ts := time.Date(2026, 7, 17, 10, 20, 30, 123456000, time.UTC)
				if _, err := tp.EditCell(ctx, provider.CellEdit{
					Path: tablePath, RowKey: map[string]any{"id": id1},
					Column: "ts", NewValue: ts, ExpectedOld: nil,
				}); err != nil {
					t.Fatalf("EditCell(ts): %v", err)
				}
				cols, row, found, err := findRowByID(ctx, tp, tablePath, id1)
				if err != nil || !found {
					t.Fatalf("findRowByID after EditCell(ts): found=%v err=%v", found, err)
				}
				gotTS := fmt.Sprint(row[colIndex(cols, "ts")])
				parsed, perr := time.Parse(time.RFC3339Nano, gotTS)
				if perr != nil {
					t.Fatalf("ts column %q did not parse as RFC3339Nano: %v", gotTS, perr)
				}
				if parsed.Nanosecond() != ts.Nanosecond() {
					t.Errorf("ts sub-second precision lost: got %q (nanosecond=%d), want nanosecond=%d (spec-dataconsole.md §7.3.2: normalize()'s RFC3339Nano fix)",
						gotTS, parsed.Nanosecond(), ts.Nanosecond())
				}
			}

			// ---- ProofDelete ----
			applied, err := tp.DeleteRow(ctx, tablePath, map[string]any{"id": id1})
			if err != nil {
				t.Fatalf("DeleteRow: %v", err)
			}
			if applied.Affected != 1 {
				t.Errorf("DeleteRow Affected = %d, want 1", applied.Affected)
			}
			if _, _, found, err := findRowByID(ctx, tp, tablePath, id1); err != nil {
				t.Fatalf("findRowByID after DeleteRow: %v", err)
			} else if found {
				t.Errorf("row id=%v still present after DeleteRow", id1)
			}

			// ---- ProofErrorParity: drop the table, confirm ErrNotFound ----
			dropProbeTable(db, tableName) // ahead of the deferred safety-net drop (idempotent)
			if _, err := tp.ReadTable(ctx, tablePath, provider.Page{Limit: 10}); !errors.Is(err, provider.ErrNotFound) {
				t.Errorf("ReadTable(dropped table) = %v, want ErrNotFound (spec-dataconsole.md §7.5 tabular: a missing table is ErrNotFound)", err)
			}

			recordSummary(t, entry.Hostname, string(provider.FamilyTabular))
		})
	}
}

// TestTabular_QueryReadOnly proves ProofQueryReadOnly across EVERY tabular
// base type (full and view-only alike — Query is unconditionally available
// per provider.DerivedCaps): a plain read succeeds, and a write-shaped
// statement issued through the SAME arbitrary-SQL path is refused by the
// ENGINE (a READ ONLY transaction for postgres/mysql, the readonly=1
// connection setting for clickhouse) — never a client-side statement-text
// whitelist. The rejection is ErrInvalid per spec-dataconsole.md §7.1 I-2:
// the engine evaluated and rejected the operand, which is not an outage.
func TestTabular_QueryReadOnly(t *testing.T) {
	requireHarness(t)
	entries := activeConfig.ByFamily(provider.FamilyTabular)
	for _, entry := range entries {
		t.Run(entry.Hostname, func(t *testing.T) {
			prov := setupService(t, entry) // armed writes — proves the ENGINE refuses, not a caller-side switch
			if prov == nil {
				return
			}
			defer func() { _ = prov.Close() }()
			tp, ok := prov.(provider.TabularProvider)
			if !ok {
				t.Fatalf("%s: provider %T does not implement TabularProvider", entry.Hostname, prov)
			}
			ctx, cancel := context.WithTimeout(context.Background(), assertTimeout)
			defer cancel()

			if _, err := tp.Query(ctx, "SELECT 1", provider.Page{Limit: 1}); err != nil {
				t.Fatalf("Query(read): %v", err)
			}

			probeName := fmt.Sprintf("zcp_dc_conf_ro_%d", time.Now().UnixNano())
			ddl := "CREATE TABLE " + probeName + " (x int)"
			if provider.BaseType(entry.Type) == "clickhouse" {
				ddl = "CREATE TABLE " + probeName + " (x Int32) ENGINE = Memory"
			}
			_, err := tp.Query(ctx, ddl, provider.Page{Limit: 1})
			if err == nil {
				t.Errorf("Query(write-shaped DDL) succeeded — the engine-enforced read-only guard did not refuse it")
			} else if !errors.Is(err, provider.ErrInvalid) {
				t.Errorf("Query(write-shaped DDL) = %v, want ErrInvalid (spec-dataconsole.md §7.1 I-2: the engine rejecting the operand, not an outage)", err)
			}

			recordSummary(t, entry.Hostname, string(provider.FamilyTabular))
		})
	}
}

// TestTabular_MutationRefusal_ClickHouse proves ProofMutationRefusal for the
// tabular family's one view-only engine: EditCell on a live, armed-writes
// clickhouse provider still answers ErrReadOnly — tabular.New forces NoEdit
// for the clickhouse dialect (tabular.go's normalizeConfig), so the
// PROVIDER's own constructed posture wins regardless of the caller's
// requested write flag. The read-only gate fires before any query executes
// (tabular.go's EditCell checks p.caps.ReadOnly first), so this proves the
// posture of the real, live-connected provider object without needing an
// actual table/row to edit.
func TestTabular_MutationRefusal_ClickHouse(t *testing.T) {
	requireHarness(t)
	entries := activeConfig.ByFamily(provider.FamilyTabular)
	for _, entry := range entries {
		if provider.BaseType(entry.Type) != "clickhouse" {
			continue
		}
		t.Run(entry.Hostname, func(t *testing.T) {
			prov := setupService(t, entry) // armed writes requested — clickhouse's own posture must still win
			if prov == nil {
				return
			}
			defer func() { _ = prov.Close() }()
			tp, ok := prov.(provider.TabularProvider)
			if !ok {
				t.Fatalf("%s: provider %T does not implement TabularProvider", entry.Hostname, prov)
			}
			ctx, cancel := context.WithTimeout(context.Background(), assertTimeout)
			defer cancel()

			_, err := tp.EditCell(ctx, provider.CellEdit{
				Path:   provider.Path{Service: entry.Hostname, Segments: []string{"default", "does_not_matter"}},
				RowKey: map[string]any{"id": 0},
				Column: "name", NewValue: "should-be-refused",
			})
			if !errors.Is(err, provider.ErrReadOnly) {
				t.Errorf("EditCell on a view-only (clickhouse) engine = %v, want ErrReadOnly (the provider's constructed posture, not the caller's requested write flag)", err)
			}

			recordSummary(t, entry.Hostname, string(provider.FamilyTabular))
		})
	}
}
