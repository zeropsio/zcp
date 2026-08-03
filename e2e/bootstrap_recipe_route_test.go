//go:build e2e

// Tests for: e2e — recipe route (nodejs-hello-world) against a real Zerops
// project, from authored slug through a live stage URL.
//
// Spec: docs/spec-workflows.md §8 RCO-1..RCO-7 (recipe-route derive/rewrite,
// RCO-6 services-only provision YAML, RCO-7 structured runtime-URL
// collection); docs/spec-testing-architecture.md (e2e tier: -tags e2e, real
// Zerops, cleanup discipline).
//
// HARD PREREQUISITE — dedicated disposable project. This test NEVER runs
// against a shared project. Every recipe in the corpus uses the immutable
// managed hostname "db" (RCO-3: managed hostnames never renamed), so a fresh
// managed dependency requires a project with no pre-existing "db" — and more
// generally no pre-existing services at all, which is the ownership proof a
// shared project cannot offer. Point ZCP_E2E_RECIPE_PROJECT_ID at a project
// owned by the e2e identity that holds NO non-system services; the test
// preflights that emptiness and refuses to proceed otherwise. Unset →
// t.Skip (blocked), never a silent no-op, never a fallback to some other
// project.
//
// RED protocol (e2e RED per the slice brief): with ZCP_E2E_RECIPE_PROJECT_ID
// and Zerops credentials both live, temporarily change the recipeSlugUnderTest
// constant below to an unknown slug (e.g. "definitely-not-a-recipe"), run the
// command below, and confirm the FIRST assertion (kind=="session-active")
// fails loudly — the bootstrap-start call itself errors on an unresolvable
// slug, so mustCallSuccess fatals with the platform's "recipe route: unknown
// slug" message. That failure is the proof the assertion is load-bearing, not
// vacuous. Revert the constant to "nodejs-hello-world" and re-run for GREEN.
// This run recorded ONLY the SKIP gate (see the slice report) — the sandbox
// this slice built in has no ZCP_E2E_RECIPE_PROJECT_ID (the test's ONLY skip
// condition, checked before anything else runs), so the RED/GREEN pair above
// could not be executed live; it is deferred to the owner, who has a
// disposable project to point it at.
//
// Run:
//
//	ZCP_API_KEY=<token> ZCP_E2E_RECIPE_PROJECT_ID=<disposable project id> \
//	  go test ./e2e/ -tags e2e -run TestBootstrapRecipeRoute_AuthoredSlug_LiveURL -count=1 -v -timeout 20m
package e2e_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
	"github.com/zeropsio/zcp/internal/workflow"
)

// recipeSlugUnderTest is the authored slug this test drives end-to-end.
// RED-protocol note: temporarily set this to "definitely-not-a-recipe" to
// prove the first assertion is load-bearing (see file header), then revert.
const recipeSlugUnderTest = "nodejs-hello-world"

// recipeRuntimeType is the recipe's runtime type — a known-good literal read
// directly from internal/knowledge/recipes/nodejs-hello-world.import.yml
// (appdev / appstage, both `type: nodejs@22`), not recomputed from any ZCP
// derivation code.
const recipeRuntimeType = "nodejs@22"

// recipeManagedHostname is RCO-3's immutable managed hostname — every recipe
// in the corpus uses "db" and the rewrite never renames it.
const recipeManagedHostname = "db"

// recipeRouteBudget bounds the ACTIVE-status poll and the stage HTTP-200
// poll. Live-verified 2026-08-03: import→stage-200 in 2m29s; this leaves
// ample headroom.
const recipeRouteBudget = 5 * time.Minute

