package recipe

import (
	"slices"
	"strings"
	"testing"
)

// Run-41 — refinement-2 (cross-surface audit) brief composer. Pins the
// transactional contract: brief carries the phase entry, the per-
// defect-class audit checklist, the citation map, and the stitched-
// output pointer block. No filesystem-local references leak from the
// design-time author's machine. Findings-only output contract — the
// closing footer in `briefs_subagent_prompt.go::writePromptCloseFooter`
// is what enforces "no record-fragment calls"; the brief body itself
// is the audit substrate.

// TestBuildRefinement2Brief_AssemblesCoreAtoms — composer threads both
// embedded atoms (phase_entry + audit_checklist) into the brief body
// and surfaces them in brief.Parts for downstream attribution.
func TestBuildRefinement2Brief_AssemblesCoreAtoms(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "synth-showcase",
		Tier: "showcase",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
			{Hostname: "worker", Role: RoleWorker, BaseRuntime: "nodejs@22", IsWorker: true},
		},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	if brief.Kind != BriefRefinement2 {
		t.Errorf("brief.Kind = %v, want %v", brief.Kind, BriefRefinement2)
	}
	for _, want := range []string{
		"briefs/refinement2/phase_entry.md",
		"briefs/refinement2/audit_checklist.md",
		"stitched-output-pointer-block",
		"citation-map",
	} {
		if !slices.Contains(brief.Parts, want) {
			t.Errorf("brief.Parts missing %q; got %v", want, brief.Parts)
		}
	}
	if brief.Bytes == 0 {
		t.Error("brief.Bytes is zero — composer produced no body")
	}
}

// TestBuildRefinement2Brief_CarriesAuditDefectClasses — brief body
// names every cross-surface defect class the run-40 validation
// flagged. The audit_checklist atom is the source of truth; this
// test pins that the load-bearing class names survive the embed and
// composition pipeline.
func TestBuildRefinement2Brief_CarriesAuditDefectClasses(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	for _, defectClass := range []string{
		"kb-ig-duplication",
		// Run-43 F2 — `kb-below-floor` removed (spec §S5 now declares
		// "no floor; cap 8"; goldens span 2-7 bullets). `kb-over-cap`
		// retained.
		"kb-over-cap",
		"surface-misplacement",
		"aspirational-as-current",
		"yaml-comment-content-drift",
		"scaffold-code-in-kb",
		"ig-cites-recipe-internal-file",
		"missing-citation",
		"cross-codebase-named-constant-drift",
		// Run-41 dogfood additions — spec-anchored classes the
		// run-41 audit's 10-class set didn't cover.
		"framework-quirk-as-gotcha",
		"scaffold-decision-as-gotcha",
		"cross-codebase-content-duplication",
		// Run-43 P2 — self-inflicted classifier (spec §"Fact
		// classification taxonomy" litmus #4).
		"self-inflicted-as-gotcha",
	} {
		if !strings.Contains(brief.Body, defectClass) {
			t.Errorf("brief.Body missing defect class %q", defectClass)
		}
	}
}

