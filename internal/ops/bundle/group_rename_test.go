package bundle

import (
	"maps"
	"testing"
)

// A group environment has one runtime per pair, under its promoted name, so
// a value naming a pair's dev or stage half — a `${medusastage_…}`
// reference, an `http://medusastage:9000` — names a service that is not
// there. The Mate's project is where a pair's environment-specific wiring
// lives (a storefront reaching its backend through MEDUSA_INTERNAL_URL), and
// copied as it is into a stage or a production it points nowhere. So the
// group environments name their own runtimes; the AI Agent tier, which
// re-creates the Mate's pairs, keeps every value as written.
func TestPromotePairHostnames(t *testing.T) {
	t.Parallel()
	renames := map[string]string{
		"medusadev": "medusa", "medusastage": "medusa",
		"nextstoredev": "nextstore", "nextstorestage": "nextstore",
		"api-dev": "api",
	}
	tests := []struct {
		name, value, want string
	}{
		{"a stage half's reference", "${medusastage_hostname}", "${medusa_hostname}"},
		{"a dev half's reference", "${medusadev_CHANNEL_PUBLISHABLE_KEY}", "${medusa_CHANNEL_PUBLISHABLE_KEY}"},
		{"a reference as the platform spells a dashed hostname", "${api_dev_port}", "${api_port}"},
		{"references among other text", "http://${medusastage_hostname}:${medusastage_port}", "http://${medusa_hostname}:${medusa_port}"},
		{"an internal URL", "http://medusastage:9000", "http://medusa:9000"},
		{"a URL with a path", "http://nextstorestage/api", "http://nextstore/api"},
		{"a URL ending on the host", "http://medusastage", "http://medusa"},
		{"a subdomain URL", "https://medusadev-${zeropsSubdomainHost}-9000.prg1.zerops.app", "https://medusa-${zeropsSubdomainHost}-9000.prg1.zerops.app"},
		{"a list of URLs", "https://nextstoredev-${zeropsSubdomainHost}-8000.prg1.zerops.app,https://nextstorestage-${zeropsSubdomainHost}-8000.prg1.zerops.app", "https://nextstore-${zeropsSubdomainHost}-8000.prg1.zerops.app,https://nextstore-${zeropsSubdomainHost}-8000.prg1.zerops.app"},
		{"another service's reference is its own", "${db_hostname}", "${db_hostname}"},
		{"a longer hostname is another service", "http://medusadevtools:1 ${medusadevtools_x}", "http://medusadevtools:1 ${medusadevtools_x}"},
		{"a bare word is not a hostname", "medusastage", "medusastage"},
		{"a custom domain is not a hostname", "https://shop.medusastage.example.com", "https://shop.medusastage.example.com"},
		{"nor one that opens on the hostname", "https://medusastage.example.com", "https://medusastage.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := promotePairHostnames(tt.value, renames); got != tt.want {
				t.Errorf("promotePairHostnames(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestBuildGroupRecipe_GroupEnvironmentsNameTheirOwnRuntimes(t *testing.T) {
	t.Parallel()
	in := groupInputsFixture()
	in.ProjectEnvs = []ProjectEnvVar{
		{Key: "API_INTERNAL_URL", Value: "http://apistage:3000"},
		{Key: "API_PUBLIC_KEY", Value: "${apistage_PUBLIC_KEY}"},
		{Key: "DB_HOST", Value: "${db_hostname}"},
	}
	in.Runtimes[0].StageServiceEnvs = []ProjectEnvVar{{Key: "SELF_URL", Value: "https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app"}}
	layout, _, err := BuildGroupRecipe(in)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	asWritten := map[string]string{"API_INTERNAL_URL": "http://apistage:3000", "API_PUBLIC_KEY": "${apistage_PUBLIC_KEY}", "DB_HOST": "${db_hostname}"}
	promoted := map[string]string{"API_INTERNAL_URL": "http://api:3000", "API_PUBLIC_KEY": "${api_PUBLIC_KEY}", "DB_HOST": "${db_hostname}"}
	tests := []struct {
		tier, host, selfURL string
		project             map[string]string
	}{
		{"AI Agent", "apistage", "https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app", asWritten},
		{"Stage", "api", "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app", promoted},
		{"Small Production", "api", "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app", promoted},
	}
	for _, tt := range tests {
		t.Run(tt.tier, func(t *testing.T) {
			t.Parallel()
			body := tierBody(t, layout.Tiers, tt.tier)
			project := mappingValue(tierMapping(t, body), "project")
			if got := scalarMap(mappingValue(project, "envVariables")); !maps.Equal(got, tt.project) {
				t.Errorf("envVariables = %v, want %v", got, tt.project)
			}
			secrets := scalarMap(mappingValue(serviceNode(t, body, tt.host), "envSecrets"))
			if got := secrets["SELF_URL"]; got != tt.selfURL {
				t.Errorf("%s SELF_URL = %q, want %q", tt.host, got, tt.selfURL)
			}
		})
	}
}