func TestBootstrapRecipeRoute_AuthoredSlug_LiveURL(t *testing.T) {
	projectID := os.Getenv("ZCP_E2E_RECIPE_PROJECT_ID")
	if projectID == "" {
		t.Skip("blocked: ZCP_E2E_RECIPE_PROJECT_ID not set — point this at a disposable Zerops project (owned by the e2e identity, containing NO existing services) to run this test live; see the file header for the hard prerequisite and the RED protocol")
	}

	h := newRecipeRouteHarness(t, projectID)
	s := newSession(t, h.srv)

	suffix := randomSuffix()[:4]
	devHostname := "bs" + suffix + "dev"
	stageHostname := "bs" + suffix + "stage"

	createdServiceIDs := map[string]string{} // hostname -> serviceID, captured after import

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		teardownByCapturedIDs(cleanupCtx, t, h.client, createdServiceIDs)
		assertProjectEmptyOfUserServices(cleanupCtx, t, h.client, projectID, "teardown verification")
	})

	// Preflight ownership check (before touching anything): emptiness is the
	// ownership proof a shared project cannot offer.
	preflightCtx, preflightCancel := context.WithTimeout(context.Background(), 30*time.Second)
	assertProjectEmptyOfUserServices(preflightCtx, t, h.client, projectID, "preflight")
	preflightCancel()

	step := 0

	// --- Step 1: start bootstrap route=recipe, recipe preloaded ---
	step++
	logStep(t, step, "start bootstrap route=recipe recipeSlug=%s", recipeSlugUnderTest)
	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	startText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":     "start",
		"workflow":   "bootstrap",
		"route":      "recipe",
		"recipeSlug": recipeSlugUnderTest,
		"intent":     "e2e recipe route — " + recipeSlugUnderTest,
	})
	var startResp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(startText), &startResp); err != nil {
		t.Fatalf("parse bootstrap start: %v", err)
	}
	if string(startResp.Kind) != "session-active" {
		t.Fatalf("kind = %q, want %q", startResp.Kind, "session-active")
	}
	if startResp.SessionID == "" {
		t.Fatal("expected non-empty sessionId")
	}
	if startResp.Current == nil || startResp.Current.Name != "discover" {
		t.Fatalf("expected current step 'discover', got %+v", startResp.Current)
	}
	if !strings.Contains(startResp.Current.DetailedGuide, recipeSlugUnderTest) {
		t.Fatalf("recipe not preloaded — discover guide does not mention %q:\n%s", recipeSlugUnderTest, startResp.Current.DetailedGuide)
	}
	t.Logf("  session=%s current=%s", startResp.SessionID, startResp.Current.Name)

	// --- Step 2: complete derive/confirm — rename to unique test hostnames ---
	step++
	logStep(t, step, "complete discover — rename appdev/appstage to %s/%s", devHostname, stageHostname)
	discoverText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"plan": []any{
			map[string]any{
				"runtime": map[string]any{
					"devHostname":   devHostname,
					"stageHostname": stageHostname,
					"type":          recipeRuntimeType,
					"bootstrapMode": "standard",
				},
			},
		},
	})
	var discoverResp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(discoverText), &discoverResp); err != nil {
		t.Fatalf("parse discover complete: %v", err)
	}
	if discoverResp.Current == nil || discoverResp.Current.Name != "provision" {
		t.Fatalf("expected current step 'provision', got %+v", discoverResp.Current)
	}

	// --- Step 3: rendered provision YAML — services-only (S5a/RCO-6), db present ---
	step++
	logStep(t, step, "assert rendered provision YAML is services-only and carries db + renamed hostnames")
	provisionYAML := extractServicesYAML(t, discoverResp.Current.DetailedGuide)
	if strings.Contains(provisionYAML, "project:") {
		t.Errorf("rendered provision YAML carries a project: key (RCO-6/S5a violation):\n%s", provisionYAML)
	}
	if !strings.Contains(provisionYAML, "hostname: "+recipeManagedHostname) {
		t.Errorf("rendered provision YAML missing managed hostname %q verbatim:\n%s", recipeManagedHostname, provisionYAML)
	}
	if !strings.Contains(provisionYAML, "hostname: "+devHostname) {
		t.Errorf("rendered provision YAML missing renamed dev hostname %q:\n%s", devHostname, provisionYAML)
	}
	if !strings.Contains(provisionYAML, "hostname: "+stageHostname) {
		t.Errorf("rendered provision YAML missing renamed stage hostname %q:\n%s", stageHostname, provisionYAML)
	}
	t.Logf("  provision YAML:\n%s", provisionYAML)

	// --- Step 4: import — all processes FINISHED; poll by-id to ACTIVE ---
	step++
	logStep(t, step, "zerops_import the rendered services-only YAML")
	importText := s.mustCallSuccess("zerops_import", map[string]any{"content": provisionYAML})
	var importResult ops.ImportResult
	if err := json.Unmarshal([]byte(importText), &importResult); err != nil {
		t.Fatalf("parse import result: %v", err)
	}
	if len(importResult.Processes) == 0 {
		t.Fatal("import returned no processes")
	}
	for _, p := range importResult.Processes {
		if p.Status != "FINISHED" {
			t.Fatalf("import process for %s (%s) did not finish: status=%s failReason=%v", p.Service, p.ProcessID, p.Status, p.FailReason)
		}
		if p.ServiceID == "" {
			t.Fatalf("import process for %s carries no serviceId", p.Service)
		}
		createdServiceIDs[p.Service] = p.ServiceID
	}
	for _, want := range []string{devHostname, stageHostname, recipeManagedHostname} {
		if _, ok := createdServiceIDs[want]; !ok {
			t.Fatalf("import did not create expected service %q (created: %v)", want, createdServiceIDs)
		}
	}
	t.Logf("  created services: %v", createdServiceIDs)

	waitForServiceStatusByID(t, h.client, createdServiceIDs[devHostname], devHostname, []string{"ACTIVE"}, recipeRouteBudget)
	waitForServiceStatusByID(t, h.client, createdServiceIDs[stageHostname], stageHostname, []string{"ACTIVE"}, recipeRouteBudget)
	waitForServiceStatusByID(t, h.client, createdServiceIDs[recipeManagedHostname], recipeManagedHostname, []string{"RUNNING", "ACTIVE"}, recipeRouteBudget)
	t.Log("  dev, stage, db all reached their live status (by-id direct reads)")

	// Record discovered env vars so the provision checker's own live fetch
	// (checkProvision calls ops.FetchServiceEnv directly) has a warm read;
	// also gives the run a readable trail of what's discoverable.
	s.mustCallSuccess("zerops_discover", map[string]any{"includeEnvs": true})

	// --- Step 5: complete provision — consume the structured runtime-URL collection ---
	step++
	logStep(t, step, "complete provision — consume RuntimeURLs (RCO-7), stage=200 dev=502")
	provisionText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "provision",
		"attestation": "Recipe route e2e: dev/stage/db all live, env vars discovered.",
	})
	var provisionResp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(provisionText), &provisionResp); err != nil {
		t.Fatalf("parse provision complete: %v", err)
	}
	if provisionResp.CheckResult == nil || !provisionResp.CheckResult.Passed {
		var detail strings.Builder
		if provisionResp.CheckResult != nil {
			for _, c := range provisionResp.CheckResult.Checks {
				detail.WriteString("\n  " + c.Name + ": " + c.Status + " " + c.Detail)
			}
		}
		t.Fatalf("provision check did not pass:%s", detail.String())
	}
	if provisionResp.Current == nil || provisionResp.Current.Name != "close" {
		t.Fatalf("expected current step 'close' after provision, got %+v", provisionResp.Current)
	}

	stageURL := findRuntimeURLByRole(provisionResp.RuntimeURLs, "stage")
	if stageURL == nil {
		t.Fatalf("no stage entry in runtimeUrls: %+v", provisionResp.RuntimeURLs)
	}
	if !stageURL.Handoff {
		t.Errorf("stage entry must be marked handoff: %+v", *stageURL)
	}
	if stageURL.Hostname != stageHostname {
		t.Errorf("stage entry hostname = %q, want %q", stageURL.Hostname, stageHostname)
	}
	devURL := findRuntimeURLByRole(provisionResp.RuntimeURLs, "dev")
	if devURL == nil {
		t.Fatalf("no dev entry in runtimeUrls: %+v", provisionResp.RuntimeURLs)
	}
	if devURL.Handoff {
		t.Errorf("dev entry must NOT be marked handoff (idle zsc noop, 502): %+v", *devURL)
	}
	t.Logf("  stage url=%s handoff=%v", stageURL.URL, stageURL.Handoff)
	t.Logf("  dev url=%s handoff=%v", devURL.URL, devURL.Handoff)

	stageCode, stageOK := pollHTTPHealth(stageURL.URL, 5*time.Second, recipeRouteBudget)
	if !stageOK {
		t.Fatalf("stage URL %s did not reach HTTP 200 within %s (last=%d)", stageURL.URL, recipeRouteBudget, stageCode)
	}
	t.Logf("  stage HTTP %d OK", stageCode)

	const devPollBudget = 2 * time.Minute
	devCode, devOK := pollHTTPStatusEquals(devURL.URL, 502, 5*time.Second, devPollBudget)
	if !devOK {
		t.Fatalf("dev URL %s did not settle at HTTP 502 (idle, zsc noop) within %s (last=%d)", devURL.URL, devPollBudget, devCode)
	}
	t.Logf("  dev HTTP %d (idle, as expected)", devCode)

	// --- Step 6: close the session (teardown + removal-verification run in t.Cleanup) ---
	step++
	logStep(t, step, "complete close step")
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "close",
		"attestation": "Recipe route e2e complete: stage served 200, dev idle at 502.",
	})
	t.Log("  bootstrap closed — teardown + direct-read removal verification run in cleanup")
}