// TestBuildRefinement2Brief_StitchedPointerBlockListsAllSurfaces —
// when runDir is non-empty the brief renders Read-targets for every
// stitched surface (root, six tier directories, every codebase).
func TestBuildRefinement2Brief_StitchedPointerBlockListsAllSurfaces(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "synth-showcase",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
			{Hostname: "app", Role: RoleFrontend, BaseRuntime: "nodejs@22"},
			{Hostname: "worker", Role: RoleWorker, BaseRuntime: "nodejs@22", IsWorker: true},
		},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	// Root surfaces.
	for _, want := range []string{
		"/run/dir/README.md",
		"/run/dir/environments/plan.json",
		"/run/dir/environments/facts.jsonl",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("brief.Body missing pointer %q", want)
		}
	}
	// Per-tier surfaces — every tier folder appears with README +
	// import.yaml on the same pointer line (relative-yaml shorthand
	// for compact rendering; the agent reads both files from the
	// same tier dir). Pin presence of the README path + an explicit
	// `import.yaml` token in the body.
	if !strings.Contains(brief.Body, "import.yaml") {
		t.Error("brief.Body missing `import.yaml` shorthand reference for tier pointer lines")
	}
	for _, tier := range Tiers() {
		readme := "/run/dir/environments/" + tier.Folder + "/README.md"
		if !strings.Contains(brief.Body, readme) {
			t.Errorf("brief.Body missing tier README pointer %q", readme)
		}
	}
	// Per-codebase surfaces — every codebase appears in a single
	// pointer line listing README + zerops.yaml + CLAUDE.md.
	for _, cb := range plan.Codebases {
		want := "/run/dir/" + cb.Hostname + "dev/README.md"
		if !strings.Contains(brief.Body, want) {
			t.Errorf("brief.Body missing codebase pointer %q", want)
		}
	}
	// Run-41 — per-codebase dependency manifests for
	// aspirational-as-current framework-feature cross-check.
	if !strings.Contains(brief.Body, "package.json") {
		t.Error("brief.Body missing per-codebase package.json pointer for aspirational-as-current manifest scan")
	}
	if !strings.Contains(brief.Body, "composer.json") {
		t.Error("brief.Body missing per-codebase composer.json pointer for aspirational-as-current manifest scan")
	}
}

// TestBuildRefinement2Brief_NoFilesystemReferenceLeak — composer must
// not bleed author-machine paths into the brief body. Mirrors the
// run-23 F-24 contract that pins BuildRefinementBrief.
func TestBuildRefinement2Brief_NoFilesystemReferenceLeak(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	// Pass a porter-facing runDir, NOT the author's working tree.
	brief, err := BuildRefinement2Brief(plan, nil, "/var/www/some-recipe", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	// `/Users/<name>/` and `/var/www/zcprecipator/` are the author-
	// side leak shapes the ENG-2 sanitizer scrubs in TIMELINE. Brief
	// composer must not produce them in the first place.
	for _, forbidden := range []string{
		"/Users/",
		"/var/www/zcprecipator/",
	} {
		if strings.Contains(brief.Body, forbidden) {
			t.Errorf("brief.Body leaks author-side path containing %q", forbidden)
		}
	}
}

// TestBuildRefinement2Brief_EmptyRunDirSkipsStitchedPointerBlock —
// static-composition entry path (buildBriefForKind from
// buildSubagentPromptForPhase) passes empty runDir. Composer must
// suppress the pointer block in that case so the brief still
// composes and carries the audit substrate.
func TestBuildRefinement2Brief_EmptyRunDirSkipsStitchedPointerBlock(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief with empty runDir: %v", err)
	}
	if slices.Contains(brief.Parts, "stitched-output-pointer-block") {
		t.Error("empty runDir should suppress stitched-output-pointer-block; got it in Parts")
	}
	// Audit substrate still lands.
	for _, want := range []string{
		"briefs/refinement2/phase_entry.md",
		"briefs/refinement2/audit_checklist.md",
		"citation-map",
	} {
		if !slices.Contains(brief.Parts, want) {
			t.Errorf("brief.Parts missing %q with empty runDir; got %v", want, brief.Parts)
		}
	}
}

