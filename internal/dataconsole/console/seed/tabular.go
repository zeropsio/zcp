package seed

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2" // sql.Open("clickhouse", ...) driver registration
	_ "github.com/go-sql-driver/mysql"         // sql.Open("mysql", ...) driver registration
	_ "github.com/jackc/pgx/v5/stdlib"         // sql.Open("pgx", ...) driver registration

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

const sqlOpTimeout = 30 * time.Second

// Table identifiers below are always either a hardcoded literal (static
// mode) or namespace+"_"+literal (TabularName), and Service/Cleanup reject
// any namespace failing ValidNamespace before either seed or cleanup ever
// runs — so every %s used as a bare SQL identifier here is drawn from a
// charset with no quote, semicolon, or whitespace, never from
// unvalidated/attacker-reachable input. SQL has no placeholder syntax for
// identifiers (only values), so composed-but-validated is the correct
// pattern, not a shortcut.

func seedPostgres(ctx context.Context, conn provider.SQLConn, opts Options) error {
	dsn := fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable",
		url.QueryEscape(conn.User), url.QueryEscape(conn.Password),
		net.JoinHostPort(conn.Host, conn.Port), conn.Database)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("seed postgres: open: %w", err)
	}
	defer db.Close()

	customers := TabularName(opts.Namespace, "dc_customers")
	orders := TabularName(opts.Namespace, "dc_orders")
	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (id serial PRIMARY KEY, name text NOT NULL, email text, created timestamptz DEFAULT now())`, customers),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (id serial PRIMARY KEY, customer_id int, total numeric(10,2), status text DEFAULT 'pending')`, orders),
		fmt.Sprintf(`INSERT INTO %s (name, email) SELECT * FROM (VALUES ('Alice','alice@example.io'),('Bob','bob@example.io'),('Carol','carol@example.io')) v(n,e) WHERE NOT EXISTS (SELECT 1 FROM %s)`, customers, customers),
		fmt.Sprintf(`INSERT INTO %s (customer_id, total, status) SELECT * FROM (VALUES (1,49.90,'paid'),(2,19.50,'pending'),(1,4.25,'shipped')) v(c,t,s) WHERE NOT EXISTS (SELECT 1 FROM %s)`, orders, orders),
	}
	if err := execAll(ctx, db, stmts); err != nil {
		return fmt.Errorf("seed postgres: %w", err)
	}
	return nil
}

func seedMySQL(ctx context.Context, conn provider.SQLConn, opts Options) error {
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true", conn.User, conn.Password, net.JoinHostPort(conn.Host, conn.Port), conn.Database)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("seed mysql: open: %w", err)
	}
	defer db.Close()

	users := TabularName(opts.Namespace, "users")
	products := TabularName(opts.Namespace, "products")
	stmts := []string{
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(120) NOT NULL, email VARCHAR(190))", users),
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id INT AUTO_INCREMENT PRIMARY KEY, title VARCHAR(190) NOT NULL, price DECIMAL(10,2))", products),
		fmt.Sprintf("INSERT INTO %s (name,email) SELECT 'Alice','alice@example.io' FROM DUAL WHERE NOT EXISTS (SELECT 1 FROM %s)", users, users),
		fmt.Sprintf("INSERT INTO %s (name,email) SELECT 'Bob','bob@example.io' FROM DUAL WHERE (SELECT COUNT(*) FROM %s) < 3", users, users),
		fmt.Sprintf("INSERT INTO %s (title,price) SELECT 'Widget',9.99 FROM DUAL WHERE NOT EXISTS (SELECT 1 FROM %s)", products, products),
	}
	if err := execAll(ctx, db, stmts); err != nil {
		return fmt.Errorf("seed mysql: %w", err)
	}
	return nil
}

