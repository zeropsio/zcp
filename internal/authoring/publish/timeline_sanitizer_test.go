package publish

import (
	"strings"
	"testing"
)

// Run-40 ENG-2 — SanitizeTimeline strips author-data leaks and
// optionally substitutes a plan-derived service count before
// TIMELINE.md enters the export tarball.

// TestSanitizeTimeline_ProjectID redacts the parenthetical
// `(id `XYZ`)` idiom emitted by the TIMELINE prompt. Pinned against
// the literal run-39 leak: project name + id "7HfLxoquTxiNEg1fD4Xo7w".
func TestSanitizeTimeline_ProjectID(t *testing.T) {
	t.Parallel()
	in := []byte("Project name: `zcprecipator-nestjs-showcase` (id `7HfLxoquTxiNEg1fD4Xo7w`).\n")
	out := string(SanitizeTimeline(in, SanitizeTimelineOpts{}))
	if strings.Contains(out, "7HfLxoquTxiNEg1fD4Xo7w") {
		t.Errorf("project id leaked through sanitizer: %q", out)
	}
	if !strings.Contains(out, "(id `<project-id>`)") {
		t.Errorf("project id placeholder missing; got %q", out)
	}
}

// TestSanitizeTimeline_HostnameHash redacts the Zerops-generated
// subdomain pattern. The project-hash digits identify the author's
// project; the rest of the URL shape is preserved so porters still
// see the format they'll encounter on their own deploys.
func TestSanitizeTimeline_HostnameHash(t *testing.T) {
	t.Parallel()
	in := []byte("Access the dashboard at https://apidev-2304-3000.prg1.zerops.app/api/status.\n")
	out := string(SanitizeTimeline(in, SanitizeTimelineOpts{}))
	if strings.Contains(out, "2304-3000") {
		t.Errorf("hostname hash leaked: %q", out)
	}
	if !strings.Contains(out, "apidev-<id>-3000.prg1.zerops.app") {
		t.Errorf("expected `apidev-<id>-3000.prg1.zerops.app`; got %q", out)
	}
}

// TestSanitizeTimeline_HostnameHash_MultipleHostsAndPorts covers the
// per-codebase fan-out: appdev, apistage, workerstage each render
// with their own hash + port.
func TestSanitizeTimeline_HostnameHash_MultipleHostsAndPorts(t *testing.T) {
	t.Parallel()
	in := []byte(`URLs:
  - apidev-2304-3000.prg1.zerops.app
  - appdev-2304-5173.prg1.zerops.app
  - workerstage-2304-9229.prg1.zerops.app
`)
	out := string(SanitizeTimeline(in, SanitizeTimelineOpts{}))
	for _, leaked := range []string{"-2304-3000", "-2304-5173", "-2304-9229"} {
		if strings.Contains(out, leaked) {
			t.Errorf("hostname hash %q leaked through sanitizer; got %q", leaked, out)
		}
	}
	if !strings.Contains(out, "apidev-<id>-3000") || !strings.Contains(out, "appdev-<id>-5173") || !strings.Contains(out, "workerstage-<id>-9229") {
		t.Errorf("expected all three host placeholders; got %q", out)
	}
}

// TestSanitizeTimeline_ZcprecipatorPath redacts the author's
// machine-side output-root path.
func TestSanitizeTimeline_ZcprecipatorPath(t *testing.T) {
	t.Parallel()
	in := []byte("Output root: /var/www/zcprecipator/nestjs-showcase/environments/.\n")
	out := string(SanitizeTimeline(in, SanitizeTimelineOpts{}))
	if strings.Contains(out, "/var/www/zcprecipator/nestjs-showcase/") {
		t.Errorf("author output-root leaked: %q", out)
	}
	if !strings.Contains(out, "<output-root>/environments/") {
		t.Errorf("expected <output-root>/ placeholder; got %q", out)
	}
}

// TestSanitizeTimeline_UsersPath redacts macOS dev paths.
func TestSanitizeTimeline_UsersPath(t *testing.T) {
	t.Parallel()
	in := []byte("Author session captured under /Users/fxck/www/zcp/.\n")
	out := string(SanitizeTimeline(in, SanitizeTimelineOpts{}))
	if strings.Contains(out, "/Users/fxck/") {
		t.Errorf("author home path leaked: %q", out)
	}
	if !strings.Contains(out, "<machine-path>/www/zcp/") {
		t.Errorf("expected <machine-path>/ placeholder; got %q", out)
	}
}

