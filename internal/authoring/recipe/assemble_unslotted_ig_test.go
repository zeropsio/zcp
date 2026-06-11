package recipe

import (
	"strings"
	"testing"
)

// Run-34 Fix 2 — un-slotted IG `codebase/<host>/integration-guide`
// MUST always be appended into the assembled README, regardless of
// whether slotted IG slots (`/integration-guide/<n>`) also exist or
// the order they were recorded in.
//
// Pre-fix: mergeSlottedIGFragments OVERWROTE the un-slotted key with
// the concatenated slot bodies whenever any slotted slot existed,
// silently dropping un-slotted content. cc-content-api recorded
// `codebase/api/integration-guide` (un-slotted) carrying appended
// codebase integration content; engine acked but the published apidev
// README shipped without that un-slotted body.
//
// Diagnosed in plans/run-34-validation.md §"Top 5 surprises" #2.

func TestStitchUnslottedIG_AlwaysAppends(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	for i, cb := range plan.Codebases {
		plan.Codebases[i].SourceRoot = "/var/www/" + cb.Hostname + "dev"
	}

	const unslottedBody = "### 4. Extra integration note\n\nKeep this unslotted body.\n"
	cases := []struct {
		name      string
		fragments map[string]string
	}{
		{
			name: "unslotted only — back-compat path",
			fragments: map[string]string{
				"codebase/api/integration-guide": unslottedBody,
			},
		},
		{
			name: "slotted then unslotted — agent recorded slot first",
			fragments: map[string]string{
				"codebase/api/integration-guide/2": "### 2. Slotted item two\n",
				"codebase/api/integration-guide/3": "### 3. Slotted item three\n",
				"codebase/api/integration-guide":   unslottedBody,
			},
		},
		{
			name: "unslotted then slotted — agent recorded unslotted first",
			fragments: map[string]string{
				"codebase/api/integration-guide":   unslottedBody,
				"codebase/api/integration-guide/2": "### 2. Slotted item two\n",
				"codebase/api/integration-guide/3": "### 3. Slotted item three\n",
			},
		},
		{
			name: "slot 5 only + unslotted — sparse slot space",
			fragments: map[string]string{
				"codebase/api/integration-guide":   unslottedBody,
				"codebase/api/integration-guide/5": "### 5. Slotted item five\n",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			merged := mergeSlottedIGFragments(tc.fragments, "api")
			got := merged["codebase/api/integration-guide"]
			if !strings.Contains(got, "Extra integration note") {
				t.Errorf("un-slotted IG body dropped from merge result.\nFragments: %v\nMerged: %q",
					tc.fragments, got)
			}
		})
	}
}

// TestStitchUnslottedIG_SiblingHostsUnaffected — merging slotted IG
// for host A must not touch host B's un-slotted body.
func TestStitchUnslottedIG_SiblingHostsUnaffected(t *testing.T) {
	t.Parallel()

	frags := map[string]string{
		"codebase/api/integration-guide/2": "### 2. api slot two\n",
		"codebase/api/integration-guide":   "### Api unslotted\n\nApi unslotted\n",
		"codebase/app/integration-guide":   "### App unslotted\n\nApp unslotted\n",
	}
	merged := mergeSlottedIGFragments(frags, "api")
	if got := merged["codebase/app/integration-guide"]; !strings.Contains(got, "App unslotted") {
		t.Errorf("merging api slots clobbered app's un-slotted IG body; got %q", got)
	}
}

func TestForbidRecipeLevelSectionsOnAppsRepos_Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		host     string
		body     string
		wantCode string
	}{
		{
			name:     "canonical_apps_repo_rf1_blocks",
			host:     "api",
			body:     "## Recipe features\n\n- forbidden\n",
			wantCode: "apps-repo-has-rf1",
		},
		{
			name:     "non_canonical_apps_repo_rf1_blocks",
			host:     "app",
			body:     "## Recipe features\n\n- forbidden\n",
			wantCode: "apps-repo-has-rf1",
		},
		{
			name:     "pd1_blocks",
			host:     "api",
			body:     "## Production vs. Development\n\n- forbidden\n",
			wantCode: "apps-repo-has-pd1",
		},
		{
			name:     "understand_blocks",
			host:     "worker",
			body:     "## Understand Zerops Core Concepts\n\nRead docs.\n",
			wantCode: "apps-repo-has-understand",
		},
		{
			name: "clean_apps_repo_passes",
			host: "api",
			body: "### 4. Configure uploads\n\nUse the S3 client.\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := appsRepoSectionTestPlan(t, tc.host, tc.body)
			violations := forbidRecipeLevelSectionsOnAppsRepos(plan)
			if tc.wantCode == "" {
				if len(violations) > 0 {
					t.Fatalf("expected no violations, got %+v", violations)
				}
				return
			}
			if len(violations) == 0 {
				t.Fatalf("expected %s violation", tc.wantCode)
			}
			var found bool
			for _, v := range violations {
				if v.Code == tc.wantCode {
					found = true
					if v.Severity != SeverityBlocking {
						t.Errorf("violation %q must be blocking, got %v", v.Code, v.Severity)
					}
				}
			}
			if !found {
				t.Fatalf("missing %s violation in %+v", tc.wantCode, violations)
			}
		})
	}
}

func TestForbidRecipeLevelSectionsOnAppsRepos_PreScaffoldNoOp(t *testing.T) {
	t.Parallel()
	plan := appsRepoSectionTestPlan(t, "api", "## Recipe features\n\n- forbidden\n")
	for i := range plan.Codebases {
		plan.Codebases[i].SourceRoot = ""
	}
	if violations := forbidRecipeLevelSectionsOnAppsRepos(plan); len(violations) > 0 {
		t.Fatalf("pre-scaffold codebases should no-op, got %+v", violations)
	}
}

