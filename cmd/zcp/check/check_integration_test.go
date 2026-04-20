package check

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFixture materializes a minimal-but-complete recipe mount under
// tmpDir. One codebase mount (apidev/) + one environment (0 — AI Agent/)
// with valid content for every shim. Fixtures are deliberately kept
// small — predicate correctness is exercised in the ops/checks package's
// own tests. This fixture proves the CLI plumbing reaches each predicate
// and emits pass/fail output that the exit code reflects.
func writeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	files := map[string]string{
		"apidev/zerops.yaml": `zerops:
  - setup: dev
    envVariables:
      APP_PORT: "3000"
      LOG_LEVEL: info
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
      deployFiles:
        - ./
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node server.js
`,
		"apidev/README.md": `# apidev

<!-- #ZEROPS_EXTRACT_START:intro# -->
Some intro text naming the zerops platform and a real failure mode:
requests fail because the port binds to 127.0.0.1 by default, which
prevents the L7 balancer from reaching the container. Bind to 0.0.0.0
so traffic routed through the zerops subdomain arrives at the process.
<!-- #ZEROPS_EXTRACT_END:intro# -->

<!-- #ZEROPS_EXTRACT_START:integration-guide# -->
### 1. zerops.yaml

` + "```yaml" + `
zerops:
  - setup: dev
    # This comment names zerops L7 balancer explicitly because the
    # httpSupport flag on the port entry is the reason the balancer
    # terminates ssl for us; without it the subdomain would return 502.
    run:
      ports:
        - port: 3000
          httpSupport: true
    # execOnce on prepare commands prevents the step from running on
    # every horizontal container fresh start — required so build time
    # installs aren't duplicated across runtime replicas.
` + "```" + `

### 2. Bind to 0.0.0.0

` + "```typescript" + `
// zerops L7 balancer reaches the container over the internal VXLAN,
// so server must bind to 0.0.0.0 instead of localhost otherwise
// requests fail at the balancer with 502 connection refused.
server.listen(3000, "0.0.0.0");
` + "```" + `
<!-- #ZEROPS_EXTRACT_END:integration-guide# -->

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
## Gotchas
- **Port binds to 127.0.0.1 by default** — fails with 502 because zerops L7 balancer cannot reach the container; bind to 0.0.0.0.
- **httpSupport flag required on ports** — without it zerops terminates ssl externally but the container expects plain http, so requests fail at the balancer.
- **execOnce on prepareCommands** — prevents the step from running on every fresh container; required so build time installs aren't duplicated across runtime replicas.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`,
		"apidev/CLAUDE.md":    "# apidev CLAUDE\n\nRun the dev loop from here.\n",
		"apidev/.env.example": "DB_HOST=db\nDB_PASSWORD=secret\n",
		"workerdev/zerops.yaml": `zerops:
  - setup: dev
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
      deployFiles:
        - ./
    run:
      base: nodejs@22
      start: node worker.js
`,
		"workerdev/README.md": `# workerdev

<!-- #ZEROPS_EXTRACT_START:intro# -->
Worker running alongside the api.
<!-- #ZEROPS_EXTRACT_END:intro# -->

<!-- #ZEROPS_EXTRACT_START:integration-guide# -->
### 1. zerops.yaml

` + "```yaml" + `
zerops:
  - setup: dev
    run:
      start: node worker.js
` + "```" + `
<!-- #ZEROPS_EXTRACT_END:integration-guide# -->

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
## Gotchas
- **NATS queue group required when minContainers > 1** — without a queue group every replica double-processes each message; fails silently under scale.
- **SIGTERM drain for graceful shutdown during rolling deploys** — catch SIGTERM, call nc.drain(), await, then exit(0); otherwise in-flight messages are lost.
- **execOnce on prepare for build time installs** — prevents duplicate work across horizontal container restarts.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`,
		"ZCP_CONTENT_MANIFEST.json": `{
  "version": 1,
  "facts": [
    {
      "fact_title": "Port binds to 127.0.0.1 by default",
      "classification": "platform-trap",
      "routed_to": "content_gotcha",
      "override_reason": ""
    }
  ]
}`,
		"0 \u2014 AI Agent/import.yaml": `project:
  name: fixture-agent
  tags:
    - recipe:fixture
services:
  # apidev runs in dev mode because rebuild is cheap and the iteration
  # loop matters more than uptime here; standard mode is overkill for
  # an AI-agent tier that restarts often.
  - hostname: api
    type: nodejs@22
    mode: DEV
    # minContainers 1 matches the agent tier because rolling deploys
  # are not needed at this scale — a single container restart during
  # redeploy is acceptable.
    minContainers: 1
`,
		"0 \u2014 AI Agent/README.md": "# AI Agent env\n",
	}

	for rel, body := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return dir
}