// TestBuildRefinement2Brief_PerServiceTypeAliasAllowlist — pins the
// run-41 round-2 fix for the yaml-comment-content-drift mechanizability
// hole. The v1 alias list was service-type-agnostic (listed
// `${<host>_password}` generically), causing the rule to contradict
// its own N-1 worked example for ${search_password} (meilisearch has
// no `password` alias). The fix splits the allowlist per service
// type. Pin the table presence + the load-bearing per-type rows.
func TestBuildRefinement2Brief_PerServiceTypeAliasAllowlist(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	// Per-service-type table header lands.
	if !strings.Contains(brief.Body, "Per-service-type alias allowlist") {
		t.Error("brief.Body missing per-service-type alias allowlist section")
	}
	// Each service type row pins its allowed keys. The N-1 fix
	// requires `password` to live ONLY in postgres/valkey + nats
	// rows, NOT in the meilisearch row.
	for _, want := range []string{
		"postgresql",
		"meilisearch",
		"masterKey, defaultSearchKey",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("brief.Body missing per-service-type row substring %q", want)
		}
	}
	// Negative check: meilisearch row must NOT include `password` as
	// a documented alias. The brief renders allowlist rows in
	// sequence; verify by scanning the chunk between
	// "meilisearch@*" and the next pipe-delimited row boundary.
	if idx := strings.Index(brief.Body, "| meilisearch@*"); idx >= 0 {
		tail := brief.Body[idx:]
		// next row begins at the next "|" at start of line after
		// the meilisearch row's pipe-trailing closer; pragmatically,
		// grab the next 200 chars and verify `password` absent.
		end := min(idx+200, len(brief.Body))
		mline := brief.Body[idx:end]
		if strings.Contains(mline, "password") {
			t.Errorf("meilisearch row leaks `password` as a documented alias; got %q", strings.SplitN(tail, "\n", 2)[0])
		}
	} else {
		t.Error("brief.Body missing meilisearch row in per-service-type table")
	}
}

// TestBuildRefinement2Brief_CitationMapMatchesSpec — citation map
// inlined into the brief lists exactly the topics named in
// docs/spec-content-surfaces.md §"Citation map". Pin the keyword
// presence so spec drift surfaces as a test failure.
func TestBuildRefinement2Brief_CitationMapMatchesSpec(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	for _, topic := range []string{
		"rolling-deploys",
		"init-commands",
		"object-storage",
		"env-var-model",
		"subdomain access",
		"managed NATS broker",
		"managed Meilisearch service",
		// Run-41 — spec-parity widening. These three topics live in
		// the spec's citation map but were missing from the brief's
		// rendered block; the audit silently passed bullets that
		// should have flagged.
		"deploy-files",
		"readiness",
		"trust proxy",
	} {
		if !strings.Contains(brief.Body, topic) {
			t.Errorf("citation map missing topic %q", topic)
		}
	}
}

// TestBuildRefinement2Brief_HostGlossary — run-41 dogfood found every
// finding mis-routed fragmentIds through the SSHFS-mount form
// (`codebase/appdev/...`) instead of the plan.fragments canonical
// short form (`codebase/app/...`). Pin the audit-checklist glossary
// that disambiguates `<host>`.
func TestBuildRefinement2Brief_HostGlossary(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	// Glossary names the short-form / SSHFS-mount distinction
	// explicitly.
	for _, want := range []string{
		"<host>` placeholder convention",
		"READ FIRST",
		"plan.codebases[].host",
		"NOT the SSHFS-mount",
		// wrong shape — must be called out explicitly
		"codebase/appdev/knowledge-base",
		// right shape — must be shown
		"codebase/app/knowledge-base",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("brief.Body missing host-glossary substring %q", want)
		}
	}
}

// TestBuildRefinement2Brief_PerFindingTriageContract — run-41 dogfood
// the main agent bulk-HELD all 10 advisory findings with a one-line
// "ships acceptably" rather than per-finding ACT/HOLD/ACCEPT. Pin
// the phase_entry instruction that requires per-finding triage in
// the close transcript.
func TestBuildRefinement2Brief_PerFindingTriageContract(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	for _, want := range []string{
		"Severity is a starting point",
		"Per-finding triage is the contract",
		"ACT",
		"HOLD",
		"ACCEPT",
		"Bulk dismissals like \"all advisories HELD\" are not acceptable",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("brief.Body missing per-finding-triage substring %q", want)
		}
	}
}

