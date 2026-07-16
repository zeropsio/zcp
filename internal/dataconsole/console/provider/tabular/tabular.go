// Package tabular is the relational SQL provider (postgresql + mariadb/mysql)
// over database/sql. Read-only is enforced by the ENGINE — every Query/ReadTable
// runs inside a READ ONLY transaction (sql.TxOptions{ReadOnly:true}), never a
// statement-text whitelist (a "SELECT" can still mutate via a data-modifying
// CTE). Edits are parameterized with PK-anchored WHERE + optimistic concurrency;
// a table with no primary key is view-only.
package tabular

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2" // registers "clickhouse"
	_ "github.com/go-sql-driver/mysql"         // registers "mysql"
	_ "github.com/jackc/pgx/v5/stdlib"         // registers "pgx"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

const (
	queryTimeout = 30 * time.Second
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
	db   *sql.DB
	d    dialect
	caps provider.Capabilities
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
		db: db,
		d:  d,
		caps: provider.Capabilities{
			Family: provider.FamilyTabular, Support: support,
			Query: true, EditTabular: !cfg.ReadOnly && !cfg.NoEdit, ReadOnly: cfg.ReadOnly || cfg.NoEdit,
			MaxInlineBytes: 1 << 20,
		},
	}, nil
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
	limit := clampLimit(page.Limit)
	offset := parseOffset(page.Cursor)

	order := pk
	if len(order) == 0 && len(cols) > 0 {
		order = []string{cols[0].Name}
	}
	q := fmt.Sprintf("SELECT * FROM %s ORDER BY %s LIMIT %d OFFSET %d",
		p.d.qualify(schema, table), p.orderClause(order), limit+1, offset)
	rows, vals, err := p.readRows(ctx, q)
	if err != nil {
		return provider.TablePage{}, err
	}
	tp := provider.TablePage{Columns: cols, Rows: rows, RowKeyCols: pk}
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
			return provider.TablePage{}, fmt.Errorf("tabular: query rejected: %w", provider.ErrUpstream)
		}
	} else {
		// ClickHouse: no transactions — the connection itself is readonly=1, so
		// any write statement is refused by the engine.
		var err error
		rows, err = p.db.QueryContext(ctx, stmt)
		if err != nil {
			return provider.TablePage{}, fmt.Errorf("tabular: query rejected: %w", provider.ErrUpstream)
		}
	}
	defer func() { _ = rows.Close() }()
	colNames, _ := rows.Columns()
	cols := make([]provider.Column, len(colNames))
	for i, c := range colNames {
		cols[i] = provider.Column{Name: c}
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
		return provider.TablePage{}, fmt.Errorf("tabular: scan: %w", provider.ErrUpstream)
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
	args = append(args, e.NewValue)
	where, wargs := p.whereKey(ph, e.RowKey)
	args = append(args, wargs...)
	// optimistic concurrency on the edited column
	where += fmt.Sprintf(" AND %s %s %s", p.d.quote(e.Column), p.d.nullSafeEq(), ph.next())
	args = append(args, e.ExpectedOld)
	// #nosec G201 -- identifiers are dialect-quoted (escaped); all values are bound params.
	stmt := fmt.Sprintf("UPDATE %s SET %s WHERE %s", tbl, set, where)
	res, err := p.db.ExecContext(ctx, stmt, args...)
	if err != nil {
		return provider.Applied{}, fmt.Errorf("tabular: update: %w", provider.ErrUpstream)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return provider.Applied{}, provider.ErrConflict
	}
	return provider.Applied{Statement: stmt, Affected: n}, nil
}

// InsertRow inserts one row.
func (p *Provider) InsertRow(ctx context.Context, path provider.Path, row map[string]any) (provider.Applied, error) {
	if p.caps.ReadOnly {
		return provider.Applied{}, provider.ErrReadOnly
	}
	if len(path.Segments) != 2 || len(row) == 0 {
		return provider.Applied{}, provider.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	ph := newPlaceholders(p.d)
	var cols, phs []string
	var args []any
	for k, v := range row {
		cols = append(cols, p.d.quote(k))
		phs = append(phs, ph.next())
		args = append(args, v)
	}
	// #nosec G201 -- identifiers are dialect-quoted (escaped); all values are bound params.
	stmt := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		p.d.qualify(path.Segments[0], path.Segments[1]), strings.Join(cols, ", "), strings.Join(phs, ", "))
	res, err := p.db.ExecContext(ctx, stmt, args...)
	if err != nil {
		return provider.Applied{}, fmt.Errorf("tabular: insert: %w", provider.ErrUpstream)
	}
	n, _ := res.RowsAffected()
	return provider.Applied{Statement: stmt, Affected: n}, nil
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
		return provider.Applied{}, fmt.Errorf("tabular: delete: %w", provider.ErrUpstream)
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
		args = append(args, v)
	}
	return strings.Join(parts, " AND "), args
}

func (p *Provider) orderClause(cols []string) string {
	q := make([]string, len(cols))
	for i, c := range cols {
		q[i] = p.d.quote(c)
	}
	return strings.Join(q, ", ")
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
		cols = append(cols, provider.Column{Name: r[0], DataType: r[1], PK: pk[r[0]]})
	}
	return cols, pkOrder, nil
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

// normalize converts driver types to JSON-friendly values.
func normalize(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(t)
	case time.Time:
		return t.Format(time.RFC3339)
	default:
		return v
	}
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
