package recipe

import (
	"context"
	"strings"
	"testing"
)

// run48_h3_kb_validators_test.go — Run-48 substrate diff.
//
// Run-47 forensic (plans/run-47-validation.md "Critical finding"):
// citationBlockSplits requires `- **stem**` bullets or `### N. <title>`
// numbered IG headings. Every codebase KB authored across runs 44-47
// used un-numbered `### <stem>` H3, so the splitter returned nil and
// validateCitationDisplayAgreement skipped the body entirely — Item 5's
// display↔URL axis was structurally inert against published KB content
// for four runs.
//
// Plus a bidirectional gap: when the display IS a known friendly name
// (e.g. "zero-downtime deploys with multi-container setups") but the
// URL doesn't resolve to ANY guide (e.g. `#minrunningcontainers-`),
// validateCitationDisplayAgreement skipped with `matchedGuide == ""`.
// Three citations shipped through that gap in run-47 (worker KB #1, #2;
// apidev KB #1's display-text drift).
//
// And: validators_kb_quality.go's self-inflicted detection scans `^- `
// bullets via kbBulletStartRE — H3-shape KBs bypassed all four
// kb-quality validators (cited-guide boilerplate, self-inflicted shape,
// no-platform-mention, paraphrase). Run-47 shipped two clean
// self-inflicted bullets (apidev KB #6 Search-card-0-indexed re-intro,
// worker KB #4 MEILI_HOST naming-narration) because of this gap.
//
// This file pins all three fixes.

// TestCitationBlockSplits_UnumberedH3KB — citationBlockSplits MUST split
// un-numbered H3 KB bodies (the authored shape) AND keep doing the
// existing numbered-IG + bold-bullet splits. Table-driven across four
// input shapes.
func TestCitationBlockSplits_UnumberedH3KB(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		body      string
		wantMin   int    // minimum number of blocks
		wantExact int    // when >0, exact block count required (precedence pinning)
		mustHaveA string // substring that must appear in some block
		mustHaveB string // second substring (different block)
	}{
		{
			name: "bold-bullet KB shape (existing path)",
			body: `- **First topic** — body A here.
- **Second topic** — body B here.
`,
			wantMin:   2,
			mustHaveA: "**First topic**",
			mustHaveB: "**Second topic**",
		},
		{
			name: "numbered IG heading shape (existing path)",
			body: `### 1. Adding zerops.yaml

Some content here.

### 2. Importing project

More content.
`,
			wantMin:   2,
			mustHaveA: "### 1. Adding zerops.yaml",
			mustHaveB: "### 2. Importing project",
		},
		{
			name: "un-numbered H3 KB shape (NEW — the run-47 frontier)",
			body: "### TypeORM `synchronize: true` corrupts schema\n\n" +
				"Body A here for the first H3 stem.\n\n" +
				"### NATS boot crashes with `Authorization Violation`\n\n" +
				"Body B here for the second H3 stem.\n",
			wantMin:   2,
			mustHaveA: "TypeORM",
			mustHaveB: "NATS boot",
		},
		{
			name: "mixed body — bold-bullet wins (delimiter precedence)",
			body: `### Some heading that should not split

- **Bullet one** — body A.
- **Bullet two** — body B.
`,
			wantMin:   2,
			mustHaveA: "**Bullet one**",
			mustHaveB: "**Bullet two**",
		},
		{
			// Pinning precedence: igHeadingItemRE is tried before
			// kbHeadingRE, so the numbered item drives the split and the
			// un-numbered H3 below it falls inside whichever numbered
			// block contains it. Not a defect — this guards against
			// drift if a future contributor reorders the three
			// fallbacks in citationBlockSplits.
			name: "mixed numbered + unnumbered H3 — numbered wins (kbHeadingRE NOT consulted)",
			body: "### 1. Numbered item that opens the body\n\n" +
				"Body of the numbered IG item.\n\n" +
				"### Unnumbered H3 that should NOT produce a second block\n\n" +
				"Body that belongs inside the numbered block above.\n",
			wantMin:   1,
			wantExact: 1, // un-numbered H3 falls inside the single numbered block; NO second block.
			mustHaveA: "### 1. Numbered item",
			mustHaveB: "Unnumbered H3",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			blocks := citationBlockSplits(tc.body)
			if len(blocks) < tc.wantMin {
				t.Fatalf("got %d blocks, want >= %d; blocks=%q", len(blocks), tc.wantMin, blocks)
			}
			if tc.wantExact > 0 && len(blocks) != tc.wantExact {
				t.Fatalf("got %d blocks, want EXACTLY %d (precedence-pin); blocks=%q", len(blocks), tc.wantExact, blocks)
			}
			var foundA, foundB bool
			for _, b := range blocks {
				if strings.Contains(b, tc.mustHaveA) {
					foundA = true
				}
				if strings.Contains(b, tc.mustHaveB) {
					foundB = true
				}
			}
			if !foundA {
				t.Errorf("no block contained %q; blocks=%q", tc.mustHaveA, blocks)
			}
			if !foundB {
				t.Errorf("no block contained %q; blocks=%q", tc.mustHaveB, blocks)
			}
		})
	}
}

