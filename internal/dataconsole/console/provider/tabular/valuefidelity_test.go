package tabular

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// This file pins DD-9's value-fidelity contract as applied to the tabular
// provider (S28): T-AUD-01 (timestamp sub-second precision), T-AUD-02
// (bigint string transport end-to-end, incl. PK/rowKey/expectedOld), and
// T-AUD-03 (insertRow echoes the new row's key). Evidence:
// plans/dataconsole-audit/tabular.md.

// ---- T-AUD-01: timestamp full precision ----

// TestNormalize_TimestampPreservesSubSecondPrecision pins T-AUD-01: normalize()
// used to format a time.Time via plain RFC3339 (no fractional seconds), so
// the value handed back to a caller never matched a microsecond-precision
// timestamptz/DATETIME column. Using that truncated value as a subsequent
// editCell's expectedOld spuriously 409s — the exact live reproduction in
// tabular.md §2 (inserted row's true value 2026-07-16 15:52:00.934297+00;
// the API's own read returned "...T17:52:00+02:00", zero fractional
// seconds).
func TestNormalize_TimestampPreservesSubSecondPrecision(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 7, 16, 15, 52, 0, 934297000, time.UTC)
	got := normalize(ts)
	s, ok := got.(string)
	if !ok {
		t.Fatalf("normalize(time.Time) = %T, want string", got)
	}
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatalf("normalize output %q is not RFC3339Nano-parseable: %v", s, err)
	}
	if !parsed.Equal(ts) {
		t.Fatalf("normalize(%v) round-tripped to %v — sub-second precision lost (T-AUD-01)", ts, parsed)
	}
}

// TestNormalize_WholeSecondTimestamp_NoSpuriousFraction guards the other
// direction: a whole-second time.Time must not sprout a fake ".000000000"
// tail — RFC3339Nano already elides an all-zero fractional part, but this
// pins that behavior explicitly since it is load-bearing (a naive fix could
// otherwise always append a fixed-width fraction).
func TestNormalize_WholeSecondTimestamp_NoSpuriousFraction(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 7, 16, 15, 52, 0, 0, time.UTC)
	got, ok := normalize(ts).(string)
	if !ok {
		t.Fatalf("normalize(time.Time) = %T, want string", normalize(ts))
	}
	if strings.Contains(got, ".") {
		t.Errorf("normalize(whole-second time) = %q, want no fractional component", got)
	}
}

// ---- vfDriver: a fake database/sql driver for T-AUD-02/T-AUD-03 ----
//
// It answers the small set of metadata (pk/columns) + DML (INSERT/UPDATE)
// queries EditCell/InsertRow/DeleteRow issue, and records every bound arg it
// receives — proving the EXACT value/type that reaches the driver layer,
// which is what T-AUD-02 is about: a bigint-range value must never pass
// through a float64 intermediate on its way from decoded JSON to the bound
// SQL parameter.

type vfCall struct {
	query string
	args  []driver.NamedValue
}

type vfDriver struct {
	mu sync.Mutex

	calls *[]vfCall

	// pkCols/cols answer columnsAndPK's two metadata queries (pk names;
	// name+type pairs). Both dialects' SQL text is matched generically (see
	// vfConn.QueryContext), so the same driver instance serves pg- and
	// mysql-dialect provider instances alike.
	pkCols []string
	cols   [][2]string

	// returning answers an "INSERT ... RETURNING" query (postgres key-echo
	// path); one row of pk-column values.
	returning [][]driver.Value

	// lastInsertID answers ExecContext's driver.Result.LastInsertId (mysql
	// key-echo path).
	lastInsertID int64
}

func (d *vfDriver) Open(string) (driver.Conn, error) { return vfConn{d: d}, nil }

func (d *vfDriver) record(query string, args []driver.NamedValue) {
	d.mu.Lock()
	defer d.mu.Unlock()
	*d.calls = append(*d.calls, vfCall{query: query, args: append([]driver.NamedValue(nil), args...)})
}

type vfConn struct{ d *vfDriver }

func (vfConn) Prepare(string) (driver.Stmt, error) { return nil, io.EOF }
func (vfConn) Close() error                        { return nil }
func (vfConn) Begin() (driver.Tx, error)           { return vfTx{}, nil }

