package content

import (
	"sort"
	"strings"
	"testing"
)

// TestAtomAuthoringLint proves the atom corpus is free of authoring-
// contract violations: no spec-ID citations, no handler-behavior verbs,
// no invisible-state field names, no plan-doc cross-references, and no
// axis K/L/M/N drift (spec §11.5/§11.6). Part of the atom authoring
// contract — atoms describe observable response/envelope state, not
// developer taxonomy or handler internals.
//
// Failure messages are grouped by atom file and name the category,
// pattern, 1-indexed line number, and matching snippet. An intentional
// exception can be added to atomLintAllowlist (cross-axis) or one of
// the per-axis allowlists in atoms_lint_seed_allowlist.go with a
// documented rationale.
func TestAtomAuthoringLint(t *testing.T) {
	t.Parallel()

	violations, err := LintAtomCorpus()
	if err != nil {
		t.Fatalf("LintAtomCorpus: %v", err)
	}
	if len(violations) == 0 {
		return
	}

	sort.Slice(violations, func(i, j int) bool {
		if violations[i].AtomFile != violations[j].AtomFile {
			return violations[i].AtomFile < violations[j].AtomFile
		}
		return violations[i].Line < violations[j].Line
	})
	for _, v := range violations {
		t.Errorf(
			"authoring-contract violation — %s:%d [%s / %s]\n\t%s",
			v.AtomFile, v.Line, v.Category, v.Pattern, v.Snippet,
		)
	}
}

