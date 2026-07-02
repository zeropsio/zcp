package ingest

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// schemaSQL is the embedded schema body (tables + materialized views) with
// __DB__ / __REPLICATED__ tokens; renderBootstrapStatements prepends the
// CREATE DATABASE + GRANT preamble. This file is the single source of truth
// for the ClickHouse schema (docs/spec-telemetry.md §7).
//
//go:embed schema.sql
var schemaSQL string

// Bootstrap pacing: the db service may still be starting when ingest boots
// (fresh import, restart storm), so connection-level failures retry until
// bootstrapWindow elapses. DDL statements get a generous per-statement
// timeout because ON CLUSTER DDL waits for all replicas.
const (
	bootstrapWindow      = 3 * time.Minute
	bootstrapRetryDelay  = 3 * time.Second
	bootstrapStmtTimeout = 120 * time.Second
)

// chIdentifier restricts database/user/cluster names interpolated into DDL —
// these come from our own deploy env, but SQL is never composed from
// unvalidated strings (CLAUDE.md shell/SQL composition rule).
var chIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// splitStatements splits a SQL blob on ';' into executable statements,
// dropping fragments that are empty or consist solely of `--` comment lines
// (ClickHouse rejects empty queries; comment-only fragments arise from
// trailing comments after the last statement).
func splitStatements(sql string) []string {
	// Full-line comments are stripped BEFORE splitting on ';' — a semicolon
	// inside a comment line would otherwise cut the blob mid-comment and
	// leak the comment's tail as a bogus "statement" (live-hit 2026-07-02:
	// the schema header's "…preamble; see renderBootstrapStatements)."
	// produced an unparseable fragment). Trailing same-line comments after
	// SQL survive — ClickHouse accepts them inside statements.
	var kept []string
	for line := range strings.SplitSeq(sql, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "--") {
			continue
		}
		kept = append(kept, line)
	}
	var out []string
	for frag := range strings.SplitSeq(strings.Join(kept, "\n"), ";") {
		if stmt := strings.TrimSpace(frag); stmt != "" {
			out = append(out, stmt)
		}
	}
	return out
}

// renderBootstrapStatements produces the full ordered DDL: database + grant
// preamble, then the embedded schema body. cluster == "" renders the
// single-node variant (plain engines, no ON CLUSTER); otherwise the HA
// variant — a Replicated database engine (which then propagates the body
// DDL itself, so only the preamble targets the cluster) and Replicated*
// table engines with auto-derived keeper paths.
func renderBootstrapStatements(db, user, cluster string) ([]string, error) {
	for name, v := range map[string]string{"database": db, "user": user} {
		if !chIdentifier.MatchString(v) {
			return nil, fmt.Errorf("invalid clickhouse %s identifier %q", name, v)
		}
	}
	if cluster != "" && !chIdentifier.MatchString(cluster) {
		return nil, fmt.Errorf("invalid clickhouse cluster identifier %q", cluster)
	}

	var stmts []string
	replicated := ""
	if cluster == "" {
		stmts = append(stmts,
			fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", db),
			fmt.Sprintf("GRANT SELECT, INSERT ON %s.* TO %s", db, user),
		)
	} else {
		replicated = "Replicated"
		stmts = append(stmts,
			fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s ON CLUSTER '%s' ENGINE = Replicated('/clickhouse/databases/{uuid}', '{shard}', '{replica}')", db, cluster),
			fmt.Sprintf("GRANT ON CLUSTER '%s' SELECT, INSERT ON %s.* TO %s", cluster, db, user),
		)
	}

	body := strings.ReplaceAll(schemaSQL, "__DB__", db)
	body = strings.ReplaceAll(body, "__REPLICATED__", replicated)
	return append(stmts, splitStatements(body)...), nil
}

// bootstrapSchema idempotently applies the base telemetry schema (spec §7)
// as the ClickHouse admin user (runs when CH_SUPER_PASSWORD is set), then
// applies any pending versioned migrations (spec §8, S3): fresh installs
// get the full current shape via the base schema's CREATE ... IF NOT
// EXISTS; existing installs pick up whatever incremental migrations they
// haven't seen yet. Connection-level failures during the base-schema phase
// retry for bootstrapWindow — on a fresh project import the db service
// routinely comes up after ingest.
func bootstrapSchema(ctx context.Context, cfg Config, logger *slog.Logger) error {
	stmts, err := renderBootstrapStatements(cfg.CHDatabase, cfg.CHUser, cfg.CHCluster)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(bootstrapWindow)
	var lastErr error
	for attempt := 1; ; attempt++ {
		lastErr = applyStatements(ctx, cfg, stmts)
		if lastErr == nil {
			logger.Info("ingest: schema bootstrap applied", "statements", len(stmts), "attempt", attempt)
			if err := applyMigrations(ctx, cfg, logger); err != nil {
				return fmt.Errorf("schema migrations: %w", err)
			}
			return nil
		}
		if ctx.Err() != nil {
			return fmt.Errorf("bootstrap canceled: %w", ctx.Err())
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("bootstrap window exhausted after %d attempts: %w", attempt, lastErr)
		}
		logger.Warn("ingest: schema bootstrap attempt failed, retrying", "attempt", attempt, "error", lastErr)
		select {
		case <-ctx.Done():
			return fmt.Errorf("bootstrap canceled: %w", ctx.Err())
		case <-time.After(bootstrapRetryDelay):
		}
	}
}

// openAdminConn opens a fresh ClickHouse admin connection (shared by the
// base-schema bootstrap and the migration runner, both spec §7/§8).
func openAdminConn(cfg Config) (driver.Conn, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%d", cfg.CHHost, cfg.CHPort)},
		Auth: clickhouse.Auth{
			Username: cfg.CHSuperUser,
			Password: cfg.CHSuperPassword,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("open admin connection: %w", err)
	}
	return conn, nil
}

// applyStatements opens a fresh admin connection and executes every
// statement in order, each under its own timeout.
func applyStatements(ctx context.Context, cfg Config, stmts []string) error {
	conn, err := openAdminConn(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	for _, stmt := range stmts {
		stmtCtx, cancel := context.WithTimeout(ctx, bootstrapStmtTimeout)
		err := conn.Exec(stmtCtx, stmt)
		cancel()
		if err != nil {
			head := stmt
			if i := strings.IndexByte(head, '\n'); i > 0 {
				head = head[:i]
			}
			return fmt.Errorf("exec %q: %w", head, err)
		}
	}
	return nil
}