// CheckNamedValue mirrors real pgx v5's stdlib.Conn.CheckNamedValue (verified
// against the pinned github.com/jackc/pgx/v5 v5.10.0 source): it accepts
// every arg unconditionally and returns nil, so database/sql passes nv.Value
// through UNTOUCHED instead of running its own DefaultParameterConverter
// fallback. That fallback would itself reduce a json.Number to a plain
// string via reflection (Kind()==String) regardless of anything tabular.go
// does — which would make a T-AUD-02 test pass "by accident" even with a
// broken bindArg. Implementing this interface here closes that gap: the ONLY
// thing standing between a raw json.Number/float64 and the captured arg is
// tabular.go's own conversion, exactly matching what happens against the
// real driver.
func (vfConn) CheckNamedValue(*driver.NamedValue) error { return nil }

func (c vfConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.d.record(query, args)
	return vfResult{lastInsertID: c.d.lastInsertID}, nil
}

func (c vfConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.d.record(query, args)
	lower := strings.ToLower(query)
	switch {
	case strings.Contains(lower, "table_constraints"), strings.Contains(lower, "key_column_usage"):
		// pkSQL: two columns per row (column_name, "") so queryPairs' primary
		// 2-arg Scan succeeds without needing its single-column retry path.
		rows := make([][]driver.Value, len(c.d.pkCols))
		for i, col := range c.d.pkCols {
			rows[i] = []driver.Value{col, ""}
		}
		return &vfRows{cols: []string{"column_name", "_"}, data: rows}, nil
	case strings.Contains(lower, "information_schema.columns"), strings.Contains(lower, "system.columns"):
		rows := make([][]driver.Value, len(c.d.cols))
		for i, col := range c.d.cols {
			rows[i] = []driver.Value{col[0], col[1]}
		}
		return &vfRows{cols: []string{"column_name", "data_type"}, data: rows}, nil
	case strings.Contains(lower, "returning"):
		return &vfRows{cols: c.d.pkCols, data: c.d.returning}, nil
	default:
		return &vfRows{}, nil
	}
}

type vfTx struct{}

func (vfTx) Commit() error   { return nil }
func (vfTx) Rollback() error { return nil }

type vfResult struct{ lastInsertID int64 }

func (r vfResult) LastInsertId() (int64, error) { return r.lastInsertID, nil }
func (r vfResult) RowsAffected() (int64, error) { return 1, nil }

type vfRows struct {
	cols []string
	data [][]driver.Value
	pos  int
}

func (r *vfRows) Columns() []string { return r.cols }
func (r *vfRows) Close() error      { return nil }
func (r *vfRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.pos])
	r.pos++
	return nil
}

var (
	vfRegisterN  int
	vfRegisterMu sync.Mutex
)

// registerVFDriver registers d under a fresh unique name (sql.Register panics
// on a duplicate name) and returns it, so each test gets an isolated driver
// instance without needing a hand-picked literal name.
func registerVFDriver(t *testing.T, d *vfDriver) string {
	t.Helper()
	vfRegisterMu.Lock()
	vfRegisterN++
	name := "vf-fake-" + strconv.Itoa(vfRegisterN)
	vfRegisterMu.Unlock()
	sql.Register(name, d)
	return name
}

func findCall(t *testing.T, calls []vfCall, substr string) vfCall {
	t.Helper()
	for _, c := range calls {
		if strings.Contains(strings.ToUpper(c.query), strings.ToUpper(substr)) {
			return c
		}
	}
	t.Fatalf("no captured call matched %q among %d calls", substr, len(calls))
	return vfCall{}
}

func assertArgValue(t *testing.T, call vfCall, want string) {
	t.Helper()
	for _, a := range call.args {
		if s, ok := a.Value.(string); ok && s == want {
			return
		}
	}
	t.Fatalf("query %q args %+v: no bound string arg equals %q — a bigint value must bind as its exact decimal text, never a float64 (T-AUD-02)", call.query, call.args, want)
}

func assertNoFloatArgs(t *testing.T, call vfCall) {
	t.Helper()
	for _, a := range call.args {
		if f, ok := a.Value.(float64); ok {
			t.Fatalf("query %q bound a float64 arg (%v) — bigint precision is already lost by the time this reaches the driver (T-AUD-02)", call.query, f)
		}
	}
}

// ---- T-AUD-02: bigint string transport ----

