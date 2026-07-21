// Package tabular is the relational SQL provider (postgresql + mariadb/mysql)
// over database/sql. Read-only is enforced by the ENGINE, never a
// statement-text whitelist (a "SELECT" can still mutate via a data-modifying
// CTE) — but that guard applies only where it is needed: Query runs an
// arbitrary user-supplied statement, so it wraps it in a READ ONLY transaction
// (sql.TxOptions{ReadOnly:true}). ReadTable never runs user text — schema/table
// are dialect-quoted identifiers, not a caller-supplied statement — so it reads
// via a plain QueryContext, no transaction needed. Edits are parameterized with
// PK-anchored WHERE + optimistic concurrency; a table with no primary key is
// view-only.
package tabular

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2" // registers "clickhouse"
	_ "github.com/go-sql-driver/mysql"         // registers "mysql"
	_ "github.com/jackc/pgx/v5/stdlib"         // registers "pgx"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

const (
	queryTimeout = 30 * time.Second
	countBudget  = 5 * time.Second
	// defaultLimit is the page cap when callers omit or zero Page.Limit.
	defaultLimit = 100
	maxLimit     = 1000

	driverPGX        = "pgx"
	driverMySQL      = "mysql"
	driverClickHouse = "clickhouse"

	dialectPostgreSQL = "postgresql"
	dialectMariaDB    = "mariadb"
	dialectMySQL      = driverMySQL
	dialectClickHouse = driverClickHouse
)

// Config is the resolved SQL connection.
type Config struct {
	Conn     provider.SQLConn
	Driver   string // "pgx" | "mysql" | "clickhouse"
	DSN      string
	ReadOnly bool
	NoEdit   bool // force view-only (ClickHouse: mutations are async ALTER, not a cell edit)
}

// Provider drives one SQL database.
type Provider struct {
	db           *sql.DB
	d            dialect
	caps         provider.Capabilities
	countTimeout time.Duration
	counting     atomic.Bool
}

// New opens the pool. Production callers pass Conn and this provider builds the
// driver DSN it owns; Driver/DSN remain for package tests with fake SQL drivers.
func New(cfg Config) (*Provider, error) {
	var err error
	cfg, err = normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(cfg.Driver, cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("tabular: open: %w", provider.ErrUpstream)
	}
	db.SetMaxOpenConns(4)
	db.SetConnMaxIdleTime(5 * time.Minute)
	var d dialect
	switch cfg.Driver {
	case driverMySQL:
		d = myDialect{}
	case driverClickHouse:
		d = chDialect{}
	default:
		d = pgDialect{}
	}
	support := provider.SupportFull
	if cfg.NoEdit {
		support = provider.SupportViewOnly
	}
	return &Provider{
		db:           db,
		d:            d,
		countTimeout: countBudget,
		caps: provider.Capabilities{
			Family: provider.FamilyTabular, Support: support,
			Query: true, EditTabular: !cfg.ReadOnly && !cfg.NoEdit, ReadOnly: cfg.ReadOnly || cfg.NoEdit,
			MaxInlineBytes: 1 << 20,
		},
	}, nil
}

// CountTable returns the exact relation count independently from ReadTable.
func (p *Provider) CountTable(ctx context.Context, path provider.Path) (int64, error) {
	if len(path.Segments) != 2 {
		return 0, fmt.Errorf("tabular: count path: %w", provider.ErrInvalid)
	}
	if !p.counting.CompareAndSwap(false, true) {
		return 0, fmt.Errorf("tabular: count busy: %w", provider.ErrConflict)
	}
	defer p.counting.Store(false)
	timeout := p.countTimeout
	if timeout <= 0 {
		timeout = countBudget
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cols, _, err := p.columnsAndPK(ctx, path.Segments[0], path.Segments[1])
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, countContextErr(ctxErr)
		}
		return 0, err
	}
	if len(cols) == 0 {
		return 0, provider.ErrNotFound
	}
	// #nosec G201 -- both identifiers come from discovered relation metadata and
	// are escaped by the active dialect's identifier quoter before interpolation.
	q := fmt.Sprintf("SELECT count(*) FROM %s", p.d.qualify(path.Segments[0], path.Segments[1]))
	var count int64
	if err := p.db.QueryRowContext(ctx, q).Scan(&count); err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return 0, countContextErr(err)
		}
		return 0, fmt.Errorf("tabular: count: %w", provider.ErrUpstream)
	}
	return count, nil
}

