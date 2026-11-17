package zcpadapter

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestDescriptorFor_LocalStorage_ReturnsUnsupportedFileConnection(t *testing.T) {
	t.Parallel()
	const serviceType = "local-storage:single@1"
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{service("local-id", "data", serviceType)}).
		WithServiceEnv("local-id", nil)
	adapter := New(client, &auth.Info{ProjectID: "p", ProjectName: "proj"})
	services, err := adapter.ManagedServices(context.Background())
	if err != nil {
		t.Fatalf("ManagedServices: %v", err)
	}
	if len(services) != 1 || services[0].Type != serviceType {
		t.Fatalf("Local Storage managed listing = %+v", services)
	}
	if provider.Classify(serviceType) != provider.FamilyFile || provider.SupportFor(serviceType) != provider.SupportNotYet {
		t.Fatalf("Local Storage provider profile = %q/%q", provider.Classify(serviceType), provider.SupportFor(serviceType))
	}
	_, err = adapter.ConnectionInfo(context.Background(), "local-id")
	if err == nil || !strings.Contains(err.Error(), `unsupported connection type "local-storage:single@1"`) {
		t.Fatalf("ConnectionInfo error = %v, want explicit unsupported file connection", err)
	}
}

func TestAdapter_ConnectionInfo_MapsEnvScalarsToTypedDescriptor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		typ  string
		env  map[string]string
		want provider.ConnectionDescriptor
	}{
		{
			name: "object storage",
			typ:  "object-storage",
			env: map[string]string{
				"apiHost":         "s3.example.internal",
				"apiUrl":          "https://s3.example.internal",
				"accessKeyId":     "access",
				"secretAccessKey": "secret",
				"bucketName":      "bucket",
			},
			want: provider.ObjectConn{
				Endpoint: "s3.example.internal", AccessKey: "access", SecretKey: "secret",
				Bucket: "bucket", Secure: true,
			},
		},
		{
			name: "postgresql",
			typ:  "postgresql:single@18",
			env: map[string]string{
				"hostname": "pg", "port": "5432", "user": "app", "password": "secret", "dbName": "appdb",
			},
			want: provider.SQLConn{
				Driver: "pgx", Dialect: "postgresql", Host: "pg", Port: "5432",
				User: "app", Password: "secret", Database: "appdb",
			},
		},
		{
			name: "mariadb db fallback",
			typ:  "mariadb@10.6",
			env:  map[string]string{"hostname": "maria", "port": "3306", "user": "app", "password": "secret"},
			want: provider.SQLConn{
				Driver: "mysql", Dialect: "mariadb", Host: "maria", Port: "3306",
				User: "app", Password: "secret", Database: "app",
			},
		},
		{
			name: "clickhouse native port and defaults",
			typ:  "clickhouse@24",
			env: map[string]string{
				"hostname": "ch", "port": "8123", "portNative": "9000", "superUser": "admin", "password": "secret",
			},
			want: provider.SQLConn{
				Driver: "clickhouse", Dialect: "clickhouse", Host: "ch", Port: "9000",
				User: "admin", Password: "secret", Database: "default",
			},
		},
		{
			name: "valkey",
			typ:  "valkey@7",
			env:  map[string]string{"hostname": "cache", "port": "6379", "password": "secret"},
			want: provider.KVConn{Host: "cache", Port: "6379", Password: "secret"},
		},
		{
			name: "elasticsearch credential fallbacks",
			typ:  "elasticsearch@8",
			env:  map[string]string{"hostname": "es", "port": "9200", "username": "elastic2", "elasticPassword": "secret"},
			want: provider.DocumentConn{Engine: "elasticsearch", BaseURL: "http://es:9200", User: "elastic2", APIKey: "secret"},
		},
		{
			name: "meilisearch api key fallback",
			typ:  "meilisearch",
			env:  map[string]string{"hostname": "meili", "port": "7700", "defaultAdminKey": "secret"},
			want: provider.DocumentConn{Engine: "meilisearch", BaseURL: "http://meili:7700", APIKey: "secret"},
		},
		{
			name: "typesense api key fallback",
			typ:  "typesense",
			env:  map[string]string{"hostname": "typesense", "port": "8108", "adminApiKey": "secret"},
			want: provider.DocumentConn{Engine: "typesense", BaseURL: "http://typesense:8108", APIKey: "secret"},
		},
		{
			name: "qdrant api key fallback",
			typ:  "qdrant",
			env:  map[string]string{"hostname": "qdrant", "port": "6333", "password": "secret"},
			want: provider.DocumentConn{Engine: "qdrant", BaseURL: "http://qdrant:6333", APIKey: "secret"},
		},
		{
			name: "stream credentials",
			typ:  "kafka",
			env:  map[string]string{"hostname": "kafka", "port": "9092", "username": "app", "password": "secret"},
			want: provider.StreamConn{Engine: "kafka", Host: "kafka", Port: "9092", User: "app", Password: "secret"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := platform.NewMock().
				WithServices([]platform.ServiceStack{service("svc", "managed", tt.typ)}).
				WithServiceEnv("svc", serviceEnv(tt.env))
			adapter := New(client, &auth.Info{ProjectID: "p", ProjectName: "proj"})

			if _, err := adapter.ManagedServices(context.Background()); err != nil {
				t.Fatalf("managed services: %v", err)
			}
			got, err := adapter.ConnectionInfo(context.Background(), "svc")
			if err != nil {
				t.Fatalf("connection info: %v", err)
			}
			if got.Type != tt.typ {
				t.Fatalf("type = %q, want %q", got.Type, tt.typ)
			}
			if got.Family != provider.Classify(tt.typ) {
				t.Fatalf("family = %q, want %q", got.Family, provider.Classify(tt.typ))
			}
			if !reflect.DeepEqual(got.Descriptor, tt.want) {
				t.Fatalf("descriptor = %#v, want %#v", got.Descriptor, tt.want)
			}
		})
	}
}