// TestBuildRefinement2Brief_P2_SelfInflictedAsGotchaClass — Run-43
// P2. The refinement-2 audit checklist gains a new defect class
// `self-inflicted-as-gotcha` that fires when a KB bullet documents
// a trap firing only when the porter deviates from IG #1's shipped
// envVariables. Spec citation: docs/spec-content-surfaces.md
// §"Fact classification taxonomy" litmus #4 + §"Self-inflicted
// (should have been discarded)". Run-42 dogfood evidence: apidev
// KB #2 `UnknownError on first GetObject` + apidev KB #3
// `fetch().headers.get('X-Cache') returns null` both fail the
// porter-following-IG#1-verbatim test; both should have routed to
// DROP.
func TestBuildRefinement2Brief_P2_SelfInflictedAsGotchaClass(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	// Class header lands.
	if !strings.Contains(brief.Body, "## Defect class: self-inflicted-as-gotcha") {
		t.Error("audit_checklist missing self-inflicted-as-gotcha class header")
	}
	// Decisive Check #1 — porter-following-IG#1-verbatim test.
	for _, want := range []string{
		"porter-following-IG#1-verbatim test",
		"copying IG #1's shipped envVariables verbatim",
		"http://${storage_apiHost}",
		"S3_ENDPOINT: ${storage_apiUrl}",
		"Run-42 dogfood: apidev KB #2",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("self-inflicted-as-gotcha class missing anchor %q", want)
		}
	}
	// Counter-example — NATS Pattern A vs Pattern B is NOT self-inflicted.
	for _, want := range []string{
		"Counter-example (NOT self-inflicted)",
		"Pattern A (four separate `${broker_*}`",
		"Pattern B (connection-string assembly",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("self-inflicted-as-gotcha counter-example missing anchor %q", want)
		}
	}
	// Severity is blocker (spec routes to DISCARD unambiguously).
	idx := strings.Index(brief.Body, "## Defect class: self-inflicted-as-gotcha")
	if idx < 0 {
		t.Fatal("class header missing")
	}
	tail := brief.Body[idx:]
	next := strings.Index(tail[1:], "\n## Defect class:")
	var chunk string
	if next > 0 {
		chunk = tail[:next+1]
	} else {
		chunk = tail
	}
	if !strings.Contains(chunk, "**Severity**: **blocker**") {
		t.Error("self-inflicted-as-gotcha must declare **Severity**: **blocker**")
	}
	if !strings.Contains(chunk, "**Action**: `drop`") {
		t.Error("self-inflicted-as-gotcha must declare **Action**: `drop`")
	}
}

// TestBuildRefinement2Brief_P2_PhaseEntryEnumIncludesSelfInflicted —
// Run-43 P2. The defectClass JSON enum in phase_entry.md must
// include the new class so the audit sub-agent's emitted JSON
// findings block lists it among valid values.
func TestBuildRefinement2Brief_P2_PhaseEntryEnumIncludesSelfInflicted(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	// Locate the defectClass enum line in phase_entry.md as embedded
	// in the brief body. It's the long pipe-separated alternation;
	// pin the new class is one of the alternatives.
	enumIdx := strings.Index(brief.Body, `"defectClass":`)
	if enumIdx < 0 {
		t.Fatal("defectClass enum line missing from brief")
	}
	enumEnd := strings.Index(brief.Body[enumIdx:], ",\n")
	if enumEnd < 0 {
		t.Fatal("defectClass enum line not terminated")
	}
	enumLine := brief.Body[enumIdx : enumIdx+enumEnd]
	if !strings.Contains(enumLine, `"self-inflicted-as-gotcha"`) {
		t.Errorf("phase_entry.md defectClass enum missing `self-inflicted-as-gotcha`; got: %s", enumLine)
	}
	// Walk-every-class summary at the audit_checklist.md tail must
	// also list the new class.
	walkIdx := strings.Index(brief.Body, "Walk EVERY defect class in this checklist")
	if walkIdx < 0 {
		t.Fatal("walk-every-class summary missing")
	}
	walkEnd := min(walkIdx+600, len(brief.Body))
	walkWindow := brief.Body[walkIdx:walkEnd]
	if !strings.Contains(walkWindow, "self-inflicted-as-gotcha") {
		t.Errorf("walk-every-class summary missing `self-inflicted-as-gotcha`; got chunk: %s", walkWindow)
	}
}