func countContextErr(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("tabular: count: %w: %w", provider.ErrTimeout, context.DeadlineExceeded)
	}
	return fmt.Errorf("tabular: count: %w", context.Canceled)
}

func normalizeConfig(cfg Config) (Config, error) {
	if cfg.Driver != "" || cfg.DSN != "" {
		if cfg.Driver == "" || cfg.DSN == "" {
			return Config{}, fmt.Errorf("tabular: %w: driver and dsn must be set together", provider.ErrInvalid)
		}
		return cfg, nil
	}
	conn := cfg.Conn
	driver := strings.ToLower(strings.TrimSpace(conn.Driver))
	dialect := strings.ToLower(strings.TrimSpace(conn.Dialect))
	if dialect == "" {
		switch driver {
		case driverPGX:
			dialect = dialectPostgreSQL
		case driverMySQL:
			dialect = dialectMySQL
		case driverClickHouse:
			dialect = dialectClickHouse
		}
	}
	if driver == "" {
		switch dialect {
		case dialectPostgreSQL:
			driver = driverPGX
		case dialectMariaDB, dialectMySQL:
			driver = driverMySQL
		case dialectClickHouse:
			driver = driverClickHouse
		}
	}
	hostPort := net.JoinHostPort(conn.Host, conn.Port)
	switch dialect {
	case dialectPostgreSQL:
		cfg.Driver = driver
		cfg.DSN = fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable",
			url.QueryEscape(conn.User), url.QueryEscape(conn.Password), hostPort, conn.Database)
	case dialectMariaDB, dialectMySQL:
		cfg.Driver = driver
		cfg.DSN = fmt.Sprintf("%s:%s@tcp(%s)/%s", conn.User, conn.Password, hostPort, conn.Database)
	case dialectClickHouse:
		cfg.Driver = driver
		cfg.NoEdit = true
		cfg.DSN = fmt.Sprintf("clickhouse://%s:%s@%s/%s?readonly=1",
			url.QueryEscape(conn.User), url.QueryEscape(conn.Password), hostPort, conn.Database)
	default:
		return Config{}, fmt.Errorf("tabular: %w: unknown dialect %q", provider.ErrInvalid, conn.Dialect)
	}
	if cfg.Driver == "" {
		return Config{}, fmt.Errorf("tabular: %w: missing driver for dialect %q", provider.ErrInvalid, dialect)
	}
	return cfg, nil
}

func (p *Provider) Kind() string                { return "sql" }
func (p *Provider) Caps() provider.Capabilities { return p.caps }
func (p *Provider) Close() error                { return p.db.Close() }

// Health forces an authenticated round-trip (the preflight: VPN up AND the
// handshake works, not just a TCP open).
func (p *Provider) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	if err := p.db.PingContext(ctx); err != nil {
		return provider.HealthErr("tabular: ping", err)
	}
	return nil
}

// List: [] → schemas; [schema] → tables. Uses offset cursors because the
// metadata queries are already ordered by stable schema/table name.
func (p *Provider) List(ctx context.Context, path provider.Path, page provider.Page) ([]provider.Node, string, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	limit := clampLimit(page.Limit)
	offset := parseOffset(page.Cursor)
	switch len(path.Segments) {
	case 0:
		names, next, err := p.queryStringsPage(ctx, p.d.schemasSQL(), limit, offset)
		if err != nil {
			return nil, "", err
		}
		return containerNodes(path, names), next, nil
	case 1:
		names, next, err := p.queryStringsPage(ctx, p.d.tablesSQL(), limit, offset, path.Segments[0])
		if err != nil {
			return nil, "", err
		}
		nodes := make([]provider.Node, 0, len(names))
		for _, n := range names {
			nodes = append(nodes, provider.Node{Name: n, Kind: provider.KindTabular, Path: child(path, n)})
		}
		return nodes, next, nil
	default:
		return nil, "", nil
	}
}

