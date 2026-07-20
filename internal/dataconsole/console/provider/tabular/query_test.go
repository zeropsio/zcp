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
	"sync/atomic"
	"testing"
	"time"

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

var (
	registerSortContractDriver      sync.Once
	sortContractQuery               string
	registerAggregateContractDriver sync.Once
	aggregateContractQueries        []string
)

type sortContractDriver struct{}

func (sortContractDriver) Open(string) (driver.Conn, error) { return sortContractConn{}, nil }

type sortContractConn struct{}

func (sortContractConn) Prepare(string) (driver.Stmt, error) { return nil, io.EOF }
func (sortContractConn) Close() error                        { return nil }
func (sortContractConn) Begin() (driver.Tx, error)           { return pagingTx{}, nil }
func (sortContractConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	lower := strings.ToLower(query)
	switch {
	case strings.Contains(lower, "information_schema.table_constraints"):
		return newPagingRows([]string{"column_name"}, [][]driver.Value{{"id"}}), nil
	case strings.Contains(lower, "information_schema.columns"):
		return newPagingRows([]string{"column_name", "data_type"}, [][]driver.Value{
			{"id", "integer"},
			{"name", "text"},
		}), nil
	default:
		sortContractQuery = query
		return newPagingRows([]string{"id", "name"}, [][]driver.Value{
			{int64(8), "zulu"},
			{int64(7), "zulu"},
			{int64(6), "yankee"},
		}), nil
	}
}

func TestReadTable_Sort_ReturnsStableBoundedPage(t *testing.T) {
	registerSortContractDriver.Do(func() { sql.Register("sortcontractfake", sortContractDriver{}) })
	p, err := New(Config{Driver: "sortcontractfake", DSN: "x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	tp, err := p.ReadTable(context.Background(), provider.Path{Segments: []string{"app", "things"}}, provider.Page{
		Cursor: "2",
		Limit:  2,
		Sort:   &provider.Sort{Column: "name", Direction: provider.SortDesc},
	})
	if err != nil {
		t.Fatalf("ReadTable: %v", err)
	}
	wantSQL := `SELECT "id", "name" FROM "app"."things" ORDER BY "name" DESC, "id" ASC LIMIT 3 OFFSET 2`
	if sortContractQuery != wantSQL {
		t.Fatalf("query = %q, want %q", sortContractQuery, wantSQL)
	}
	if len(tp.Rows) != 2 || fmt.Sprint(tp.Rows[0][0]) != "8" || fmt.Sprint(tp.Rows[1][0]) != "7" {
		t.Fatalf("rows = %#v, want bounded first two rows [8, 7]", tp.Rows)
	}
	if tp.NextCursor != "4" {
		t.Fatalf("nextCursor = %q, want 4", tp.NextCursor)
	}
	if len(tp.Columns) != 2 || !tp.Columns[0].Sortable || !tp.Columns[1].Sortable {
		t.Fatalf("columns = %+v, want both server-declared sortable", tp.Columns)
	}
}

var (
	registerPostgresSortabilityDriver sync.Once
	postgresSortabilityQueries        []string
)

type postgresSortabilityDriver struct{}

func (postgresSortabilityDriver) Open(string) (driver.Conn, error) {
	return postgresSortabilityConn{}, nil
}

type postgresSortabilityConn struct{}

func (postgresSortabilityConn) Prepare(string) (driver.Stmt, error) { return nil, io.EOF }
func (postgresSortabilityConn) Close() error                        { return nil }
func (postgresSortabilityConn) Begin() (driver.Tx, error)           { return pagingTx{}, nil }
func (postgresSortabilityConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	lower := strings.ToLower(query)
	switch {
	case strings.Contains(lower, "information_schema.table_constraints"):
		return newPagingRows([]string{"column_name"}, nil), nil
	case strings.Contains(lower, "information_schema.columns"):
		return newPagingRows([]string{"column_name", "data_type"}, [][]driver.Value{
			{"payload", "json"},
			{"document", "xml"},
			{"tags", "ARRAY"},
			{"custom", "USER-DEFINED"},
			{"label", "text"},
		}), nil
	default:
		postgresSortabilityQueries = append(postgresSortabilityQueries, query)
		return newPagingRows([]string{"payload", "document", "tags", "custom", "label"}, [][]driver.Value{
			{`{"ok":true}`, "<ok/>", "{one}", "custom", "alpha"},
		}), nil
	}
}