// newRecipeRouteHarness builds an e2eHarness pinned to projectID directly —
// deliberately bypassing auth.Resolve's default-project discovery (which
// would pick whatever project the ambient credentials are scoped to). This
// test must NEVER touch any project other than the one the caller names via
// ZCP_E2E_RECIPE_PROJECT_ID; constructing auth.Info by hand with ProjectID
// set is what guarantees that, regardless of what a shared identity's
// default project happens to be.
func newRecipeRouteHarness(t *testing.T, projectID string) *e2eHarness {
	t.Helper()
	creds, err := auth.ResolveCredentials()
	if err != nil {
		t.Skipf("blocked: no Zerops credentials available (%v) — set ZCP_API_KEY or run `zcli login <token>`", err)
	}
	client, err := platform.NewZeropsClient(creds.Token, creds.APIHost)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	authInfo := &auth.Info{
		Token:     creds.Token,
		APIHost:   creds.APIHost,
		Region:    creds.Region,
		ProjectID: projectID,
	}
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	logFetcher := platform.NewLogFetcher()
	sshDeployer := platform.NewSystemSSHDeployer()
	srv := server.New(context.Background(), client, authInfo, store, logFetcher, sshDeployer, nil, runtime.Info{}, nil)
	return &e2eHarness{t: t, client: client, projectID: projectID, authInfo: authInfo, srv: srv}
}