// Stat confirms existence of one [schema, table] and returns it as a
// KindTabular node — the tabular half of "every family answers /api/stat
// uniformly" (before this, TabularProvider had no Stat method at all and
// /api/stat on any table 422'd). It reuses columnsAndPK's own not-found
// detection (a table with zero columns does not exist — see ReadTable's
// comment above) rather than issuing the row SELECT. No cheap NodeMeta fact
// exists for a SQL table (counting rows is a full scan, not "where cheap" —
// NodeMeta.Count's documented bar), so Meta stays nil.
func (p *Provider) Stat(ctx context.Context, path provider.Path) (provider.Node, error) {
	if len(path.Segments) != 2 {
		return provider.Node{}, fmt.Errorf("tabular: stat: %w: expected [schema, table]", provider.ErrInvalid)
	}
	schema, table := path.Segments[0], path.Segments[1]
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	cols, _, err := p.columnsAndPK(ctx, schema, table)
	if err != nil {
		return provider.Node{}, err
	}
	if len(cols) == 0 {
		return provider.Node{}, provider.ErrNotFound
	}
	return provider.Node{Name: table, Kind: provider.KindTabular, Path: path}, nil
}

// ReadTable pages rows of [schema, table] (offset cursor, hard cap), with column
// + PK metadata. RowKeyCols empty ⇒ the UI marks the table view-only.
func (p *Provider) ReadTable(ctx context.Context, path provider.Path, page provider.Page) (provider.TablePage, error) {
	if len(path.Segments) != 2 {
		return provider.TablePage{}, fmt.Errorf("tabular: path: %w", provider.ErrInvalid)
	}
	schema, table := path.Segments[0], path.Segments[1]
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	cols, pk, err := p.columnsAndPK(ctx, schema, table)
	if err != nil {
		return provider.TablePage{}, err
	}
	if len(cols) == 0 {
		// information_schema (or the dialect equivalent) returned no columns
		// at all for [schema, table] — the relation does not exist. Detecting
		// this BEFORE issuing the row SELECT avoids depending on a
		// driver-specific "relation does not exist" error string (which
		// differs across postgres/mariadb/clickhouse and would otherwise have
		// to be pattern-matched, violating the sanitized-message discipline);
		// every real table has at least one column, so an empty result is
		// unambiguous. Previously the SELECT ran anyway and failed at the
		// engine, flattening into the same 502 upstream as a genuine outage
		// (T-AUD-06).
		return provider.TablePage{}, provider.ErrNotFound
	}
	limit := clampLimit(page.Limit)
	offset := parseOffset(page.Cursor)

	order, err := p.tableOrder(cols, pk, page.Sort)
	if err != nil {
		return provider.TablePage{}, err
	}
	q := fmt.Sprintf("SELECT %s FROM %s", p.projection(cols), p.d.qualify(schema, table))
	if len(order) > 0 {
		q += " ORDER BY " + strings.Join(order, ", ")
	}
	q += fmt.Sprintf(" LIMIT %d OFFSET %d", limit+1, offset)
	rows, vals, err := p.readRows(ctx, q)
	if err != nil {
		return provider.TablePage{}, err
	}
	tp := provider.TablePage{Columns: cols, Rows: rows, RowKeyCols: pk, BestEffort: len(pk) == 0, Numbered: true}
	if len(vals) > limit {
		tp.Rows = tp.Rows[:limit]
		tp.NextCursor = strconv.Itoa(offset + limit)
	}
	return tp, nil
}

