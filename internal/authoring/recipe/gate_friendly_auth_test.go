package recipe

import (
	"os"
	"testing"
)

// gate_friendly_auth_test.go — Run-46 Item 4 substrate pin.
//
// Substrate F-FRIENDLY-AUTH (run-43) teaches the friendly-authority
// adapt-path pattern (declarative statement + invitation + named
// porter-side trigger). Run-44 author hit ~7 adapt-paths across 3
// yamls; run-45 author hit ~2 across 3 yamls. Author-stochastic
// adherence — refinement-1 substrate didn't enforce.
//
// Item 4 closes the loop with a close-gate at codebase-content phase:
// count porter-tunable directives in each codebase's zerops.yaml +
// require adapt-path coverage proportional to count (floor =
// max(0, ceil(porter_tunable_count / 3))). Yamls with zero
// porter-tunables pass naturally; yamls with 6 porter-tunables need
// ≥2 adapt-paths.
//
// Adapt-path detection — regex match on the friendly-authority phrasing
// the substrate names: "feel free to", "if you", "once you", "swap",
// "bump", "tune", "customize", "rotate", "change ... if".
//
// Porter-tunable detection — static allowlist of fields whose value is
// porter-decision territory (minContainers, maxContainers, verticalAutoscaling.*,
// objectStorageSize, priority, ports[].port — except required-port families
// like NestJS 3000, env-var values matching the custom-domain / secret-
// rotation patterns).

// TestFriendlyAuthGate_ProportionalFloor — yaml with N porter-tunable
// directives, M adapt-paths, M < ceil(N/3) → close fails with
// friendly-auth-floor-below-tunable-count.
func TestFriendlyAuthGate_ProportionalFloor(t *testing.T) {
	t.Parallel()
	yaml := `zerops:
  - setup: api
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      verticalAutoscaling:
        minRam: 0.5
        maxRam: 2
        minCpu: 1
        maxCpu: 4
      minContainers: 2
      maxContainers: 5
`
	tunables := countPorterTunableDirectives(yaml)
	if tunables < 6 {
		t.Fatalf("test fixture should expose ≥6 porter-tunables; counter returned %d", tunables)
	}
	adapts := countAdaptPaths(yaml)
	if adapts > 1 {
		t.Fatalf("test fixture should expose ≤1 adapt-path; counter returned %d", adapts)
	}
	required := requiredAdaptPaths(tunables)
	if required < 2 {
		t.Fatalf("floor formula must require ≥2 adapt-paths on 6-tunable yaml; got %d", required)
	}
	violations := evalFriendlyAuthGate("codebase/api/zerops-yaml", yaml)
	if len(violations) == 0 {
		t.Fatal("expected friendly-auth-floor-below-tunable-count violation; got none")
	}
	found := false
	for _, v := range violations {
		if v.Code == "friendly-auth-floor-below-tunable-count" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected friendly-auth-floor-below-tunable-count; got %+v", violations)
	}
}

// TestFriendlyAuthGate_NoTunablesPassesNaturally — workerdev-shape yaml
// with zero porter-tunable directives → close passes.
func TestFriendlyAuthGate_NoTunablesPassesNaturally(t *testing.T) {
	t.Parallel()
	yaml := `zerops:
  - setup: worker
    run:
      base: nodejs@22
      start: node ./dist/worker.js
`
	tunables := countPorterTunableDirectives(yaml)
	if tunables != 0 {
		t.Errorf("workerdev-shape yaml should expose zero tunables; got %d", tunables)
	}
	violations := evalFriendlyAuthGate("codebase/worker/zerops-yaml", yaml)
	if len(violations) != 0 {
		t.Errorf("yaml with zero tunables must pass; got %+v", violations)
	}
}

// TestFriendlyAuthGate_AdequateCoveragePasses — yaml with 3 tunables
// + 1 adapt-path passes (ceil(3/3) = 1).
func TestFriendlyAuthGate_AdequateCoveragePasses(t *testing.T) {
	t.Parallel()
	yaml := `zerops:
  - setup: api
    run:
      base: nodejs@22
      # Feel free to bump this to 4 once your traffic profile warrants it,
      # after the L7 balancer load tests on stage.
      minContainers: 2
      maxContainers: 3
      verticalAutoscaling:
        minRam: 0.5
`
	tunables := countPorterTunableDirectives(yaml)
	if tunables < 3 {
		t.Fatalf("fixture should expose ≥3 tunables; got %d", tunables)
	}
	adapts := countAdaptPaths(yaml)
	if adapts < 1 {
		t.Fatalf("fixture should expose ≥1 adapt-path; got %d", adapts)
	}
	violations := evalFriendlyAuthGate("codebase/api/zerops-yaml", yaml)
	if len(violations) != 0 {
		t.Errorf("yaml with adequate coverage must pass; got %+v", violations)
	}
}