// assertProjectEmptyOfUserServices direct-reads the project's service stack
// and fatals if any non-system service exists. Emptiness is the ownership
// proof a shared project cannot offer (see file header's hard prerequisite).
func assertProjectEmptyOfUserServices(ctx context.Context, t *testing.T, client platform.Client, projectID, when string) {
	t.Helper()
	services, err := client.ListServicesDirect(ctx, projectID)
	if err != nil {
		t.Fatalf("%s: ListServicesDirect: %v", when, err)
	}
	for _, svc := range services {
		if !svc.IsSystem() {
			t.Fatalf("%s: project %s is not empty of user services — found %q (status=%s, id=%s). "+
				"This test requires a disposable project owned by the e2e identity with NO existing "+
				"services; refusing to run against what may be a shared project.",
				when, projectID, svc.Name, svc.Status, svc.ID)
		}
	}
	t.Logf("%s: confirmed project %s has no non-system services (%d service(s) total, all system)", when, projectID, len(services))
}

// teardownByCapturedIDs deletes every captured service by ID via direct
// reads — never by re-listing and matching hostname, and never inferred
// from an HTTP 502 (balancer records outlive origins). Best-effort: a
// delete failure is logged, not fatal, so every remaining ID still gets a
// deletion attempt.
func teardownByCapturedIDs(ctx context.Context, t *testing.T, client platform.Client, ids map[string]string) {
	t.Helper()
	for hostname, id := range ids {
		if id == "" {
			continue
		}
		proc, err := client.DeleteService(ctx, id)
		if err != nil {
			t.Logf("teardown: delete %s (%s): %v", hostname, id, err)
			continue
		}
		if proc != nil {
			waitForProcessDirect(ctx, client, proc.ID)
		}
	}
}

