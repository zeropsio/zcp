package tabular

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// This file classifies an error the ENGINE returns while executing a
// user-composed operand — query-pane SQL text, a cell-edit/insert value, a
// delete-row key — as either a connectivity/system-class failure (the
// engine never got to evaluate the operand: ErrUpstream, unchanged from
// before) or an honest rejection of that operand (ErrInvalid, carrying a
// sanitized reason). Before this, every one of these call sites wrapped
// unconditionally in ErrUpstream, so a SQL syntax typo, a wrong-type
// literal, or a constraint violation all read as "the platform broke"
// (spec-dataconsole.md §7.1 I-2: "a validation rejection is not an
// outage"). System-composed reads (schema/table listing, columnsAndPK) are
// UNCHANGED — they never run caller-supplied text, so their existing flat
// ErrUpstream default already had nothing to reclassify.

// maxEngineReasonLen bounds the sanitized engine-rejection reason folded
// into a user-composed operation's ErrInvalid message — sized the same as
// the house pattern (document/meilisearch.go's sanitizeTaskReason).
const maxEngineReasonLen = 200

// dsnURLPattern / dsnKeyValPattern redact connection-string-shaped text
// defensively before it can reach engineErr's returned message: none of the
// three drivers' Error() text actually embeds host/credential text for a
// query-rejection error (verified against the pinned jackc/pgx/v5 v5.10.0,
// go-sql-driver/mysql v1.10.0, and ClickHouse/clickhouse-go/v2 v2.47.0
// sources — each formats a single line naming only the SQL condition, e.g.
// `ERROR: syntax error at or near "SELEKT" (SQLSTATE 42601)`), but this is
// the one engine-error text this package lets reach the client-visible
// sentinel message, so it is sanitized as if a future driver or error path
// might embed one.
var (
	dsnURLPattern    = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://\S+`)
	dsnKeyValPattern = regexp.MustCompile(`(?i)\b(host|hostaddr|port|password|pwd|user|dbname|sslmode)=\S+`)
)

// engineErr classifies err — returned by the ENGINE while executing a
// user-composed operand — into the honest provider sentinel. Callers pass
// it only from inside an `if err != nil` guard. op names the call site,
// folded into the wrapped error's message for the server-side diagnostic
// log only: server.publicErrorMessage today collapses every sentinel to
// its flat generic client-facing string (pinned by
// TestServer_ErrorEnvelope_SentinelErrors), so — exactly like
// document/meilisearch.go's sanitizeTaskReason before it — this reason
// does not cross the wire to the client yet.
func engineErr(op string, err error) error {
	if isConnectivityErr(err) {
		return fmt.Errorf("tabular: %s: %w", op, provider.ErrUpstream)
	}
	return fmt.Errorf("tabular: %s: %s: %w", op, sanitizeEngineReason(err), provider.ErrInvalid)
}

// isConnectivityErr reports whether err reflects the engine being
// UNREACHABLE rather than the engine evaluating and rejecting a
// user-composed operand: a canceled/expired context, an exhausted
// database/sql connection, or a dial/network failure — reusing
// provider.IsNetUnreachable's net.Error/net.OpError check and its
// text-pattern fallback (a driver often flattens a genuine network failure
// to a bare string) rather than duplicating that logic.
//
// Where the driver exposes a typed, structured server error
// (*pgconn.PgError, *mysql.MySQLError), its SQLSTATE class sharpens the
// call: class "08" (connection exception — the same two-digit class in
// both Postgres and MySQL/MariaDB's 5-character ANSI SQLSTATE) is the one
// class that still means connectivity even though the error is structured.
// A *pgconn.PgError/*mysql.MySQLError value reaching this far already
// proves the round-trip to the engine succeeded — the driver only
// constructs these FROM a server response — so every other class is, by
// construction, the engine rejecting the operand, not an outage.
func isConnectivityErr(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	if errors.Is(err, driver.ErrBadConn) || errors.Is(err, sql.ErrConnDone) {
		return true
	}
	if provider.IsNetUnreachable(err) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return sqlStateClass(pgErr.Code) == "08"
	}
	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) {
		if myErr.SQLState == ([5]byte{}) {
			return false
		}
		return sqlStateClass(string(myErr.SQLState[:])) == "08"
	}
	return false
}

func sqlStateClass(code string) string {
	if len(code) < 2 {
		return ""
	}
	return code[:2]
}

// sanitizeEngineReason renders err's message as a bounded, single-line,
// control-character-free, connection-string-redacted summary — the house
// pattern (document/meilisearch.go's sanitizeTaskReason), applied to a SQL
// engine's rejection of a user-composed operand instead of a meilisearch
// task failure.
func sanitizeEngineReason(err error) string {
	clean := strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, err.Error())
	clean = dsnURLPattern.ReplaceAllString(clean, "[redacted]")
	clean = dsnKeyValPattern.ReplaceAllString(clean, "$1=[redacted]")
	if len(clean) > maxEngineReasonLen {
		clean = clean[:maxEngineReasonLen] + "..."
	}
	return clean
}
