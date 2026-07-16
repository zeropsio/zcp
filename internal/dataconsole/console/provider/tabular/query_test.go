package tabular

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// A fake database/sql driver that records the TxOptions of the transaction the
// provider opens, so we can PIN that Query runs inside a READ ONLY transaction —
// the engine-level write-safety the /api/query route relies on entirely (that
// route has no AuthorizeWrite gate by design; a data-modifying CTE must be
// refused by the engine, not a statement-text filter). This is the structural
// half; the engine actually refusing the write is verified live against a real
// Postgres.

type ftDriver struct{ readOnly *bool }

func (d ftDriver) Open(string) (driver.Conn, error) { return ftConn(d), nil }

type ftConn struct{ readOnly *bool }

func (ftConn) Prepare(string) (driver.Stmt, error) { return nil, io.EOF }
func (ftConn) Close() error                        { return nil }
func (ftConn) Begin() (driver.Tx, error)           { return ftTx{}, nil }
func (c ftConn) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	*c.readOnly = opts.ReadOnly
	return ftTx{}, nil
}
func (ftConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return ftRows{}, nil
}

type ftTx struct{}

func (ftTx) Commit() error   { return nil }
func (ftTx) Rollback() error { return nil }

type ftRows struct{}

func (ftRows) Columns() []string              { return []string{"x"} }
func (ftRows) Close() error                   { return nil }
func (ftRows) Next(dest []driver.Value) error { return io.EOF }

