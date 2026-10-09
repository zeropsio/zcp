package console

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
	"github.com/zeropsio/zcp/internal/dataconsole/console/safety"
)

type summaryHost struct{ descriptor provider.ConnectionDescriptor }

func (summaryHost) Project(context.Context) (ProjectRef, error) { return ProjectRef{}, nil }
func (summaryHost) ManagedServices(context.Context) ([]ManagedServiceRef, error) {
	return []ManagedServiceRef{{ID: "1", Hostname: "db", Type: "postgresql"}}, nil
}
func (h summaryHost) ConnectionInfo(context.Context, string) (ConnectionInfo, error) {
	return ConnectionInfo{Descriptor: h.descriptor}, nil
}

func TestConnectionSecretsStayInTheContainer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		descriptor provider.ConnectionDescriptor
		want       string
	}{
		{"postgres", provider.SQLConn{Host: "db", Port: "5432", User: "secret-user", Password: "secret-password", Database: "app"}, "postgresql://••••:••••@db:5432/app"},
		{"mysql", provider.SQLConn{Driver: "mysql", Host: "db", Port: "3306", User: "secret-user", Password: "secret-password", Database: "app"}, "mysql://••••:••••@db:3306/app"},
		{"redis", provider.KVConn{Host: "cache", Port: "6379", Password: "secret-password"}, "redis://:••••@cache:6379"},
		{"object", provider.ObjectConn{Endpoint: "https://secret-user:secret-password@storage.example?signature=secret-signature", Bucket: "assets", AccessKey: "secret-access", SecretKey: "secret-key"}, "https://storage.example/assets"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			engine := NewEngine(summaryHost{tc.descriptor}, safety.NewPolicy(false, "", ""), nil)
			if err := engine.Refresh(context.Background()); err != nil {
				t.Fatal(err)
			}
			summary, err := engine.Summary(context.Background(), "db")
			if err != nil {
				t.Fatal(err)
			}
			if summary.MaskedConnection != tc.want {
				t.Fatalf("summary = %q, want %q", summary.MaskedConnection, tc.want)
			}
			wire, err := json.Marshal(summary)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(wire), "secret-") {
				t.Fatal("connection secret escaped")
			}
		})
	}
}
