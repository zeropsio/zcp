package workflow

import (
	"strings"
	"testing"
)

// TestClassifyGotcha exercises the shape classifier that distinguishes
// authentic platform/failure-mode gotchas from synthetic architectural
// narration. The v12 audit found roughly half of emitted gotchas were
// self-referential quirks of the scaffold's own code rather than real
// problems a fresh integrator would hit. The classifier is the forcing
// function for v13: synthetic gotchas don't count toward the floor.
func TestClassifyGotcha(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		stem string
		body string
		want GotchaShape
	}{
		{
			name: "real — forcePathStyle S3 with DNS failure mode",
			stem: "forcePathStyle: true for S3/MinIO",
			body: "Zerops object storage uses MinIO which requires path-style S3 URLs. The AWS SDK defaults to virtual-hosted style which fails with DNS resolution errors.",
			want: ShapeAuthentic,
		},
		{
			name: "real — Vite host-check with concrete symptom",
			stem: "Vite dev server host-check",
			body: "Vite 6+ blocks requests from unrecognized hosts. The allowedHosts setting in vite.config.js is required or the dev server returns \"Blocked request\" for the Zerops subdomain.",
			want: ShapeAuthentic,
		},
		{
			name: "real — trust proxy behind L7",
			stem: "Trust proxy and bind 0.0.0.0",
			body: "Zerops terminates SSL at the L7 balancer and forwards via reverse proxy. Without trust proxy, Express sees every request as plain HTTP from an internal IP, breaking protocol detection and secure cookies.",
			want: ShapeAuthentic,
		},
		{
			name: "real — tilde in deployFiles with concrete effect",
			stem: "Tilde suffix in deployFiles",
			body: "The tilde suffix strips the parent directory so files land at /var/www/index.html instead of /var/www/dist/index.html. Required for static/Nginx base which serves from /var/www/.",
			want: ShapeAuthentic,
		},
		{
			name: "real — worker has no HTTP ports (zsc noop)",
			stem: "Worker has no HTTP ports",
			body: "Never add ports, healthCheck, or readinessCheck to zerops.yaml. The process stays alive via the NATS subscription loop, not by listening on a port.",
			want: ShapeAuthentic,
		},
		{
			name: "real — execOnce advisory lock",
			stem: "execOnce on multi-container deploys",
			body: "Migrations acquire advisory locks and must not race across horizontal containers. zsc execOnce guarantees exactly-one execution across all containers for a given appVersionId.",
			want: ShapeAuthentic,
		},
		{
			name: "synthetic — shared database architectural narration",
			stem: "Shared database with the API",
			body: "The worker and API share the same PostgreSQL database. Schema migrations are owned by the API service — the worker uses synchronize: false and never alters the schema.",
			want: ShapeSynthetic,
		},
		{
			name: "synthetic — NATS credentials description",
			stem: "NATS authentication",
			body: "The NATS connection uses username/password auth from Zerops-managed credentials injected via queue_user and queue_password.",
			want: ShapeSynthetic,
		},
		{
			name: "synthetic — static has no Node restatement",
			stem: "Static base has no Node runtime",
			body: "The static runtime provides Nginx only. No shell, no package manager, no server-side logic. All processing happens at build time.",
			want: ShapeSynthetic,
		},
		{
			name: "synthetic — afterInsert hooks quirk of own seed script",
			stem: "Meilisearch index must be seeded explicitly",
			body: "TypeORM's afterInsert hooks don't fire during raw SQL seeding. The seed script must call the Meilisearch addDocuments API after inserting records.",
			want: ShapeSynthetic,
		},
		{
			name: "synthetic — NATS lazy connection pattern dressed as gotcha",
			stem: "NATS lazy connection pattern",
			body: "The NATS client connects on first publish, not at module init. This prevents the API from crashing at startup if the NATS service is still initializing.",
			want: ShapeSynthetic,
		},
		{
			name: "real — VITE build-time only with concrete trap",
			stem: "VITE_* env vars are build-time only in prod",
			body: "In production builds, import.meta.env.VITE_* references are statically replaced at build time. The bundle contains hardcoded strings. Developers who expect runtime env vars in prod find their values missing.",
			want: ShapeAuthentic,
		},
		{
			name: "real — no .env on Zerops",
			stem: "No .env files on Zerops",
			body: "Zerops injects all environment variables as OS-level env vars. Creating a .env file with empty values will shadow the platform-injected values.",
			want: ShapeAuthentic,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyGotcha(tt.stem, tt.body)
			if got != tt.want {
				t.Errorf("ClassifyGotcha() = %v, want %v\n  stem: %q\n  body: %q", got, tt.want, tt.stem, tt.body)
			}
		})
	}
}

// TestExtractGotchaEntries parses a knowledge-base fragment and returns
// both the stem and the body for each bullet, so the shape classifier can
// score the full text.
func TestExtractGotchaEntries(t *testing.T) {
	t.Parallel()
	input := "### Gotchas\n\n" +
		"- **First gotcha** — body text that describes a failure mode with error messages.\n" +
		"- **Second gotcha** — body text that mentions Zerops L7 balancer and ${env_var}.\n"
	entries := ExtractGotchaEntries(input)
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	if entries[0].Stem != "First gotcha" {
		t.Errorf("entry 0 stem = %q", entries[0].Stem)
	}
	if !strings.Contains(entries[0].Body, "failure mode") {
		t.Errorf("entry 0 body missing failure mode text: %q", entries[0].Body)
	}
	if entries[1].Stem != "Second gotcha" {
		t.Errorf("entry 1 stem = %q", entries[1].Stem)
	}
	if !strings.Contains(entries[1].Body, "L7 balancer") {
		t.Errorf("entry 1 body missing L7 balancer text: %q", entries[1].Body)
	}
}
