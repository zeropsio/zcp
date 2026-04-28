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
	if msg := checkSlotShape("root/intro", body); msg == "" {
		t.Error("oversize root/intro should be refused")
	}
}

func TestCheckSlotShape_RootIntro_RefusesHeadings(t *testing.T) {
	t.Parallel()
	if msg := checkSlotShape("root/intro", "## Heading"); msg == "" {
		t.Error("root/intro with H2 should be refused")
	}
}

func TestCheckSlotShape_RootIntro_AcceptsShortPlain(t *testing.T) {
	t.Parallel()
	if msg := checkSlotShape("root/intro", "A short framing sentence."); msg != "" {
		t.Errorf("plain root/intro should pass: %s", msg)
	}
}

func TestCheckSlotShape_EnvIntro_RefusesNestedExtractMarkers(t *testing.T) {
	t.Parallel()
	body := "Some intro text\n<!-- #ZEROPS_EXTRACT_START -->\nmore text"
	if msg := checkSlotShape("env/0/intro", body); msg == "" {
		t.Error("env intro with nested extract marker should be refused (R-15-3)")
	} else if !strings.Contains(msg, "R-15-3") {
		t.Errorf("refusal should name R-15-3: %s", msg)
	}
}

func TestCheckSlotShape_EnvIntro_AcceptsClean(t *testing.T) {
	t.Parallel()
	if msg := checkSlotShape("env/3/intro", "Tier 3 small-prod single-region."); msg != "" {
		t.Errorf("clean env intro should pass: %s", msg)
	}
}

func TestCheckSlotShape_EnvImportComments_RefusesOversize(t *testing.T) {
	t.Parallel()
	body := strings.Repeat("# line\n", 9)
	if msg := checkSlotShape("env/3/import-comments/db", body); msg == "" {
		t.Error("oversized env import comment block should be refused")
	}
}

func TestCheckSlotShape_SlottedIG_RefusesMultiHeading(t *testing.T) {
	t.Parallel()
	body := "### 2. Trust the L7\nbody\n### 3. Drain on SIGTERM\nbody"
	if msg := checkSlotShape("codebase/api/integration-guide/2", body); msg == "" {
		t.Error("multi-heading IG slot should be refused (R-15-5)")
	}
}

func TestCheckSlotShape_SlottedIG_RefusesNoHeading(t *testing.T) {
	t.Parallel()
	body := "Just prose, no heading"
	if msg := checkSlotShape("codebase/api/integration-guide/2", body); msg == "" {
		t.Error("IG slot without exactly one ### heading should be refused")
	}
}

func TestCheckSlotShape_SlottedIG_AcceptsSingleHeading(t *testing.T) {
	t.Parallel()
	body := "### 2. Trust the reverse proxy\n\nSet trust proxy."
	if msg := checkSlotShape("codebase/api/integration-guide/2", body); msg != "" {
		t.Errorf("clean IG slot should pass: %s", msg)
	}
}

func TestCheckSlotShape_LegacyIG_NotConstrained(t *testing.T) {
	t.Parallel()
	// Legacy single-fragment IG (no /<n>) is back-compat and unconstrained.
	body := "### 2. Trust the L7\nbody\n### 3. Drain on SIGTERM\nbody"
	if msg := checkSlotShape("codebase/api/integration-guide", body); msg != "" {
		t.Errorf("legacy IG single-fragment should not be subject to slot constraints: %s", msg)
	}
}

func TestCheckSlotShape_KB_RefusesNonTopicBullet(t *testing.T) {
	t.Parallel()
	body := "- a free-prose bullet without **Topic**"
	if msg := checkSlotShape("codebase/api/knowledge-base", body); msg == "" {
		t.Error("KB bullet without **Topic** prefix should be refused")
	}
}

func TestCheckSlotShape_KB_AcceptsTopicShape(t *testing.T) {
	t.Parallel()
	body := "- **Custom response headers** — browsers strip headers cross-origin.\n- **CORS expose** — fix via exposeHeaders."
	if msg := checkSlotShape("codebase/api/knowledge-base", body); msg != "" {
		t.Errorf("KB with topic-shape bullets should pass: %s", msg)
	}
}

func TestCheckSlotShape_KB_RefusesOverEightBullets(t *testing.T) {
	t.Parallel()
	body := strings.Repeat("- **T** — body\n", 9)
	if msg := checkSlotShape("codebase/api/knowledge-base", body); msg == "" {
		t.Error("KB > 8 bullets should be refused")
	}
}