func seedClickhouse(ctx context.Context, conn provider.SQLConn, opts Options) error {
	hostPort := net.JoinHostPort(conn.Host, conn.Port)
	// NO readonly flag here — we are seeding (the read-path provider opens readonly=1).
	dsn := fmt.Sprintf("clickhouse://%s:%s@%s/%s", url.QueryEscape(conn.User), url.QueryEscape(conn.Password), hostPort, conn.Database)
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return fmt.Errorf("seed clickhouse: open: %w", err)
	}
	defer db.Close()

	events := TabularName(opts.Namespace, "events")
	stmts := []string{
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id UInt64, name String, ts DateTime DEFAULT now()) ENGINE = MergeTree ORDER BY id", events),
		fmt.Sprintf("INSERT INTO %s (id,name) SELECT number, concat('event-', toString(number)) FROM numbers(20) WHERE (SELECT count() FROM %s) = 0", events, events),
	}
	if err := execAll(ctx, db, stmts); err != nil {
		return fmt.Errorf("seed clickhouse: %w", err)
	}
	return nil
}

func execAll(ctx context.Context, db *sql.DB, stmts []string) error {
	cx, cancel := context.WithTimeout(ctx, sqlOpTimeout)
	defer cancel()
	if err := db.PingContext(cx); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	for _, q := range stmts {
		if _, err := db.ExecContext(cx, q); err != nil {
			return fmt.Errorf("%.50q: %w", q, err)
		}
	}
	return nil
}

// cleanupPostgres drops every table pg_catalog.pg_tables (schema "public")
// reports as owned by namespace (TabularOwns).
func cleanupPostgres(ctx context.Context, conn provider.SQLConn, namespace string) error {
	dsn := fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable",
		url.QueryEscape(conn.User), url.QueryEscape(conn.Password),
		net.JoinHostPort(conn.Host, conn.Port), conn.Database)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("seed cleanup postgres: open: %w", err)
	}
	defer db.Close()
	return dropOwnedTables(ctx, db, namespace, `SELECT tablename FROM pg_catalog.pg_tables WHERE schemaname = 'public'`)
}

// cleanupMySQL drops every table information_schema.tables (the connected
// database) reports as owned by namespace. mariadb classifies to the same
// dialect base as mysql (see provider.Classify), so this one function
// covers both.
func cleanupMySQL(ctx context.Context, conn provider.SQLConn, namespace string) error {
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true", conn.User, conn.Password, net.JoinHostPort(conn.Host, conn.Port), conn.Database)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("seed cleanup mysql: open: %w", err)
	}
	defer db.Close()
	return dropOwnedTables(ctx, db, namespace, `SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE()`)
}

// cleanupClickhouse drops every table system.tables (the connected
// database) reports as owned by namespace.
func cleanupClickhouse(ctx context.Context, conn provider.SQLConn, namespace string) error {
	hostPort := net.JoinHostPort(conn.Host, conn.Port)
	dsn := fmt.Sprintf("clickhouse://%s:%s@%s/%s", url.QueryEscape(conn.User), url.QueryEscape(conn.Password), hostPort, conn.Database)
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return fmt.Errorf("seed cleanup clickhouse: open: %w", err)
	}
	defer db.Close()
	return dropOwnedTables(ctx, db, namespace, `SELECT name FROM system.tables WHERE database = currentDatabase()`)
}

// dropOwnedTables runs listQuery (a dialect-specific single-column catalog
// query naming every table in the connected database/schema), filters the
// result through TabularOwns, and DROP TABLE IF EXISTS each match. Safe to
// run when nothing matches (idempotent).
func dropOwnedTables(ctx context.Context, db *sql.DB, namespace, listQuery string) error {
	cx, cancel := context.WithTimeout(ctx, sqlOpTimeout)
	defer cancel()
	if err := db.PingContext(cx); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	rows, err := db.QueryContext(cx, listQuery)
	if err != nil {
		return fmt.Errorf("list tables: %w", err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan table name: %w", err)
		}
		if TabularOwns(namespace, name) {
			names = append(names, name)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list tables: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("list tables: %w", err)
	}
	for _, name := range names {
		if _, err := db.ExecContext(cx, fmt.Sprintf("DROP TABLE IF EXISTS %s", name)); err != nil {
			return fmt.Errorf("drop table %s: %w", name, err)
		}
	}
	return nil
}
