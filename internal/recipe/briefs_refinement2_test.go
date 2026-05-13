package recipe

import (
	"slices"
	"strings"
	"testing"
)

// Run-45 — refinement-2 brief composer tests. Pillar B rewrote the
// audit substrate around per-surface single-question walks; the
// 13-defect-class enumeration retired with the substrate. Tests here
// pin the generic composer contracts (atoms assembled, stitched-output
// pointer block, citation map, host-glossary, per-finding triage) plus
// the Run-45 per-surface walk shape.

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

// TestBuildRefinement2Brief_PerSurfaceWalkShape — Run-45 Pillar B. The
// rewritten audit substrate carries the per-surface single-question
// walk as the load-bearing shape, NOT a 13-defect-class enumeration.
// Pin every surface's section header + its single-question test
// presence.
func TestBuildRefinement2Brief_PerSurfaceWalkShape(t *testing.T) {
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
		"**S4 (Integration Guide) — single-question test**",
		"**S5 (Knowledge Base) — single-question test**",
		"**S6 (Per-codebase CLAUDE.md) — single-question test**",
		"**S7 (Per-codebase `zerops.yaml`) — single-question test**",
		"**S3 (Tier `import.yaml` comments) — single-question test**",
		// Failure-mode emission shape.
		"## Failure-mode emission shape",
		"**DROP**",
		"**MOVE-TO-",
		"**REWRITE**",
		// Mechanical walk loop.
		"For SURFACE in {S3, S4, S5, S6, S7}",
		// Cross-surface uniqueness pass.
		"## Cross-surface uniqueness pass",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("brief.Body missing per-surface-walk anchor %q", want)
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
	if !strings.Contains(brief.Body, "import.yaml") {
		t.Error("brief.Body missing `import.yaml` shorthand reference for tier pointer lines")
	}
	for _, tier := range Tiers() {
		readme := "/run/dir/environments/" + tier.Folder + "/README.md"
		if !strings.Contains(brief.Body, readme) {
			t.Errorf("brief.Body missing tier README pointer %q", readme)
		}
	}
	for _, cb := range plan.Codebases {
		want := "/run/dir/" + cb.Hostname + "dev/README.md"
		if !strings.Contains(brief.Body, want) {
			t.Errorf("brief.Body missing codebase pointer %q", want)
		}
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
	brief, err := BuildRefinement2Brief(plan, nil, "/var/www/some-recipe", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
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
		"deploy-files",
		"readiness",
		"trust proxy",
	} {
		if !strings.Contains(brief.Body, topic) {
			t.Errorf("citation map missing topic %q", topic)
		}
	}
}

// TestBuildRefinement2Brief_HostGlossary — pin the audit-checklist
// glossary that disambiguates `<host>` vs `<host>dev`. Run-41 dogfood
// found every finding mis-routed fragmentIds through the SSHFS-mount
// form (`codebase/appdev/...`) instead of the plan.fragments canonical
// short form (`codebase/app/...`).
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
	for _, want := range []string{
		"<host>` placeholder convention",
		"plan.codebases[].host",
		"NOT the SSHFS-mount",
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
		`Bulk dismissals like "all advisories HELD" are not acceptable`,
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("brief.Body missing per-finding-triage substring %q", want)
		}
	}
}

// TestBuildRefinement2Brief_CitationMapURLsAreStable — Run-43 P7.
// The citation-map block exposes URL fragments as named constants
// (`citationURL*`) and the brief renders them verbatim.
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
	for _, want := range []string{
		"host+path match",
		"scheme-and-host-case-insensitive",
		"trailing slash optional",
		"docs.zerops.io/features/scaling-ha#high-availability-via-rolling-deploys",
		"exact anchor match",
		"same-path wrong-fragment",
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
