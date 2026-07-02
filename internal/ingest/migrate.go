package ingest

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// migrationsFS embeds the ordered, idempotent ClickHouse migration steps
// (spec §8 "additive-only enforced by mechanism"). Each file is named
// NNNN_description.sql; NNNN is the migration id recorded in
// telemetry.schema_migrations once applied, so a re-run (process restart,
// redeploy, or the historical restart-race window) is a no-op — it never
// re-applies a migration it has already recorded (G3).
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationsDir is the directory name inside migrationsFS holding the
// embedded migration files — also the fixed dir loadMigrationsFrom reads
// from (test doubles mirror this same layout, spec §8).
const migrationsDir = "migrations"

// migrationFileName is the embedded migration naming convention: a 4-digit
// zero-padded id, an underscore, a lower_snake_case description, `.sql`.
var migrationFileName = regexp.MustCompile(`^(\d{4})_[a-z0-9_]+\.sql$`)

// migration is one embedded, ordered ClickHouse migration step.
type migration struct {
	id    uint32
	name  string
	stmts []string
}

// loadMigrations reads and orders the embedded migration files.
func loadMigrations() ([]migration, error) {
	return loadMigrationsFrom(migrationsFS)
}

// loadMigrationsFrom reads migration files from migrationsDir in fsys,
// parses their id from the filename, and returns them sorted ascending by
// id. Split out from loadMigrations so the naming/ordering/duplicate-id
// contract is testable against a synthetic fs.FS without touching the real
// embedded migrations.
func loadMigrationsFrom(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir %q: %w", migrationsDir, err)
	}

	out := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		m := migrationFileName.FindStringSubmatch(entry.Name())
		if m == nil {
			return nil, fmt.Errorf("migration file %q violates the NNNN_description.sql naming convention", entry.Name())
		}
		id, err := strconv.ParseUint(m[1], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("migration file %q: invalid id: %w", entry.Name(), err)
		}
		body, err := fs.ReadFile(fsys, migrationsDir+"/"+entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		out = append(out, migration{id: uint32(id), name: entry.Name(), stmts: splitStatements(string(body))})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	for i := 1; i < len(out); i++ {
		if out[i].id == out[i-1].id {
			return nil, fmt.Errorf("duplicate migration id %d (%q, %q)", out[i].id, out[i-1].name, out[i].name)
		}
	}
	return out, nil
}

// renderMigrationDB substitutes the __DB__ token (schema.sql's own
// database-name placeholder convention, spec §7) into every migration
// statement, returning a new slice — migrations is never mutated in place.
// openAdminConn (spec §8) carries no default database, so any migration DDL
// that references a base-schema object (telemetry.events, etc.) must be
// fully qualified, exactly like renderBootstrapStatements does for
// schema.sql. Authoring convention: migration files write __DB__.<table>,
// same as schema.sql.
func renderMigrationDB(migrations []migration, db string) []migration {
	out := make([]migration, len(migrations))
	for i, m := range migrations {
		stmts := make([]string, len(m.stmts))
		for j, s := range m.stmts {
			stmts[j] = strings.ReplaceAll(s, "__DB__", db)
		}
		out[i] = migration{id: m.id, name: m.name, stmts: stmts}
	}
	return out
}

// fetchAppliedMigrationIDs queries telemetry.schema_migrations for the ids
// already recorded, so runMigrations can skip them.
func fetchAppliedMigrationIDs(ctx context.Context, conn driver.Conn, database string) (map[uint32]bool, error) {
	query := fmt.Sprintf("SELECT id FROM %s.schema_migrations", database)
	var ids []uint32
	if err := conn.Select(ctx, &ids, query); err != nil {
		return nil, fmt.Errorf("query applied migrations: %w", err)
	}
	applied := make(map[uint32]bool, len(ids))
	for _, id := range ids {
		applied[id] = true
	}
	return applied, nil
}

// runMigrations applies every migration in migrations not yet recorded in
// telemetry.schema_migrations, in id order, recording each as it completes.
// A migration whose DDL fails is never recorded, so the next run retries it
// from scratch rather than skipping it as done.
func runMigrations(ctx context.Context, conn driver.Conn, database string, migrations []migration, logger *slog.Logger) error {
	applied, err := fetchAppliedMigrationIDs(ctx, conn, database)
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if applied[m.id] {
			continue
		}
		for _, stmt := range m.stmts {
			stmtCtx, cancel := context.WithTimeout(ctx, bootstrapStmtTimeout)
			execErr := conn.Exec(stmtCtx, stmt)
			cancel()
			if execErr != nil {
				return fmt.Errorf("migration %s: exec statement: %w", m.name, execErr)
			}
		}

		recordQuery := fmt.Sprintf("INSERT INTO %s.schema_migrations (id) VALUES (%d)", database, m.id)
		recordCtx, cancel := context.WithTimeout(ctx, bootstrapStmtTimeout)
		recordErr := conn.Exec(recordCtx, recordQuery)
		cancel()
		if recordErr != nil {
			return fmt.Errorf("migration %s: record applied: %w", m.name, recordErr)
		}
		logger.Info("ingest: migration applied", "id", m.id, "name", m.name)
	}
	return nil
}

// applyMigrations loads the embedded migrations and applies whichever are
// pending against cfg's ClickHouse admin connection. Called from
// bootstrapSchema once the base schema has been applied and connectivity is
// proven, so this makes exactly one connection attempt (no outer retry
// window — a migration exec failure here is a real, surfaced error, not a
// transient connectivity gap).
func applyMigrations(ctx context.Context, cfg Config, logger *slog.Logger) error {
	if !chIdentifier.MatchString(cfg.CHDatabase) {
		return fmt.Errorf("invalid clickhouse database identifier %q", cfg.CHDatabase)
	}

	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("load embedded migrations: %w", err)
	}
	if len(migrations) == 0 {
		return nil
	}
	migrations = renderMigrationDB(migrations, cfg.CHDatabase)

	conn, err := openAdminConn(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	return runMigrations(ctx, conn, cfg.CHDatabase, migrations, logger)
}
