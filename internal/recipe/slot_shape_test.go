package recipe

import (
	"strings"
	"testing"
)

// Run-16 §8 — slot-shape refusal at record-fragment time. Each test
// pins one constraint from the §8.1 refusal table.

func TestCheckSlotShape_RootIntro_RefusesOversize(t *testing.T) {
	t.Parallel()
	body := strings.Repeat("a", 501)
	if msgs := checkSlotShape("root/intro", body); len(msgs) == 0 {
		t.Error("oversize root/intro should be refused")
	}
}

func TestCheckSlotShape_RootIntro_RefusesHeadings(t *testing.T) {
	t.Parallel()
	if msgs := checkSlotShape("root/intro", "## Heading"); len(msgs) == 0 {
		t.Error("root/intro with H2 should be refused")
	}
}

func TestCheckSlotShape_RootIntro_AcceptsShortPlain(t *testing.T) {
	t.Parallel()
	if msgs := checkSlotShape("root/intro", "A short framing sentence."); len(msgs) > 0 {
		t.Errorf("plain root/intro should pass: %v", msgs)
	}
}

func TestCheckSlotShape_EnvIntro_RefusesNestedExtractMarkers(t *testing.T) {
	t.Parallel()
	body := "Some intro text\n<!-- #ZEROPS_EXTRACT_START -->\nmore text"
	if msgs := checkSlotShape("env/0/intro", body); len(msgs) == 0 {
		t.Error("env intro with nested extract marker should be refused (R-15-3)")
	} else if !strings.Contains(strings.Join(msgs, "\n"), "R-15-3") {
		t.Errorf("refusal should name R-15-3: %v", msgs)
	}
}

func TestCheckSlotShape_EnvIntro_AcceptsClean(t *testing.T) {
	t.Parallel()
	if msgs := checkSlotShape("env/3/intro", "Tier 3 small-prod single-region."); len(msgs) > 0 {
		t.Errorf("clean env intro should pass: %v", msgs)
	}
}

func TestCheckSlotShape_EnvImportComments_RefusesOversize(t *testing.T) {
	t.Parallel()
	body := strings.Repeat("# line\n", 9)
	if msgs := checkSlotShape("env/3/import-comments/db", body); len(msgs) == 0 {
		t.Error("oversized env import comment block should be refused")
	}
}

func TestCheckSlotShape_SlottedIG_RefusesMultiHeading(t *testing.T) {
	t.Parallel()
	body := "### 2. Trust the L7\nbody\n### 3. Drain on SIGTERM\nbody"
	if msgs := checkSlotShape("codebase/api/integration-guide/2", body); len(msgs) == 0 {
		t.Error("multi-heading IG slot should be refused (R-15-5)")
	}
}

func TestCheckSlotShape_SlottedIG_RefusesNoHeading(t *testing.T) {
	t.Parallel()
	body := "Just prose, no heading"
	if msgs := checkSlotShape("codebase/api/integration-guide/2", body); len(msgs) == 0 {
		t.Error("IG slot without exactly one ### heading should be refused")
	}
}

func TestCheckSlotShape_SlottedIG_AcceptsSingleHeading(t *testing.T) {
	t.Parallel()
	body := "### 2. Trust the reverse proxy\n\nSet trust proxy."
	if msgs := checkSlotShape("codebase/api/integration-guide/2", body); len(msgs) > 0 {
		t.Errorf("clean IG slot should pass: %v", msgs)
	}
}

func TestCheckSlotShape_LegacyIG_NotConstrained(t *testing.T) {
	t.Parallel()
	// Legacy single-fragment IG (no /<n>) is back-compat and unconstrained.
	body := "### 2. Trust the L7\nbody\n### 3. Drain on SIGTERM\nbody"
	if msgs := checkSlotShape("codebase/api/integration-guide", body); len(msgs) > 0 {
		t.Errorf("legacy IG single-fragment should not be subject to slot constraints: %v", msgs)
	}
}

func TestCheckSlotShape_KB_RefusesNonTopicBullet(t *testing.T) {
	t.Parallel()
	body := "- a free-prose bullet without **Topic**"
	if msgs := checkSlotShape("codebase/api/knowledge-base", body); len(msgs) == 0 {
		t.Error("KB bullet without **Topic** prefix should be refused")
	}
}

func TestCheckSlotShape_KB_AcceptsTopicShape(t *testing.T) {
	t.Parallel()
	// Stems must carry a symptom-first signal per Tranche 2 — first uses
	// HTTP status (`403`), second uses backtick-quoted error string.
	body := "- **403 on custom response headers** — browsers strip non-CORS-safelisted headers cross-origin.\n- **`Access-Control-Expose-Headers` missing** — fix via exposeHeaders."
	if msgs := checkSlotShape("codebase/api/knowledge-base", body); len(msgs) > 0 {
		t.Errorf("KB with topic-shape bullets should pass: %v", msgs)
	}
}

func TestCheckSlotShape_KB_RefusesOverEightBullets(t *testing.T) {
	t.Parallel()
	// Stems carry a symptom-first signal so the cap check (not the stem
	// heuristic) is what fires.
	body := strings.Repeat("- **HTTP 503 on boot** — body\n", 9)
	if msgs := checkSlotShape("codebase/api/knowledge-base", body); len(msgs) == 0 {
		t.Error("KB > 8 bullets should be refused")
	}
}

func TestCheckSlotShape_KB_AcceptsBulletlessBody(t *testing.T) {
	t.Parallel()
	// Bulletless body is content-degraded but structurally vacuous; the
	// validator (validateCodebaseKB) catches the "must have bullets"
	// quality concern, not slot-shape refusal.
	if msgs := checkSlotShape("codebase/api/knowledge-base", "scaffold body"); len(msgs) > 0 {
		t.Errorf("bulletless KB body should pass slot-shape: %v", msgs)
	}
}