func TestQuery_RunsInReadOnlyTx(t *testing.T) {
	var gotReadOnly bool
	sql.Register("faketx", ftDriver{&gotReadOnly})
	p, err := New(Config{Driver: "faketx", DSN: "x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	// A write statement must still travel the READ ONLY transaction path — the
	// engine is what refuses it, not any text inspection here.
	if _, err := p.Query(context.Background(), "INSERT INTO t VALUES (1)", provider.Page{}); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !gotReadOnly {
		t.Fatal("Query did not open a READ ONLY transaction — the /api/query write-safety invariant is broken")
	}
}

var registerPagingDriver sync.Once

func newPagingProvider(t *testing.T) *Provider {
	t.Helper()
	registerPagingDriver.Do(func() {
		sql.Register("pagingfake", pagingDriver{})
	})
	p, err := New(Config{Driver: "pagingfake", DSN: "x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func TestList_HonorsCursorPagination(t *testing.T) {
	p := newPagingProvider(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name  string
		path  provider.Path
		want  []string
		limit int
	}{
		{name: "schemas", want: fakeSchemas(), limit: 17},
		{name: "tables", path: provider.Path{Segments: []string{"app"}}, want: fakeTables(), limit: 13},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nodes, _, err := p.List(ctx, tc.path, provider.Page{})
			if err != nil {
				t.Fatalf("List default page: %v", err)
			}
			if len(nodes) > defaultLimit {
				t.Fatalf("default List returned %d nodes, want at most %d", len(nodes), defaultLimit)
			}

			walkNames(t, tc.want, tc.limit, func(cursor string, limit int) ([]string, string, error) {
				nodes, next, err := p.List(ctx, tc.path, provider.Page{Cursor: cursor, Limit: limit})
				if err != nil {
					return nil, "", err
				}
				names := make([]string, len(nodes))
				for i, n := range nodes {
					names[i] = n.Name
				}
				return names, next, nil
			})
		})
	}
}

func TestReadTable_HonorsCursorPagination(t *testing.T) {
	p := newPagingProvider(t)
	ctx := context.Background()

	first, err := p.ReadTable(ctx, provider.Path{Segments: []string{"app", "things"}}, provider.Page{})
	if err != nil {
		t.Fatalf("ReadTable default page: %v", err)
	}
	if len(first.Rows) > defaultLimit {
		t.Fatalf("default ReadTable returned %d rows, want at most %d", len(first.Rows), defaultLimit)
	}

	walkNames(t, fakeRowKeys(), 25, func(cursor string, limit int) ([]string, string, error) {
		tp, err := p.ReadTable(ctx, provider.Path{Segments: []string{"app", "things"}}, provider.Page{Cursor: cursor, Limit: limit})
		if err != nil {
			return nil, "", err
		}
		return rowIDs(tp.Rows), tp.NextCursor, nil
	})
}

func TestQuery_HonorsCursorPaginationInsideReadOnlyTx(t *testing.T) {
	p := newPagingProvider(t)
	ctx := context.Background()

	first, err := p.Query(ctx, "SELECT id, name FROM things ORDER BY id", provider.Page{})
	if err != nil {
		t.Fatalf("Query default page: %v", err)
	}
	if len(first.Rows) > defaultLimit {
		t.Fatalf("default Query returned %d rows, want at most %d", len(first.Rows), defaultLimit)
	}

	walkNames(t, fakeRowKeys(), 31, func(cursor string, limit int) ([]string, string, error) {
		tp, err := p.Query(ctx, "SELECT id, name FROM things ORDER BY id", provider.Page{Cursor: cursor, Limit: limit})
		if err != nil {
			return nil, "", err
		}
		return rowIDs(tp.Rows), tp.NextCursor, nil
	})
}

func walkNames(t *testing.T, want []string, limit int, fetch func(cursor string, limit int) ([]string, string, error)) {
	t.Helper()
	wantSet := map[string]bool{}
	for _, w := range want {
		wantSet[w] = true
	}
	seen := map[string]bool{}
	cursor := ""
	for page := 1; page <= len(want)+2; page++ {
		items, next, err := fetch(cursor, limit)
		if err != nil {
			t.Fatalf("fetch page %d: %v", page, err)
		}
		if len(items) > limit {
			t.Fatalf("page %d returned %d items, want at most limit %d", page, len(items), limit)
		}
		for _, item := range items {
			if !wantSet[item] {
				t.Fatalf("page %d returned unexpected item %q", page, item)
			}
			if seen[item] {
				t.Fatalf("duplicate item across pages: %q", item)
			}
			seen[item] = true
		}
		if len(seen) < len(want) && next == "" {
			t.Fatalf("page %d exhausted cursor after %d/%d items", page, len(seen), len(want))
		}
		if len(seen) == len(want) && next != "" {
			t.Fatalf("page %d returned cursor %q after the final item", page, next)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if len(seen) != len(want) {
		t.Fatalf("pagination returned %d distinct items, want %d", len(seen), len(want))
	}
}

func rowIDs(rows [][]any) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = fmt.Sprint(row[0])
	}
	return out
}

type pagingDriver struct{}

func (pagingDriver) Open(string) (driver.Conn, error) { return pagingConn{}, nil }

type pagingConn struct{}

func (pagingConn) Prepare(string) (driver.Stmt, error) { return nil, io.EOF }
func (pagingConn) Close() error                        { return nil }
func (pagingConn) Begin() (driver.Tx, error)           { return pagingTx{}, nil }
func (pagingConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return pagingTx{}, nil
}
func (pagingConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	return rowsForPagingQuery(query), nil
}

type pagingTx struct{}

func (pagingTx) Commit() error   { return nil }
func (pagingTx) Rollback() error { return nil }

type pagingRows struct {
	cols []string
	data [][]driver.Value
	pos  int
}

func (r *pagingRows) Columns() []string { return r.cols }
func (r *pagingRows) Close() error      { return nil }
func (r *pagingRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.pos])
	r.pos++
	return nil
}

func rowsForPagingQuery(query string) driver.Rows {
	lower := strings.ToLower(query)
	switch {
	case strings.Contains(lower, "information_schema.table_constraints"):
		return newPagingRows([]string{"column_name"}, [][]driver.Value{{"id"}})
	case strings.Contains(lower, "information_schema.columns"):
		return newPagingRows([]string{"column_name", "data_type"}, [][]driver.Value{
			{"id", "integer"},
			{"name", "text"},
		})
	case strings.Contains(lower, "information_schema.tables"):
		return newPagingRows([]string{"table_name"}, windowValues(tableValues(), query))
	case strings.Contains(lower, "information_schema.schemata"):
		return newPagingRows([]string{"schema_name"}, windowValues(schemaValues(), query))
	default:
		return newPagingRows([]string{"id", "name"}, windowValues(rowValues(), query))
	}
}

