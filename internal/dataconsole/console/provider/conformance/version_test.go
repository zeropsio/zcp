//go:build e2e

package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	goredis "github.com/redis/go-redis/v9"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// Version capture is best-effort ONLY: a server-version string is logged when
// cheap to obtain through a well-known, stable protocol surface, and silently
// "" (rendered "unavailable" by the caller) otherwise. It is never a test
// assertion — a missing version string is not a conformance failure. See
// doc.go; this deliberately covers exactly the three techniques named in the
// S10a brief (SQL SELECT version(), redis-protocol INFO server, the
// Elasticsearch root JSON) and no others: object storage, kafka/nats, and the
// non-Elasticsearch document engines have no equally cheap, equally certain
// primitive through the provider surface this suite is allowed to use.

// sqlVersion runs "SELECT version()" through the ALREADY-OPEN TabularProvider
// — supported by postgresql, mariadb/mysql, and clickhouse alike — and
// returns the first cell as a string, or "" on any failure.
func sqlVersion(ctx context.Context, tp provider.TabularProvider) string {
	page, err := tp.Query(ctx, "SELECT version()", provider.Page{Limit: 1})
	if err != nil || len(page.Rows) == 0 || len(page.Rows[0]) == 0 {
		return ""
	}
	return fmt.Sprint(page.Rows[0][0])
}

// kvVersion opens a short-lived redis-protocol client (independent of the
// KVProvider, which has no INFO passthrough) and reads the "# Server"
// section's version field. Valkey retains redis_version for backward
// compatibility and may additionally report valkey_version; prefer the
// latter when present. Returns "" on any failure.
func kvVersion(ctx context.Context, d *KVDescriptor) string {
	if d == nil {
		return ""
	}
	cli := goredis.NewClient(&goredis.Options{Addr: net.JoinHostPort(d.Host, d.Port), Password: d.Password})
	defer func() { _ = cli.Close() }()
	info, err := cli.Info(ctx, "server").Result()
	if err != nil {
		return ""
	}
	if v := grepInfoField(info, "valkey_version:"); v != "" {
		return v
	}
	return grepInfoField(info, "redis_version:")
}

func grepInfoField(info, prefix string) string {
	for _, line := range strings.Split(info, "\r\n") {
		if v, ok := strings.CutPrefix(line, prefix); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// esVersion fetches the Elasticsearch root endpoint (GET /, the canonical,
// long-stable "cluster_name/version/tagline" response) and extracts
// version.number. Returns "" on any failure — including a non-2xx status or
// an unexpected body shape, so this never masks a REAL health problem as a
// version-capture problem: Health() already gated reachability before this
// runs.
func esVersion(ctx context.Context, d *DocumentDescriptor) string {
	if d == nil {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(d.BaseURL, "/")+"/", nil)
	if err != nil {
		return ""
	}
	if d.User != "" || d.APIKey != "" {
		req.SetBasicAuth(d.User, d.APIKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var out struct {
		Version struct {
			Number string `json:"number"`
		} `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ""
	}
	return out.Version.Number
}

// logVersion renders v for a t.Logf call, substituting "unavailable" for an
// empty capture so every case logs a consistent line whether or not the
// cheap path applied.
func logVersion(v string) string {
	if v == "" {
		return "unavailable"
	}
	return v
}
