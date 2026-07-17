package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider/conformance"
)

// TestGenConfig_OutputPermissions0600AndValidJSON pins the two non-negotiable
// properties of the written DC_LIVE_CONFIG artifact: it is 0600 (never
// world/group-readable — it carries service credentials) and it is exactly
// what conformance.LoadLiveConfig — the real loader the live harness uses —
// accepts, never a hand-rolled shape that happens to look right.
func TestGenConfig_OutputPermissions0600AndValidJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file mode bits don't apply on windows")
	}
	t.Parallel()

	cfg := &conformance.LiveConfig{
		Services: []conformance.ServiceEntry{
			{
				Hostname: "db",
				Type:     "postgresql:single@18",
				Tabular: &conformance.SQLDescriptor{
					Dialect: "postgresql", Host: "pg", Port: "5432", User: "app", Password: "secret", Database: "appdb",
				},
			},
			{
				Hostname: "storage",
				Type:     "object-storage",
				Object: &conformance.ObjectDescriptor{
					Endpoint: "s3.example.internal", AccessKey: "access", SecretKey: "secret", Bucket: "bucket", Secure: true,
				},
			},
		},
	}

	out := filepath.Join(t.TempDir(), "dc-live-config.json")
	if err := writeLiveConfig(out, cfg); err != nil {
		t.Fatalf("writeLiveConfig: %v", err)
	}

	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat %s: %v", out, err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 0600", got)
	}

	loaded, err := conformance.LoadLiveConfig(out)
	if err != nil {
		t.Fatalf("LoadLiveConfig: %v", err)
	}
	if len(loaded.Services) != len(cfg.Services) {
		t.Fatalf("loaded %d services, want %d", len(loaded.Services), len(cfg.Services))
	}
	if _, ok := loaded.Lookup("db"); !ok {
		t.Fatalf("loaded config missing hostname %q", "db")
	}
	if _, ok := loaded.Lookup("storage"); !ok {
		t.Fatalf("loaded config missing hostname %q", "storage")
	}
}
