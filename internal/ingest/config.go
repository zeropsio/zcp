// Package ingest is the standalone ZCP telemetry ingest service (spec
// docs/spec-telemetry.md §6): a POST /v1/events HTTP handler with caps,
// gzip decoding, strict wire validation, dedup, per-IP/install/session
// rate limits, and a ClickHouse batch inserter behind an interface. It is
// deployed separately from the zcp CLI/MCP binary (cmd/zcp-ingest) and
// imports internal/telemetry/wire only from the core ZCP tree.
package ingest

import (
	"strconv"
	"strings"
)

// Config is the ingest service's runtime configuration, resolved once from
// environment variables at process start (spec §6 item 1).
type Config struct {
	// ListenAddr is the HTTP listen address, e.g. ":8080".
	ListenAddr string
	// CHHost/CHPort/CHDatabase/CHUser/CHPassword address the ClickHouse
	// native-protocol endpoint (spec §6 item 5, §7).
	CHHost     string
	CHPort     int
	CHDatabase string
	CHUser     string
	CHPassword string
	// CHSuperUser/CHSuperPassword are the ClickHouse admin credentials used
	// only by the startup schema bootstrap (bootstrap.go). Empty
	// CHSuperPassword disables bootstrap entirely — the service then assumes
	// the schema already exists.
	CHSuperUser     string
	CHSuperPassword string
	// CHCluster names the ClickHouse cluster for HA deployments (Replicated
	// database + table engines); empty renders the single-node schema
	// variant.
	CHCluster string
	// BlockedIPs/BlockedInstalls are the R5 ops-lever kill switch (spec §6):
	// comma-separated INGEST_BLOCK_IPS/INGEST_BLOCK_INSTALLS env values,
	// matched in-memory, never logged.
	BlockedIPs      []string
	BlockedInstalls []string
}

// Default config values (spec §6 item 1).
const (
	defaultListenAddr  = ":8080"
	defaultCHPort      = 9000
	defaultCHDatabase  = "telemetry"
	defaultCHUser      = "zerops"
	defaultCHSuperUser = "super"
)

// LoadConfig resolves Config from environment variables via getenv (injected
// so tests never touch real process env). Missing/invalid values fall back
// to the documented defaults; LoadConfig never fails — a misconfigured
// value is silently ignored in favor of the default so the service still
// starts (defense against a bad deploy env taking down ingest entirely).
func LoadConfig(getenv func(string) string) Config {
	cfg := Config{
		ListenAddr:  defaultListenAddr,
		CHPort:      defaultCHPort,
		CHDatabase:  defaultCHDatabase,
		CHUser:      defaultCHUser,
		CHSuperUser: defaultCHSuperUser,
	}
	if v := getenv("LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}
	cfg.CHHost = getenv("CH_HOST")
	if v := getenv("CH_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil && port > 0 {
			cfg.CHPort = port
		}
	}
	if v := getenv("CH_DATABASE"); v != "" {
		cfg.CHDatabase = v
	}
	if v := getenv("CH_USER"); v != "" {
		cfg.CHUser = v
	}
	cfg.CHPassword = getenv("CH_PASSWORD")
	if v := getenv("CH_SUPER_USER"); v != "" {
		cfg.CHSuperUser = v
	}
	cfg.CHSuperPassword = getenv("CH_SUPER_PASSWORD")
	cfg.CHCluster = getenv("CH_CLUSTER")
	cfg.BlockedIPs = splitCSV(getenv("INGEST_BLOCK_IPS"))
	cfg.BlockedInstalls = splitCSV(getenv("INGEST_BLOCK_INSTALLS"))
	return cfg
}

// splitCSV splits a comma-separated env value into trimmed, non-empty
// entries (spec §6 R5 blocklists). Empty input yields a nil slice.
func splitCSV(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}