// TestValidateCitationDisplayAgreement_KnownDisplayUnknownURL_Refuses —
// the run-47 worker KB #1 + #2 defect: display text is the canonical
// friendly for `rolling-deploys` but the URL points at
// `zerops-yaml/specification#minrunningcontainers-`, which doesn't
// resolve to any known guide. The CURRENT validator's Check 1 skips
// (matchedGuide == ""); the new Check 4 must refuse with the new code
// kb-citation-display-name-without-canonical-url.
func TestValidateCitationDisplayAgreement_KnownDisplayUnknownURL_Refuses(t *testing.T) {
	t.Parallel()
	body := "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n" +
		"\n" +
		"### Missing `queue` option crashes exactly-once delivery\n" +
		"\n" +
		"NATS core pub/sub fans out to ALL subscribers by default. The Zerops " +
		"[zero-downtime deploys with multi-container setups](https://docs.zerops.io/zerops-yaml/specification#minrunningcontainers-) " +
		"reference covers how the `rolling-deploys` flow keeps that invariant during rollouts.\n" +
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n"
	vs, err := validateCodebaseKB(context.Background(), "codebases/worker/README.md", []byte(body), SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "kb-citation-display-name-without-canonical-url") {
		t.Errorf("expected kb-citation-display-name-without-canonical-url on known-display-unknown-URL; got %+v", vs)
	}
}

// TestValidateCitationDisplayAgreement_DisplayTextDriftOnCanonicalFriendlyName_Refuses
// — the run-47 apidev KB #1 defect: display says "zero-downtime deploys
// ACROSS multi-container setups" (one-word drift from the canonical
// friendly "WITH"); URL correctly resolves to `rolling-deploys`. The
// existing Check 1 (kb-citation-display-mismatch) MUST fire — but it
// can only fire if citationBlockSplits now produces a block from the
// un-numbered H3 KB shape.
func TestValidateCitationDisplayAgreement_DisplayTextDriftOnCanonicalFriendlyName_Refuses(t *testing.T) {
	t.Parallel()
	body := "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n" +
		"\n" +
		"### TypeORM `synchronize: true` corrupts schema on rolling deploys\n" +
		"\n" +
		"`TypeOrmModule.forRoot({ synchronize: true })` re-runs DDL on every container start. " +
		"With [zero-downtime deploys across multi-container setups](https://docs.zerops.io/features/scaling-ha) " +
		"the platform boots a new container alongside the old one.\n" +
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n"
	vs, err := validateCodebaseKB(context.Background(), "codebases/api/README.md", []byte(body), SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "kb-citation-display-mismatch") {
		t.Errorf("expected kb-citation-display-mismatch on display-text drift; got %+v", vs)
	}
}

// TestValidateKB_RejectsFirstPersonShape_OnH3Bullet — the run-47 apidev
// KB #6 + worker KB #4 defects. The self-inflicted-shape detector (V-4)
// scans `^- ` bullets via kbBulletStartRE; H3-shape KBs bypassed it.
// After the fix kbBulletBlocks recognizes un-numbered H3 bullets and the
// detector fires on both fixtures.
func TestValidateKB_RejectsFirstPersonShape_OnH3Bullet(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body []byte
	}{
		{
			name: "apidev KB #6 Search-card 0-indexed re-introduction (we discovered/the fix was)",
			body: []byte("<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n" +
				"\n" +
				"### Search-card badge reads `0 indexed` after the first deploy\n" +
				"\n" +
				"`SearchService.onModuleInit()` calls `ensureIndex()`. We discovered " +
				"on a fresh deploy, the seed inserts items into Postgres and the index " +
				"stays empty until the porter manually triggers a re-index. The fix " +
				"was folding the Meilisearch sync into `src/seed.ts` so the seed step " +
				"produces a consistent state.\n" +
				"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n"),
		},
		{
			name: "worker KB #4 MEILI_HOST naming narration (we wrote/feel free to rename)",
			body: []byte("<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n" +
				"\n" +
				"### `MEILI_HOST` is read first, `SEARCH_URL` is the fallback\n" +
				"\n" +
				"The worker reads `process.env.MEILI_HOST ?? process.env.SEARCH_URL` " +
				"when constructing the Meilisearch client. Our setup aliases " +
				"`SEARCH_URL: ${search_connectionString}`. Feel free to rename to " +
				"`MEILI_HOST` to match the api codebase's convention; both keys point " +
				"to the same internal endpoint, so the alignment is cosmetic.\n" +
				"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vs, err := validateCodebaseKB(context.Background(), "codebases/x/README.md", tc.body, SurfaceInputs{Plan: &Plan{}})
			if err != nil {
				t.Fatalf("validate: %v", err)
			}
			if !containsCode(vs, "kb-bullet-self-inflicted-shape") {
				t.Errorf("expected kb-bullet-self-inflicted-shape on H3-shape KB body; got %+v", vs)
			}
		})
	}
}

