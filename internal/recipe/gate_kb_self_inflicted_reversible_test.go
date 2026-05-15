package recipe

import (
	"strings"
	"testing"
)

// TestGateKBSelfInflictedReversible_Run48Cases pins every named
// run-48 audit case (per the recalibration brief) against the new
// gate. The fixtures wire a plan that ships the matching directive
// on the recipe-side surface; the gate MUST refuse the KB body that
// describes the symptom whose only trigger is undoing that directive.
func TestGateKBSelfInflictedReversible_Run48Cases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		hostname     string
		kbBody       string
		yamlFragment string // codebase/<host>/zerops-yaml
		igFragment   string // codebase/<host>/integration-guide
		claudeMD     string // codebase/<host>/claude-md
		wantRefusal  bool
		wantMatch    string // substring expected in the refusal Message
	}{
		{
			name:     "env-file-in-deployed-tree — recipe ships ignoreEnvFile:true (REFUSE)",
			hostname: "api",
			kbBody: "### Gotchas\n\n" +
				"- **No `.env` file in the deployed tree** — Zerops injects env vars at the container env directly; a `.env` file shipped in the source tree would shadow them. The porter never creates one if they follow the IG.",
			yamlFragment: "zerops:\n  - setup: api\n    build:\n      base: nodejs@22\n    run:\n      base: nodejs@22\n      ignoreEnvFile: true\n      envVariables:\n        FOO: bar\n",
			wantRefusal:  true,
			wantMatch:    "ignoreEnvFile",
		},
		{
			name:     "custom-response-headers-undefined-from-spa — recipe ships exposedHeaders in IG (REFUSE)",
			hostname: "api",
			kbBody: "### Gotchas\n\n" +
				"- **Custom response headers return `undefined` from the SPA but show up in `curl`** — browsers strip non-CORS-safelisted headers cross-origin unless the server lists them under `Access-Control-Expose-Headers`.",
			igFragment:  "### 2. Configure CORS exposedHeaders\n\nThe SPA reads `X-Cache` from API responses; CORS strips it unless the server exposes it explicitly:\n\n```yaml\nexposedHeaders: ['X-Cache']\n```\n",
			wantRefusal: true,
			wantMatch:   "exposedHeaders",
		},
		{
			name:     "start-directive-on-base-static — recipe ships base:static (REFUSE)",
			hostname: "app",
			kbBody: "### Gotchas\n\n" +
				"- **`start:` directive on `base: static` is silently ignored** — the static runtime has no process to start; adding `start:` is a no-op that confuses ported-from-Node deployments.",
			yamlFragment: "zerops:\n  - setup: app\n    build:\n      base: nodejs@22\n    run:\n      base: static\n      deployFiles: ./dist/~\n",
			wantRefusal:  true,
			wantMatch:    "base: static",
		},
		{
			name:     "execonce-key-collision — recipe ships scoped execOnce keys (REFUSE)",
			hostname: "worker",
			kbBody: "### Gotchas\n\n" +
				"- **`relation \"job_log\" already exists` after a co-deployed migrator race** — multiple migrator containers race to CREATE TABLE; the second loses with a relation-already-exists error.",
			yamlFragment: "zerops:\n  - setup: worker\n    run:\n      initCommands:\n        - command: zsc execOnce worker-migrate-v1 -- npm run migrate\n",
			wantRefusal:  true,
			wantMatch:    "execOnce",
		},
		{
			name:     "ioredis-auth-against-unauth-valkey — recipe omits cache password alias (REFUSE)",
			hostname: "worker",
			kbBody: "### Gotchas\n\n" +
				"- **`ioredis` sends garbage AUTH commands when wired to Valkey with a password** — Zerops Valkey runs unauth by default; aliasing `${cache_password}` makes ioredis send AUTH against an unauth instance and the connection drops.",
			yamlFragment: "zerops:\n  - setup: worker\n    run:\n      envVariables:\n        CACHE_HOST: ${cache_hostname}\n        CACHE_PORT: ${cache_port}\n",
			wantRefusal:  true,
			wantMatch:    "cache `password`",
		},
		{
			name:     "POSITIVE — forward-looking adaptation-cost bullet (ACCEPT)",
			hostname: "app",
			kbBody: "### Custom Domain Rotation\n\n" +
				"When you swap `apistage` for a custom domain, the auto-injected `API_URL` env var stops tracking the new origin and needs manual rotation. The yaml entry for `API_URL` is the only adapt-path you need.",
			yamlFragment: "zerops:\n  - setup: app\n    run:\n      base: static\n",
			wantRefusal:  false,
		},
		{
			name:         "POSITIVE — platform-constraint bullet (ACCEPT)",
			hostname:     "worker",
			kbBody:       "### Object Storage backend",
			yamlFragment: "zerops:\n  - setup: worker\n    run:\n      base: nodejs@22\n",
			// "Object Storage runs on a MinIO backend — no Glacier/Object Lock" should pass.
			// No pattern in the table matches this prose. Empty KB body is also accepted.
			wantRefusal: false,
		},
		{
			name:        "EDGE — empty KB body — no violations",
			hostname:    "api",
			kbBody:      "",
			wantRefusal: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := &Plan{
				Codebases: []Codebase{{Hostname: tc.hostname, SourceRoot: tc.hostname}},
				Fragments: map[string]string{},
			}
			if tc.kbBody != "" {
				plan.Fragments["codebase/"+tc.hostname+"/knowledge-base"] = tc.kbBody
			}
			if tc.yamlFragment != "" {
				plan.Fragments["codebase/"+tc.hostname+"/zerops-yaml"] = tc.yamlFragment
			}
			if tc.igFragment != "" {
				plan.Fragments["codebase/"+tc.hostname+"/integration-guide"] = tc.igFragment
			}
			if tc.claudeMD != "" {
				plan.Fragments["codebase/"+tc.hostname+"/claude-md"] = tc.claudeMD
			}
			vs := gateKBSelfInflictedReversible(GateContext{Plan: plan})
			got := len(vs) > 0
			if got != tc.wantRefusal {
				t.Fatalf("gateKBSelfInflictedReversible refusal=%v, want %v: %+v", got, tc.wantRefusal, vs)
			}
			if tc.wantRefusal && tc.wantMatch != "" {
				matched := false
				for _, v := range vs {
					if strings.Contains(v.Message, tc.wantMatch) {
						matched = true
						break
					}
				}
				if !matched {
					t.Errorf("expected refusal to mention %q; got messages: %+v", tc.wantMatch, vs)
				}
			}
		})
	}
}