// TestAtomAuthoringLint_FiresOnKnownViolations is the lint engine's
// self-test: the regression-floor `TestAtomAuthoringLint` only proves
// the production corpus is clean, which silently passes if any rule has
// a typo / wrong char-class / wrong anchor (broken regex matches
// nothing → zero violations → green).
//
// This test constructs one synthetic atom per rule that should trip
// exactly that rule. If any rule stops firing, this test catches it
// before the broken pattern ships.
//
// Categories covered:
//   - regex rules: spec-id, handler-behavior-*, invisible-state, plan-doc
//   - axis-l: env-only title qualifier (HARD-FORBID)
//   - axis-k: abstraction-leak candidate (marker convention)
//   - axis-m: terminology-drift candidate (marker convention)
//   - axis-n: universal-atom env leak (marker convention)
func TestAtomAuthoringLint_FiresOnKnownViolations(t *testing.T) {
	t.Parallel()

	const fmHeader = "---\nphases: [idle]\n---\n"

	tests := []struct {
		name        string
		frontmatter string
		body        string
		wantCat     string
		wantPattern string // substring match — full pattern names include token suffixes
	}{
		// regex rules ----------------------------------------------------
		{
			name:        "spec-id-DM",
			body:        "The deploy mode invariant DM-2 means source IS target.\n",
			wantPattern: "spec-id",
			wantCat:     "spec-id",
		},
		{
			name:        "spec-id-INV",
			body:        "See INV-42 for the load-bearing assertion.\n",
			wantPattern: "spec-id",
			wantCat:     "spec-id",
		},
		{
			name:        "handler-behavior-handler",
			body:        "The deploy handler automatically enables the subdomain.\n",
			wantPattern: "handler-behavior-handler",
			wantCat:     "handler-behavior",
		},
		{
			name:        "handler-behavior-tool-auto",
			body:        "The tool will auto-enable the route on first call.\n",
			wantPattern: "handler-behavior-tool-auto",
			wantCat:     "handler-behavior",
		},
		{
			name:        "handler-behavior-zcp",
			body:        "ZCP writes the meta file on success.\n",
			wantPattern: "handler-behavior-zcp",
			wantCat:     "handler-behavior",
		},
		{
			name:        "invisible-state",
			body:        "FirstDeployedAt is stamped after the first deploy.\n",
			wantPattern: "invisible-state",
			wantCat:     "invisible-state",
		},
		{
			name:        "plan-doc",
			body:        "See plans/instruction-delivery-rewrite.md for context.\n",
			wantPattern: "plan-doc",
			wantCat:     "plan-doc",
		},
		// axis-L (HARD-FORBID env-only title qualifiers) -----------------
		{
			name:        "axis-l-title-em-dash-container",
			frontmatter: "---\nphases: [idle]\ntitle: \"Push-Dev Deploy Strategy — container\"\n---\n",
			body:        "Body without violations.\n",
			wantPattern: "title-env-only-qualifier",
			wantCat:     "axis-l",
		},
		{
			name:        "axis-l-title-paren-local",
			frontmatter: "---\nphases: [idle]\ntitle: \"Push-dev iteration cycle (local)\"\n---\n",
			body:        "Body without violations.\n",
			wantPattern: "title-env-only-qualifier",
			wantCat:     "axis-l",
		},
		{
			name:        "axis-l-heading-em-dash-container-env",
			frontmatter: "---\nphases: [idle]\n---\n",
			body:        "### Setup — container env\n",
			wantPattern: "heading-env-only-qualifier",
			wantCat:     "axis-l",
		},
		{
			name:        "axis-l-heading-plus-paired-env",
			frontmatter: "---\nphases: [idle]\n---\n",
			body:        "### Closing the task — local + push-git\n",
			wantPattern: "heading-env-only-qualifier",
			wantCat:     "axis-l",
		},
		// axis-K (abstraction leak) --------------------------------------
		{
			name:        "axis-k-do-not-use",
			body:        "Do NOT use the dev server in local mode.\n",
			wantPattern: "leak-candidate:do not use",
			wantCat:     "axis-k",
		},
		{
			name:        "axis-k-no-sshfs",
			body:        "There is no SSHFS in local mode.\n",
			wantPattern: "leak-candidate:no sshfs",
			wantCat:     "axis-k",
		},
		{
			name:        "axis-k-container-only",
			body:        "That mount is container-only.\n",
			wantPattern: "leak-candidate:container-only",
			wantCat:     "axis-k",
		},
		// axis-M (terminology drift) -------------------------------------
		{
			name:        "axis-m-the-platform",
			body:        "The platform routes traffic via subdomain.\n",
			wantPattern: "drift:the platform",
			wantCat:     "axis-m",
		},
		{
			name:        "axis-m-the-tool",
			body:        "Use the tool to verify the deploy.\n",
			wantPattern: "drift:the tool",
			wantCat:     "axis-m",
		},
		{
			name:        "axis-m-the-agent",
			body:        "The agent reads the envelope.\n",
			wantPattern: "drift:the agent",
			wantCat:     "axis-m",
		},
		// axis-N (universal-atom env leak) -------------------------------
		{
			name:        "axis-n-locally-universal",
			frontmatter: fmHeader, // no environments axis → universal
			body:        "Edit files locally and run the build.\n",
			wantPattern: "leak-candidate:locally",
			wantCat:     "axis-n",
		},
		{
			name:        "axis-n-sshfs-universal",
			frontmatter: fmHeader,
			body:        "Files live on the SSHFS mount.\n",
			wantPattern: "leak-candidate:sshfs",
			wantCat:     "axis-n",
		},
		{
			name:        "axis-n-var-www-universal",
			frontmatter: fmHeader,
			body:        "The runtime path is /var/www/{hostname}.\n",
			wantPattern: "leak-candidate:/var/www/",
			wantCat:     "axis-n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fm := tt.frontmatter
			if fm == "" {
				fm = fmHeader
			}
			atom := AtomFile{
				Name:    tt.name + "-fixture.md",
				Content: fm + tt.body,
			}
			violations := lintAtomCorpus([]AtomFile{atom})
			if len(violations) == 0 {
				t.Fatalf("expected rule %q (cat %q) to fire on fixture %q, got zero violations",
					tt.wantPattern, tt.wantCat, tt.body)
			}
			var sawTarget bool
			for _, v := range violations {
				if v.Category == tt.wantCat && strings.Contains(v.Pattern, tt.wantPattern) {
					sawTarget = true
					break
				}
			}
			if !sawTarget {
				t.Errorf("rule %q (cat %q) did not fire on fixture %q; got violations: %+v",
					tt.wantPattern, tt.wantCat, tt.body, violations)
			}
		})
	}
}