// waitForServiceStatusByID polls a service by ID (direct read, never an ES
// search) until it reaches one of the wanted statuses or the budget expires.
// Owns its own context (sized to budget, not to any single call) so a
// multi-minute poll never trips on a short-lived caller context.
func waitForServiceStatusByID(t *testing.T, client platform.Client, id, hostname string, want []string, budget time.Duration) *platform.ServiceStack {
	t.Helper()
	if id == "" {
		t.Fatalf("waitForServiceStatusByID: empty service id for %s", hostname)
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget+30*time.Second)
	defer cancel()
	deadline := time.Now().Add(budget)
	var last *platform.ServiceStack
	for {
		svc, err := client.GetService(ctx, id)
		if err != nil {
			t.Fatalf("GetService(%s / %s): %v", hostname, id, err)
		}
		last = svc
		for _, w := range want {
			if svc.Status == w {
				return svc
			}
		}
		if svc.Status == "FAILED" {
			t.Fatalf("service %s (%s) reached FAILED while waiting for %v", hostname, id, want)
		}
		if time.Now().After(deadline) {
			t.Fatalf("service %s (%s) did not reach %v within %s (last status=%q)", hostname, id, want, budget, last.Status)
		}
		time.Sleep(pollInterval)
	}
}

// extractServicesYAML pulls the ```yaml fenced block whose content starts
// with "services:" out of a bootstrap step guide — the services-only block
// RCO-6/S5a renders at the provision step, as opposed to the recipe's
// verbatim (project:-carrying) YAML shown at discover. Scanning for the
// "services:"-prefixed block (rather than just the first fenced block)
// keeps this robust against unrelated fenced examples other atoms in the
// same guide composition might render.
func extractServicesYAML(t *testing.T, guide string) string {
	t.Helper()
	const openTag = "```yaml\n"
	searchFrom := 0
	for {
		rel := strings.Index(guide[searchFrom:], openTag)
		if rel == -1 {
			break
		}
		start := searchFrom + rel + len(openTag)
		relEnd := strings.Index(guide[start:], "```")
		if relEnd == -1 {
			break
		}
		block := guide[start : start+relEnd]
		if strings.HasPrefix(strings.TrimSpace(block), "services:") {
			return block
		}
		searchFrom = start + relEnd + 3
	}
	t.Fatalf("no ```yaml fenced block starting with \"services:\" found in guide:\n%s", guide)
	return ""
}

// findRuntimeURLByRole returns the RuntimeURLs entry with the given role, or
// nil. role is compared against a plain string literal (independent of the
// workflow.RuntimeURLRole* constants) so the assertion doesn't recompute the
// expectation the implementation's own way.
func findRuntimeURLByRole(urls []workflow.RuntimeURL, role string) *workflow.RuntimeURL {
	for i := range urls {
		if urls[i].Role == role {
			return &urls[i]
		}
	}
	return nil
}

// pollHTTPStatusEquals polls url until it returns exactly want or the
// deadline passes. Sibling of pollHTTPHealth (subdomain_test.go), which is
// hardcoded to 200 — this test also needs to assert a settled 502 (the idle
// recipe dev container, RCO-7).
func pollHTTPStatusEquals(url string, want int, interval, deadline time.Duration) (int, bool) {
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		code := httpGetStatus(url, 10*time.Second)
		if code == want {
			return code, true
		}
		select {
		case <-timer.C:
			return code, false
		case <-ticker.C:
		}
	}
}
