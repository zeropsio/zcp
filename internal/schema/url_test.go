package schema

import (
	"strings"
	"testing"
	"time"
)

// TestURLs pins the host-derivation normalization: it MIRRORS
// platform.resolveEndpoint (preserve explicit scheme, default https, keep port,
// trim trailing slash) and returns the canonical prg1 URLs byte-for-byte for the
// empty host so default users are unchanged.
func TestURLs(t *testing.T) {
	t.Parallel()
	const canonZerops = "https://api.app-prg1.zerops.io/api/rest/public/settings/zerops-yml-json-schema.json"
	const canonImport = "https://api.app-prg1.zerops.io/api/rest/public/settings/import-project-yml-json-schema.json"

	cases := []struct {
		name                   string
		host                   string
		wantZerops, wantImport string
	}{
		{"empty defaults to canonical (byte-exact)", "", canonZerops, canonImport},
		{"canonical host equals empty", "api.app-prg1.zerops.io", canonZerops, canonImport},
		{"bare host gets https", "api.example.com",
			"https://api.example.com/api/rest/public/settings/zerops-yml-json-schema.json",
			"https://api.example.com/api/rest/public/settings/import-project-yml-json-schema.json"},
		{"explicit http scheme preserved + trailing slash trimmed", "http://localhost:8080/",
			"http://localhost:8080/api/rest/public/settings/zerops-yml-json-schema.json",
			"http://localhost:8080/api/rest/public/settings/import-project-yml-json-schema.json"},
		{"explicit https + port kept", "https://zerops.internal:9000",
			"https://zerops.internal:9000/api/rest/public/settings/zerops-yml-json-schema.json",
			"https://zerops.internal:9000/api/rest/public/settings/import-project-yml-json-schema.json"},
		{"trailing slash trimmed on bare host", "api.example.com/",
			"https://api.example.com/api/rest/public/settings/zerops-yml-json-schema.json",
			"https://api.example.com/api/rest/public/settings/import-project-yml-json-schema.json"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			gotZ, gotI := URLs(c.host)
			if gotZ != c.wantZerops {
				t.Errorf("URLs(%q) zerops = %q, want %q", c.host, gotZ, c.wantZerops)
			}
			if gotI != c.wantImport {
				t.Errorf("URLs(%q) import = %q, want %q", c.host, gotI, c.wantImport)
			}
		})
	}
}

// TestNewCache_UsesAPIHost pins that the cache stores its host and the live
// fetch derives from it (asserted via URLs — no network).
func TestNewCache_UsesAPIHost(t *testing.T) {
	t.Parallel()
	c := NewCache(15*time.Minute, "api.example.com")
	if c.apiHost != "api.example.com" {
		t.Fatalf("NewCache did not store apiHost: got %q", c.apiHost)
	}
	z, _ := URLs(c.apiHost)
	if !strings.Contains(z, "api.example.com") {
		t.Errorf("cache host does not flow into fetch URL: %q", z)
	}
}

// TestSchemaCLIPinsCanonical pins that dev tooling targets the canonical host:
// FetchRawSchemas is called with schema.CanonicalAPIHost in cmd/zcp/schema.go,
// and URLs(CanonicalAPIHost) must equal URLs("") so the committed
// artifacts are stable regardless of ambient ZCP_API_HOST.
func TestSchemaCLIPinsCanonical(t *testing.T) {
	t.Parallel()
	zc, ic := URLs(CanonicalAPIHost)
	ze, ie := URLs("")
	if zc != ze || ic != ie {
		t.Errorf("CanonicalAPIHost must resolve identically to empty:\n canonical: %s | %s\n empty:     %s | %s", zc, ic, ze, ie)
	}
}
