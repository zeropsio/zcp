package server

import (
	"os"
	"strings"
	"testing"
)

func TestRuntimeSchemaCache_WiresAuthenticatedActiveProvider(t *testing.T) {
	t.Parallel()
	body, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("read server.go: %v", err)
	}
	const want = "schema.NewCache(schema.DefaultCacheTTL, s.authInfo.APIHost, s.client.ActiveServiceTypeVersions)"
	if !strings.Contains(string(body), want) {
		t.Fatalf("normal runtime schema cache is not wired to the authenticated ACTIVE provider; want %q", want)
	}
}