// Query runs a user statement inside a READ ONLY transaction — engine-enforced,
// so a data-modifying CTE cannot write. Pagination uses an offset cursor over
// the user statement's result; callers that need stable multi-page ordering
// should include ORDER BY in the query.
func (p *Provider) Query(ctx context.Context, stmt string, page provider.Page) (provider.TablePage, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	limit := clampLimit(page.Limit)
	offset := parseOffset(page.Cursor)
	stmt = pagedQuery(stmt, limit+1, offset)
	var rows *sql.Rows
	if p.d.supportsReadOnlyTx() {
		tx, err := p.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return provider.TablePage{}, fmt.Errorf("tabular: begin: %w", provider.ErrUpstream)
		}
		defer func() { _ = tx.Rollback() }()
		rows, err = tx.QueryContext(ctx, stmt)
		if err != nil {
			return provider.TablePage{}, engineErr("query", err)
		}
	} else {
		// ClickHouse: no transactions — the connection itself is readonly=1, so
		// any write statement is refused by the engine.
		var err error
		rows, err = p.db.QueryContext(ctx, stmt)
		if err != nil {
			return provider.TablePage{}, engineErr("query", err)
		}
	}
	defer func() { _ = rows.Close() }()
	colNames, _ := rows.Columns()
	cols := make([]provider.Column, len(colNames))
	for i, c := range colNames {
		// Query results carry no dataType/pk and RowKeyCols is always nil
		// below (Query is read-only by contract) — the grid MUST render as
		// explicitly non-editable, never as an editable grid that silently
		// ignores clicks (tabular.md §4, U-01).
		cols[i] = provider.Column{Name: c, Editable: false, Reason: "query results are read-only"}
	}
	out := [][]any{}
	for rows.Next() && len(out) < limit+1 {
		row, scanErr := scanRow(rows, len(colNames))
		if scanErr != nil {
			return provider.TablePage{}, fmt.Errorf("tabular: scan: %w", provider.ErrUpstream)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return provider.TablePage{}, engineErr("query", err)
	}
	tp := provider.TablePage{Columns: cols, Rows: out, RowKeyCols: nil}
	if len(out) > limit {
		tp.Rows = tp.Rows[:limit]
		tp.NextCursor = strconv.Itoa(offset + limit)
	}
	return tp, nil
}