// TestSanitizeTimeline_ServiceCountSubstitution rewrites the
// agent-authored claim to the plan-derived count. Pinned against the
// run-39 miscount (TIMELINE said 14; real was 11).
func TestSanitizeTimeline_ServiceCountSubstitution(t *testing.T) {
	t.Parallel()
	in := []byte("`zerops_import` provisioned 14 services in a single batch.\n")
	out := string(SanitizeTimeline(in, SanitizeTimelineOpts{ServiceCount: 11}))
	if !strings.Contains(out, "provisioned 11 services") {
		t.Errorf("expected count substitution to 11; got %q", out)
	}
	if strings.Contains(out, "provisioned 14 services") {
		t.Errorf("pre-substitution count still present: %q", out)
	}
}

// TestSanitizeTimeline_ServiceCount_ZeroSkipsSubstitution — when no
// plan is available (ServiceCount=0) the sanitizer leaves the count
// alone; only the always-on redactions fire.
func TestSanitizeTimeline_ServiceCount_ZeroSkipsSubstitution(t *testing.T) {
	t.Parallel()
	in := []byte("provisioned 14 services\n")
	out := string(SanitizeTimeline(in, SanitizeTimelineOpts{}))
	if !strings.Contains(out, "14 services") {
		t.Errorf("count should be preserved when ServiceCount=0; got %q", out)
	}
}

// TestSanitizeTimeline_Idempotent — running the sanitizer twice
// produces the same output as running it once. Placeholders are
// chosen to not re-trigger their own patterns.
func TestSanitizeTimeline_Idempotent(t *testing.T) {
	t.Parallel()
	in := []byte(`Project name: ` + "`zcprecipator-nestjs-showcase`" + ` (id ` + "`7HfLxoquTxiNEg1fD4Xo7w`" + `).
Dashboard: apidev-2304-3000.prg1.zerops.app
Output root: /var/www/zcprecipator/nestjs-showcase/
Author cwd: /Users/fxck/www/zcp/
provisioned 14 services
`)
	once := SanitizeTimeline(in, SanitizeTimelineOpts{ServiceCount: 11})
	twice := SanitizeTimeline(once, SanitizeTimelineOpts{ServiceCount: 11})
	if string(once) != string(twice) {
		t.Errorf("sanitizer not idempotent.\nOnce:\n%s\nTwice:\n%s", once, twice)
	}
}

// TestSanitizeTimeline_FullRun39Fixture covers a synthetic excerpt of
// the run-39 TIMELINE.md leaks end-to-end. Every redaction must land
// in a single pass.
func TestSanitizeTimeline_FullRun39Fixture(t *testing.T) {
	t.Parallel()
	in := []byte(`# Run-39 TIMELINE

Recipe engine: zcprecipator3.
Output root: /var/www/zcprecipator/nestjs-showcase/.

## 1. Research
Parent recipe absent; outputRoot=/var/www/zcprecipator/nestjs-showcase/.

## 2. Provision
` + "`zerops_import` provisioned 14 services in a single batch." + `

## 6. Close
Project name: ` + "`zcprecipator-nestjs-showcase`" + ` (id ` + "`7HfLxoquTxiNEg1fD4Xo7w`" + `).
Dashboard: https://apidev-2304-3000.prg1.zerops.app/api/status
`)
	out := string(SanitizeTimeline(in, SanitizeTimelineOpts{ServiceCount: 11}))

	forbidden := []string{
		"7HfLxoquTxiNEg1fD4Xo7w",
		"apidev-2304-3000",
		"/var/www/zcprecipator/nestjs-showcase/",
		"provisioned 14 services",
	}
	for _, leak := range forbidden {
		if strings.Contains(out, leak) {
			t.Errorf("forbidden leak %q still in output:\n%s", leak, out)
		}
	}
	required := []string{
		"(id `<project-id>`)",
		"apidev-<id>-3000.prg1.zerops.app",
		"<output-root>/",
		"provisioned 11 services",
	}
	for _, want := range required {
		if !strings.Contains(out, want) {
			t.Errorf("expected placeholder %q missing from output:\n%s", want, out)
		}
	}
}