// TestEditCell_BigintJSONNumber_BindsExactText reproduces the audit's proven
// 2^53+1 corruption case (tabular.md §2's exact mapping table) against
// EditCell. json.Number is exactly what server.go's decode() (with
// UseNumber()) now produces for a bare JSON integer in CellEdit.NewValue/
// ExpectedOld/RowKey — the value bound to the driver must be the exact
// integer text, never a float64.
func TestEditCell_BigintJSONNumber_BindsExactText(t *testing.T) {
	t.Parallel()
	var calls []vfCall
	name := registerVFDriver(t, &vfDriver{calls: &calls})
	p, err := New(Config{Driver: name, DSN: "x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	edit := provider.CellEdit{
		Path:        provider.Path{Segments: []string{"app", "things"}},
		RowKey:      map[string]any{"id": json.Number("42")},
		Column:      "size_bytes",
		NewValue:    json.Number("9007199254740993"), // 2^53+1 — proven ±1 float64 corruption
		ExpectedOld: json.Number("9007199254740992"), // 2^53
	}
	if _, err := p.EditCell(context.Background(), edit); err != nil {
		t.Fatalf("EditCell: %v", err)
	}
	call := findCall(t, calls, "UPDATE")
	assertArgValue(t, call, "9007199254740993")
	assertArgValue(t, call, "9007199254740992")
	assertArgValue(t, call, "42")
	assertNoFloatArgs(t, call)
}

// TestInsertRow_BigintJSONNumber_BindsExactText mirrors the EditCell case for
// InsertRow.row map[string]any — the audit's other proven corruption site
// (tabular.md §2: "both as an insertRow value and as an editCell newValue").
// The row supplies its own PK explicitly, so this exercises the
// pkFromRow-echo path (decoupled from T-AUD-03's RETURNING/LastInsertId
// paths, tested separately below) while still proving the bound value is
// exact.
func TestInsertRow_BigintJSONNumber_BindsExactText(t *testing.T) {
	t.Parallel()
	var calls []vfCall
	name := registerVFDriver(t, &vfDriver{
		calls:  &calls,
		pkCols: []string{"id"},
		cols:   [][2]string{{"id", "bigint"}, {"size_bytes", "bigint"}},
	})
	p, err := New(Config{Driver: name, DSN: "x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	row := map[string]any{
		"id":         json.Number("7"),
		"size_bytes": json.Number("9007199254740993"), // 2^53+1
	}
	applied, err := p.InsertRow(context.Background(), provider.Path{Segments: []string{"app", "things"}}, row)
	if err != nil {
		t.Fatalf("InsertRow: %v", err)
	}
	if applied.Key["id"] != json.Number("7") {
		t.Errorf("Applied.Key[id] = %#v, want json.Number(7) echoed back verbatim", applied.Key["id"])
	}
	call := findCall(t, calls, "INSERT")
	assertArgValue(t, call, "9007199254740993")
	assertNoFloatArgs(t, call)
}

// TestDeleteRow_BigintJSONNumberKey_BindsExactText pins the third site named
// by DD-9's resolution ("incl. PK/rowKey/expectedOld"): DeleteRow's key
// shares EditCell's whereKey() helper, so a bigint-range PK value must bind
// exactly there too.
func TestDeleteRow_BigintJSONNumberKey_BindsExactText(t *testing.T) {
	t.Parallel()
	var calls []vfCall
	name := registerVFDriver(t, &vfDriver{calls: &calls})
	p, err := New(Config{Driver: name, DSN: "x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	key := map[string]any{"id": json.Number("9007199254740993")}
	if _, err := p.DeleteRow(context.Background(), provider.Path{Segments: []string{"app", "things"}}, key); err != nil {
		t.Fatalf("DeleteRow: %v", err)
	}
	call := findCall(t, calls, "DELETE")
	assertArgValue(t, call, "9007199254740993")
	assertNoFloatArgs(t, call)
}

// TestEditCell_DecimalFloat_StillWorks guards the fix's other direction: an
// ordinary JSON float (not bigint-range) must still bind as its numeric
// text via the exact same bindArg path (json.Number's string form) — the
// fix must not regress plain decimal/float columns.
func TestEditCell_DecimalFloat_StillWorks(t *testing.T) {
	t.Parallel()
	var calls []vfCall
	name := registerVFDriver(t, &vfDriver{calls: &calls})
	p, err := New(Config{Driver: name, DSN: "x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	edit := provider.CellEdit{
		Path:        provider.Path{Segments: []string{"app", "orders"}},
		RowKey:      map[string]any{"id": json.Number("1")},
		Column:      "total",
		NewValue:    json.Number("555.55"),
		ExpectedOld: json.Number("555.50"),
	}
	if _, err := p.EditCell(context.Background(), edit); err != nil {
		t.Fatalf("EditCell: %v", err)
	}
	call := findCall(t, calls, "UPDATE")
	assertArgValue(t, call, "555.55")
	assertArgValue(t, call, "555.50")
}

// ---- T-AUD-03: insert-key echo ----

// TestInsertRow_Postgres_ReturningEchoesGeneratedKey pins the Postgres half
// of T-AUD-03: when the caller omits the (server-generated) PK, InsertRow
// must recover it via RETURNING in the same round-trip rather than leaving
// the caller with no reliable way to address the just-inserted row.
func TestInsertRow_Postgres_ReturningEchoesGeneratedKey(t *testing.T) {
	t.Parallel()
	var calls []vfCall
	name := registerVFDriver(t, &vfDriver{
		calls:     &calls,
		pkCols:    []string{"id"},
		cols:      [][2]string{{"id", "integer"}, {"name", "text"}},
		returning: [][]driver.Value{{int64(99)}},
	})
	p, err := New(Config{Driver: name, DSN: "x"}) // non-mysql/clickhouse name -> pgDialect
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	row := map[string]any{"name": "new-row"} // no explicit id: server-generated
	applied, err := p.InsertRow(context.Background(), provider.Path{Segments: []string{"app", "things"}}, row)
	if err != nil {
		t.Fatalf("InsertRow: %v", err)
	}
	if applied.Key == nil || applied.Key["id"] != int64(99) {
		t.Fatalf("Applied.Key = %+v, want {id: 99} recovered via RETURNING (T-AUD-03)", applied.Key)
	}
	if !strings.Contains(strings.ToUpper(applied.Statement), "RETURNING") {
		t.Errorf("Applied.Statement = %q, want it to include RETURNING", applied.Statement)
	}
}

// TestInsertRow_MySQLDialect_LastInsertIdEchoesGeneratedKey pins the
// MySQL/MariaDB half of T-AUD-03. myDialect is constructed directly (bypassing
// New()'s driver-name dispatch, which selects a dialect by literal driver
// name "mysql"/"clickhouse" — a name this hermetic fake driver cannot borrow,
// since the real go-sql-driver/mysql package already claims "mysql" via its
// own init()) so the test exercises the mysql-dialect InsertRow code path
// directly against the fake driver.
func TestInsertRow_MySQLDialect_LastInsertIdEchoesGeneratedKey(t *testing.T) {
	t.Parallel()
	var calls []vfCall
	d := &vfDriver{
		calls:        &calls,
		pkCols:       []string{"id"},
		cols:         [][2]string{{"id", "int"}, {"name", "varchar"}},
		lastInsertID: 55,
	}
	name := registerVFDriver(t, d)
	db, err := sql.Open(name, "x")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p := &Provider{db: db, d: myDialect{}, caps: provider.Capabilities{Family: provider.FamilyTabular}}

	row := map[string]any{"name": "new-row"} // no explicit id: AUTO_INCREMENT-generated
	applied, err := p.InsertRow(context.Background(), provider.Path{Segments: []string{"app", "things"}}, row)
	if err != nil {
		t.Fatalf("InsertRow: %v", err)
	}
	if applied.Key == nil || applied.Key["id"] != int64(55) {
		t.Fatalf("Applied.Key = %+v, want {id: 55} recovered via LastInsertId (T-AUD-03)", applied.Key)
	}
}

// TestInsertRow_ExplicitCompositeKey_EchoesWithoutExtraRoundTrip covers the
// case pkFromRow exists for: the caller already supplied every PK column's
// value (including a multi-column PK, which neither RETURNING nor
// LastInsertId could recover generically) — InsertRow must still honestly
// echo it back, straight from what the caller sent.
func TestInsertRow_ExplicitCompositeKey_EchoesWithoutExtraRoundTrip(t *testing.T) {
	t.Parallel()
	var calls []vfCall
	name := registerVFDriver(t, &vfDriver{
		calls:  &calls,
		pkCols: []string{"tenant_id", "item_id"},
		cols:   [][2]string{{"tenant_id", "integer"}, {"item_id", "integer"}},
	})
	p, err := New(Config{Driver: name, DSN: "x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	row := map[string]any{"tenant_id": json.Number("3"), "item_id": json.Number("12")}
	applied, err := p.InsertRow(context.Background(), provider.Path{Segments: []string{"app", "composite"}}, row)
	if err != nil {
		t.Fatalf("InsertRow: %v", err)
	}
	if applied.Key["tenant_id"] != json.Number("3") || applied.Key["item_id"] != json.Number("12") {
		t.Fatalf("Applied.Key = %+v, want the caller-supplied composite key echoed back", applied.Key)
	}
	for _, c := range calls {
		if strings.Contains(strings.ToUpper(c.query), "RETURNING") {
			t.Errorf("an explicit composite key must not need a RETURNING round-trip: %q", c.query)
		}
	}
}
