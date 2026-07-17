package tabular

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// This file pins the S1 fix: every tabular SQL rejection used to be wrapped
// unconditionally in ErrUpstream (502 "the platform broke"), even a SQL
// typo, a wrong-type literal, or a NOT NULL violation — an I-2 honesty
// violation (spec-dataconsole.md §7.1: "a validation rejection is not an
// outage"). Evidence: plans/dataconsole-audit/ (UX interaction critic).

// ---- pgErr / mysqlErr: typed driver errors matching real server responses ----
//
// Constructed exactly as the pinned jackc/pgx/v5 v5.10.0 / go-sql-driver/mysql
// v1.10.0 sources build them from a real server response, so isConnectivityErr's
// SQLSTATE-class detection is exercised the same way it would be against a
// live engine.

func pgErr(code, message string) *pgconn.PgError {
	return &pgconn.PgError{Severity: "ERROR", Code: code, Message: message}
}

func mysqlErr(number uint16, sqlState, message string) *mysql.MySQLError {
	me := &mysql.MySQLError{Number: number, Message: message}
	copy(me.SQLState[:], sqlState)
	return me
}

// ---- rejectDriver: a fake database/sql driver for engine-error-
// classification tests ----
//
// Metadata queries (pk/columns) answer with a default single "id" PK column
// so EditCell/InsertRow/DeleteRow reach their mutation statement without
// per-test setup; the query/DML statement itself fails with the configured
// error — proving what tabular.go's classifier maps a given ENGINE error to,
// without a live database. Modeled on valuefidelity_test.go's vfDriver.

type rejectDriver struct {
	pkCols []string
	cols   [][2]string

	queryErr error // QueryContext error for a non-metadata SELECT/RETURNING
	execErr  error // ExecContext error
}

func (d rejectDriver) Open(string) (driver.Conn, error) { return rejectConn{d: d}, nil }

type rejectConn struct{ d rejectDriver }

func (rejectConn) Prepare(string) (driver.Stmt, error) { return nil, io.EOF }
func (rejectConn) Close() error                        { return nil }
func (rejectConn) Begin() (driver.Tx, error)           { return rejectTx{}, nil }
func (rejectConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return rejectTx{}, nil
}

func (c rejectConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	if c.d.execErr != nil {
		return nil, c.d.execErr
	}
	return rejectResult{}, nil
}

func (c rejectConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	lower := strings.ToLower(query)
	switch {
	case strings.Contains(lower, "table_constraints"), strings.Contains(lower, "key_column_usage"):
		cols := c.d.pkCols
		if cols == nil {
			cols = []string{"id"}
		}
		rows := make([][]driver.Value, len(cols))
		for i, col := range cols {
			rows[i] = []driver.Value{col, ""}
		}
		return &rejectRows{cols: []string{"column_name", "_"}, data: rows}, nil
	case strings.Contains(lower, "information_schema.columns"), strings.Contains(lower, "system.columns"):
		cols := c.d.cols
		if cols == nil {
			cols = [][2]string{{"id", "integer"}}
		}
		rows := make([][]driver.Value, len(cols))
		for i, col := range cols {
			rows[i] = []driver.Value{col[0], col[1]}
		}
		return &rejectRows{cols: []string{"column_name", "data_type"}, data: rows}, nil
	case strings.Contains(lower, "returning"):
		if c.d.queryErr != nil {
			return nil, c.d.queryErr
		}
		return &rejectRows{cols: []string{"id"}, data: [][]driver.Value{{int64(1)}}}, nil
	default:
		if c.d.queryErr != nil {
			return nil, c.d.queryErr
		}
		return &rejectRows{cols: []string{"x"}}, nil
	}
}

type rejectTx struct{}

func (rejectTx) Commit() error   { return nil }
func (rejectTx) Rollback() error { return nil }

type rejectResult struct{}

func (rejectResult) LastInsertId() (int64, error) { return 0, nil }
func (rejectResult) RowsAffected() (int64, error) { return 1, nil }

type rejectRows struct {
	cols []string
	data [][]driver.Value
	pos  int
}