// TestSanitizeTimeline_NonZeropsHostUnchanged — the hostname-hash
// regex anchors on `.zerops.app`; external hostnames must pass
// through. Catches over-eager regex regressions.
func TestSanitizeTimeline_NonZeropsHostUnchanged(t *testing.T) {
	t.Parallel()
	in := []byte("Refer to https://nestjs.com/docs and https://example.com/api-2304-3000 for details.\n")
	out := string(SanitizeTimeline(in, SanitizeTimelineOpts{}))
	if !strings.Contains(out, "example.com/api-2304-3000") {
		t.Errorf("non-zerops host should pass through unchanged; got %q", out)
	}
	if !strings.Contains(out, "nestjs.com/docs") {
		t.Errorf("framework doc URL should pass through unchanged; got %q", out)
	}
}

// Run-41 — ENG-2 widening tests. The original (run-40) sanitizer
// regex coverage was narrower than plan §ENG-2 specified: the
// project-ID redactor required the literal `id ` keyword inside
// the parentheses, the hostname-hash redactor required a `-<port>`
// segment, session UUIDs and engine-vocab were out of scope
// entirely. Run-40 dogfood validation
// ([plans/run-40-validation.md]) surfaced four leak shapes the
// original sanitizer let through:
//
//  1. `Zerops project `<slug>` (`<id>`)` — bare-paren id, no keyword.
//  2. `Session: `<uuid>`` — UUID anywhere.
//  3. `appstage-<digits>.prg1.zerops.app` — no-port stage subdomain.
//  4. `zerops_*`, `complete-phase`, `record-fragment`, … — engine
//     vocab the agent free-cites in TIMELINE narrative.
//
// These tests pin closure of each.

// TestSanitizeTimeline_ProjectIDBareParenForm — actual TIMELINE
// prompt emit shape `Zerops project `<slug>` (`<id>`)`. No "id "
// keyword inside the parentheses; original regex didn't match.
func TestSanitizeTimeline_ProjectIDBareParenForm(t *testing.T) {
	t.Parallel()
	in := []byte("Session: `ca8266e6-3e42-4620-854a-cd02c6ac2b40` on Zerops project `zcprecipator-nestjs-showcase` (`f1NS28GZRByGbQz3WaihAw`).\n")
	out := string(SanitizeTimeline(in, SanitizeTimelineOpts{}))
	if strings.Contains(out, "f1NS28GZRByGbQz3WaihAw") {
		t.Errorf("bare-paren project id leaked: %q", out)
	}
	if !strings.Contains(out, "(`<project-id>`)") {
		t.Errorf("expected bare-paren `<project-id>` placeholder; got %q", out)
	}
}

// TestSanitizeTimeline_SessionUUID — UUIDs in `Session: `<uuid>“
// lines from the TIMELINE prompt's run-header.
func TestSanitizeTimeline_SessionUUID(t *testing.T) {
	t.Parallel()
	in := []byte("Session: `ca8266e6-3e42-4620-854a-cd02c6ac2b40` on Zerops project `slug` (`f1NS28GZRByGbQz3WaihAw`).\n")
	out := string(SanitizeTimeline(in, SanitizeTimelineOpts{}))
	if strings.Contains(out, "ca8266e6-3e42-4620-854a-cd02c6ac2b40") {
		t.Errorf("session UUID leaked: %q", out)
	}
	if !strings.Contains(out, "Session: `<session-id>`") {
		t.Errorf("expected Session: `<session-id>` placeholder; got %q", out)
	}
}

// TestSanitizeTimeline_HostnameHashNoPort — base: static stage
// subdomains have no port suffix (`<host>stage-<digits>.<zone>.zerops.app`).
// Run-40 leaked these because the with-port redactor required
// `-<port>`.
func TestSanitizeTimeline_HostnameHashNoPort(t *testing.T) {
	t.Parallel()
	in := []byte("Browser-walked on appstage (https://appstage-2311.prg1.zerops.app/).\n")
	out := string(SanitizeTimeline(in, SanitizeTimelineOpts{}))
	if strings.Contains(out, "appstage-2311.prg1.zerops.app") {
		t.Errorf("no-port stage hostname leaked: %q", out)
	}
	if !strings.Contains(out, "appstage-<id>.prg1.zerops.app") {
		t.Errorf("expected `appstage-<id>.prg1.zerops.app`; got %q", out)
	}
}

