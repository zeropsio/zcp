package workflow

import (
	"reflect"
	"testing"
)

// TestExtractGotchaStems parses knowledge-base markdown fragments and returns
// the bolded stem of each "- **X** — ..." bullet under a Gotchas heading.
// Handles both `### Gotchas` and `## Gotchas`, markdown backticks in stems,
// and dash/em-dash separators between stem and body.
func TestExtractGotchaStems(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name: "v10 apidev knowledge-base with 4 bolded gotchas",
			input: "### Gotchas\n\n" +
				"- **No .env files on Zerops.** All environment variables are injected at container start.\n" +
				"- **TypeORM `synchronize: true` must never run in the application process.** It can drop columns.\n" +
				"- **NestJS listens on `localhost` by default.** The Zerops load balancer cannot reach 127.0.0.1.\n" +
				"- **ts-node requires devDependencies.** The dev setup uses npm install.\n",
			want: []string{
				"No .env files on Zerops.",
				"TypeORM `synchronize: true` must never run in the application process.",
				"NestJS listens on `localhost` by default.",
				"ts-node requires devDependencies.",
			},
		},
		{
			name: "nestjs-minimal.md predecessor gotchas with em-dash separator",
			input: "## Gotchas\n" +
				"- **No `.env` files on Zerops** — Zerops injects all env vars as OS-level.\n" +
				"- **TypeORM `synchronize: true` in production** — never use in production.\n" +
				"- **NestJS listens on `localhost` by default** — explicit 0.0.0.0 required.\n" +
				"- **`ts-node` needs devDependencies** — dev setup uses npm install.\n",
			want: []string{
				"No `.env` files on Zerops",
				"TypeORM `synchronize: true` in production",
				"NestJS listens on `localhost` by default",
				"`ts-node` needs devDependencies",
			},
		},
		{
			name: "mixed bullet styles (asterisk and dash)",
			input: "### Gotchas\n\n" +
				"- **First stem** — body.\n" +
				"* **Second stem** — body.\n",
			want: []string{"First stem", "Second stem"},
		},
		{
			name:  "gotchas section absent returns empty",
			input: "# Recipe\n\n## Integration Guide\n\nNothing here.\n",
			want:  nil,
		},
		{
			name: "bullets without bold markers are skipped",
			input: "### Gotchas\n\n" +
				"- regular bullet, no bold\n" +
				"- **Bolded stem** — counted.\n",
			want: []string{"Bolded stem"},
		},
		{
			name: "section ends at next sibling H3",
			input: "### Gotchas\n\n" +
				"- **In-section stem** — kept.\n" +
				"\n### Other Section\n\n" +
				"- **Out-of-section stem** — dropped.\n",
			want: []string{"In-section stem"},
		},
		{
			name: "nested H4 inside ### Gotchas does NOT terminate",
			input: "### Gotchas\n\n" +
				"- **First stem** — kept.\n" +
				"\n#### Rationale\n\n" +
				"Some explanation.\n\n" +
				"- **Second stem after nested subheading** — kept.\n" +
				"\n### Other Section\n\n" +
				"- **Out-of-section stem** — dropped.\n",
			want: []string{"First stem", "Second stem after nested subheading"},
		},
		{
			name: "H2 Gotchas terminates at next H2 not at nested H3",
			input: "## Gotchas\n\n" +
				"- **First stem** — kept.\n" +
				"\n### Subcategory\n\n" +
				"- **Nested stem** — kept (still inside H2 Gotchas section).\n" +
				"\n## Integration Guide\n\n" +
				"- **Outside stem** — dropped.\n",
			want: []string{"First stem", "Nested stem"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractGotchaStems(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractGotchaStems():\n got  %q\n want %q", got, tt.want)
			}
		})
	}
}