// TestBuildRefinement2Brief_CitationMapURLsAreStable — Run-43 P7.
// The citation-map block now exposes URL fragments as named constants
// (`citationURL*`) and the brief renders them verbatim. Form (b) and
// form (c) acceptance tightened to demand EXACT URL match — host-
// only matches no longer pass. Run-42 dogfood evidence:
// workerdev/README.md:241 shipped
// `docs.zerops.io/features/env-variables#env-var-model` for an
// env-var-model citation; the brief's citation map specifies
// `docs.zerops.io/zerops-yaml/specification#envvariables-`. The
// fabricated fragment passed the loose docs.zerops.io host check.
// Pin every URL fragment as it should appear in the brief, so a
// future drift surfaces here, not in the next dogfood run.
func TestBuildRefinement2Brief_CitationMapURLsAreStable(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	// Every citation-map URL renders verbatim in the brief body —
	// pin against the named constants so the test fails fast if the
	// brief composer drifts from the constants OR the constants
	// drift from the spec.
	urls := []struct {
		name string
		url  string
	}{
		{"rollingDeploys", citationURLRollingDeploys},
		{"initCommands", citationURLInitCommands},
		{"objectStorage", citationURLObjectStorage},
		{"envVarModel", citationURLEnvVarModel},
		{"httpSupport", citationURLHTTPSupport},
		{"deployFiles", citationURLDeployFiles},
		{"readinessChecks", citationURLReadinessChecks},
		{"managedNATS", citationURLManagedNATS},
		{"managedMeilisearch", citationURLManagedMeilisearch},
	}
	for _, u := range urls {
		if !strings.Contains(brief.Body, u.url) {
			t.Errorf("brief body missing citation URL for %s: %q", u.name, u.url)
		}
	}
	// Run-43 F3 — citation URL form (b)/(c) wording rewrite. Codex
	// flagged three internal contradictions in the prior "path-starts-with"
	// framing:
	//   (1) Title said "path-starts-with" but the definition required
	//       host+path EQUALITY (not prefix-matching). Self-contradictory.
	//   (2) "The anchor extends the same path" conflated URL fragments
	//       with path components — fragments don't extend paths, they
	//       select anchors within a document.
	//   (3) No URL normalization rule (trailing slash, scheme casing) —
	//       cosmetic variants could false-positive.
	//
	// Edge case: a link to
	// `docs.zerops.io/zerops-yaml/specification#some-wrong-fragment`
	// (same path as the map URL `...#envvariables-`, but wrong
	// fragment) would FALSE-NEGATIVE under the prior "anchor extends
	// path" tolerance — the defect was invisible when the citation map
	// URL had a specific anchor.
	//
	// The rewrite:
	//   - Renames the rule "host+path match" (accurate; not
	//     prefix-matching).
	//   - Defines normalization: scheme-and-host-case-insensitive,
	//     trailing slash optional on path.
	//   - Fragment rule: if the citation map URL has NO fragment, any
	//     fragment on the link passes. If the map URL has a SPECIFIC
	//     fragment, the link's fragment must match it exactly.
	//   - Three worked examples (two PASSES, two FAILS).
	for _, want := range []string{
		// New "host+path match" wording.
		"host+path match",
		// Normalization rule.
		"scheme-and-host-case-insensitive",
		"trailing slash optional",
		// Worked example 1 — PASSES: bare map URL + fragment-extended
		// link (same-path in-page anchor).
		"docs.zerops.io/features/scaling-ha#high-availability-via-rolling-deploys",
		// Worked example 2 — PASSES: exact-fragment match.
		"exact anchor match",
		// Worked example 3 — FAILS: same path, wrong fragment.
		"same-path wrong-fragment",
		// Worked example 4 — FAILS: different path entirely (Run-42
		// fabricated URL).
		"docs.zerops.io/features/env-variables#env-var-model",
		"different path from map's",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("brief body missing F3 citation-rule rewrite anchor %q", want)
		}
	}
}

