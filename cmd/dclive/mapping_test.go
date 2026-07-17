package main

import (
	"reflect"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider/conformance"
)

// TestGenConfig_MapsServiceEnvToDescriptors pins mapServiceEnv's field
// selection against zcpadapter/adapter.go's descriptorFor family (lines
// 102-193): every want value below is hand-written from that mapping (not
// computed by calling the adapter), so a genConfig regression that silently
// drops or renames an env key fails here independent of the adapter's own
// tests.
func TestGenConfig_MapsServiceEnvToDescriptors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hostname string
		typ      string
		env      map[string]string
		wantOK   bool
		want     conformance.ServiceEntry
	}{
		{
			name:     "postgresql",
			hostname: "db",
			typ:      "postgresql:single@18",
			env:      map[string]string{"hostname": "pg", "port": "5432", "user": "app", "password": "secret", "dbName": "appdb"},
			wantOK:   true,
			want: conformance.ServiceEntry{
				Hostname: "db", Type: "postgresql:single@18",
				Tabular: &conformance.SQLDescriptor{
					Dialect: "postgresql", Host: "pg", Port: "5432", User: "app", Password: "secret", Database: "appdb",
				},
			},
		},
		{
			name:     "mariadb db fallback",
			hostname: "mariadb",
			typ:      "mariadb@10.6",
			env:      map[string]string{"hostname": "maria", "port": "3306", "user": "app", "password": "secret"},
			wantOK:   true,
			want: conformance.ServiceEntry{
				Hostname: "mariadb", Type: "mariadb@10.6",
				Tabular: &conformance.SQLDescriptor{
					Dialect: "mariadb", Host: "maria", Port: "3306", User: "app", Password: "secret", Database: "app",
				},
			},
		},
		{
			name:     "clickhouse native port and defaults",
			hostname: "ch",
			typ:      "clickhouse@24",
			env:      map[string]string{"hostname": "ch-host", "port": "8123", "portNative": "9000", "superUser": "admin", "password": "secret"},
			wantOK:   true,
			want: conformance.ServiceEntry{
				Hostname: "ch", Type: "clickhouse@24",
				Tabular: &conformance.SQLDescriptor{
					Dialect: "clickhouse", Host: "ch-host", Port: "9000", User: "admin", Password: "secret", Database: "default",
				},
			},
		},
		{
			name:     "valkey",
			hostname: "cache",
			typ:      "valkey@7",
			env:      map[string]string{"hostname": "cache-host", "port": "6379", "password": "secret"},
			wantOK:   true,
			want: conformance.ServiceEntry{
				Hostname: "cache", Type: "valkey@7",
				KV: &conformance.KVDescriptor{Host: "cache-host", Port: "6379", Password: "secret"},
			},
		},
		{
			name:     "object storage",
			hostname: "storage",
			typ:      "object-storage",
			env: map[string]string{
				"apiHost": "s3.example.internal", "apiUrl": "https://s3.example.internal",
				"accessKeyId": "access", "secretAccessKey": "secret", "bucketName": "bucket",
			},
			wantOK: true,
			want: conformance.ServiceEntry{
				Hostname: "storage", Type: "object-storage",
				Object: &conformance.ObjectDescriptor{
					Endpoint: "s3.example.internal", AccessKey: "access", SecretKey: "secret", Bucket: "bucket", Secure: true,
				},
			},
		},
		{
			name:     "elasticsearch credential fallbacks",
			hostname: "es",
			typ:      "elasticsearch@8",
			env:      map[string]string{"hostname": "es-host", "port": "9200", "username": "elastic2", "elasticPassword": "secret"},
			wantOK:   true,
			want: conformance.ServiceEntry{
				Hostname: "es", Type: "elasticsearch@8",
				Document: &conformance.DocumentDescriptor{Engine: "elasticsearch", BaseURL: "http://es-host:9200", User: "elastic2", APIKey: "secret"},
			},
		},
		{
			name:     "meilisearch api key fallback",
			hostname: "search",
			typ:      "meilisearch",
			env:      map[string]string{"hostname": "meili", "port": "7700", "defaultAdminKey": "secret"},
			wantOK:   true,
			want: conformance.ServiceEntry{
				Hostname: "search", Type: "meilisearch",
				Document: &conformance.DocumentDescriptor{Engine: "meilisearch", BaseURL: "http://meili:7700", APIKey: "secret"},
			},
		},
		{
			name:     "typesense api key fallback",
			hostname: "docs",
			typ:      "typesense",
			env:      map[string]string{"hostname": "typesense-host", "port": "8108", "adminApiKey": "secret"},
			wantOK:   true,
			want: conformance.ServiceEntry{
				Hostname: "docs", Type: "typesense",
				Document: &conformance.DocumentDescriptor{Engine: "typesense", BaseURL: "http://typesense-host:8108", APIKey: "secret"},
			},
		},
		{
			name:     "qdrant api key fallback",
			hostname: "vectors",
			typ:      "qdrant",
			env:      map[string]string{"hostname": "qdrant-host", "port": "6333", "password": "secret"},
			wantOK:   true,
			want: conformance.ServiceEntry{
				Hostname: "vectors", Type: "qdrant",
				Document: &conformance.DocumentDescriptor{Engine: "qdrant", BaseURL: "http://qdrant-host:6333", APIKey: "secret"},
			},
		},
		{
			name:     "kafka stream credentials",
			hostname: "events",
			typ:      "kafka",
			env:      map[string]string{"hostname": "kafka-host", "port": "9092", "username": "app", "password": "secret"},
			wantOK:   true,
			want: conformance.ServiceEntry{
				Hostname: "events", Type: "kafka",
				Stream: &conformance.StreamDescriptor{Engine: "kafka", Host: "kafka-host", Port: "9092", User: "app", Password: "secret"},
			},
		},
		{
			name:     "nats stream credentials",
			hostname: "queue",
			typ:      "nats",
			env:      map[string]string{"hostname": "nats-host", "port": "4222", "user": "app", "password": "secret"},
			wantOK:   true,
			want: conformance.ServiceEntry{
				Hostname: "queue", Type: "nats",
				Stream: &conformance.StreamDescriptor{Engine: "nats", Host: "nats-host", Port: "4222", User: "app", Password: "secret"},
			},
		},
		{
			name:     "unclassifiable runtime type is skipped",
			hostname: "app",
			typ:      "nodejs@20",
			env:      map[string]string{"hostname": "app-host", "port": "3000"},
			wantOK:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := mapServiceEnv(tt.hostname, tt.typ, tt.env)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("entry = %#v, want %#v", got, tt.want)
			}
		})
	}
}