func (r *rejectRows) Columns() []string { return r.cols }
func (r *rejectRows) Close() error      { return nil }
func (r *rejectRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.pos])
	r.pos++
	return nil
}

var (
	rejectRegisterN  int
	rejectRegisterMu sync.Mutex
)

// registerRejectDriver registers d under a fresh unique name (sql.Register
// panics on a duplicate name), mirroring valuefidelity_test.go's
// registerVFDriver.
func registerRejectDriver(t *testing.T, d rejectDriver) string {
	t.Helper()
	rejectRegisterMu.Lock()
	rejectRegisterN++
	name := "reject-fake-" + strconv.Itoa(rejectRegisterN)
	rejectRegisterMu.Unlock()
	sql.Register(name, d)
	return name
}

func newRejectProvider(t *testing.T, d rejectDriver) *Provider {
	t.Helper()
	name := registerRejectDriver(t, d)
	p, err := New(Config{Driver: name, DSN: "x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// connectionRefused mimics a driver flattening a genuine dial failure to a
// bare string (provider.IsNetUnreachable's documented fallback case) —
// deliberately NOT a typed *net.OpError, so this exercises the text-pattern
// path rather than the type-assertion path.
func connectionRefused() error {
	return errors.New("dial tcp 10.0.0.5:5432: connect: connection refused")
}

// ---- Query(): the query-pane SQL path (tx and non-tx/ClickHouse branches) ----

func TestQuery_EngineSyntaxRejection_Postgres_IsInvalidWithReason(t *testing.T) {
	t.Parallel()
	p := newRejectProvider(t, rejectDriver{queryErr: pgErr("42601", `syntax error at or near "SELEKT"`)})

	_, err := p.Query(context.Background(), "SELEKT * FROM things", provider.Page{})
	if !errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("Query(syntax error) = %v, want errors.Is(_, ErrInvalid) — a SQL typo is a rejection, not an outage", err)
	}
	if errors.Is(err, provider.ErrUpstream) {
		t.Fatalf("Query(syntax error) = %v, still classified as ErrUpstream (S1 regression)", err)
	}
	if !strings.Contains(err.Error(), `syntax error at or near "SELEKT"`) {
		t.Fatalf("Query(syntax error).Error() = %q, want it to carry the engine's reason", err.Error())
	}
}

func TestQuery_EngineSyntaxRejection_MySQL_IsInvalidWithReason(t *testing.T) {
	t.Parallel()
	p := newRejectProvider(t, rejectDriver{
		queryErr: mysqlErr(1064, "42000", "You have an error in your SQL syntax near 'SELEKT'"),
	})

	_, err := p.Query(context.Background(), "SELEKT * FROM things", provider.Page{})
	if !errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("Query(mysql syntax error) = %v, want errors.Is(_, ErrInvalid)", err)
	}
	if !strings.Contains(err.Error(), "SELEKT") {
		t.Fatalf("Query(mysql syntax error).Error() = %q, want it to carry the engine's reason", err.Error())
	}
}

func TestQuery_UntypedEngineRejection_ClickHouseNonTxBranch_IsInvalid(t *testing.T) {
	t.Parallel()
	// chDialect.supportsReadOnlyTx() is false, so Query() takes the non-tx
	// db.QueryContext branch directly (no transaction wraps a ClickHouse
	// query) — constructed directly like
	// TestInsertRow_MySQLDialect_LastInsertIdEchoesGeneratedKey does, since
	// this hermetic fake driver cannot borrow the literal "clickhouse" name.
	name := registerRejectDriver(t, rejectDriver{
		queryErr: errors.New(`code: 62, message: Syntax error: failed at position 7 ('SELEKT')`),
	})
	db, err := sql.Open(name, "x")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p := &Provider{db: db, d: chDialect{}, caps: provider.Capabilities{Family: provider.FamilyTabular}}

	_, err = p.Query(context.Background(), "SELEKT * FROM things", provider.Page{})
	if !errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("Query(clickhouse untyped rejection) = %v, want errors.Is(_, ErrInvalid) — an untyped engine error on a user-composed path still falls to ErrInvalid, never silently ErrUpstream", err)
	}
	if !strings.Contains(err.Error(), "Syntax error") {
		t.Fatalf("Query(clickhouse untyped rejection).Error() = %q, want it to carry the engine's reason", err.Error())
	}
}