// TestAtomAuthoringLint_CleanFixtureYieldsZero proves a fixture that
// intentionally avoids every forbidden pattern produces zero violations.
// Counterpoint to FiresOnKnownViolations: it asserts the engine does NOT
// spuriously match clean prose.
func TestAtomAuthoringLint_CleanFixtureYieldsZero(t *testing.T) {
	t.Parallel()

	atom := AtomFile{
		Name: "clean-fixture.md",
		Content: "---\nphases: [idle]\nenvironments: [container, local]\ntitle: \"Clean fixture\"\n---\n" +
			"You observe the project state and choose the next action.\n" +
			"Service status transitions from PENDING to RUNNING after the first deploy.\n" +
			"Use zerops_workflow action=start to begin a develop session.\n",
	}
	violations := lintAtomCorpus([]AtomFile{atom})
	if len(violations) != 0 {
		t.Errorf("clean fixture produced %d violations: %+v", len(violations), violations)
	}
}

// TestAtomAuthoringLint_MultipleRulesOnOneAtom proves one atom can trip
// multiple rules independently — the rule engine doesn't short-circuit
// after the first match. Two rules on one line, plus a third on a
// separate line.
func TestAtomAuthoringLint_MultipleRulesOnOneAtom(t *testing.T) {
	t.Parallel()

	atom := AtomFile{
		Name: "multi-rule-fixture.md",
		Content: "---\nphases: [idle]\n---\n" +
			// One line, two rules: spec-id (DM-2) + handler-behavior-handler.
			"The handler automatically applies DM-2.\n" +
			// Separate line: plan-doc cross-ref.
			"See plans/test.md.\n",
	}
	violations := lintAtomCorpus([]AtomFile{atom})

	wantPatterns := map[string]bool{
		"spec-id":                  false,
		"handler-behavior-handler": false,
		"plan-doc":                 false,
	}
	for _, v := range violations {
		if _, ok := wantPatterns[v.Pattern]; ok {
			wantPatterns[v.Pattern] = true
		}
	}
	for pat, fired := range wantPatterns {
		if !fired {
			t.Errorf("expected rule %q to fire on multi-rule fixture; violations: %+v",
				pat, violations)
		}
	}
}

// TestAtomAuthoringLint_MarkersSuppressAxes proves the inline marker
// convention works for axes K, M, and N: a `<!-- axis-X-keep -->`
// comment on the same line, the previous non-blank line, or the next
// non-blank line suppresses the lint hit. Axis L is HARD-FORBID and
// SHOULD NOT respect markers — the L sub-test asserts that.
func TestAtomAuthoringLint_MarkersSuppressAxes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		wantClean bool // true = expect zero violations
	}{
		{
			name:      "axis-k-same-line-keep",
			body:      "Do NOT use that tool. <!-- axis-k-keep: signal-#3 -->\n",
			wantClean: true,
		},
		{
			name:      "axis-k-previous-line-keep",
			body:      "<!-- axis-k-keep: signal-#1 -->\nDo NOT run git init.\n",
			wantClean: true,
		},
		{
			name:      "axis-k-next-line-keep",
			body:      "Do NOT run git init.\n<!-- axis-k-keep: signal-#1 -->\n",
			wantClean: true,
		},
		{
			name:      "axis-k-drop-marker-also-suppresses",
			body:      "Do NOT run git init. <!-- axis-k-drop -->\n",
			wantClean: true,
		},
		{
			name:      "axis-m-marker-suppresses",
			body:      "The agent reads the envelope. <!-- axis-m-keep -->\n",
			wantClean: true,
		},
		{
			name:      "axis-n-marker-suppresses",
			body:      "Edit files locally. <!-- axis-n-keep -->\n",
			wantClean: true,
		},
		{
			name:      "axis-k-no-marker-fires",
			body:      "Do NOT use that tool.\n",
			wantClean: false,
		},
		{
			name: "axis-l-marker-DOES-NOT-suppress-hard-forbid",
			// Even with k/m/n markers nearby, axis-L doesn't honor them.
			// Heading must stay free of env-only qualifiers regardless.
			body:      "<!-- axis-k-keep -->\n### Setup — container\n<!-- axis-m-keep -->\n",
			wantClean: false, // axis-L doesn't honor any marker
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			atom := AtomFile{
				Name:    tt.name + "-fixture.md",
				Content: "---\nphases: [idle]\n---\n" + tt.body,
			}
			violations := lintAtomCorpus([]AtomFile{atom})
			if tt.wantClean && len(violations) > 0 {
				t.Errorf("expected zero violations (marker should suppress), got %d: %+v",
					len(violations), violations)
			}
			if !tt.wantClean && len(violations) == 0 {
				t.Errorf("expected at least one violation, got zero")
			}
		})
	}
}