// TestFriendlyDisplayToGuide_NoCollisions — collision-safety pin for the
// run-48 reverse map. friendlyDisplayToGuide is built lazily by iterating
// CitationGuideURL and resolving each guide through FriendlyDisplayName.
// Today there are no collisions across the 22 guides; if a future
// contributor adds a guide whose friendly display lowercases to an
// already-mapped value, Go's unordered map iteration would make
// last-writer-wins NONDETERMINISTIC and the reverse lookup would
// silently return whichever guide won the race. Guard the bidirectional
// Check 4 by failing fast when two guides claim the same friendly key.
//
// No t.Parallel() — touches the package-level reverse map.
func TestFriendlyDisplayToGuide_NoCollisions(t *testing.T) {
	groups := map[string][]string{}
	for guide := range CitationGuideURL {
		friendly := FriendlyDisplayName(guide)
		if friendly == "" {
			continue // mirrors ensureFriendlyDisplayToGuide's skip-on-empty branch
		}
		key := strings.ToLower(friendly)
		groups[key] = append(groups[key], guide)
	}
	for friendlyLower, guides := range groups {
		if len(guides) > 1 {
			t.Errorf("friendly display %q (lower-cased) maps to %d guides %v — reverse-map last-writer-wins would be nondeterministic across map iteration. Adjust FriendlyDisplayName for one of these guides.",
				friendlyLower, len(guides), guides)
		}
	}
}

// TestValidateCitationDisplayAgreement_KnownURLUnknownDisplay_OnlyCheck1Fires —
// negative case for Check 4. When the URL DOES resolve to a known guide
// (rolling-deploys here) but the display text is NOT a known friendly
// display name, Check 1 (kb-citation-display-mismatch) MUST fire and
// Check 4 (kb-citation-display-name-without-canonical-url) MUST NOT —
// the new code is gated on `matchedGuide == ""`, so when a URL matches
// it stays out of the way.
func TestValidateCitationDisplayAgreement_KnownURLUnknownDisplay_OnlyCheck1Fires(t *testing.T) {
	t.Parallel()
	body := "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n" +
		"\n" +
		"### Some H3 stem that splits the body via kbHeadingRE\n" +
		"\n" +
		"The codebase relies on " +
		"[some random display text that is not a friendly name](https://docs.zerops.io/features/scaling-ha) " +
		"as the underlying mechanism.\n" +
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n"
	vs, err := validateCodebaseKB(context.Background(), "codebases/x/README.md", []byte(body), SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "kb-citation-display-mismatch") {
		t.Errorf("expected Check 1 (kb-citation-display-mismatch) to fire on known-URL/unknown-display; got %+v", vs)
	}
	if containsCode(vs, "kb-citation-display-name-without-canonical-url") {
		t.Errorf("did NOT expect Check 4 (kb-citation-display-name-without-canonical-url) on known-URL/unknown-display — Check 4 is gated on matchedGuide==\"\"; got %+v", vs)
	}
}

// TestValidateCitationDisplayAgreement_EmptyDisplayText_NoCheck4 — negative
// case exercising ensureFriendlyDisplayToGuide's skip-on-empty branch
// (validators_codebase.go:480-487). When the display text is empty AND
// the URL is unknown, the reverse-map lookup
// (`lookupGuideIDFromFriendlyDisplay("")`) MUST return "" and Check 4
// MUST NOT fire. Pin so an accidental "" key in the reverse map (the
// missing skip-empty branch) would be caught.
//
// Note: `markdownLinkRE` requires at least one character inside the
// display brackets (`[([^\]]+)\]`), so a literal `[]` link does not even
// register as a markdown link — Check 4 cannot fire because no link is
// parsed. We use a single-space display `[ ]` which DOES match the regex
// and trims to "" inside `lookupGuideIDFromFriendlyDisplay`, exercising
// the empty-key path through the reverse-map lookup itself.
func TestValidateCitationDisplayAgreement_EmptyDisplayText_NoCheck4(t *testing.T) {
	t.Parallel()
	body := "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n" +
		"\n" +
		"### Some H3 stem that splits the body via kbHeadingRE\n" +
		"\n" +
		"Some text with [ ](https://example.com/unknown-not-in-citation-map) " +
		"a link whose display is whitespace and URL is unknown.\n" +
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n"
	vs, err := validateCodebaseKB(context.Background(), "codebases/x/README.md", []byte(body), SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "kb-citation-display-name-without-canonical-url") {
		t.Errorf("did NOT expect Check 4 on empty/whitespace display text — lookupGuideIDFromFriendlyDisplay(\"\") returns \"\" and Check 4 MUST stay silent; got %+v", vs)
	}
	// Defensive: the lookup helper itself MUST return "" for both empty
	// and whitespace-only display text. Pins the skip-on-empty branch
	// in ensureFriendlyDisplayToGuide and the TrimSpace in
	// lookupGuideIDFromFriendlyDisplay.
	if got := lookupGuideIDFromFriendlyDisplay(""); got != "" {
		t.Errorf("lookupGuideIDFromFriendlyDisplay(\"\") = %q, want \"\"", got)
	}
	if got := lookupGuideIDFromFriendlyDisplay("   "); got != "" {
		t.Errorf("lookupGuideIDFromFriendlyDisplay(\"   \") = %q, want \"\"", got)
	}
}