func TestReadTable_PostgresNonOrderableTypes_RejectsBeforeQuery(t *testing.T) {
	registerPostgresSortabilityDriver.Do(func() {
		sql.Register("postgressortabilityfake", postgresSortabilityDriver{})
	})
	p, err := New(Config{Driver: "postgressortabilityfake", DSN: "x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	path := provider.Path{Segments: []string{"app", "documents"}}

	postgresSortabilityQueries = nil
	page, err := p.ReadTable(context.Background(), path, provider.Page{Limit: 10})
	if err != nil {
		t.Fatalf("ReadTable(default): %v", err)
	}
	if len(postgresSortabilityQueries) != 1 {
		t.Fatalf("default data queries = %v, want exactly one", postgresSortabilityQueries)
	}
	wantDefault := `SELECT "payload", "document", "tags", "custom", "label" FROM "app"."documents" ORDER BY "label" ASC LIMIT 11 OFFSET 0`
	if postgresSortabilityQueries[0] != wantDefault {
		t.Fatalf("default query = %q, want %q", postgresSortabilityQueries[0], wantDefault)
	}
	if len(page.Columns) != 5 {
		t.Fatalf("columns = %+v, want five", page.Columns)
	}
	for _, col := range page.Columns[:4] {
		if col.Sortable || col.SortReason == "" {
			t.Errorf("column %+v, want non-sortable with a reason", col)
		}
	}
	if !page.Columns[4].Sortable || page.Columns[4].SortReason != "" {
		t.Errorf("scalar column = %+v, want sortable without a reason", page.Columns[4])
	}

	for _, column := range []string{"payload", "document", "tags", "custom"} {
		t.Run(column, func(t *testing.T) {
			postgresSortabilityQueries = nil
			_, err := p.ReadTable(context.Background(), path, provider.Page{
				Sort: &provider.Sort{Column: column, Direction: provider.SortAsc},
			})
			if !errors.Is(err, provider.ErrInvalid) {
				t.Fatalf("ReadTable(sort=%s) = %v, want ErrInvalid", column, err)
			}
			if len(postgresSortabilityQueries) != 0 {
				t.Fatalf("data queries = %v, want rejection before relation SELECT", postgresSortabilityQueries)
			}
		})
	}
}

type aggregateContractDriver struct{}

func (aggregateContractDriver) Open(string) (driver.Conn, error) { return aggregateContractConn{}, nil }

type aggregateContractConn struct{}

func (aggregateContractConn) Prepare(string) (driver.Stmt, error) { return nil, io.EOF }
func (aggregateContractConn) Close() error                        { return nil }
func (aggregateContractConn) Begin() (driver.Tx, error)           { return pagingTx{}, nil }
func (aggregateContractConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	lower := strings.ToLower(query)
	switch {
	case strings.Contains(lower, "system.columns") && strings.Contains(lower, "is_in_primary_key"):
		return newPagingRows([]string{"name"}, [][]driver.Value{{"id"}}), nil
	case strings.Contains(lower, "system.columns"):
		return newPagingRows([]string{"name", "type"}, [][]driver.Value{
			{"id", "UInt64"},
			{"big", "AggregateFunction(sum, UInt64)"},
			{"text", "AggregateFunction(any, String)"},
			{"array", "AggregateFunction(groupArray, UInt64)"},
			{"tuple", "AggregateFunction(any, Tuple(String, UInt64))"},
			{"simple", "SimpleAggregateFunction(sum, UInt64)"},
		}), nil
	default:
		aggregateContractQueries = append(aggregateContractQueries, query)
		return newPagingRows([]string{"id", "big", "text", "array", "tuple", "simple"}, [][]driver.Value{
			{int64(1), "9007199254740993", "alpha", "[1,2,3]", "('x',42)", int64(9)},
		}), nil
	}
}

func newAggregateContractProvider(t *testing.T) *Provider {
	t.Helper()
	registerAggregateContractDriver.Do(func() { sql.Register("aggregatecontractfake", aggregateContractDriver{}) })
	db, err := sql.Open("aggregatecontractfake", "x")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &Provider{db: db, d: chDialect{}, caps: provider.Capabilities{Family: provider.FamilyTabular, Support: provider.SupportViewOnly, ReadOnly: true}}
}

func TestReadTable_InvalidOrUnsupportedSort_RejectsDescriptor(t *testing.T) {
	p := newAggregateContractProvider(t)
	path := provider.Path{Segments: []string{"telemetry", "daily"}}

	for _, tc := range []struct {
		name string
		sort provider.Sort
	}{
		{name: "unknown column", sort: provider.Sort{Column: "missing", Direction: provider.SortAsc}},
		{name: "invalid direction", sort: provider.Sort{Column: "id", Direction: "sideways"}},
		{name: "aggregate state", sort: provider.Sort{Column: "big", Direction: provider.SortDesc}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			aggregateContractQueries = nil
			_, err := p.ReadTable(context.Background(), path, provider.Page{Sort: &tc.sort})
			if !errors.Is(err, provider.ErrInvalid) {
				t.Fatalf("ReadTable(sort=%+v) = %v, want ErrInvalid", tc.sort, err)
			}
			if len(aggregateContractQueries) != 0 {
				t.Fatalf("data queries = %v, want descriptor rejected before relation SELECT", aggregateContractQueries)
			}
		})
	}
}

func TestReadTable_ClickHouseAggregateFunctionColumns_FinalizesAsText(t *testing.T) {
	p := newAggregateContractProvider(t)
	aggregateContractQueries = nil

	tp, err := p.ReadTable(context.Background(), provider.Path{Segments: []string{"telemetry", "daily"}}, provider.Page{Limit: 1})
	if err != nil {
		t.Fatalf("ReadTable: %v", err)
	}
	wantSQL := "SELECT `id`, " +
		"toString(finalizeAggregation(`big`)) AS `big`, " +
		"toString(finalizeAggregation(`text`)) AS `text`, " +
		"toString(finalizeAggregation(`array`)) AS `array`, " +
		"toString(finalizeAggregation(`tuple`)) AS `tuple`, `simple` " +
		"FROM `telemetry`.`daily` ORDER BY `id` ASC LIMIT 2 OFFSET 0"
	if len(aggregateContractQueries) != 1 || aggregateContractQueries[0] != wantSQL {
		t.Fatalf("data queries = %#v, want exactly %q", aggregateContractQueries, wantSQL)
	}
	wantRow := []any{int64(1), "9007199254740993", "alpha", "[1,2,3]", "('x',42)", int64(9)}
	if len(tp.Rows) != 1 || fmt.Sprint(tp.Rows[0]) != fmt.Sprint(wantRow) {
		t.Fatalf("rows = %#v, want exact textual literals %#v", tp.Rows, wantRow)
	}
	for _, col := range tp.Columns {
		if isClickHouseAggregateState(col.DataType) {
			if col.Sortable || col.SortReason != "aggregate state" {
				t.Errorf("aggregate column %+v, want non-sortable with reason", col)
			}
		}
		if col.Name == "simple" && !col.Sortable {
			t.Errorf("SimpleAggregateFunction column = %+v, want ordinary pass-through sortable column", col)
		}
	}
}

var (
	registerDefaultOrderDriver sync.Once
	defaultOrderQueries        = map[string]string{}
)

type defaultOrderDriver struct{}

func (defaultOrderDriver) Open(mode string) (driver.Conn, error) {
	return defaultOrderConn{mode: mode}, nil
}

type defaultOrderConn struct{ mode string }

func (defaultOrderConn) Prepare(string) (driver.Stmt, error) { return nil, io.EOF }
func (defaultOrderConn) Close() error                        { return nil }
func (defaultOrderConn) Begin() (driver.Tx, error)           { return pagingTx{}, nil }
func (c defaultOrderConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	lower := strings.ToLower(query)
	switch {
	case strings.Contains(lower, "system.columns") && strings.Contains(lower, "is_in_primary_key"):
		if c.mode == "pk" {
			return newPagingRows([]string{"name"}, [][]driver.Value{{"id"}}), nil
		}
		return newPagingRows([]string{"name"}, nil), nil
	case strings.Contains(lower, "system.columns"):
		cols := [][]driver.Value{{"state", "AggregateFunction(sum, UInt64)"}}
		switch c.mode {
		case "pk":
			cols = [][]driver.Value{{"id", "UInt64"}, {"label", "String"}}
		case "first-sortable":
			cols = [][]driver.Value{{"state", "AggregateFunction(sum, UInt64)"}, {"label", "String"}}
		}
		return newPagingRows([]string{"name", "type"}, cols), nil
	default:
		defaultOrderQueries[c.mode] = query
		switch c.mode {
		case "pk":
			return newPagingRows([]string{"id", "label"}, nil), nil
		case "first-sortable":
			return newPagingRows([]string{"state", "label"}, nil), nil
		default:
			return newPagingRows([]string{"state"}, nil), nil
		}
	}
}

func TestReadTable_DefaultOrder_SkipsUnsupportedColumns(t *testing.T) {
	registerDefaultOrderDriver.Do(func() { sql.Register("defaultorderfake", defaultOrderDriver{}) })
	for _, tc := range []struct {
		name       string
		mode       string
		wantSuffix string
		bestEffort bool
	}{
		{name: "primary key", mode: "pk", wantSuffix: "ORDER BY `id` ASC LIMIT 101 OFFSET 0"},
		{name: "first sortable after aggregate state", mode: "first-sortable", wantSuffix: "ORDER BY `label` ASC LIMIT 101 OFFSET 0", bestEffort: true},
		{name: "no sortable columns", mode: "none", wantSuffix: "FROM `telemetry`.`daily` LIMIT 101 OFFSET 0", bestEffort: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := sql.Open("defaultorderfake", tc.mode)
			if err != nil {
				t.Fatalf("sql.Open: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })
			p := &Provider{db: db, d: chDialect{}, caps: provider.Capabilities{Support: provider.SupportViewOnly, ReadOnly: true}}
			tp, err := p.ReadTable(context.Background(), provider.Path{Segments: []string{"telemetry", "daily"}}, provider.Page{})
			if err != nil {
				t.Fatalf("ReadTable: %v", err)
			}
			if got := defaultOrderQueries[tc.mode]; !strings.HasSuffix(got, tc.wantSuffix) {
				t.Fatalf("query = %q, want suffix %q", got, tc.wantSuffix)
			}
			if tp.BestEffort != tc.bestEffort {
				t.Fatalf("bestEffort = %v, want %v", tp.BestEffort, tc.bestEffort)
			}
		})
	}
}

var (
	registerCountContractDriver sync.Once
	activeCountContractState    atomic.Pointer[countContractState]
)

type countContractState struct {
	entered chan struct{}
	release chan struct{}
}

type countContractDriver struct{}

func (countContractDriver) Open(string) (driver.Conn, error) { return countContractConn{}, nil }

type countContractConn struct{}

func (countContractConn) Prepare(string) (driver.Stmt, error) { return nil, io.EOF }
func (countContractConn) Close() error                        { return nil }
func (countContractConn) Begin() (driver.Tx, error)           { return pagingTx{}, nil }
func (countContractConn) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	lower := strings.ToLower(query)
	switch {
	case strings.Contains(lower, "information_schema.table_constraints"):
		return newPagingRows([]string{"column_name"}, [][]driver.Value{{"id"}}), nil
	case strings.Contains(lower, "information_schema.columns"):
		return newPagingRows([]string{"column_name", "data_type"}, [][]driver.Value{{"id", "integer"}}), nil
	case strings.Contains(lower, "count(*)"):
		state := activeCountContractState.Load()
		select {
		case state.entered <- struct{}{}:
		default:
		}
		select {
		case <-state.release:
			return newPagingRows([]string{"count"}, [][]driver.Value{{int64(50000)}}), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	default:
		return newPagingRows([]string{"id"}, [][]driver.Value{{int64(1)}}), nil
	}
}

func newCountContractProvider(t *testing.T, timeout time.Duration, state *countContractState) *Provider {
	t.Helper()
	registerCountContractDriver.Do(func() { sql.Register("countcontractfake", countContractDriver{}) })
	activeCountContractState.Store(state)
	p, err := New(Config{Driver: "countcontractfake", DSN: "x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.countTimeout = timeout
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func TestCountTable_TimeoutBusyAndCancel_NeverBlocksReadTable(t *testing.T) {
	path := provider.Path{Segments: []string{"app", "things"}}

	t.Run("busy count does not block a readable page and cancel releases the slot", func(t *testing.T) {
		state := &countContractState{entered: make(chan struct{}, 1), release: make(chan struct{})}
		p := newCountContractProvider(t, time.Second, state)
		ctx, cancel := context.WithCancel(context.Background())
		firstDone := make(chan error, 1)
		go func() {
			_, err := p.CountTable(ctx, path)
			firstDone <- err
		}()
		select {
		case <-state.entered:
		case <-time.After(time.Second):
			t.Fatal("first count did not enter driver")
		}

		busyStart := time.Now()
		if _, err := p.CountTable(context.Background(), path); err == nil {
			t.Fatal("concurrent CountTable = nil, want busy error")
		}
		if elapsed := time.Since(busyStart); elapsed > 100*time.Millisecond {
			t.Fatalf("busy CountTable took %s, want immediate refusal", elapsed)
		}

		readStart := time.Now()
		if _, err := p.ReadTable(context.Background(), path, provider.Page{Limit: 1}); err != nil {
			t.Fatalf("ReadTable while count active: %v", err)
		}
		if elapsed := time.Since(readStart); elapsed > 100*time.Millisecond {
			t.Fatalf("ReadTable while count active took %s, want independent readable page", elapsed)
		}

		cancel()
		select {
		case err := <-firstDone:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled CountTable = %v, want context.Canceled", err)
			}
		case <-time.After(time.Second):
			t.Fatal("canceled CountTable did not return")
		}
	})

	t.Run("hard timeout releases the count slot", func(t *testing.T) {
		state := &countContractState{entered: make(chan struct{}, 2), release: make(chan struct{})}
		p := newCountContractProvider(t, 20*time.Millisecond, state)
		if _, err := p.CountTable(context.Background(), path); !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, provider.ErrTimeout) {
			t.Fatalf("timed CountTable = %v, want context.DeadlineExceeded plus sanitized ErrTimeout", err)
		}
		close(state.release)
		got, err := p.CountTable(context.Background(), path)
		if err != nil {
			t.Fatalf("CountTable after timeout released slot: %v", err)
		}
		if got != 50000 {
			t.Fatalf("count = %d, want 50000", got)
		}
	})
}

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