func service(id, hostname, typ string) platform.ServiceStack {
	return platform.ServiceStack{
		ID:   id,
		Name: hostname,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName: typ,
		},
		Status: platform.ServiceStatusRunning,
	}
}

func serviceEnv(env map[string]string) []platform.ServiceEnvVar {
	out := make([]platform.ServiceEnvVar, 0, len(env))
	for k, v := range env {
		out = append(out, platform.ServiceEnvVar{Key: k, Content: v})
	}
	return out
}

// maskedPostgres is a postgres service whose password the API withholds from
// a project-scoped key.
func maskedPostgres() *platform.Mock {
	return platform.NewMock().
		WithServices([]platform.ServiceStack{service("pg-id", "db", "postgresql@16")}).
		WithServiceEnv("pg-id", []platform.ServiceEnvVar{
			{ID: "e1", Key: "hostname", Content: "db"},
			{ID: "e2", Key: "port", Content: "5432"},
			{ID: "e3", Key: "user", Content: "app"},
			{ID: "e4", Key: "dbName", Content: "app"},
			{ID: "e5", Key: "password", Content: platform.MaskedEnvValue, Sensitive: true},
		})
}

// storeOf is a container env store holding exactly env.
func storeOf(env map[string]string) func(string) (string, bool, error) {
	return func(key string) (string, bool, error) {
		v, ok := env[key]
		return v, ok, nil
	}
}

// witness: internal/dataconsole/zcpadapter/adapter.go | scalars[key] = v → _ = v
func TestAdapter_ConnectionInfo_ValueTheAPIWithholds_IsReadFromTheContainersEnvStore(t *testing.T) {
	t.Parallel()
	adapter := New(maskedPostgres(), &auth.Info{ProjectID: "p", ProjectName: "proj"})
	adapter.env = storeOf(map[string]string{"db_password": "s3cret", "other_password": "wrong"})
	if _, err := adapter.ManagedServices(context.Background()); err != nil {
		t.Fatalf("ManagedServices: %v", err)
	}
	info, err := adapter.ConnectionInfo(context.Background(), "pg-id")
	if err != nil {
		t.Fatalf("ConnectionInfo: %v", err)
	}
	conn, ok := info.Descriptor.(provider.SQLConn)
	if !ok || conn.Password != "s3cret" || conn.Host != "db" || conn.User != "app" || conn.Database != "app" {
		t.Fatalf("descriptor = %#v, want the API's plain values and the store's db_password", info.Descriptor)
	}
}

// witness: internal/dataconsole/zcpadapter/adapter.go | if len(vars.missing) > 0 { → if len(vars.absent) > 0 {
func TestAdapter_ConnectionInfo_WithheldKeyTheConnectionNeverReads_RefusesNothing(t *testing.T) {
	t.Parallel()
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{service("ms-id", "search", "meilisearch@1.44")}).
		WithServiceEnv("ms-id", []platform.ServiceEnvVar{
			{ID: "e1", Key: "hostname", Content: "search"},
			{ID: "e2", Key: "port", Content: "7700"},
			{ID: "e3", Key: "masterKey", Content: platform.MaskedEnvValue, Sensitive: true},
			{ID: "e4", Key: "defaultSearchKey", Content: platform.MaskedEnvValue, Sensitive: true},
		})
	adapter := New(client, &auth.Info{ProjectID: "p", ProjectName: "proj"})
	adapter.env = storeOf(map[string]string{"search_masterKey": "m4ster"})
	if _, err := adapter.ManagedServices(context.Background()); err != nil {
		t.Fatalf("ManagedServices: %v", err)
	}
	info, err := adapter.ConnectionInfo(context.Background(), "ms-id")
	if err != nil {
		t.Fatalf("ConnectionInfo = %v, want the connection: defaultSearchKey is withheld and absent, but the connection reads masterKey", err)
	}
	if conn, ok := info.Descriptor.(provider.DocumentConn); !ok || conn.APIKey != "m4ster" {
		t.Fatalf("descriptor = %#v, want the store's masterKey", info.Descriptor)
	}
}

// witness: internal/dataconsole/zcpadapter/adapter.go | if !found { → if found {
func TestAdapter_ConnectionInfo_ServiceThatDoesNotShareItsVariables_AnswersNoCredentialsNamingServiceAndKey(t *testing.T) {
	t.Parallel()
	adapter := New(maskedPostgres(), &auth.Info{ProjectID: "p", ProjectName: "proj"})
	adapter.env = storeOf(map[string]string{"other_password": "s3cret"})
	if _, err := adapter.ManagedServices(context.Background()); err != nil {
		t.Fatalf("ManagedServices: %v", err)
	}
	_, err := adapter.ConnectionInfo(context.Background(), "pg-id")
	if !errors.Is(err, provider.ErrNoCredentials) || provider.ErrorCode(err) != "no_credentials" {
		t.Fatalf("ConnectionInfo error = %v, want ErrNoCredentials", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "db") || !strings.Contains(msg, "password") || strings.Contains(msg, "s3cret") || strings.Contains(msg, platform.MaskedEnvValue) {
		t.Fatalf("error text = %q, want the service and key named and no value", msg)
	}
}