func TestCheckSlotShape_ZeropsYamlComments_RefusesOversize(t *testing.T) {
	t.Parallel()
	body := strings.Repeat("# line\n", 7)
	if msgs := checkSlotShape("codebase/api/zerops-yaml-comments/run.envVariables", body); len(msgs) == 0 {
		t.Error("zerops-yaml-comments block > 6 lines should be refused")
	}
}

func TestCheckSlotShape_CodebaseIntro_RefusesHeadings(t *testing.T) {
	t.Parallel()
	if msgs := checkSlotShape("codebase/api/intro", "## H2 in intro"); len(msgs) == 0 {
		t.Error("codebase intro with ## should be refused")
	}
}

// CLAUDE.md slot-shape refusal — R-15-4 closure. Bodies must be
// /init-shaped, Zerops-free.

func TestCheckSlotShape_ClaudeMD_RefusesZeropsHeading(t *testing.T) {
	t.Parallel()
	body := "## Build & run\n- npm test\n## Zerops service facts\n- db: postgres\n## Architecture\n- src/"
	if msgs := checkSlotShape("codebase/api/claude-md", body); len(msgs) == 0 {
		t.Error("claude-md with `## Zerops` heading should be refused")
	}
}

func TestCheckSlotShape_ClaudeMD_Refuses_zsc_Token(t *testing.T) {
	t.Parallel()
	body := "## Build & run\n- npm test\n## Architecture\n- run zsc noop\n- src/"
	if msgs := checkSlotShape("codebase/api/claude-md", body); len(msgs) == 0 {
		t.Error("claude-md with `zsc` token should be refused")
	}
}

func TestCheckSlotShape_ClaudeMD_Refuses_zerops_Token(t *testing.T) {
	t.Parallel()
	body := "## Build & run\n- npm test\n## Architecture\n- call zerops_deploy\n- src/"
	if msgs := checkSlotShape("codebase/api/claude-md", body); len(msgs) == 0 {
		t.Error("claude-md with `zerops_deploy` token should be refused")
	}
}

func TestCheckSlotShape_ClaudeMD_Refuses_zcp_Token(t *testing.T) {
	t.Parallel()
	body := "## Build & run\n- zcp sync push\n## Architecture\n- src/"
	if msgs := checkSlotShape("codebase/api/claude-md", body); len(msgs) == 0 {
		t.Error("claude-md with `zcp` token should be refused")
	}
}

func TestCheckSlotShape_ClaudeMD_AcceptsZeropsFreeContent(t *testing.T) {
	t.Parallel()
	body := `# api

NestJS REST API for the showcase.

## Build & run

- npm install
- npm run start:dev
- npm test

## Architecture

- src/main.ts — bootstrap
- src/app.module.ts — root module
- src/items/ — items REST controller`
	if msgs := checkSlotShape("codebase/api/claude-md", body); len(msgs) > 0 {
		t.Errorf("clean /init-style claude-md should pass: %v", msgs)
	}
}

func TestCheckSlotShape_ClaudeMD_RefusesOverSizeLines(t *testing.T) {
	t.Parallel()
	body := "## Build & run\n## Architecture\n" + strings.Repeat("body line\n", 100)
	if msgs := checkSlotShape("codebase/api/claude-md", body); len(msgs) == 0 {
		t.Error("claude-md > 80 lines should be refused")
	}
}

func TestCheckSlotShape_ClaudeMD_RefusesNoH2(t *testing.T) {
	t.Parallel()
	if msgs := checkSlotShape("codebase/api/claude-md", "# title\nbody"); len(msgs) == 0 {
		t.Error("claude-md without `## ` sections should be refused")
	}
}

func TestCheckSlotShape_LegacyClaudeMDSubslots_NotConstrained(t *testing.T) {
	t.Parallel()
	// Legacy `claude-md/service-facts` and `claude-md/notes` sub-slots stay
	// back-compat-accepted (per §6.5) — slot-shape refusal targets only the
	// new single-slot `codebase/<h>/claude-md` form.
	if msgs := checkSlotShape("codebase/api/claude-md/service-facts", "## Zerops service facts\n- port 3000"); len(msgs) > 0 {
		t.Errorf("legacy claude-md sub-slot should be unconstrained: %v", msgs)
	}
}

func TestCheckSlotShape_UnknownFragment_NoOpinion(t *testing.T) {
	t.Parallel()
	// Unknown fragment IDs fall through to the existing isValidFragmentID
	// path (handlers_fragments.go); slot_shape doesn't gate them.
	if msgs := checkSlotShape("codebase/api/random", "anything"); len(msgs) > 0 {
		t.Errorf("unknown fragment id should be unconstrained: %v", msgs)
	}
}

func TestClaudeMDFragmentRefusalForHostname_FlagsLeakedHostname(t *testing.T) {
	t.Parallel()

	body := "## Build & run\n- npm test\n## Architecture\n- connect to db.local"
	if msg := claudeMDFragmentRefusalForHostname(body, "db"); msg == "" {
		t.Error("body referencing managed-service hostname `db` should be refused")
	}

	if msg := claudeMDFragmentRefusalForHostname(body, "search"); msg != "" {
		t.Errorf("hostname `search` not in body should pass: %s", msg)
	}

	if msg := claudeMDFragmentRefusalForHostname(body, ""); msg != "" {
		t.Errorf("empty hostname should pass: %s", msg)
	}
}