// TestFriendlyAuthGate_GoldensPass — jetstream + showcase regression
// fixtures. The plan named both goldens as regression anchors. Run-46
// Item 4 G5 followup — the original test read from on-disk repos and
// silently skipped on missing fixtures, producing a vacuous pass on
// any non-author machine. Fixtures now live in
// `internal/authoring/recipe/testdata/goldens/` so the test always runs against
// a stable corpus; the source-of-truth is the upstream zeropsio OSS
// recipes (laravel-jetstream-app, laravel-showcase-app) — re-sync via
// `cp` if those recipes change.
func TestFriendlyAuthGate_GoldensPass(t *testing.T) {
	t.Parallel()
	goldens := []string{
		"testdata/goldens/jetstream-zerops.yaml",
		"testdata/goldens/showcase-zerops.yaml",
	}
	for _, path := range goldens {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read golden %s: %v — testdata/goldens/ must ship with the repo per Run-46 Item 4 G5", path, err)
		}
		violations := evalFriendlyAuthGate("codebase/golden/zerops-yaml", string(body))
		for _, v := range violations {
			if v.Code == "friendly-auth-floor-below-tunable-count" {
				t.Errorf("golden %s fails Item 4 gate: %s", path, v.Message)
			}
		}
	}
}

// TestRequiredAdaptPaths_FormulaIsCeilDivThree — ceil(n/3) for the
// floor. 0→0, 1→1, 2→1, 3→1, 4→2, 6→2, 7→3.
func TestRequiredAdaptPaths_FormulaIsCeilDivThree(t *testing.T) {
	t.Parallel()
	cases := []struct {
		n    int
		want int
	}{
		{0, 0},
		{1, 1},
		{2, 1},
		{3, 1},
		{4, 2},
		{6, 2},
		{7, 3},
		{9, 3},
		{10, 4},
	}
	for _, tc := range cases {
		got := requiredAdaptPaths(tc.n)
		if got != tc.want {
			t.Errorf("requiredAdaptPaths(%d) = %d, want %d", tc.n, got, tc.want)
		}
	}
}

// TestPorterTunableEnumeration_PopulatesPlanMetadata — Item 4 design
// extends plan.json with porter-tunable directive enumeration computed
// at codebase-content close. The gate reads this metadata; agents +
// downstream tooling can inspect plan.PorterTunableDirectives[host].
//
// G4-followup — yaml fixture now exercises `ports[].port` AND a
// `*_URL` env-var so the test covers the expanded detector. Both
// patterns count alongside the existing minContainers / verticalAutoscaling
// allowlist.
func TestPorterTunableEnumeration_PopulatesPlanMetadata(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "api"}},
		Fragments: map[string]string{
			"codebase/api/zerops-yaml": `zerops:
  - setup: api
    run:
      base: nodejs@22
      minContainers: 2
      verticalAutoscaling:
        minRam: 0.5
      ports:
        - port: 3000
      envVariables:
        APP_URL: ${zeropsSubdomain}
`,
		},
	}
	populatePorterTunableDirectives(plan)
	got := plan.ObservedFacts.PorterTunableDirectives["api"]
	// minContainers + verticalAutoscaling.minRam + ports[].port + APP_URL = 4.
	if got < 4 {
		t.Errorf("expected ≥4 tunables (minContainers + minRam + port + APP_URL); got %d", got)
	}
}

// TestEnumeratePorterTunableDirectives_NamesPortsAndURLEnvVars — Run-46
// Item 4 G4 followup. The enumeration must surface `ports[].port`
// list-item declarations + env-var keys ending in `_URL` / `_HOST` /
// `_DOMAIN` / `_ORIGIN` whose values are NOT managed-service hostname
// templates. Managed-service wiring (`${db_hostname}`, value carries
// `_hostname}` substring) stays excluded so DB_HOST: ${db_hostname}
// doesn't count as porter-tunable.
func TestEnumeratePorterTunableDirectives_NamesPortsAndURLEnvVars(t *testing.T) {
	t.Parallel()
	yaml := `zerops:
  - setup: api
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        APP_URL: ${zeropsSubdomain}
        DB_HOST: ${db_hostname}
        MEILISEARCH_HOST: http://${search_hostname}:${search_port}
        CUSTOM_DOMAIN: example.com
`
	got := EnumeratePorterTunableDirectives(yaml)
	want := map[string]bool{
		"ports[].port":  true,
		"APP_URL":       true,
		"CUSTOM_DOMAIN": true,
	}
	gotSet := map[string]bool{}
	for _, n := range got {
		gotSet[n] = true
	}
	for w := range want {
		if !gotSet[w] {
			t.Errorf("expected %q in tunables; got %v", w, got)
		}
	}
	if gotSet["DB_HOST"] {
		t.Error("DB_HOST with ${db_hostname} value must be excluded as managed-service wiring")
	}
	if gotSet["MEILISEARCH_HOST"] {
		t.Error("MEILISEARCH_HOST with ${search_hostname} substring must be excluded as managed-service wiring")
	}
}

// TestPortTunableRE_SkipsHttpGetPort — Run-46 Item 4 G4 followup. The
// list-item-dash discriminator ensures `port:` under `httpGet:` /
// `readinessCheck:` is NOT counted (framework-pinned health-check port,
// not porter-tunable). Only the `- port: <value>` shape under
// `ports:` counts.
func TestPortTunableRE_SkipsHttpGetPort(t *testing.T) {
	t.Parallel()
	yaml := `zerops:
  - setup: api
    deploy:
      readinessCheck:
        httpGet:
          port: 80
          path: /health
    run:
      base: nodejs@22
      ports:
        - port: 3000
`
	got := EnumeratePorterTunableDirectives(yaml)
	portCount := 0
	for _, n := range got {
		if n == "ports[].port" {
			portCount++
		}
	}
	if portCount != 1 {
		t.Errorf("expected exactly 1 ports[].port (the list-item under ports:), got %d in %v", portCount, got)
	}
}