// EditCell updates one cell, PK-anchored + optimistic (expectedOld). 0 rows
// affected ⇒ ErrConflict (row changed or gone).
func (p *Provider) EditCell(ctx context.Context, e provider.CellEdit) (provider.Applied, error) {
	if p.caps.ReadOnly {
		return provider.Applied{}, provider.ErrReadOnly
	}
	if len(e.Path.Segments) != 2 || len(e.RowKey) == 0 {
		return provider.Applied{}, fmt.Errorf("tabular: edit needs schema.table + a primary key: %w", provider.ErrUnsupported)
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	tbl := p.d.qualify(e.Path.Segments[0], e.Path.Segments[1])
	args := make([]any, 0, len(e.RowKey)+2)
	ph := newPlaceholders(p.d)
	set := fmt.Sprintf("%s = %s", p.d.quote(e.Column), ph.next())
	args = append(args, bindArg(e.NewValue))
	where, wargs := p.whereKey(ph, e.RowKey)
	args = append(args, wargs...)
	// optimistic concurrency on the edited column
	where += fmt.Sprintf(" AND %s %s %s", p.d.quote(e.Column), p.d.nullSafeEq(), ph.next())
	args = append(args, bindArg(e.ExpectedOld))
	// #nosec G201 -- identifiers are dialect-quoted (escaped); all values are bound params.
	stmt := fmt.Sprintf("UPDATE %s SET %s WHERE %s", tbl, set, where)
	res, err := p.db.ExecContext(ctx, stmt, args...)
	if err != nil {
		return provider.Applied{}, engineErr("update", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return provider.Applied{}, provider.ErrConflict
	}
	return provider.Applied{Statement: stmt, Affected: n}, nil
}

// InsertRow inserts one row. Applied.Key echoes the new row's primary key
// whenever it can be determined honestly (T-AUD-03), so a caller can address
// the row for a follow-up edit/delete without a separate lookup: if the
// caller already supplied every PK column's value in row, that value is
// echoed directly (no ambiguity, no extra round-trip); otherwise a
// server-generated key is recovered via RETURNING (Postgres) or
// LastInsertId (MySQL/MariaDB, single-column PK only — a multi-column PK
// with no caller-supplied value cannot be recovered this way, so Key stays
// nil rather than guessing).
func (p *Provider) InsertRow(ctx context.Context, path provider.Path, row map[string]any) (provider.Applied, error) {
	if p.caps.ReadOnly {
		return provider.Applied{}, provider.ErrReadOnly
	}
	if len(path.Segments) != 2 || len(row) == 0 {
		return provider.Applied{}, provider.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	_, pk, err := p.columnsAndPK(ctx, path.Segments[0], path.Segments[1])
	if err != nil {
		return provider.Applied{}, err
	}

	ph := newPlaceholders(p.d)
	var cols, phs []string
	var args []any
	for k, v := range row {
		cols = append(cols, p.d.quote(k))
		phs = append(phs, ph.next())
		args = append(args, bindArg(v))
	}
	// #nosec G201 -- identifiers are dialect-quoted (escaped); all values are bound params.
	insert := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		p.d.qualify(path.Segments[0], path.Segments[1]), strings.Join(cols, ", "), strings.Join(phs, ", "))

	if key, ok := pkFromRow(pk, row); ok {
		res, err := p.db.ExecContext(ctx, insert, args...)
		if err != nil {
			return provider.Applied{}, engineErr("insert", err)
		}
		n, _ := res.RowsAffected()
		return provider.Applied{Statement: insert, Affected: n, Key: key}, nil
	}

	if returning := p.d.returningClause(pk); returning != "" {
		stmt := insert + " " + returning
		vals := make([]any, len(pk))
		ptrs := make([]any, len(pk))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := p.db.QueryRowContext(ctx, stmt, args...).Scan(ptrs...); err != nil {
			return provider.Applied{}, engineErr("insert", err)
		}
		key := make(map[string]any, len(pk))
		for i, col := range pk {
			key[col] = normalize(vals[i])
		}
		return provider.Applied{Statement: stmt, Affected: 1, Key: key}, nil
	}

	res, err := p.db.ExecContext(ctx, insert, args...)
	if err != nil {
		return provider.Applied{}, engineErr("insert", err)
	}
	n, _ := res.RowsAffected()
	applied := provider.Applied{Statement: insert, Affected: n}
	if len(pk) == 1 {
		if id, idErr := res.LastInsertId(); idErr == nil && id != 0 {
			applied.Key = map[string]any{pk[0]: id}
		}
	}
	return applied, nil
}

// pkFromRow reports whether row already carries an explicit value for every
// PK column, returning that value set verbatim (untouched by bindArg, so a
// json.Number PK value re-marshals with full precision on the way back out).
func pkFromRow(pk []string, row map[string]any) (map[string]any, bool) {
	if len(pk) == 0 {
		return nil, false
	}
	key := make(map[string]any, len(pk))
	for _, col := range pk {
		v, ok := row[col]
		if !ok {
			return nil, false
		}
		key[col] = v
	}
	return key, true
}

// DeleteRow deletes one row by its PK.
func (p *Provider) DeleteRow(ctx context.Context, path provider.Path, key map[string]any) (provider.Applied, error) {
	if p.caps.ReadOnly {
		return provider.Applied{}, provider.ErrReadOnly
	}
	if len(path.Segments) != 2 || len(key) == 0 {
		return provider.Applied{}, fmt.Errorf("tabular: delete needs a primary key: %w", provider.ErrUnsupported)
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	ph := newPlaceholders(p.d)
	where, args := p.whereKey(ph, key)
	// #nosec G201 -- identifiers are dialect-quoted (escaped); all values are bound params.
	stmt := fmt.Sprintf("DELETE FROM %s WHERE %s", p.d.qualify(path.Segments[0], path.Segments[1]), where)
	res, err := p.db.ExecContext(ctx, stmt, args...)
	if err != nil {
		return provider.Applied{}, engineErr("delete", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return provider.Applied{}, provider.ErrConflict
	}
	return provider.Applied{Statement: stmt, Affected: n}, nil
}

// ---- internals ----

func (p *Provider) whereKey(ph *placeholders, key map[string]any) (string, []any) {
	parts := make([]string, 0, len(key))
	args := make([]any, 0, len(key))
	for k, v := range key {
		parts = append(parts, fmt.Sprintf("%s = %s", p.d.quote(k), ph.next()))
		args = append(args, bindArg(v))
	}
	return strings.Join(parts, " AND "), args
}

func (p *Provider) projection(cols []provider.Column) string {
	q := make([]string, len(cols))
	for i, c := range cols {
		q[i] = p.d.browseColumn(c)
	}
	return strings.Join(q, ", ")
}

func (p *Provider) tableOrder(cols []provider.Column, pk []string, sortDesc *provider.Sort) ([]string, error) {
	if sortDesc != nil {
		var selected *provider.Column
		for i := range cols {
			if cols[i].Name == sortDesc.Column {
				selected = &cols[i]
				break
			}
		}
		if selected == nil || !selected.Sortable || (sortDesc.Direction != provider.SortAsc && sortDesc.Direction != provider.SortDesc) {
			return nil, fmt.Errorf("tabular: sort descriptor: %w", provider.ErrInvalid)
		}
		order := []string{p.d.quote(selected.Name) + " " + strings.ToUpper(string(sortDesc.Direction))}
		for _, name := range pk {
			if name != selected.Name {
				order = append(order, p.d.quote(name)+" ASC")
			}
		}
		return order, nil
	}
	if len(pk) > 0 {
		order := make([]string, len(pk))
		for i, name := range pk {
			order[i] = p.d.quote(name) + " ASC"
		}
		return order, nil
	}
	for _, col := range cols {
		if col.Sortable {
			return []string{p.d.quote(col.Name) + " ASC"}, nil
		}
	}
	return nil, nil
}

func (p *Provider) columnsAndPK(ctx context.Context, schema, table string) ([]provider.Column, []string, error) {
	pk := map[string]bool{}
	pkRows, err := p.queryPairs(ctx, p.d.pkSQL(), schema, table)
	if err != nil {
		return nil, nil, err
	}
	var pkOrder []string
	for _, r := range pkRows {
		pk[r[0]] = true
		pkOrder = append(pkOrder, r[0])
	}
	colRows, err := p.queryPairs(ctx, p.d.columnsSQL(), schema, table)
	if err != nil {
		return nil, nil, err
	}
	cols := make([]provider.Column, 0, len(colRows))
	for _, r := range colRows {
		isPK := pk[r[0]]
		editable, reason := provider.ColumnEditability(isPK)
		sortable, sortReason := p.d.columnSortability(r[1])
		if p.caps.Support == provider.SupportViewOnly {
			// A view-only-tier table (clickhouse: mutations are async ALTER,
			// not a cell edit) is non-editable in full, PK-ness notwithstanding
			// — the engine-level refusal fires even with a valid write token
			// (tabular.md §1.3), so the column-level signal must agree.
			editable, reason = false, "view-only"
		}
		cols = append(cols, provider.Column{
			Name: r[0], DataType: r[1], PK: isPK, Editable: editable, Reason: reason,
			Sortable: sortable, SortReason: sortReason,
		})
	}
	return cols, pkOrder, nil
}

func isClickHouseAggregateState(dataType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(dataType)), "aggregatefunction(")
}

func (p *Provider) readRows(ctx context.Context, q string) ([][]any, []struct{}, error) {
	rows, err := p.db.QueryContext(ctx, q)
	if err != nil {
		return nil, nil, fmt.Errorf("tabular: read: %w", provider.ErrUpstream)
	}
	defer func() { _ = rows.Close() }()
	cols, _ := rows.Columns()
	var out [][]any
	for rows.Next() {
		row, scanErr := scanRow(rows, len(cols))
		if scanErr != nil {
			return nil, nil, fmt.Errorf("tabular: scan: %w", provider.ErrUpstream)
		}
		out = append(out, row)
	}
	return out, make([]struct{}, len(out)), nil
}

func (p *Provider) queryStrings(ctx context.Context, q string, args ...any) ([]string, error) {
	rows, err := p.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("tabular: %w", provider.ErrUpstream)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("tabular: scan: %w", provider.ErrUpstream)
		}
		out = append(out, s)
	}
	return out, nil
}

