package tools

import (
	"strings"
	"testing"
)

// TestCausalAnchor_Generic_DotEnv_Fail is the v20 ".env" gotcha case:
// mentions `envVariables` and `zerops.yaml` (so it scores under the
// presence-based classifier) but its claimed failure mode ("cause
// hard-to-debug mismatches between containers") names no concrete
// symptom — no HTTP code, no quoted error, no specific Zerops verb.
// Generic Node advice mis-anchored. Must fail.
func TestCausalAnchor_Generic_DotEnv_Fail(t *testing.T) {
	t.Parallel()
	kb := `### Gotchas
- **Do not commit .env files -- use zerops.yaml envVariables** — All environment variables are declared in the envVariables block of zerops.yaml and injected at runtime. A .env file in the repo will override Zerops-managed values and cause hard-to-debug mismatches between containers. Add .env to .gitignore unconditionally.

  ` + "```\n.env\n```" + `
`
	checks := checkCausalAnchor(kb, "apidev")
	if len(checks) != 1 || checks[0].Status != statusFail {
		t.Fatalf("expected single fail; got %+v", checks)
	}
	if !strings.Contains(checks[0].Detail, "Do not commit .env files") {
		t.Fatalf("detail must name the failing stem: %s", checks[0].Detail)
	}
}

// TestCausalAnchor_NamedError_Pass — gotcha quotes a specific platform
// error (`serviceStackIsNotHttp`) and names a specific mechanism
// (`httpSupport`). Both halves of the load-bearing rule satisfied.
func TestCausalAnchor_NamedError_Pass(t *testing.T) {
	t.Parallel()
	kb := "### Gotchas\n" +
		"- **Dev container returns `serviceStackIsNotHttp`** — Without `ports: [{port: 5173, httpSupport: true}]` in the dev setup, Zerops rejects subdomain activation because it cannot find an HTTP-capable port declaration.\n\n" +
		"  ```yaml\n  ports:\n    - port: 5173\n      httpSupport: true\n  ```\n"
	checks := checkCausalAnchor(kb, "appdev")
	if checks[0].Status != statusPass {
		t.Fatalf("expected pass; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestCausalAnchor_HTTPStatus_Pass — concrete failure mode named via
// HTTP status code, plus L7 balancer mechanism named.
func TestCausalAnchor_HTTPStatus_Pass(t *testing.T) {
	t.Parallel()
	kb := `### Gotchas
- **200 OK with text/html on /api/* in production** — Nginx returns ` + "`index.html`" + ` for unknown paths via ` + "`try_files`" + `. The L7 balancer forwards the request and the static base serves the SPA fallback, so API calls silently receive HTML.
`
	checks := checkCausalAnchor(kb, "appdev")
	if checks[0].Status != statusPass {
		t.Fatalf("expected pass; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestCausalAnchor_DeadlockSymptom_Pass — "deadlocks and schema
// corruption" is a concrete failure-mode symptom, plus `initCommands`
// is a specific Zerops mechanism. Real gotcha.
func TestCausalAnchor_DeadlockSymptom_Pass(t *testing.T) {
	t.Parallel()
	kb := `### Gotchas
- **synchronize: true must be off in production TypeORM config** — TypeORM auto-alters tables to match entities on every startup. With multiple containers starting concurrently under ` + "`initCommands`" + `, this causes deadlocks and schema corruption.
`
	checks := checkCausalAnchor(kb, "apidev")
	if checks[0].Status != statusPass {
		t.Fatalf("expected pass; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestCausalAnchor_QuotedException_Pass — named exception in
// backticks counts as concrete failure mode regardless of mechanism.
// Plus `${queue_password}` is a specific Zerops env-var pattern.
func TestCausalAnchor_QuotedException_Pass(t *testing.T) {
	t.Parallel()
	kb := `### Gotchas
- **NATS credentials must be passed as separate options** — URL-embedded credentials are silently ignored by the nats.js client; ` + "`${queue_user}`" + `/` + "`${queue_password}`" + ` belong on the connection options object. Misconfiguration surfaces as ` + "`AUTHORIZATION_VIOLATION`" + ` at boot.
`
	checks := checkCausalAnchor(kb, "workerdev")
	if checks[0].Status != statusPass {
		t.Fatalf("expected pass; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestCausalAnchor_OnlyMechanism_NoSymptom_Fail — names mechanism
// (`zerops.yaml`, `envVariables`) but no concrete failure mode quoted
// or symptom-verbed. Decorative.
func TestCausalAnchor_OnlyMechanism_NoSymptom_Fail(t *testing.T) {
	t.Parallel()
	kb := `### Gotchas
- **Use envVariables in zerops.yaml** — All env vars belong in the envVariables block of zerops.yaml. They are injected into containers at runtime by the platform.
`
	checks := checkCausalAnchor(kb, "apidev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail — no concrete failure mode; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestCausalAnchor_OnlySymptom_NoMechanism_Fail — names a symptom
// ("crashes" / "404") but no specific Zerops mechanism. Generic.
func TestCausalAnchor_OnlySymptom_NoMechanism_Fail(t *testing.T) {
	t.Parallel()
	kb := `### Gotchas
- **App crashes with 404 on first request** — When the app is misconfigured, it returns 404 errors and the user sees a blank screen.
`
	checks := checkCausalAnchor(kb, "appdev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail — no specific Zerops mechanism; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestCausalAnchor_MixedBag — when 6 of 7 pass and 1 fails, the check
// must surface the failing stem precisely.
func TestCausalAnchor_MixedBag(t *testing.T) {
	t.Parallel()
	kb := `### Gotchas
- **trust proxy must be enabled behind the L7 balancer** — Without ` + "`trust proxy`" + `, Express reports all requests as HTTP from an internal IP. Rate limiters, logging, and redirect logic break.
- **ioredis lazyConnect mandatory for Valkey without auth** — If ioredis sends an empty AUTH, Valkey rejects with a connection error.
- **Generic .env warning** — Just don't commit env files, it's bad practice.
`
	checks := checkCausalAnchor(kb, "apidev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail (1 of 3 generic); got %s — %s", checks[0].Status, checks[0].Detail)
	}
	if !strings.Contains(checks[0].Detail, "Generic .env warning") {
		t.Fatalf("detail must name the failing stem: %s", checks[0].Detail)
	}
	if strings.Contains(checks[0].Detail, "trust proxy") || strings.Contains(checks[0].Detail, "ioredis") {
		t.Fatalf("detail must NOT name the passing stems: %s", checks[0].Detail)
	}
}

// TestCausalAnchor_NoGotchas_NoOp — empty knowledge-base content.
func TestCausalAnchor_NoGotchas_NoOp(t *testing.T) {
	t.Parallel()
	checks := checkCausalAnchor("", "apidev")
	if len(checks) != 0 {
		t.Fatalf("empty content should no-op; got %d", len(checks))
	}
}
