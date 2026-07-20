package tabular

import (
	"strconv"
	"strings"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// dialect captures the SQL differences between PostgreSQL and MySQL/MariaDB.
// All identifiers are quoted (never interpolated unescaped) and all values are
// passed as bound parameters — no value ever reaches a statement as text.
type dialect interface {
	schemasSQL() string
	tablesSQL() string  // 1 placeholder: schema
	columnsSQL() string // 2 placeholders: schema, table
	pkSQL() string      // 2 placeholders: schema, table
	quote(ident string) string
	qualify(schema, table string) string
	browseColumn(provider.Column) string
	nullSafeEq() string // for optimistic-concurrency: IS NOT DISTINCT FROM / <=>
	placeholder(n int) string
	// supportsReadOnlyTx reports whether the engine honours a READ ONLY
	// transaction (Postgres/MySQL yes; ClickHouse has no transactions, so its
	// Query relies on the connection's readonly=1 setting instead).
	supportsReadOnlyTx() bool
	// returningClause builds a "RETURNING <pkcols>" suffix for InsertRow to
	// echo a server-generated key in the same round-trip (T-AUD-03). Empty
	// pkCols or an engine with no RETURNING support (MySQL/MariaDB; the
	// caller falls back to LastInsertId instead) yields "".
	returningClause(pkCols []string) string
}

// ---- PostgreSQL ----

type pgDialect struct{}

func (pgDialect) schemasSQL() string {
	return `SELECT schema_name FROM information_schema.schemata
	        WHERE schema_name NOT IN ('pg_catalog','information_schema','pg_toast')
	          AND schema_name NOT LIKE 'pg_temp%'
	        ORDER BY schema_name`
}
func (pgDialect) tablesSQL() string {
	return `SELECT table_name FROM information_schema.tables
	        WHERE table_schema=$1 AND table_type IN ('BASE TABLE','VIEW')
	        ORDER BY table_name`
}
func (pgDialect) columnsSQL() string {
	return `SELECT column_name, data_type FROM information_schema.columns
	        WHERE table_schema=$1 AND table_name=$2 ORDER BY ordinal_position`
}
func (pgDialect) pkSQL() string {
	return `SELECT kcu.column_name
	        FROM information_schema.table_constraints tc
	        JOIN information_schema.key_column_usage kcu
	          ON tc.constraint_name=kcu.constraint_name
	         AND tc.table_schema=kcu.table_schema
	         AND tc.table_name=kcu.table_name
	        WHERE tc.constraint_type='PRIMARY KEY'
	          AND tc.table_schema=$1 AND tc.table_name=$2
	        ORDER BY kcu.ordinal_position`
}
func (pgDialect) quote(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}
func (d pgDialect) qualify(schema, table string) string {
	return d.quote(schema) + "." + d.quote(table)
}
func (d pgDialect) browseColumn(c provider.Column) string { return d.quote(c.Name) }
func (pgDialect) nullSafeEq() string                      { return "IS NOT DISTINCT FROM" }
func (pgDialect) placeholder(n int) string                { return "$" + strconv.Itoa(n) }
func (pgDialect) supportsReadOnlyTx() bool                { return true }
func (d pgDialect) returningClause(pkCols []string) string {
	if len(pkCols) == 0 {
		return ""
	}
	q := make([]string, len(pkCols))
	for i, c := range pkCols {
		q[i] = d.quote(c)
	}
	return "RETURNING " + strings.Join(q, ", ")
}

// ---- MySQL / MariaDB ----

type myDialect struct{}

func (myDialect) schemasSQL() string {
	return `SELECT schema_name FROM information_schema.schemata
	        WHERE schema_name NOT IN ('mysql','information_schema','performance_schema','sys')
	        ORDER BY schema_name`
}
func (myDialect) tablesSQL() string {
	return `SELECT table_name FROM information_schema.tables
	        WHERE table_schema=? ORDER BY table_name`
}
func (myDialect) columnsSQL() string {
	return `SELECT column_name, data_type FROM information_schema.columns
	        WHERE table_schema=? AND table_name=? ORDER BY ordinal_position`
}
func (myDialect) pkSQL() string {
	return `SELECT column_name FROM information_schema.key_column_usage
	        WHERE table_schema=? AND table_name=? AND constraint_name='PRIMARY'
	        ORDER BY ordinal_position`
}
func (myDialect) quote(ident string) string {
	return "`" + strings.ReplaceAll(ident, "`", "``") + "`"
}
func (d myDialect) qualify(schema, table string) string {
	return d.quote(schema) + "." + d.quote(table)
}
func (d myDialect) browseColumn(c provider.Column) string { return d.quote(c.Name) }
func (myDialect) nullSafeEq() string                      { return "<=>" }
func (myDialect) placeholder(n int) string                { return "?" }
func (myDialect) supportsReadOnlyTx() bool                { return true }

// returningClause: MySQL/MariaDB have no portable RETURNING across the
// versions this provider targets — InsertRow falls back to LastInsertId for
// a single-column PK instead (T-AUD-03).
func (myDialect) returningClause([]string) string { return "" }

// ---- ClickHouse (columnar; VIEW-ONLY — append-oriented, async mutations) ----

type chDialect struct{}

func (chDialect) schemasSQL() string {
	return `SELECT name FROM system.databases
	        WHERE name NOT IN ('system','information_schema','INFORMATION_SCHEMA')
	        ORDER BY name`
}
func (chDialect) tablesSQL() string {
	return `SELECT name FROM system.tables WHERE database = ? ORDER BY name`
}
func (chDialect) columnsSQL() string {
	return `SELECT name, type FROM system.columns WHERE database = ? AND table = ? ORDER BY position`
}
func (chDialect) pkSQL() string {
	return `SELECT name FROM system.columns
	        WHERE database = ? AND table = ? AND is_in_primary_key = 1 ORDER BY position`
}
func (chDialect) quote(ident string) string {
	return "`" + strings.ReplaceAll(ident, "`", "``") + "`"
}
func (d chDialect) qualify(schema, table string) string {
	return d.quote(schema) + "." + d.quote(table)
}
func (d chDialect) browseColumn(c provider.Column) string {
	quoted := d.quote(c.Name)
	if isClickHouseAggregateState(c.DataType) {
		return "toString(finalizeAggregation(" + quoted + ")) AS " + quoted
	}
	return quoted
}
func (chDialect) nullSafeEq() string              { return "=" } // unused: ClickHouse is view-only
func (chDialect) placeholder(n int) string        { return "?" }
func (chDialect) supportsReadOnlyTx() bool        { return false }
func (chDialect) returningClause([]string) string { return "" } // unused: ClickHouse is view-only

// placeholders generates positional bind markers in dialect order.
type placeholders struct {
	d dialect
	n int
}

func newPlaceholders(d dialect) *placeholders { return &placeholders{d: d} }
func (p *placeholders) next() string {
	p.n++
	return p.d.placeholder(p.n)
}