func TestQuery_ConnectionRefused_StaysUpstream(t *testing.T) {
	t.Parallel()
	p := newRejectProvider(t, rejectDriver{queryErr: connectionRefused()})

	_, err := p.Query(context.Background(), "SELECT * FROM things", provider.Page{})
	if !errors.Is(err, provider.ErrUpstream) {
		t.Fatalf("Query(connection refused) = %v, want errors.Is(_, ErrUpstream) — a dead engine during a user query is still an outage", err)
	}
	if errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("Query(connection refused) = %v, misclassified as ErrInvalid", err)
	}
}

func TestQuery_ContextDeadlineExceeded_StaysUpstream(t *testing.T) {
	t.Parallel()
	p := newRejectProvider(t, rejectDriver{queryErr: context.DeadlineExceeded})

	_, err := p.Query(context.Background(), "SELECT * FROM things", provider.Page{})
	if !errors.Is(err, provider.ErrUpstream) {
		t.Fatalf("Query(context deadline) = %v, want errors.Is(_, ErrUpstream)", err)
	}
}

func TestQuery_PgConnectionExceptionClass_StaysUpstream(t *testing.T) {
	t.Parallel()
	// SQLSTATE class 08 (connection_exception) reaching this far as a
	// STRUCTURED PgError (e.g. the backend was terminated mid-session) is
	// still connectivity, not a rejection of the query text.
	p := newRejectProvider(t, rejectDriver{queryErr: pgErr("08006", "connection to server was lost")})

	_, err := p.Query(context.Background(), "SELECT * FROM things", provider.Page{})
	if !errors.Is(err, provider.ErrUpstream) {
		t.Fatalf("Query(pg class-08 error) = %v, want errors.Is(_, ErrUpstream)", err)
	}
	if errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("Query(pg class-08 error) = %v, misclassified as ErrInvalid", err)
	}
}

// ---- EditCell(): cell-edit values ----

func TestEditCell_NotNullViolation_Postgres_IsInvalidWithReason(t *testing.T) {
	t.Parallel()
	p := newRejectProvider(t, rejectDriver{
		execErr: pgErr("23502", `null value in column "email" violates not-null constraint`),
	})

	edit := provider.CellEdit{
		Path:   provider.Path{Segments: []string{"app", "things"}},
		RowKey: map[string]any{"id": "1"},
		Column: "email",
	}
	_, err := p.EditCell(context.Background(), edit)
	if !errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("EditCell(NOT NULL violation) = %v, want errors.Is(_, ErrInvalid)", err)
	}
	if !strings.Contains(err.Error(), "violates not-null constraint") {
		t.Fatalf("EditCell(NOT NULL violation).Error() = %q, want it to carry the engine's reason", err.Error())
	}
}

func TestEditCell_WrongTypeLiteral_MySQL_IsInvalidWithReason(t *testing.T) {
	t.Parallel()
	p := newRejectProvider(t, rejectDriver{
		execErr: mysqlErr(1366, "22007", "Incorrect integer value: 'abc' for column 'age' at row 1"),
	})

	edit := provider.CellEdit{
		Path:   provider.Path{Segments: []string{"app", "things"}},
		RowKey: map[string]any{"id": "1"},
		Column: "age",
	}
	_, err := p.EditCell(context.Background(), edit)
	if !errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("EditCell(wrong type literal) = %v, want errors.Is(_, ErrInvalid) — a letter typed into a numeric column is a rejection, not an outage", err)
	}
	if !strings.Contains(err.Error(), "Incorrect integer value") {
		t.Fatalf("EditCell(wrong type literal).Error() = %q, want it to carry the engine's reason", err.Error())
	}
}