// TestAtomAuthoringLint_PerEnvAtomExemptFromAxisN proves an atom whose
// `environments:` axis restricts to a single env (e.g. `[local]`) is
// exempt from axis-N (env-specific detail is on-axis there). Universal
// atoms (no `environments:` axis OR both values listed) DO get linted.
func TestAtomAuthoringLint_PerEnvAtomExemptFromAxisN(t *testing.T) {
	t.Parallel()

	body := "Edit files locally and run the build.\n"

	tests := []struct {
		name      string
		envAxis   string
		wantClean bool
	}{
		{
			name:      "no-environments-axis-is-universal",
			envAxis:   "",
			wantClean: false, // universal → axis-N fires
		},
		{
			name:      "both-envs-listed-is-universal",
			envAxis:   "environments: [container, local]\n",
			wantClean: false, // universal → axis-N fires
		},
		{
			name:      "local-only-exempt",
			envAxis:   "environments: [local]\n",
			wantClean: true, // per-env atom → axis-N skips
		},
		{
			name:      "container-only-exempt",
			envAxis:   "environments: [container]\n",
			wantClean: true, // per-env atom → axis-N skips
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			atom := AtomFile{
				Name:    tt.name + "-fixture.md",
				Content: "---\nphases: [idle]\n" + tt.envAxis + "---\n" + body,
			}
			violations := lintAtomCorpus([]AtomFile{atom})
			gotAxisN := false
			for _, v := range violations {
				if v.Category == "axis-n" {
					gotAxisN = true
					break
				}
			}
			if tt.wantClean && gotAxisN {
				t.Errorf("expected no axis-n violation, got: %+v", violations)
			}
			if !tt.wantClean && !gotAxisN {
				t.Errorf("expected axis-n violation, got: %+v", violations)
			}
		})
	}
}

// TestStripAxisMarkers proves marker comments are removed from rendered
// atom bodies — agents never see `<!-- axis-{k,m,n}-... -->` metadata.
// Marker-only lines collapse entirely; inline markers consume their
// leading whitespace so prose flow is preserved.
func TestStripAxisMarkers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no-markers-passthrough",
			in:   "Some prose.\n\nMore prose.",
			want: "Some prose.\n\nMore prose.",
		},
		{
			name: "inline-trailing-marker",
			in:   "Do NOT run git init. <!-- axis-k-keep: signal-#1 -->",
			want: "Do NOT run git init.",
		},
		{
			name: "marker-only-line-dropped",
			in:   "Above.\n<!-- axis-k-keep -->\nBelow.",
			want: "Above.\nBelow.",
		},
		{
			name: "drop-marker-also-stripped",
			in:   "Phrase. <!-- axis-n-drop -->",
			want: "Phrase.",
		},
		{
			name: "multiple-markers-stripped",
			in:   "First. <!-- axis-k-keep --> Then more. <!-- axis-m-keep -->",
			want: "First. Then more.",
		},
		{
			name: "all-three-axes-supported",
			in:   "<!-- axis-k-keep -->\n<!-- axis-m-keep -->\n<!-- axis-n-keep -->\nProse line.",
			want: "Prose line.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := StripAxisMarkers(tt.in)
			if got != tt.want {
				t.Errorf("StripAxisMarkers mismatch\n  in:   %q\n  got:  %q\n  want: %q",
					tt.in, got, tt.want)
			}
		})
	}
}
