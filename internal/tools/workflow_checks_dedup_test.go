package tools

import (
	"slices"
	"strings"
	"testing"
)

// TestCheckCrossReadmeGotchaUniqueness exercises the v9 cross-codebase
// duplicate-gotcha check. v15's nestjs-showcase had the same NATS-credential
// fact appearing in api + worker READMEs, SSHFS-ownership in api + worker,
// and zsc-execOnce burn in api + worker. Per-codebase authenticity floors
// didn't catch these because each README scored the duplicate as authentic
// independently. The cross-README check normalizes stems and fails when the
// same bolded stem appears in >1 README — each fact should live in exactly
// one codebase's README and the others link to it.
func TestCheckCrossReadmeGotchaUniqueness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		readmes   map[string]string
		wantPass  bool
		wantDupOf []string // substrings that must appear in the fail detail
	}{
		{
			name: "distinct gotchas across two READMEs pass",
			readmes: map[string]string{
				"apidev": dedupReadmeWithGotchas(
					"NATS `AUTHORIZATION_VIOLATION` with URL-embedded credentials",
					"S3 `forcePathStyle` required for MinIO",
					"Meilisearch connection uses http:// not https://",
				),
				"appdev": dedupReadmeWithGotchas(
					"Vite `allowedHosts` blocks Zerops subdomain",
					"`VITE_API_URL` undefined in dev mode",
					"Static deploy missing tilde suffix",
				),
			},
			wantPass: true,
		},
		{
			name: "duplicate NATS stem across api + worker fails",
			readmes: map[string]string{
				"apidev": dedupReadmeWithGotchas(
					"NATS credentials must be separate options",
					"S3 `forcePathStyle` required for MinIO",
					"Meilisearch connection uses http:// not https://",
				),
				"workerdev": dedupReadmeWithGotchas(
					"NATS credentials must be separate options",
					"Worker missing package.json triggers MODULE_NOT_FOUND",
					"Shared TypeORM entities need the worker's migration",
				),
			},
			wantPass:  false,
			wantDupOf: []string{"NATS credentials"},
		},
		{
			name: "three READMEs with two independent duplicate pairs fail",
			readmes: map[string]string{
				"apidev": dedupReadmeWithGotchas(
					"NATS `AUTHORIZATION_VIOLATION` with URL credentials",
					"SSHFS ownership blocks npm install",
					"`zsc execOnce` burn on failure",
				),
				"appdev": dedupReadmeWithGotchas(
					"Vite `allowedHosts` blocks Zerops subdomain",
					"`VITE_API_URL` undefined in dev mode",
					"Static deploy missing tilde suffix",
				),
				"workerdev": dedupReadmeWithGotchas(
					"NATS `AUTHORIZATION_VIOLATION` with URL credentials",
					"SSHFS ownership blocks npm install on fresh mount",
					"Worker package.json missing MODULE_NOT_FOUND",
				),
			},
			wantPass:  false,
			wantDupOf: []string{"NATS", "SSHFS"},
		},
		{
			name: "reworded near-clone still detected (add/remove stopwords)",
			readmes: map[string]string{
				"apidev": dedupReadmeWithGotchas(
					"Meilisearch SDK is ESM-only and breaks CJS imports",
					"MinIO `forcePathStyle` required",
					"CORS origin must include full subdomain URL",
				),
				"appdev": dedupReadmeWithGotchas(
					"Meilisearch SDK ESM-only breaks CJS imports",
					"VITE_API_URL build-time vs run-time placement",
					"Svelte curly braces in HTML attributes",
				),
			},
			wantPass:  false,
			wantDupOf: []string{"Meilisearch"},
		},
		{
			name: "single README — no cross-dup possible",
			readmes: map[string]string{
				"apidev": dedupReadmeWithGotchas(
					"NATS AUTHORIZATION_VIOLATION",
					"S3 forcePathStyle required",
					"Meilisearch http not https",
				),
			},
			wantPass: true,
		},
		{
			name: "empty knowledge-base fragments — nothing to compare",
			readmes: map[string]string{
				"apidev": "# Empty\n",
				"appdev": "# Empty\n",
			},
			wantPass: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			checks := checkCrossReadmeGotchaUniqueness(t.Context(), tt.readmes)
			if len(checks) == 0 {
				t.Fatal("expected at least one check result")
			}
			c := checks[0]
			if c.Name != "cross_readme_gotcha_uniqueness" {
				t.Errorf("unexpected check name %q", c.Name)
			}
			if tt.wantPass {
				if c.Status != "pass" {
					t.Errorf("expected pass, got %q: %s", c.Status, c.Detail)
				}
				return
			}
			if c.Status != "fail" {
				t.Errorf("expected fail, got %q: %s", c.Status, c.Detail)
			}
			for _, sub := range tt.wantDupOf {
				if !strings.Contains(c.Detail, sub) {
					t.Errorf("fail detail missing %q:\n%s", sub, c.Detail)
				}
			}
		})
	}
}

