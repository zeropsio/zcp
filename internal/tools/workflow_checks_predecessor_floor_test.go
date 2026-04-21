package tools

import (
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

// Tests for `checkKnowledgeBaseAuthenticity` — the upstream replacement
// for the now-deleted `checkKnowledgeBaseExceedsPredecessor` check.
// The exceeds-predecessor check was informational-only after the v8.78
// rollback and was dropped entirely in C-9. Authenticity grades gotcha
// SHAPE (platform-anchored or failure-mode described) rather than
// net-new count; the production quality bar moved to this check.

// TestCheckKnowledgeBaseAuthenticity_V12SyntheticMix replays the v12
// nestjs-showcase API README: 5 gotchas where only 1 (forcePathStyle)
// is a real platform trap. The other 4 are scaffold-self-referential
// narration — CORS env shadowing, afterInsert hooks, lazy NATS
// connection, ioredis store config. The authenticity check must fail
// so retries reach the v7 quality bar.
func TestCheckKnowledgeBaseAuthenticity_V12SyntheticMix(t *testing.T) {
	t.Parallel()
	kbContent := "### Gotchas\n\n" +
		"- **forcePathStyle: true for S3/MinIO** — Zerops object storage uses MinIO which requires path-style S3 URLs. The AWS SDK defaults to virtual-hosted style which fails with DNS resolution errors.\n" +
		"- **CORS origin must match the frontend URL** — the API sets FRONTEND_URL from the project-level STAGE_APP_URL. Use a different env var name (FRONTEND_URL) to avoid shadowing the project-level var.\n" +
		"- **Meilisearch index must be seeded explicitly** — TypeORM's afterInsert hooks don't fire during raw SQL seeding. The seed script must call the Meilisearch addDocuments API after inserting records.\n" +
		"- **NATS lazy connection pattern** — the NATS client connects on first publish, not at module init. This prevents the API from crashing at startup if the NATS service is still initializing.\n" +
		"- **Valkey cache-manager store configuration** — the cache-manager library for NestJS requires an ioredis-backed store adapter. Valkey is Redis-compatible but the connection must use plain TCP for internal traffic on Zerops.\n"
	checks := checkKnowledgeBaseAuthenticity(t.Context(), kbContent, "api")
	var authenticity *workflow.StepCheck
	for i := range checks {
		if checks[i].Name == "knowledge_base_authenticity" {
			authenticity = &checks[i]
			break
		}
	}
	if authenticity == nil {
		t.Fatalf("expected knowledge_base_authenticity check in results, got: %+v", checks)
	}
	if authenticity.Status != "fail" {
		t.Errorf("v12 synthetic mix should fail authenticity, got status=%q detail=%q", authenticity.Status, authenticity.Detail)
	}
}

// TestCheckKnowledgeBaseAuthenticity_V7Style replays v7-style authentic
// gotchas: every entry names a concrete Zerops constraint or a failure
// mode. The check must pass — this is the quality bar for showcase
// recipes written to the new scaffold-minimal + feature-subagent design.
func TestCheckKnowledgeBaseAuthenticity_V7Style(t *testing.T) {
	t.Parallel()
	kbContent := "### Gotchas\n\n" +
		"- **forcePathStyle: true for S3/MinIO** — Zerops object storage uses MinIO which requires path-style URLs. AWS SDK defaults to virtual-hosted style which fails with DNS resolution errors.\n" +
		"- **Trust proxy and bind 0.0.0.0** — Zerops terminates SSL at the L7 balancer. Without trust proxy Express sees every request as plain HTTP, breaking protocol detection and secure cookies.\n" +
		"- **execOnce on multi-container deploys** — migrations acquire advisory locks and must not race across horizontal containers. zsc execOnce guarantees exactly-one execution for a given appVersionId.\n" +
		"- **Vite dev server host-check** — Vite 6+ blocks requests from unrecognized hosts. The allowedHosts setting is required or the dev server returns Blocked request for the Zerops subdomain.\n" +
		"- **No .env files on Zerops** — Zerops injects all environment variables as OS-level env vars. Creating a .env file shadows the platform-injected values.\n"
	checks := checkKnowledgeBaseAuthenticity(t.Context(), kbContent, "api")
	var authenticity *workflow.StepCheck
	for i := range checks {
		if checks[i].Name == "knowledge_base_authenticity" {
			authenticity = &checks[i]
			break
		}
	}
	if authenticity == nil {
		t.Fatalf("expected knowledge_base_authenticity check in results, got: %+v", checks)
	}
	if authenticity.Status != "pass" {
		t.Errorf("v7-style authentic gotchas should pass authenticity, got status=%q detail=%q", authenticity.Status, authenticity.Detail)
	}
}