// TestRun_SubcommandsPlumbThroughToPredicates is the CLI-plumbing
// integration test: one row per subcommand, exercise it against the
// shared fixture, assert the exit code + a substring of stdout that
// proves the predicate ran (either PASS/FAIL/SKIP prefix with the
// expected check name). Not exhaustive — the ops/checks package owns
// the predicate-behavior tests. This test proves the CLI adapter maps
// flags → predicate inputs correctly for every subcommand.
func TestRun_SubcommandsPlumbThroughToPredicates(t *testing.T) {
	t.Parallel()
	fixture := writeFixture(t)

	tests := []struct {
		name     string
		args     []string
		wantExit int
		wantOut  []string // every substring must appear in combined stdout+stderr
	}{
		{
			name:     "env-refs",
			args:     []string{"env-refs", "--hostname=api", "--path=" + fixture},
			wantExit: 0,
			wantOut:  []string{"api_env_refs", "PASS"},
		},
		{
			name:     "run-start-build-contract",
			args:     []string{"run-start-build-contract", "--hostname=api", "--path=" + fixture},
			wantExit: 0,
			wantOut:  []string{"SKIP"},
		},
		{
			name:     "env-self-shadow",
			args:     []string{"env-self-shadow", "--hostname=api", "--path=" + fixture},
			wantExit: 0,
			wantOut:  []string{"api_env_self_shadow", "PASS"},
		},
		{
			name:     "ig-code-adjustment",
			args:     []string{"ig-code-adjustment", "--hostname=api", "--path=" + fixture, "--showcase"},
			wantExit: 0,
			wantOut:  []string{"integration_guide_code_adjustment", "PASS"},
		},
		{
			name:     "ig-per-item-code",
			args:     []string{"ig-per-item-code", "--hostname=api", "--path=" + fixture},
			wantExit: 0,
			wantOut:  []string{"SKIP"},
		},
		{
			name:     "comment-specificity",
			args:     []string{"comment-specificity", "--hostname=api", "--path=" + fixture, "--showcase"},
			wantExit: 0,
			wantOut:  []string{"comment_specificity", "PASS"},
		},
		{
			name:     "yml-schema-no-cache",
			args:     []string{"yml-schema", "--hostname=api", "--path=" + fixture},
			wantExit: 0,
			wantOut:  []string{"SKIP", "schema"},
		},
		{
			name:     "kb-authenticity",
			args:     []string{"kb-authenticity", "--hostname=api", "--path=" + fixture},
			wantExit: 0,
			wantOut:  []string{"knowledge_base_authenticity", "PASS"},
		},
		{
			name:     "worker-queue-group-gotcha",
			args:     []string{"worker-queue-group-gotcha", "--hostname=worker", "--path=" + fixture, "--is-worker"},
			wantExit: 0,
			wantOut:  []string{"worker_queue_group_gotcha", "PASS"},
		},
		{
			name:     "worker-shutdown-gotcha",
			args:     []string{"worker-shutdown-gotcha", "--hostname=worker", "--path=" + fixture, "--is-worker"},
			wantExit: 0,
			wantOut:  []string{"worker_shutdown_gotcha", "PASS"},
		},
		{
			name:     "manifest-honesty",
			args:     []string{"manifest-honesty", "--mount-root=" + fixture},
			wantExit: 0,
			wantOut:  []string{"writer_manifest_honesty", "PASS"},
		},
		{
			name:     "manifest-completeness-no-facts",
			args:     []string{"manifest-completeness", "--mount-root=" + fixture},
			wantExit: 0,
			wantOut:  []string{"writer_manifest_completeness", "PASS"},
		},
		{
			name:     "comment-depth",
			args:     []string{"comment-depth", "--env=0", "--path=" + fixture},
			wantExit: 0,
			wantOut:  []string{"comment_depth"},
		},
		{
			name:     "factual-claims",
			args:     []string{"factual-claims", "--env=0", "--path=" + fixture},
			wantExit: 0,
			wantOut:  []string{"factual_claims", "PASS"},
		},
		{
			name:     "cross-readme-dedup",
			args:     []string{"cross-readme-dedup", "--path=" + fixture},
			wantExit: 0,
			wantOut:  []string{"cross_readme_gotcha_uniqueness", "PASS"},
		},
		{
			name:     "symbol-contract-env-consistency-degraded",
			args:     []string{"symbol-contract-env-consistency", "--mount-root=" + fixture},
			wantExit: 0,
			wantOut:  []string{"symbol_contract_env_var_consistency", "PASS"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			exit := run(context.Background(), tt.args, &stdout, &stderr)
			combined := stdout.String() + stderr.String()
			if exit != tt.wantExit {
				t.Fatalf("exit=%d want=%d\nstdout:\n%s\nstderr:\n%s", exit, tt.wantExit, stdout.String(), stderr.String())
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(combined, want) {
					t.Fatalf("output missing %q\nstdout:\n%s\nstderr:\n%s", want, stdout.String(), stderr.String())
				}
			}
		})
	}
}

