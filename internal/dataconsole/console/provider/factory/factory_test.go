package factory

import (
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// Independent oracle: every want* value below is hand-copied from
// cmd/zcp/studio_console.go's current per-family factory wiring (ReadOnly:
// !policy.ArmingPermitted()) + docs/spec-dataconsole.md §6's support-tier
// table — never computed by calling the factory itself or the provider's own
// constructor a second time.
func TestFactory_ParityWithProductionConstruction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		desc         provider.ConnectionDescriptor
		armed        bool
		wantReadOnly bool
		wantSupport  provider.Support
	}{
		{
			name:         "object armed=false is read-only",
			desc:         provider.ObjectConn{Endpoint: "minio.local:9000", AccessKey: "ak", SecretKey: "sk", Bucket: "b"},
			armed:        false,
			wantReadOnly: true,
			wantSupport:  provider.SupportFull,
		},
		{
			name:         "object armed=true is writable",
			desc:         provider.ObjectConn{Endpoint: "minio.local:9000", AccessKey: "ak", SecretKey: "sk", Bucket: "b"},
			armed:        true,
			wantReadOnly: false,
			wantSupport:  provider.SupportFull,
		},
		{
			name:         "tabular postgresql armed=false is read-only",
			desc:         provider.SQLConn{Driver: "pgx", Dialect: "postgresql", Host: "db.local", Port: "5432", User: "u", Password: "p", Database: "d"},
			armed:        false,
			wantReadOnly: true,
			wantSupport:  provider.SupportFull,
		},
		{
			name:         "tabular postgresql armed=true is writable",
			desc:         provider.SQLConn{Driver: "pgx", Dialect: "postgresql", Host: "db.local", Port: "5432", User: "u", Password: "p", Database: "d"},
			armed:        true,
			wantReadOnly: false,
			wantSupport:  provider.SupportFull,
		},
		{
			// The characterization pin (I-2): clickhouse forces NoEdit
			// intrinsically, regardless of what the factory is asked to arm.
			name:         "tabular clickhouse armed=true STAYS read-only (NoEdit forced)",
			desc:         provider.SQLConn{Driver: "clickhouse", Dialect: "clickhouse", Host: "ch.local", Port: "9000", User: "u", Password: "p", Database: "d"},
			armed:        true,
			wantReadOnly: true,
			wantSupport:  provider.SupportViewOnly,
		},
		{
			name:         "kv armed=false is read-only",
			desc:         provider.KVConn{Host: "kv.local", Port: "6379"},
			armed:        false,
			wantReadOnly: true,
			wantSupport:  provider.SupportFull,
		},
		{
			name:         "kv armed=true is writable",
			desc:         provider.KVConn{Host: "kv.local", Port: "6379"},
			armed:        true,
			wantReadOnly: false,
			wantSupport:  provider.SupportFull,
		},
		{
			name:         "document elasticsearch armed=false is read-only",
			desc:         provider.DocumentConn{Engine: "elasticsearch", BaseURL: "http://es.local:9200"},
			armed:        false,
			wantReadOnly: true,
			wantSupport:  provider.SupportViewOnly, // document.New downgrades reported Support whenever ReadOnly is true, for any reason
		},
		{
			name:         "document elasticsearch armed=true is writable",
			desc:         provider.DocumentConn{Engine: "elasticsearch", BaseURL: "http://es.local:9200"},
			armed:        true,
			wantReadOnly: false,
			wantSupport:  provider.SupportFull,
		},
		{
			name:         "document qdrant armed=true STAYS read-only (vectors forced)",
			desc:         provider.DocumentConn{Engine: "qdrant", BaseURL: "http://qdrant.local:6333"},
			armed:        true,
			wantReadOnly: true,
			wantSupport:  provider.SupportViewOnly,
		},
		{
			// Stream carries no write posture at all — messaging is read-only
			// by nature; armed is accepted for signature symmetry and ignored.
			name:         "stream ignores armed — always read-only",
			desc:         provider.StreamConn{Engine: "kafka", Host: "kafka.local", Port: "9092"},
			armed:        true,
			wantReadOnly: true,
			wantSupport:  provider.SupportViewOnly,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := New(tt.desc, tt.armed)
			if err != nil {
				t.Fatalf("New(%T, armed=%v): %v", tt.desc, tt.armed, err)
			}
			defer func() { _ = p.Close() }()
			caps := p.Caps()
			if caps.ReadOnly != tt.wantReadOnly {
				t.Errorf("Caps().ReadOnly = %v, want %v", caps.ReadOnly, tt.wantReadOnly)
			}
			if caps.Support != tt.wantSupport {
				t.Errorf("Caps().Support = %v, want %v", caps.Support, tt.wantSupport)
			}
		})
	}
}

// TestFactory_UnknownDescriptor_Errors pins that an unrecognized descriptor
// type (nil, or any future connection shape not yet wired) is a clean error,
// never a nil-pointer panic — factory.New is the ONLY construction path, so
// this is the single place a new family must be added.
func TestFactory_UnknownDescriptor_Errors(t *testing.T) {
	t.Parallel()
	if _, err := New(nil, true); err == nil {
		t.Fatal("New(nil, true) succeeded, want an error")
	}
}
