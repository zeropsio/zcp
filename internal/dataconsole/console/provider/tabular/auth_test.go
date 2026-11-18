package tabular

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// witness: internal/dataconsole/console/provider/tabular/errors.go | return sqlStateClass(pgErr.Code) == "28" → return sqlStateClass(pgErr.Code) == "42"
func TestIsAuthErr_RefusedCredentialVsRevokedGrant_OnlyRefusalIsAuthentication(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"pg bad password", pgErr("28P01", `password authentication failed for user "appuser"`), true},
		{"mysql access denied for the password", mysqlErr(1045, "28000", "Access denied for user 'appuser'@'10.0.0.5' (using password: YES)"), true},
		{"clickhouse authentication failed", &clickhouse.Exception{Code: 516, Message: "appuser: Authentication failed"}, true},
		{"pg revoked grant", pgErr("42501", "permission denied for table things"), false},
		{"mysql database grant missing", mysqlErr(1044, "42000", "Access denied for user 'appuser'@'%' to database 'app'"), false},
		{"mysql table grant missing", mysqlErr(1142, "42000", "SELECT command denied to user 'appuser'@'%' for table 'things'"), false},
		{"clickhouse syntax error", &clickhouse.Exception{Code: 62, Message: "Syntax error"}, false},
		{"network failure", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isAuthErr(tc.err); got != tc.want {
				t.Fatalf("isAuthErr = %v, want %v", got, tc.want)
			}
		})
	}
}

// witness: internal/dataconsole/console/provider/tabular/tabular.go | return nil, upstreamErr("list", err) → return nil, fmt.Errorf("tabular: list: %w", provider.ErrUpstream)
func TestList_CredentialsRefused_IsAnAuthenticationFailureWithoutDriverText(t *testing.T) {
	t.Parallel()
	p := newRejectProvider(t, rejectDriver{queryErr: pgErr("28P01", `password authentication failed for user "appuser"`)})

	_, _, err := p.List(context.Background(), provider.Path{Service: "db"}, provider.Page{})
	if !errors.Is(err, provider.ErrAuth) || !errors.Is(err, provider.ErrUpstream) {
		t.Fatalf("List = %v, want ErrAuth joined with ErrUpstream", err)
	}
	if strings.Contains(err.Error(), "appuser") {
		t.Fatalf("List error carries the driver's text: %q", err)
	}
}