func TestEditCell_ConnectionDropped_StaysUpstream(t *testing.T) {
	t.Parallel()
	p := newRejectProvider(t, rejectDriver{execErr: driver.ErrBadConn})

	edit := provider.CellEdit{
		Path:   provider.Path{Segments: []string{"app", "things"}},
		RowKey: map[string]any{"id": "1"},
		Column: "email",
	}
	_, err := p.EditCell(context.Background(), edit)
	if !errors.Is(err, provider.ErrUpstream) {
		t.Fatalf("EditCell(bad conn) = %v, want errors.Is(_, ErrUpstream)", err)
	}
}

// ---- InsertRow(): all three key-recovery sub-paths ----

func TestInsertRow_ConstraintViolation_CallerSuppliedKeyPath_IsInvalid(t *testing.T) {
	t.Parallel()
	p := newRejectProvider(t, rejectDriver{
		execErr: pgErr("23505", `duplicate key value violates unique constraint "things_pkey"`),
	})

	row := map[string]any{"id": "1", "email": "a@example.com"}
	_, err := p.InsertRow(context.Background(), provider.Path{Segments: []string{"app", "things"}}, row)
	if !errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("InsertRow(unique violation, caller key) = %v, want errors.Is(_, ErrInvalid)", err)
	}
	if !strings.Contains(err.Error(), "duplicate key value") {
		t.Fatalf("InsertRow(unique violation).Error() = %q, want it to carry the engine's reason", err.Error())
	}
}

func TestInsertRow_ConstraintViolation_ReturningPath_IsInvalid(t *testing.T) {
	t.Parallel()
	// No explicit "id" in row: pkFromRow fails, so InsertRow takes the
	// RETURNING round-trip (pgDialect is New()'s default for a non-mysql/
	// clickhouse driver name), and the engine rejects there instead.
	p := newRejectProvider(t, rejectDriver{
		pkCols:   []string{"id"},
		cols:     [][2]string{{"id", "integer"}, {"email", "text"}},
		queryErr: pgErr("23502", `null value in column "email" violates not-null constraint`),
	})

	row := map[string]any{"name": "new-row"}
	_, err := p.InsertRow(context.Background(), provider.Path{Segments: []string{"app", "things"}}, row)
	if !errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("InsertRow(NOT NULL violation, RETURNING path) = %v, want errors.Is(_, ErrInvalid)", err)
	}
	if !strings.Contains(err.Error(), "violates not-null constraint") {
		t.Fatalf("InsertRow(RETURNING path).Error() = %q, want it to carry the engine's reason", err.Error())
	}
}

func TestInsertRow_ConnectionRefused_PlainExecPath_MySQL_StaysUpstream(t *testing.T) {
	t.Parallel()
	// myDialect.returningClause is always "" and no explicit "id" is
	// supplied, so InsertRow falls to the plain-Exec/LastInsertId path
	// (constructed directly, mirroring
	// TestInsertRow_MySQLDialect_LastInsertIdEchoesGeneratedKey, since this
	// hermetic fake driver cannot borrow the literal "mysql" name).
	name := registerRejectDriver(t, rejectDriver{
		pkCols:  []string{"id"},
		cols:    [][2]string{{"id", "int"}, {"email", "varchar"}},
		execErr: connectionRefused(),
	})
	db, err := sql.Open(name, "x")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p := &Provider{db: db, d: myDialect{}, caps: provider.Capabilities{Family: provider.FamilyTabular}}

	row := map[string]any{"email": "a@example.com"}
	_, err = p.InsertRow(context.Background(), provider.Path{Segments: []string{"app", "things"}}, row)
	if !errors.Is(err, provider.ErrUpstream) {
		t.Fatalf("InsertRow(connection refused, plain-exec path) = %v, want errors.Is(_, ErrUpstream)", err)
	}
}

// ---- DeleteRow(): delete-row keys ----

func TestDeleteRow_ForeignKeyViolation_IsInvalidWithReason(t *testing.T) {
	t.Parallel()
	p := newRejectProvider(t, rejectDriver{
		execErr: pgErr("23503", `update or delete on table "things" violates foreign key constraint "orders_thing_id_fkey"`),
	})

	_, err := p.DeleteRow(context.Background(), provider.Path{Segments: []string{"app", "things"}}, map[string]any{"id": "1"})
	if !errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("DeleteRow(FK violation) = %v, want errors.Is(_, ErrInvalid)", err)
	}
	if !strings.Contains(err.Error(), "violates foreign key constraint") {
		t.Fatalf("DeleteRow(FK violation).Error() = %q, want it to carry the engine's reason", err.Error())
	}
}