// TestSanitizeTimeline_EngineMCPToolNames — strips Zerops MCP tool
// names (zerops_*) the agent free-cites in TIMELINE narrative.
// Whitelist: `zerops.app`, `zerops.io`, `zerops.yaml`, `zsc` must
// pass through unchanged.
func TestSanitizeTimeline_EngineMCPToolNames(t *testing.T) {
	t.Parallel()
	in := []byte(`Env keys cataloged via zerops_discover includeEnvs=true.
Stage subdomain access enabled via zerops_subdomain action=enable.
Cross-deploys verified via zerops_verify healthy on every slot.
zerops_import ran 14 processes.
See https://docs.zerops.io for more.
The file zerops.yaml carries the deploy config.
Run zsc execOnce in initCommands.
`)
	out := string(SanitizeTimeline(in, SanitizeTimelineOpts{}))
	for _, leaked := range []string{
		"zerops_discover", "zerops_subdomain", "zerops_verify", "zerops_import",
	} {
		if strings.Contains(out, leaked) {
			t.Errorf("engine MCP tool name leaked: %q in %q", leaked, out)
		}
	}
	// Porter-facing tokens must pass through.
	for _, want := range []string{
		"docs.zerops.io",
		"zerops.yaml",
		"zsc execOnce",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("porter-facing token %q stripped accidentally; got %q", want, out)
		}
	}
}

// TestSanitizeTimeline_EnginePhaseCommands — strips engine recipe-
// state-machine commands the agent free-cites in TIMELINE narrative.
func TestSanitizeTimeline_EnginePhaseCommands(t *testing.T) {
	t.Parallel()
	in := []byte(`complete-phase phase=finalize refused with "phase=finalize requires refinement sub-agent dispatch first".
Called enter-phase phase=provision then re-completed.
record-fragment refused first call without classification field.
After stitch-content rendered every surface.
build-subagent-prompt briefKind=refinement returned the brief.
`)
	out := string(SanitizeTimeline(in, SanitizeTimelineOpts{}))
	for _, leaked := range []string{
		"complete-phase", "enter-phase", "record-fragment", "stitch-content", "build-subagent-prompt",
	} {
		if strings.Contains(out, leaked) {
			t.Errorf("engine phase command leaked: %q in %q", leaked, out)
		}
	}
}

// TestSanitizeTimeline_Run40FixtureFullCoverage — end-to-end on the
// actual run-40 TIMELINE shape (composite of bare-paren project ID,
// session UUID, no-port stage hostname, engine MCP tools, engine
// phase commands). Pinned against the leak list from
// [plans/run-40-validation.md] §"ENG-2 — PARTIAL".
func TestSanitizeTimeline_Run40FixtureFullCoverage(t *testing.T) {
	t.Parallel()
	in := []byte("# nestjs-showcase recipe build timeline\n\n" +
		"Session: `ca8266e6-3e42-4620-854a-cd02c6ac2b40` on Zerops project `zcprecipator-nestjs-showcase` (`f1NS28GZRByGbQz3WaihAw`).\n\n" +
		"## 2. Provision\n\n" +
		"- `zerops_import` ran 14 processes.\n" +
		"- Env keys cataloged via `zerops_discover includeEnvs=true`.\n\n" +
		"## 3. Scaffold\n\n" +
		"Cross-deploys dev→stage all green (`zerops_verify` healthy on every slot).\n\n" +
		"Stage subdomain URLs (workspace, not deliverable):\n" +
		"- apistage: `https://apistage-<id>-3000.prg1.zerops.app/`\n" +
		"- appstage: `https://appstage-2311.prg1.zerops.app/`\n\n" +
		"## 7. Finalize\n\n" +
		"- `stitch-content` rendered every surface into `<output-root>/`.\n" +
		"- `complete-phase phase=finalize` refused with `phase=finalize requires refinement sub-agent dispatch first`.\n")
	out := string(SanitizeTimeline(in, SanitizeTimelineOpts{ServiceCount: 11}))
	for _, leak := range []string{
		"f1NS28GZRByGbQz3WaihAw",
		"ca8266e6-3e42-4620-854a-cd02c6ac2b40",
		"appstage-2311.prg1.zerops.app",
		"zerops_import",
		"zerops_discover",
		"zerops_verify",
		"complete-phase",
		"stitch-content",
	} {
		if strings.Contains(out, leak) {
			t.Errorf("run-40 leak %q still in output:\n%s", leak, out)
		}
	}
}