// TestForbidRecipeLevelSectionsOnAppsRepos_HeadingsInCodeFence_NotFalsePositive —
// an IG step that quotes the literal heading text inside a fenced code
// block must NOT trip the absence gate. Pre-fix containsHeading scanned every line
// including those inside ``` fences, so a markdown code-block sample
// of `## Recipe features` would block the close on every apps-repo.
func TestForbidRecipeLevelSectionsOnAppsRepos_HeadingsInCodeFence_NotFalsePositive(t *testing.T) {
	t.Parallel()
	plan := appsRepoSectionTestPlan(t, "api", "### 2. Example heading shapes you'll see\n\n```markdown\n## Recipe features\n\n## Production vs. Development\n\n## Understand Zerops Core Concepts\n```\n")
	violations := forbidRecipeLevelSectionsOnAppsRepos(plan)
	if len(violations) > 0 {
		t.Errorf("gate must not fire on heading-text inside code fences; got %+v", violations)
	}
}

// TestContainsHeading_HeadingInsideCodeFence_NotMatched — direct unit
// test of containsHeading's fence-aware behavior. Pinning the helper
// directly so a future containsHeading refactor doesn't lose the
// fence-skip property without surfacing on the gate-level tests
// (which exercise multiple layers).
func TestContainsHeading_HeadingInsideCodeFence_NotMatched(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "real H2 above a fence — matched",
			body: "## Recipe features\n\n- bullet\n\n```\nirrelevant\n```\n",
			want: true,
		},
		{
			name: "H2 only inside fence — not matched",
			body: "Intro paragraph.\n\n```markdown\n## Recipe features\n```\n",
			want: false,
		},
		{
			name: "H2 only inside language-tagged fence — not matched",
			body: "Intro paragraph.\n\n```yaml\n## Recipe features\n```\n",
			want: false,
		},
		{
			name: "H2 outside fence after a fence closes — matched",
			body: "```\ncode\n```\n\n## Recipe features\n",
			want: true,
		},
		{
			name: "H2 inside fence + real H2 after — matched (real H2 wins)",
			body: "```markdown\n## Recipe features\n```\n\n## Recipe features\n",
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := containsHeading(tc.body, "## Recipe features")
			if got != tc.want {
				t.Errorf("containsHeading(...) = %v, want %v\nbody:\n%s", got, tc.want, tc.body)
			}
		})
	}
}

// TestCompletePhase_ForbidsRecipeLevelSectionsOnAppsRepos — the wired-in
// gate: un-scoped main-agent `complete-phase phase=codebase-content`
// refuses when any apps-repo's assembled README carries recipe-level H2s.
func TestCompletePhase_ForbidsRecipeLevelSectionsOnAppsRepos(t *testing.T) {
	t.Parallel()
	gates := CodebaseContentGates()
	var foundCC bool
	for _, g := range gates {
		if g.Name == "apps-repo-no-recipe-level-sections" {
			foundCC = true
		}
	}
	if !foundCC {
		t.Errorf("CodebaseContentGates() must register `apps-repo-no-recipe-level-sections`; got %v", gateNames(gates))
	}
	// FinalizeGates() chains CodebaseGates() which unions
	// CodebaseScaffoldGates() + CodebaseContentGates(); the absence gate
	// must be reachable through that chain so finalize re-runs it.
	finalize := FinalizeGates()
	var foundFinalize bool
	for _, g := range finalize {
		if g.Name == "apps-repo-no-recipe-level-sections" {
			foundFinalize = true
		}
	}
	if !foundFinalize {
		t.Errorf("FinalizeGates() must include `apps-repo-no-recipe-level-sections`; got %v", gateNames(finalize))
	}
}

// canonicalAppsRepoTestPlan returns a synthetic plan whose canonical
// apps-repo (api codebase) has an un-slotted IG fragment with the
// caller-supplied body. Body shape lets each test target one of
// forbidden-section / clean cases without sharing state.
func appsRepoSectionTestPlan(t *testing.T, host, unslottedIGBody string) *Plan {
	t.Helper()
	plan := syntheticShowcasePlan()
	for i, cb := range plan.Codebases {
		plan.Codebases[i].SourceRoot = "/var/www/" + cb.Hostname + "dev"
	}
	plan.Fragments = map[string]string{}
	for _, cb := range plan.Codebases {
		base := "codebase/" + cb.Hostname
		plan.Fragments[base+"/intro"] = "Codebase intro paragraph.\n"
		// Whole-yaml fragment so AssembleCodebaseREADME doesn't trip on
		// the missing-on-disk-yaml hardening (M-2 in assemble.go).
		plan.Fragments[fragmentIDCodebaseZeropsYAML(cb.Hostname)] = "zerops:\n  - setup: " + cb.Hostname + "\n    build:\n      base: nodejs@22\n    run:\n      base: nodejs@22\n      start: npm start\n"
		// Two slotted IG items so the IG validator passes; this helper
		// targets only the recipe-level section gate.
		plan.Fragments[base+"/integration-guide/2"] = "### 2. Trust the reverse proxy\n\nSet trust proxy.\n"
		plan.Fragments[base+"/integration-guide/3"] = "### 3. Drain on SIGTERM\n\nGraceful shutdown.\n"
		plan.Fragments[base+"/knowledge-base"] = "### Gotchas\n\n- **404 on Topic** — explanation.\n"
		plan.Fragments[base+"/claude-md"] = "# " + cb.Hostname + "\n\nApp scaffold.\n"
	}
	if unslottedIGBody != "" {
		plan.Fragments["codebase/"+host+"/integration-guide"] = unslottedIGBody
	}
	return plan
}
