package tabular

import (
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

func TestDialectQuoting(t *testing.T) {
	t.Parallel()
	pg := pgDialect{}
	my := myDialect{}

	// Identifier quoting must escape the quote char (injection safety).
	if got := pg.quote(`a"b`); got != `"a""b"` {
		t.Errorf("pg.quote = %q", got)
	}
	if got := my.quote("a`b"); got != "`a``b`" {
		t.Errorf("my.quote = %q", got)
	}
	if got := pg.qualify("pub", "users"); got != `"pub"."users"` {
		t.Errorf("pg.qualify = %q", got)
	}
	if got := my.qualify("app", "orders"); got != "`app`.`orders`" {
		t.Errorf("my.qualify = %q", got)
	}
}

func TestPlaceholders(t *testing.T) {
	t.Parallel()
	pg := newPlaceholders(pgDialect{})
	if a, b := pg.next(), pg.next(); a != "$1" || b != "$2" {
		t.Errorf("pg placeholders = %q,%q", a, b)
	}
	my := newPlaceholders(myDialect{})
	if a, b := my.next(), my.next(); a != "?" || b != "?" {
		t.Errorf("my placeholders = %q,%q", a, b)
	}
}

func TestNullSafeEq(t *testing.T) {
	t.Parallel()
	if (pgDialect{}).nullSafeEq() != "IS NOT DISTINCT FROM" {
		t.Error("pg nullSafeEq")
	}
	if (myDialect{}).nullSafeEq() != "<=>" {
		t.Error("my nullSafeEq")
	}
}

func TestClampLimit(t *testing.T) {
	t.Parallel()
	cases := [][2]int{{0, defaultLimit}, {-5, defaultLimit}, {50, 50}, {5000, maxLimit}}
	for _, c := range cases {
		if got := clampLimit(c[0]); got != c[1] {
			t.Errorf("clampLimit(%d) = %d, want %d", c[0], got, c[1])
		}
	}
}

// TestReturningClause pins T-AUD-03's per-dialect insert-key-echo mechanism:
// Postgres gets a quoted RETURNING suffix; MySQL/MariaDB and ClickHouse have
// none (InsertRow falls back to LastInsertId for mysql; ClickHouse is
// view-only and never reaches InsertRow at all).
func TestReturningClause(t *testing.T) {
	t.Parallel()
	if got := (pgDialect{}).returningClause([]string{"id"}); got != `RETURNING "id"` {
		t.Errorf("pg.returningClause([id]) = %q", got)
	}
	if got := (pgDialect{}).returningClause([]string{"tenant_id", "item_id"}); got != `RETURNING "tenant_id", "item_id"` {
		t.Errorf("pg.returningClause(composite) = %q", got)
	}
	if got := (pgDialect{}).returningClause(nil); got != "" {
		t.Errorf("pg.returningClause(nil) = %q, want empty", got)
	}
	if got := (myDialect{}).returningClause([]string{"id"}); got != "" {
		t.Errorf("mysql.returningClause = %q, want empty (no RETURNING support)", got)
	}
	if got := (chDialect{}).returningClause([]string{"id"}); got != "" {
		t.Errorf("clickhouse.returningClause = %q, want empty (view-only)", got)
	}
}

func TestNormalize(t *testing.T) {
	t.Parallel()
	if normalize([]byte("hi")) != "hi" {
		t.Error("[]byte should become string")
	}
	if normalize(nil) != nil {
		t.Error("nil stays nil")
	}
	if normalize(int64(7)) != int64(7) {
		t.Error("int passes through")
	}
}

// TestNew_Clickhouse_ForcesNoEditUnderArmedPosture is a CHARACTERIZATION test
// (pins EXISTING behavior — no RED stage, this passes immediately):
// normalizeConfig forces NoEdit=true for the clickhouse dialect regardless of
// the caller's ReadOnly request, so even ReadOnly:false (the "armed" posture
// the S2 factory package passes uniformly for every family — see
// provider/factory) produces a non-editable, view-only-support provider.
// ClickHouse mutations are async ALTER, never a cell edit; this constructor
// is the ultimate posture owner here, and the factory never re-decides it.
func TestNew_Clickhouse_ForcesNoEditUnderArmedPosture(t *testing.T) {
	t.Parallel()
	p, err := New(Config{
		Conn: provider.SQLConn{
			Dialect: "clickhouse", Host: "ch.local", Port: "9000",
			User: "u", Password: "p", Database: "d",
		},
		ReadOnly: false, // armed
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = p.Close() }()
	caps := p.Caps()
	if !caps.ReadOnly {
		t.Errorf("Caps().ReadOnly = false, want true (NoEdit forces it regardless of the armed posture)")
	}
	if caps.EditTabular {
		t.Errorf("Caps().EditTabular = true, want false (clickhouse is never cell-editable)")
	}
	if caps.Support != provider.SupportViewOnly {
		t.Errorf("Caps().Support = %v, want view-only", caps.Support)
	}
}