func TestCheckSlotShape_KB_AcceptsBulletlessBody(t *testing.T) {
	t.Parallel()
	// Bulletless body is content-degraded but structurally vacuous; the
	// validator (validateCodebaseKB) catches the "must have bullets"
	// quality concern, not slot-shape refusal.
	if msg := checkSlotShape("codebase/api/knowledge-base", "scaffold body"); msg != "" {
		t.Errorf("bulletless KB body should pass slot-shape: %s", msg)
	}
}

func TestCheckSlotShape_ZeropsYamlComments_RefusesOversize(t *testing.T) {
	t.Parallel()
	body := strings.Repeat("# line\n", 7)
	if msg := checkSlotShape("codebase/api/zerops-yaml-comments/run.envVariables", body); msg == "" {
		t.Error("zerops-yaml-comments block > 6 lines should be refused")
	}
}

func TestCheckSlotShape_CodebaseIntro_RefusesHeadings(t *testing.T) {
	t.Parallel()
	if msg := checkSlotShape("codebase/api/intro", "## H2 in intro"); msg == "" {
		t.Error("codebase intro with ## should be refused")
	}
}

// CLAUDE.md slot-shape refusal — R-15-4 closure. Bodies must be
// /init-shaped, Zerops-free.

func TestCheckSlotShape_ClaudeMD_RefusesZeropsHeading(t *testing.T) {
	t.Parallel()
	body := "## Build & run\n- npm test\n## Zerops service facts\n- db: postgres\n## Architecture\n- src/"
	if msg := checkSlotShape("codebase/api/claude-md", body); msg == "" {
		t.Error("claude-md with `## Zerops` heading should be refused")
	}
}

func TestCheckSlotShape_ClaudeMD_Refuses_zsc_Token(t *testing.T) {
	t.Parallel()
	body := "## Build & run\n- npm test\n## Architecture\n- run zsc noop\n- src/"
	if msg := checkSlotShape("codebase/api/claude-md", body); msg == "" {
		t.Error("claude-md with `zsc` token should be refused")
	}
}

func TestCheckSlotShape_ClaudeMD_Refuses_zerops_Token(t *testing.T) {
	t.Parallel()
	body := "## Build & run\n- npm test\n## Architecture\n- call zerops_deploy\n- src/"
	if msg := checkSlotShape("codebase/api/claude-md", body); msg == "" {
		t.Error("claude-md with `zerops_deploy` token should be refused")
	}
}

func TestCheckSlotShape_ClaudeMD_Refuses_zcp_Token(t *testing.T) {
	t.Parallel()
	body := "## Build & run\n- zcp sync push\n## Architecture\n- src/"
	if msg := checkSlotShape("codebase/api/claude-md", body); msg == "" {
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
	if msg := checkSlotShape("codebase/api/claude-md", body); msg != "" {
		t.Errorf("clean /init-style claude-md should pass: %s", msg)
	}
}

func TestCheckSlotShape_ClaudeMD_RefusesOverSizeLines(t *testing.T) {
	t.Parallel()
	body := "## Build & run\n## Architecture\n" + strings.Repeat("body line\n", 100)
	if msg := checkSlotShape("codebase/api/claude-md", body); msg == "" {
		t.Error("claude-md > 80 lines should be refused")
	}
}

func TestCheckSlotShape_ClaudeMD_RefusesNoH2(t *testing.T) {
	t.Parallel()
	if msg := checkSlotShape("codebase/api/claude-md", "# title\nbody"); msg == "" {
		t.Error("claude-md without `## ` sections should be refused")
	}
}

func TestCheckSlotShape_LegacyClaudeMDSubslots_NotConstrained(t *testing.T) {
	t.Parallel()
	// Legacy `claude-md/service-facts` and `claude-md/notes` sub-slots stay
	// back-compat-accepted (per §6.5) — slot-shape refusal targets only the
	// new single-slot `codebase/<h>/claude-md` form.
	if msg := checkSlotShape("codebase/api/claude-md/service-facts", "## Zerops service facts\n- port 3000"); msg != "" {
		t.Errorf("legacy claude-md sub-slot should be unconstrained: %s", msg)
	}
}

func TestCheckSlotShape_UnknownFragment_NoOpinion(t *testing.T) {
	t.Parallel()
	// Unknown fragment IDs fall through to the existing isValidFragmentID
	// path (handlers_fragments.go); slot_shape doesn't gate them.
	if msg := checkSlotShape("codebase/api/random", "anything"); msg != "" {
		t.Errorf("unknown fragment id should be unconstrained: %s", msg)
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