// TestGateKBSelfInflictedReversible_EmptyPlan — defensive: gate must
// no-op on nil Plan.
func TestGateKBSelfInflictedReversible_EmptyPlan(t *testing.T) {
	t.Parallel()
	if vs := gateKBSelfInflictedReversible(GateContext{}); len(vs) != 0 {
		t.Errorf("empty GateContext: expected 0 violations, got %d (%+v)", len(vs), vs)
	}
}

// TestGateKBSelfInflictedReversible_NoDirectiveShipped — gate must
// NOT fire when the KB trigger pattern matches but the recipe does
// not ship the directive. Catches false-positives on platform-only
// content that happens to share vocabulary.
func TestGateKBSelfInflictedReversible_NoDirectiveShipped(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "api", SourceRoot: "api"}},
		Fragments: map[string]string{
			"codebase/api/knowledge-base": "### Gotchas\n\n" +
				"- **No `.env` file in deployed tree** — text mentioning the .env vector, but the recipe DOES NOT ship `ignoreEnvFile: true`. The discriminator must NOT fire.",
			"codebase/api/zerops-yaml": "zerops:\n  - setup: api\n    run:\n      base: nodejs@22\n",
		},
	}
	if vs := gateKBSelfInflictedReversible(GateContext{Plan: plan}); len(vs) != 0 {
		t.Errorf("trigger matched but directive NOT shipped: expected 0 violations, got %d (%+v)", len(vs), vs)
	}
}

// TestCodebaseContentGates_KBSelfInflictedReversibleRegistered pins
// the gate's registration so refactors of CodebaseContentGates can't
// silently drop it.
func TestCodebaseContentGates_KBSelfInflictedReversibleRegistered(t *testing.T) {
	t.Parallel()
	if !gateRegistered(CodebaseContentGates(), "kb-self-inflicted-reversible") {
		t.Fatalf("CodebaseContentGates missing kb-self-inflicted-reversible")
	}
}