// TestCheckGotchaRestatesGuide locks in the rule that a gotcha may not
// restate an integration-guide item in the same README. v15's appdev README
// had three gotchas ("Vite allowedHosts blocks Zerops subdomain",
// "VITE_API_URL undefined in dev mode", "Static deploy missing tilde") that
// were normalized-token-identical to three integration-guide item headings
// immediately above them. A gotcha that tells the reader nothing the guide
// didn't already cover is wasted publication surface.
func TestCheckGotchaRestatesGuide(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		readme   string
		wantPass bool
		wantSubs []string
	}{
		{
			name: "gotcha distinct from IG items passes",
			readme: readmeWithIGItemsAndGotchas(
				[]string{
					"Bind NestJS to `0.0.0.0`",
					"Trust proxy headers",
					"NATS credentials as separate options",
				},
				[]string{
					"`npx tsc` resolves to wrong package",
					"SSHFS ownership blocks `npm install`",
					"CORS origin exact-match requires full subdomain URL",
				},
			),
			wantPass: true,
		},
		{
			name: "gotcha stem tokens match IG heading tokens → fail",
			readme: readmeWithIGItemsAndGotchas(
				[]string{
					"Add `.zerops.app` to Vite `allowedHosts`",
					"Use `VITE_API_URL` in `build.envVariables` for prod",
				},
				[]string{
					"Vite `allowedHosts` blocks Zerops subdomain",
					"Svelte curly braces in HTML attributes",
				},
			),
			wantPass: false,
			wantSubs: []string{"Vite", "allowedHosts"},
		},
		{
			name: "three appdev-style restatements — all flagged",
			readme: readmeWithIGItemsAndGotchas(
				[]string{
					"Add `.zerops.app` to Vite `allowedHosts`",
					"Use `VITE_API_URL` in `build.envVariables` for prod, `run.envVariables` for dev",
					"Static `deployFiles` tilde suffix (`./dist/~`)",
				},
				[]string{
					"Vite `allowedHosts` blocks Zerops subdomain",
					"`VITE_API_URL` undefined in dev mode",
					"Static deploy missing tilde suffix",
					"Svelte curly braces in HTML attributes",
				},
			),
			wantPass: false,
			wantSubs: []string{"restates", "allowedHosts", "VITE_API_URL", "tilde"},
		},
		{
			name: "boilerplate Adding zerops.yaml IG item never triggers a restatement fail",
			readme: readmeWithIGItemsAndGotchas(
				[]string{
					"Adding `zerops.yaml`",
				},
				[]string{
					"No `.env` files on Zerops",
					"TypeORM `synchronize: true` drops columns in prod",
				},
			),
			wantPass: true,
		},
		{
			name: "empty IG — check skipped gracefully",
			readme: readmeWithIGItemsAndGotchas(
				[]string{},
				[]string{
					"Meilisearch http not https",
				},
			),
			wantPass: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			checks := checkGotchaRestatesGuide("appdev", tt.readme)
			if len(checks) == 0 {
				if !tt.wantPass {
					t.Error("expected a fail check, got nothing")
				}
				return
			}
			c := checks[0]
			if c.Name != "appdev_gotcha_distinct_from_guide" {
				t.Errorf("unexpected check name %q", c.Name)
			}
			if tt.wantPass {
				if c.Status != "pass" {
					t.Errorf("expected pass, got %q: %s", c.Status, c.Detail)
				}
				return
			}
			if c.Status != "fail" {
				t.Errorf("expected fail, got %q: %s", c.Status, c.Detail)
			}
			for _, sub := range tt.wantSubs {
				if !strings.Contains(c.Detail, sub) {
					t.Errorf("fail detail missing %q:\n%s", sub, c.Detail)
				}
			}
		})
	}
}

