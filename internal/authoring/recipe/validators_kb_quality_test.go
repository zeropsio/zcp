package recipe

import (
	"context"
	"testing"
)

// TestValidateKB_RejectsCitedGuideParaphrase — run-11 gap V-2. A KB
// bullet that just paraphrases the cited guide adds no porter-facing
// teaching beyond what zerops_knowledge already returns. Detected by
// Jaccard overlap of bullet body vs. guide key-phrase set.
func TestValidateKB_RejectsCitedGuideParaphrase(t *testing.T) {
	t.Parallel()

	body := []byte("# codebase/api\n" +
		"\n" +
		"<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n" +
		"### Gotchas\n" +
		"\n" +
		"- **L7 balancer routes subdomain traffic** — the L7 balancer " +
		"terminates SSL and routes traffic to the subdomain hostname on " +
		"configurable ports; binding 0.0.0.0 not localhost is required " +
		"for the container network. The subdomain DNS resolves through " +
		"the L7 balancer. Cited guide: `http-support`.\n" +
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n")
	vs, err := validateCodebaseKB(context.Background(), "codebases/api/README.md", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "kb-bullet-paraphrases-cited-guide") {
		t.Errorf("expected kb-bullet-paraphrases-cited-guide, got %+v", vs)
	}
}

// TestValidateKB_RejectsNoPlatformMention — run-11 gap V-3. A KB
// bullet with zero platform-side vocabulary (only framework concerns)
// is framework-quirk per spec → DISCARD. Run 10's worker bullet
// "Standalone context vs HTTP app" is the canonical case.
func TestValidateKB_RejectsNoPlatformMention(t *testing.T) {
	t.Parallel()

	body := []byte("# codebase/processor\n" +
		"\n" +
		"<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n" +
		"### Gotchas\n" +
		"\n" +
		"- **Standalone context vs Express app** — NestFactory bootstraps " +
		"the consumer via createApplicationContext, not create. The " +
		"standalone context skips the lifecycle of an Express server, so " +
		"OnModuleInit hooks fire but request-scope providers never " +
		"resolve. Use static module exports instead.\n" +
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n")
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "processor"}, {Hostname: "frontier"}},
	}
	vs, err := validateCodebaseKB(context.Background(), "codebases/processor/README.md", body, SurfaceInputs{Plan: plan})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "kb-bullet-no-platform-mention") {
		t.Errorf("expected kb-bullet-no-platform-mention, got %+v", vs)
	}
}

// TestValidateKB_AcceptsBulletNamingZeropsExplicitly — bullet that
// names a platform-side mechanism (subdomain, L7, etc.) is genuine
// platform teaching and passes V-3.
func TestValidateKB_AcceptsBulletNamingZeropsExplicitly(t *testing.T) {
	t.Parallel()

	body := []byte("# codebase/api\n" +
		"\n" +
		"<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n" +
		"### Gotchas\n" +
		"\n" +
		"- **Subdomain registration is two-step** — the L7 balancer " +
		"requires explicit subdomain activation; after enable the route " +
		"propagates asynchronously and a 502 for ~10 seconds is normal.\n" +
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n")
	vs, err := validateCodebaseKB(context.Background(), "codebases/api/README.md", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "kb-bullet-no-platform-mention") {
		t.Errorf("expected NO platform-mention violation, got %+v", vs)
	}
}

// TestValidateKB_AcceptsBulletMentioningRuntimeHostname — runtime
// hostnames named in Plan are platform mentions too. A bullet that
// references "appdev" / "workerdev" satisfies V-3.
func TestValidateKB_AcceptsBulletMentioningRuntimeHostname(t *testing.T) {
	t.Parallel()

	body := []byte("# codebase/worker\n" +
		"\n" +
		"<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n" +
		"### Gotchas\n" +
		"\n" +
		"- **NATS contract** — the worker codebase consumes from `articles.events` " +
		"with queue group `articles-workers`; the api publishes the same subject. " +
		"Both sides must agree on the wire format.\n" +
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n")
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "worker"}, {Hostname: "api"}},
		Services:  []Service{{Hostname: "nats", Kind: ServiceKindManaged}},
	}
	vs, err := validateCodebaseKB(context.Background(), "codebases/worker/README.md", body, SurfaceInputs{Plan: plan})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "kb-bullet-no-platform-mention") {
		t.Errorf("expected NO platform-mention violation when bullet names runtime hostnames, got %+v", vs)
	}
}