func (p *Provider) queryStringsPage(ctx context.Context, q string, limit, offset int, args ...any) ([]string, string, error) {
	// #nosec G201 -- pagination values are clamped ints; q is provider-owned SQL.
	paged := fmt.Sprintf("%s LIMIT %d OFFSET %d", q, limit+1, offset)
	names, err := p.queryStrings(ctx, paged, args...)
	if err != nil {
		return nil, "", err
	}
	if len(names) > limit {
		return names[:limit], strconv.Itoa(offset + limit), nil
	}
	return names, "", nil
}

func (p *Provider) queryPairs(ctx context.Context, q string, args ...any) ([][2]string, error) {
	rows, err := p.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("tabular: %w", provider.ErrUpstream)
	}
	defer func() { _ = rows.Close() }()
	var out [][2]string
	for rows.Next() {
		var a string
		var b sql.NullString
		if err := rows.Scan(&a, &b); err != nil {
			// single-column query (pk) — retry with one dest
			return p.queryPairsSingle(ctx, q, args...)
		}
		out = append(out, [2]string{a, b.String})
	}
	return out, nil
}

func (p *Provider) queryPairsSingle(ctx context.Context, q string, args ...any) ([][2]string, error) {
	names, err := p.queryStrings(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	out := make([][2]string, len(names))
	for i, n := range names {
		out[i] = [2]string{n, ""}
	}
	return out, nil
}

func scanRow(rows *sql.Rows, n int) ([]any, error) {
	vals := make([]any, n)
	ptrs := make([]any, n)
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	out := make([]any, n)
	for i, v := range vals {
		out[i] = normalize(v)
	}
	return out, nil
}

// normalize converts driver types to JSON-friendly values. time.Time uses
// RFC3339Nano (not RFC3339): a plain RFC3339 has no fractional-second
// component, so the value handed back to a caller never matches a
// microsecond-precision timestamptz/DATETIME column, and using that
// truncated value as a subsequent editCell's expectedOld spuriously 409s —
// optimistic concurrency was permanently broken for any now()-populated
// column (T-AUD-01). RFC3339Nano preserves whatever sub-second precision the
// driver actually returned (and cleanly omits it for a whole-second value,
// same as RFC3339 would).
func normalize(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(t)
	case time.Time:
		return t.Format(time.RFC3339Nano)
	default:
		return v
	}
}