// TestNormalizeStem strips backticks, punctuation, and stopwords, then returns
// the key-token slice that drives clone detection. Order is preserved for
// stable tests; the matcher treats this as a set.
func TestNormalizeStem(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want []string
	}{
		{"No `.env` files on Zerops", []string{"no", "env", "files", "zerops"}},
		{"No .env files on Zerops.", []string{"no", "env", "files", "zerops"}},
		{"TypeORM `synchronize: true` in production", []string{"typeorm", "synchronize", "production"}},
		{"TypeORM `synchronize: true` must never run in the application process.", []string{"typeorm", "synchronize", "run", "application", "process"}},
		{"`ts-node` needs devDependencies", []string{"ts", "node", "devdependencies"}},
		{"ts-node requires devDependencies.", []string{"ts", "node", "devdependencies"}},
		{"NestJS listens on `localhost` by default", []string{"nestjs", "listens", "localhost"}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got := normalizeStem(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("normalizeStem(%q):\n got  %v\n want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestStemsMatch covers the clone-detection logic: two stems match when their
// key-token sets overlap by at least min(|A|,|B|) * 0.67 (floor), with a hard
// minimum of 2 shared tokens. This catches lightly-reworded clones ("needs" →
// "requires", "in production" → "must never run in the application process")
// while letting genuinely different gotchas on related subjects coexist.
func TestStemsMatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"identical", "No .env files on Zerops", "No .env files on Zerops", true},
		{"backtick diff", "No `.env` files on Zerops", "No .env files on Zerops.", true},
		{"needs vs requires", "`ts-node` needs devDependencies", "ts-node requires devDependencies.", true},
		{"synchronize elaborated", "TypeORM `synchronize: true` in production", "TypeORM `synchronize: true` must never run in the application process.", true},
		{"listens on localhost", "NestJS listens on `localhost` by default", "NestJS listens on `localhost` by default.", true},
		{"different topics - meilisearch vs env", "Meilisearch SDK is ESM-only", "No .env files on Zerops", false},
		{"different topics - seeder vs synchronize", "Auto-indexing skips on redeploy seed runs", "TypeORM `synchronize: true` in production", false},
		{"different topics - forcePathStyle vs trust proxy", "forcePathStyle for object storage", "Trust proxy for L7 balancer", false},
		{"single-word stems never match (below min floor)", "zerops", "zerops", false},
		{"empty stems never match", "", "something", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := stemsMatch(normalizeStem(tt.a), normalizeStem(tt.b))
			if got != tt.want {
				t.Errorf("stemsMatch(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestPredecessorGotchaStems resolves the direct predecessor via the
// knowledge-store discovery path that recipeKnowledgeChain already uses and
// returns the bolded stems from its Gotchas section. Separate tiers and
// missing Gotchas sections must both return nil without panicking.
func TestPredecessorGotchaStems(t *testing.T) {
	t.Parallel()

	minimalContent := "# NestJS Minimal\n\n" +
		"## Gotchas\n" +
		"- **No `.env` files on Zerops** — body.\n" +
		"- **TypeORM `synchronize: true` in production** — body.\n" +
		"- **NestJS listens on `localhost` by default** — body.\n"

	minimalWithoutGotchas := "# Bare Minimal\n\n## 1. Adding `zerops.yaml`\n\n```yaml\nzerops: []\n```\n"

	provider := &mockRecipeProvider{
		recipes: map[string]string{
			"nestjs-minimal": minimalContent,
			"bare-minimal":   minimalWithoutGotchas,
		},
	}

	tests := []struct {
		name string
		plan *RecipePlan
		want []string
	}{
		{
			name: "showcase resolves its direct framework predecessor",
			plan: &RecipePlan{
				Framework:   "nestjs",
				Slug:        "nestjs-showcase",
				RuntimeType: "nodejs@24",
			},
			want: []string{
				"No `.env` files on Zerops",
				"TypeORM `synchronize: true` in production",
				"NestJS listens on `localhost` by default",
			},
		},
		{
			name: "predecessor without Gotchas section returns nil",
			plan: &RecipePlan{
				Framework:   "bare",
				Slug:        "bare-showcase",
				RuntimeType: "nodejs@24",
			},
			want: nil,
		},
		{
			name: "minimal tier has no predecessor at delta 1 from hello-world in this mock",
			plan: &RecipePlan{
				Framework:   "nestjs",
				Slug:        "nestjs-minimal",
				RuntimeType: "nodejs@24",
			},
			want: nil,
		},
		{
			name: "nil plan returns nil without panicking",
			plan: nil,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := PredecessorGotchaStems(tt.plan, provider)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("PredecessorGotchaStems():\n got  %q\n want %q", got, tt.want)
			}
		})
	}
}

// TestCountNetNewGotchas counts emitted gotcha stems whose key-token sets
// don't match any predecessor stem. This is the forcing function that makes
// the showcase exceed the injected predecessor's baseline — v10 cloned 4 for
// 4 and would return 0 here; v7 cloned 4 and added 4 net-new → returns 4.
func TestCountNetNewGotchas(t *testing.T) {
	t.Parallel()
	predecessor := []string{
		"No `.env` files on Zerops",
		"TypeORM `synchronize: true` in production",
		"NestJS listens on `localhost` by default",
		"`ts-node` needs devDependencies",
	}
	tests := []struct {
		name    string
		emitted []string
		want    int
	}{
		{
			name: "v10 pattern — all 4 emitted stems clone predecessor",
			emitted: []string{
				"No .env files on Zerops.",
				"TypeORM `synchronize: true` must never run in the application process.",
				"NestJS listens on `localhost` by default.",
				"ts-node requires devDependencies.",
			},
			want: 0,
		},
		{
			name: "v7 pattern — 4 clones + 4 net-new stems",
			emitted: []string{
				"No .env files on Zerops",
				"TypeORM synchronize is unsafe in production",
				"NestJS listens on localhost by default",
				"ts-node requires devDependencies",
				"Meilisearch SDK is ESM-only",
				"Auto-indexing skips on redeploy seed runs",
				"forcePathStyle for object storage with MinIO",
				"CORS origin comes from project env var",
			},
			want: 4,
		},
		{
			name:    "no predecessor — every emitted stem is net-new",
			emitted: []string{"Alpha", "Beta gamma delta"},
			want:    2,
		},
		{
			name:    "empty emitted",
			emitted: nil,
			want:    0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CountNetNewGotchas(tt.emitted, predecessor)
			if got != tt.want {
				t.Errorf("CountNetNewGotchas() = %d, want %d", got, tt.want)
			}
		})
	}
}