func TestDeleteRow_BadConnection_StaysUpstream(t *testing.T) {
	t.Parallel()
	p := newRejectProvider(t, rejectDriver{execErr: driver.ErrBadConn})

	_, err := p.DeleteRow(context.Background(), provider.Path{Segments: []string{"app", "things"}}, map[string]any{"id": "1"})
	if !errors.Is(err, provider.ErrUpstream) {
		t.Fatalf("DeleteRow(bad conn) = %v, want errors.Is(_, ErrUpstream)", err)
	}
}

// ---- isConnectivityErr: pure classification unit tests ----

func TestIsConnectivityErr(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"context deadline exceeded", context.DeadlineExceeded, true},
		{"context canceled", context.Canceled, true},
		{"driver bad conn", driver.ErrBadConn, true},
		{"sql conn done", sql.ErrConnDone, true},
		{"net.OpError dial failure", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}, true},
		{"connection refused text", errors.New("dial tcp 10.0.0.5:5432: connect: connection refused"), true},
		{"pg class 08 connection exception", pgErr("08006", "connection to server was lost"), true},
		{"pg class 42 syntax error", pgErr("42601", `syntax error at or near "SELEKT"`), false},
		{"pg class 22 data exception", pgErr("22P02", "invalid input syntax for type integer"), false},
		{"pg class 23 constraint violation", pgErr("23502", "null value violates not-null constraint"), false},
		{"mysql class 08 connection exception", mysqlErr(2013, "08S01", "Lost connection to MySQL server during query"), true},
		{"mysql class 42 syntax error", mysqlErr(1064, "42000", "SQL syntax error"), false},
		{"mysql zero-value SQLState", mysqlErr(1146, "", "Table doesn't exist"), false},
		{"generic untyped engine error", errors.New(`code: 62, message: Syntax error`), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isConnectivityErr(tc.err); got != tc.want {
				t.Errorf("isConnectivityErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// ---- sanitizeEngineReason: bounded, single-line, credential-free ----

func TestSanitizeEngineReason_StripsControlCharacters(t *testing.T) {
	t.Parallel()
	got := sanitizeEngineReason(errors.New("line one\nline two\ttabbed"))
	if strings.ContainsAny(got, "\n\t") {
		t.Fatalf("sanitizeEngineReason(%q) = %q, want no control characters", "line one\nline two\ttabbed", got)
	}
}

func TestSanitizeEngineReason_CapsLength(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", maxEngineReasonLen*2)
	got := sanitizeEngineReason(errors.New(long))
	if len(got) > maxEngineReasonLen+len("...") {
		t.Fatalf("sanitizeEngineReason(long) length = %d, want <= %d", len(got), maxEngineReasonLen+3)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("sanitizeEngineReason(long) = %q, want a truncation suffix", got)
	}
}

func TestSanitizeEngineReason_RedactsConnectionStrings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		msg  string
		leak string
	}{
		{"url-shaped DSN", `dial failed: postgres://appuser:s3cr3t@10.0.0.5:5432/mydb`, "s3cr3t"},
		{"keyword/value DSN", `connect: host=10.0.0.5 password=s3cr3t user=appuser failed`, "s3cr3t"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := sanitizeEngineReason(errors.New(tc.msg))
			if strings.Contains(got, tc.leak) {
				t.Fatalf("sanitizeEngineReason(%q) = %q, leaked %q", tc.msg, got, tc.leak)
			}
		})
	}
}

func TestSanitizeEngineReason_PlainEngineTextPassesThroughUnredacted(t *testing.T) {
	t.Parallel()
	msg := `syntax error at or near "SELEKT"`
	if got := sanitizeEngineReason(errors.New(msg)); got != msg {
		t.Fatalf("sanitizeEngineReason(%q) = %q, want it unchanged (no DSN-shaped text to redact)", msg, got)
	}
}
