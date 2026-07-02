package ingest_test

import (
	"slices"
	"testing"

	"github.com/zeropsio/zcp/internal/ingest"
)

func TestLoadConfig_Defaults(t *testing.T) {
	t.Parallel()

	cfg := ingest.LoadConfig(envFunc(map[string]string{}))

	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want :8080", cfg.ListenAddr)
	}
	if cfg.CHPort != 9000 {
		t.Errorf("CHPort = %d, want 9000", cfg.CHPort)
	}
	if cfg.CHDatabase != "telemetry" {
		t.Errorf("CHDatabase = %q, want telemetry", cfg.CHDatabase)
	}
	if cfg.CHUser != "zerops" {
		t.Errorf("CHUser = %q, want zerops", cfg.CHUser)
	}
	if cfg.CHHost != "" {
		t.Errorf("CHHost = %q, want empty default", cfg.CHHost)
	}
	if cfg.CHPassword != "" {
		t.Errorf("CHPassword = %q, want empty default", cfg.CHPassword)
	}
	if cfg.CHSuperUser != "super" {
		t.Errorf("CHSuperUser = %q, want super", cfg.CHSuperUser)
	}
	if cfg.CHSuperPassword != "" {
		t.Errorf("CHSuperPassword = %q, want empty default (bootstrap disabled)", cfg.CHSuperPassword)
	}
	if cfg.CHCluster != "" {
		t.Errorf("CHCluster = %q, want empty default (single-node)", cfg.CHCluster)
	}
}

func TestLoadConfig_Overrides(t *testing.T) {
	t.Parallel()

	cfg := ingest.LoadConfig(envFunc(map[string]string{
		"LISTEN_ADDR":       ":9999",
		"CH_HOST":           "db",
		"CH_PORT":           "9440",
		"CH_DATABASE":       "custom_db",
		"CH_USER":           "custom_user",
		"CH_PASSWORD":       "s3cret",
		"CH_SUPER_USER":     "admin",
		"CH_SUPER_PASSWORD": "sup3r",
		"CH_CLUSTER":        "zerops",
	}))

	if cfg.ListenAddr != ":9999" {
		t.Errorf("ListenAddr = %q, want :9999", cfg.ListenAddr)
	}
	if cfg.CHHost != "db" {
		t.Errorf("CHHost = %q, want db", cfg.CHHost)
	}
	if cfg.CHPort != 9440 {
		t.Errorf("CHPort = %d, want 9440", cfg.CHPort)
	}
	if cfg.CHDatabase != "custom_db" {
		t.Errorf("CHDatabase = %q, want custom_db", cfg.CHDatabase)
	}
	if cfg.CHUser != "custom_user" {
		t.Errorf("CHUser = %q, want custom_user", cfg.CHUser)
	}
	if cfg.CHPassword != "s3cret" {
		t.Errorf("CHPassword = %q, want s3cret", cfg.CHPassword)
	}
	if cfg.CHSuperUser != "admin" {
		t.Errorf("CHSuperUser = %q, want admin", cfg.CHSuperUser)
	}
	if cfg.CHSuperPassword != "sup3r" {
		t.Errorf("CHSuperPassword = %q, want sup3r", cfg.CHSuperPassword)
	}
	if cfg.CHCluster != "zerops" {
		t.Errorf("CHCluster = %q, want zerops", cfg.CHCluster)
	}
}

func TestLoadConfig_InvalidPortIgnored(t *testing.T) {
	t.Parallel()

	cfg := ingest.LoadConfig(envFunc(map[string]string{"CH_PORT": "not-a-number"}))

	if cfg.CHPort != 9000 {
		t.Errorf("CHPort = %d, want default 9000 on invalid override", cfg.CHPort)
	}
}

func TestLoadConfig_ZeroPortIgnored(t *testing.T) {
	t.Parallel()

	cfg := ingest.LoadConfig(envFunc(map[string]string{"CH_PORT": "0"}))

	if cfg.CHPort != 9000 {
		t.Errorf("CHPort = %d, want default 9000 on zero override", cfg.CHPort)
	}
}

func TestLoadConfig_BlocklistsDefaultEmpty(t *testing.T) {
	t.Parallel()

	cfg := ingest.LoadConfig(envFunc(map[string]string{}))

	if len(cfg.BlockedIPs) != 0 {
		t.Errorf("BlockedIPs = %v, want empty", cfg.BlockedIPs)
	}
	if len(cfg.BlockedInstalls) != 0 {
		t.Errorf("BlockedInstalls = %v, want empty", cfg.BlockedInstalls)
	}
}

func TestLoadConfig_BlocklistsParsedCommaSeparatedTrimmed(t *testing.T) {
	t.Parallel()

	cfg := ingest.LoadConfig(envFunc(map[string]string{
		"INGEST_BLOCK_IPS":      "203.0.113.1, 203.0.113.2 ,,203.0.113.3",
		"INGEST_BLOCK_INSTALLS": "11111111-1111-4111-8111-111111111111 , 22222222-2222-4222-8222-222222222222",
	}))

	wantIPs := []string{"203.0.113.1", "203.0.113.2", "203.0.113.3"}
	if !slices.Equal(cfg.BlockedIPs, wantIPs) {
		t.Errorf("BlockedIPs = %v, want %v", cfg.BlockedIPs, wantIPs)
	}
	wantInstalls := []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"}
	if !slices.Equal(cfg.BlockedInstalls, wantInstalls) {
		t.Errorf("BlockedInstalls = %v, want %v", cfg.BlockedInstalls, wantInstalls)
	}
}

// envFunc adapts a map to the getenv(string) string shape LoadConfig takes.
func envFunc(m map[string]string) func(string) string {
	return func(key string) string { return m[key] }
}