// TestValidateKB_CitedGuideBoilerplate_Flagged — run-11 gap O-2.
// KB bullets ending with literal "Cited guide: <name>." boilerplate
// are flagged. Citations belong in prose, not as a tail.
func TestValidateKB_CitedGuideBoilerplate_Flagged(t *testing.T) {
	t.Parallel()

	body := []byte("# codebase/api\n" +
		"\n" +
		"<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n" +
		"### Gotchas\n" +
		"\n" +
		"- **L7 Host header rewrite** — the L7 balancer rewrites the Host " +
		"header to the public subdomain; trust proxy must be enabled. " +
		"**Cited guide: `http-support`.**\n" +
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n")
	vs, err := validateCodebaseKB(context.Background(), "codebases/api/README.md", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "kb-cited-guide-boilerplate") {
		t.Errorf("expected kb-cited-guide-boilerplate, got %+v", vs)
	}
}

// TestValidateKB_InProseCitation_Passes — bullet citing a guide
// inline ("Per the http-support guide…") is the allowed shape.
func TestValidateKB_InProseCitation_Passes(t *testing.T) {
	t.Parallel()

	body := []byte("# codebase/api\n" +
		"\n" +
		"<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n" +
		"### Gotchas\n" +
		"\n" +
		"- **L7 Host header rewrite** — Per the http-support guide, the " +
		"L7 balancer rewrites the Host header to the public subdomain; " +
		"trust proxy must be enabled or req.hostname returns the VXLAN peer.\n" +
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n")
	vs, err := validateCodebaseKB(context.Background(), "codebases/api/README.md", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "kb-cited-guide-boilerplate") {
		t.Errorf("expected NO boilerplate violation for in-prose citation, got %+v", vs)
	}
}

// TestValidateKB_RejectsFirstPersonShape — run-11 gap V-4. A bullet
// using first-person/recipe-author voice ("we tried X", "I switched
// to Y", "after running") describes scaffold-debugging forensics, not
// platform teaching. Spec rule 4: discard.
func TestValidateKB_RejectsFirstPersonShape(t *testing.T) {
	t.Parallel()

	body := []byte("# codebase/api\n" +
		"\n" +
		"<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n" +
		"### Gotchas\n" +
		"\n" +
		"- **npx ts-node trap** — the L7 balancer fronts the api codebase " +
		"on Zerops; we tried npx ts-node first but it resolves against " +
		"~/.npm/_npx not project node_modules. The fix was switching to " +
		"node dist/migrate.js. After running this we discovered the " +
		"deploy bricks if you forget.\n" +
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n")
	vs, err := validateCodebaseKB(context.Background(), "codebases/api/README.md", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "kb-bullet-self-inflicted-shape") {
		t.Errorf("expected kb-bullet-self-inflicted-shape, got %+v", vs)
	}
}

// TestValidateKB_AcceptsPorterVoice — porter-voice prose passes V-4.
// Bullet talks to the porter ("the L7 balancer rewrites the Host
// header") not from the recipe author's debugging journey.
func TestValidateKB_AcceptsPorterVoice(t *testing.T) {
	t.Parallel()

	body := []byte("# codebase/api\n" +
		"\n" +
		"<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n" +
		"### Gotchas\n" +
		"\n" +
		"- **L7 Host header rewrite** — the L7 balancer rewrites the Host " +
		"header to the public subdomain; trust proxy must be enabled or " +
		"req.hostname returns the VXLAN peer.\n" +
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n")
	vs, err := validateCodebaseKB(context.Background(), "codebases/api/README.md", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "kb-bullet-self-inflicted-shape") {
		t.Errorf("expected NO self-inflicted-shape violation, got %+v", vs)
	}
}

// TestValidateKB_AcceptsCitedGuideExtension — bullet citing http-support
// with new content beyond the guide (a recipe-specific intersection)
// passes. The guide body's tokens make up < 50% of the bullet's
// vocabulary.
func TestValidateKB_AcceptsCitedGuideExtension(t *testing.T) {
	t.Parallel()

	body := []byte("# codebase/api\n" +
		"\n" +
		"<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n" +
		"### Gotchas\n" +
		"\n" +
		"- **Subdomain registration is two-step** — Per the http-support " +
		"guide the L7 balancer requires explicit subdomain activation; " +
		"after `zerops_subdomain action=enable` the route propagates " +
		"asynchronously and a 502 for ~10 seconds is normal. Watch the " +
		"Zerops dashboard subdomain card for state=ACTIVE before declaring " +
		"the deploy green. Worker codebases skip this step entirely.\n" +
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n")
	vs, err := validateCodebaseKB(context.Background(), "codebases/api/README.md", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "kb-bullet-paraphrases-cited-guide") {
		t.Errorf("expected NO paraphrase violation for extension bullet, got %+v", vs)
	}
}