func newPagingRows(cols []string, data [][]driver.Value) *pagingRows {
	return &pagingRows{cols: cols, data: data}
}

func schemaValues() [][]driver.Value {
	names := fakeSchemas()
	out := make([][]driver.Value, len(names))
	for i, n := range names {
		out[i] = []driver.Value{n}
	}
	return out
}

func tableValues() [][]driver.Value {
	names := fakeTables()
	out := make([][]driver.Value, len(names))
	for i, n := range names {
		out[i] = []driver.Value{n}
	}
	return out
}

func rowValues() [][]driver.Value {
	keys := fakeRowKeys()
	out := make([][]driver.Value, len(keys))
	for i, k := range keys {
		id, _ := strconv.ParseInt(k, 10, 64)
		out[i] = []driver.Value{id, fmt.Sprintf("row-%03d", id)}
	}
	return out
}

func fakeSchemas() []string { return numberedNames("schema", defaultLimit+5) }
func fakeTables() []string  { return numberedNames("table", defaultLimit+5) }

func fakeRowKeys() []string {
	out := make([]string, defaultLimit+5)
	for i := range out {
		out[i] = strconv.Itoa(i + 1)
	}
	return out
}

func numberedNames(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s_%03d", prefix, i+1)
	}
	sort.Strings(out)
	return out
}

var (
	limitRE  = regexp.MustCompile(`(?i)\blimit\s+(\d+)`)
	offsetRE = regexp.MustCompile(`(?i)\boffset\s+(\d+)`)
)

func windowValues(rows [][]driver.Value, query string) [][]driver.Value {
	limit := len(rows)
	if matches := limitRE.FindAllStringSubmatch(query, -1); len(matches) > 0 {
		limit, _ = strconv.Atoi(matches[len(matches)-1][1])
	}
	offset := 0
	if matches := offsetRE.FindAllStringSubmatch(query, -1); len(matches) > 0 {
		offset, _ = strconv.Atoi(matches[len(matches)-1][1])
	}
	if offset >= len(rows) {
		return nil
	}
	end := min(offset+limit, len(rows))
	return rows[offset:end]
}

// ---- ReadTable: relation-not-found is 404, split from a real outage (T-AUD-06) ----

// missingTableDriver simulates the one unambiguous, dialect-agnostic signal
// that [schema, table] does not exist: the information_schema (or system.*)
// metadata queries always succeed but return zero rows — never a driver
// error (a real "relation does not exist" error only ever surfaces if
// ReadTable actually issues the row SELECT, which the fix must avoid). The
// row SELECT itself is wired to fail loudly if reached, so a passing test
// proves the short-circuit runs, not just that it's plausible.
type missingTableDriver struct{}

func (missingTableDriver) Open(string) (driver.Conn, error) { return missingTableConn{}, nil }

type missingTableConn struct{}

func (missingTableConn) Prepare(string) (driver.Stmt, error) { return nil, io.EOF }
func (missingTableConn) Close() error                        { return nil }
func (missingTableConn) Begin() (driver.Tx, error)           { return pagingTx{}, nil }
func (missingTableConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	lower := strings.ToLower(query)
	switch {
	case strings.Contains(lower, "information_schema.columns"):
		return newPagingRows([]string{"column_name", "data_type"}, nil), nil
	case strings.Contains(lower, "information_schema.table_constraints"):
		return newPagingRows([]string{"column_name"}, nil), nil
	default:
		return nil, fmt.Errorf("missingTableDriver: unexpected SELECT reached the engine: %q", query)
	}
}

// outageDriver simulates a genuine outage at the metadata-query layer itself
// (as opposed to a clean "zero rows" not-found signal) — proves the
// not-found short-circuit never swallows a real connection/driver failure.
type outageDriver struct{}

func (outageDriver) Open(string) (driver.Conn, error) { return outageConn{}, nil }