// bindArg converts a decoded JSON value to the form bound to the SQL driver.
// The server decodes request bodies with json.Decoder.UseNumber(), so a bare
// JSON integer/decimal arrives as json.Number (the exact source decimal
// text) rather than float64 — bindArg carries that text through to the
// driver as a native Go string (pgx/mysql both parse a string parameter
// against a numeric column directly from its decimal text, never through a
// float64 intermediate) so an int64-range value never re-enters the lossy
// IEEE-754 double rounding a bare JSON number would otherwise take on its
// way to an interface{} (T-AUD-02: proven ±1 corruption above 2^53). Every
// other decoded type passes through unchanged.
func bindArg(v any) any {
	if n, ok := v.(json.Number); ok {
		return string(n)
	}
	return v
}

func containerNodes(parent provider.Path, names []string) []provider.Node {
	out := make([]provider.Node, 0, len(names))
	for _, n := range names {
		out = append(out, provider.Node{Name: n, Kind: provider.KindContainer, Path: child(parent, n), HasChildren: true})
	}
	return out
}

func child(parent provider.Path, name string) provider.Path {
	seg := make([]string, len(parent.Segments)+1)
	copy(seg, parent.Segments)
	seg[len(parent.Segments)] = name
	return provider.Path{Service: parent.Service, Segments: seg}
}

func clampLimit(l int) int {
	if l <= 0 {
		return defaultLimit
	}
	if l > maxLimit {
		return maxLimit
	}
	return l
}

func parseOffset(cursor string) int {
	offset, err := strconv.Atoi(cursor)
	if err != nil || offset < 0 {
		return 0
	}
	return offset
}

func pagedQuery(stmt string, limit, offset int) string {
	trimmed := strings.TrimRight(strings.TrimSpace(stmt), "; \t\r\n")
	// #nosec G201 -- stmt is intentionally user SQL, executed only inside the
	// provider's engine-enforced READ ONLY query path; limit/offset are clamped ints.
	return fmt.Sprintf("SELECT * FROM (%s) AS zcp_query_page LIMIT %d OFFSET %d", trimmed, limit, offset)
}