// TestBuildRefinement2Brief_CitationMapURLConstants_MatchSpec — pin
// the named URL constants against the spec's authoritative shape.
// The spec map at docs/spec-content-surfaces.md §"Citation map"
// lists guide IDs; the actual docs URLs are inferred from
// zerops_knowledge guide structure. This test guards against an
// inadvertent constant edit that diverges the brief from production
// docs URLs without surfacing the divergence in CI.
func TestBuildRefinement2Brief_CitationMapURLConstants_MatchSpec(t *testing.T) {
	t.Parallel()
	expected := map[string]string{
		"rolling-deploys":         "docs.zerops.io/features/scaling-ha",
		"init-commands":           "docs.zerops.io/zerops-yaml/specification#initcommands-",
		"object-storage":          "docs.zerops.io/services/object-storage",
		"env-var-model":           "docs.zerops.io/zerops-yaml/specification#envvariables-",
		"http-support":            "docs.zerops.io/features/access",
		"deploy-files":            "docs.zerops.io/zerops-yaml/specification#deployfiles-",
		"readiness-health-checks": "docs.zerops.io/zerops-yaml/specification#readinesscheck-",
		"managed-nats":            "docs.zerops.io/services/nats",
		"managed-meilisearch":     "docs.zerops.io/services/meilisearch",
	}
	actual := map[string]string{
		"rolling-deploys":         citationURLRollingDeploys,
		"init-commands":           citationURLInitCommands,
		"object-storage":          citationURLObjectStorage,
		"env-var-model":           citationURLEnvVarModel,
		"http-support":            citationURLHTTPSupport,
		"deploy-files":            citationURLDeployFiles,
		"readiness-health-checks": citationURLReadinessChecks,
		"managed-nats":            citationURLManagedNATS,
		"managed-meilisearch":     citationURLManagedMeilisearch,
	}
	for k, want := range expected {
		got, ok := actual[k]
		if !ok {
			t.Errorf("constant for topic %q missing", k)
			continue
		}
		if got != want {
			t.Errorf("citation URL drift for topic %q: got %q, want %q", k, got, want)
		}
	}
}

// TestBuildRefinement2Brief_NewDefectClassesAreBlocker — the three
// run-41 spec-anchored classes are blocker severity because the
// spec's classification taxonomy + cross-surface duplication rule
// are unambiguous. Pin the severity assignment in the checklist
// body so an inadvertent demotion to advisory surfaces in CI.
func TestBuildRefinement2Brief_NewDefectClassesAreBlocker(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	// Each class header + its severity line in the audit_checklist
	// embed. The atom uses a `**Severity**: **blocker** ...` shape
	// after each defect class.
	for _, cls := range []string{
		"framework-quirk-as-gotcha",
		"scaffold-decision-as-gotcha",
		"cross-codebase-content-duplication",
	} {
		header := "## Defect class: " + cls
		idx := strings.Index(brief.Body, header)
		if idx < 0 {
			t.Errorf("class header missing for %q", cls)
			continue
		}
		// Look at the body from class header until next class header
		// or end-of-body.
		tail := brief.Body[idx:]
		nextDivider := strings.Index(tail[1:], "\n## Defect class:")
		var chunk string
		if nextDivider > 0 {
			chunk = tail[:nextDivider+1]
		} else {
			chunk = tail
		}
		if !strings.Contains(chunk, "**Severity**: **blocker**") {
			t.Errorf("class %q must declare **Severity**: **blocker** in its body; chunk did not match", cls)
		}
	}
}