type outageConn struct{}

func (outageConn) Prepare(string) (driver.Stmt, error) { return nil, io.EOF }
func (outageConn) Close() error                        { return nil }
func (outageConn) Begin() (driver.Tx, error)           { return pagingTx{}, nil }
func (outageConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return nil, errors.New("connection reset by peer")
}

var (
	registerMissingTableDriver sync.Once
	registerOutageDriver       sync.Once
)

func TestReadTable_NonexistentTable_ReturnsNotFound(t *testing.T) {
	t.Parallel()
	registerMissingTableDriver.Do(func() {
		sql.Register("missingtablefake", missingTableDriver{})
	})
	p, err := New(Config{Driver: "missingtablefake", DSN: "x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	_, err = p.ReadTable(context.Background(), provider.Path{Segments: []string{"public", "does_not_exist"}}, provider.Page{})
	if !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("ReadTable(nonexistent table) = %v, want ErrNotFound", err)
	}
	if errors.Is(err, provider.ErrUpstream) {
		t.Fatalf("ReadTable(nonexistent table) still classified as ErrUpstream (T-AUD-06 regression)")
	}
}

func TestReadTable_MetadataQueryFails_StaysUpstream(t *testing.T) {
	t.Parallel()
	registerOutageDriver.Do(func() {
		sql.Register("outagefake", outageDriver{})
	})
	p, err := New(Config{Driver: "outagefake", DSN: "x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	_, err = p.ReadTable(context.Background(), provider.Path{Segments: []string{"public", "t"}}, provider.Page{})
	if !errors.Is(err, provider.ErrUpstream) {
		t.Fatalf("ReadTable(metadata query failure) = %v, want ErrUpstream (a real outage must not be reclassified as not_found)", err)
	}
	if errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("ReadTable(metadata query failure) misclassified as ErrNotFound")
	}
}

// ---- Column.Editable/Reason (DD-4, resolution 6) ----

func newPagingProviderViewOnly(t *testing.T) *Provider {
	t.Helper()
	registerPagingDriver.Do(func() {
		sql.Register("pagingfake", pagingDriver{})
	})
	p, err := New(Config{Driver: "pagingfake", DSN: "x", NoEdit: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// TestReadTable_ColumnEditability pins DD-4/resolution 6: a PK column is
// never editable in v1 (Reason "primary key" — an edit there is a
// delete+recreate relational operation, out of scope), a non-PK column is
// editable.
func TestReadTable_ColumnEditability(t *testing.T) {
	t.Parallel()
	p := newPagingProvider(t)
	tp, err := p.ReadTable(context.Background(), provider.Path{Segments: []string{"app", "things"}}, provider.Page{})
	if err != nil {
		t.Fatalf("ReadTable: %v", err)
	}
	byName := map[string]provider.Column{}
	for _, c := range tp.Columns {
		byName[c.Name] = c
	}
	id, ok := byName["id"]
	if !ok {
		t.Fatalf("Columns = %+v, want an id column", tp.Columns)
	}
	if !id.PK || id.Editable || id.Reason != "primary key" {
		t.Errorf("id column = %+v, want PK=true Editable=false Reason=\"primary key\"", id)
	}
	name, ok := byName["name"]
	if !ok {
		t.Fatalf("Columns = %+v, want a name column", tp.Columns)
	}
	if name.PK || !name.Editable || name.Reason != "" {
		t.Errorf("name column = %+v, want PK=false Editable=true Reason=\"\"", name)
	}
}

// TestQuery_ColumnsAreNonEditable pins §7.1's "query grid MUST render as
// explicitly non-editable, not as an editable grid that silently ignores
// clicks" (U-01): Query never returns dataType/pk, so every column must be
// EXPLICITLY Editable=false with a stated reason, never editable-by-omission.
func TestQuery_ColumnsAreNonEditable(t *testing.T) {
	t.Parallel()
	p := newPagingProvider(t)
	tp, err := p.Query(context.Background(), "SELECT id, name FROM things", provider.Page{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(tp.Columns) == 0 {
		t.Fatal("Query returned no columns")
	}
	for _, c := range tp.Columns {
		if c.Editable {
			t.Errorf("column %q Editable = true, want false (query results are read-only, U-01)", c.Name)
		}
		if c.Reason == "" {
			t.Errorf("column %q Reason = \"\", want a stated reason", c.Name)
		}
	}
}

// TestReadTable_ViewOnlyTier_AllColumnsNonEditable pins DD-4: on a
// view-only-tier table (clickhouse — Config.NoEdit), every column is
// non-editable regardless of PK-ness — the engine-level refusal fires even
// with a valid write token, so the column-level signal must agree.
func TestReadTable_ViewOnlyTier_AllColumnsNonEditable(t *testing.T) {
	t.Parallel()
	p := newPagingProviderViewOnly(t)
	tp, err := p.ReadTable(context.Background(), provider.Path{Segments: []string{"app", "things"}}, provider.Page{})
	if err != nil {
		t.Fatalf("ReadTable: %v", err)
	}
	if len(tp.Columns) == 0 {
		t.Fatal("ReadTable returned no columns")
	}
	for _, c := range tp.Columns {
		if c.Editable {
			t.Errorf("column %q Editable = true, want false (view-only tier)", c.Name)
		}
		if c.Reason == "" {
			t.Errorf("column %q Reason = \"\", want a stated reason", c.Name)
		}
	}
}

// ---- Stat: every family answers /api/stat uniformly ----

// TestStat_ReturnsNodeForExistingTable pins finding #3: before this,
// TabularProvider had no Stat method at all, so /api/stat on any table
// 422'd (ErrUnsupported) — the one family/route combination that didn't
// answer.
func TestStat_ReturnsNodeForExistingTable(t *testing.T) {
	t.Parallel()
	p := newPagingProvider(t)
	node, err := p.Stat(context.Background(), provider.Path{Segments: []string{"app", "things"}})
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if node.Kind != provider.KindTabular {
		t.Errorf("Kind = %q, want %q", node.Kind, provider.KindTabular)
	}
	if node.Name != "things" {
		t.Errorf("Name = %q, want things", node.Name)
	}
}

// TestStat_NonexistentTable_ReturnsNotFound mirrors
// TestReadTable_NonexistentTable_ReturnsNotFound's not-found detection
// (T-AUD-06): Stat must not depend on issuing the row SELECT to notice a
// missing relation.
func TestStat_NonexistentTable_ReturnsNotFound(t *testing.T) {
	t.Parallel()
	registerMissingTableDriver.Do(func() {
		sql.Register("missingtablefake", missingTableDriver{})
	})
	p, err := New(Config{Driver: "missingtablefake", DSN: "x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	_, err = p.Stat(context.Background(), provider.Path{Segments: []string{"public", "does_not_exist"}})
	if !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("Stat(nonexistent table) = %v, want ErrNotFound", err)
	}
}

// TestStat_MetadataQueryFails_StaysUpstream is Stat's outage-vs-not-found
// counterpart to TestReadTable_MetadataQueryFails_StaysUpstream.
func TestStat_MetadataQueryFails_StaysUpstream(t *testing.T) {
	t.Parallel()
	registerOutageDriver.Do(func() {
		sql.Register("outagefake", outageDriver{})
	})
	p, err := New(Config{Driver: "outagefake", DSN: "x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	_, err = p.Stat(context.Background(), provider.Path{Segments: []string{"public", "t"}})
	if !errors.Is(err, provider.ErrUpstream) {
		t.Fatalf("Stat(metadata query failure) = %v, want ErrUpstream", err)
	}
}

// TestStat_RejectsWrongArity mirrors the arity guard every other family's
// Stat applies (object/document/kv/stream) — a schema-only path is not a
// table address.
func TestStat_RejectsWrongArity(t *testing.T) {
	t.Parallel()
	p := newPagingProvider(t)
	if _, err := p.Stat(context.Background(), provider.Path{Segments: []string{"only-schema"}}); !errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("Stat(1-seg) = %v, want ErrInvalid", err)
	}
}