// TestGotchaRestatesGuide_PerturbsCrossReadmeUniqueness — v8.104 Fix E.
// A failing gotcha_distinct_from_guide check must carry PerturbsChecks
// naming cross_readme_gotcha_uniqueness (rewording a gotcha stem flips
// its token set, which can newly collide with a sibling codebase's
// stem). The human-readable HowToFix must surface the perturbation so
// the author cross-checks siblings before re-running — not after the
// next failure round.
func TestGotchaRestatesGuide_PerturbsCrossReadmeUniqueness(t *testing.T) {
	t.Parallel()

	readme := readmeWithIGItemsAndGotchas(
		[]string{
			"Add `.zerops.app` to Vite `allowedHosts`",
		},
		[]string{
			"Vite `allowedHosts` blocks Zerops subdomain",
		},
	)
	checks := checkGotchaRestatesGuide("appdev", readme)
	if len(checks) == 0 {
		t.Fatal("expected a fail check, got nothing")
	}
	c := checks[0]
	if c.Status != "fail" {
		t.Fatalf("expected fail, got %q", c.Status)
	}
	if len(c.PerturbsChecks) == 0 {
		t.Fatal("failing gotcha_distinct_from_guide must carry PerturbsChecks (v8.104 Fix E)")
	}
	if !slices.Contains(c.PerturbsChecks, "cross_readme_gotcha_uniqueness") {
		t.Errorf("PerturbsChecks must include \"cross_readme_gotcha_uniqueness\"; got %v", c.PerturbsChecks)
	}
	// Human-readable HowToFix must surface the perturbation inline so
	// the author sees it in the failure payload.
	if !strings.Contains(c.HowToFix, "PerturbsChecks") {
		t.Errorf("HowToFix must name PerturbsChecks inline; got:\n%s", c.HowToFix)
	}
	if !strings.Contains(c.HowToFix, "cross_readme_gotcha_uniqueness") {
		t.Errorf("HowToFix must name the sibling check by name; got:\n%s", c.HowToFix)
	}
}

// TestExtractIntegrationGuideHeadings pulls H3 headings from inside the
// integration-guide fragment and strips any leading numeric enumeration
// ("### 2. Trust proxy headers" → "Trust proxy headers"). The boilerplate
// "Adding zerops.yaml" / "zerops.yaml" heading is returned too — callers
// are responsible for filtering it out since its skip rule is domain-
// specific (the zerops.yaml block is always required, not a user code
// change).
func TestExtractIntegrationGuideHeadings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ig   string
		want []string
	}{
		{
			name: "enumerated headings stripped",
			ig: `### 1. Adding ` + "`zerops.yaml`" + `
body

### 2. Trust proxy headers
body

### 3. Bind to 0.0.0.0
body`,
			want: []string{"Adding `zerops.yaml`", "Trust proxy headers", "Bind to 0.0.0.0"},
		},
		{
			name: "unnumbered headings returned verbatim",
			ig: `### ` + "`zerops.yaml`" + `
body

### Trust proxy headers
body`,
			want: []string{"`zerops.yaml`", "Trust proxy headers"},
		},
		{
			name: "no headings — empty list",
			ig:   "just prose, no headings\n```yaml\nzerops: {}\n```\n",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractIntegrationGuideHeadings(tt.ig)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d headings, want %d: %v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("heading[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// readmeWithGotchas builds a minimal README with just the knowledge-base
// fragment populated — the cross-README check only reads knowledge-base
// content, so the rest of the README can stay empty.
func dedupReadmeWithGotchas(stems ...string) string {
	var b strings.Builder
	b.WriteString("# Test\n\n")
	b.WriteString("<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n\n")
	b.WriteString("### Gotchas\n\n")
	for _, s := range stems {
		b.WriteString("- **")
		b.WriteString(s)
		b.WriteString("** — body describing the failure and fix.\n")
	}
	b.WriteString("\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n")
	return b.String()
}

// readmeWithIGItemsAndGotchas builds a README with integration-guide
// H3 headings and knowledge-base gotcha stems — the two inputs the
// restatement check correlates.
func readmeWithIGItemsAndGotchas(igItems, gotchas []string) string {
	var b strings.Builder
	b.WriteString("# Test\n\n")
	b.WriteString("<!-- #ZEROPS_EXTRACT_START:integration-guide# -->\n\n")
	for i, item := range igItems {
		b.WriteString("### ")
		if i > 0 { // enumerate items after the first for realism
			b.WriteString("2. ")
		}
		b.WriteString(item)
		b.WriteString("\n\nbody paragraph.\n\n")
	}
	b.WriteString("<!-- #ZEROPS_EXTRACT_END:integration-guide# -->\n\n")
	b.WriteString("<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n\n")
	b.WriteString("### Gotchas\n\n")
	for _, g := range gotchas {
		b.WriteString("- **")
		b.WriteString(g)
		b.WriteString("** — body describing the failure.\n")
	}
	b.WriteString("\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n")
	return b.String()
}