// TestRun_UnknownSubcommand_UsagePrintedExit1 verifies the dispatcher's
// sad-path behavior: unknown name → exit 1 + usage on stderr.
func TestRun_UnknownSubcommand_UsagePrintedExit1(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	exit := run(context.Background(), []string{"this-is-not-a-check"}, &stdout, &stderr)
	if exit != 1 {
		t.Fatalf("exit=%d want=1", exit)
	}
	if !strings.Contains(stderr.String(), "unknown check") {
		t.Fatalf("stderr missing 'unknown check': %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Available checks:") {
		t.Fatalf("stderr missing usage: %s", stderr.String())
	}
}

// TestRun_NoArgs_UsagePrintedExit1 verifies the dispatcher's empty-args
// path: no subcommand → exit 1 + usage on stderr.
func TestRun_NoArgs_UsagePrintedExit1(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	exit := run(context.Background(), nil, &stdout, &stderr)
	if exit != 1 {
		t.Fatalf("exit=%d want=1", exit)
	}
	if !strings.Contains(stderr.String(), "Available checks:") {
		t.Fatalf("stderr missing usage: %s", stderr.String())
	}
}

// TestRun_AllSixteenRegistered guards against a shim file being added
// without the corresponding registerCheck call. The §18 list is
// canonical; regressions here mean the refactor split the registry.
func TestRun_AllSixteenRegistered(t *testing.T) {
	t.Parallel()
	want := []string{
		"env-refs",
		"run-start-build-contract",
		"env-self-shadow",
		"ig-code-adjustment",
		"ig-per-item-code",
		"comment-specificity",
		"yml-schema",
		"kb-authenticity",
		"worker-queue-group-gotcha",
		"worker-shutdown-gotcha",
		"manifest-honesty",
		"manifest-completeness",
		"comment-depth",
		"factual-claims",
		"cross-readme-dedup",
		"symbol-contract-env-consistency",
	}
	for _, n := range want {
		if _, ok := registry[n]; !ok {
			t.Errorf("subcommand %q not registered", n)
		}
	}
	if len(registry) != len(want) {
		t.Errorf("registry size=%d want=%d", len(registry), len(want))
	}
}
